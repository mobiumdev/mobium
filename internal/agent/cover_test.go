package agent

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// obstructionScreen is MobiumApp's Obstruction Demo as UiAutomator2 reported
// it on a Pixel 7 AVD, optionally with one node taken out — the toast gone.
func obstructionScreen(t *testing.T, without string) *uitree.Tree {
	t.Helper()
	data, err := os.ReadFile("../uitree/testdata/obstruction-uia2.xml")
	if err != nil {
		t.Fatal(err)
	}
	if without != "" {
		// UiAutomator2 names each element after its class; the cover's own
		// closing tag ends it, since a cover's children are a different class.
		re := regexp.MustCompile(`(?s)<(android\.widget\.Button) [^>]*resource-id="` + without + `".*?</android\.widget\.Button>`)
		if !re.Match(data) {
			t.Fatalf("%s is not in the fixture", without)
		}
		data = re.ReplaceAll(data, nil)
	}
	tree, err := uitree.ParseAndroid(data)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func coverOf(t *testing.T, res *ToolsCallResult) *CoverView {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var v ActionView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v.Cover
}

func tapPoint(t *testing.T, s string) (int, int) {
	t.Helper()
	parts := strings.Split(s, ",")
	x, _ := strconv.Atoi(parts[0])
	y, _ := strconv.Atoi(parts[1])
	return x, y
}

// A control over all of the target is refused, not tapped: on the device
// the tap reported success and pressed the cover (CHALLENGES 115). The
// refusal names the cover, and its code says found and not touchable.
func TestATapUnderAControlIsRefused(t *testing.T) {
	h, s, f := withFake(t, obstructionScreen(t, ""))
	_, err := tap(h, s, map[string]interface{}{"target": "testid=fullTarget"})
	if err == nil || len(f.tapped) != 0 {
		t.Fatalf("a covered button was tapped (%v): %v", f.tapped, err)
	}
	if mobiumerr.CodeOf(err) != mobiumerr.ElementNotReachable {
		t.Errorf("code %s, want element_not_reachable", mobiumerr.CodeOf(err))
	}
	if !strings.Contains(err.Error(), `"full cover"`) {
		t.Errorf("the refusal does not name the cover: %v", err)
	}
	var me *mobiumerr.Error
	if !errorsAs(err, &me) || me.Details["check"] != "receivesEvents" {
		t.Errorf("details do not say which check failed: %v", err)
	}
}

// A cover that goes away within the wait is waited out, as a toast does.
func TestATapWaitsForACoverToGo(t *testing.T) {
	covered, clear := obstructionScreen(t, ""), obstructionScreen(t, "fullCover")
	h, s, f := withFake(t, covered, covered, clear)
	h.implicitWait = 2 * time.Second
	if _, err := tap(h, s, map[string]interface{}{"target": "testid=fullTarget"}); err != nil {
		t.Fatalf("the cover went and the tap was still refused: %v", err)
	}
	if len(f.tapped) != 1 {
		t.Errorf("tapped %d times, want once", len(f.tapped))
	}
}

// A control over the center only is aimed around, and the result says so.
func TestATapUnderAPartialCoverIsAimedAtAClearPoint(t *testing.T) {
	screen := obstructionScreen(t, "")
	h, s, f := withFake(t, screen)
	res, err := tap(h, s, map[string]interface{}{"target": "testid=halfTarget"})
	if err != nil || len(f.tapped) != 1 {
		t.Fatalf("tapped %v: %v", f.tapped, err)
	}
	x, y := tapPoint(t, f.tapped[0])
	var cover *uitree.Node
	screen.Walk(func(n *uitree.Node) bool {
		if n.TestID == "halfCover" {
			cover = n
		}
		return cover == nil
	})
	if cover == nil {
		t.Fatal("no halfCover in the fixture")
	}
	if b := cover.Bounds; x >= b.X1 && x < b.X2 && y >= b.Y1 && y < b.Y2 {
		t.Errorf("tapped (%d,%d), inside the cover %s", x, y, b)
	}
	if c := coverOf(t, res); c == nil || !c.Control || c.Label != "half cover" {
		t.Errorf("structured cover = %+v, want the half cover as a control", c)
	}
	if !strings.Contains(res.Content[0].Text, "clear point") {
		t.Errorf("the result does not say the aim moved: %q", res.Content[0].Text)
	}
}

// Something over the point that is not a control is tapped through and
// reported: the pass-through cover lets the tap reach the target on the
// device, and the tree cannot tell it from one that swallows it.
func TestATapUnderANonControlIsTappedAndReported(t *testing.T) {
	h, s, f := withFake(t, obstructionScreen(t, ""))
	res, err := tap(h, s, map[string]interface{}{"target": "testid=passTarget"})
	if err != nil || len(f.tapped) != 1 {
		t.Fatalf("the pass-through case was not tapped (%v): %v", f.tapped, err)
	}
	if c := coverOf(t, res); c == nil || c.Control || c.Label != "pass-through cover" {
		t.Errorf("structured cover = %+v, want the pass-through cover, not a control", c)
	}
	if !strings.Contains(res.Content[0].Text, "may take the touch") {
		t.Errorf("the result does not report what is over the point: %q", res.Content[0].Text)
	}
}

// Nothing over the point, nothing said.
func TestAnUncoveredTapSaysNothingAboutCovers(t *testing.T) {
	h, s, _ := withFake(t, obstructionScreen(t, ""))
	res, err := tap(h, s, map[string]interface{}{"target": "testid=edgeTarget"})
	if err != nil {
		t.Fatal(err)
	}
	if c := coverOf(t, res); c != nil {
		t.Errorf("structured cover = %+v on a target whose center is clear", c)
	}
}

func errorsAs(err error, target **mobiumerr.Error) bool {
	for e := err; e != nil; {
		if me, ok := e.(*mobiumerr.Error); ok {
			*target = me
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// A moved aim says what it moved from, and says it plainly when that was
// not a control: no claim that it is one, and no warning that it may take
// the touch, since the point touched has nothing over it. CHALLENGES 238.
func TestAMovedAimNamesWhatItAvoided(t *testing.T) {
	divider := &uitree.Node{Class: "XCUIElementTypeOther"}
	note, view := aimNote(uitree.Aim{Moved: true, CenterCover: divider})
	if !strings.Contains(note, "touched where nothing is") || strings.Contains(note, "may take") || view.Control {
		t.Errorf("note %q, view %+v", note, view)
	}
	button := &uitree.Node{Class: "XCUIElementTypeButton", Label: "Close"}
	if note, view := aimNote(uitree.Aim{Moved: true, CenterCover: button}); !strings.Contains(note, "covered by") || !view.Control {
		t.Errorf("a control over the center: note %q, view %+v", note, view)
	}
}
