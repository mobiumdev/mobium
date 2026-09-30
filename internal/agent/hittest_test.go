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

// A ref whose locator now finds another element — different words and a
// different place — is refused; one that only moved, or only changed its
// words, is not.
func TestStaleRef(t *testing.T) {
	h := &Handlers{refs: map[string]*refTable{}}
	table := &refTable{entries: map[string]uitree.Locator{}}
	home := &uitree.Node{Text: "Login Demo", TestID: "loginBtn", Bounds: uitree.Rect{X1: 48, Y1: 866, X2: 1158, Y2: 1007}}
	table.add(uitree.Entry{Ref: "@e4", Locator: uitree.Locator{Kind: uitree.KindTestID, Value: "loginBtn"}, Node: home})
	h.refs["sim"] = table

	logIn := &uitree.Node{Text: "Log In", TestID: "loginBtn", Bounds: uitree.Rect{X1: 48, Y1: 1540, X2: 1158, Y2: 1682}}
	if err := h.staleRef("sim", "@e4", logIn); mobiumerr.CodeOf(err) != mobiumerr.NoSuchElement {
		t.Errorf("another element: %v", err)
	}
	moved := &uitree.Node{Text: "Login Demo", TestID: "loginBtn", Bounds: uitree.Rect{X1: 48, Y1: 400, X2: 1158, Y2: 541}}
	if err := h.staleRef("sim", "@e4", moved); err != nil {
		t.Errorf("the same element, scrolled: %v", err)
	}
	relabeled := &uitree.Node{Text: "Signing in…", TestID: "loginBtn", Bounds: home.Bounds}
	if err := h.staleRef("sim", "@e4", relabeled); err != nil {
		t.Errorf("the same element, its words changed: %v", err)
	}
	if err := h.staleRef("sim", "testid=loginBtn", logIn); err != nil {
		t.Errorf("a locator is not a ref: %v", err)
	}
}
