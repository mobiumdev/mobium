package agent

import (
	"context"
	"fmt"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// BatteryView is the result of app_battery.
type BatteryView struct {
	Device string `json:"device"`
	// Present is false on a device with no battery — an iOS simulator.
	Present bool `json:"present"`
	// Level is the charge in percent; absent when there is no battery.
	Level *int `json:"level,omitempty"`
	// State is charging, discharging, not_charging, full or unknown.
	State string `json:"state"`
	// Plugged is ac, usb, wireless, dock or none, on Android; iOS does not
	// say what it is plugged into.
	Plugged string `json:"plugged,omitempty"`
}

// TimeView is the result of app_time.
type TimeView struct {
	Device string `json:"device"`
	// Time is the device's clock, RFC 3339 with milliseconds, in its own
	// offset from UTC.
	Time string `json:"time"`
	// Zone is the device's IANA timezone.
	Zone string `json:"zone,omitempty"`
	// Clock is "device", or "mac" for a simulator, which has none of its own.
	Clock string `json:"clock"`
}

func (h *Handlers) battery(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	r, ok := mobiumdriver.AsBatteryReader(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapBattery, "read the battery")
	}
	b, err := r.Battery(ctx)
	if err != nil {
		return nil, err
	}
	view := BatteryView{Device: s.dev.Serial, Present: b.Present, State: b.State, Plugged: b.Plugged}
	if !b.Present {
		return Result("no battery — a simulator has none", view), nil
	}
	msg := fmt.Sprintf("%d%%, %s", b.Level, b.State)
	if b.Level >= 0 {
		level := b.Level
		view.Level = &level
	} else {
		msg = "level unknown, " + b.State
	}
	switch b.Plugged {
	case "":
	case "none":
		msg += ", not plugged in"
	default:
		msg += ", plugged into " + b.Plugged
	}
	return Result(msg, view), nil
}

func (h *Handlers) deviceTime(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	c, ok := mobiumdriver.AsDeviceClock(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapDeviceClock, "read the device's clock")
	}
	r, err := c.Now(ctx)
	if err != nil {
		return nil, err
	}
	stamp := r.Time.Format("2006-01-02T15:04:05.000Z07:00")
	view := TimeView{Device: s.dev.Serial, Time: stamp, Zone: r.Zone, Clock: r.Source}
	msg := stamp
	if r.Zone != "" {
		msg += " " + r.Zone
	}
	if r.Source == device.ClockMac {
		msg += " — the Mac's clock: a simulator has none of its own"
	}
	return Result(msg, view), nil
}
