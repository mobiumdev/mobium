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

// AccessibilitySetting reads one accessibility setting on a simulator. A
// phone is declined through HasCapability; this is the backstop.
func (w *WDA) AccessibilitySetting(ctx context.Context, name string) (string, error) {
	if err := w.simOnly(CapAccessibility); err != nil {
		return "", err
	}
	return w.sim.AccessibilitySetting(ctx, name)
}

// SetAccessibilitySetting changes one accessibility setting on a simulator.
func (w *WDA) SetAccessibilitySetting(ctx context.Context, name, value string) (device.AXUndo, error) {
	if err := w.simOnly(CapAccessibility); err != nil {
		return nil, err
	}
	return w.sim.SetAccessibilitySetting(ctx, name, value)
}
