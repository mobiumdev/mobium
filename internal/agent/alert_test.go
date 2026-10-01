package agent

import (
	"context"
	"errors"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// alertDriver is a device with a dialog on it, or without one.
type alertDriver struct {
	fakeDriver
	text string
	// gone is how many reads it takes for the dialog to disappear after being
	// answered. A real dialog animates out, so "answered" and "gone" are not
	// the same instant.
	gone     int
	answered string
	typed    string
	// typeErr is what the platform says when there is nothing to type into.
	typeErr error
}

func (d *alertDriver) AlertText(ctx context.Context) (string, error) {
	if d.text == "" {
		return "", mobiumdriver.ErrNoAlert
	}
	if d.answered != "" {
		if d.gone > 0 {
			d.gone--
			return d.text, nil
		}
		return "", mobiumdriver.ErrNoAlert
	}
	return d.text, nil
}

func (d *alertDriver) AnswerAlert(ctx context.Context, accept bool) error {
	if d.text == "" {
		return mobiumdriver.ErrNoAlert
	}
	d.answered = "dismiss"
	if accept {
		d.answered = "accept"
	}
	return nil
}

func (d *alertDriver) SendAlertText(ctx context.Context, text string) error {
	if d.typeErr != nil {
		return d.typeErr
	}
	d.typed = text
	return nil
}

var _ mobiumdriver.Alerts = (*alertDriver)(nil)

func alertOn(t *testing.T, d mobiumdriver.Driver, args map[string]interface{}) (*ToolsCallResult, error) {
	t.Helper()
	h := NewHandlers()
	h.implicitWait, h.settleWindow = 0, 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.alertOn(ctx, s, args)
}

// Nothing asking the user anything is a state, not a failure. Reporting it as
// an error would make a calm screen indistinguishable from a broken device.
func TestAlertReadsNothingWithoutFailing(t *testing.T) {
	res, err := alertOn(t, &alertDriver{}, map[string]interface{}{})
	if err != nil {
		t.Fatalf("no dialog was reported as an error: %v", err)
	}
	if v := res.StructuredContent.(AlertView); v.Present {
		t.Error("Present is true with no dialog on screen")
	}
	if !strings.Contains(textOf(res), "no dialog") {
		t.Errorf("answer = %q", textOf(res))
	}
}

func TestAlertReadsTheText(t *testing.T) {
	res, err := alertOn(t, &alertDriver{text: "Allow access?"}, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	v := res.StructuredContent.(AlertView)
	if !v.Present || v.Text != "Allow access?" {
		t.Errorf("view = %+v", v)
	}
	if v.Answered != "" {
		t.Error("reading answered the dialog")
	}
}

// Answering waits for the dialog to actually leave. "The request was accepted"
// is not "the dialog is gone", which is the rule about commands that start
// something not being finished when they return.
func TestAlertWaitsForTheDialogToGo(t *testing.T) {
	d := &alertDriver{text: "Allow access?", gone: 3}
	res, err := alertOn(t, d, map[string]interface{}{"action": "accept"})
	if err != nil {
		t.Fatal(err)
	}
	if d.gone != 0 {
		t.Errorf("returned with %d reads still showing the dialog", d.gone)
	}
	if d.answered != "accept" {
		t.Errorf("answered %q", d.answered)
	}
	// The answer must not claim an outcome it did not choose.
	if !strings.Contains(textOf(res), "did not choose an outcome") {
		t.Errorf("the answer implies more than it did: %q", textOf(res))
	}
}

// A dialog that will not leave must be reported, not waited on forever and
// then called success.
func TestAlertReportsADialogThatWillNotGo(t *testing.T) {
	d := &alertDriver{text: "Stuck", gone: 1 << 30}
	_, err := alertOn(t, d, map[string]interface{}{"action": "dismiss"})
	if err == nil {
		t.Fatal("a dialog still on screen was reported as answered")
	}
	if !strings.Contains(err.Error(), "still on screen") {
		t.Errorf("err = %v", err)
	}
}

func TestAlertRefusesToAnswerNothing(t *testing.T) {
	_, err := alertOn(t, &alertDriver{}, map[string]interface{}{"action": "accept"})
	if err == nil {
		t.Fatal("accepting with no dialog was reported as success")
	}
	if !strings.Contains(err.Error(), "no dialog to accept") {
		t.Errorf("err = %v", err)
	}
}

func TestAlertRejectsAnUnknownAction(t *testing.T) {
	_, err := alertOn(t, &alertDriver{text: "x"}, map[string]interface{}{"action": "maybe"})
	if err == nil || !strings.Contains(err.Error(), "accept") {
		t.Errorf("err = %v", err)
	}
}

// Typing happens before answering, so one call can fill a prompt and confirm
// it — which is the shape a prompt actually has.
func TestAlertTypesThenAnswers(t *testing.T) {
	d := &alertDriver{text: "Name this draft"}
	res, err := alertOn(t, d, map[string]interface{}{"action": "accept", "text": "q3"})
	if err != nil {
		t.Fatal(err)
	}
	if d.typed != "q3" {
		t.Errorf("typed %q", d.typed)
	}
	if d.answered != "accept" {
		t.Errorf("answered %q — typing must not replace answering", d.answered)
	}
	if v := res.StructuredContent.(AlertView); v.Typed != "q3" {
		t.Errorf("view = %+v", v)
	}
}

func TestAlertTypesWithoutAnswering(t *testing.T) {
	d := &alertDriver{text: "Name this draft"}
	if _, err := alertOn(t, d, map[string]interface{}{"text": "q3"}); err != nil {
		t.Fatal(err)
	}
	if d.typed != "q3" || d.answered != "" {
		t.Errorf("typed %q, answered %q", d.typed, d.answered)
	}
}

// The two reasons typing can fail are different and a caller should not have
// to guess which it met: Android's server does not implement it at all, and
// React Native has no prompt there either, while on iOS a plain alert simply
// has no field.
func TestAlertSaysWhyItCannotType(t *testing.T) {
	// Shaped as the W3C client returns it: the server's own code kept in
	// Details, not only in the wording.
	unsupported := &alertDriver{text: "Delete?", typeErr: mobiumerr.New(mobiumerr.Unsupported,
		"unknown command: no such route").WithDetail("w3c", "unknown command")}
	_, err := alertOn(t, unsupported, map[string]interface{}{"text": "x"})
	if err == nil || !strings.Contains(err.Error(), "does not implement it") {
		t.Errorf("an unsupporting backend gave: %v", err)
	}
	if mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
		t.Errorf("code = %s, want unsupported", mobiumerr.CodeOf(err))
	}

	noField := &alertDriver{text: "Delete?", typeErr: errors.New("invalid element state")}
	_, err = alertOn(t, noField, map[string]interface{}{"text": "x"})
	if err == nil || !strings.Contains(err.Error(), "no text field") {
		t.Errorf("an alert with no field gave: %v", err)
	}
}
