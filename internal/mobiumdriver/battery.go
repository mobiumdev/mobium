package mobiumdriver

import (
	"context"
	"net/http"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Battery reads the battery service.
func (a *Android) Battery(ctx context.Context) (device.Battery, error) { return a.adb.Battery(ctx) }

// Battery reads the battery service.
func (u *UIA2) Battery(ctx context.Context) (device.Battery, error) { return u.adb.Battery(ctx) }

// Now reads the device's clock and zone.
func (a *Android) Now(ctx context.Context) (device.ClockReading, error) {
	return androidNow(ctx, a.adb)
}

// Now reads the device's clock and zone.
func (u *UIA2) Now(ctx context.Context) (device.ClockReading, error) { return androidNow(ctx, u.adb) }

func androidNow(ctx context.Context, adb *device.ADB) (device.ClockReading, error) {
	t, err := adb.Now(ctx)
	if err != nil {
		return device.ClockReading{}, err
	}
	zone, err := adb.Timezone(ctx)
	if err != nil {
		return device.ClockReading{}, err
	}
	return device.ClockReading{Time: t, Zone: zone, Source: device.ClockDevice}, nil
}

// Battery asks UIDevice, through WebDriverAgent. A simulator has no battery:
// it reports level -1 and state unknown, measured on the iPhone 17 Pro
// simulator, and that is what it is called here rather than a number.
func (w *WDA) Battery(ctx context.Context) (device.Battery, error) {
	var out struct {
		Value struct {
			Level float64 `json:"level"`
			State int     `json:"state"`
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, w.w3c.sessionPath("/wda/batteryInfo"), nil, &out); err != nil {
		return device.Battery{}, err
	}
	if out.Value.Level < 0 {
		return device.Battery{Present: false, Level: -1, State: "unknown"}, nil
	}
	b := device.Battery{Present: true, Level: int(out.Value.Level*100 + 0.5)}
	// UIDeviceBatteryState: unknown, unplugged, charging, full.
	switch out.Value.State {
	case 1:
		b.State = "discharging"
	case 2:
		b.State = "charging"
	case 3:
		b.State = "full"
	default:
		b.State = "unknown"
	}
	return b, nil
}

// Now is the phone's own clock, from lockdown; a simulator's is the Mac's,
// in the zone WebDriverAgent reports for it.
func (w *WDA) Now(ctx context.Context) (device.ClockReading, error) {
	if w.phone != nil {
		return device.PhoneNow(ctx, w.phone.Phone.UDID)
	}
	var out struct {
		Value struct {
			TimeZone string `json:"timeZone"`
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, w.w3c.sessionPath("/wda/device/info"), nil, &out); err != nil {
		return device.ClockReading{}, err
	}
	loc, err := time.LoadLocation(out.Value.TimeZone)
	if err != nil {
		return device.ClockReading{}, mobiumerr.New(mobiumerr.DeviceServer,
			"the simulator's zone %q is not one this Mac knows: %v", out.Value.TimeZone, err)
	}
	return device.ClockReading{Time: time.Now().In(loc), Zone: out.Value.TimeZone, Source: device.ClockMac}, nil
}
