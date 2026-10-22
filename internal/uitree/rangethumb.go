package uitree

// RangeThumb reports a node that is one thumb of a slider with two — a
// price range — with the bounds of the view holding both, which is its
// track, and the other thumb.
//
// Both platforms give it one shape: two sliders under one parent, each less
// than half the parent's width. Android's are Material's RangeSlider, two
// seek bars the size of their thumbs; iOS has no such control, and an app
// that draws one — Morpho Market's price range — gives two adjustable
// elements. Measured on the Android 15 emulator and the iPhone 17 Pro
// simulator, 2026-10-22.
func RangeThumb(n *Node) (track Rect, other *Node, ok bool) {
	if n == nil || n.Parent == nil || !isThumb(n) {
		return Rect{}, nil, false
	}
	var thumbs []*Node
	for _, c := range n.Parent.Children {
		if isThumb(c) {
			thumbs = append(thumbs, c)
		}
	}
	p := n.Parent.Bounds
	if len(thumbs) != 2 || 2*n.Bounds.Width() > p.Width() {
		return Rect{}, nil, false
	}
	other = thumbs[0]
	if other == n {
		other = thumbs[1]
	}
	return p, other, true
}

func isThumb(n *Node) bool { return HasClassRole(n, "slider") || HasClassRole(n, "adjustable") }

// Union is the smallest rectangle holding both.
func (r Rect) Union(o Rect) Rect {
	return Rect{X1: min(r.X1, o.X1), Y1: min(r.Y1, o.Y1), X2: max(r.X2, o.X2), Y2: max(r.Y2, o.Y2)}
}
