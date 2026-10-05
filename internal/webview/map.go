package webview

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// mapScript collects the actionable elements of a page, in the same spirit as
// vibium's browser_map: a ref-able element plus the label a person reads.
//
// Coordinates come back in CSS pixels from getBoundingClientRect, which is
// what the viewport metrics are expressed in, so the two can be reconciled.
// candidatesJS declares __mobiumCandidates(): the elements map lists, in the
// order it lists them. Shared with the actionability check, which picks its
// element by the same index, so the two can never disagree about which
// element a ref means.
//
// The candidates include those inside every frame the page can see into — a
// same-origin iframe, and frames nested in it — after the page's own, so a
// page with no frames numbers its elements as it always did. A cross-origin
// frame's document is closed to the page and is skipped. out.paths says
// which frame each one is in, "" for the page itself. MobiumApp's Frames
// page mapped its own button and none of its frames' (CHALLENGES 228).
//
// __mobiumRect is an element's rectangle in the top page's viewport: a
// frame's elements report theirs in the frame's, so each enclosing frame's
// position and border is added.
//
// __mobiumTopAt is what is on top at a point of the top page, descending
// through shadow roots, as Vibium does, and through frames the page can see
// into; a cross-origin frame is itself the answer.
const candidatesJS = `function __mobiumCandidates() {
  const sel = 'a,button,input,select,textarea,summary,[role=button],[role=link],[role=checkbox],[role=tab],[onclick],[contenteditable=true]';
  const out = [];
  out.paths = [];
  const visit = (doc, path) => {
    doc.querySelectorAll(sel).forEach(e => {
      const r = e.getBoundingClientRect();
      if (r.width <= 0 || r.height <= 0) return;
      const st = doc.defaultView.getComputedStyle(e);
      if (st.visibility === 'hidden' || st.display === 'none' || st.opacity === '0') return;
      out.push(e);
      out.paths.push(path);
    });
  };
  const frames = (doc, path) => {
    doc.querySelectorAll('iframe,frame').forEach((f, i) => {
      let d = null;
      try { d = f.contentDocument; } catch (_) {}
      if (!d || !d.documentElement) return;
      const r = f.getBoundingClientRect();
      if (r.width <= 0 || r.height <= 0) return;
      const p = (path ? path + ' >> ' : '') + (f.id ? f.tagName.toLowerCase() + '#' + f.id : f.tagName.toLowerCase() + '[' + i + ']');
      visit(d, p);
      frames(d, p);
    });
  };
  visit(document, '');
  frames(document, '');
  return out;
}
function __mobiumRect(el) {
  const r = el.getBoundingClientRect();
  let x = r.x, y = r.y, w = el.ownerDocument.defaultView;
  while (w && w !== window && w.frameElement) {
    const f = w.frameElement, fr = f.getBoundingClientRect();
    x += fr.x + f.clientLeft;
    y += fr.y + f.clientTop;
    w = w.parent;
  }
  return {x: x, y: y, width: r.width, height: r.height, left: x, top: y, right: x + r.width, bottom: y + r.height};
}
function __mobiumTopAt(x, y) {
  let doc = document, ox = 0, oy = 0;
  let hit = doc.elementFromPoint(x, y);
  for (;;) {
    while (hit && hit.shadowRoot) {
      const inner = hit.shadowRoot.elementFromPoint(x - ox, y - oy);
      if (!inner || inner === hit) break;
      hit = inner;
    }
    if (!hit || (hit.tagName !== 'IFRAME' && hit.tagName !== 'FRAME')) return hit;
    let d = null;
    try { d = hit.contentDocument; } catch (_) {}
    if (!d) return hit;
    const fr = hit.getBoundingClientRect();
    ox += fr.x + hit.clientLeft;
    oy += fr.y + hit.clientTop;
    doc = d;
    const inner = d.elementFromPoint(x - ox, y - oy);
    if (!inner) return hit;
    hit = inner;
  }
}`

const mapScript = `(() => {
  ` + candidatesJS + `
  const out = [];
  const cands = __mobiumCandidates();
  cands.forEach((e, i) => {
    const r = __mobiumRect(e);
    // A checkbox or radio's value is usually "on", which is not a name; the
    // label a person reads is. Only for those two: every other field keeps
    // what it shows.
    const t = (e.getAttribute('type') || '').toLowerCase();
    const named = (t === 'checkbox' || t === 'radio') && e.labels && e.labels.length ? e.labels[0].innerText : '';
    const label = (named || e.innerText || e.value || e.getAttribute('aria-label') ||
                   e.getAttribute('placeholder') || e.getAttribute('title') ||
                   e.getAttribute('alt') || '').trim().replace(/\s+/g, ' ').slice(0, 60);
    out.push({
      tag: e.tagName.toLowerCase(),
      type: (e.getAttribute('type') || '').toLowerCase(),
      role: e.getAttribute('role') || '',
      id: e.id || '',
      testid: e.getAttribute('data-testid') || '',
      label: label,
      disabled: !!e.disabled,
      frame: cands.paths[i],
      x: r.x, y: r.y, w: r.width, h: r.height
    });
  });
  return JSON.stringify(out);
})()`

// Element is one actionable element of a page, in CSS pixels.
type Element struct {
	Tag      string `json:"tag"`
	Type     string `json:"type"`
	Role     string `json:"role"`
	ID       string `json:"id"`
	TestID   string `json:"testid"`
	Label    string `json:"label"`
	Disabled bool   `json:"disabled"`
	// Frame is the frame the element is in, as a path of frame selectors
	// from the top page — "iframe#sameFrame >> iframe#nestedFrame" — or ""
	// for the page itself.
	Frame string  `json:"frame,omitempty"`
	X     float64 `json:"x"`
	// Where it lives, for acting on it: the execution context of the
	// cross-origin frame it is in (0 for the page and the frames the page
	// can see into), its index among that context's candidates, and the
	// path of that frame's element in the page.
	context int
	local   int
	box     string
	Y       float64 `json:"y"`
	W       float64 `json:"w"`
	H       float64 `json:"h"`
}

// evaluator is the one thing a page has to be able to do. Everything else
// here — the map, the text, the viewport — is a script run through it, which
// is why Android over CDP and iOS over Remote Web Inspector share all of it
// and differ only in transport.
type evaluator interface {
	Evaluate(ctx context.Context, expression string) (string, error)
}

// Page is what the tool layer needs from an attached web context, whichever
// protocol is underneath.
type Page interface {
	evaluator
	CookieJar
	LayoutMetrics(ctx context.Context) (*Metrics, error)
	Map(ctx context.Context) ([]Element, error)
	Text(ctx context.Context) (string, error)
	Healthy(ctx context.Context) bool
	Close() error
}

func mapPage(ctx context.Context, e evaluator) ([]Element, error) {
	raw, err := e.Evaluate(ctx, mapScript)
	if err != nil {
		return nil, err
	}
	var els []Element
	if err := json.Unmarshal([]byte(raw), &els); err != nil {
		return nil, fmt.Errorf("parse page map: %w", err)
	}
	return els, nil
}

// closedFramesScript counts the frames on a page, at any depth, whose
// document the page cannot see into: cross-origin ones. Their elements are
// not among the candidates, and a map that left them out without a word
// would read as a frame with nothing in it.
const closedFramesScript = `(() => {
  let n = 0;
  const walk = (doc) => doc.querySelectorAll('iframe,frame').forEach(f => {
    const r = f.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return;
    let d = null;
    try { d = f.contentDocument; } catch (_) {}
    if (!d || !d.documentElement) { n++; return; }
    walk(d);
  });
  walk(document);
  return String(n);
})()`

// ClosedFrames is how many frames on the page are closed to it.
func ClosedFrames(ctx context.Context, p evaluator) (int, error) {
	raw, err := p.Evaluate(ctx, closedFramesScript)
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscanf(strings.Trim(raw, `"`), "%d", &n); err != nil {
		return 0, fmt.Errorf("parse closed frames %q: %w", raw, err)
	}
	return n, nil
}

func pageText(ctx context.Context, e evaluator) (string, error) {
	return e.Evaluate(ctx, `document.body ? document.body.innerText : ""`)
}

// Map returns the page's actionable elements, its cross-origin frames' too.
func (s *Session) Map(ctx context.Context) ([]Element, error) {
	els, _, err := mapAll(ctx, s)
	return els, err
}

// Text returns the page's visible text.
func (s *Session) Text(ctx context.Context) (string, error) { return pageText(ctx, s) }

// Frame converts between a page's CSS pixels and the device's screen pixels.
//
// The native WebView element is the ground truth for both: its on-screen
// rectangle gives the origin, and its width against the page's CSS viewport
// width gives the scale. Deriving the scale from a devicePixelRatio the page
// reports would be wrong wherever the WebView does not fill the screen, which
// is most hybrid apps.
type Frame struct {
	OriginX, OriginY int
	Scale            float64
}

// NewFrame reconciles a native WebView node with a page's viewport.
//
// It also checks the two agree, which is not a formality. On iOS Safari the
// XCUIElementTypeWebView element covers the entire window — chrome included —
// while the page's viewport is 160 CSS points shorter, and the page cannot see
// its own inset: `screenY`, `visualViewport.offsetTop` and `pageTop` are all
// zero. Trusting the host's origin there puts every tap 186 device pixels too
// high, which is enough to hit a different link and not enough to look wrong.
//
// An embedded WKWebView or Android WebView — the case this is actually for —
// has a frame equal to its content, so the two agree and nothing changes. When
// they do not, there is no way to recover the origin from the hierarchy, so
// this refuses rather than approximating. Reading the page still works; only
// coordinates are affected.
func NewFrame(host uitree.Rect, m *Metrics) (*Frame, error) {
	if host.Width() <= 0 || host.Height() <= 0 {
		return nil, mobiumerr.New(mobiumerr.ElementNotReachable, "the webview has no on-screen area to map into")
	}
	if m == nil || m.CSSWidth <= 0 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the page reported no viewport size")
	}
	scale := float64(host.Width()) / m.CSSWidth

	// Compare like with like: the host's height in the page's own units.
	if m.CSSHeight > 0 {
		hostCSSHeight := float64(host.Height()) / scale
		// A tolerance rather than equality: a scrollbar or a rounded density
		// can move this by a pixel or two, and a hybrid app's WebView is
		// sometimes a hair taller than its viewport.
		const tolerance = 4
		if diff := hostCSSHeight - m.CSSHeight; diff > tolerance || diff < -tolerance {
			return nil, mobiumerr.New(mobiumerr.DeviceServer,
				"the WebView element is %.0f CSS pixels tall but the page's viewport is %.0f, "+
					"so the page does not fill its host and where it sits inside it cannot be "+
					"determined — the page reports no offset of its own. Taps are refused rather "+
					"than landing somewhere up to %.0f device pixels away. This is what mobile "+
					"Safari looks like, since it composes its chrome over a full-screen WebView; "+
					"an app's own WKWebView has a frame equal to its content and is unaffected. "+
					"Reading still works — app_text and app_map are unaffected.",
				hostCSSHeight, m.CSSHeight, (hostCSSHeight-m.CSSHeight)*scale)
		}
	}

	return &Frame{OriginX: host.X1, OriginY: host.Y1, Scale: scale}, nil
}

// ToDevice converts a CSS-pixel rectangle to device pixels on screen.
func (f *Frame) ToDevice(x, y, w, h float64) uitree.Rect {
	x1 := f.OriginX + int(x*f.Scale)
	y1 := f.OriginY + int(y*f.Scale)
	return uitree.Rect{
		X1: x1,
		Y1: y1,
		X2: x1 + int(w*f.Scale),
		Y2: y1 + int(h*f.Scale),
	}
}

// NeutralRole maps an element onto mobium's neutral role vocabulary, so `role=button`
// means the same thing whether it resolves in a native view or a WebView.
func (e Element) NeutralRole() string {
	if e.Role != "" {
		switch e.Role {
		case "button", "link", "checkbox", "tab", "radio":
			return e.Role
		}
	}
	switch e.Tag {
	case "a":
		return "link"
	case "button", "summary":
		return "button"
	case "select":
		return "input"
	case "textarea":
		return "input"
	case "input":
		switch e.Type {
		case "checkbox":
			return "checkbox"
		case "radio":
			return "radio"
		case "button", "submit", "reset", "image":
			return "button"
		default:
			return "input"
		}
	}
	return "button"
}

// Selector is the most durable CSS selector for this element, used as its
// locator so a ref survives a re-map.
//
// Inside a frame it is the frame's path, then " >> ", then the selector in
// the frame's document: CSS does not cross a frame, and the same id can be
// in a frame and on the page.
func (e Element) Selector() string {
	var sel string
	switch {
	case e.TestID != "":
		sel = fmt.Sprintf("[data-testid=%q]", e.TestID)
	case e.ID != "":
		sel = "#" + e.ID
	default:
		return ""
	}
	if e.Frame != "" {
		return e.Frame + " >> " + sel
	}
	return sel
}

// Both transports must satisfy the same contract, or the tool layer would have
// to know which platform it is on — which is the thing this package exists to
// prevent.
var (
	_ Page = (*Session)(nil)
	_ Page = (*IOSSession)(nil)
)

// sourceScript serializes the document with every password input's value
// attribute replaced by bullets, keeping the length — in a clone, so the page
// is not touched. What a user types into an input is a property, not an
// attribute, and outerHTML never carries it; a value written into the
// markup is an attribute, and would.
const sourceScript = `(() => {
  const doc = document.documentElement.cloneNode(true);
  let redacted = 0;
  for (const el of doc.querySelectorAll('input[type=password]')) {
    const v = el.getAttribute('value');
    if (v) { el.setAttribute('value', '•'.repeat([...v].length)); redacted++; }
  }
  const doctype = document.doctype ? new XMLSerializer().serializeToString(document.doctype) + '\n' : '';
  return JSON.stringify({html: doctype + doc.outerHTML, redacted});
})()`

// Source returns the page's markup as it stands now — the DOM serialized,
// not the HTML that was loaded — and how many password values it hid.
func Source(ctx context.Context, p Page) (string, int, error) {
	raw, err := p.Evaluate(ctx, sourceScript)
	if err != nil {
		return "", 0, err
	}
	var out struct {
		HTML     string `json:"html"`
		Redacted int    `json:"redacted"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return "", 0, mobiumerr.New(mobiumerr.DeviceServer, "the page's source did not come back as expected: %v", err)
	}
	return out.HTML, out.Redacted, nil
}
