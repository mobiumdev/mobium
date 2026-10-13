package agent

import (
	"context"
	"errors"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"os"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/uitree"
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

// Tap is a button pressed on the dialog, which answers it.
func (d *alertDriver) Tap(ctx context.Context, x, y int) error {
	d.answered = "tap"
	return d.fakeDriver.Tap(ctx, x, y)
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
	return alertOnBackend(t, d, BackendUIA2, args)
}

func alertOnBackend(t *testing.T, d mobiumdriver.Driver, b Backend, args map[string]interface{}) (*ToolsCallResult, error) {
	t.Helper()
	h := NewHandlers()
	h.implicitWait, h.settleWindow = 0, 0
	s := &session{dev: fakeDevice(), driver: d, backend: b}
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
	// With no buttons it could read, the answer says it cannot name the one
	// it pressed, rather than naming one.
	v := res.StructuredContent.(AlertView)
	if v.Pressed != "" || !strings.Contains(v.Note, "could not be read") {
		t.Errorf("the answer claims more than it knows: pressed %q, note %q", v.Pressed, v.Note)
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

// dialogOn is a device with a dialog whose screen is a captured tree.
func dialogOn(t *testing.T, fixture, text string, ios bool) *alertDriver {
	t.Helper()
	raw, err := os.ReadFile("../uitree/testdata/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	parse := uitree.ParseAndroid
	if ios {
		parse = uitree.ParseIOS
	}
	tree, err := parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &alertDriver{fakeDriver: fakeDriver{screens: []*uitree.Tree{tree}}, text: text}
}

// A read names the dialog's buttons in the order it shows them, so a caller
// can choose one without a map of its own.
func TestAlertReadNamesTheButtons(t *testing.T) {
	d := dialogOn(t, "android-alert-three-uia2.xml", "Save this draft?", false)
	res, err := alertOn(t, d, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(res.StructuredContent.(AlertView).Buttons, "|"); got != "CANCEL|DON'T SAVE|SAVE" {
		t.Errorf("buttons = %q, want them left to right", got)
	}
}

// accept and dismiss mean yes and no, which a dialog of three does not have:
// they are refused, naming the buttons, and nothing is pressed. On Android's
// three-button alert dismiss pressed Don't Save, and on its camera prompt two
// of the three buttons grant it. CHALLENGES 305.
func TestAlertRefusesAcceptOnThreeButtons(t *testing.T) {
	for _, c := range []struct{ fixture, text, want string }{
		{"android-alert-three-uia2.xml", "Save this draft?", `"CANCEL", "DON'T SAVE", "SAVE"`},
		{"android-camera-permission-uia2.xml", "Allow MobiumApp to take pictures and record video?",
			`"While using the app", "Only this time", "Don’t allow"`},
	} {
		for _, action := range []string{"accept", "dismiss"} {
			d := dialogOn(t, c.fixture, c.text, false)
			_, err := alertOn(t, d, map[string]interface{}{"action": action})
			if mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
				t.Fatalf("%s %s on three buttons: err = %v, want unsupported", c.fixture, action, err)
			}
			if !strings.Contains(err.Error(), "has 3 buttons — "+c.want+" —") {
				t.Errorf("the refusal does not list the buttons in order: %v", err)
			}
			if d.answered != "" || len(d.tapped) > 0 {
				t.Errorf("%s pressed something it refused: answered %q, tapped %v", action, d.answered, d.tapped)
			}
			e, _ := mobiumerr.As(err)
			if !strings.Contains(e.Remedy, "button") {
				t.Errorf("the remedy does not name button: %q", e.Remedy)
			}
		}
	}
}

// On two buttons accept and dismiss go on through the platform, and say
// which caption they pressed — the one the dialog marks positive or
// negative on Android, by its id, not by where it sits.
func TestAlertSaysWhichButtonItPressed(t *testing.T) {
	for action, want := range map[string]string{"accept": "DISCARD", "dismiss": "KEEP EDITING"} {
		d := dialogOn(t, "android-alert-two-uia2.xml", "Discard changes?", false)
		res, err := alertOn(t, d, map[string]interface{}{"action": action})
		if err != nil {
			t.Fatal(err)
		}
		v := res.StructuredContent.(AlertView)
		if d.answered != action || v.Pressed != want || v.Answered != action {
			t.Errorf("%s: answered %q, pressed %q, want %q", action, d.answered, v.Pressed, want)
		}
		if !strings.Contains(textOf(res), `pressed "`+want+`"`) {
			t.Errorf("%s: the text does not say what was pressed: %q", action, textOf(res))
		}
	}
}

// button presses one by caption — any case, straight or curly quotes, since
// a caller types Don't where the dialog says Don’t — or by position from 1,
// tapping it where the screen shows it.
func TestAlertPressesAButtonByCaptionOrPosition(t *testing.T) {
	for button, want := range map[string]string{
		"don't allow": "Don’t allow", "While using the app": "While using the app", "2": "Only this time",
	} {
		d := dialogOn(t, "android-camera-permission-uia2.xml", "Allow MobiumApp to take pictures and record video?", false)
		res, err := alertOn(t, d, map[string]interface{}{"button": button})
		if err != nil {
			t.Fatalf("button %q: %v", button, err)
		}
		if v := res.StructuredContent.(AlertView); v.Pressed != want {
			t.Errorf("button %q pressed %q, want %q", button, v.Pressed, want)
		}
		if len(d.tapped) != 1 {
			t.Errorf("button %q tapped %v", button, d.tapped)
		}
	}
}

// A button the dialog does not have is refused with the ones it has, and
// button with accept or dismiss is refused before anything is touched.
func TestAlertRefusesAButtonItDoesNotHave(t *testing.T) {
	d := dialogOn(t, "android-alert-two-uia2.xml", "Discard changes?", false)
	_, err := alertOn(t, d, map[string]interface{}{"button": "Save"})
	if mobiumerr.CodeOf(err) != mobiumerr.NoSuchElement || !strings.Contains(err.Error(), `"KEEP EDITING", "DISCARD"`) {
		t.Errorf("err = %v", err)
	}
	_, err = alertOn(t, d, map[string]interface{}{"button": "4"})
	if mobiumerr.CodeOf(err) != mobiumerr.NoSuchElement {
		t.Errorf("a position past the end: err = %v", err)
	}
	_, err = alertOn(t, d, map[string]interface{}{"button": "DISCARD", "action": "accept"})
	if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Errorf("button with accept: err = %v", err)
	}
	if len(d.tapped) > 0 || d.answered != "" {
		t.Errorf("a refused call touched the dialog: tapped %v, answered %q", d.tapped, d.answered)
	}
}

// On iOS a two-button dialog's cancel button comes first, so dismiss pressed
// it and accept the other — the rule pressedBy states, measured on iOS 26.5.
func TestPressedByOnIOS(t *testing.T) {
	bs := []uitree.Entry{{Label: "Don’t Allow", Node: &uitree.Node{}}, {Label: "Allow", Node: &uitree.Node{}}}
	if got := pressedBy("ios", bs, true); got != "Allow" {
		t.Errorf("accept pressed %q", got)
	}
	if got := pressedBy("ios", bs, false); got != "Don’t Allow" {
		t.Errorf("dismiss pressed %q", got)
	}
	if got := pressedBy("ios", append(bs, bs[0]), true); got != "" {
		t.Errorf("three buttons named %q; accept is refused there", got)
	}
}

// Wear OS's own dialog is no alert to UiAutomator2, and app_alert said no
// dialog was on screen. Its ids mark ✓ positive and ✕ negative, so accept
// and dismiss press those, and say which. CHALLENGES 317.
func TestAWearDialogIsAnsweredByItsMarkedButtons(t *testing.T) {
	read := dialogOn(t, "wear-alert-uia2.xml", "", false)
	res, err := alertOn(t, read, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	v := res.StructuredContent.(AlertView)
	if !v.Present || !strings.Contains(v.Text, "Tilt-to-wake reduces battery life.") || strings.Join(v.Buttons, "|") != "Cancel|OK" {
		t.Errorf("read: present %v, text %q, buttons %q", v.Present, v.Text, v.Buttons)
	}
	if strings.Contains(textOf(res), "not one the platform") {
		t.Errorf("read says accept cannot answer it: %q", textOf(res))
	}
	after, err := os.ReadFile("../uitree/testdata/wear-gestures-uia2.xml")
	if err != nil {
		t.Fatal(err)
	}
	for action, want := range map[string]string{"accept": "OK", "dismiss": "Cancel"} {
		d := dialogOn(t, "wear-alert-uia2.xml", "", false)
		gone, err := uitree.ParseAndroid(after)
		if err != nil {
			t.Fatal(err)
		}
		d.screens = append(d.screens, gone)
		res, err := alertOn(t, d, map[string]interface{}{"action": action})
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		v := res.StructuredContent.(AlertView)
		if v.Pressed != want || v.Answered != action || len(d.tapped) != 1 {
			t.Errorf("%s: pressed %q, answered %q, tapped %v", action, v.Pressed, v.Answered, d.tapped)
		}
		if !strings.HasPrefix(textOf(res), action+"ed the dialog") || !strings.Contains(textOf(res), `pressed "`+want+`"`) {
			t.Errorf("%s: the text does not say it was answered: %q", action, textOf(res))
		}
	}
}
