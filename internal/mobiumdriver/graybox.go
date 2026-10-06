package mobiumdriver

import (
	"context"
	"sync"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// GrayBoxer is implemented by backends that can launch an app with Mobium's
// gray-box library turned on and hear what it says about being busy. See
// device.GrayBox for the lines the library writes.
type GrayBoxer interface {
	// LaunchGrayBox launches app with the library turned on, and starts
	// listening for it.
	LaunchGrayBox(ctx context.Context, app string) error
	// GrayBox is what the app launched that way has said, or nil when the
	// last app launched was launched the ordinary way.
	GrayBox() *device.GrayBox
}

// AsGrayBoxer returns the driver's gray box, if it has one.
func AsGrayBoxer(d Driver) (GrayBoxer, bool) {
	g, ok := d.(GrayBoxer)
	return g, ok && has(d, CapGrayBox)
}

// grayBox is a driver's gray box: one listener for the session, started on
// the first gray-box launch, and the app it belongs to. Every backend holds
// one; only how the lines arrive differs.
type grayBox struct {
	mu   sync.Mutex
	app  string
	box  *device.GrayBox
	stop func()
}

// use starts listening, through listen, unless already listening, and
// makes app the gray-box app, heard from nothing yet.
func (g *grayBox) use(app string, listen func(*device.GrayBox) (func(), error)) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.box == nil {
		box := device.NewGrayBox()
		stop, err := listen(box)
		if err != nil {
			return err
		}
		g.box, g.stop = box, stop
	}
	g.app = app
	g.box.Reset()
	return nil
}

// current is what the gray-box app has said, while it is the last app
// launched.
func (g *grayBox) current() *device.GrayBox {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.app == "" {
		return nil
	}
	return g.box
}

// isFor reports whether app is the gray-box app.
func (g *grayBox) isFor(app string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return app != "" && g.app == app
}

// off forgets the gray-box app: a launch the ordinary way is driven as any
// other.
func (g *grayBox) off() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.app = ""
}

// close stops listening.
func (g *grayBox) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stop != nil {
		g.stop()
	}
	g.box, g.app, g.stop = nil, "", nil
}

// LaunchGrayBox launches app with the gray-box argument, and listens: on a
// phone through the log the session already captures, on a simulator
// through a log stream of its own.
func (w *WDA) LaunchGrayBox(ctx context.Context, app string) error {
	err := w.gray.use(app, func(box *device.GrayBox) (func(), error) {
		if w.phone == nil {
			return w.sim.StreamGrayBox(box)
		}
		if w.plog == nil {
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "the phone's log is not being captured, "+
				"and the gray box is heard through it: %v", w.plogErr)
		}
		w.plog.FeedGrayBox(box)
		return func() { w.plog.FeedGrayBox(nil) }, nil
	})
	if err != nil {
		return err
	}
	return w.launchWith(ctx, app, w.pinnedLocale(app), w.sessionZone())
}

// GrayBox is what the gray-box app has said.
func (w *WDA) GrayBox() *device.GrayBox { return w.gray.current() }

// LaunchGrayBox starts app afresh with the gray-box extra, and listens
// through logcat.
func (u *UIA2) LaunchGrayBox(ctx context.Context, app string) error {
	if err := u.gray.use(app, u.adb.StreamGrayBox); err != nil {
		return err
	}
	return u.adb.LaunchAppWith(ctx, app, device.GrayBoxExtra...)
}

// GrayBox is what the gray-box app has said.
func (u *UIA2) GrayBox() *device.GrayBox { return u.gray.current() }

// LaunchGrayBox starts app afresh with the gray-box extra, and listens
// through logcat.
func (a *Android) LaunchGrayBox(ctx context.Context, app string) error {
	if err := a.gray.use(app, a.adb.StreamGrayBox); err != nil {
		return err
	}
	return a.adb.LaunchAppWith(ctx, app, device.GrayBoxExtra...)
}

// GrayBox is what the gray-box app has said.
func (a *Android) GrayBox() *device.GrayBox { return a.gray.current() }

// Close stops listening for the gray box, the dump driver's only state.
func (a *Android) Close() error {
	a.gray.close()
	return nil
}
