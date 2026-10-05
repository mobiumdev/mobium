package uitree

import "regexp"

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

// isScrollIndicator reports whether n is a UIKit scroll view's indicator. It
// is listed after the content and lies over the bottom row, 30 points deep —
// "Horizontal scroll bar, 1 page" over MobiumApp's last button — and takes no
// touch, so it is never a cover. Known by its shape rather than its label,
// which is in the device's language: an Other that is not accessible, whose
// value is how far it has scrolled, "0%" to "100%". CHALLENGES 167.
func isScrollIndicator(n *Node) bool {
	return n.Class == "XCUIElementTypeOther" && !n.Clickable && percent.MatchString(n.Text)
}

var percent = regexp.MustCompile(`^\d{1,3}%$`)

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
		if !seen || n.Within(target) || target.Within(n) || !n.Displayed || n.Bounds.Empty() || isScrollIndicator(n) ||
			namesTheSameControl(target, n) {
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

// namesTheSameControl reports a sibling that is the control a target names
// rather than something over it. Flutter on iOS turns Semantics(identifier:)
// into an element of its own — an Other carrying the identifier — followed by
// the button it wraps, in exactly the same frame; a tap on testid=signIn was
// refused as covered by "Sign In", which is the control itself (CHALLENGES
// 206). Only that shape: the target is no control, the sibling has its very
// frame, and a tap at the center lands on the sibling either way.
//
// And a control reported twice. iOS Settings' search field holds two
// Dictate buttons, siblings with one label and one frame; map prints one,
// and the second read as a control over the whole of the first, so a tap on
// it would have been refused as blocked by itself.
//
// And a target's own highlight. iOS 26 draws the selected tab's pill as an
// unlabeled Other with exactly the tab's frame, after it and outside it; a
// tap on Ice Cubes' selected Settings tab, noted as under "Other", went
// through it and took a page inside the tab back to the tab's root.
func namesTheSameControl(target, n *Node) bool {
	if n.Bounds == target.Bounds && !IsControl(n) && n.Label == "" && n.Text == "" && isIOSClass(n.Class) {
		return true
	}
	if target.Parent == nil || n.Parent != target.Parent || n.Bounds != target.Bounds {
		return false
	}
	return !IsControl(target) || n.Class == target.Class && n.Label != "" && n.Label == target.Label
}

// Aim is where to touch a target, and what the tree says is over that point.
type Aim struct {
	X, Y int
	// Moved is set when the target's center was under something and the
	// point was moved to a clear part of the target instead; CenterCover is
	// what was over the center — a control, or, where the rest of the
	// target is clear of it, something that is not (CHALLENGES 238).
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
// for a partly covered element — a button pressed off center is the same
// button. When a control covers every point, Blocker says
// which.
func (t *Tree) AimAt(target *Node) Aim {
	cx, cy := target.Bounds.Center()
	// The keyboard has a refusal of its own, and its keys abut: a key's
	// neighbor reads as a control over its edge.
	if k := t.Keyboard(); k != nil && target.Within(k) {
		return Aim{X: cx, Y: cy}
	}
	if a, ok := t.aimAtPoint(target, cx, cy); ok {
		// Something that is not a control over the center may take the
		// touch or not, and the tree cannot say — but where the rest of the
		// target is clear, there is no need to find out: Pocket Casts' episode
		// sheet draws a two-point divider across its row of buttons, through
		// Play's center, and every tap on Play carried a note that the
		// divider "may take the touch" while it played the episode. Touched
		// a little off center, nothing is over the point, and the result
		// says only that it moved. The note is kept for a cover with no
		// clear point around it. CHALLENGES 238.
		if a.Over != nil {
			if clear, found := t.clearestPoint(target, cx, cy, func(a Aim) bool { return a.Over == nil }); found {
				clear.Moved, clear.CenterCover = true, a.Over
				return clear
			}
		}
		return a
	}
	centerCover := firstControl(t.DrawnOver(target, cx, cy))
	if a, found := t.clearestPoint(target, cx, cy, func(Aim) bool { return true }); found {
		a.Moved, a.CenterCover = true, centerCover
		return a
	}
	return Aim{X: cx, Y: cy, Blocker: centerCover}
}

// clearestPoint is the point of the grid over target nearest (cx, cy) that is
// on the screen, has no control drawn over it, and ok accepts. On the screen:
// below its bottom edge nothing is drawn, so every point there is clear — an
// Ice Cubes post running behind the tab bar was aimed 81 points below the
// screen.
func (t *Tree) clearestPoint(target *Node, cx, cy int, ok func(Aim) bool) (Aim, bool) {
	screen := t.Screen
	if screen.Empty() && t.Root != nil {
		screen = t.Root.Bounds
	}
	b := target.Bounds
	best, bestDist := Aim{}, -1
	for r := 0; r < aimRows; r++ {
		for c := 0; c < aimCols; c++ {
			x := b.X1 + (2*c+1)*b.Width()/(2*aimCols)
			y := b.Y1 + (2*r+1)*b.Height()/(2*aimRows)
			if !screen.Empty() && !contains(screen, x, y) {
				continue
			}
			a, free := t.aimAtPoint(target, x, y)
			if !free || !ok(a) {
				continue
			}
			if d := (x-cx)*(x-cx) + (y-cy)*(y-cy); bestDist < 0 || d < bestDist {
				best, bestDist = a, d
			}
		}
	}
	return best, bestDist >= 0
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
	// Nor is anything that spans the whole screen, whatever the target's
	// size: the size rule let one through over a target more than half the
	// screen high — Pocket Casts' podcast header, under the layer that holds
	// iOS 26's floating tab bar.
	screen := t.Screen
	if screen.Empty() && t.Root != nil {
		screen = t.Root.Bounds
	}
	for i := len(over) - 1; i >= 0 && !target.Scrollable; i-- {
		if !screen.Empty() && enclosesRect(over[i].Bounds, screen) {
			continue
		}
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
