package mobiumdriver

import (
	"testing"
	"time"
)

func TestWDAHoldsADragByMovingInPlace(t *testing.T) {
	// WebDriverAgent shortens a pause straight after pointerDown to about
	// 180ms; a move to the same point lasting the hold arrives in full.
	chain := dragHoldChain(100, 200, 100, 600, 1500*time.Millisecond, 400*time.Millisecond, true)
	third := chain[2]
	if third["type"] != "pointerMove" || third["x"] != 100 || third["y"] != 200 || third["duration"] != int64(1500) {
		t.Errorf("the opening hold is %v, want a 1500ms move to where the finger already is", third)
	}
	// Android's chain keeps its pause, which arrives in full there.
	if got := dragHoldActions(100, 200, 100, 600, 1500*time.Millisecond, 400*time.Millisecond)[2]["type"]; got != "pause" {
		t.Errorf("Android's opening hold is a %v, want the pause it measured as 1522ms", got)
	}
}
