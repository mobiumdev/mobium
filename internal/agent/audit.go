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
	// Unchecked names the rules this audit could not run, so an empty
	// Findings is not read as a pass on them: on Android, contrast.
	Unchecked []string `json:"unchecked,omitempty"`
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

// audit is app_audit: an accessibility audit of the screen in front — on
// iOS, Apple's, through WebDriverAgent; on Android, Mobium's checks on the
// hierarchy, since Android has none a tool outside the app can run.
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
	android := s.backend == BackendUIA2 || s.backend == BackendDump
	lines := []string{}
	for _, f := range found {
		loc := f.Locator
		switch {
		case loc != "":
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
		// Android's details are the measurement — a size, a count — and
		// short; Apple's restate the summary at length, and stay in the
		// structured result.
		if android && f.Detail != "" {
			line += "\n      " + f.Detail
		}
		lines = append(lines, line)
	}
	// Android's audit is Mobium's own, on the hierarchy, and says what it
	// left out: a clean screen there has not had its contrast checked.
	scope := "The audit covers only what is on screen."
	if android {
		view.Unchecked = []string{"TextContrast", "ImageContrast"}
		scope = "Android's audit is Mobium's checks on the hierarchy, named as the Accessibility Test " +
			"Framework names them; it covers only what is on screen, and contrast is not checked."
	}
	if len(found) == 0 {
		// Said so it does not read as "not checked".
		return Result("No accessibility audit findings on this screen. "+scope, view), nil
	}
	return Result(fmt.Sprintf("%d accessibility audit finding(s) on this screen:\n  %s\n%s", len(found),
		strings.Join(lines, "\n  "), scope), view), nil
}
