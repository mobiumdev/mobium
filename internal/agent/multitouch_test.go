package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// toucherDriver is a fakeDriver that can put several fingers down, recording
// where each gesture's fingers went.
type toucherDriver struct {
	fakeDriver
	multiTaps  [][]mobiumdriver.Point
	pressTaps  []string
	pressDrags []string
}

func (d *toucherDriver) MultiTap(ctx context.Context, fingers []mobiumdriver.Point) error {
	d.multiTaps = append(d.multiTaps, fingers)
	return nil
}

func (d *toucherDriver) PressTap(ctx context.Context, hold, tap mobiumdriver.Point, lead time.Duration) error {
	d.pressTaps = append(d.pressTaps, fmt.Sprintf("%v+%v lead=%s", hold, tap, lead))
	return nil
}

func (d *toucherDriver) PressDrag(ctx context.Context, hold, from, to mobiumdriver.Point, lead, move time.Duration) error {
	d.pressDrags = append(d.pressDrags, fmt.Sprintf("%v+%v->%v lead=%s move=%s", hold, from, to, lead, move))
	return nil
}

func withToucher(t *testing.T, screens ...*uitree.Tree) (*Handlers, *session, *toucherDriver) {
	t.Helper()
	d := &toucherDriver{fakeDriver: fakeDriver{screens: screens}}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	h.sessions["fake"] = s
	return h, s, d
}

func ctxFor(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	t.Cleanup(cancel)
	return ctx
}

// The fingers land where the elements are, and the first finger is the held
// one. Asserting the points rather than "a gesture happened" because a press
// and tap on the wrong two elements is still a successful gesture.
func TestPressTapHoldsOneAndTapsTheOther(t *testing.T) {
	h, s, d := withToucher(t, twoThings(t))
	if _, err := h.pressTapOn(ctxFor(t), s, map[string]interface{}{
		"hold": "text=Notes", "tap": "text=Work",
	}); err != nil {
		t.Fatalf("press-tap: %v", err)
	}
	if len(d.pressTaps) != 1 || d.pressTaps[0] != "{200 240}+{800 1640} lead=300ms" {
		t.Errorf("sent %v, want the held center, then the tapped center, with the 300ms lead", d.pressTaps)
	}
}

func TestPressDragHoldsOneAndDragsAnother(t *testing.T) {
	h, s, d := withToucher(t, twoThings(t))
	if _, err := h.pressDragOn(ctxFor(t), s, map[string]interface{}{
		"x1": 100, "y1": 100, "x2": 500, "y2": 900, "x3": 500, "y3": 300, "duration_ms": 400,
	}); err != nil {
		t.Fatalf("press-drag: %v", err)
	}
	if len(d.pressDrags) != 1 || d.pressDrags[0] != "{100 100}+{500 900}->{500 300} lead=300ms move=400ms" {
		t.Errorf("sent %v", d.pressDrags)
	}
}

// A two-finger tap lands both fingers side by side about the target, a
// tenth of the screen apart, on the same row.
func TestTapWithFingersLandsThemSideBySide(t *testing.T) {
	h, s, d := withToucher(t, twoThings(t))
	if _, err := h.tapOn(ctxFor(t), s, map[string]interface{}{"target": "text=Work", "fingers": 3}); err != nil {
		t.Fatalf("three-finger tap: %v", err)
	}
	if len(d.multiTaps) != 1 {
		t.Fatalf("sent %d multi-taps, want 1", len(d.multiTaps))
	}
	want := []mobiumdriver.Point{{X: 692, Y: 1640}, {X: 800, Y: 1640}, {X: 908, Y: 1640}}
	if fmt.Sprint(d.multiTaps[0]) != fmt.Sprint(want) {
		t.Errorf("fingers at %v, want %v", d.multiTaps[0], want)
	}
	if len(d.tapped) != 0 {
		t.Errorf("also sent %d one-finger taps", len(d.tapped))
	}
}

// Fingers past the edge are not touches, so they are kept on the screen.
func TestFingerRowStaysOnScreen(t *testing.T) {
	for _, p := range fingerRow(10, 50, 5, 1080) {
		if p.X < 1 || p.X > 1078 {
			t.Errorf("a finger at x=%d is off a 1080-wide screen", p.X)
		}
	}
}

// Everything that is not the gesture is refused before anything touches the
// screen, each with a reason that names what to change.
func TestMultiTouchRefusesBadInstructions(t *testing.T) {
	cases := []struct {
		name string
		call func(h *Handlers, s *session) error
		want string
	}{
		{"press-tap with one end", func(h *Handlers, s *session) error {
			_, err := h.pressTapOn(ctxFor(t), s, map[string]interface{}{"hold": "text=Notes"})
			return err
		}, "missing [tap]"},
		{"press-tap mixing forms", func(h *Handlers, s *session) error {
			_, err := h.pressTapOn(ctxFor(t), s, map[string]interface{}{"hold": "text=Notes", "x2": 1, "y2": 2})
			return err
		}, "not both"},
		{"press-drag with half its points", func(h *Handlers, s *session) error {
			_, err := h.pressDragOn(ctxFor(t), s, map[string]interface{}{"x1": 1, "y1": 1, "x2": 2, "y2": 2})
			return err
		}, "a point for each"},
		{"press-drag that goes nowhere", func(h *Handlers, s *session) error {
			_, err := h.pressDragOn(ctxFor(t), s, map[string]interface{}{"hold": "text=Notes", "from": "text=Work", "to": "text=Work"})
			return err
		}, "app_press_tap"},
		{"a zero lead", func(h *Handlers, s *session) error {
			_, err := h.pressTapOn(ctxFor(t), s, map[string]interface{}{"hold": "text=Notes", "tap": "text=Work", "lead_ms": 0})
			return err
		}, "above zero"},
		{"six fingers", func(h *Handlers, s *session) error {
			_, err := h.tapOn(ctxFor(t), s, map[string]interface{}{"target": "text=Work", "fingers": 6})
			return err
		}, "1 to 5"},
		{"a multi-finger double tap", func(h *Handlers, s *session) error {
			_, err := h.tapOn(ctxFor(t), s, map[string]interface{}{"target": "text=Work", "fingers": 2, "double": true})
			return err
		}, "not both"},
	}
	for _, c := range cases {
		h, s, d := withToucher(t, twoThings(t))
		err := c.call(h, s)
		if err == nil {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
			t.Errorf("%s: code %s, want invalid_argument", c.name, mobiumerr.CodeOf(err))
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: said %q, want it to mention %q", c.name, err, c.want)
		}
		if len(d.pressTaps)+len(d.pressDrags)+len(d.multiTaps)+len(d.tapped) != 0 {
			t.Errorf("%s: touched the screen anyway", c.name)
		}
	}
}

// A backend with one finger says so, and sends nothing.
func TestMultiTouchRefusalOnAOneFingerBackend(t *testing.T) {
	h, s, f := withFake(t, twoThings(t))
	for name, call := range map[string]func() error{
		"press-tap": func() error {
			_, err := h.pressTapOn(ctxFor(t), s, map[string]interface{}{"hold": "text=Notes", "tap": "text=Work"})
			return err
		},
		"two-finger tap": func() error {
			_, err := h.tapOn(ctxFor(t), s, map[string]interface{}{"target": "text=Work", "fingers": 2})
			return err
		},
	} {
		err := call()
		if mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
			t.Errorf("%s on a one-finger backend: %v, want unsupported", name, err)
		}
	}
	if len(f.tapped) != 0 {
		t.Errorf("tapped anyway: %v", f.tapped)
	}
}
