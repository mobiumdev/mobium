package webview

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// A cross-origin frame is closed to the page's scripts, so nothing run in
// the page can see into it — but each frame has an execution context of its
// own, and both protocols will run a script in it. Measured on MobiumApp's
// Frames page: Chrome's DevTools protocol announces one context per frame,
// the data: frame included, as Runtime.enable's events, each with its
// frameId; WebKit's Remote Web Inspector does the same once Page is enabled,
// and announces nothing without it. CHALLENGES 229.

// frameCtx is one frame of the page and the execution context its document
// runs in.
type frameCtx struct {
	ID       string
	ParentID string
	Name     string
	Context  int
}

// contextual is what a transport does for frames: list the page's frames with
// their contexts, children in the order the frame tree gives them, and run a
// script in one.
type contextual interface {
	frameContexts(ctx context.Context) ([]frameCtx, error)
	evaluateIn(ctx context.Context, contextID int, expression string) (string, error)
}

// rawFrame is a frame tree node as both protocols send it: CDP's
// Page.getFrameTree and WebKit's Page.getResourceTree share this shape.
type rawFrame struct {
	Frame struct {
		ID       string `json:"id"`
		ParentID string `json:"parentId"`
		Name     string `json:"name"`
	} `json:"frame"`
	ChildFrames []rawFrame `json:"childFrames"`
}

func flattenFrames(f rawFrame, out *[]frameCtx) {
	*out = append(*out, frameCtx{ID: f.Frame.ID, ParentID: f.Frame.ParentID, Name: f.Frame.Name})
	for _, c := range f.ChildFrames {
		flattenFrames(c, out)
	}
}

// contextCreated is Runtime.executionContextCreated in both protocols: CDP
// puts the frame in auxData, WebKit beside the id.
type contextCreated struct {
	Context struct {
		ID      int    `json:"id"`
		Type    string `json:"type"`
		FrameID string `json:"frameId"`
		AuxData struct {
			FrameID   string `json:"frameId"`
			IsDefault bool   `json:"isDefault"`
		} `json:"auxData"`
	} `json:"context"`
}

// frameOf is the frame a context belongs to, or "" for one that is not a
// frame's main world (an extension's, an isolated world).
func (c contextCreated) frameOf() string {
	if f := c.Context.AuxData.FrameID; f != "" {
		if c.Context.AuxData.IsDefault {
			return f
		}
		return ""
	}
	if c.Context.Type == "" || c.Context.Type == "normal" {
		return c.Context.FrameID
	}
	return ""
}

func attachContexts(frames []frameCtx, created []contextCreated) {
	byFrame := map[string]int{}
	for _, c := range created {
		if f := c.frameOf(); f != "" {
			byFrame[f] = c.Context.ID
		}
	}
	for i := range frames {
		frames[i].Context = byFrame[frames[i].ID]
	}
}

func (s *Session) frameContexts(ctx context.Context) ([]frameCtx, error) {
	raw, err := s.call(ctx, "Page.getFrameTree", nil)
	if err != nil {
		return nil, err
	}
	var tree struct {
		FrameTree rawFrame `json:"frameTree"`
	}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("parse frame tree: %w", err)
	}
	var frames []frameCtx
	flattenFrames(tree.FrameTree, &frames)

	// Runtime.enable announces the contexts that exist, once; turned off
	// first so it announces them again, and off again after so the session
	// does not go on receiving every console call as an event.
	if _, err := s.call(ctx, "Runtime.disable", nil); err != nil {
		return nil, err
	}
	var created []contextCreated
	_, err = s.callCollect(ctx, "Runtime.enable", nil, "Runtime.executionContextCreated", func(p json.RawMessage) {
		var c contextCreated
		if json.Unmarshal(p, &c) == nil {
			created = append(created, c)
		}
	})
	_, _ = s.call(ctx, "Runtime.disable", nil)
	if err != nil {
		return nil, err
	}
	attachContexts(frames, created)
	return frames, nil
}

func (s *IOSSession) frameContexts(ctx context.Context) ([]frameCtx, error) {
	// Without Page enabled WebKit announced no context at all on
	// Runtime.enable; with it, one per frame. Once per target: a second
	// Page.enable is an error.
	s.mu.Lock()
	on := s.pageOn
	s.mu.Unlock()
	if !on {
		if _, err := s.call(ctx, "Page.enable", map[string]any{}); err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.pageOn = true
		s.mu.Unlock()
	}
	raw, err := s.call(ctx, "Page.getResourceTree", map[string]any{})
	if err != nil {
		return nil, err
	}
	var tree struct {
		FrameTree rawFrame `json:"frameTree"`
	}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("parse frame tree: %w", err)
	}
	var frames []frameCtx
	flattenFrames(tree.FrameTree, &frames)

	if _, err := s.call(ctx, "Runtime.disable", map[string]any{}); err != nil {
		return nil, err
	}
	msgs, unsubscribe := s.in.subscribe()
	defer unsubscribe()
	if _, err := s.call(ctx, "Runtime.enable", map[string]any{}); err != nil {
		return nil, err
	}
	// The events come ahead of the reply, so they are queued by now; a
	// moment more lets any still in flight arrive.
	var created []contextCreated
	settle := time.After(300 * time.Millisecond)
collect:
	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				break collect
			}
			body := socketData(msg)
			if body == nil || body["method"] != "Target.dispatchMessageFromTarget" {
				continue
			}
			params, _ := body["params"].(map[string]any)
			text, _ := params["message"].(string)
			var ev struct {
				Method string         `json:"method"`
				Params contextCreated `json:"params"`
			}
			if json.Unmarshal([]byte(text), &ev) == nil && ev.Method == "Runtime.executionContextCreated" {
				created = append(created, ev.Params)
			}
		case <-settle:
			break collect
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	attachContexts(frames, created)
	return frames, nil
}

var (
	_ contextual = (*Session)(nil)
	_ contextual = (*IOSSession)(nil)
)

// frameAtJS declares __mobiumFrameAt(path): the frame element a path from
// the candidates names, "iframe#outer >> iframe[1]", or null.
const frameAtJS = `function __mobiumFrameAt(path) {
  let doc = document, el = null;
  for (const seg of path.split(' >> ')) {
    if (!doc) return null;
    const byID = seg.match(/^(iframe|frame)#(.+)$/);
    if (byID) {
      el = doc.getElementById(byID[2]);
    } else {
      const byIndex = seg.match(/^(iframe|frame)\[(\d+)\]$/);
      if (!byIndex) return null;
      el = doc.querySelectorAll('iframe,frame')[+byIndex[2]];
    }
    if (!el) return null;
    let d = null;
    try { d = el.contentDocument; } catch (_) {}
    doc = d;
  }
  return el;
}`

// framesScript lists the frames of every document the page can see into,
// in the order querySelectorAll gives them: the path the candidates use,
// the name the frame tree knows it by, whether it is closed to the page, the
// top-left of its content in the top page's viewport, and its own frames.
const framesScript = `(() => {
  ` + candidatesJS + `
  const walk = (doc, path) => {
    const out = [];
    doc.querySelectorAll('iframe,frame').forEach((f, i) => {
      const p = (path ? path + ' >> ' : '') + (f.id ? f.tagName.toLowerCase() + '#' + f.id : f.tagName.toLowerCase() + '[' + i + ']');
      let d = null;
      try { d = f.contentDocument; } catch (_) {}
      const open = !!(d && d.documentElement);
      const r = __mobiumRect(f);
      out.push({path: p, name: f.getAttribute('name') || f.id || '', closed: !open,
        shown: r.width > 0 && r.height > 0, x: r.x + f.clientLeft, y: r.y + f.clientTop,
        children: open ? walk(d, p) : []});
    });
    return out;
  };
  return JSON.stringify(walk(document, ''));
})()`

type pageFrame struct {
	Path     string      `json:"path"`
	Name     string      `json:"name"`
	Closed   bool        `json:"closed"`
	Shown    bool        `json:"shown"`
	X        float64     `json:"x"`
	Y        float64     `json:"y"`
	Children []pageFrame `json:"children"`
}

// pairFrames matches the frames a document holds with the frame tree's
// children of that document's frame: by name where both have one — WebKit
// names a frame by its element's name, or else its id — and the rest in
// order, when as many are left on each side. Chrome's tree names a frame
// only by its name attribute, so on Android an id-only frame pairs by order.
func pairFrames(doc []pageFrame, children []frameCtx) map[int]frameCtx {
	out := map[int]frameCtx{}
	used := map[int]bool{}
	for i, f := range doc {
		if f.Name == "" {
			continue
		}
		for j, c := range children {
			if !used[j] && c.Name == f.Name {
				out[i], used[j] = c, true
				break
			}
		}
	}
	var restDoc, restTree []int
	for i := range doc {
		if _, ok := out[i]; !ok {
			restDoc = append(restDoc, i)
		}
	}
	for j := range children {
		if !used[j] {
			restTree = append(restTree, j)
		}
	}
	if len(restDoc) == len(restTree) {
		for k, i := range restDoc {
			out[i] = children[restTree[k]]
		}
	}
	return out
}

// closedFrame is a cross-origin frame the page holds and the context its
// document runs in.
type closedFrame struct {
	pageFrame
	context int
}

// closedFrames finds the page's cross-origin frames and their contexts, and
// counts the shown ones it could not pair with a context.
func closedFrames(ctx context.Context, p evaluator, c contextual) ([]closedFrame, int, error) {
	raw, err := p.Evaluate(ctx, framesScript)
	if err != nil {
		return nil, 0, err
	}
	var top []pageFrame
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return nil, 0, fmt.Errorf("parse page frames: %w", err)
	}
	if !anyClosed(top) {
		return nil, 0, nil
	}
	frames, err := c.frameContexts(ctx)
	if err != nil || len(frames) == 0 {
		return nil, countClosed(top), err
	}
	children := func(parent string) []frameCtx {
		var out []frameCtx
		for _, f := range frames {
			if f.ParentID == parent {
				out = append(out, f)
			}
		}
		return out
	}
	var found []closedFrame
	unreached := 0
	var walk func(doc []pageFrame, parent string)
	walk = func(doc []pageFrame, parent string) {
		pairs := pairFrames(doc, children(parent))
		for i, f := range doc {
			fc, ok := pairs[i]
			switch {
			case f.Closed && f.Shown && ok && fc.Context != 0:
				found = append(found, closedFrame{pageFrame: f, context: fc.Context})
			case f.Closed && f.Shown:
				unreached++
			case !f.Closed && ok:
				walk(f.Children, fc.ID)
			case !f.Closed:
				unreached += countClosed(f.Children)
			}
		}
	}
	walk(top, frames[0].ID)
	return found, unreached, nil
}

func anyClosed(doc []pageFrame) bool { return countClosed(doc) > 0 }

func countClosed(doc []pageFrame) int {
	n := 0
	for _, f := range doc {
		if f.Closed && f.Shown {
			n++
		}
		n += countClosed(f.Children)
	}
	return n
}

// MapAll is the page's map, with what is inside its cross-origin frames,
// and how many of those it could not reach.
func MapAll(ctx context.Context, p Page) ([]Element, int, error) { return mapAll(ctx, p) }

func mapAll(ctx context.Context, p evaluator) ([]Element, int, error) {
	els, err := mapPage(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	for i := range els {
		els[i].local = i
	}
	c, ok := p.(contextual)
	if !ok {
		n, err := ClosedFrames(ctx, p)
		return els, n, err
	}
	closed, unreached, err := closedFrames(ctx, p, c)
	if err != nil {
		return els, unreached, nil
	}
	for _, f := range closed {
		raw, err := c.evaluateIn(ctx, f.context, mapScript)
		if err != nil {
			unreached++
			continue
		}
		var inner []Element
		if json.Unmarshal([]byte(raw), &inner) != nil {
			unreached++
			continue
		}
		for j, e := range inner {
			e.X += f.X
			e.Y += f.Y
			if e.Frame != "" {
				e.Frame = f.Path + " >> " + e.Frame
			} else {
				e.Frame = f.Path
			}
			e.context, e.local, e.box = f.context, j, f.Path
			els = append(els, e)
		}
	}
	return els, unreached, nil
}
