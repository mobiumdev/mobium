package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// press is app_press: a hardware button.
//
// On Android **back is primary navigation** — an app that opens a detail
// screen expects it — and no amount of tapping what `map` shows substitutes.
func (h *Handlers) press(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.pressOn(ctx, s, args)
}

func (h *Handlers) pressOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsButtons(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapButtons, "press hardware buttons")
	}

	button := strings.ToLower(strings.TrimSpace(stringArg(args, "button")))
	if button == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_press needs a button — this device has %s",
			mobiumdriver.ButtonNames(ctrl.SupportedButtons()))
	}
	// Two checks, deliberately. A name that is not in the vocabulary at all is
	// a typo and is caught here, before any device is touched. A name that is
	// real but absent on this platform is passed to the driver, because only
	// the driver can say *why* — that iOS has no back button and what to do
	// instead is worth far more than a list of what it does have.
	known := false
	for _, b := range mobiumdriver.AllButtons() {
		if b == button {
			known = true
			break
		}
	}
	if !known {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "there is no %q button — mobium knows %s",
			button, mobiumdriver.ButtonNames(mobiumdriver.AllButtons()))
	}

	// Home is the one press with an outcome worth checking: the launcher
	// should come to the front. The others have no platform-wide signal —
	// what `back` does is the app's business, and volume moved nothing
	// measurable on a device with no audio playing — so they are reported as
	// sent rather than as confirmed, which is the honest difference.
	var before string
	if button == mobiumdriver.ButtonHome {
		before, _ = h.screenNow(ctx, s)
	}

	if err := ctrl.Press(ctx, button); err != nil {
		return nil, err
	}

	// Any press can move the screen, so refs from the last map are gone.
	delete(h.refs, s.dev.Serial)

	view := PressView{Button: button, Device: s.dev.Serial}
	if button == mobiumdriver.ButtonHome {
		if after, ok := h.awaitLauncher(ctx, s, before); ok {
			view.Confirmed, view.Foreground = true, after
			return Result(fmt.Sprintf("pressed home — %s is in the foreground", after), view), nil
		}
	}
	return Result(fmt.Sprintf("pressed %s", button), view), nil
}

// awaitLauncher waits for the home screen to come forward after a home press.
func (h *Handlers) awaitLauncher(ctx context.Context, s *session, before string) (string, bool) {
	deadline := time.Now().Add(3 * time.Second)
	for {
		app, _ := h.screenNow(ctx, s)
		if app != "" && app != before {
			return app, true
		}
		if time.Now().After(deadline) {
			return "", false
		}
		select {
		case <-ctx.Done():
			return "", false
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// PressView is the result of app_press.
type PressView struct {
	Button string `json:"button"`
	// Confirmed distinguishes a press whose effect was checked from one that
	// was merely sent. Only home has a platform-wide outcome to check.
	Confirmed  bool   `json:"confirmed"`
	Foreground string `json:"foreground,omitempty"`
	Device     string `json:"device"`
}

// lock is app_lock: read whether the screen is locked, or lock and unlock it.
//
// A state rather than a power-button press, because power is a toggle: asking
// twice leaves the device where it started and the caller cannot tell which
// way it went.
func (h *Handlers) lock(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.lockOn(ctx, s, args)
}

func (h *Handlers) lockOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsScreenLock(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapScreenLock, "lock or unlock the screen")
	}

	want := strings.ToLower(strings.TrimSpace(stringArg(args, "state")))
	switch want {
	case "", "lock", "locked", "unlock", "unlocked":
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown state %q (want \"lock\" or \"unlock\", "+
			"or omit it to read)", want)
	}

	before, err := ctrl.ScreenLocked(ctx)
	if err != nil {
		return nil, err
	}
	if want == "" {
		return Result(lockWord(before), LockView{Locked: before, Device: s.dev.Serial}), nil
	}

	locked := want == "lock" || want == "locked"
	if before == locked {
		return Result(fmt.Sprintf("already %s", lockWord(locked)),
			LockView{Locked: locked, Device: s.dev.Serial}), nil
	}
	if err := ctrl.SetScreenLocked(ctx, locked); err != nil {
		return nil, err
	}
	// The screen behind a lock is not the screen that comes back.
	delete(h.refs, s.dev.Serial)

	return Result(fmt.Sprintf("%s (was %s)", lockWord(locked), lockWord(before)),
		LockView{Locked: locked, Previous: &before, Device: s.dev.Serial}), nil
}

func lockWord(locked bool) string {
	if locked {
		return "locked"
	}
	return "unlocked"
}

// LockView is the result of app_lock.
type LockView struct {
	Locked   bool   `json:"locked"`
	Previous *bool  `json:"previous,omitempty"`
	Device   string `json:"device"`
}
