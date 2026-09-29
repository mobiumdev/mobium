package mobiumdriver

import (
	"context"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Networker is implemented by backends that can read and set network
// conditions: offline, latency and bandwidth.
type Networker interface {
	NetworkConditions(ctx context.Context) (device.NetworkConditions, error)
	SetOffline(ctx context.Context, offline bool) error
	// Shape replaces the latency and rate limits; zeros remove them.
	Shape(ctx context.Context, latencyMs, downloadKbps, uploadKbps int) (device.NetworkConditions, error)
}

// NetworkConditions reads airplane mode, the default network and the shaping.
func (u *UIA2) NetworkConditions(ctx context.Context) (device.NetworkConditions, error) {
	return u.adb.NetworkConditions(ctx)
}

// SetOffline switches airplane mode and waits for the network to follow.
func (u *UIA2) SetOffline(ctx context.Context, offline bool) error {
	return u.adb.SetOffline(ctx, offline)
}

// Shape shapes traffic, which needs root.
func (u *UIA2) Shape(ctx context.Context, latencyMs, downloadKbps, uploadKbps int) (device.NetworkConditions, error) {
	return androidShape(ctx, u.adb, latencyMs, downloadKbps, uploadKbps)
}

// NetworkConditions reads airplane mode, the default network and the shaping.
func (a *Android) NetworkConditions(ctx context.Context) (device.NetworkConditions, error) {
	return a.adb.NetworkConditions(ctx)
}

// SetOffline switches airplane mode and waits for the network to follow.
func (a *Android) SetOffline(ctx context.Context, offline bool) error {
	return a.adb.SetOffline(ctx, offline)
}

// Shape shapes traffic, which needs root.
func (a *Android) Shape(ctx context.Context, latencyMs, downloadKbps, uploadKbps int) (device.NetworkConditions, error) {
	return androidShape(ctx, a.adb, latencyMs, downloadKbps, uploadKbps)
}

// androidShape refuses where there is no root to shape with: a phone's user
// build — the Pixel 8 Pro's — has no su, and airplane mode is all it takes.
func androidShape(ctx context.Context, adb *device.ADB, latencyMs, downloadKbps, uploadKbps int) (device.NetworkConditions, error) {
	if (latencyMs > 0 || downloadKbps > 0 || uploadKbps > 0) && !adb.CanShape(ctx) {
		return device.NetworkConditions{}, mobiumerr.New(mobiumerr.Unsupported, "latency and bandwidth are set by "+
			"shaping the device's traffic, which needs root, and this device does not give it — a phone's user "+
			"build has no su. Offline works here; for latency and bandwidth, use an emulator").
			WithRemedy("offline true, or run the flow on an emulator")
	}
	return adb.Shape(ctx, latencyMs, downloadKbps, uploadKbps)
}

// NetworkConditions is refused: iOS has no outside control of the network —
// simctl has no network command,
// and the only Apple tool, the Network Link Conditioner, is a system-wide
// preference pane that would throttle the test runner with the app.
func (w *WDA) NetworkConditions(context.Context) (device.NetworkConditions, error) {
	return device.NetworkConditions{}, iosNoNetwork()
}

// SetOffline is refused on iOS; see NetworkConditions.
func (w *WDA) SetOffline(context.Context, bool) error { return iosNoNetwork() }

// Shape is refused on iOS; see NetworkConditions.
func (w *WDA) Shape(context.Context, int, int, int) (device.NetworkConditions, error) {
	return device.NetworkConditions{}, iosNoNetwork()
}

func iosNoNetwork() error {
	return mobiumerr.New(mobiumerr.Unsupported, "iOS has no outside control of the network: simctl has no network "+
		"command, and Apple's Network Link Conditioner is a system-wide setting that would slow the test runner "+
		"along with the app. Network conditions are Android's, on an emulator for latency and bandwidth").
		WithRemedy("run the flow on Android")
}
