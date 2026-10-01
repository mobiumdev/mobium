package mobiumdriver

import (
	"context"
	"net/http"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// The time zone on iOS, as TZ in the environment of every launch.
//
// Android's time zone is the device's, and changing it changes every app at
// once. iOS offers no such setting from outside: a simulator follows the
// Mac, and a phone changes only in Settings, which would move its owner's
// clock. But an app takes its time zone from TZ when it is set, so the zone
// lives in the session, as the language does (wdalocale.go), and goes into
// the environment of every launch Mobium makes: only apps Mobium launches
// see it, until it is set back to the device's own zone or the session ends.
// Measured on an iPhone 17 Pro simulator: Calendar launched with
// TZ=Asia/Tokyo marked 11:00 AM in progress, the hour in Tokyo, against
// 7:00 PM on the Mac's zone, and the next plain launch marked 7:00 PM again.

func (w *WDA) sessionZone() string {
	w.localeMu.Lock()
	defer w.localeMu.Unlock()
	return w.zone
}

// Timezone reports the zone apps are launched in here: the session's, when
// one is set, and otherwise the device's own.
func (w *WDA) Timezone(ctx context.Context) (string, error) {
	if z := w.sessionZone(); z != "" {
		return z, nil
	}
	return w.deviceZone(ctx)
}

func (w *WDA) deviceZone(ctx context.Context) (string, error) {
	var resp struct {
		Value struct {
			TimeZone string `json:"timeZone"`
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, w.w3c.sessionPath("/wda/device/info"), nil, &resp); err != nil {
		return "", err
	}
	return resp.Value.TimeZone, nil
}

// SetTimezone sets the zone for every launch from here, and launches the app
// in front again so it is in it now. The device's own zone clears it. A zone
// the IANA database does not know is refused, since an app given one falls
// back to UTC without saying so.
func (w *WDA) SetTimezone(ctx context.Context, tz string) error {
	if _, err := time.LoadLocation(tz); err != nil || tz == "" || tz == "Local" {
		return mobiumerr.New(mobiumerr.InvalidArgument, "%q is not a time zone the IANA database knows — give "+
			"one like \"Asia/Tokyo\" or \"America/New_York\"", tz)
	}
	device, err := w.deviceZone(ctx)
	if err != nil {
		return err
	}
	w.localeMu.Lock()
	if tz == device {
		w.zone = ""
	} else {
		w.zone = tz
	}
	w.localeMu.Unlock()

	front, err := w.Snapshot(ctx)
	if err != nil {
		return err
	}
	app := front.Package()
	if app == "" || app == springboardBundleID {
		return nil
	}
	// Stopped and started through WebDriverAgent, as the language is, with
	// whatever the session now holds — no TZ once it is cleared.
	_ = w.phoneTerminate(ctx, app)
	return w.launchWith(ctx, app, w.pinnedLocale(app), w.sessionZone())
}
