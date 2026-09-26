package mobiumdriver

import (
	"context"

	"github.com/mobiumdev/mobium/internal/device"
)

// Placing a device is one capability on Android and one and a half on iOS, so
// the methods are split across two interfaces and the backends implement what
// they can honestly do. See the Geolocation and GeolocationReader docs.

// SetLocation places the device at a coordinate, through a test provider.
func (a *Android) SetLocation(ctx context.Context, lat, lon float64) error {
	return a.adb.SetMockLocation(ctx, lat, lon)
}

// ClearLocation removes the test provider.
func (a *Android) ClearLocation(ctx context.Context) error {
	return a.adb.ClearMockLocation(ctx)
}

// Location reports where the device is, and whether the fix was injected.
func (a *Android) Location(ctx context.Context) (*device.Fix, error) {
	return a.adb.MockLocation(ctx)
}

// SetLocation places the device at a coordinate, through a test provider.
func (u *UIA2) SetLocation(ctx context.Context, lat, lon float64) error {
	return u.adb.SetMockLocation(ctx, lat, lon)
}

// ClearLocation removes the test provider.
func (u *UIA2) ClearLocation(ctx context.Context) error {
	return u.adb.ClearMockLocation(ctx)
}

// Location reports where the device is, and whether the fix was injected.
func (u *UIA2) Location(ctx context.Context) (*device.Fix, error) {
	return u.adb.MockLocation(ctx)
}

// SetLocation places the simulator at a coordinate.
//
// WDA deliberately does **not** implement GeolocationReader: `simctl location`
// has no `get`, so there is nothing to read back. A method returning "the
// coordinate you just asked for" would be a field nothing acts on, which this
// project treats as worse than no field.
func (w *WDA) SetLocation(ctx context.Context, lat, lon float64) error {
	if err := w.simOnly(CapGeolocation); err != nil {
		return err
	}
	return w.sim.SetLocation(ctx, lat, lon)
}

// ClearLocation returns the simulator to reporting its own position.
func (w *WDA) ClearLocation(ctx context.Context) error {
	if err := w.simOnly(CapGeolocation); err != nil {
		return err
	}
	return w.sim.ClearLocation(ctx)
}

// StartRoute hands a route to the simulator, which interpolates it itself.
//
// Android has no equivalent, so `Android` and `UIA2` deliberately do not
// implement RouteRunner: the tool layer steps them through SetLocation
// instead. Claiming the capability and then emulating it here would put the
// same loop one layer lower and hide which platform is doing what.
func (w *WDA) StartRoute(ctx context.Context, pts []device.Point, speedMPS float64) error {
	if err := w.simOnly(CapRoutes); err != nil {
		return err
	}
	return w.sim.StartRoute(ctx, pts, speedMPS)
}
