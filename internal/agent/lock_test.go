package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// lockedDriver is a device whose screen reads come from a script, for the
// ways a lock meets an action. CHALLENGES 389.
type lockedDriver struct {
	*buttonDriver
	reads  []func() (*uitree.Tree, error)
	tapped int
	asleep bool
}

func (d *lockedDriver) ScreenAsleep(ctx context.Context) (bool, error) { return d.asleep, nil }

func (d *lockedDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	read := d.reads[0]
	if len(d.reads) > 1 {
		d.reads = d.reads[1:]
	}
	return read()
}

func (d *lockedDriver) Tap(ctx context.Context, x, y int) error {
	d.tapped++
	return nil
}

func withLock(t *testing.T, locked bool, reads ...func() (*uitree.Tree, error)) (*Handlers, *session, *lockedDriver) {
	t.Helper()
	d := &lockedDriver{buttonDriver: &buttonDriver{locked: locked}, reads: reads}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	h.sessions["fake"] = s
	return h, s, d
}

func unreadable() (*uitree.Tree, error) { return nil, errors.New("could not read the UI hierarchy") }

func keyguard(t *testing.T) func() (*uitree.Tree, error) {
	t.Helper()
	tree, err := uitree.ParseAndroid([]byte(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" package="com.android.systemui" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]" />` +
		`</hierarchy>`))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return func() (*uitree.Tree, error) { return tree, nil }
}

func TestAMissOnALockScreenSaysTheDeviceIsLocked(t *testing.T) {
	h, s, d := withLock(t, true, keyguard(t))
	_, err := tap(h, s, map[string]interface{}{"target": "text=Login"})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "mobium lock unlock") {
		t.Errorf("err = %v, want device_not_ready naming the unlock", err)
	}
	if d.tapped != 0 {
		t.Errorf("touched a locked device %d times", d.tapped)
	}
}

func TestAMissOnTheShellOfAnUnlockedDeviceStaysAMiss(t *testing.T) {
	h, s, _ := withLock(t, false, keyguard(t))
	_, err := tap(h, s, map[string]interface{}{"target": "text=Login"})
	if mobiumerr.CodeOf(err) != mobiumerr.NoSuchElement {
		t.Errorf("err = %v, want no_such_element", err)
	}
}

func TestAPointIsNotTouchedOnADeviceThatCannotBeRead(t *testing.T) {
	h, s, d := withLock(t, true, unreadable)
	_, err := tap(h, s, map[string]interface{}{"x": 10, "y": 10})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady {
		t.Errorf("err = %v, want device_not_ready", err)
	}
	if d.tapped != 0 {
		t.Errorf("touched a locked device %d times", d.tapped)
	}
}

func TestAPointIsTouchedWhenAnUnlockedDeviceCannotBeRead(t *testing.T) {
	h, s, d := withLock(t, false, unreadable)
	if _, err := tap(h, s, map[string]interface{}{"x": 10, "y": 10}); err != nil {
		t.Errorf("err = %v", err)
	}
	if d.tapped != 1 {
		t.Errorf("tapped %d times, want 1", d.tapped)
	}
}

func TestAReadLostWhileSettlingSaysTheDeviceIsLocked(t *testing.T) {
	first := screen(t, "Login")
	h, s, d := withLock(t, true, func() (*uitree.Tree, error) { return first, nil }, unreadable)
	h.settleWindow = time.Millisecond
	h.settleTimeout = 10 * time.Millisecond
	_, err := tap(h, s, map[string]interface{}{"target": "text=Login"})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady {
		t.Errorf("err = %v, want device_not_ready", err)
	}
	if d.tapped != 0 {
		t.Errorf("touched a locked device %d times", d.tapped)
	}
}

func TestAPointIsNotTouchedOnAScreenThatIsOff(t *testing.T) {
	h, s, d := withLock(t, true, keyguard(t))
	d.asleep = true
	_, err := tap(h, s, map[string]interface{}{"x": 10, "y": 10})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "the screen is off") {
		t.Errorf("err = %v, want device_not_ready naming the screen", err)
	}
	if d.tapped != 0 {
		t.Errorf("touched a screen that is off %d times", d.tapped)
	}
}

// A lock screen that is up takes touches: a swipe up, a PIN pad.
func TestAPointIsTouchedOnALockScreenThatIsUp(t *testing.T) {
	h, s, d := withLock(t, true, keyguard(t))
	if _, err := tap(h, s, map[string]interface{}{"x": 10, "y": 10}); err != nil {
		t.Errorf("err = %v", err)
	}
	if d.tapped != 1 {
		t.Errorf("tapped %d times, want 1", d.tapped)
	}
}

func TestARefusalOfAnotherCodeIsNotBlamedOnTheLock(t *testing.T) {
	h, _, _ := withLock(t, true, keyguard(t))
	refused := mobiumerr.New(mobiumerr.NoSuchElement, "no element")
	if got := h.lockedFailure(context.Background(), "app_find", nil, refused); got != refused {
		t.Errorf("got %v", got)
	}
}

func TestAFailedReadOnALockedDeviceNamesTheLock(t *testing.T) {
	h, _, _ := withLock(t, true, keyguard(t))
	read := errors.New("could not read the UI hierarchy")
	got := h.lockedFailure(context.Background(), "app_find", nil, read)
	if mobiumerr.CodeOf(got) != mobiumerr.DeviceNotReady {
		t.Errorf("got %v", got)
	}
	if got := h.lockedFailure(context.Background(), "app_lock", nil, read); got != read {
		t.Errorf("app_lock's own failure: %v", got)
	}
}

func TestAFailedReadOnAnUnlockedDeviceStaysAsItIs(t *testing.T) {
	h, _, _ := withLock(t, false, keyguard(t))
	read := errors.New("could not read the UI hierarchy")
	if got := h.lockedFailure(context.Background(), "app_find", nil, read); got != read {
		t.Errorf("got %v", got)
	}
}

func TestScrollToDoesNotSwipeALockScreen(t *testing.T) {
	h, s, _ := withLock(t, true, keyguard(t))
	_, err := h.scrollToOn(context.Background(), s, map[string]interface{}{"target": "text=Login"})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady {
		t.Errorf("err = %v, want device_not_ready", err)
	}
}

// A lock screen holds nothing of the app, so a target absent from it has not
// gone. iOS answered "is hidden", exit 0. CHALLENGES 391.
func TestAWaitForAbsenceIsRefusedOnALockScreen(t *testing.T) {
	for name, args := range map[string]map[string]interface{}{
		"hidden": {"target": "text=Login", "condition": "hidden", "timeout_ms": 100},
		"not":    {"target": "text=Login", "not": true, "timeout_ms": 100},
		"none":   {"target": "text=Login", "condition": "count", "count": 0, "timeout_ms": 100},
		"timed":  {"target": "text=Login", "timeout_ms": 100},
	} {
		h, s, _ := withLock(t, true, keyguard(t))
		_, err := wait(h, s, args)
		if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "mobium lock unlock") {
			t.Errorf("%s: err = %v, want device_not_ready naming the unlock", name, err)
		}
	}
}

func TestAWaitForAbsenceStaysOnAnUnlockedShell(t *testing.T) {
	h, s, _ := withLock(t, false, keyguard(t))
	if _, err := wait(h, s, map[string]interface{}{"target": "text=Login", "condition": "hidden", "timeout_ms": 100}); err != nil {
		t.Errorf("hidden: %v", err)
	}
	if _, err := wait(h, s, map[string]interface{}{"target": "text=Login", "timeout_ms": 100}); mobiumerr.CodeOf(err) != mobiumerr.Timeout {
		t.Errorf("visible: %v, want timeout", err)
	}
}

func TestAFindMissOnALockScreenNamesTheLock(t *testing.T) {
	h, s, _ := withLock(t, true, keyguard(t))
	h.defaultDevice = s.dev.Serial
	_, err := h.find(context.Background(), map[string]interface{}{"locator": "text=Login", "device": s.dev.Serial})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady {
		t.Errorf("err = %v", err)
	}
}
