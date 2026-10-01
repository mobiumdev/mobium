package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// AuditView is the result of app_audit.
type AuditView struct {
	Device   string             `json:"device"`
	Findings []AuditFindingView `json:"findings"`
}

// AuditFindingView is one finding of Apple's audit. Locator names the
// element as map's locators do, when it has a test id or a label; Bounds are
// device pixels, as map's are.
type AuditFindingView struct {
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
	Element string `json:"element,omitempty"`
	Locator string `json:"locator,omitempty"`
	// Bounds is nil when Apple names no element: on a real iPhone its
	// contrast findings carry none, where a simulator's do.
	Bounds *BoundsView `json:"bounds,omitempty"`
}

// audit is app_audit: the platform's own accessibility audit of the screen
// in front — on iOS, Apple's, through WebDriverAgent.
func (h *Handlers) audit(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	a, ok := mobiumdriver.AsAuditor(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapAudit, "run an accessibility audit")
	}
	found, err := a.Audit(ctx)
	if err != nil {
		return nil, err
	}
	view := AuditView{Device: s.dev.Serial, Findings: []AuditFindingView{}}
	lines := []string{}
	for _, f := range found {
		loc := ""
		switch {
		case f.TestID != "":
			loc = "testid=" + f.TestID
		case f.Label != "":
			loc = "label=" + f.Label
		}
		fv := AuditFindingView{Type: f.Type, Summary: f.Summary, Detail: f.Detail, Element: f.Element, Locator: loc}
		if !f.Bounds.Empty() {
			fv.Bounds = &BoundsView{X1: f.Bounds.X1, Y1: f.Bounds.Y1, X2: f.Bounds.X2, Y2: f.Bounds.Y2}
		}
		view.Findings = append(view.Findings, fv)
		line := fmt.Sprintf("%-11s %s", f.Type, f.Summary)
		switch {
		case f.Element != "":
			line += " — " + f.Element
		case f.Bounds.Empty():
			line += " — Apple's audit names no element for it"
		}
		if loc != "" {
			line += "  [" + loc + "]"
		}
		lines = append(lines, line)
	}
	if len(found) == 0 {
		// Said so it does not read as "not checked".
		return Result("No accessibility audit findings on this screen. The audit covers only what is on screen.", view), nil
	}
	return Result(fmt.Sprintf("%d accessibility audit finding(s) on this screen:\n  %s", len(found),
		strings.Join(lines, "\n  ")), view), nil
}
