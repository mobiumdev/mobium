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
	// SetProxy points the device's HTTP proxy at host:port, or takes it off
	// for "", and waits for it to be in force.
	SetProxy(ctx context.Context, hostport string) error
	// ReversePort makes the device's port on its loopback reach the same
	// port on this machine, or stops it, and says whether it changed
	// anything: a reverse already there is not added again.
	ReversePort(ctx context.Context, port int, on bool) (changed bool, err error)
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

// SetProxy writes the device's proxy setting and reads it back.
func (u *UIA2) SetProxy(ctx context.Context, hostport string) error {
	return u.adb.SetProxy(ctx, hostport)
}

// ReversePort adds or removes an adb reverse.
func (u *UIA2) ReversePort(ctx context.Context, port int, on bool) (bool, error) {
	return reversePort(ctx, u.adb, port, on)
}

// SetProxy writes the device's proxy setting and reads it back.
func (a *Android) SetProxy(ctx context.Context, hostport string) error {
	return a.adb.SetProxy(ctx, hostport)
}

// ReversePort adds or removes an adb reverse.
func (a *Android) ReversePort(ctx context.Context, port int, on bool) (bool, error) {
	return reversePort(ctx, a.adb, port, on)
}

func reversePort(ctx context.Context, adb *device.ADB, port int, on bool) (bool, error) {
	if on {
		return adb.Reverse(ctx, port)
	}
	return true, adb.Unreverse(ctx, port)
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

// SetProxy is refused on iOS; see NetworkConditions.
func (w *WDA) SetProxy(context.Context, string) error { return iosNoNetwork() }

// ReversePort is refused on iOS; see NetworkConditions.
func (w *WDA) ReversePort(context.Context, int, bool) (bool, error) { return false, iosNoNetwork() }

func iosNoNetwork() error {
	return mobiumerr.New(mobiumerr.Unsupported, "iOS has no outside control of the network: simctl has no network "+
		"command, and Apple's Network Link Conditioner is a system-wide setting that would slow the test runner "+
		"along with the app. Network conditions are Android's, on an emulator for latency and bandwidth").
		WithRemedy("run the flow on Android")
}
