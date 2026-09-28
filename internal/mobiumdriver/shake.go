package mobiumdriver

import (
	"context"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Shaker is implemented by backends that can shake the device.
type Shaker interface {
	Shake(ctx context.Context) error
}

// Shake plays a shake on an emulator's accelerometer. A real phone's sensors
// read the phone, and nothing outside it can move them.
func (a *Android) Shake(ctx context.Context) error { return androidShake(ctx, a.adb) }

// Shake plays a shake on an emulator's accelerometer.
func (u *UIA2) Shake(ctx context.Context) error { return androidShake(ctx, u.adb) }

// emulatorShaker is the part of adb a shake needs, so the refusal on a phone
// can be tested without one.
type emulatorShaker interface {
	IsEmulator(context.Context) bool
	Shake(context.Context) error
}

func androidShake(ctx context.Context, adb emulatorShaker) error {
	if !adb.IsEmulator(ctx) {
		return mobiumerr.New(mobiumerr.Unsupported, "nothing outside a real phone can shake it — its "+
			"accelerometer reads the phone. Shake is an emulator feature; shake the phone by hand, or run "+
			"the flow on an emulator")
	}
	return adb.Shake(ctx)
}

// Shake sends a simulator the shake from its Device menu. A phone refuses.
func (w *WDA) Shake(ctx context.Context) error {
	if w.phone != nil {
		return mobiumerr.New(mobiumerr.Unsupported, "nothing outside an iPhone can shake it — XCTest has no "+
			"shake, and its motion sensors read the phone. Shake is a simulator feature; shake the phone by "+
			"hand, or run the flow on a simulator")
	}
	return w.sim.Shake(ctx)
}
