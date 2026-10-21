package agent

import (
	"context"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// touch taps a point as the device's user would: a plain tap, or, with a
// screen reader on, a touch that moves its cursor there and a double tap
// that activates what the cursor landed on. With VoiceOver on, a plain tap
// only moves the cursor, and was reported done while the app received
// nothing (CHALLENGES 348).
//
// name is what the target is called, when the caller knows: the cursor is
// read back before anything is activated, and a cursor that landed on
// something not called that is refused, since its double tap would activate
// whatever it is on. An empty name skips the comparison — a point, or a
// page's element, whose outcome the caller reads back itself.
func (h *Handlers) touch(ctx context.Context, s *session, x, y int, name string) error {
	sr, ok := mobiumdriver.AsScreenReader(s.driver)
	if !ok {
		return s.driver.Tap(ctx, x, y)
	}
	if on, _ := sr.ScreenReaderOn(ctx); !on {
		return s.driver.Tap(mobiumdriver.ScreenReaderAsked(ctx), x, y)
	}
	spoken, err := sr.FocusAt(ctx, x, y)
	if err != nil {
		return err
	}
	if name != "" && !speaks(spoken, name) {
		return mobiumerr.New(mobiumerr.ElementNotReachable, "VoiceOver is on, and a touch on %q at (%d, %d) put its "+
			"cursor on %q instead, so nothing was activated", name, x, y, spoken).
			WithRemedy("app_map again: the screen may have moved, or something drawn over the target takes the "+
				"touch — act on that first").
			WithDetail("spoken", spoken)
	}
	return sr.ActivateFocused(ctx, x, y)
}

// speaks reports whether what a screen reader said names the target: it
// speaks the label, then the role and state ("Pro radio button,
// unchecked"), so the label is a part of it. Compared without case, spacing
// or the kind of apostrophe, which iOS writes curly.
func speaks(spoken, name string) bool {
	norm := func(s string) string {
		s = strings.NewReplacer("’", "'", "‘", "'", "“", "\"", "”", "\"").Replace(s)
		return strings.Join(strings.Fields(strings.ToLower(s)), " ")
	}
	return strings.Contains(norm(spoken), norm(name))
}

// accessibleName is what a screen reader calls a node: its accessibility
// label, else its text. Never a password field's value.
func accessibleName(n *uitree.Node) string {
	if n == nil {
		return ""
	}
	if l := strings.TrimSpace(n.Label); l != "" {
		return l
	}
	if n.Password {
		return ""
	}
	return strings.TrimSpace(n.Text)
}
