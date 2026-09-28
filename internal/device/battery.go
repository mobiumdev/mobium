package device

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Battery is the battery as the device reports it.
type Battery struct {
	// Present is false on a device with no battery — an iOS simulator.
	Present bool
	// Level is the charge in percent, 0-100; -1 when the device does not say.
	Level int
	// State is "charging", "discharging", "not_charging", "full" or "unknown".
	State string
	// Plugged is what powers it — "ac", "usb", "wireless", "dock" or "none" —
	// where the platform says; iOS does not, and leaves it empty.
	Plugged string
}

var batteryLineRe = regexp.MustCompile(`(?m)^\s*([A-Za-z ]+):\s*(\S+)\s*$`)

// Battery reads `dumpsys battery`. Measured live on the Pixel 7 AVD: the
// console's `power capacity 37`, `power ac off` and `power status
// discharging` came back as level 37, not powered and status 3, and back
// again — so it is the battery service's own state, not a constant.
func (a *ADB) Battery(ctx context.Context) (Battery, error) {
	out, err := a.Shell(ctx, "dumpsys", "battery")
	if err != nil {
		return Battery{}, err
	}
	return parseBattery(string(out))
}

func parseBattery(dump string) (Battery, error) {
	v := map[string]string{}
	for _, m := range batteryLineRe.FindAllStringSubmatch(dump, -1) {
		v[strings.ToLower(strings.TrimSpace(m[1]))] = m[2]
	}
	level, err1 := strconv.Atoi(v["level"])
	scale, err2 := strconv.Atoi(v["scale"])
	if err1 != nil || err2 != nil || scale <= 0 {
		return Battery{}, mobiumerr.New(mobiumerr.DeviceServer, "could not read the battery level from dumpsys battery")
	}
	b := Battery{Present: v["present"] != "false", Level: level * 100 / scale, Plugged: "none"}
	// BatteryManager.BATTERY_STATUS_*.
	switch v["status"] {
	case "2":
		b.State = "charging"
	case "3":
		b.State = "discharging"
	case "4":
		b.State = "not_charging"
	case "5":
		b.State = "full"
	default:
		b.State = "unknown"
	}
	for _, p := range []struct{ key, name string }{
		{"ac powered", "ac"}, {"usb powered", "usb"}, {"wireless powered", "wireless"}, {"dock powered", "dock"},
	} {
		if v[p.key] == "true" {
			b.Plugged = p.name
			break
		}
	}
	return b, nil
}

// Now reads the device's own clock, in its own offset from UTC.
//
// One `date` call for both, so the time and its offset cannot straddle a
// change of zone. The emulator's clock follows the Mac's — within the round
// trip, measured — but a phone's is its own.
func (a *ADB) Now(ctx context.Context) (time.Time, error) {
	// Quoted: the format has a space, and adb shell hands the device one
	// command line, which would split it in two (CHALLENGES 52).
	out, err := a.Shell(ctx, "date "+shellQuote("+%s.%N %z"))
	if err != nil {
		return time.Time{}, err
	}
	return parseDeviceDate(strings.TrimSpace(string(out)))
}

// parseDeviceDate reads "1790625653.233458676 -0700".
func parseDeviceDate(s string) (time.Time, error) {
	fields := strings.Fields(s)
	if len(fields) != 2 {
		return time.Time{}, mobiumerr.New(mobiumerr.DeviceServer, "could not read the device's clock from %q", s)
	}
	sec, frac, _ := strings.Cut(fields[0], ".")
	secs, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return time.Time{}, mobiumerr.New(mobiumerr.DeviceServer, "could not read the device's clock from %q", s)
	}
	var nanos int64
	if frac != "" {
		frac = (frac + "000000000")[:9]
		nanos, _ = strconv.ParseInt(frac, 10, 64)
	}
	off, err := time.Parse("-0700", fields[1])
	if err != nil {
		return time.Time{}, mobiumerr.New(mobiumerr.DeviceServer, "could not read the device's offset from %q", s)
	}
	_, offset := off.Zone()
	return time.Unix(secs, nanos).In(time.FixedZone(fields[1], offset)), nil
}

// Where a clock reading came from.
const (
	// ClockDevice is the device's own clock.
	ClockDevice = "device"
	// ClockMac is the Mac's: a simulator runs on it and has no clock of its
	// own. Measured through Safari's Date.now() on the iPhone 17 Pro
	// simulator: within 19-25ms of the Mac's, the round trip.
	ClockMac = "mac"
)

// ClockReading is what time a device says it is, and in what zone.
type ClockReading struct {
	Time time.Time
	// Zone is the IANA name where the device gives one.
	Zone string
	// Source is ClockDevice or ClockMac.
	Source string
}

// PhoneNow reads a real iPhone's clock and zone from lockdown.
func PhoneNow(ctx context.Context, udid string) (ClockReading, error) {
	v, err := LockdownValues(ctx, udid, "TimeIntervalSince1970", "TimeZone", "TimeZoneOffsetFromUTC")
	if err != nil {
		return ClockReading{}, err
	}
	secs, ok1 := number(v["TimeIntervalSince1970"])
	offset, ok2 := number(v["TimeZoneOffsetFromUTC"])
	zone, _ := v["TimeZone"].(string)
	if !ok1 || !ok2 {
		return ClockReading{}, mobiumerr.New(mobiumerr.DeviceServer, "lockdown gave the phone's clock as %v and its offset as %v",
			v["TimeIntervalSince1970"], v["TimeZoneOffsetFromUTC"])
	}
	whole := int64(secs)
	t := time.Unix(whole, int64((secs-float64(whole))*1e9))
	return ClockReading{Time: t.In(time.FixedZone(zone, int(offset))), Zone: zone, Source: ClockDevice}, nil
}

// number reads a plist number, which arrives as an integer or a real.
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}
