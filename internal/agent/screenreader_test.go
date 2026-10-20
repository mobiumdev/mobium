package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// readerDriver is a scripted screen with a screen reader that may be on,
// and that says spoken wherever it is touched.
type readerDriver struct {
	*fakeDriver
	on        bool
	spoken    string
	focused   []string
	activated []string
}

func (r *readerDriver) ScreenReaderOn(context.Context) (bool, bool) { return r.on, true }
func (r *readerDriver) FocusAt(_ context.Context, x, y int) (string, error) {
	r.focused = append(r.focused, fmt.Sprintf("%d,%d", x, y))
	return r.spoken, nil
}
func (r *readerDriver) ActivateFocused(_ context.Context, x, y int) error {
	r.activated = append(r.activated, fmt.Sprintf("%d,%d", x, y))
	return nil
}

func withReader(t *testing.T, on bool, spoken string) (*Handlers, *session, *readerDriver) {
	t.Helper()
	h, s, f := withFake(t, twoThings(t))
	r := &readerDriver{fakeDriver: f, on: on, spoken: spoken}
	s.driver = r
	return h, s, r
}

// With VoiceOver on, a touch only moves its cursor: a tap reported done
// while MobiumApp's Pro radio stayed unchecked, on the iPhone and on an iOS
// 27 simulator. So the tap is a touch, the cursor read back, and a double
// tap — never an ordinary tap, which would be the defect again.
func TestATapWithAScreenReaderOnIsATouchAndADoubleTap(t *testing.T) {
	h, s, r := withReader(t, true, "Notes Button")
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if _, err := h.tapOn(ctx, s, map[string]interface{}{"target": "text=Notes"}); err != nil {
		t.Fatalf("tap: %v", err)
	}
	if len(r.focused) != 1 || len(r.activated) != 1 || r.activated[0] != r.focused[0] {
		t.Errorf("focused %v and activated %v, want one of each at the same point", r.focused, r.activated)
	}
	if len(r.tapped) != 0 {
		t.Errorf("also sent %d ordinary taps, which VoiceOver takes as a move of its cursor", len(r.tapped))
	}
}

// A cursor that landed on something else is refused before the double tap,
// which would activate whatever the cursor is on.
func TestACursorOnSomethingElseIsRefusedBeforeAnythingIsActivated(t *testing.T) {
	h, s, r := withReader(t, true, "Work Button")
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	_, err := h.tapOn(ctx, s, map[string]interface{}{"target": "text=Notes"})
	if mobiumerr.CodeOf(err) != mobiumerr.ElementNotReachable || !strings.Contains(err.Error(), `"Work Button"`) {
		t.Fatalf("tap with the cursor on Work answered %v, want element_not_reachable naming where it went", err)
	}
	if len(r.activated) != 0 {
		t.Errorf("activated %v after the cursor went to the wrong element", r.activated)
	}
}

// With it off — the negative control — a tap is an ordinary tap.
func TestATapWithTheScreenReaderOffIsAnOrdinaryTap(t *testing.T) {
	h, s, r := withReader(t, false, "")
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if _, err := h.tapOn(ctx, s, map[string]interface{}{"target": "text=Notes"}); err != nil {
		t.Fatalf("tap: %v", err)
	}
	if len(r.tapped) != 1 || len(r.focused) != 0 || len(r.activated) != 0 {
		t.Errorf("sent %d taps, %d touches and %d double taps, want 1, 0 and 0",
			len(r.tapped), len(r.focused), len(r.activated))
	}
}

func TestWhatIsSpokenNamesTheTarget(t *testing.T) {
	for _, c := range []struct {
		spoken, name string
		want         bool
	}{
		{"Pro radio button, unchecked", "Pro", true},
		{"Dark mode Switch button off Double tap to toggle setting.", "Dark mode", true},
		{"Don’t Allow Button", "Don't Allow", true},
		{"username  username Text field", "Username", true},
		{"Accept terms checkbox, unchecked", "Email me", false},
		{"", "Pro", false},
	} {
		if got := speaks(c.spoken, c.name); got != c.want {
			t.Errorf("speaks(%q, %q) = %v, want %v", c.spoken, c.name, got, c.want)
		}
	}
}
