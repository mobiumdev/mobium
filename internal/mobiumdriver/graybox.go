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

// grayBox is WebDriverAgent's: one listener for the session, fed by the
// phone's log capture or, on a simulator, by a log stream of its own.
type grayBox struct {
	mu   sync.Mutex
	app  string
	box  *device.GrayBox
	stop func()
}

// LaunchGrayBox launches app with the gray-box argument, and listens.
func (w *WDA) LaunchGrayBox(ctx context.Context, app string) error {
	w.gray.mu.Lock()
	if w.gray.box == nil {
		box := device.NewGrayBox()
		if w.phone != nil {
			if w.plog == nil {
				w.gray.mu.Unlock()
				return mobiumerr.New(mobiumerr.DeviceServer, "the phone's log is not being captured, "+
					"and the gray box is heard through it: %v", w.plogErr)
			}
			w.plog.FeedGrayBox(box)
		} else {
			stop, err := w.sim.StreamGrayBox(box)
			if err != nil {
				w.gray.mu.Unlock()
				return err
			}
			w.gray.stop = stop
		}
		w.gray.box = box
	}
	w.gray.app = app
	w.gray.box.Reset()
	w.gray.mu.Unlock()
	return w.launchWith(ctx, app, w.pinnedLocale(app), w.sessionZone())
}

// GrayBox is what the app launched with the library on has said, while it
// is the last app launched.
func (w *WDA) GrayBox() *device.GrayBox {
	w.gray.mu.Lock()
	defer w.gray.mu.Unlock()
	if w.gray.app == "" {
		return nil
	}
	return w.gray.box
}

// isGray reports whether app is being launched with the library on.
func (w *WDA) isGray(app string) bool {
	w.gray.mu.Lock()
	defer w.gray.mu.Unlock()
	return app != "" && w.gray.app == app
}

// grayOff forgets the gray-box app: a launch the ordinary way is driven
// as any other.
func (w *WDA) grayOff() {
	w.gray.mu.Lock()
	defer w.gray.mu.Unlock()
	w.gray.app = ""
}

// grayClose stops listening.
func (w *WDA) grayClose() {
	w.gray.mu.Lock()
	defer w.gray.mu.Unlock()
	if w.gray.stop != nil {
		w.gray.stop()
		w.gray.stop = nil
	}
	w.gray.box, w.gray.app = nil, ""
}
