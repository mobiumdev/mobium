package uitree

// HugeScreen is how many elements make a screen too large to read with
// visibility on iOS. WebDriverAgent works visible out for every element, and
// a long table reports every row whether it is on screen or not: Pocket
// Casts' Radiolab page, 673 episodes, was 2,847 elements, read in 83.8s with
// visible and 6.3s without on an iPhone 15 Plus — past the 60s a read is
// given. The largest of the captured screens before it had 393.
const HugeScreen = 1000

// InferVisibility marks shown and hidden from geometry, for a read made
// without the platform's visibility: an element outside the screen is
// hidden, and so is everything inside one, whatever its own bounds say — the
// texts and views of an off-screen row of that same page report themselves
// near the top of the screen. On that page this agreed with iOS on 2,820 of
// 2,847 elements and called nothing hidden that iOS called shown. The 27 it
// missed were all covered — under the mini player and the tab bar — which
// geometry cannot see, so a tree read this way is marked, and what is
// printed from it says so. CHALLENGES 258.
func (t *Tree) InferVisibility() {
	if t == nil || t.Root == nil {
		return
	}
	screen := t.Screen
	if screen.Empty() {
		screen = t.Root.Bounds
	}
	if screen.Empty() && len(t.Root.Children) > 0 {
		screen = t.Root.Children[0].Bounds
	}
	if screen.Empty() {
		return
	}
	var walk func(n *Node, off bool)
	walk = func(n *Node, off bool) {
		if !off && !n.Bounds.Empty() && !overlaps(n.Bounds, screen) {
			off = true
		}
		if off {
			n.Displayed = false
			n.Clickable = false
		}
		for _, c := range n.Children {
			walk(c, off)
		}
	}
	walk(t.Root, false)
	t.VisibilityInferred = true
}

// Count is how many elements a tree holds, its root aside.
func (t *Tree) Count() int {
	c := 0
	t.Walk(func(*Node) bool { c++; return true })
	return c
}
