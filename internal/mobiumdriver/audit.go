package mobiumdriver

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// Apple's accessibility audit, through WebDriverAgent.
//
// XCUITest's performAccessibilityAudit (iOS 17 and later) checks the app in
// front against Apple's own rules — hit regions too small to touch, contrast,
// labels that repeat their traits, Dynamic Type, clipped text — and
// WebDriverAgent runs it with /wda/performAccessibilityAudit. Measured on an
// iPhone 17 Pro simulator, iOS 26.5: Settings gave five findings in about
// two seconds, an 18x22pt Dictate button too small, a label duplicating its
// traits and three contrast failures, each with the element's rectangle,
// label and identifier. On the iPhone 15 Plus, iOS 26.6.2, Settings gave
// eight in about six seconds, and its seven contrast findings named no
// element at all — a phone's contrast findings carry none, where the
// simulator's do. It audits only what is on screen, and it judges
// sizes in points itself. Its hit-region rule is not Apple's 44pt design
// guideline: MobiumApp's 24x25pt tiny target passed it, where formflux's
// touch-target check (app_screen inspect) names it.

// AuditFinding is one thing Apple's audit found.
type AuditFinding struct {
	// Type is the kind of rule, as WebDriverAgent names it without its
	// prefix: HitRegion, Contrast, Trait, DynamicType, TextClipped, …
	Type string `json:"type"`
	// Summary is Apple's short description, Detail its sentence.
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
	// Element is Apple's name for the element, and Label and TestID its
	// accessibility label and identifier, either of which may be empty.
	Element string `json:"element,omitempty"`
	Label   string `json:"label,omitempty"`
	TestID  string `json:"testid,omitempty"`
	// Bounds are in device pixels, as map's are.
	Bounds uitree.Rect `json:"bounds"`
}

// Auditor is implemented by backends that can run the platform's own
// accessibility audit on the screen in front.
type Auditor interface {
	Audit(ctx context.Context) ([]AuditFinding, error)
}

// Audit runs Apple's accessibility audit on the app in front.
func (w *WDA) Audit(ctx context.Context) ([]AuditFinding, error) {
	var resp struct {
		Value []struct {
			AuditType           string `json:"auditType"`
			CompactDescription  string `json:"compactDescription"`
			DetailedDescription string `json:"detailedDescription"`
			Element             string `json:"element"`
			ElementAttributes   struct {
				Label         string `json:"label"`
				RawIdentifier string `json:"rawIdentifier"`
				Identifier    string `json:"identifier"`
				Rect          struct {
					X, Y, Width, Height float64
				} `json:"rect"`
			} `json:"elementAttributes"`
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/wda/performAccessibilityAudit"),
		map[string]interface{}{}, &resp); err != nil {
		return nil, fmt.Errorf("run the accessibility audit: %w", err)
	}
	out := make([]AuditFinding, 0, len(resp.Value))
	for _, f := range resp.Value {
		a := f.ElementAttributes
		id := a.Identifier
		if id == "" {
			id = a.RawIdentifier
		}
		r := uitree.Rect{X1: int(a.Rect.X), Y1: int(a.Rect.Y), X2: int(a.Rect.X + a.Rect.Width), Y2: int(a.Rect.Y + a.Rect.Height)}
		scaled := &uitree.Tree{Root: &uitree.Node{Bounds: r}}
		scaled.Scale(w.scale)
		out = append(out, AuditFinding{
			Type:    strings.TrimPrefix(f.AuditType, "XCUIAccessibilityAuditType"),
			Summary: f.CompactDescription, Detail: f.DetailedDescription,
			Element: f.Element, Label: a.Label, TestID: id, Bounds: scaled.Root.Bounds,
		})
	}
	return out, nil
}

// androidHasNoAudit is why Android refuses.
const androidHasNoAudit = "Android has no accessibility audit that can be run from outside the app — Google's " +
	"Accessibility Scanner and the Accessibility Test Framework run inside it. app_screen with inspect " +
	"(mobium screen --inspect) checks touch targets, truncated text and controls with nothing to announce"

// Audit refuses on Android, saying why.
func (a *Android) Audit(context.Context) ([]AuditFinding, error) {
	return nil, mobiumerr.New(mobiumerr.Unsupported, "%s", androidHasNoAudit)
}

// Audit refuses on Android, saying why.
func (u *UIA2) Audit(context.Context) ([]AuditFinding, error) {
	return nil, mobiumerr.New(mobiumerr.Unsupported, "%s", androidHasNoAudit)
}
