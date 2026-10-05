package mobiumdriver

import (
	"context"

	"github.com/mobiumdev/mobium/internal/device"
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
	if w.phone != nil {
		return w.phoneAX(ctx, name, "")
	}
	return w.sim.AccessibilitySetting(ctx, name)
}

// SetAccessibilitySetting changes one accessibility setting, confirmed by
// reading it back, and returns how to put it back: on a phone, the same
// switch in Settings flipped back.
func (w *WDA) SetAccessibilitySetting(ctx context.Context, name, value string) (device.AXUndo, error) {
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
