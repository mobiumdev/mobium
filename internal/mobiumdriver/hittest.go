package mobiumdriver

import (
	"context"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// HitTester is implemented by backends that can ask the platform's own hit
// test, below accessibility, which view a touch at a point would reach.
type HitTester interface {
	// HitTest answers for a touch at (x, y), in device pixels, aimed at
	// target, in the app in front.
	HitTest(ctx context.Context, target *uitree.Node, x, y int, app string) (device.Hit, error)
}

// androidNeedsNoHitTest is why Android refuses: nothing there is hidden from
// the tree the way iOS hides it, so the checks every action makes already
// see what a hit test would.
const androidNeedsNoHitTest = "Android needs no hit test from outside: its hierarchy lists a view that " +
	"accessibility hides — the Obstruction Demo's hidden overlay is a clickable, unnamed view there — and " +
	"app_tap already refuses a control drawn over its target. The hit test is for an iOS simulator"

// HitTest refuses on Android, saying why none is needed.
func (a *Android) HitTest(context.Context, *uitree.Node, int, int, string) (device.Hit, error) {
	return device.Hit{}, mobiumerr.New(mobiumerr.Unsupported, "%s", androidNeedsNoHitTest)
}

// HitTest refuses on Android, saying why none is needed.
func (u *UIA2) HitTest(context.Context, *uitree.Node, int, int, string) (device.Hit, error) {
	return device.Hit{}, mobiumerr.New(mobiumerr.Unsupported, "%s", androidNeedsNoHitTest)
}

// HitTest asks UIKit inside the app, on a simulator, through lldb. A real
// iPhone refuses: attaching a debugger to an app on a phone is not built.
func (w *WDA) HitTest(ctx context.Context, target *uitree.Node, x, y int, app string) (device.Hit, error) {
	if w.phone != nil {
		return device.Hit{}, mobiumerr.New(mobiumerr.Unsupported, "the hit test attaches a debugger to the app, "+
			"which mobium does on a simulator only — on an iPhone it would need the app signed for debugging and "+
			"a debug server on the phone, which is not built. Run the check on a simulator")
	}
	if app == "" {
		return device.Hit{}, mobiumerr.New(mobiumerr.DeviceServer, "could not tell which app is in front")
	}
	scale := w.scale
	if scale <= 0 {
		scale = 1
	}
	b := target.Bounds
	frame := [4]float64{float64(b.X1) / scale, float64(b.Y1) / scale, float64(b.Width()) / scale, float64(b.Height()) / scale}
	hit, err := w.sim.HitTest(ctx, app, float64(x)/scale, float64(y)/scale, frame, target.TestID)
	if err != nil {
		return hit, err
	}
	for i := range hit.Frame {
		hit.Frame[i] *= scale
	}
	return hit, nil
}
