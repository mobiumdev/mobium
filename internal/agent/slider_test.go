package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// sliderScreen is an iOS screen holding one slider that reads value.
func sliderScreen(t *testing.T, value string) *uitree.Tree {
	t.Helper()
	xml := `<?xml version="1.0" encoding="UTF-8"?><XCUIElementTypeApplication type="XCUIElementTypeApplication" ` +
		`name="App" label="App" enabled="true" visible="true" accessible="false" x="0" y="0" width="402" height="874">` +
		`<XCUIElementTypeWindow type="XCUIElementTypeWindow" enabled="true" visible="true" accessible="false" x="0" y="0" width="402" height="874">` +
		`<XCUIElementTypeSlider type="XCUIElementTypeSlider" value="` + value + `" enabled="true" visible="true" ` +
		`accessible="true" x="32" y="603" width="338" height="31" traits="Adjustable"/>` +
		`</XCUIElementTypeWindow></XCUIElementTypeApplication>`
	tree, err := uitree.ParseIOS([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// sliderDriver is a driver whose one slider reads before until it is
// moved, and after once it has been.
type sliderDriver struct {
	recordingTyper
	before, after *uitree.Tree
	moved         []float64
}

func (d *sliderDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	if len(d.moved) > 0 {
		return d.after, nil
	}
	return d.before, nil
}

func (d *sliderDriver) SetSliderPosition(ctx context.Context, n *uitree.Node, position float64) error {
	d.moved = append(d.moved, position)
	return nil
}

// app_fill on a slider moves it to a position and reports what the app then
// reads, never typing into it; app_type is refused with fill named; and a
// position that is not a number from 0 to 1 is refused before anything
// moves — WebDriverAgent took "abc" as 0 and moved the slider to the start
// (CHALLENGES 219).
func TestFillMovesASliderAndReadsItBack(t *testing.T) {
	run := func(text string, replace bool) (*sliderDriver, *ToolsCallResult, error) {
		d := &sliderDriver{before: sliderScreen(t, "100%"), after: sliderScreen(t, "70%")}
		h := NewHandlers()
		h.implicitWait, h.settleWindow = 0, 0
		s := &session{dev: fakeDevice(), driver: d, backend: BackendWDA}
		res, err := h.typeTextOn(context.Background(), s, map[string]interface{}{"target": "role=slider", "text": text}, replace)
		return d, res, err
	}

	d, res, err := run("0.25", true)
	if err != nil || len(d.moved) != 1 || d.moved[0] != 0.25 || len(d.set) != 0 {
		t.Fatalf("fill 0.25 moved %v and typed %q (%v)", d.moved, d.set, err)
	}
	if text := res.Content[0].Text; !strings.Contains(text, `reads "70%"`) || !strings.Contains(text, `was "100%"`) {
		t.Errorf("the result does not say what the slider reads now and before: %q", text)
	}
	for _, bad := range []string{"abc", "1.5", "-0.1", "NaN", ""} {
		d, _, err := run(bad, true)
		if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument || len(d.moved) != 0 {
			t.Errorf("fill %q: moved %v (%v), want refused before moving", bad, d.moved, err)
		}
	}
	d, _, err = run("0.5", false)
	if err == nil || !strings.Contains(err.Error(), "app_fill") || len(d.moved)+len(d.set) != 0 {
		t.Errorf("type on a slider: %v, moved %v, typed %q — want refused, naming app_fill", err, d.moved, d.set)
	}
}

// A driver that cannot move a slider says so, rather than typing the
// position into it.
func TestFillOnASliderWithoutSupportIsRefused(t *testing.T) {
	d := &recordingTyper{fakeDriver: fakeDriver{screens: []*uitree.Tree{sliderScreen(t, "100%")}}}
	h := NewHandlers()
	h.implicitWait, h.settleWindow = 0, 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	_, err := h.typeTextOn(context.Background(), s, map[string]interface{}{"target": "role=slider", "text": "0.5"}, true)
	if mobiumerr.CodeOf(err) != mobiumerr.Unsupported || len(d.set) != 0 {
		t.Errorf("fill on a slider with no slider support: %v, typed %q", err, d.set)
	}
}

// seekBarScreen is an Android screen holding one seek bar whose progress, in
// its own steps, is progress.
func seekBarScreen(t *testing.T, progress string) *uitree.Tree {
	t.Helper()
	xml := `<?xml version="1.0" encoding="UTF-8"?><hierarchy rotation="0">` +
		`<android.widget.FrameLayout class="android.widget.FrameLayout" package="dev.mobium.mobiumapp" bounds="[0,0][1080,2400]" displayed="true" enabled="true">` +
		`<android.widget.SeekBar class="android.widget.SeekBar" package="dev.mobium.mobiumapp" text="` + progress + `" content-desc="Volume" ` +
		`resource-id="volumeSlider" focusable="true" enabled="true" displayed="true" bounds="[36,542][972,583]"/>` +
		`</android.widget.FrameLayout></hierarchy>`
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// An Android seek bar's reading is said to be in its own steps, beside the
// position it was moved to, in the structured answer as well as the text: an
// agent told only `"value":"2.0"` after filling 0.2 — volume 20 — decided
// the slider ran to 10 and gave up (CHALLENGES 302). iOS's reading is the
// app's own and is left bare.
func TestFillSaysWhatASeekBarsReadingIsIn(t *testing.T) {
	fill := func(d *sliderDriver, backend Backend) (*ToolsCallResult, error) {
		h := NewHandlers()
		h.implicitWait, h.settleWindow = 0, 0
		s := &session{dev: fakeDevice(), driver: d, backend: backend}
		return h.typeTextOn(context.Background(), s, map[string]interface{}{"target": "role=slider", "text": "0.2"}, true)
	}
	res, err := fill(&sliderDriver{before: seekBarScreen(t, "5.0"), after: seekBarScreen(t, "2.0")}, BackendUIA2)
	if err != nil {
		t.Fatal(err)
	}
	v := res.StructuredContent.(ActionView)
	if v.Position == nil || *v.Position != 0.2 || v.Value != "2.0" || !strings.Contains(v.ValueScale, "own steps") {
		t.Errorf("the structured answer does not say the reading is in the seek bar's own steps: %+v", v)
	}
	if !strings.Contains(res.Content[0].Text, "own steps") {
		t.Errorf("the text does not say so either: %q", res.Content[0].Text)
	}

	res, err = fill(&sliderDriver{before: sliderScreen(t, "100%"), after: sliderScreen(t, "20%")}, BackendWDA)
	if err != nil {
		t.Fatal(err)
	}
	if v := res.StructuredContent.(ActionView); v.ValueScale != "" || v.Position == nil {
		t.Errorf("an iOS slider's own reading is marked, or its position missing: %+v", v)
	}
}
