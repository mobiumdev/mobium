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
	// maxGain is the most a drag here may magnify a thumb's travel before
	// one has been measured: 1 on a phone, more where a drag was seen to
	// carry a thumb further than the finger (Horizon OS).
	maxGain  float64
	drag     func(x1, y1, x2, y2 int) error
	snapshot func(context.Context) (*uitree.Tree, error)
}

// dragThumb carries a two-thumb slider's thumb to position, then reads
// where the thumb stopped — its own bounds are where it is drawn — and
// corrects a drag that missed with another. A long drag trailed by a step
// or two: 0.9 of $0–$60 read $52 for $54, where drags of half the track or
// less landed exactly.
//
// A drag does not always move a thumb as far as the finger. On Horizon OS a
// Compose app's window is drawn 2.48 times its size and its drags were
// magnified by about that, finger moves of 159 and 309 pixels taking a thumb
// $15 and $34 along a $60 track 1,155 pixels long; a React Native app's drag
// on the same panel landed exactly (CHALLENGES 367). So nothing assumes a
// gain: each drag that moved the thumb without stopping at a stop teaches the
// next how far the thumb goes per pixel of finger. A thumb that does not end
// at the position is refused, never reported as moved.
func dragThumb(ctx context.Context, n *uitree.Node, position float64, d thumbDrive) error {
	const drags = 5
	slop := d.slop
	fit := dragFit{gain: 1, lag: float64(slop), slop: float64(slop)}
	for attempt := 0; ; attempt++ {
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
		inset := d.inset(n)
		x := sliderAlong(track, inset, position)
		at, _ := n.Bounds.Center()
		ox, _ := other.Bounds.Center()
		tol := max(1, track.Width()/200)
		if attempt > 0 {
			if abs(at-x) <= tol {
				return nil
			}
			// Stopped against the other thumb, sent to a point at or past
			// it: no drag gets past it, and with the two on one point a hold
			// cannot say which it takes. Sent to a point short of it, the
			// drag went too far, and the next is shorter.
			if abs(at-ox) <= n.Bounds.Width()/2 && pastOther(n, other, x, ox, tol) {
				return nil
			}
			if attempt == drags {
				return mobiumerr.New(mobiumerr.NotConfirmed, "%q stopped at %.2f of its track after %d drags, "+
					"not at %g: a drag on this screen moves the thumb %.1f times as far as the finger, and "+
					"no drag landed it", n.Label, along(track, inset, at), drags, position, fit.gain).
					WithRemedy("read the slider's value in app_map; app_fill again from where it stopped, or drag " +
						"the thumb with app_swipe, as a finger would")
			}
		}
		fromX, fromY, err := thumbGrip(n, other, d.edge)
		if err != nil {
			return err
		}
		// The thumb goes fit.gain times the finger's travel, less a fixed
		// fit.lag the slop takes, in the drag's direction; an end is carried
		// past the track, where the slider holds it.
		// The slider brings the thumb to the press, so the drag is planned
		// from where it takes hold, which is moved in from a screen's sides.
		need := float64(x - fromX)
		want := fit.travel(need)
		// Until a drag has said what the gain is, one toward the other thumb
		// stops where the most gain expected would still leave the two apart:
		// a Compose thumb carried onto the other on a Horizon OS panel hid
		// it, and with one thumb in the tree the slider was no longer one.
		if g := d.maxGain; g > 1 && !fit.learned() && (ox-at)*(x-at) > 0 && position != 0 && position != 1 {
			room := float64(abs(ox-fromX)-n.Bounds.Width()) / g
			want = math.Copysign(max(0, min(math.Abs(want), room)), want)
		}
		to := fromX + int(math.Round(want))
		switch {
		case position == 0:
			to = min(to, track.X1-slop)
		case position == 1:
			to = max(to, track.X2+slop)
		}
		if err := d.drag(fromX, fromY, to, fromY); err != nil {
			return err
		}
		tree, err := d.snapshot(ctx)
		if err != nil {
			return nil // the caller reads it back, and says so
		}
		moved := sameThumb(tree, n)
		if moved == nil {
			return mobiumerr.New(mobiumerr.NotConfirmed, "after a drag, %q could no longer be read as one thumb "+
				"of a two-thumb slider, so where it stopped is not known", n.Label).
				WithRemedy("read the slider in app_map; if the two thumbs met, fill the other one first to move it away")
		}
		// What this drag says of the gain, unless the thumb stopped at an
		// end or at the other thumb, which hold it however far the finger
		// went past: there it says only that the gain is at least that.
		now, _ := moved.Bounds.Center()
		stopped := now <= track.X1+inset+tol || now >= track.X2-inset-tol || abs(now-ox) <= tol
		fit.add(float64(to-fromX), float64(now-fromX), stopped)
		n = moved
	}
}

// dragFit is how a thumb follows a finger, learned from the drags so far:
// it goes gain times the finger's travel, less lag in the drag's direction —
// the touch slop, which on a phone is Android's 8dp at a gain of 1; less
// than none where the thumb runs ahead of the finger. On a
// Horizon OS panel neither held: the gain was about 2.4 and the lag not the
// slop Mobium computes, so corrections made with a gain alone stopped one
// to three $1 steps off, every time.
type dragFit struct {
	gain, lag float64
	slop      float64      // the slop assumed, which bounds the lag fitted
	obs       [][2]float64 // finger travel, thumb travel, from drags that stopped at no stop
}

func (f *dragFit) learned() bool { return len(f.obs) > 0 }

// travel is the finger's travel that takes the thumb need.
func (f *dragFit) travel(need float64) float64 {
	if need == 0 {
		return 0
	}
	// A lag that runs ahead never plans away a correction altogether: a
	// short one is the need at the gain.
	t := math.Abs(need)/f.gain + f.lag
	if t < 1 {
		t = math.Abs(need) / f.gain
	}
	return math.Copysign(max(1, t), need)
}

// add learns from a drag of the finger that moved the thumb moved. A drag
// held at a stop says only that the gain is at least what it shows.
func (f *dragFit) add(finger, moved float64, stopped bool) {
	// A drag that moved the thumb nothing did not get past the slop.
	if moved == 0 && finger != 0 {
		f.lag = max(f.lag, math.Abs(finger)+1)
		return
	}
	if math.Abs(finger) <= f.lag || finger*moved <= 0 {
		return
	}
	if stopped {
		if g := math.Abs(moved) / (math.Abs(finger) - f.lag); !f.learned() && g > f.gain {
			f.gain = min(4, g)
		}
		return
	}
	f.obs = append(f.obs, [2]float64{math.Abs(finger), math.Abs(moved)})
	if len(f.obs) == 1 {
		f.gain = max(0.25, min(4, f.obs[0][1]/(f.obs[0][0]-f.lag)))
		return
	}
	// moved = gain*finger - gain*lag, by least squares over every drag.
	var n, sx, sy, sxx, sxy float64
	for _, o := range f.obs {
		n++
		sx += o[0]
		sy += o[1]
		sxx += o[0] * o[0]
		sxy += o[0] * o[1]
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return
	}
	g := (n*sxy - sx*sy) / den
	b := (sy - g*sx) / n
	if g < 0.25 || g > 4 {
		return
	}
	// Two long drags with a step's rounding in each can put the line's
	// intercept anywhere; a lag past these bounds took the next drag to no
	// travel at all.
	f.gain, f.lag = g, max(-f.slop, min(4*f.slop, -b/g))
}

// pastOther is whether x is at or past the other thumb, seen from thumb's
// side of it: the first of the two under the track is the lower.
func pastOther(thumb, other *uitree.Node, x, ox, tol int) bool {
	for _, c := range thumb.Parent.Children {
		switch c {
		case thumb:
			return x >= ox-tol
		case other:
			return x <= ox+tol
		}
	}
	return false
}

// along is where x is on a track inset at each end, from 0 to 1.
func along(track uitree.Rect, inset, x int) float64 {
	if 4*inset > track.Width() {
		inset = track.Width() / 4
	}
	w := track.Width() - 2*inset
	if w <= 0 {
		return 0
	}
	return max(0, min(1, float64(x-track.X1-inset)/float64(w)))
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
