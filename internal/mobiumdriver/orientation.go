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

// OrientationKeeper saves the orientation as it is, for a session to put it
// back when it ends.
type OrientationKeeper interface {
	SaveOrientation(context.Context) (func(context.Context) error, error)
}

// userRotator is the rotation request Android keeps: locked or following
// the sensor, and to which quarter.
type userRotator interface {
	UserRotation(context.Context) (bool, int, error)
	RestoreUserRotation(context.Context, bool, int) error
}

func androidSetOrientation(ctx context.Context, adb interface {
	Rotation(context.Context) (int, error)
	SetRotation(context.Context, int) error
	SetRotationAuto(context.Context) error
	userRotator
}, mode string) error {
	if mode == OrientationAuto {
		return adb.SetRotationAuto(ctx)
	}
	quarter, err := OrientationQuarter(mode)
	if err != nil {
		return err
	}
	locked, asked, saveErr := adb.UserRotation(ctx)
	err = adb.SetRotation(ctx, quarter)
	if err == nil || mobiumerr.CodeOf(err) != mobiumerr.NotConfirmed || saveErr != nil {
		return err
	}
	// The app did not turn, and the request stays: Android's turn is a
	// device setting, and the lock it sets turns auto-rotate off. MobiumApp,
	// portrait only, refused landscape on the Android 15 emulator, and
	// Settings, opened next, came up in landscape with auto-rotate off —
	// CHALLENGES 218 on iOS, measured here only later (308). So the request
	// is put back as it was before the refusal, as iOS takes its back.
	if rerr := adb.RestoreUserRotation(ctx, locked, asked); rerr != nil {
		return mobiumerr.New(mobiumerr.NotConfirmed, "asked for %s and the app in front did not turn, and putting "+
			"the rotation back as it was failed too, so the next app may open turned: %w", mode, rerr)
	}
	got := "unread"
	if q, rerr := adb.Rotation(ctx); rerr == nil {
		if name, nerr := OrientationName(q); nerr == nil {
			got = name
		}
	}
	return mobiumerr.New(mobiumerr.NotConfirmed, "asked for %s and the app in front is still %s, and the request "+
		"was taken back so the next app is not turned. The usual cause is an app that supports only some "+
		"orientations, which cannot be turned from outside", mode, got).
		WithDetail("orientation", got)
}

// androidSaveOrientation keeps the rotation request as it is, and returns
// the undo that puts it back.
func androidSaveOrientation(ctx context.Context, adb userRotator) (func(context.Context) error, error) {
	locked, quarter, err := adb.UserRotation(ctx)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error { return adb.RestoreUserRotation(ctx, locked, quarter) }, nil
}

// SaveOrientation keeps the rotation as it is, lock and all, and returns
// the undo that puts it back.
func (a *Android) SaveOrientation(ctx context.Context) (func(context.Context) error, error) {
	return androidSaveOrientation(ctx, a.adb)
}

// SaveOrientation keeps the rotation as it is, lock and all, and returns
// the undo that puts it back.
func (u *UIA2) SaveOrientation(ctx context.Context) (func(context.Context) error, error) {
	return androidSaveOrientation(ctx, u.adb)
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
