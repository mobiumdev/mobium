package mobiumdriver

import (
	"fmt"
	"testing"
	"time"
)

// stepMS is how long one action lasts; down and up are instants.
func stepMS(a action) int64 {
	if d, ok := a["duration"].(int64); ok {
		return d
	}
	return 0
}

// misaligned says what breaks the promise in multitouch.go — equal step counts
// and equal step durations in every chain — or "" when nothing does. Without
// it UiAutomator2 and WebDriverAgent would place the second finger at
// different moments.
func misaligned(chains [][]action) string {
	for i, c := range chains[1:] {
		if len(c) != len(chains[0]) {
			return fmt.Sprintf("finger %d has %d steps, finger 1 has %d", i+2, len(c), len(chains[0]))
		}
	}
	for step := range chains[0] {
		want := stepMS(chains[0][step])
		for i, c := range chains[1:] {
			if got := stepMS(c[step]); got != want {
				return fmt.Sprintf("step %d lasts %dms for finger %d and %dms for finger 1", step, got, i+2, want)
			}
		}
	}
	return ""
}

func assertAligned(t *testing.T, name string, chains [][]action) {
	t.Helper()
	if why := misaligned(chains); why != "" {
		t.Errorf("%s: %s", name, why)
	}
}

// The control: two chains equal in total and not step for step — the exact
// mistake the rule exists for — must be caught.
func TestMisalignedChainsAreCaught(t *testing.T) {
	a := []action{down(), pause(100 * time.Millisecond), pause(0), up()}
	b := []action{down(), pause(0), pause(100 * time.Millisecond), up()}
	if misaligned([][]action{a, b}) == "" {
		t.Error("chains equal in total but not step for step passed as aligned")
	}
	if misaligned([][]action{a, a[:3]}) == "" {
		t.Error("chains of different lengths passed as aligned")
	}
}

// at returns the time each finger goes down and comes up, from the start.
func at(chain []action) (downMS, upMS int64) {
	var now int64
	for _, a := range chain {
		switch a["type"] {
		case "pointerDown":
			downMS = now
		case "pointerUp":
			upMS = now
		}
		now += stepMS(a)
	}
	return
}

func TestMultiTouchChainsAreAligned(t *testing.T) {
	p := func(x, y int) Point { return Point{x, y} }
	assertAligned(t, "multi-tap", multiTapActions([]Point{p(1, 1), p(2, 2), p(3, 3)}))
	assertAligned(t, "press-tap", pressTapActions(p(1, 1), p(2, 2), 300*time.Millisecond))
	assertAligned(t, "press-drag", pressDragActions(p(1, 1), p(2, 2), p(3, 3), 300*time.Millisecond, 500*time.Millisecond))
}

// Press and tap is only itself if the second finger lands after the first,
// lifts before it, and never while the first is up.
func TestPressTapHoldsAcrossTheTap(t *testing.T) {
	c := pressTapActions(Point{1, 1}, Point{2, 2}, 300*time.Millisecond)
	d1, u1 := at(c[0])
	d2, u2 := at(c[1])
	if !(d1 < d2 && d2 < u2 && u2 < u1) {
		t.Errorf("want first down < second down < second up < first up, got %d %d %d %d", d1, d2, u2, u1)
	}
	if d2-d1 != 300 {
		t.Errorf("second finger landed %dms after the first, want the lead of 300", d2-d1)
	}
}

func TestPressDragHoldsAcrossTheDrag(t *testing.T) {
	c := pressDragActions(Point{1, 1}, Point{2, 2}, Point{9, 9}, 300*time.Millisecond, 500*time.Millisecond)
	d1, u1 := at(c[0])
	d2, u2 := at(c[1])
	if !(d1 < d2 && d2 < u2 && u2 < u1) {
		t.Errorf("want first down < second down < second up < first up, got %d %d %d %d", d1, d2, u2, u1)
	}
	var moved bool
	for _, a := range c[1] {
		if a["type"] == "pointerMove" && a["x"] == 9 && stepMS(a) == 500 {
			moved = true
		}
	}
	if !moved {
		t.Error("the second finger never travels to the end point over the move duration")
	}
}

func TestMultiTapLandsTogether(t *testing.T) {
	c := multiTapActions([]Point{{1, 1}, {2, 2}, {3, 3}})
	d0, u0 := at(c[0])
	for i, ch := range c[1:] {
		if d, u := at(ch); d != d0 || u != u0 {
			t.Errorf("finger %d down %d up %d, finger 1 down %d up %d", i+2, d, u, d0, u0)
		}
	}
}
