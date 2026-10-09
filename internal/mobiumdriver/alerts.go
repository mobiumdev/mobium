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
		return errTVAlert
	}
	return w.w3c.answerAlert(ctx, accept)
}

var errTVAlert = mobiumerr.New(mobiumerr.Unsupported, "an Apple TV's alert is answered with its remote — it "+
	"opens with focus on its cancel button. Move focus along the buttons with app_press dpad-left or "+
	"dpad-right and press select; back answers with the cancel button").
	WithRemedy("press dpad-left or dpad-right until the button has focus — each press reports where focus " +
		"went — then press select")

// SendAlertText types into a prompt's field.
func (w *WDA) SendAlertText(ctx context.Context, text string) error {
	return w.w3c.sendAlertText(ctx, text)
}
