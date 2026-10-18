package mobiumdriver

import (
	"context"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// tvEditor finds an Apple TV keyboard's text view while it is up. tvOS types
// into a screen of the keyboard's own, in front of the app (see
// uitree.KeyboardEdits), so while it is up the keyboard's text view is the
// field being typed into: it is what a typed key lands in, what is read back
// to confirm it, and what a retry clears — the app's field follows it, and
// the focused element is the keyboard, which holds no text. Measured on a
// tvOS 26.5 simulator: clear and set on the text view held "WELCOME10", and
// the app's field with it. CHALLENGES 333.
//
// el is the text view's element and field the app's field it edits, nil when
// the keyboard's title names none. ok is false on anything but an Apple TV,
// and when no keyboard is up. With n given, a keyboard editing some other
// field is refused: typing there would fill the wrong field.
func (w *WDA) tvEditor(ctx context.Context, n *uitree.Node) (el string, field *uitree.Node, ok bool, err error) {
	if !w.tv() {
		return "", nil, false, nil
	}
	tree, err := w.Snapshot(ctx)
	if err != nil {
		return "", nil, false, err
	}
	editor, field := tree.KeyboardEdits()
	if editor == nil {
		return "", nil, false, nil
	}
	if n != nil && field != nil && !sameField(field, n) {
		return "", nil, false, mobiumerr.New(mobiumerr.DeviceNotReady, "the keyboard is up for %s, not for %s",
			fieldName(field), fieldName(n)).
			WithRemedy("hide the keyboard with app_keyboard hide, move focus to the field with the D-pad, and " +
				"press select to open the keyboard for it")
	}
	el, err = w.w3c.findElement(ctx, "predicate string", `type == "XCUIElementTypeTextView" AND visible == 1`)
	if err != nil {
		return "", nil, false, err
	}
	return el, field, true, nil
}

// tvKeyboardFocused reports whether the focused element is an Apple TV's
// keyboard: two requests, so that a read of focus need not read the screen.
func (w *WDA) tvKeyboardFocused(ctx context.Context, active string) bool {
	if !w.tv() || active == "" {
		return false
	}
	kind, _ := w.w3c.elementAttribute(ctx, active, "type")
	return kind == "XCUIElementTypeKeyboard"
}

// sameField says whether two nodes, perhaps from different reads, are one
// text field: by its id, else by where it is in the tree.
func sameField(a, b *uitree.Node) bool {
	if a.TestID != "" || b.TestID != "" {
		return a.TestID == b.TestID
	}
	return a.Path == b.Path
}

func fieldName(n *uitree.Node) string {
	switch {
	case n.TestID != "":
		return "testid=" + n.TestID
	case n.Label != "":
		return "the field " + n.Label
	case n.Hint != "":
		return "the field " + n.Hint
	}
	return "another field"
}
