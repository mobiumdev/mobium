package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// handyDriver is a fakeDriver that can also drag and double-tap, so the two
// halves of each feature can be tested apart: what the tool does when the
// backend can, and what it says when it cannot.
type handyDriver struct {
	fakeDriver
	doubleTapped []string
	drags        []string
}

func (h *handyDriver) DoubleTap(ctx context.Context, x, y int) error {
	h.doubleTapped = append(h.doubleTapped, fmt.Sprintf("%d,%d", x, y))
	return nil
}

func (h *handyDriver) Drag(ctx context.Context, x1, y1, x2, y2 int, hold, move time.Duration) error {
	h.drags = append(h.drags, fmt.Sprintf("%d,%d->%d,%d hold=%s move=%s",
		x1, y1, x2, y2, hold, move))
	return nil
}

// twoThings is a screen with two named elements far enough apart that a drag
// between them has somewhere to go.
func twoThings(t *testing.T) *uitree.Tree {
	t.Helper()
	xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		`<node index="0" text="Notes" resource-id="app:id/notes" class="android.widget.TextView" ` +
		`content-desc="" clickable="true" enabled="true" bounds="[100,200][300,280]" />` +
		`<node index="1" text="Work" resource-id="app:id/work" class="android.widget.TextView" ` +
		`content-desc="" clickable="true" enabled="true" bounds="[700,1600][900,1680]" />` +
		`</node></hierarchy>`
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return tree
}

// withHandy is withFake, backed by a driver that can do both gestures.
func withHandy(t *testing.T, screens ...*uitree.Tree) (*Handlers, *session, *handyDriver) {
	t.Helper()
	d := &handyDriver{fakeDriver: fakeDriver{screens: screens}}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	h.sessions["fake"] = s
	return h, s, d
}

func dragCall(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.dragOn(ctx, s, args)
}

// A drag resolves both ends from one snapshot and sends the centers. The
// point of asserting the centers rather than "a drag happened" is that a
// drag between the wrong two points is still a successful drag.
func TestDragCarriesOneElementOntoAnother(t *testing.T) {
	h, s, d := withHandy(t, twoThings(t))
	if _, err := dragCall(h, s, map[string]interface{}{
		"from": "text=Notes", "to": "text=Work",
	}); err != nil {
		t.Fatalf("drag: %v", err)
	}
	if len(d.drags) != 1 {
		t.Fatalf("sent %d drags, want 1", len(d.drags))
	}
	if !strings.HasPrefix(d.drags[0], "200,240->800,1640") {
		t.Errorf("dragged %q, want it to run between the two centers", d.drags[0])
	}
	if !strings.Contains(d.drags[0], "hold=700ms") {
		t.Errorf("dragged %q, want the default 700ms hold — below Android's "+
			"500ms long-press timeout nothing is picked up", d.drags[0])
	}
}

// Whatever was dragged is somewhere else now, and so is everything it
// displaced, so the refs from the last map are no longer answers.
func TestDragDiscardsTheOldRefs(t *testing.T) {
	h, s, _ := withHandy(t, twoThings(t))
	h.refs[s.dev.Serial] = &refTable{entries: map[string]uitree.Locator{"@e1": {}}}
	if _, err := dragCall(h, s, map[string]interface{}{
		"from": "text=Notes", "to": "text=Work",
	}); err != nil {
		t.Fatalf("drag: %v", err)
	}
	if _, ok := h.refs[s.dev.Serial]; ok {
		t.Error("the refs from the last map survived a drag")
	}
}

// Half a drag is not a drag, and the error has to say which half is missing
// rather than reporting a generic bad-argument.
func TestDragRefusesHalfAnInstruction(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"nothing at all", map[string]interface{}{}, "needs both from and to"},
		{"only a source", map[string]interface{}{"from": "text=Notes"}, "two ends"},
		{"only a destination", map[string]interface{}{"to": "text=Work"}, "two ends"},
		{"both spellings", map[string]interface{}{
			"from": "text=Notes", "to": "text=Work",
			"x1": 1, "y1": 2, "x2": 3, "y2": 4,
		}, "not both"},
		{"a hold of zero", map[string]interface{}{
			"from": "text=Notes", "to": "text=Work", "hold_ms": 0,
		}, "above zero"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, s, _ := withHandy(t, twoThings(t))
			_, err := dragCall(h, s, c.args)
			if err == nil {
				t.Fatalf("accepted %v", c.args)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("said %q, want it to mention %q", err, c.want)
			}
		})
	}
}

// Two locators that land on the same element would deliver a gesture that
// travels nowhere, which the platform reads as a long press. Refusing names
// the mistake; delivering it would report a successful drag that moved
// nothing.
func TestDragRefusesToDragSomethingOntoItself(t *testing.T) {
	h, s, d := withHandy(t, twoThings(t))
	_, err := dragCall(h, s, map[string]interface{}{
		"from": "text=Notes", "to": "testid=notes",
	})
	if err == nil {
		t.Fatal("dragged an element onto itself")
	}
	if !strings.Contains(err.Error(), "same place") {
		t.Errorf("said %q, want it to say the two ends resolved to one place", err)
	}
	if len(d.drags) != 0 {
		t.Errorf("sent the gesture anyway: %v", d.drags)
	}
}

// A backend that can swipe cannot necessarily drag, and the refusal has to
// say why rather than reporting the tool as broken — the holds are the
// difference, and `adb shell input swipe` has no argument for one.
func TestDragRefusalExplainsItself(t *testing.T) {
	h, s, _ := withFake(t, twoThings(t))
	_, err := dragCall(h, s, map[string]interface{}{"from": "text=Notes", "to": "text=Work"})
	if err == nil {
		t.Fatal("the dump backend claimed it could drag")
	}
	for _, want := range []string{"hold", "uiautomator2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("said %q, want it to mention %q", err, want)
		}
	}
}

// A double tap is app_tap with one argument set, so it must resolve its
// target exactly as a tap does — and reach the double-tap method rather than
// tapping twice through the ordinary one.
func TestDoubleTapGoesThroughTheDoubleTapPath(t *testing.T) {
	h, s, d := withHandy(t, twoThings(t))
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	res, err := h.tapOn(ctx, s, map[string]interface{}{"target": "text=Notes", "double": true})
	if err != nil {
		t.Fatalf("double tap: %v", err)
	}
	if len(d.doubleTapped) != 1 || d.doubleTapped[0] != "200,240" {
		t.Errorf("double-tapped %v, want one at 200,240", d.doubleTapped)
	}
	if len(d.tapped) != 0 {
		t.Errorf("also sent %d ordinary taps — two taps is not a double tap", len(d.tapped))
	}
	if !strings.Contains(textOf(res), "double-tapped") {
		t.Errorf("answered %q, want it to say which gesture was sent", textOf(res))
	}
}

// And without the argument it is still an ordinary tap, or the flag is not a
// flag — it is the only behavior there is.
func TestTapWithoutDoubleIsStillOneTap(t *testing.T) {
	h, s, d := withHandy(t, twoThings(t))
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	if _, err := h.tapOn(ctx, s, map[string]interface{}{"target": "text=Notes"}); err != nil {
		t.Fatalf("tap: %v", err)
	}
	if len(d.tapped) != 1 || len(d.doubleTapped) != 0 {
		t.Errorf("sent %d taps and %d double taps, want 1 and 0",
			len(d.tapped), len(d.doubleTapped))
	}
}

// The dump backend refuses, and the refusal names the interval rather than
// saying the backend is unsupported: the caller's next question is always
// "why not", and the answer is what tells them a different backend fixes it.
func TestDoubleTapRefusalNamesTheInterval(t *testing.T) {
	h, s, f := withFake(t, twoThings(t))
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	_, err := h.tapOn(ctx, s, map[string]interface{}{"target": "text=Notes", "double": true})
	if err == nil {
		t.Fatal("the dump backend claimed it could double-tap")
	}
	for _, want := range []string{"interval", "40 to 300ms", "uiautomator2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("said %q, want it to mention %q", err, want)
		}
	}
	if len(f.tapped) != 0 {
		t.Errorf("tapped anyway: %v — a refused gesture must send nothing", f.tapped)
	}
}
