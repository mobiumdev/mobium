package uitree

import (
	"os"
	"strings"
	"testing"
)

// Android's seek bar keeps its progress as its text (227); the one a WebView
// draws for a page's <input type="range"> keeps the range's label there and
// reports no progress, and was mapped as "SeekBar" valued by its label.
// MobiumApp's Slider Demo on the Android 15 emulator. CHALLENGES 319.
func TestAWebViewsSeekBarIsNamedByItsText(t *testing.T) {
	raw, err := os.ReadFile("testdata/mobiumapp-sliders-uia2.xml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := ParseAndroid(raw)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]Entry{}
	for _, e := range tree.Map() {
		if e.Role == "slider" {
			seen[e.Label] = e
		}
	}
	if e, ok := seen["Level"]; !ok || e.Node.Value != "5.0" || !e.Node.ValueSteps {
		t.Errorf("Level, a native seek bar: %+v", seen["Level"])
	}
	e, ok := seen["Range, 0 to 100 in steps of 10"]
	if !ok {
		t.Fatalf("the page's range is not a slider named by its label: %v", seen)
	}
	if e.Node.Value != "" || e.Node.ValueSteps {
		t.Errorf("the page's range has a value it was never given: %q", e.Node.Value)
	}
}

// WebView 155, on the Pixel 8 Pro, gives a page's range no text and puts its
// <label> in a node of its own just before it, where WebView 124 made the
// label its text: "Range, 0 to 100 in steps of 10" mapped as "r", its HTML
// id. The seek bar takes the label's text. The two-thumb price and the ARIA
// hours have no label node at all, and stay nameless here — the page names
// them. The app's native sliders keep their names and steps. CHALLENGES 347.
func TestAWebSliderIsNamedByTheLabelBeforeIt(t *testing.T) {
	tree := loadTree(t, "mobiumapp-slider-webview155-uia2.xml")
	var web []*Node
	byLabel := map[string]*Node{}
	tree.Walk(func(n *Node) bool {
		if HasClassRole(n, "slider") {
			if insideWebView(n) {
				web = append(web, n)
			} else {
				byLabel[n.Label] = n
			}
		}
		return true
	})
	if len(web) != 5 {
		t.Fatalf("%d web sliders, want the range, two price thumbs and two hours", len(web))
	}
	if web[0].Text != "Range, 0 to 100 in steps of 10" || web[0].TestID != "r" {
		t.Errorf("the range is %q (%s), want it named by its label", web[0].Text, web[0].TestID)
	}
	for _, n := range web[1:] {
		if n.Text != "" {
			t.Errorf("%s at %s was named %q from a node that is not its label", n.TestID, n.Bounds, n.Text)
		}
	}
	if v := byLabel["Volume"]; v == nil || v.Value != "5.0" || v.Text != "" {
		t.Errorf("the native Volume slider changed: %+v", v)
	}
	lines := map[string]bool{}
	for _, e := range tree.Map() {
		lines[strings.SplitN(e.Line(), " ", 2)[1]] = true
	}
	if !lines["Range, 0 to 100 in steps of 10 (slider)"] {
		t.Errorf("map has no named range: %v", lines)
	}
}
