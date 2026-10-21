package mobiumdriver

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Accessibility settings, delegated to the device layer, which holds each
// platform's table and confirms every write by reading it back.

// AccessibilitySetting reads one accessibility setting.
func (a *Android) AccessibilitySetting(ctx context.Context, name string) (string, error) {
	return a.adb.AccessibilitySetting(ctx, name)
}

// SetAccessibilitySetting changes one accessibility setting.
func (a *Android) SetAccessibilitySetting(ctx context.Context, name, value string) (device.AXUndo, error) {
	return a.adb.SetAccessibilitySetting(ctx, name, value)
}

// AccessibilitySetting reads one accessibility setting.
func (u *UIA2) AccessibilitySetting(ctx context.Context, name string) (string, error) {
	return u.adb.AccessibilitySetting(ctx, name)
}

// SetAccessibilitySetting changes one accessibility setting.
func (u *UIA2) SetAccessibilitySetting(ctx context.Context, name, value string) (device.AXUndo, error) {
	return u.adb.SetAccessibilitySetting(ctx, name, value)
}

// AccessibilitySetting reads one accessibility setting: from simctl on a
// simulator, and on a phone from the Settings app's own switch.
func (w *WDA) AccessibilitySetting(ctx context.Context, name string) (string, error) {
	if name == device.AXScreenReader {
		return w.voiceOverSetting(ctx)
	}
	if w.phone != nil {
		return w.phoneAX(ctx, name, "")
	}
	return w.sim.AccessibilitySetting(ctx, name)
}

// SetAccessibilitySetting changes one accessibility setting, confirmed by
// reading it back, and returns how to put it back: on a phone, the same
// switch in Settings flipped back.
func (w *WDA) SetAccessibilitySetting(ctx context.Context, name, value string) (device.AXUndo, error) {
	if name == device.AXScreenReader {
		return w.setVoiceOver(ctx, value)
	}
	if w.phone == nil {
		return w.sim.SetAccessibilitySetting(ctx, name, value)
	}
	// What the change will be undone to is read from the switch itself, never
	// from what the last visit to Settings saw: a read answers from that
	// visit for ten seconds, and only a change through Mobium forgets it, so
	// a switch flipped any other way — a test's own tap in Settings, a
	// person — was recorded as it had been, and the restore put back the
	// opposite of what was there. A phone left with Reduce Motion off after
	// a run that found it on fits exactly this (ROADMAP's open lead, #118).
	// CHALLENGES 243.
	w.axSeen.forget()
	was, err := w.phoneAX(ctx, name, "")
	if err != nil {
		return nil, err
	}
	w.axPend.note(name, was)
	axLog(w.phone, "record %s was %s, before setting it %s", name, was, value)
	// Every setting's undo puts back all of them: the first to run does the
	// work, in one visit per Settings page, and the rest find it done.
	undo := w.restoreAX
	if _, err := w.phoneAX(ctx, name, value); err != nil {
		return undo, err
	}
	return undo, nil
}

// VoiceOver as a setting, through WebDriverAgent's VoiceOver routes, which
// a runner built with Xcode 27 has on iOS 27 (CHALLENGES 348).

// voiceOverWait bounds waiting for VoiceOver to report the state it was
// asked for.
const voiceOverWait = 5 * time.Second

func (w *WDA) voiceOverRefusal() error {
	switch {
	case w.tv():
		return mobiumerr.New(mobiumerr.Unsupported, "an Apple TV's VoiceOver is not set by Mobium: WebDriverAgent's VoiceOver routes are iOS's")
	case w.phone == nil:
		return mobiumerr.New(mobiumerr.Unsupported, "VoiceOver is not set on a simulator: Mobium's simulator runner is "+
			"WebDriverAgent's prebuilt one, built before Xcode 27, which has no VoiceOver control — turn it on in "+
			"the simulator's Settings > Accessibility > VoiceOver")
	}
	return mobiumerr.New(mobiumerr.Unsupported, "this iPhone's runner cannot reach VoiceOver: it needs iOS 27 and a "+
		"runner built with Xcode 27, which Mobium rebuilds when the Xcode installed changes")
}

func (w *WDA) voiceOverSetting(ctx context.Context) (string, error) {
	if w.tv() || w.phone == nil {
		return "", w.voiceOverRefusal()
	}
	on, known := w.ScreenReaderOn(ctx)
	if !known {
		return "", w.voiceOverRefusal()
	}
	if on {
		return device.AXOn, nil
	}
	return device.AXOff, nil
}

// setVoiceOver turns VoiceOver on or off, waits for WebDriverAgent to report
// it so, and returns how to set it back as found.
func (w *WDA) setVoiceOver(ctx context.Context, value string) (device.AXUndo, error) {
	was, err := w.voiceOverSetting(ctx)
	if err != nil {
		return nil, err
	}
	var want bool
	switch strings.ToLower(strings.TrimSpace(value)) {
	case device.AXOn, "true", "1", "yes":
		want = true
	case device.AXOff, "false", "0", "no":
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "screen_reader is on or off — got %q", value)
	}
	set := func(ctx context.Context, on bool) error {
		route := "/wda/voiceOver/disable"
		if on {
			route = "/wda/voiceOver/enable"
		}
		if err := w.w3c.do(ctx, http.MethodPost, route, map[string]interface{}{}, nil); err != nil {
			return fmt.Errorf("turn VoiceOver %s: %w", map[bool]string{true: "on", false: "off"}[on], err)
		}
		deadline := time.Now().Add(voiceOverWait)
		for {
			if now, known := w.ScreenReaderOn(ctx); known && now == on {
				return nil
			}
			if time.Now().After(deadline) {
				return mobiumerr.New(mobiumerr.NotConfirmed, "asked for VoiceOver %s and the phone does not report it",
					map[bool]string{true: "on", false: "off"}[on])
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	undo := func(ctx context.Context) error { return set(ctx, was == device.AXOn) }
	return undo, set(ctx, want)
}
