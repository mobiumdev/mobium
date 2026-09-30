package agent

import (
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

func remedyOf(err error) string {
	if e, ok := mobiumerr.As(err); ok {
		return e.Remedy
	}
	return ""
}

// A receiver hidden from accessibility has no ref, so its refusal must not
// offer one; a visible one may be what the caller meant.
func TestHitCoveredRemedies(t *testing.T) {
	loc := uitree.Locator{Kind: uitree.KindTestID, Value: "hiddenTarget"}
	aim := uitree.Aim{X: 603, Y: 2280}
	hidden := hitCovered(loc, aim, device.Hit{Verdict: "covered", Hidden: true, Class: "RCTViewComponentView",
		Label: "hidden overlay", Frame: [4]float64{48, 2208, 1110, 142}})
	if mobiumerr.CodeOf(hidden) != mobiumerr.ElementNotReachable || !strings.Contains(hidden.Error(), "hidden from accessibility") {
		t.Errorf("hidden: %v", hidden)
	}
	if strings.Contains(remedyOf(hidden), "ref from app_map") {
		t.Errorf("offered a ref for a view with none: %s", remedyOf(hidden))
	}
	seen := hitCovered(loc, aim, device.Hit{Verdict: "covered", Class: "RCTViewComponentView", Label: "scrim"})
	if !strings.Contains(remedyOf(seen), "ref from app_map") || strings.Contains(seen.Error(), "hidden from accessibility") {
		t.Errorf("visible: %v / %s", seen, remedyOf(seen))
	}
}
