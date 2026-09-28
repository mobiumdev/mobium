package uitree

// What the app has drawn over a target. A dialog and the keyboard are found
// elsewhere, each by its own evidence; this is the app's own view laid over
// another — a scrim, a toast, a floating button — which both platforms leave
// in the tree, marked visible, while a tap on the target lands on the cover.
//
// Measured on MobiumApp's Obstruction Demo, on a Pixel 7 AVD and an iPhone 17
// Pro simulator (CHALLENGES 115). Three things decide the shape of this file:
//
//   - A cover is a later node in document order whose bounds contain the
//     point. Document order is drawing order in both trees: Android's
//     `drawing-order` attribute agrees with it on every case measured.
//   - Neither tree can say whether a cover takes touches. A view with
//     `pointerEvents="none"` and a plain view with no handler are identical
//     attribute for attribute on iOS, and differ only by position on
//     Android — yet one lets the tap through and the other swallows it. So
//     a cover is only known to take the tap when it is itself a control.
//   - iOS's `visible` is visual, not touchable: it marks a target under a
//     pass-through view not visible, and one whose center is covered visible.
//     It is not consulted here.

// iosControlTypes are the XCUIElementTypes that take a tap themselves. On iOS
// `Clickable` also holds for an accessible XCUIElementTypeOther, which is what
// a plain or a pass-through view reports, so it cannot say whether a cover is
// a control; the element's type can.
var iosControlTypes = map[string]bool{
	"XCUIElementTypeButton":           true,
	"XCUIElementTypeLink":             true,
	"XCUIElementTypeCell":             true,
	"XCUIElementTypeSwitch":           true,
	"XCUIElementTypeToggle":           true,
	"XCUIElementTypeCheckBox":         true,
	"XCUIElementTypeRadioButton":      true,
	"XCUIElementTypeSlider":           true,
	"XCUIElementTypeStepper":          true,
	"XCUIElementTypeSegmentedControl": true,
	"XCUIElementTypeTextField":        true,
	"XCUIElementTypeSecureTextField":  true,
	"XCUIElementTypeTextView":         true,
	"XCUIElementTypeSearchField":      true,
	"XCUIElementTypeTab":              true,
	"XCUIElementTypeMenuItem":         true,
	"XCUIElementTypeKey":              true,
	"XCUIElementTypeIcon":             true,
}

// IsControl reports whether a cover is itself something a tap would press:
// on Android the platform's own clickable flags, on iOS the element's type.
// A cover that is not a control may or may not take a touch, and nothing in
// either tree says which.
func IsControl(n *Node) bool {
	if n == nil {
		return false
	}
	if IsNotificationBanner(n) {
		return true
	}
	if isIOSClass(n.Class) {
		return iosControlTypes[n.Class]
	}
	return n.Clickable || n.LongClickable
}

func isIOSClass(class string) bool {
	return len(class) > len("XCUIElementType") && class[:len("XCUIElementType")] == "XCUIElementType"
}

// DrawnOver returns what the tree says is drawn over point (x, y) of target:
// nodes after it in document order whose bounds contain the point, other than
// the target, its ancestors and its descendants. Only the outermost of a
// nested set is returned — a cover's own label is part of the cover.
func (t *Tree) DrawnOver(target *Node, x, y int) []*Node {
	if t == nil || target == nil {
		return nil
	}
	var over []*Node
	seen := false
	t.Walk(func(n *Node) bool {
		if n == target {
			seen = true
			return true
		}
		if !seen || n.Within(target) || target.Within(n) || !n.Displayed || n.Bounds.Empty() {
			return true
		}
		for _, o := range over {
			if n.Within(o) {
				return true
			}
		}
		if contains(n.Bounds, x, y) {
			over = append(over, n)
		}
		return true
	})
	return over
}

// Aim is where to touch a target, and what the tree says is over that point.
type Aim struct {
	X, Y int
	// Moved is set when the target's center was under a control and the
	// point was moved to a clear part of the target instead; CenterCover is
	// that control.
	Moved       bool
	CenterCover *Node
	// Blocker is a control drawn over the whole of the target, so there is no
	// point a tap would reach it by. Nil when the target can be reached.
	Blocker *Node
	// Over is something drawn over the chosen point that is not a control.
	// It may take the tap or let it through, and the tree cannot say which,
	// so it is reported rather than refused.
	Over *Node
}

// aimGrid is how finely a target is searched for a clear point when its
// center is covered: the centers of a 9 by 5 grid over its bounds.
const aimCols, aimRows = 9, 5

// AimAt decides where to touch target so a tap reaches it. The center, when
// no control is drawn over it; otherwise the clear point nearest the center,
// as EarlGrey does for a partly covered element — a button pressed off
// center is the same button. When a control covers every point, Blocker says
// which.
func (t *Tree) AimAt(target *Node) Aim {
	cx, cy := target.Bounds.Center()
	// The keyboard has a refusal of its own, and its keys abut: a key's
	// neighbor reads as a control over its edge.
	if k := t.Keyboard(); k != nil && target.Within(k) {
		return Aim{X: cx, Y: cy}
	}
	if a, ok := t.aimAtPoint(target, cx, cy); ok {
		return a
	}
	centerCover := firstControl(t.DrawnOver(target, cx, cy))
	b := target.Bounds
	best, bestDist := Aim{}, -1
	for r := 0; r < aimRows; r++ {
		for c := 0; c < aimCols; c++ {
			x := b.X1 + (2*c+1)*b.Width()/(2*aimCols)
			y := b.Y1 + (2*r+1)*b.Height()/(2*aimRows)
			a, ok := t.aimAtPoint(target, x, y)
			if !ok {
				continue
			}
			if d := (x-cx)*(x-cx) + (y-cy)*(y-cy); bestDist < 0 || d < bestDist {
				a.Moved, a.CenterCover = true, centerCover
				best, bestDist = a, d
			}
		}
	}
	if bestDist >= 0 {
		return best
	}
	return Aim{X: cx, Y: cy, Blocker: centerCover}
}

// aimAtPoint reports whether (x, y) is free of any control drawn over it, and
// what else is over it if so.
func (t *Tree) aimAtPoint(target *Node, x, y int) (Aim, bool) {
	over := t.DrawnOver(target, x, y)
	if k := t.Keyboard(); k != nil {
		over = without(over, k)
	}
	if firstControl(over) != nil {
		return Aim{}, false
	}
	a := Aim{X: x, Y: y}
	// Something that is not a control is reported only when it is local to
	// the target — inside the target's parent, as a cover laid over one
	// button is. A full-screen one is indistinguishable from the transparent
	// layers both platforms put over everything: a later XCUIElementTypeWindow
	// or Other on iOS, the launcher's drag layer on Android, over nearly
	// every target on every captured screen. Reporting those would bury the
	// one note that means something.
	// It must also be about the target's size: every note left on a captured
	// screen after the first rule was a container over a container.
	// And a scroll container is not reported on: what lies over its center is
	// its own content, and tapping a list's middle is rarely the point.
	for i := len(over) - 1; i >= 0 && !target.Scrollable; i-- {
		if target.Parent != nil && enclosesRect(target.Parent.Bounds, over[i].Bounds) &&
			area(over[i].Bounds) <= 2*area(target.Bounds) {
			a.Over = over[i]
			break
		}
	}
	return a, true
}

func firstControl(nodes []*Node) *Node {
	for _, n := range nodes {
		if IsControl(n) {
			return n
		}
	}
	return nil
}

func without(nodes []*Node, anc *Node) []*Node {
	var out []*Node
	for _, n := range nodes {
		if !n.Within(anc) {
			out = append(out, n)
		}
	}
	return out
}

func area(r Rect) int { return r.Width() * r.Height() }

func enclosesRect(outer, inner Rect) bool {
	return inner.X1 >= outer.X1 && inner.Y1 >= outer.Y1 && inner.X2 <= outer.X2 && inner.Y2 <= outer.Y2
}

func contains(r Rect, x, y int) bool {
	return x >= r.X1 && x < r.X2 && y >= r.Y1 && y < r.Y2
}

// Describe names a node the way map does — cleaned, bounded, a password
// field's value never shown — falling back to its class, since an overlay
// often carries no name at all.
func Describe(n *Node) string {
	if d := describe(n); d != "" {
		return d
	}
	return n.ShortClass()
}

// IsNotificationBanner reports an iOS notification banner, laid over an app
// by the driver while SpringBoard shows one (CHALLENGES 155). SpringBoard
// types it Other, but it is a control: a tap on it opens the notification,
// and a tap meant for what it covers lands on it.
func IsNotificationBanner(n *Node) bool {
	return n != nil && n.TestID == "NotificationShortLookView"
}
