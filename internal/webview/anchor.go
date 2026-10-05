package webview

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// AnchorFrame finds where a page sits inside a WebView that is taller than
// it, from text both sides can see.
//
// NewFrame refuses a WebView taller than its page, because the page cannot
// say where in it it starts. That was written for mobile Safari, whose chrome
// is composed over a full-screen WebView, and it said an app's own WKWebView
// was unaffected. Kiwix's is not: it runs the full height of the screen,
// under iOS 26's bars, and its page starts below the navigation bar, so
// every tap into a ZIM page was refused (CHALLENGES 248).
//
// The native tree already says where the page's text is on screen: WebKit
// publishes each run of text as a static text with a real frame. A run whose
// words occur once among the WebView's texts, and once in the page, is an
// anchor; its frame against the page's own rectangle for the same words
// gives the page's origin. One anchor could be a coincidence, so two at
// least must agree, within a few device pixels, or the frame is refused as
// before.
func AnchorFrame(ctx context.Context, e evaluator, host *uitree.Node, m *Metrics) (*Frame, error) {
	if host.Bounds.Width() <= 0 || m == nil || m.CSSWidth <= 0 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "no WebView frame or page viewport to anchor")
	}
	scale := float64(host.Bounds.Width()) / m.CSSWidth

	native := map[string]uitree.Rect{}
	seen := map[string]int{}
	var walk func(n *uitree.Node)
	walk = func(n *uitree.Node) {
		for _, c := range n.Children {
			if c.Class == "XCUIElementTypeStaticText" && c.Displayed && !c.Bounds.Empty() {
				words := strings.TrimSpace(c.Label)
				if len([]rune(words)) >= 4 {
					seen[words]++
					native[words] = c.Bounds
				}
			}
			walk(c)
		}
	}
	walk(host)
	var words []string
	for w := range native {
		if seen[w] == 1 {
			words = append(words, w)
		}
	}
	sort.Strings(words)
	if len(words) > anchorsAsked {
		words = words[:anchorsAsked]
	}
	if len(words) < 2 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "too few words on the page to anchor it")
	}

	arg, _ := json.Marshal(words)
	raw, err := e.Evaluate(ctx, fmt.Sprintf(anchorScript, arg))
	if err != nil {
		return nil, err
	}
	var rects map[string]struct{ X, Y float64 }
	if err := json.Unmarshal([]byte(raw), &rects); err != nil {
		return nil, fmt.Errorf("parse page anchors: %w", err)
	}

	var xs, ys []int
	for w, r := range rects {
		n, ok := native[w]
		if !ok {
			continue
		}
		xs = append(xs, n.X1-int(r.X*scale))
		ys = append(ys, n.Y1-int(r.Y*scale))
	}
	x, xok := agreed(xs, scale)
	y, yok := agreed(ys, scale)
	if !xok || !yok {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the page's text and the WebView's did not agree on "+
			"where the page starts (%d anchors)", len(xs))
	}
	return &Frame{OriginX: x, OriginY: y, Scale: scale}, nil
}

// anchorsAsked bounds how many words are looked for in the page.
const anchorsAsked = 40

// agreed is the median of offsets when at least two lie within two CSS
// pixels of it — what rounding a frame to whole points can move it by.
func agreed(offsets []int, scale float64) (int, bool) {
	if len(offsets) < 2 {
		return 0, false
	}
	sorted := append([]int(nil), offsets...)
	sort.Ints(sorted)
	median := sorted[len(sorted)/2]
	tolerance := int(2*scale) + 1
	close := 0
	for _, o := range sorted {
		if o-median <= tolerance && median-o <= tolerance {
			close++
		}
	}
	return median, close >= 2
}

// anchorScript finds each of the given words as one text node in the page,
// and returns where that text is, in the visual viewport's CSS pixels.
// Words found more than once are left out: an anchor has to be one place.
const anchorScript = `(() => {
  const want = new Set(%s);
  const found = {}, count = {};
  const walker = document.createTreeWalker(document.body || document, NodeFilter.SHOW_TEXT);
  for (let t = walker.nextNode(); t; t = walker.nextNode()) {
    const w = t.textContent.replace(/\s+/g, ' ').trim();
    if (!want.has(w)) continue;
    count[w] = (count[w] || 0) + 1;
    const range = document.createRange();
    range.selectNodeContents(t);
    const r = range.getBoundingClientRect();
    if (r.width > 0 && r.height > 0) found[w] = r;
  }
  const vv = window.visualViewport || {offsetLeft: 0, offsetTop: 0};
  const out = {};
  for (const w in found) {
    if (count[w] === 1) out[w] = {X: found[w].left - vv.offsetLeft, Y: found[w].top - vv.offsetTop};
  }
  return JSON.stringify(out);
})()`
