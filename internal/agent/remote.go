package agent

import (
	"context"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// remoteOnly reports whether the session's device is driven with its remote
// alone: an Apple TV, which has no touch screen, so nothing may be tapped.
func remoteOnly(s *session) bool { return mobiumdriver.RemoteOnly(s.driver) }

// maxRemoteSteps bounds the D-pad presses that bring focus to a target. A
// tvOS alert's buttons are a row or a column of a few; a target this far
// away is not where focus can be walked to blindly.
const maxRemoteSteps = 8

// selectByRemote presses target as a person with the remote does: the
// D-pad toward it, read back press by press, until it has focus, then
// select. A dialog rule on an Apple TV tapped its button, and a TV refuses
// every tap, so the "Open in …?" prompt a link raises could not be answered
// by a rule (CHALLENGES 334). Each press goes the way target lies from what
// has focus, along the axis it is further out on; focus that does not move
// is refused rather than pressed again.
func (h *Handlers) selectByRemote(ctx context.Context, s *session, target *uitree.Node) error {
	ctrl, ok := mobiumdriver.AsButtons(s.driver)
	if !ok {
		return cannot(s, mobiumdriver.CapButtons, "press the remote's buttons")
	}
	want := uitree.Describe(target)
	for step := 0; ; step++ {
		tree, err := s.driver.Snapshot(ctx)
		if err != nil {
			return err
		}
		focus := focusedNode(tree)
		if focus == nil {
			return mobiumerr.New(mobiumerr.NotConfirmed, "nothing on screen reports focus, so the remote cannot "+
				"be walked to %s", want)
		}
		if uitree.Describe(focus) == want || focus.Within(target) {
			if err := ctrl.Press(ctx, mobiumdriver.ButtonSelect); err != nil {
				return err
			}
			delete(h.refs, s.dev.Serial)
			return nil
		}
		if step >= maxRemoteSteps {
			return mobiumerr.New(mobiumerr.NotConfirmed, "pressed the D-pad %d times toward %s and focus is on %s",
				step, want, uitree.Describe(focus))
		}
		dir := towards(focus.Bounds, target.Bounds)
		if err := ctrl.Press(ctx, dir); err != nil {
			return err
		}
		if _, _, moved := h.awaitFocus(ctx, s, dir, focus); !moved {
			return mobiumerr.New(mobiumerr.NotConfirmed, "pressed %s toward %s and focus did not move from %s",
				dir, want, uitree.Describe(focus))
		}
	}
}

// towards is the D-pad button that moves from one rectangle toward another,
// along the axis the second is further out on.
func towards(from, to uitree.Rect) string {
	fx, fy := from.Center()
	tx, ty := to.Center()
	dx, dy := tx-fx, ty-fy
	if abs(dx) >= abs(dy) {
		if dx < 0 {
			return mobiumdriver.ButtonDpadLeft
		}
		return mobiumdriver.ButtonDpadRight
	}
	if dy < 0 {
		return mobiumdriver.ButtonDpadUp
	}
	return mobiumdriver.ButtonDpadDown
}

// forRemote gives a refusal on a device driven with its remote the way out
// that device has. A dialog's remedy named app_alert's button and a tap, and
// a screen in front named a tap at a point outside it: on an Apple TV both
// are refused, and a remedy that cannot work is obeyed anyway (CHALLENGES
// 334). Other refusals, and every refusal elsewhere, pass through.
func forRemote(s *session, err error) error {
	e, ok := mobiumerr.As(err)
	if !ok || !remoteOnly(s) {
		return err
	}
	loc, _ := e.Details["locator"].(string)
	if dialog, ok := e.Details["dialog"].(string); ok {
		return mobiumerr.New(e.Code, "a dialog is over the app — %q — and %s is not on it; an Apple TV answers "+
			"a dialog with its remote: move focus along its buttons with the D-pad and press select, or "+
			"declare an answer with app_dialogs", dialog, loc).
			WithRemedy("press dpad-left or dpad-right until the button has focus — each press reports where "+
				"focus went — then press select; or app_dialogs with when and press, which does that for you").
			WithDetail("locator", loc).
			WithDetail("dialog", dialog)
	}
	if over, _ := e.Details["screen"].(bool); over {
		return failedCheck(e.Code, loc, checkReceivesEvents, "another screen of the app is in front of it",
			"close that one first: back, the remote's Menu button, closes a screen the app put in front").
			WithRemedy("app_press back closes the screen in front; then try again").
			WithDetail("locator", loc)
	}
	return err
}
