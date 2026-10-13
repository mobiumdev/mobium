package mobiumdriver

import (
	"context"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Both device-side servers implement the W3C alert endpoints, so both backends
// get this for free and the methods are one line each. That is the whole
// argument for using those endpoints rather than tapping a button by its
// label: the platforms differ in what the dialog says and agree on how to
// answer it.

// AlertText reports what a system dialog says.
func (u *UIA2) AlertText(ctx context.Context) (string, error) { return u.w3c.alertText(ctx) }

// AnswerAlert accepts or dismisses a system dialog.
func (u *UIA2) AnswerAlert(ctx context.Context, accept bool) error {
	return u.w3c.answerAlert(ctx, accept)
}

// SendAlertText types into a prompt's field.
func (u *UIA2) SendAlertText(ctx context.Context, text string) error {
	return u.w3c.sendAlertText(ctx, text)
}

// AlertText reports what a system dialog says.
func (w *WDA) AlertText(ctx context.Context) (string, error) { return w.w3c.alertText(ctx) }

// AnswerAlert accepts or dismisses a system dialog.
//
// Not on an Apple TV: WebDriverAgent's accept there left the alert up,
// measured on a tvOS 26.5 simulator, and a TV's alert is answered with the
// remote anyway — it opens with focus on its cancel button, the D-pad moves
// along the buttons, and select presses the one that has focus.
func (w *WDA) AnswerAlert(ctx context.Context, accept bool) error {
	if w.tv() {
		return tvAlertError()
	}
	return w.w3c.answerAlert(ctx, accept)
}

// tvAlertError is how an Apple TV's alert is answered, made afresh each time
// so a caller can add details without changing anyone else's.
func tvAlertError() *mobiumerr.Error {
	return mobiumerr.New(mobiumerr.Unsupported, "an Apple TV's alert is answered with its remote — it "+
		"opens with focus on its cancel button. Move focus along the buttons with app_press dpad-left or "+
		"dpad-right and press select; back answers with the cancel button").
		WithRemedy("press dpad-left or dpad-right until the button has focus — each press reports where focus " +
			"went — then press select")
}

// AlertAnsweredByRemote is, for a device driven only with its remote, the
// refusal that says how its alert is answered, and nil for any other. An
// Apple TV has no touch: WebDriverAgent's accept left its alert up
// (CHALLENGES 298), and pressing a button by caption is a tap, which a TV
// refuses. app_alert asks before it chooses between accept, dismiss and a
// button, so none of its refusals names a way a TV cannot take.
func AlertAnsweredByRemote(d Driver) error {
	if w, ok := d.(*WDA); ok && w.tv() {
		return tvAlertError()
	}
	return nil
}

// SendAlertText types into a prompt's field.
func (w *WDA) SendAlertText(ctx context.Context, text string) error {
	return w.w3c.sendAlertText(ctx, text)
}
