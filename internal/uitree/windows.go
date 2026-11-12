package uitree

import "strings"

// A device where apps are windows over one another. Horizon OS, on the Meta
// Spatial Simulator, keeps every app open as a panel smaller than the display,
// over its shell's own windows: a full-screen one with the home screen, a
// 200x200 one with the settings gear and a 1x1 one, all resumed at once and
// stacked in an order that changed from one boot to the next. UiAutomator2
// reads the window in front alone unless asked for every one, so the home
// screen mapped as nothing — the 1x1 window was in front — and as the gear
// alone on the next boot. And a panel does not fill the screen, so the window
// in front read as a dialog: the browser over Gallery was "a dialog — "" —"
// to answer, and MobiumApp itself "a dialog — Popup —", its screen's heading.
//
// Asked for every window, UiAutomator2 lists them top first. Everything here
// decides by document order being drawing order — a cover is a later node —
// so MarkWindows puts them bottom first, and a window, which takes every
// touch inside its bounds, covers whatever lies under it in a lower one.
// CHALLENGES 364-367.

// MarkWindows turns a hierarchy of every window, top first, into one in
// drawing order: bottom first, each window's root marked Window. Paths are
// kept as the source gave them, so a node is still found where it was.
func (t *Tree) MarkWindows() {
	if t == nil || t.Root == nil {
		return
	}
	ws := t.Root.Children
	for i, j := 0, len(ws)-1; i < j; i, j = i+1, j-1 {
		ws[i], ws[j] = ws[j], ws[i]
	}
	for _, w := range ws {
		w.Window = true
	}
	t.Windowed = true
}

// frontWindow is the window in front, in a windowed hierarchy, or nil.
func (t *Tree) frontWindow() *Node {
	if t == nil || !t.Windowed || t.Root == nil {
		return nil
	}
	for i := len(t.Root.Children) - 1; i >= 0; i-- {
		if w := t.Root.Children[i]; !w.Bounds.Empty() && !t.isBar(w) && !t.isKeyboard(w) {
			return w
		}
	}
	return nil
}

func (t *Tree) isBar(w *Node) bool {
	for _, p := range t.SystemBars {
		if w.Package == p {
			return true
		}
	}
	return false
}

func (t *Tree) isKeyboard(w *Node) bool {
	return t.KeyboardPackage != "" && w.Package == t.KeyboardPackage
}

// appArea is the screen less the system bars along its edges: what an app
// has to draw in. False when no bar lies along an edge.
func (t *Tree) appArea() (Rect, bool) {
	area, found := t.Screen, false
	for _, w := range t.Root.Children {
		b := w.Bounds
		if !t.isBar(w) || b.Empty() {
			continue
		}
		wide := b.X1 <= area.X1 && b.X2 >= area.X2
		tall := b.Y1 <= area.Y1 && b.Y2 >= area.Y2
		switch {
		case wide && b.Y1 <= area.Y1 && b.Y2 > area.Y1:
			area.Y1, found = b.Y2, true
		case wide && b.Y2 >= area.Y2 && b.Y1 < area.Y2:
			area.Y2, found = b.Y1, true
		case tall && b.X1 <= area.X1 && b.X2 > area.X1:
			area.X1, found = b.X2, true
		case tall && b.X2 >= area.X2 && b.X1 < area.X2:
			area.X2, found = b.X1, true
		}
	}
	return area, found && !area.Empty()
}

// windowedDialog is the window in front when it is a dialog: inside a lower
// window that is itself no bigger than a panel. A dialog an app opens lies
// over its own panel — the permission prompt's window had MobiumApp's
// panel's very bounds — while a panel lies over the shell's full-screen
// window, or beside another panel, inside neither. A small panel placed
// wholly over a larger one would read as a dialog; not yet seen.
func (t *Tree) windowedDialog() *Node {
	front := t.frontWindow()
	if front == nil {
		return nil
	}
	// Under system bars the app's own window is not in the hierarchy while
	// its dialog is up. A letterboxed app keeps one side of the app's area
	// whole, and a dialog neither (CHALLENGES 395).
	if area, ok := t.appArea(); ok {
		b := front.Bounds
		tall := b.Y1 <= area.Y1 && b.Y2 >= area.Y2
		wide := b.X1 <= area.X1 && b.X2 >= area.X2
		if !tall && !wide {
			return front
		}
		return nil
	}
	for _, w := range t.Root.Children {
		if w == front {
			break
		}
		if w.Bounds.Empty() || encloses(w.Bounds, t.Screen) {
			continue
		}
		if encloses(w.Bounds, front.Bounds) {
			return front
		}
	}
	return nil
}

// windowName is what a window is called in a refusal: its app's, since a
// window's root says nothing of its own.
func windowName(w *Node) string {
	if w.Package == "" {
		return "another window"
	}
	return w.Package + "'s window"
}

// behindFront is the window in front when target lies in another app's
// window, or nil. On Horizon OS a touch on a panel that is not in front is
// not delivered to it: the injection fails and the panel comes forward,
// moved — the browser's Reload, tapped while MobiumApp was in front, raised
// the browser and pressed nothing, and the same tap again then landed.
// The shell's own windows are one app: its home screen took taps while its
// gear's window was in front.
func (t *Tree) behindFront(target *Node) *Node {
	front := t.frontWindow()
	if front == nil {
		return nil
	}
	w := t.windowOf(target)
	if w == nil || t.isBar(w) {
		return nil
	}
	if w.Package != front.Package || t.isKeyboard(w) {
		return front
	}
	return nil
}

// InFront reports whether n is in a window of the app in front, which is
// all a phone's hierarchy ever holds and all a touch can reach: map and
// locators see only that, as on a phone. Every other window stays in the
// tree, where the dialog rule and the covers need it. Without this,
// MobiumApp's Back and the browser's behind it were one locator for two
// nodes, and a check's tap on Back went to the browser's. Always true on a
// hierarchy that is not windowed.
func (t *Tree) InFront(n *Node) bool {
	return t.behindFront(n) == nil
}

func (t *Tree) windowOf(n *Node) *Node {
	if t == nil || t.Root == nil || n == nil {
		return nil
	}
	w := n
	for w.Parent != nil && w.Parent != t.Root {
		w = w.Parent
	}
	if !w.Window {
		return nil
	}
	return w
}

func (t *Tree) inFrontOnly(ns []*Node) []*Node {
	if t == nil || !t.Windowed {
		return ns
	}
	var out []*Node
	for _, n := range ns {
		if t.InFront(n) {
			out = append(out, n)
		}
	}
	return out
}

// WalkFront is Walk over the app in front: on a windowed hierarchy, the
// windows of the app in front alone, as a phone's hierarchy holds. Whatever
// asks the screen for its largest list, its focused field or its bar wants
// these: the largest scrollable on a Quest was the browser's page behind
// Morpho Market's panel, and a swipe there only brought the browser
// forward. Only the dialog rule and the covers want every window, and they
// use Walk. A real headset's keyboard, which may be a window of its own,
// has not been seen: the simulator has no input method at all.
func (t *Tree) WalkFront(fn func(*Node) bool) {
	front := t.frontWindow()
	if front == nil {
		t.Walk(fn)
		return
	}
	for _, w := range t.Root.Children {
		if w.Package != front.Package {
			continue
		}
		if !fn(w) || !walkBelow(w, fn) {
			return
		}
	}
}

// DialogWords is what a dialog or popover says — its texts in order, title
// first, one to a line — leaving out its buttons' captions, or "" when it
// has only buttons.
func DialogWords(d *Node) string {
	var parts []string
	var walk func(n *Node)
	walk = func(n *Node) {
		// A button's words are its caption, not what the dialog says: a
		// platform alert's text is its title and message alone. So is the
		// text a nameless button lies over (coveredSiblingText).
		if n.Clickable || underClickableSibling(n) {
			return
		}
		text := strings.TrimSpace(n.Text)
		if masked, ok := Redact(n); ok {
			text = masked
		}
		if text == "" {
			text = strings.TrimSpace(n.Label)
		}
		if text != "" {
			parts = append(parts, text)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(d)
	return strings.Join(parts, "\n")
}

// underClickableSibling reports whether a clickable beside n, with no text of
// its own, lies wholly over it.
func underClickableSibling(n *Node) bool {
	if n.Parent == nil || n.Bounds.Empty() {
		return false
	}
	for _, sib := range n.Parent.Children {
		if sib != n && sib.Clickable && clean(sib.Text) == "" && clean(sib.Label) == "" &&
			enclosesRect(sib.Bounds, n.Bounds) {
			return true
		}
	}
	return false
}
