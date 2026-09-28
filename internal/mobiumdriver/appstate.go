package mobiumdriver

import (
	"context"
	"net/http"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// AppState reads an app's state from the device.
func (a *Android) AppState(ctx context.Context, appID string) (device.AppState, error) {
	return a.adb.AppState(ctx, appID)
}

// Background sends the app home for d and resumes it.
func (a *Android) Background(ctx context.Context, appID string, d time.Duration) error {
	return backgroundAndroid(ctx, a.adb, appID, d)
}

// AppState reads an app's state from the device.
func (u *UIA2) AppState(ctx context.Context, appID string) (device.AppState, error) {
	return u.adb.AppState(ctx, appID)
}

// Background sends the app home for d and resumes it.
func (u *UIA2) Background(ctx context.Context, appID string, d time.Duration) error {
	return backgroundAndroid(ctx, u.adb, appID, d)
}

// backgroundLeaveWait bounds how long the app has to leave after Home.
const backgroundLeaveWait = 3 * time.Second

// backgroundAndroid presses Home, confirms the app left, waits, and starts
// its launcher activity again — which brings its existing task to the front
// rather than starting over: `am start` answers "Activity not started, its
// current task has been brought to the front", and the app came back on the
// screen it was left on, in the same process (measured on Settings, three
// levels in, and on Wikipedia).
func backgroundAndroid(ctx context.Context, adb *device.ADB, pkg string, d time.Duration) error {
	if home, err := adb.HomePackage(ctx); err == nil && home == pkg {
		return homeInFront()
	}
	start := time.Now()
	if err := adb.PressKey(ctx, "KEYCODE_HOME"); err != nil {
		return err
	}
	for {
		st, err := adb.AppState(ctx, pkg)
		if err != nil {
			return err
		}
		if st.State != device.AppForeground {
			break
		}
		if time.Since(start) > backgroundLeaveWait {
			return mobiumerr.New(mobiumerr.NotConfirmed, "pressed Home and %s stayed in front", pkg)
		}
		if err := sleepCtx(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
	if err := sleepCtx(ctx, d-time.Since(start)); err != nil {
		return err
	}
	return adb.LaunchApp(ctx, pkg)
}

// sleepCtx waits for d, or until ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// AppState asks XCTest, through WebDriverAgent. It cannot tell an app that is
// not installed from one that is not running — a bundle id that does not
// exist reads as not running — so that one reading is checked against the
// installed apps.
func (w *WDA) AppState(ctx context.Context, appID string) (device.AppState, error) {
	var out struct {
		Value int `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/wda/apps/state"),
		map[string]interface{}{"bundleId": appID}, &out); err != nil {
		return device.AppState{}, err
	}
	yes, no := true, false
	switch out.Value {
	case 4:
		return device.AppState{State: device.AppForeground}, nil
	case 3:
		return device.AppState{State: device.AppBackground, Suspended: &no}, nil
	case 2:
		return device.AppState{State: device.AppBackground, Suspended: &yes}, nil
	case 1:
		apps, err := w.ListApps(ctx, true)
		if err != nil {
			return device.AppState{}, err
		}
		for _, a := range apps {
			if a.ID == appID {
				return device.AppState{State: device.AppNotRunning}, nil
			}
		}
		return device.AppState{State: device.AppNotInstalled}, nil
	}
	return device.AppState{}, mobiumerr.New(mobiumerr.DeviceServer,
		"WebDriverAgent reported state %d for %s, which XCTest calls unknown", out.Value, appID)
}

// Background is XCTest's own: Home, a wait, and the app activated again.
// WebDriverAgent answers one request at a time, so nothing can watch it leave;
// a simctl screenshot taken halfway showed the home screen.
func (w *WDA) Background(ctx context.Context, appID string, d time.Duration) error {
	if appID == springboardBundleID {
		return homeInFront()
	}
	// On a phone the switch back is announced, as a launch is (CHALLENGES 71).
	w.expectApp(ctx, appID)
	err := w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/wda/deactivateApp"),
		map[string]interface{}{"duration": d.Seconds()}, nil)
	if err != nil {
		w.clearExpected(ctx)
	}
	return err
}

// homeInFront refuses to send the home screen to itself.
func homeInFront() error {
	return mobiumerr.New(mobiumerr.InvalidArgument, "the home screen is in front, so there is no app to send away").
		WithRemedy("app_launch the app first, or name it with app")
}
