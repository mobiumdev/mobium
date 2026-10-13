package uitree

import (
	"os"
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
