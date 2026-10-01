package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// coverPoll is how often a target under a control looks again to see whether
// the cover has gone — a toast, a snackbar, a sheet on its way out.
var coverPoll = 200 * time.Millisecond

// bannerTimeout bounds the wait for an iOS notification banner to go from
// over a target.
var bannerTimeout = 10 * time.Second

// CoverView is what an action met over its target: a control it aimed around,
// or something that is not a control and may take the tap. Omitted when
// nothing was drawn over the point touched.
type CoverView struct {
	Label string `json:"label"`
	// Control is true when the cover is itself a control, which the action
	// avoided by touching a clear part of the target.
	Control bool `json:"control"`
}

// resolveAim resolves target and decides where to touch it so the touch
// reaches it rather than something the app drew over it (CHALLENGES 115).
//
// A control drawn over the target's center is avoided by aiming at the clear
// part of the target nearest the center; a control over all of it is waited
// for within the implicit wait, since a toast or a sheet on its way out is the
// ordinary case, and then refused. Something over the point that is not a
// control is reported, not refused: neither platform's tree says whether it
// takes touches — a pass-through view and a plain one are identical there —
// and refusing the pass-through case would refuse a tap that works.
func (h *Handlers) resolveAim(ctx context.Context, s *session, target string) (*uitree.Node, uitree.Aim, error) {
	start := time.Now()
	for {
		node, tree, err := h.resolveNode(ctx, s, target)
		if err != nil {
			return nil, uitree.Aim{}, err
		}
		aim := tree.AimAt(node)
		if aim.Blocker == nil {
			refusal, err := h.loadedHitRefusal(ctx, s, target, node, tree, aim)
			if err != nil {
				return nil, uitree.Aim{}, err
			}
			if refusal == nil {
				return node, aim, nil
			}
			// What UIKit gives the touch to is waited for as a control
			// over the target is: an overlay on its way out is the
			// ordinary case.
			if time.Since(start) >= h.implicitWait {
				return nil, uitree.Aim{}, refusal
			}
			select {
			case <-ctx.Done():
				return nil, uitree.Aim{}, ctx.Err()
			case <-time.After(coverPoll):
			}
			continue
		}
		// A notification banner goes by itself, in five to eight seconds
		// measured — longer than the implicit wait — so it is waited out,
		// as Android's clipboard preview is (CHALLENGES 113, 155).
		budget := h.implicitWait
		if uitree.IsNotificationBanner(aim.Blocker) && budget < bannerTimeout {
			budget = bannerTimeout
		}
		if time.Since(start) >= budget {
			return nil, uitree.Aim{}, h.coveredBy(s, target, aim.Blocker, time.Since(start))
		}
		select {
		case <-ctx.Done():
			return nil, uitree.Aim{}, ctx.Err()
		case <-time.After(coverPoll):
		}
	}
}

// loadedHitRefusal asks the hit probe loaded into the app at launch, when
// there is one, whether a touch at aim reaches the target, and is the
// refusal when it would not (docs/decisions/0008). With no probe loaded the
// question is not asked: through lldb it takes seconds, which is app_hit_test's
// to spend. An answer of unknown — no view in the app is the element, which
// happens to one named by label — leaves the action to the checks the tree
// allows, as before the probe.
func (h *Handlers) loadedHitRefusal(ctx context.Context, s *session, target string, node *uitree.Node, tree *uitree.Tree, aim uitree.Aim) (*mobiumerr.Error, error) {
	if s.web != nil {
		return nil, nil
	}
	lt, ok := mobiumdriver.AsLoadedHitTester(s.driver)
	if !ok {
		return nil, nil
	}
	hit, loaded, err := lt.LoadedHitTest(ctx, node, aim.X, aim.Y, tree.Package())
	if !loaded || err != nil {
		return nil, err
	}
	loc, lerr := h.locatorFor(s.dev.Serial, target)
	if lerr != nil {
		loc = uitree.Locator{Kind: uitree.KindText, Value: target}
	}
	switch hit.Verdict {
	case "nothing":
		return hitNothing(loc, aim), nil
	case "covered":
		return hitCovered(loc, aim, hit), nil
	}
	return nil, nil
}

// coveredBy is the refusal for a target a control lies over. Its remedies can
// all work: the cover may go on its own, can be dismissed, or may be what the
// caller meant to press.
func (h *Handlers) coveredBy(s *session, target string, cover *uitree.Node, waited time.Duration) error {
	loc, err := h.locatorFor(s.dev.Serial, target)
	if err != nil {
		loc = uitree.Locator{Kind: uitree.KindText, Value: target}
	}
	name := uitree.Describe(cover)
	return failedCheck(mobiumerr.ElementNotReachable, loc, checkReceivesEvents,
		fmt.Sprintf("it is covered by %q (%s), and was for %s", name, uitree.RoleOf(cover), waited.Round(100*time.Millisecond)),
		"a touch there would press that instead; wait for it to go, dismiss it, or tap its own ref from app_map "+
			"if it is what you meant").
		WithRemedy("dismiss what is over it, wait for it to go, or tap the cover's own ref from app_map").
		WithDetail("locator", loc.String()).
		WithDetail("cover", name)
}

// aimNote is what a result says about the point it touched, and the
// structured half of the same: nothing when nothing was over it.
func aimNote(aim uitree.Aim) (string, *CoverView) {
	switch {
	case aim.Moved:
		c := "something"
		if aim.CenterCover != nil {
			c = uitree.Describe(aim.CenterCover)
		}
		return fmt.Sprintf("; its center is covered by %q, so it was touched at a clear point", c), &CoverView{Label: c, Control: true}
	case aim.Over != nil:
		c := uitree.Describe(aim.Over)
		return fmt.Sprintf("; %q is drawn over that point and is not a control — it may take the touch", c),
			&CoverView{Label: c, Control: false}
	}
	return "", nil
}
