package mobiumdriver

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// KeyboardKeys are the named keys app_keyboard can press, the same on both
// platforms. Tab is not among them: on Android it moves focus, and on iOS it
// types a tab character into the field — measured in the same text field on
// each — so one name would mean two things.
var KeyboardKeys = []string{"enter", "delete", "space"}

// FocusedField is the text field with keyboard focus, as the device server
// reports it. Value is raw: a caller that surfaces it redacts a password.
type FocusedField struct {
	ID       string
	Kind     string
	Value    string
	Password bool
}

// ErrNoFocus is returned when no field has keyboard focus.
var ErrNoFocus = mobiumerr.New(mobiumerr.NoSuchElement, "no field has keyboard focus — tap one first, "+
	"or use app_type with a target, which finds and focuses the field itself")

// hideKeyboardWait bounds the wait for a hidden keyboard to be reported gone.
const hideKeyboardWait = 3 * time.Second

// activeElement asks for the element with keyboard focus — W3C's Get Active
// Element, which both UiAutomator2 and WebDriverAgent answer with the focused
// text field (measured on each) — or reports ErrNoFocus.
func (c *w3cClient) activeElement(ctx context.Context) (string, error) {
	var resp struct {
		Value map[string]interface{} `json:"value"`
	}
	if err := c.do(ctx, http.MethodGet, c.sessionPath("/element/active"), nil, &resp); err != nil {
		if e, ok := mobiumerr.As(err); ok && e.Code == mobiumerr.NoSuchElement {
			return "", ErrNoFocus
		}
		return "", err
	}
	for _, key := range []string{"ELEMENT", "element-6066-11e4-a52e-4f735466cecf"} {
		if id, _ := resp.Value[key].(string); id != "" {
			return id, nil
		}
	}
	return "", ErrNoFocus
}

func (c *w3cClient) elementAttribute(ctx context.Context, elementID, name string) (string, error) {
	var resp struct {
		Value interface{} `json:"value"`
	}
	if err := c.do(ctx, http.MethodGet, c.sessionPath("/element/"+elementID+"/attribute/"+name), nil, &resp); err != nil {
		return "", err
	}
	switch v := resp.Value.(type) {
	case string:
		return v, nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case nil:
		return "", nil
	}
	return "", nil
}

func (c *w3cClient) elementText(ctx context.Context, elementID string) (string, error) {
	var resp struct {
		Value *string `json:"value"`
	}
	if err := c.do(ctx, http.MethodGet, c.sessionPath("/element/"+elementID+"/text"), nil, &resp); err != nil {
		return "", err
	}
	if resp.Value == nil {
		return "", nil
	}
	return *resp.Value, nil
}

// waitKeyboardGone polls until shown reports false.
func waitKeyboardGone(ctx context.Context, shown func(context.Context) (bool, error)) error {
	deadline := time.Now().Add(hideKeyboardWait)
	for {
		up, err := shown(ctx)
		if err != nil {
			return err
		}
		if !up {
			return nil
		}
		if time.Now().After(deadline) {
			return mobiumerr.New(mobiumerr.NotConfirmed, "asked the keyboard to hide and it is still up after %s", hideKeyboardWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func unknownKey(key string) error {
	return mobiumerr.New(mobiumerr.InvalidArgument, "unknown key %q (want %s)", key, strings.Join(KeyboardKeys, ", "))
}

// ---- Android --------------------------------------------------------------

var keyboardKeycodes = map[string]string{
	"enter": "KEYCODE_ENTER", "delete": "KEYCODE_DEL", "space": "KEYCODE_SPACE",
}

func androidPressKeyboardKey(ctx context.Context, adb interface {
	PressKey(context.Context, string) error
}, key string) error {
	code, ok := keyboardKeycodes[key]
	if !ok {
		return unknownKey(key)
	}
	return adb.PressKey(ctx, code)
}

// androidHideKeyboard sends Back, and only while the keyboard is up: Back is
// the key the keyboard consumes first, which Android defines, but with no
// keyboard showing it navigates the app back instead.
func androidHideKeyboard(ctx context.Context, adb interface {
	KeyboardShown(context.Context) (bool, error)
	PressKey(context.Context, string) error
}) error {
	up, err := adb.KeyboardShown(ctx)
	if err != nil || !up {
		return err
	}
	if err := adb.PressKey(ctx, "KEYCODE_BACK"); err != nil {
		return err
	}
	return waitKeyboardGone(ctx, adb.KeyboardShown)
}

// KeyboardShown reads mInputShown.
func (u *UIA2) KeyboardShown(ctx context.Context) (bool, error) { return u.adb.KeyboardShown(ctx) }

// KeyboardRegions reports where the keyboard takes touches, in pixels.
func (u *UIA2) KeyboardRegions(ctx context.Context) ([]uitree.Rect, error) {
	return androidKeyboardRegions(ctx, u.adb)
}

// ClipboardPreviewRegions reports where Android's clipboard preview takes
// touches, in pixels.
func (u *UIA2) ClipboardPreviewRegions(ctx context.Context) ([]uitree.Rect, error) {
	return rects(u.adb.ClipboardPreviewRegions(ctx))
}

func androidKeyboardRegions(ctx context.Context, adb *device.ADB) ([]uitree.Rect, error) {
	return rects(adb.KeyboardRegions(ctx))
}

func rects(raw [][4]int, err error) ([]uitree.Rect, error) {
	if err != nil {
		return nil, err
	}
	rects := make([]uitree.Rect, len(raw))
	for i, r := range raw {
		rects[i] = uitree.Rect{X1: r[0], Y1: r[1], X2: r[2], Y2: r[3]}
	}
	return rects, nil
}

// PressKeyboardKey sends a named key.
func (u *UIA2) PressKeyboardKey(ctx context.Context, key string) error {
	return androidPressKeyboardKey(ctx, u.adb, key)
}

// HideKeyboard hides the soft keyboard, confirmed by reading it back.
func (u *UIA2) HideKeyboard(ctx context.Context) error { return androidHideKeyboard(ctx, u.adb) }

// FocusedField reports the field with keyboard focus, or nil.
func (u *UIA2) FocusedField(ctx context.Context) (*FocusedField, error) {
	id, err := u.w3c.activeElement(ctx)
	if err == ErrNoFocus {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Android answers the active-element question with whatever it last had,
	// so the element is asked whether it is focused.
	if focused, _ := u.w3c.elementAttribute(ctx, id, "focused"); focused != "true" {
		return nil, nil
	}
	f := &FocusedField{}
	f.ID, _ = u.w3c.elementAttribute(ctx, id, "resource-id")
	f.Kind, _ = u.w3c.elementAttribute(ctx, id, "class")
	pw, _ := u.w3c.elementAttribute(ctx, id, "password")
	f.Password = pw == "true"
	if f.Value, err = u.w3c.elementText(ctx, id); err != nil {
		return nil, err
	}
	// An empty field reports its placeholder as its text, and the same
	// string as its hint — measured on API 35 — so the two agreeing means
	// empty. Without this, an empty field looked full and every append to
	// it was reported as a mismatch.
	if hint, _ := u.w3c.elementAttribute(ctx, id, "hint"); hint != "" && hint == f.Value {
		f.Value = ""
	}
	return f, nil
}

// HasFocus reads the node's own `focused`, which UiAutomator2 reports.
func (u *UIA2) HasFocus(_ context.Context, n *uitree.Node) (bool, error) { return n.Focused, nil }

// TypeIntoFocus adds text to the end of the focused field and confirms it.
//
// UiAutomator2 cannot type at the cursor: its keys endpoint, and the
// element's value endpoint, replace the field's contents whatever they are
// asked — "ab" then "cd" left "cd", with replace=false too, measured on
// API 35. So the field is read, and written back as what it held plus the
// text: the end of the field rather than the cursor, which is where a
// just-focused field's cursor is anyway. A password field cannot be read —
// it reports one bullet per character — so one that already holds something
// is refused rather than silently replaced.
func (u *UIA2) TypeIntoFocus(ctx context.Context, text string) (*FocusedField, error) {
	before, err := u.FocusedField(ctx)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNoFocus
	}
	want := before.Value + text
	if before.Password {
		if n := len([]rune(before.Value)); n > 0 {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "the focused password field already holds %d characters, "+
				"which Android will not read back, and typing here would replace them — type the whole password "+
				"with app_fill, or delete it first (app_keyboard with key \"delete\")", n)
		}
		want = text
	}
	if err := u.w3c.do(ctx, http.MethodPost, u.w3c.sessionPath("/keys"), map[string]interface{}{"text": want}, nil); err != nil {
		return nil, err
	}
	after, err := u.FocusedField(ctx)
	if err != nil || after == nil {
		return nil, mobiumerr.New(mobiumerr.NotConfirmed, "typed into the focused field and could not read it back")
	}
	if after.Password {
		if len([]rune(after.Value)) != len([]rune(want)) {
			return after, mobiumerr.New(mobiumerr.NotConfirmed, "typed %d characters into the password field and it "+
				"holds %d", len([]rune(want)), len([]rune(after.Value)))
		}
		return after, nil
	}
	if after.Value != want {
		return after, mobiumerr.New(mobiumerr.NotConfirmed, "typed %q into %s and it holds %q", text, after.ID, after.Value)
	}
	return after, nil
}

// The adb-only backend shares the device-side half and refuses the rest:
// `adb shell input text` mangles quotes and non-ASCII, which is why it
// already refuses app_type, and it has no way to ask what has focus.

// KeyboardShown reads mInputShown.
func (a *Android) KeyboardShown(ctx context.Context) (bool, error) { return a.adb.KeyboardShown(ctx) }

// PressKeyboardKey sends a named key.
func (a *Android) PressKeyboardKey(ctx context.Context, key string) error {
	return androidPressKeyboardKey(ctx, a.adb, key)
}

// HideKeyboard hides the soft keyboard, confirmed by reading it back.
func (a *Android) HideKeyboard(ctx context.Context) error { return androidHideKeyboard(ctx, a.adb) }

// FocusedField is refused: `uiautomator dump` has no notion of an active element.
func (a *Android) FocusedField(ctx context.Context) (*FocusedField, error) {
	return nil, mobiumerr.New(mobiumerr.Unsupported, "the uiautomator backend cannot tell which field has focus — "+
		"use the uiautomator2 backend")
}

// HasFocus reads the node's own `focused`, which `uiautomator dump` reports.
func (a *Android) HasFocus(_ context.Context, n *uitree.Node) (bool, error) { return n.Focused, nil }

// TypeIntoFocus is refused for the reason app_type is on this backend.
func (a *Android) TypeIntoFocus(ctx context.Context, text string) (*FocusedField, error) {
	return nil, mobiumerr.New(mobiumerr.Unsupported, "the uiautomator backend cannot type correctly: `adb shell input text` "+
		"mangles quotes and non-ASCII — use the uiautomator2 backend")
}

// ---- iOS ------------------------------------------------------------------

var iosKeyChars = map[string]string{"enter": "\n", "delete": "\b", "space": " "}

func (w *WDA) typeKeys(ctx context.Context, text string) error {
	return w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/wda/keys"),
		map[string]interface{}{"value": strings.Split(text, "")}, nil)
}

// KeyboardShown looks for the keyboard in the hierarchy, where iOS puts it —
// its keys along with it.
func (w *WDA) KeyboardShown(ctx context.Context) (bool, error) {
	// Read as every read is: under a notification banner WebDriverAgent
	// reads SpringBoard, which has no keyboard, and a raw read here said
	// "hidden" over a keyboard that was up — so `keyboard --hide` answered
	// "already hidden" and the next tap was refused as covered. Snapshot
	// reads the app under the banner (CHALLENGES 155, 288).
	tree, err := w.Snapshot(ctx)
	if err != nil {
		src, serr := w.w3c.source(ctx)
		if serr != nil {
			return false, err
		}
		return strings.Contains(src, `type="XCUIElementTypeKeyboard"`), nil
	}
	// On screen, not merely in the tree: with a hardware keyboard attached a
	// simulator keeps the software one below the screen's edge, where it
	// covers nothing and nobody can see it. Counting it said "shown" over an
	// empty screen, and the keyboard check believed it (CHALLENGES 107).
	return tree.Keyboard() != nil, nil
}

// PressKeyboardKey types the key's character, which is how WebDriverAgent
// presses a key: "\b" deleted two characters for two, "\n" pressed return.
func (w *WDA) PressKeyboardKey(ctx context.Context, key string) error {
	ch, ok := iosKeyChars[key]
	if !ok {
		return unknownKey(key)
	}
	return w.typeKeys(ctx, ch)
}

// HideKeyboard asks WebDriverAgent to dismiss the keyboard and confirms it
// went. An iPhone keyboard has no key that hides it — dismissing is the
// app's to decide — and WebDriverAgent says so: "Did not know how to dismiss
// the keyboard", measured on an iPhone 17 Pro simulator. That is refused
// with the way that usually works, not approximated by tapping somewhere.
func (w *WDA) HideKeyboard(ctx context.Context) error {
	up, err := w.KeyboardShown(ctx)
	if err != nil || !up {
		return err
	}
	if err := w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/wda/keyboard/dismiss"), map[string]interface{}{}, nil); err != nil {
		if e, ok := mobiumerr.As(err); ok && e.Details["w3c"] == "invalid element state" {
			return mobiumerr.New(mobiumerr.Unsupported, "an iPhone keyboard has no key that hides it, and this app "+
				"gave WebDriverAgent no other way — press enter (app_keyboard with key \"enter\"), which most "+
				"single-line fields answer by giving up focus, or tap outside the field: %w", err)
		}
		return err
	}
	return waitKeyboardGone(ctx, w.KeyboardShown)
}

// FocusedField reports the field with keyboard focus, or nil.
func (w *WDA) FocusedField(ctx context.Context) (*FocusedField, error) {
	id, err := w.w3c.activeElement(ctx)
	if err == ErrNoFocus {
		// Under a notification banner the question goes to SpringBoard,
		// where nothing has focus; asked of the app under it, the field
		// that has it answers (CHALLENGES 288).
		if app := w.appUnderBanner(ctx); app != "" {
			_ = w.asApp(ctx, app, func() error {
				id, err = w.w3c.activeElement(ctx)
				return err
			})
		}
	}
	if err == ErrNoFocus {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f := &FocusedField{}
	f.ID, _ = w.w3c.elementAttribute(ctx, id, "name")
	f.Kind, _ = w.w3c.elementAttribute(ctx, id, "type")
	f.Password = f.Kind == "XCUIElementTypeSecureTextField"
	if f.Value, err = w.w3c.elementValue(ctx, id); err != nil {
		return nil, err
	}
	// As on Android, an empty field reports its placeholder as its value —
	// "username" for an empty username field on the simulator. Taken at its
	// word, every append looked like a dropped keystroke, and the retry for
	// one then wrote the placeholder into the field: "usernamemob…". The
	// placeholder is its own attribute, so the two agreeing means empty.
	if ph, _ := w.w3c.elementAttribute(ctx, id, "placeholderValue"); ph != "" && ph == f.Value {
		f.Value = ""
	}
	return f, nil
}

// HasFocus asks whether n is the active element. The tree cannot say — every
// field reports focused="false", the one with the cursor included — but
// WebDriverAgent gives an element the same id on every lookup, and the active
// element is the field with the cursor: username's id, then password's after
// a tap on it, and "no such element" with nothing focused, measured on the
// simulator.
func (w *WDA) HasFocus(ctx context.Context, n *uitree.Node) (bool, error) {
	active, err := w.w3c.activeElement(ctx)
	if err == ErrNoFocus {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	id, err := w.elementFor(ctx, n)
	if err != nil {
		return false, err
	}
	return id == active, nil
}

// confirmPassword confirms keys typed into a focused password field by its
// length, which is all that can be read of it.
func (w *WDA) confirmPassword(ctx context.Context, had int, text string) (*FocusedField, error) {
	want := had + len([]rune(text))
	for attempt := 1; ; attempt++ {
		after, err := w.FocusedField(ctx)
		if err != nil || after == nil {
			return nil, mobiumerr.New(mobiumerr.NotConfirmed, "typed into the focused password field and could not "+
				"read its length back")
		}
		held := len([]rune(after.Value))
		if held == want {
			return after, nil
		}
		if had > 0 || attempt >= setTextAttempts {
			if err := w.keyboardLacks(ctx, text, len([]rune(text)), had, held); err != nil {
				return after, err
			}
			return after, mobiumerr.New(mobiumerr.NotConfirmed, "typed %d characters into the password field, which "+
				"held %d, and it holds %d — the keystrokes did not all arrive", len([]rune(text)), had, held)
		}
		id, err := w.w3c.activeElement(ctx)
		if err != nil {
			return nil, err
		}
		if err := w.w3c.clearElement(ctx, id); err != nil {
			return nil, err
		}
		if err := w.w3c.setElementValueAt(ctx, id, text, typingFrequencies[attempt]); err != nil {
			return nil, err
		}
	}
}

// TypeIntoFocus types at the cursor of the focused field and confirms it.
//
// iOS drops keystrokes: "mob ü\"q'" typed into the focused field on the
// simulator came back "m ü\"q'", reported as a success (CHALLENGES 61). So
// a mismatch is retried as app_type retries — the field set to what it
// should hold and read back. A password reads back as bullets, so only its
// length is compared, and it is retried only when it was empty before, the
// one case where what it should hold is known (CHALLENGES 159).
func (w *WDA) TypeIntoFocus(ctx context.Context, text string) (*FocusedField, error) {
	before, err := w.FocusedField(ctx)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNoFocus
	}
	if err := w.typeKeys(ctx, text); err != nil {
		return nil, err
	}
	if before.Password {
		return w.confirmPassword(ctx, len([]rune(before.Value)), text)
	}
	want := before.Value + text
	for attempt := 1; ; attempt++ {
		after, err := w.FocusedField(ctx)
		if err != nil || after == nil {
			return nil, mobiumerr.New(mobiumerr.NotConfirmed, "typed into the focused field and could not read it back")
		}
		if after.Value == want {
			return after, nil
		}
		if attempt >= setTextAttempts {
			return after, mobiumerr.New(mobiumerr.NotConfirmed, "typed %q and the field holds %q — iOS dropped a "+
				"keystroke, and retrying more slowly did not recover it", text, after.Value)
		}
		id, err := w.w3c.activeElement(ctx)
		if err != nil {
			return nil, err
		}
		if err := w.w3c.clearElement(ctx, id); err != nil {
			return nil, err
		}
		if err := w.w3c.setElementValueAt(ctx, id, want, typingFrequencies[attempt]); err != nil {
			return nil, err
		}
	}
}
