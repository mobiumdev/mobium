package agent

import (
	"fmt"
	"strings"
	"time"
)

// map --diff: what changed on the screen since the last map of it. An agent
// that has just acted needs to know what the action did — what appeared,
// what went away, what changed state — and a whole new map buries that in
// everything that stayed put.
//
// Refs are renumbered by every map, so elements are paired across the two by
// what does not renumber, in order of trust: a locator that names the element
// itself (a test id or an accessibility label), then the same role and label,
// then the same role in nearly the same place, which is how a relabeled
// element is recognized. What pairs and differs is changed; what does not
// pair appeared or went away.

// lastMap is a device's most recent native map, what --diff compares with.
type lastMap struct {
	elements []ElementView
	taken    time.Time
}

// MapDiffView is what changed since the last map.
type MapDiffView struct {
	// First says there was no earlier map on this device to compare with;
	// then every element is in Added.
	First   bool          `json:"first,omitempty"`
	Since   time.Time     `json:"since,omitempty"`
	Added   []ElementView `json:"added"`
	Removed []ElementView `json:"removed"`
	Changed []ChangeView  `json:"changed"`
}

// ChangeView is one element in both maps that differs between them.
type ChangeView struct {
	Before ElementView `json:"before"`
	After  ElementView `json:"after"`
	// What names each difference: "label", "checked", "selected", "moved"
	// — the same size somewhere else — or "resized".
	What []string `json:"what"`
}

// movedBy is how far, in pixels, an element's center must move to be
// reported as moved: less is a relayout's rounding, not a move.
const movedBy = 8

// nearlyPlaced is how close two elements' centers must be to pair them as
// one element relabeled.
const nearlyPlaced = 16

func diffMaps(before, after []ElementView) MapDiffView {
	d := MapDiffView{Added: []ElementView{}, Removed: []ElementView{}, Changed: []ChangeView{}}
	pairedB := make([]bool, len(before))
	pairedA := make([]bool, len(after))
	pairs := map[int]int{} // after index -> before index

	pass := func(match func(b, a ElementView) bool) {
		for ai, a := range after {
			if pairedA[ai] {
				continue
			}
			for bi, b := range before {
				if !pairedB[bi] && match(b, a) {
					pairedA[ai], pairedB[bi] = true, true
					pairs[ai] = bi
					break
				}
			}
		}
	}
	pass(func(b, a ElementView) bool {
		k := stableKey(a)
		return k != "" && k == stableKey(b)
	})
	pass(func(b, a ElementView) bool { return b.Role == a.Role && b.Label == a.Label })
	pass(func(b, a ElementView) bool { return b.Role == a.Role && distance(b.Bounds, a.Bounds) <= nearlyPlaced })

	for ai, a := range after {
		bi, ok := pairs[ai]
		if !ok {
			d.Added = append(d.Added, a)
			continue
		}
		b := before[bi]
		var what []string
		if b.Label != a.Label {
			what = append(what, "label")
		}
		if (b.Checked == nil) != (a.Checked == nil) || (b.Checked != nil && *b.Checked != *a.Checked) {
			what = append(what, "checked")
		}
		if b.Selected != a.Selected {
			what = append(what, "selected")
		}
		// A row that grows moves its center too; that is a resize, and only
		// an element the same size somewhere else has moved. Measured on
		// MobiumApp's Form Demo on iOS, where a checked box's row grows 18
		// pixels and shifts everything under it.
		if sizeChanged(b.Bounds, a.Bounds) {
			what = append(what, "resized")
		} else if distance(b.Bounds, a.Bounds) > movedBy {
			what = append(what, "moved")
		}
		if len(what) > 0 {
			d.Changed = append(d.Changed, ChangeView{Before: b, After: a, What: what})
		}
	}
	for bi, b := range before {
		if !pairedB[bi] {
			d.Removed = append(d.Removed, b)
		}
	}
	return d
}

// stableKey is an element's locator when that names the element itself
// rather than where it sits or what it says, which a change can alter.
func stableKey(e ElementView) string {
	if e.Locator == nil {
		return ""
	}
	switch e.Locator.Kind {
	case "testid", "label":
		return e.Locator.Kind + "=" + e.Locator.Value
	}
	return ""
}

func center(b BoundsView) (int, int) { return (b.X1 + b.X2) / 2, (b.Y1 + b.Y2) / 2 }

func sizeChanged(a, b BoundsView) bool {
	abs := func(v int) int {
		if v < 0 {
			return -v
		}
		return v
	}
	return abs((a.X2-a.X1)-(b.X2-b.X1)) > movedBy || abs((a.Y2-a.Y1)-(b.Y2-b.Y1)) > movedBy
}

func distance(a, b BoundsView) int {
	ax, ay := center(a)
	bx, by := center(b)
	dx, dy := ax-bx, ay-by
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

// elementLine is map's line for an element: "@e2 Accept terms (checkbox,
// checked)", or without its ref for one that is gone and has none.
func elementLine(e ElementView, withRef bool) string {
	var tags []string
	if e.Role != "" {
		tags = append(tags, e.Role)
	}
	if e.Checked != nil {
		tags = append(tags, stateWord(*e.Checked))
	}
	if e.Selected {
		tags = append(tags, "selected")
	}
	s := e.Label
	if len(tags) > 0 {
		s += " (" + strings.Join(tags, ", ") + ")"
	}
	if withRef {
		s = e.Ref + " " + s
	}
	return s
}

// direction says which way a shared move went, in pixels.
func direction(dx, dy int) string {
	var parts []string
	switch {
	case dy > 0:
		parts = append(parts, fmt.Sprintf("down %dpx", dy))
	case dy < 0:
		parts = append(parts, fmt.Sprintf("up %dpx", -dy))
	}
	switch {
	case dx > 0:
		parts = append(parts, fmt.Sprintf("right %dpx", dx))
	case dx < 0:
		parts = append(parts, fmt.Sprintf("left %dpx", -dx))
	}
	return strings.Join(parts, " and ")
}

// diffText is --diff's answer: a line per difference, or that there was none.
func diffText(d MapDiffView, total int) string {
	var lines []string
	for _, e := range d.Removed {
		lines = append(lines, "- "+elementLine(e, false))
	}
	for _, e := range d.Added {
		lines = append(lines, "+ "+elementLine(e, true))
	}
	// Elements that only moved, all by the same amount, are one line: a
	// layout that shifted, not so many changes.
	type delta struct{ dx, dy int }
	shifted := map[delta][]ChangeView{}
	var order []delta
	for _, c := range d.Changed {
		if len(c.What) == 1 && c.What[0] == "moved" {
			bx, by := center(c.Before.Bounds)
			ax, ay := center(c.After.Bounds)
			k := delta{ax - bx, ay - by}
			if _, seen := shifted[k]; !seen {
				order = append(order, k)
			}
			shifted[k] = append(shifted[k], c)
		}
	}
	for _, c := range d.Changed {
		if len(c.What) == 1 && c.What[0] == "moved" {
			bx, by := center(c.Before.Bounds)
			ax, ay := center(c.After.Bounds)
			if len(shifted[delta{ax - bx, ay - by}]) > 1 {
				continue
			}
		}
		var was []string
		for _, w := range c.What {
			switch w {
			case "label":
				was = append(was, fmt.Sprintf("was %q", c.Before.Label))
			case "checked":
				if c.Before.Checked != nil {
					was = append(was, "was "+stateWord(*c.Before.Checked))
				}
			case "selected":
				if c.Before.Selected {
					was = append(was, "was selected")
				} else {
					was = append(was, "was not selected")
				}
			case "moved":
				x, y := center(c.Before.Bounds)
				was = append(was, fmt.Sprintf("moved from (%d, %d)", x, y))
			case "resized":
				b := c.Before.Bounds
				was = append(was, fmt.Sprintf("resized from %dx%d", b.X2-b.X1, b.Y2-b.Y1))
			}
		}
		lines = append(lines, "~ "+elementLine(c.After, true)+" — "+strings.Join(was, ", "))
	}
	for _, k := range order {
		group := shifted[k]
		if len(group) < 2 {
			continue
		}
		var labels []string
		for _, c := range group {
			labels = append(labels, c.After.Ref+" "+c.After.Label)
		}
		lines = append(lines, fmt.Sprintf("~ %d elements moved together, %s: %s", len(group), direction(k.dx, k.dy),
			strings.Join(labels, ", ")))
	}
	if len(lines) == 0 {
		return fmt.Sprintf("nothing changed since the last map: the same %d elements, in the same places", total)
	}
	return strings.Join(lines, "\n")
}
