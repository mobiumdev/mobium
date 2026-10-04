package uitree

// Viewport is the part of a scroll container a person can see and touch:
// its bounds, less any bar drawn over its top or bottom edge, and less what
// runs past the edge of the screen.
//
// iOS 26 runs a list the full height of the screen, under its navigation
// bar and toolbar, which are drawn over it. A row behind the toolbar is
// inside the list's bounds and so was taken as in view: NetNewsWire's feed
// list put "NetNewsWire Blog" there, and a tap on it touched the toolbar
// between its buttons and was reported done (CHALLENGES 197). Judged
// against the viewport, the row is out of view, and the scroll that brings
// a row in from below the list's end brings it out from under the bar.
//
// A bar is drawn after the container, outside it, as wide as it, no more
// than a third of its height, across its top or bottom edge, and it holds a
// control — what makes it a bar rather than the transparent full-screen
// layers both platforms lay over everything. Android draws its bars beside
// a list rather than over it, so there the viewport is the bounds.
func (t *Tree) Viewport(c *Node) Rect {
	if t == nil || c == nil {
		return Rect{}
	}
	v := c.Bounds
	if v.Empty() {
		return v
	}
	seen := false
	t.Walk(func(n *Node) bool {
		if n == c {
			seen = true
			return true
		}
		if !seen || n.Within(c) || c.Within(n) || !n.Displayed || !isBar(n, c) {
			return true
		}
		b := n.Bounds
		switch {
		case b.Y1 <= c.Bounds.Y1 && b.Y2 > v.Y1:
			v.Y1 = b.Y2
		case b.Y2 >= c.Bounds.Y2 && b.Y1 < v.Y2:
			v.Y2 = b.Y1
		}
		return true
	})
	if v.Y2 <= v.Y1 {
		v = c.Bounds
	}
	return t.onScreen(v)
}

// onScreen is r less whatever of it runs past the screen. A container can
// be taller than the screen it is on: the menu a long press on an Ice Cubes
// post opens is a list from y 487 to 1362 points on an 874-point screen,
// and the rows past the bottom edge were taken as in view — scroll-to
// answered "on screen already" for an item nobody could see or touch, and
// map, which asks the platform, did not list it. Unclipped when the two do
// not meet, so a container wholly off screen keeps its own bounds.
func (t *Tree) onScreen(r Rect) Rect {
	var screen Rect
	t.Walk(func(n *Node) bool {
		if !n.Bounds.Empty() {
			screen = n.Bounds
			return false
		}
		return true
	})
	if screen.Empty() {
		return r
	}
	c := Rect{X1: max(r.X1, screen.X1), Y1: max(r.Y1, screen.Y1), X2: min(r.X2, screen.X2), Y2: min(r.Y2, screen.Y2)}
	if c.Empty() {
		return r
	}
	return c
}

// isBar reports a node laid across the top or bottom edge of c, as wide as
// c, at most a third of its height, with a control in it.
func isBar(n, c *Node) bool {
	b, cb := n.Bounds, c.Bounds
	if b.Empty() || b.X1 > cb.X1 || b.X2 < cb.X2 || 3*b.Height() > cb.Height() {
		return false
	}
	acrossTop := b.Y1 <= cb.Y1 && b.Y2 > cb.Y1
	acrossBottom := b.Y2 >= cb.Y2 && b.Y1 < cb.Y2
	if !acrossTop && !acrossBottom {
		return false
	}
	return holdsControl(n)
}

func holdsControl(n *Node) bool {
	for _, c := range n.Children {
		if c.Displayed && IsControl(c) || holdsControl(c) {
			return true
		}
	}
	return false
}
