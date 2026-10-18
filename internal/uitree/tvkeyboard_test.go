package uitree

import "testing"

// An Apple TV's keyboard, captured over Morpho Market's cart on a tvOS 26.5
// simulator: the field it edits is the discount code's, named by its title,
// and that field is not closed off behind it (CHALLENGES 333).
func TestKeyboardEditsOnAppleTV(t *testing.T) {
	for _, c := range []struct{ file, typed string }{
		{"ios-tvos26-morpho-keyboard-empty.xml", ""},
		{"ios-tvos26-morpho-keyboard-typed.xml", "abc"},
	} {
		tree := loadTree(t, c.file)
		editor, field := tree.KeyboardEdits()
		if editor == nil || editor.Class != "XCUIElementTypeTextView" {
			t.Fatalf("%s: editor = %+v, want the keyboard's text view", c.file, editor)
		}
		if editor.Text != c.typed {
			t.Errorf("%s: editor holds %q, want %q", c.file, editor.Text, c.typed)
		}
		if field == nil || field.TestID != "codeField" {
			t.Fatalf("%s: field = %+v, want codeField", c.file, field)
		}
		if field.Displayed {
			t.Errorf("%s: codeField reported shown; the capture has it hidden behind the keyboard", c.file)
		}
		if tree.CoveredByScreen(field) {
			t.Errorf("%s: the field the keyboard edits counted as behind another screen", c.file)
		}
	}
}

// No keyboard, and an iPhone's, which is keys alone: nothing is edited
// through a screen of the keyboard's own.
func TestKeyboardEditsNone(t *testing.T) {
	for _, file := range []string{"ios-tvos26-morpho-cart.xml", "ios-keyboard-over-button.xml",
		"ios-keyboard-over-scrolled-form.xml"} {
		if editor, field := loadTree(t, file).KeyboardEdits(); editor != nil || field != nil {
			t.Errorf("%s: KeyboardEdits = %v, %v; want nothing", file, editor, field)
		}
	}
}
