package mobiumdriver

import (
	"math"
	"testing"
	"time"
)

// A pinch is two chains that run in lockstep, and the geometry is what makes
// it a scale rather than a drag: both fingers travel the same distance in
// opposite directions, so the midpoint does not move. A pinch that drifts
// scrolls as well as scales, which on a map looks like a bug in the app.
func TestPinchActionsKeepTheMidpointStill(t *testing.T) {
	left, right := pinchActions(500, 900, 80, 320, 600*time.Millisecond)

	startL, startR := left[0], right[0]
	endL, endR := left[3], right[3]

	// Same y throughout: a pinch along the horizontal.
	for _, a := range []map[string]interface{}{startL, startR, endL, endR} {
		if a["y"].(int) != 900 {
			t.Errorf("a finger left y=900: %v", a)
		}
	}
	// Symmetric about the center at both ends.
	if mid := (startL["x"].(int) + startR["x"].(int)) / 2; mid != 500 {
		t.Errorf("fingers start about %d, want 500", mid)
	}
	if mid := (endL["x"].(int) + endR["x"].(int)) / 2; mid != 500 {
		t.Errorf("fingers end about %d, want 500 — the pinch drifted", mid)
	}
	// And they actually moved apart.
	startGap := startR["x"].(int) - startL["x"].(int)
	endGap := endR["x"].(int) - endL["x"].(int)
	if endGap <= startGap {
		t.Errorf("gap went %d -> %d, which is not a zoom in", startGap, endGap)
	}
}

func TestPinchActionsReverseForZoomingOut(t *testing.T) {
	left, right := pinchActions(500, 900, 320, 80, 600*time.Millisecond)
	startGap := right[0]["x"].(int) - left[0]["x"].(int)
	endGap := right[3]["x"].(int) - left[3]["x"].(int)
	if endGap >= startGap {
		t.Errorf("gap went %d -> %d, which is not a zoom out", startGap, endGap)
	}
}

// Both platforms decide what gesture is beginning from the first movement
// after touch-down, and moving in the same frame as the press reads as a fling
// often enough to matter. The pause is load-bearing, not decoration.
func TestPinchPausesBeforeMoving(t *testing.T) {
	left, _ := pinchActions(500, 900, 80, 320, 600*time.Millisecond)
	if left[1]["type"] != "pointerDown" {
		t.Fatalf("second action is %v", left[1]["type"])
	}
	if left[2]["type"] != "pause" {
		t.Errorf("no pause between the press and the move: %v", left[2]["type"])
	}
}

// A rotation must keep both fingers at a constant radius. Moving straight from
// the start angle to the end would drag each along a chord, which passes
// closer to the center than the arc does — the gap between the fingers
// shortens and lengthens again, and the platform reads that as a pinch with
// some rotation attached rather than as a turn.
func TestRotateKeepsBothFingersOnTheCircle(t *testing.T) {
	const cx, cy, r = 500, 900, 200
	left, right := rotateActions(cx, cy, r, 90, 600*time.Millisecond)

	dist := func(a map[string]interface{}) float64 {
		dx := float64(a["x"].(int) - cx)
		dy := float64(a["y"].(int) - cy)
		return math.Hypot(dx, dy)
	}
	for _, chain := range [][]map[string]interface{}{left, right} {
		for i, a := range chain {
			if a["type"] != "pointerMove" {
				continue
			}
			// Integer rounding of the trig costs a pixel or so.
			if d := dist(a); math.Abs(d-r) > 2 {
				t.Errorf("step %d sits %.1f from the center, want %d — the finger left the arc", i, d, r)
			}
		}
	}
}

// And the fingers must stay opposite each other, or the pair is turning about
// something other than the point that was asked for.
func TestRotateKeepsTheFingersOpposed(t *testing.T) {
	const cx, cy = 500, 900
	left, right := rotateActions(cx, cy, 200, 90, 600*time.Millisecond)
	for i := range left {
		if left[i]["type"] != "pointerMove" || right[i]["type"] != "pointerMove" {
			continue
		}
		mx := (left[i]["x"].(int) + right[i]["x"].(int)) / 2
		my := (left[i]["y"].(int) + right[i]["y"].(int)) / 2
		if math.Abs(float64(mx-cx)) > 2 || math.Abs(float64(my-cy)) > 2 {
			t.Errorf("step %d turns about (%d,%d), want (%d,%d)", i, mx, my, cx, cy)
		}
	}
}

// Stepping is the mechanism, so a bigger turn must take more steps — a single
// move to the end is the chord this is all avoiding.
func TestRotateStepsAroundTheArc(t *testing.T) {
	small, _ := rotateActions(500, 900, 200, 20, 600*time.Millisecond)
	big, _ := rotateActions(500, 900, 200, 180, 600*time.Millisecond)
	count := func(chain []map[string]interface{}) int {
		n := 0
		for _, a := range chain {
			if a["type"] == "pointerMove" {
				n++
			}
		}
		return n
	}
	if count(big) <= count(small) {
		t.Errorf("180 degrees took %d moves and 20 took %d", count(big), count(small))
	}
	if count(small) < 4 {
		t.Errorf("a small turn took %d moves; too few and the chords return", count(small))
	}
}
