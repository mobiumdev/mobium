package mobiumdriver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// iOS orientation, through WebDriverAgent's rotation endpoint.
//
// Its orientation endpoint is the WebDriver one, which may only answer
// "PORTRAIT" or "LANDSCAPE": it reads both landscapes as LANDSCAPE and both
// portraits as PORTRAIT, so setting the reverse landscape read back as the
// one it was not. The rotation endpoint reads the front app's interface
// orientation in degrees and tells all four apart, so it is the one used,
// both ways. Measured on an iPhone 15 Plus, iOS 26.6.2, in Safari: 270, 90
// and 0 each read back as set, and the window's size turned with them.
//
// Degrees are WebDriverAgent's device orientations: 270 is "landscape left",
// the home side on the right — the device turned counterclockwise, which is
// Android's first quarter turn and so this vocabulary's "landscape".
var iosRotationZ = map[string]int{
	OrientationPortrait:         0,
	OrientationLandscape:        270,
	OrientationPortraitReverse:  180,
	OrientationLandscapeReverse: 90,
}

// iosOrientationForZ is iosRotationZ backwards.
func iosOrientationForZ(z int) (string, error) {
	for mode, deg := range iosRotationZ {
		if deg == z {
			return mode, nil
		}
	}
	return "", mobiumerr.New(mobiumerr.DeviceServer, "WebDriverAgent reported a rotation of %d degrees, which "+
		"is not a quarter turn", z)
}

// Orientation reports which way the front app is turned. Whether rotation is
// locked cannot be read from outside an iPhone or a simulator, so it is
// never claimed: locked is false, and the tool says what that means here.
func (w *WDA) Orientation(ctx context.Context) (string, bool, error) {
	var resp struct {
		Value struct {
			X, Y, Z int
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, w.w3c.sessionPath("/rotation"), nil, &resp); err != nil {
		return "", false, fmt.Errorf("read the orientation: %w", err)
	}
	mode, err := iosOrientationForZ(resp.Value.Z)
	return mode, false, err
}

// orientationWait is how long a turn has to show in what the app reports.
var orientationWait = 2 * time.Second

// SetOrientation turns the front app and reads it back.
//
// An app that supports only some orientations is not turned, and
// WebDriverAgent says so: MobiumApp, portrait only, answered "Unable To
// Rotate Device" and stayed upright. So does a Face ID iPhone asked for
// portrait-reverse, which iOS does not offer on one, even in Safari. Both are
// refused as Android's pinned activity is, with the likeliest cause offered
// rather than asserted.
func (w *WDA) SetOrientation(ctx context.Context, mode string) error {
	if mode == OrientationAuto {
		return mobiumerr.New(mobiumerr.Unsupported, "iOS has nothing that hands rotation back to the device from "+
			"outside: an orientation set here holds until the device is physically turned")
	}
	z, ok := iosRotationZ[mode]
	if !ok {
		_, err := OrientationQuarter(mode)
		return err
	}
	// The turn is waited for, not read once: Safari brought back from the
	// background answered still portrait at once, three times in three, and
	// was taken back; a second later the same request turned it. An app that
	// cannot turn costs the wait, and is refused as before. CHALLENGES 289.
	var setErr, readErr error
	var got string
	deadline := time.Now().Add(orientationWait)
	sent := false
	for {
		// Sent again only while WebDriverAgent refuses it; one it took is
		// read until it shows, so a refused turn is asked for once and taken
		// back once.
		if !sent {
			setErr = w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/rotation"),
				map[string]interface{}{"x": 0, "y": 0, "z": z}, nil)
			sent = setErr == nil
		}
		got, _, readErr = w.Orientation(ctx)
		if setErr == nil && readErr == nil && got == mode {
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if readErr != nil {
		return mobiumerr.New(mobiumerr.NotConfirmed, "asked for %s and could not read the orientation back: %w",
			mode, readErr)
	}
	// The app did not turn, but the request did not go away: WebDriverAgent
	// sets the device's orientation, and the next app opened in it. Settings,
	// portrait only, refused landscape on the iPhone 15 Plus and stayed
	// upright; Wikipedia, launched next, came up in landscape, and so did
	// everything after it until the phone was turned by hand. So the request
	// is taken back, to where the app stayed, before the refusal.
	if back, ok := iosRotationZ[got]; ok {
		_ = w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/rotation"),
			map[string]interface{}{"x": 0, "y": 0, "z": back}, nil)
	}
	cause := "an app that supports only some orientations, which cannot be turned from outside"
	if mode == OrientationPortraitReverse {
		cause = "that a Face ID iPhone never turns upside down, in any app"
	}
	return mobiumerr.New(mobiumerr.NotConfirmed, "asked for %s and the app in front is still %s, and the request "+
		"was taken back so the next app is not turned. The usual cause is %s", mode, got, cause).
		WithDetail("orientation", got)
}
