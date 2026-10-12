package webview

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// InView is where an element of the map is once scrolled into view: its
// rectangle in CSS pixels of the viewport, whether it is wholly in view, and
// whether the page moved to bring it there.
type InView struct {
	// Status is "ok", or "not_found" for an element the page no longer has.
	Status string  `json:"status"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
	Whole  bool    `json:"whole"`
	Moved  bool    `json:"moved"`
}

// scrollIntoViewScript brings an element of the map into view the way an
// action does before it touches one (actionableScript), and checks nothing
// else: a scroll asks for the element on screen, not that it can be used.
const scrollIntoViewScript = `(() => {
  ` + candidatesJS + `
  const el = __mobiumCandidates()[%d];
  if (!el) return JSON.stringify({status:'not_found'});
  let r = __mobiumRect(el);
  const vw = window.innerWidth, vh = window.innerHeight;
  const out = (q) => q.top < 0 || q.left < 0 || q.bottom > vh || q.right > vw;
  const moved = out(r);
  if (moved) {
    el.scrollIntoView({block: 'center', inline: 'center', behavior: 'instant'});
    r = __mobiumRect(el);
  }
  return JSON.stringify({status: 'ok', x: r.x, y: r.y, w: r.width, h: r.height,
    whole: r.width > 0 && r.height > 0 && !out(r), moved});
})()`

// ScrollIntoView scrolls an element of the map into view and says where it
// is. One in a cross-origin frame is scrolled in its frame's own context,
// after the page has brought the frame into view, and the frame's place in
// the page is added, as CheckActionable does (CHALLENGES 229).
func ScrollIntoView(ctx context.Context, p Page, e Element) (*InView, error) {
	if e.context == 0 {
		return scrollIn(ctx, p, e.local)
	}
	c, ok := p.(contextual)
	if !ok {
		return &InView{Status: "not_found"}, nil
	}
	if _, err := frameBox(ctx, p, e.box, true); err != nil {
		return nil, err
	}
	v, err := scrollIn(ctx, evalIn{c, e.context}, e.local)
	if err != nil || v.Status != "ok" {
		return v, err
	}
	off, err := frameBox(ctx, p, e.box, false)
	if err != nil {
		return nil, err
	}
	if off == nil {
		return &InView{Status: "not_found"}, nil
	}
	v.X, v.Y = v.X+off.X, v.Y+off.Y
	return v, nil
}

func scrollIn(ctx context.Context, e evaluator, index int) (*InView, error) {
	raw, err := e.Evaluate(ctx, fmt.Sprintf(scrollIntoViewScript, index))
	if err != nil {
		return nil, err
	}
	var v InView
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "could not read where the page scrolled the element %q: %w", raw, err)
	}
	return &v, nil
}
