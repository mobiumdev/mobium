package agent

import (
	"context"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
	"github.com/mobiumdev/mobium/internal/webview"
)

// hostDriver is a screen with one WebView filling 1080x2000 device pixels.
type hostDriver struct{ fakeDriver }

func (d *hostDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	web := &uitree.Node{Class: "android.webkit.WebView", Displayed: true, Enabled: true,
		Bounds: uitree.Rect{X1: 0, Y1: 0, X2: 1080, Y2: 2000}}
	root := &uitree.Node{Class: "hierarchy", Displayed: true, Children: []*uitree.Node{web}}
	web.Parent = root
	return &uitree.Tree{Root: root}, nil
}

// layoutPage is a page laid out at 540x1000 CSS pixels — half the device's
// scale — with the elements given.
type layoutPage struct {
	webview.Page
	els []webview.Element
}

func (p *layoutPage) LayoutMetrics(ctx context.Context) (*webview.Metrics, error) {
	return &webview.Metrics{CSSWidth: 540, CSSHeight: 1000}, nil
}
func (p *layoutPage) Map(ctx context.Context) ([]webview.Element, error) { return p.els, nil }

func elementAt(t *testing.T, els []webview.Element, want uitree.Rect) (webview.Element, error) {
	t.Helper()
	h := NewHandlers()
	s := &session{dev: fakeDevice(), driver: &hostDriver{}, backend: BackendUIA2,
		web: &layoutPage{els: els}, webCtx: "WEBVIEW_test"}
	return h.pageElementAt(context.Background(), s, want)
}

// The native node's rectangle finds the page element drawn there, in the
// page's own units scaled to the device's (CHALLENGES 301).
func TestPageElementAtFindsTheElementUnderANativeNode(t *testing.T) {
	els := []webview.Element{
		{Label: "text input", X: 10, Y: 20, W: 200, H: 20},
		{Label: "fixed", X: 10, Y: 60, W: 200, H: 20},
	}
	got, err := elementAt(t, els, uitree.Rect{X1: 20, Y1: 120, X2: 420, Y2: 160})
	if err != nil || got.Label != "fixed" {
		t.Fatalf("got %+v, %v; want the read-only field", got, err)
	}
	if _, err := elementAt(t, els, uitree.Rect{X1: 600, Y1: 1500, X2: 900, Y2: 1600}); err == nil {
		t.Error("matched an element where the page has none")
	}
}

// Two elements in the same place — a label stacked on its input — are not
// told apart by position, and an ambiguous match is worse than none.
func TestPageElementAtRefusesTwoInOnePlace(t *testing.T) {
	els := []webview.Element{
		{Label: "a", X: 10, Y: 60, W: 200, H: 20},
		{Label: "b", X: 10, Y: 60, W: 200, H: 20},
	}
	if got, err := elementAt(t, els, uitree.Rect{X1: 20, Y1: 120, X2: 420, Y2: 160}); err == nil {
		t.Errorf("chose %q between two elements in one place", got.Label)
	}
}

func TestOverlap(t *testing.T) {
	a := uitree.Rect{X1: 0, Y1: 0, X2: 10, Y2: 10}
	if overlap(a, a) != 1 {
		t.Error("a rectangle does not overlap itself fully")
	}
	if overlap(a, uitree.Rect{X1: 20, Y1: 20, X2: 30, Y2: 30}) != 0 {
		t.Error("apart rectangles overlap")
	}
	if o := overlap(a, uitree.Rect{X1: 0, Y1: 0, X2: 10, Y2: 5}); o != 0.5 {
		t.Errorf("half of a rectangle overlaps it by %v, want 0.5", o)
	}
}
