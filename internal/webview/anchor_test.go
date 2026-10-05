package webview

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

type anchorEval struct{ out string }

func (f anchorEval) Evaluate(ctx context.Context, expression string) (string, error) {
	return f.out, nil
}

// host is a WebView 402 points wide at 3x, running the full height of the
// screen as Kiwix's does, holding static texts at their screen frames.
func anchorHost(texts map[string]uitree.Rect) *uitree.Node {
	host := &uitree.Node{Class: "XCUIElementTypeWebView", Bounds: uitree.Rect{X1: 0, Y1: 0, X2: 1206, Y2: 2622}, Displayed: true}
	for label, r := range texts {
		host.Children = append(host.Children, &uitree.Node{Class: "XCUIElementTypeStaticText", Label: label,
			Bounds: r, Displayed: true, Parent: host})
	}
	return host
}

// Where a WebView is taller than its page, two runs of text both trees can
// see give where the page starts: Kiwix's page begins 116 points down, under
// its navigation bar, and the anchors say 348 device pixels. CHALLENGES 248.
func TestAnchorFrameFindsWhereThePageStarts(t *testing.T) {
	host := anchorHost(map[string]uitree.Rect{
		"Hank Crawford":      {X1: 636, Y1: 1281, X2: 900, Y2: 1320},
		"Georgia on My Mind": {X1: 636, Y1: 1170, X2: 960, Y2: 1209},
	})
	page := anchorEval{`{"Hank Crawford":{"X":212,"Y":311},"Georgia on My Mind":{"X":212,"Y":274}}`}
	f, err := AnchorFrame(context.Background(), page, host, &Metrics{CSSWidth: 402, CSSHeight: 758})
	if err != nil {
		t.Fatal(err)
	}
	if f.OriginX != 0 || f.OriginY != 348 || f.Scale != 3 {
		t.Errorf("frame = %+v, want origin (0, 348) at 3x", f)
	}
}

// Anchors that disagree are no answer: the refusal stands.
func TestAnchorFrameRefusesAnchorsThatDisagree(t *testing.T) {
	host := anchorHost(map[string]uitree.Rect{
		"Hank Crawford":      {X1: 636, Y1: 1281, X2: 900, Y2: 1320},
		"Georgia on My Mind": {X1: 636, Y1: 1170, X2: 960, Y2: 1209},
	})
	page := anchorEval{`{"Hank Crawford":{"X":212,"Y":311},"Georgia on My Mind":{"X":212,"Y":100}}`}
	if _, err := AnchorFrame(context.Background(), page, host, &Metrics{CSSWidth: 402, CSSHeight: 758}); err == nil ||
		!strings.Contains(err.Error(), "did not agree") {
		t.Errorf("disagreeing anchors were accepted: %v", err)
	}
	one := anchorHost(map[string]uitree.Rect{"Hank Crawford": {X1: 636, Y1: 1281, X2: 900, Y2: 1320}})
	if _, err := AnchorFrame(context.Background(), page, one, &Metrics{CSSWidth: 402, CSSHeight: 758}); err == nil {
		t.Error("a single run of text was taken as enough to anchor the page")
	}
}
