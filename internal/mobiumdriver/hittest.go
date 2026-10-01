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

// HitProbeLauncher is implemented by backends that can load the hit probe
// into an app as it launches, so it answers before every action in
// milliseconds instead of through a debugger.
type HitProbeLauncher interface {
	LaunchWithHitProbe(ctx context.Context, app string) error
}

// LoadedHitTester answers from a probe loaded at launch. loaded is false,
// with no error, when the app in front has none: the question is then not
// asked, rather than asked the slow way before every action.
type LoadedHitTester interface {
	LoadedHitTest(ctx context.Context, target *uitree.Node, x, y int, app string) (hit device.Hit, loaded bool, err error)
}

// androidNeedsNoHitTest is why Android refuses: nothing there is hidden from
// the tree the way iOS hides it, so the checks every action makes already
// see what a hit test would.
const androidNeedsNoHitTest = "Android needs no hit test from outside: its hierarchy lists a view that " +
	"accessibility hides — the Obstruction Demo's hidden overlay is a clickable, unnamed view there — and " +
	"app_tap already refuses a control drawn over its target. The hit test is for iOS"

// HitTest refuses on Android, saying why none is needed.
func (a *Android) HitTest(context.Context, *uitree.Node, int, int, string) (device.Hit, error) {
	return device.Hit{}, mobiumerr.New(mobiumerr.Unsupported, "%s", androidNeedsNoHitTest)
}

// LaunchWithHitProbe refuses on Android, saying why none is needed.
func (a *Android) LaunchWithHitProbe(context.Context, string) error {
	return mobiumerr.New(mobiumerr.Unsupported, "%s", androidNeedsNoHitTest)
}

// LaunchWithHitProbe refuses on Android, saying why none is needed.
func (u *UIA2) LaunchWithHitProbe(context.Context, string) error {
	return mobiumerr.New(mobiumerr.Unsupported, "%s", androidNeedsNoHitTest)
}

// HitTest refuses on Android, saying why none is needed.
func (u *UIA2) HitTest(context.Context, *uitree.Node, int, int, string) (device.Hit, error) {
	return device.Hit{}, mobiumerr.New(mobiumerr.Unsupported, "%s", androidNeedsNoHitTest)
}

// phoneHasNoLoadedProbe is why a real iPhone cannot load the probe at
// launch: Mobium could reach it there only over the network.
const phoneHasNoLoadedProbe = "a real iPhone cannot load the hit probe at launch: the probe would have to " +
	"listen on the phone's network for Mobium to reach it, which Mobium does not open. Launch without it " +
	"and call app_hit_test before the taps that need it — it asks through the debugger, about nine " +
	"seconds a call"

// LaunchWithHitProbe launches app on a simulator with the hit probe loaded
// in it, through WebDriverAgent with the probe in the environment; every
// launch Mobium makes of it again in this session — for a language or a
// zone — loads it too, until a plain launch.
func (w *WDA) LaunchWithHitProbe(ctx context.Context, app string) error {
	if w.phone != nil {
		return mobiumerr.New(mobiumerr.Unsupported, "%s", phoneHasNoLoadedProbe)
	}
	w.setProbed(app, true)
	return w.launchWith(ctx, app, w.pinnedLocale(app), w.sessionZone())
}

func (w *WDA) setProbed(app string, on bool) {
	w.localeMu.Lock()
	defer w.localeMu.Unlock()
	if w.probed == nil {
		w.probed = map[string]bool{}
	}
	w.probed[app] = on
}

func (w *WDA) isProbed(app string) bool {
	w.localeMu.Lock()
	defer w.localeMu.Unlock()
	return w.probed[app]
}

// hitArgs is a hit test's question in points, which is what UIKit answers
// in, from a target and a point in device pixels.
func (w *WDA) hitArgs(target *uitree.Node, x, y int) (scale, px, py float64, frame [4]float64) {
	scale = w.scale
	if scale <= 0 {
		scale = 1
	}
	b := target.Bounds
	frame = [4]float64{float64(b.X1) / scale, float64(b.Y1) / scale, float64(b.Width()) / scale, float64(b.Height()) / scale}
	return scale, float64(x) / scale, float64(y) / scale, frame
}

// inPixels puts a hit's receiver back in device pixels.
func inPixels(hit device.Hit, scale float64) device.Hit {
	for i := range hit.Frame {
		hit.Frame[i] *= scale
	}
	return hit
}

// LoadedHitTest asks the probe loaded into app at launch, on a simulator.
// A phone never has one.
func (w *WDA) LoadedHitTest(ctx context.Context, target *uitree.Node, x, y int, app string) (device.Hit, bool, error) {
	if w.phone != nil || app == "" {
		return device.Hit{}, false, nil
	}
	scale, px, py, frame := w.hitArgs(target, x, y)
	hit, loaded, err := w.sim.LoadedHitTest(ctx, app, px, py, frame, target.TestID)
	if !loaded || err != nil {
		return hit, loaded, err
	}
	return inPixels(hit, scale), true, nil
}

// HitTest asks UIKit inside the app: from the probe loaded at launch when
// there is one, and otherwise through lldb — on a simulator by loading the
// probe, and on a real iPhone by evaluating it as an expression, which
// needs the app built for development — get-task-allow — as one a developer
// installs from Xcode is; an App Store app refuses the debugger.
func (w *WDA) HitTest(ctx context.Context, target *uitree.Node, x, y int, app string) (device.Hit, error) {
	if app == "" {
		return device.Hit{}, mobiumerr.New(mobiumerr.DeviceServer, "could not tell which app is in front")
	}
	if hit, loaded, err := w.LoadedHitTest(ctx, target, x, y, app); loaded {
		return hit, err
	}
	scale, px, py, frame := w.hitArgs(target, x, y)
	var hit device.Hit
	var err error
	if w.phone != nil {
		hit, err = w.phone.HitTest(ctx, app, px, py, frame, target.TestID)
	} else {
		hit, err = w.sim.HitTest(ctx, app, px, py, frame, target.TestID)
	}
	if err != nil {
		return hit, err
	}
	return inPixels(hit, scale), nil
}
