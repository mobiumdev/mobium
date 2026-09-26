package mobiumdriver

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Mobium's orientation vocabulary.
//
// Four values rather than two, because 180 and 270 are real states an app can
// be put in and the two platforms number them identically — Android's
// `user_rotation` and iOS's device orientation are both quarter turns from the
// device's natural position.
//
// Deliberately not "landscape-left" and "landscape-right". Which of the two
// landscapes is "left" depends on whether you mean the rotation of the device
// or of the image on it, the two platforms' own names disagree, and a caller
// who gets it backwards gets a screen that looks plausible and is upside down.
// "landscape" and "landscape-reverse" are unambiguous about being opposites
// without claiming to know which way anybody turned their wrist.
const (
	OrientationPortrait         = "portrait"
	OrientationLandscape        = "landscape"
	OrientationPortraitReverse  = "portrait-reverse"
	OrientationLandscapeReverse = "landscape-reverse"
	// OrientationAuto hands rotation back to the sensor. It is a valid thing
	// to ask for and never a thing the screen *is*, so reads never return it;
	// they return the actual orientation plus a locked flag.
	OrientationAuto = "auto"
)

// orientationOrder maps the vocabulary to quarter turns, which is what both
// platforms speak underneath.
var orientationOrder = []string{
	OrientationPortrait,
	OrientationLandscape,
	OrientationPortraitReverse,
	OrientationLandscapeReverse,
}

// OrientationQuarter converts a name to quarter turns from natural.
func OrientationQuarter(mode string) (int, error) {
	for i, name := range orientationOrder {
		if name == mode {
			return i, nil
		}
	}
	return 0, mobiumerr.New(mobiumerr.InvalidArgument, "unknown orientation %q (want %s, or %q)",
		mode, orientationNames(), OrientationAuto)
}

// OrientationName converts quarter turns to a name.
func OrientationName(quarter int) (string, error) {
	if quarter < 0 || quarter >= len(orientationOrder) {
		return "", mobiumerr.New(mobiumerr.DeviceServer, "the device reported rotation %d, which is not a quarter turn", quarter)
	}
	return orientationOrder[quarter], nil
}

func orientationNames() string {
	out := ""
	for i, n := range orientationOrder {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%q", n)
	}
	return out
}

// Orientation reports which way the screen is turned.
func (a *Android) Orientation(ctx context.Context) (string, bool, error) {
	return androidOrientation(ctx, a.adb)
}

// SetOrientation turns the screen, confirming it moved.
func (a *Android) SetOrientation(ctx context.Context, mode string) error {
	return androidSetOrientation(ctx, a.adb, mode)
}

// Orientation reports which way the screen is turned.
func (u *UIA2) Orientation(ctx context.Context) (string, bool, error) {
	return androidOrientation(ctx, u.adb)
}

// SetOrientation turns the screen, confirming it moved.
//
// Deliberately through adb rather than WebDriver's own /orientation endpoint.
// That endpoint rotates the *session's* idea of the screen and the UiAutomator2
// server's notion of it, which can drift from what the display is doing;
// `cmd window user-rotation` moves the display itself and can be read back from
// `dumpsys`, so the change can be confirmed rather than assumed.
func (u *UIA2) SetOrientation(ctx context.Context, mode string) error {
	return androidSetOrientation(ctx, u.adb, mode)
}

func androidOrientation(ctx context.Context, adb interface {
	Rotation(context.Context) (int, error)
	RotationLocked(context.Context) (bool, error)
}) (string, bool, error) {
	quarter, err := adb.Rotation(ctx)
	if err != nil {
		return "", false, err
	}
	name, err := OrientationName(quarter)
	if err != nil {
		return "", false, err
	}
	locked, err := adb.RotationLocked(ctx)
	if err != nil {
		return "", false, err
	}
	return name, locked, nil
}

func androidSetOrientation(ctx context.Context, adb interface {
	SetRotation(context.Context, int) error
	SetRotationAuto(context.Context) error
}, mode string) error {
	if mode == OrientationAuto {
		return adb.SetRotationAuto(ctx)
	}
	quarter, err := OrientationQuarter(mode)
	if err != nil {
		return err
	}
	return adb.SetRotation(ctx, quarter)
}

// AppLocales reports the language pinned for an app.
func (a *Android) AppLocales(ctx context.Context, appID string) ([]string, error) {
	return a.adb.AppLocales(ctx, appID)
}

// SetAppLocales pins an app's language and confirms the device stored it.
func (a *Android) SetAppLocales(ctx context.Context, appID string, tags []string) error {
	return a.adb.SetAppLocales(ctx, appID, tags)
}

// DeviceLocale reports what an unpinned app follows.
func (a *Android) DeviceLocale(ctx context.Context) (string, error) {
	return a.adb.DeviceLocale(ctx)
}

// AppLocales reports the language pinned for an app.
func (u *UIA2) AppLocales(ctx context.Context, appID string) ([]string, error) {
	return u.adb.AppLocales(ctx, appID)
}

// SetAppLocales pins an app's language and confirms the device stored it.
func (u *UIA2) SetAppLocales(ctx context.Context, appID string, tags []string) error {
	return u.adb.SetAppLocales(ctx, appID, tags)
}

// DeviceLocale reports what an unpinned app follows.
func (u *UIA2) DeviceLocale(ctx context.Context) (string, error) {
	return u.adb.DeviceLocale(ctx)
}
