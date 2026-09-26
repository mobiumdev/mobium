package mobiumdriver

import (
	"testing"
	"time"
)

// A double tap is defined by the interval, not by where the finger goes, and
// the interval has a floor as well as a ceiling. AOSP discards a second tap
// sooner than DOUBLE_TAP_MIN_TIME (40ms) as a bounce and treats one later
// than DOUBLE_TAP_TIMEOUT (300ms) as an ordinary second tap. Being wrong in
// either direction produces a gesture that was delivered and ignored, which
// from outside looks exactly like one that was never sent.
func TestDoubleTapGapSitsInsideThePlatformWindow(t *testing.T) {
	if doubleTapGap < 40*time.Millisecond || doubleTapGap > 300*time.Millisecond {
		t.Fatalf("gap is %s, outside AOSP's 40-300ms window", doubleTapGap)
	}
	// Near either edge is as good as outside once the device-side server's
	// own scheduling is added, so insist on real margin at both ends.
	if doubleTapGap < 80*time.Millisecond || doubleTapGap > 260*time.Millisecond {
		t.Errorf("gap is %s, which is inside the window but too near an edge "+
			"to survive the server's own timing", doubleTapGap)
	}
}

// Two presses, one chain, and the finger never moves between them: the
// platform has a double-tap slop as well as a timeout, so re-issuing the move
// would be a chance to arrive a pixel off for no gain.
//
// This chain is **Android's alone**. WebDriverAgent drops a pause that occurs
// while the pointer is up, which made the two taps arrive together and WebKit
// discard the second as a bounce, so the iOS backend calls WebDriverAgent's
// own doubleTap primitive instead — CHALLENGES 67. Nothing here asserts that,
// because it is a fact about a server rather than about a chain, and it was
// established the only way it could be: against a device.
func TestDoubleTapActionsPressTwiceWithoutMoving(t *testing.T) {
	chain := doubleTapActions(540, 1200)

	var downs, ups, moves int
	for _, a := range chain {
		switch a["type"] {
		case "pointerDown":
			downs++
		case "pointerUp":
			ups++
		case "pointerMove":
			moves++
		}
	}
	if downs != 2 || ups != 2 {
		t.Errorf("chain has %d downs and %d ups, want 2 and 2", downs, ups)
	}
	if moves != 1 {
		t.Errorf("chain has %d moves, want 1 — the finger should not travel "+
			"between the taps", moves)
	}
	if chain[0]["x"] != 540 || chain[0]["y"] != 1200 {
		t.Errorf("chain starts at (%v, %v), want (540, 1200)", chain[0]["x"], chain[0]["y"])
	}

	// The pause between the two taps is the gesture, so it has to be the one
	// between the first up and the second down rather than any other.
	var betweenTaps int64
	for i, a := range chain {
		if a["type"] == "pointerUp" && i+1 < len(chain) && chain[i+1]["type"] == "pause" {
			betweenTaps = chain[i+1]["duration"].(int64)
			break
		}
	}
	if betweenTaps != doubleTapGap.Milliseconds() {
		t.Errorf("pause between taps is %dms, want %d", betweenTaps, doubleTapGap.Milliseconds())
	}
}

// A drag holds at both ends, and the first hold is the one that picks
// anything up: Android's long-press timeout is 500ms by default and a
// drag-to-reorder list arms on it, so a chain that starts traveling sooner
// scrolls the list and reports success.
func TestDragHoldsAtBothEnds(t *testing.T) {
	chain := dragHoldActions(100, 200, 700, 900, 700*time.Millisecond, 800*time.Millisecond)

	if chain[1]["type"] != "pointerDown" {
		t.Fatalf("second action is %v, want pointerDown", chain[1]["type"])
	}
	if chain[2]["type"] != "pause" {
		t.Fatalf("nothing is held after the press: %v", chain[2]["type"])
	}
	if got := chain[2]["duration"].(int64); got != 700 {
		t.Errorf("opening hold is %dms, want 700", got)
	}

	last, penult := chain[len(chain)-1], chain[len(chain)-2]
	if last["type"] != "pointerUp" {
		t.Errorf("chain ends with %v, want pointerUp", last["type"])
	}
	if penult["type"] != "pause" {
		t.Errorf("the finger lifts in the same frame as it arrives: %v", penult["type"])
	}
}

// The travel is stepped rather than left to one interpolated move. How many
// intermediate events a single pointerMove produces is the device-side
// server's business, and a drop target that highlights on hover needs them to
// exist — so they are sent rather than hoped for.
func TestDragStepsAndLandsExactlyOnTheTarget(t *testing.T) {
	chain := dragHoldActions(100, 200, 700, 900, 700*time.Millisecond, 800*time.Millisecond)

	var moves []map[string]interface{}
	for _, a := range chain {
		if a["type"] == "pointerMove" {
			moves = append(moves, a)
		}
	}
	// One to arrive at the source, then the steps.
	if len(moves) < 6 {
		t.Fatalf("only %d moves, so the travel is not stepped", len(moves))
	}
	if moves[0]["x"] != 100 || moves[0]["y"] != 200 {
		t.Errorf("starts at (%v, %v), want (100, 200)", moves[0]["x"], moves[0]["y"])
	}
	// Rounding a fraction per step must not leave the finger short of where
	// it was told to go: the last step is the one that has to be exact,
	// because that is the point the drop is read from.
	end := moves[len(moves)-1]
	if end["x"] != 700 || end["y"] != 900 {
		t.Errorf("ends at (%v, %v), want (700, 900)", end["x"], end["y"])
	}
}

// A short drag still needs enough events to be a hover, and a long one must
// not turn into an unbounded payload.
func TestDragStepCountIsBounded(t *testing.T) {
	count := func(x2, y2 int) int {
		n := 0
		for _, a := range dragHoldActions(0, 0, x2, y2, time.Second, time.Second) {
			if a["type"] == "pointerMove" {
				n++
			}
		}
		return n - 1 // the move to the start is not a step
	}
	if got := count(10, 0); got < 5 {
		t.Errorf("a 10px drag makes %d steps, want at least 5", got)
	}
	if got := count(100000, 0); got > 40 {
		t.Errorf("a very long drag makes %d steps, want at most 40", got)
	}
}

// The dump backend must not claim either gesture. It drives everything
// through `adb shell input`, which has no way to hold and no way to promise
// an interval between two calls — and a bare type assertion would have said
// otherwise for an external driver, which is what the As* helpers are for.
func TestDumpBackendClaimsNeitherGesture(t *testing.T) {
	var d Driver = &Android{}
	if _, ok := AsDoubleTapper(d); ok {
		t.Error("the dump backend claims it can double-tap")
	}
	if _, ok := AsDragger(d); ok {
		t.Error("the dump backend claims it can drag")
	}
	// And the claim it does make still holds, so this is not a test that
	// would pass on a backend that lost everything.
	if _, ok := AsGesturer(d); !ok {
		t.Error("the dump backend no longer claims it can swipe")
	}
}

// The W3C backends do claim both, or the refusal above is not a refusal — it
// is the only behavior there is.
func TestW3CBackendsClaimBothGestures(t *testing.T) {
	for _, d := range []Driver{&UIA2{}, &WDA{}} {
		if _, ok := AsDoubleTapper(d); !ok {
			t.Errorf("%T cannot double-tap", d)
		}
		if _, ok := AsDragger(d); !ok {
			t.Errorf("%T cannot drag", d)
		}
	}
}
