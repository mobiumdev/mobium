package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// keyboardDriver is a device with a soft keyboard and one focused field.
type keyboardDriver struct {
	fakeDriver
	shown   bool
	field   *mobiumdriver.FocusedField
	pressed []string
	hides   int
}

func (k *keyboardDriver) KeyboardShown(ctx context.Context) (bool, error) { return k.shown, nil }
func (k *keyboardDriver) FocusedField(ctx context.Context) (*mobiumdriver.FocusedField, error) {
	return k.field, nil
}
func (k *keyboardDriver) TypeIntoFocus(ctx context.Context, text string) (*mobiumdriver.FocusedField, error) {
	if k.field == nil {
		return nil, mobiumdriver.ErrNoFocus
	}
	k.field.Value += text
	return k.field, nil
}
func (k *keyboardDriver) PressKeyboardKey(ctx context.Context, key string) error {
	k.pressed = append(k.pressed, key)
	return nil
}
func (k *keyboardDriver) HideKeyboard(ctx context.Context) error {
	k.hides++
	k.shown = false
	return nil
}

var _ mobiumdriver.Keyboard = (*keyboardDriver)(nil)

// kbCall runs app_keyboard against a fake session, as the other handler
// tests do through their …On variants.
type kbCall func(args map[string]interface{}) (*ToolsCallResult, error)

func withKeyboard(t *testing.T, d *keyboardDriver) kbCall {
	t.Helper()
	d.screens = []*uitree.Tree{{Root: &uitree.Node{Class: "android.widget.FrameLayout", Package: "com.example"}}}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	return func(args map[string]interface{}) (*ToolsCallResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return h.keyboardOn(ctx, s, args)
	}
}

func TestKeyboardArgumentsAreChecked(t *testing.T) {
	h := withKeyboard(t, &keyboardDriver{shown: true})
	if _, err := h(map[string]interface{}{"hide": true, "text": "x"}); mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Errorf("hide with text gave %v", err)
	}
	if _, err := h(map[string]interface{}{"key": "tab"}); mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Errorf("tab — two meanings, deliberately absent — gave %v", err)
	}
}

// Hiding an already-hidden keyboard must send nothing: on Android the key
// that hides it is Back, which with no keyboard up navigates the app.
func TestHidingAHiddenKeyboardSendsNothing(t *testing.T) {
	d := &keyboardDriver{shown: false}
	h := withKeyboard(t, d)
	res, err := h(map[string]interface{}{"hide": true})
	if err != nil {
		t.Fatal(err)
	}
	if d.hides != 0 || !strings.Contains(textOf(res), "already hidden") {
		t.Errorf("hides = %d, result %q", d.hides, textOf(res))
	}
	d.shown = true
	if _, err := h(map[string]interface{}{"hide": true}); err != nil || d.hides != 1 {
		t.Errorf("a shown keyboard: hides = %d, err = %v", d.hides, err)
	}
}

// A password field's value is typed into and never shown, in the text or
// the structured result — the rule every path that surfaces text follows.
func TestAPasswordIsNeverShown(t *testing.T) {
	d := &keyboardDriver{shown: true, field: &mobiumdriver.FocusedField{ID: "password", Password: true}}
	h := withKeyboard(t, d)
	res, err := h(map[string]interface{}{"text": "hunter2"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(textOf(res), "hunter2") {
		t.Errorf("the text shows the password: %q", textOf(res))
	}
	view := res.StructuredContent.(KeyboardView)
	if view.Focused == nil || strings.Contains(view.Focused.Value, "hunter2") || !view.Focused.Password || view.Typed != "" {
		t.Errorf("the structured result shows the password: typed %q, %+v", view.Typed, view.Focused)
	}
}

func TestTypingWithNothingFocusedNamesTheWayThrough(t *testing.T) {
	h := withKeyboard(t, &keyboardDriver{shown: false})
	_, err := h(map[string]interface{}{"text": "x"})
	if mobiumerr.CodeOf(err) != mobiumerr.NoSuchElement || !strings.Contains(err.Error(), "app_type") {
		t.Errorf("err = %v", err)
	}
}

// typeDriver accepts typing into any node.
type typeDriver struct {
	fakeDriver
	typed string
}

func (d *typeDriver) SetText(ctx context.Context, n *uitree.Node, text string) error {
	d.typed = text
	return nil
}
func (d *typeDriver) Clear(ctx context.Context, n *uitree.Node) error { return nil }

// app_type confirmed with `typed "hunter2" into testid=password` until it
// was noticed while building app_keyboard: the caller's own input, echoed
// into every transcript and log the result reaches. It says the length now.
func TestAppTypeDoesNotEchoAPassword(t *testing.T) {
	pw := &uitree.Node{Class: "android.widget.EditText", TestID: "password", Password: true,
		Clickable: true, Enabled: true, Displayed: true, Bounds: uitree.Rect{X1: 0, Y1: 0, X2: 100, Y2: 50}}
	root := &uitree.Node{Class: "android.widget.FrameLayout", Package: "com.example", Displayed: true,
		Bounds: uitree.Rect{X1: 0, Y1: 0, X2: 1000, Y2: 2000}, Children: []*uitree.Node{pw}}
	pw.Parent = root
	d := &typeDriver{}
	d.screens = []*uitree.Tree{{Root: root}}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	res, err := h.typeTextOn(context.Background(), s, map[string]interface{}{"target": "testid=password", "text": "hunter2"})
	if err != nil {
		t.Fatal(err)
	}
	if d.typed != "hunter2" {
		t.Errorf("typed %q", d.typed)
	}
	if strings.Contains(textOf(res), "hunter2") || !strings.Contains(textOf(res), "7 characters") {
		t.Errorf("result = %q", textOf(res))
	}
}
