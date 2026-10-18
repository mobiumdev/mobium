package uitree

import "strings"

// KeyboardEdits finds what an Apple TV's keyboard is editing. tvOS has no
// keyboard under a field: selecting a text field puts a screen of its own in
// front of the app — the field's name as a title, a text view of its own,
// and the keys — and what is typed goes into that text view. The app's field
// stays in the tree, reported hidden, behind it, and follows the text view
// as it changes: "abc" typed held "abc" in both, measured on a tvOS 26.5
// simulator. Asked of the app's field, everything about the keyboard said
// the wrong thing: the field read as "behind another screen", and the
// focused element, the keyboard itself, held no text, so three keystrokes
// that had landed read as dropped and were typed twice more (CHALLENGES 333).
//
// editor is the keyboard's text view, which holds what has been typed.
// field is the app's text field it is editing: the one whose placeholder or
// label is the keyboard's title, or nil when none is, or more than one is and
// none holds what the text view does. Both are nil when no such keyboard is
// up, which is so on an iPhone: its keyboard is a window of keys alone.
func (t *Tree) KeyboardEdits() (editor, field *Node) {
	kb := t.Keyboard()
	if kb == nil {
		return nil, nil
	}
	// The panel is the nearest ancestor of the keys that holds a text view
	// besides them. The search stops at the window: the app's own text views
	// are in another branch of it, and an iPhone's keyboard window has none.
	var panel *Node
	for a := kb.Parent; a != nil && editor == nil; a = a.Parent {
		if a.Class == "XCUIElementTypeWindow" || a.Class == "XCUIElementTypeApplication" {
			break
		}
		walkNode(a, func(n *Node) bool {
			if n == kb {
				return false
			}
			if n.Class == "XCUIElementTypeTextView" && n.Displayed && !n.Bounds.Empty() {
				editor = n
				return false
			}
			return editor == nil
		})
		panel = a
	}
	if editor == nil {
		return nil, nil
	}
	// The title is the text above the text view: the field's placeholder, or
	// its label when it has one.
	var title string
	walkNode(panel, func(n *Node) bool {
		if title != "" {
			return false
		}
		if n.Class == "XCUIElementTypeStaticText" && n.Displayed && n.Bounds.Y2 <= editor.Bounds.Y1 {
			title = strings.TrimSpace(firstNonEmpty(n.Text, n.Label))
		}
		return true
	})
	if title == "" {
		return editor, nil
	}
	var named []*Node
	t.Walk(func(n *Node) bool {
		if n.Within(panel) {
			return false
		}
		if isTextEntry(n.Class) && (n.Hint == title || n.Label == title) {
			named = append(named, n)
		}
		return true
	})
	switch {
	case len(named) == 1:
		return editor, named[0]
	case len(named) > 1 && editor.Text != "":
		var holding []*Node
		for _, n := range named {
			if n.Text == editor.Text || n.Value == editor.Text {
				holding = append(holding, n)
			}
		}
		if len(holding) == 1 {
			return editor, holding[0]
		}
	}
	return editor, nil
}

// EditedByKeyboard reports whether n is the field an Apple TV's keyboard is
// editing: behind the keyboard's screen and under its keys, and reached
// through them.
func (t *Tree) EditedByKeyboard(n *Node) bool {
	_, f := t.KeyboardEdits()
	return f != nil && f == n
}

func isTextEntry(class string) bool {
	switch class {
	case "XCUIElementTypeTextField", "XCUIElementTypeSecureTextField", "XCUIElementTypeSearchField",
		"XCUIElementTypeTextView":
		return true
	}
	return false
}

func walkNode(n *Node, fn func(*Node) bool) {
	if !fn(n) {
		return
	}
	for _, c := range n.Children {
		walkNode(c, fn)
	}
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
