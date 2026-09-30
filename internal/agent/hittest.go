package agent

import (
	"context"
	"fmt"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// HitTestView is the result of app_hit_test when the touch reaches its
// target. When it would not, the tool fails, as app_tap does, and the
// details name what would take the touch.
type HitTestView struct {
	Device string `json:"device"`
	Target string `json:"target"`
	// X and Y are the point app_tap would touch, in device pixels: the
	// center, or a clear point when a control covers the center.
	X int `json:"x"`
	Y int `json:"y"`
	// Reaches is UIKit's answer: the view a touch there goes to is the
	// target or inside it.
	Reaches bool `json:"reaches"`
}

// hitTest asks the platform's own hit test, below accessibility, whether a
// touch at the point app_tap would use reaches the target. Opt-in, because
// on a simulator it attaches a debugger to the app for about two seconds
// (docs/decisions/0008).
func (h *Handlers) hitTest(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	target := stringArg(args, "target")
	if target == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_hit_test needs a target (\"@e5\" or \"testid=submit\")")
	}
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	if s.web != nil {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "in a WebView the page decides where a touch goes, and "+
			"app_tap already asks it (elementFromPoint) before touching; the hit test is for native views — "+
			"switch to NATIVE_APP with app_context first")
	}
	ht, ok := mobiumdriver.AsHitTester(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapHitTest, "hit-test a point")
	}
	node, tree, err := h.resolveNode(ctx, s, target)
	if err != nil {
		return nil, err
	}
	aim := tree.AimAt(node)
	hit, err := ht.HitTest(ctx, node, aim.X, aim.Y, tree.Package())
	if err != nil {
		return nil, err
	}
	loc, lerr := h.locatorFor(s.dev.Serial, target)
	if lerr != nil {
		loc = uitree.Locator{Kind: uitree.KindText, Value: target}
	}
	switch hit.Verdict {
	case "reaches":
		return Result(fmt.Sprintf("a touch at (%d, %d) reaches %s, by UIKit's own hit test", aim.X, aim.Y, target),
			HitTestView{Device: s.dev.Serial, Target: target, X: aim.X, Y: aim.Y, Reaches: true}), nil
	case "nothing":
		return nil, failedCheck(mobiumerr.ElementNotReachable, loc, checkReceivesEvents,
			fmt.Sprintf("no view takes a touch at (%d, %d), so a tap there would reach nothing", aim.X, aim.Y),
			"something over it takes no touches and passes none on; wait for it to go, or do what removes it").
			WithRemedy("remove what is over it in the app, or wait for it to go").
			WithDetail("locator", loc.String()).WithDetail("x", aim.X).WithDetail("y", aim.Y)
	case "covered":
		return nil, hitCovered(loc, aim, hit)
	}
	return nil, mobiumerr.New(mobiumerr.DeviceServer, "the hit test could not tell: %s", hit.Reason)
}

// hitCovered is the refusal for a touch UIKit would give to something else.
// A receiver hidden from accessibility has no ref in app_map, so its remedy
// cannot be to tap it.
func hitCovered(loc uitree.Locator, aim uitree.Aim, hit device.Hit) error {
	name := hit.Label
	if name == "" {
		name = hit.Class
	}
	r := uitree.Rect{X1: int(hit.Frame[0] + 0.5), Y1: int(hit.Frame[1] + 0.5),
		X2: int(hit.Frame[0] + hit.Frame[2] + 0.5), Y2: int(hit.Frame[1] + hit.Frame[3] + 0.5)}
	why := fmt.Sprintf("UIKit would give a touch at (%d, %d) to %q (%s, at %s)", aim.X, aim.Y, name, hit.Class, r)
	remedy := "dismiss what is over it, wait for it to go, or tap the cover's own ref from app_map if it is what you meant"
	if hit.Hidden {
		why += ", which is hidden from accessibility — so no map, tree or hittable check shows it"
		remedy = "dismiss what is over it in the app, or wait for it to go; it has no ref, since accessibility cannot see it"
	}
	return failedCheck(mobiumerr.ElementNotReachable, loc, checkReceivesEvents, why,
		"a tap there would reach that instead").
		WithRemedy(remedy).
		WithDetail("locator", loc.String()).
		WithDetail("x", aim.X).WithDetail("y", aim.Y).
		WithDetail("receiver", name).
		WithDetail("receiver_class", hit.Class).
		WithDetail("receiver_bounds", r.String()).
		WithDetail("hidden_from_accessibility", hit.Hidden)
}
