package mobiumdriver

import (
	"context"
	"math"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// A slider with two thumbs — a price range — is two sliders under the view
// that is its track (uitree.RangeThumb), on Android, where it is Material's
// RangeSlider, and on iOS, where an app draws its own. A thumb is moved by
// dragging it, as a finger does, and the drag is read back from where the
// thumb stopped. What differs between the platforms is measured, and is in
// thumbDrive.

// thumbDrive is what one platform's drag of a thumb needs.
type thumbDrive struct {
	// inset is how far inside the track view a thumb's center stops at
	// either end; slop how far a drag goes before the thumb follows; edge
	// how far from the screen's sides a drag takes hold.
	inset      func(thumb *uitree.Node) int
	slop, edge int
	drag       func(x1, y1, x2, y2 int) error
	snapshot   func(context.Context) (*uitree.Tree, error)
}

// dragThumb carries a two-thumb slider's thumb to position, then reads
// where the thumb stopped — its own bounds are where it is drawn — and
// corrects a drag that fell short with a shorter one. A long drag trailed
// by a step or two: 0.9 of $0–$60 read $52 for $54, where drags of half the
// track or less landed exactly.
func dragThumb(ctx context.Context, n *uitree.Node, position float64, d thumbDrive) error {
	slop := d.slop
	for attempt := 0; attempt < 3; attempt++ {
		track, other, ok := uitree.RangeThumb(n)
		if !ok {
			return mobiumerr.New(mobiumerr.NotConfirmed, "%q is no longer one thumb of a two-thumb slider", n.Label)
		}
		// A view that holds only its two thumbs, framed as the two, says
		// nothing of where the track ends: SwiftUI frames a container as the
		// union of what it holds, so a price range's moved with its thumbs —
		// 15 to 387 points with both at the ends, 30 to 387 with one at $15.
		// An element for the track in it, as Morpho Market's has, holds the
		// frame to the track's.
		if len(n.Parent.Children) == 2 && track == n.Bounds.Union(other.Bounds) {
			return mobiumerr.New(mobiumerr.Unsupported, "%q is one thumb of a slider whose frame is only its "+
				"two thumbs', so where its track ends is not known and a position on it cannot be found", n.Label).
				WithRemedy("drag the thumb with app_swipe from its center along the track, as a finger would; app_map then shows its value")
		}
		x := sliderAlong(track, d.inset(n), position)
		at, _ := n.Bounds.Center()
		if attempt > 0 && abs(at-x) <= max(1, track.Width()/200) {
			return nil
		}
		// Stopped against the other thumb: no drag gets past it, and with
		// the two on one point a hold cannot say which it takes.
		if ox, _ := other.Bounds.Center(); attempt > 0 && abs(at-ox) <= n.Bounds.Width()/2 {
			return nil
		}
		fromX, fromY, err := thumbGrip(n, other, d.edge)
		if err != nil {
			return err
		}
		// A drag moves the thumb only once it has gone the touch slop, and
		// the thumb trails the finger by that much: carried on past the
		// point by the slop, it stops there; an end is carried past the
		// track, where the slider holds it.
		switch {
		case position == 0:
			x = track.X1 - slop
		case position == 1:
			x = track.X2 + slop
		case x > at:
			x += slop
		case x < at:
			x -= slop
		}
		if err := d.drag(fromX, fromY, x, fromY); err != nil {
			return err
		}
		tree, err := d.snapshot(ctx)
		if err != nil {
			return nil // the caller reads it back, and says so
		}
		if n = sameThumb(tree, n); n == nil {
			return nil
		}
	}
	return nil
}

// sameThumb is the thumb in a new read that was thumb in the old: the
// thumb of its name on a track in the same place, give or take a quarter
// of a thumb — the shadow of an iOS app's thumb moved its track's frame by
// a point.
func sameThumb(t *uitree.Tree, thumb *uitree.Node) *uitree.Node {
	var found *uitree.Node
	slack := thumb.Bounds.Width() / 4
	near := func(a, b int) bool { return abs(a-b) <= slack }
	t.Walk(func(c *uitree.Node) bool {
		track, _, ok := uitree.RangeThumb(c)
		if was := thumb.Parent.Bounds; ok && c.Label == thumb.Label && near(track.X1, was.X1) &&
			near(track.X2, was.X2) && near(track.Y1, was.Y1) {
			found = c
			return false
		}
		return true
	})
	return found
}

// rangeTrackInsetDp is how far a RangeSlider's thumbs stop inside the view
// that holds them: at its ends the thumbs' centers were 5.5 pixels in at
// 420 dpi, half Material 3's 4dp thumb.
const rangeTrackInsetDp = 2

// touchSlopDp is Android's touch slop, ViewConfiguration's 8dp, which
// Compose takes as well.
const touchSlopDp = 8

// edgeGestureDp is how far from the screen's sides a drag begins. With
// gesture navigation, a drag that starts at a side is the system's back
// gesture: a full range puts both thumbs there, and dragging either one
// closed the app — the thumbs were at 6–69 and 1011–1074 of 1080 pixels.
// 32dp is past the 24dp edge Android leaves by default.
const edgeGestureDp = 32

// thumbGrip is where a drag of thumb takes hold: its center, moved in from
// the screen's sides by edge. The slider drags the thumb nearer the press,
// so a press beside a thumb carries that one — unless the point moved in is
// nearer the other thumb, which is refused rather than dragging the wrong
// one.
func thumbGrip(thumb, other *uitree.Node, edge int) (int, int, error) {
	x, y := thumb.Bounds.Center()
	screen := thumb
	for screen.Parent != nil {
		screen = screen.Parent
	}
	if w := screen.Bounds.X2; w > 2*edge {
		x = max(screen.Bounds.X1+edge, min(x, w-edge))
	}
	ox, _ := other.Bounds.Center()
	tx, _ := thumb.Bounds.Center()
	if x != tx && abs(x-tx) >= abs(x-ox) {
		return 0, 0, mobiumerr.New(mobiumerr.Unsupported, "%q's thumb is at the screen's edge with the other "+
			"thumb beside it, and a drag that starts at the edge is the system's back gesture", thumb.Label).
			WithRemedy("fill the other thumb first, to move it away")
	}
	return x, y, nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// sliderAlong is the x at position on a track inset at each end.
func sliderAlong(track uitree.Rect, inset int, position float64) int {
	if 4*inset > track.Width() {
		inset = track.Width() / 4
	}
	return track.X1 + inset + int(math.Round(position*float64(track.Width()-2*inset)))
}

// thumbDrag holds a thumb, carries it to a point and lets go, pausing at
// each end so the slider takes the touch as a drag of that thumb rather than
// a tap on its track.
func thumbDrag(x1, y1, x2, y2 int) []map[string]interface{} {
	return []map[string]interface{}{
		{"type": "pointerMove", "duration": 0, "x": x1, "y": y1},
		{"type": "pointerDown", "button": 0},
		{"type": "pause", "duration": 150},
		{"type": "pointerMove", "duration": 800, "x": x2, "y": y2},
		{"type": "pause", "duration": 150},
		{"type": "pointerUp", "button": 0},
	}
}
