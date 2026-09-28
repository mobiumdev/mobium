package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
)

type powerDriver struct {
	fakeDriver
	battery device.Battery
	clock   device.ClockReading
}

func (d *powerDriver) Battery(ctx context.Context) (device.Battery, error)  { return d.battery, nil }
func (d *powerDriver) Now(ctx context.Context) (device.ClockReading, error) { return d.clock, nil }

func withPower(t *testing.T, d *powerDriver) *Handlers {
	t.Helper()
	h := NewHandlers()
	h.sessions["fake"] = &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	return h
}

func TestBatterySaysWhatTheDeviceSays(t *testing.T) {
	for _, c := range []struct {
		b    device.Battery
		want string
	}{
		{device.Battery{Present: true, Level: 15, State: "discharging", Plugged: "none"}, "15%, discharging, not plugged in"},
		{device.Battery{Present: true, Level: 100, State: "charging", Plugged: "ac"}, "100%, charging, plugged into ac"},
		{device.Battery{Present: true, Level: 80, State: "charging"}, "80%, charging"},
		{device.Battery{Present: false, Level: -1, State: "unknown"}, "no battery"},
	} {
		h := withPower(t, &powerDriver{battery: c.b})
		res, err := h.Call("app_battery", map[string]interface{}{})
		if err != nil || res.Content[0].Text != c.want && !strings.HasPrefix(res.Content[0].Text, c.want) {
			t.Errorf("%+v: %v, %v, want %q", c.b, res, err, c.want)
			continue
		}
		v := res.StructuredContent.(BatteryView)
		if (v.Level == nil) == c.b.Present {
			t.Errorf("%+v: level %v, want one only when there is a battery", c.b, v.Level)
		}
	}
}

// A simulator's clock is the Mac's, and the answer says so rather than
// presenting it as the device's own.
func TestTimeNamesItsClock(t *testing.T) {
	at := time.Date(2026, 9, 28, 13, 5, 49, 884e6, time.FixedZone("PDT", -7*3600))
	h := withPower(t, &powerDriver{clock: device.ClockReading{Time: at, Zone: "America/Los_Angeles", Source: device.ClockMac}})
	res, err := h.Call("app_time", map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content[0].Text != "2026-09-28T13:05:49.884-07:00 America/Los_Angeles — the Mac's clock: a simulator has none of its own" {
		t.Errorf("text = %q", res.Content[0].Text)
	}
	if v := res.StructuredContent.(TimeView); v.Clock != "mac" || v.Time != "2026-09-28T13:05:49.884-07:00" {
		t.Errorf("view = %+v", v)
	}
}
