package mobiumdriver

import "context"

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
func (w *WDA) AnswerAlert(ctx context.Context, accept bool) error {
	return w.w3c.answerAlert(ctx, accept)
}

// SendAlertText types into a prompt's field.
func (w *WDA) SendAlertText(ctx context.Context, text string) error {
	return w.w3c.sendAlertText(ctx, text)
}
