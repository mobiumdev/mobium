package mobiumdriver

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"sort"
	"strings"
)

// Mobium's hardware button vocabulary.
//
// Neutral names rather than platform keycodes, for the same reason locators
// are neutral. What matters more is what is *absent*: **iOS has no back
// button.** Navigation back is a per-app affordance there — a chevron in a
// navigation bar, or an edge swipe that an app can decline. Mapping `back` to
// an edge swipe on iOS would be inventing an event the platform never sends,
// and an app can tell the difference. So the driver refuses it and says why.
const (
	ButtonBack       = "back"
	ButtonHome       = "home"
	ButtonRecents    = "recents"
	ButtonVolumeUp   = "volume-up"
	ButtonVolumeDown = "volume-down"

	// The D-pad and its center, select: how a TV is driven, where a touch
	// tap only moves focus on some views (measured on a Fire TV's launcher)
	// and a remote is the input every app is built for. A phone takes them
	// too, as keyboard navigation.
	ButtonDpadUp    = "dpad-up"
	ButtonDpadDown  = "dpad-down"
	ButtonDpadLeft  = "dpad-left"
	ButtonDpadRight = "dpad-right"
	ButtonSelect    = "select"

	// A remote's media keys, sent to whichever media session holds them.
	ButtonPlayPause   = "play-pause"
	ButtonStop        = "stop"
	ButtonNext        = "next"
	ButtonPrevious    = "previous"
	ButtonRewind      = "rewind"
	ButtonFastForward = "fast-forward"
)

// DpadButtons are the presses that move focus, which is what they are read
// back by.
func DpadButtons() []string {
	return []string{ButtonDpadUp, ButtonDpadDown, ButtonDpadLeft, ButtonDpadRight}
}

// Buttons is implemented by backends that can press hardware buttons.
//
// Separate from Gesturer because these are not screen events: they do not have
// coordinates, they cannot be aimed at an element, and which of them exist is
// a property of the platform rather than of the screen.
type Buttons interface {
	// Press sends one hardware button. A backend refuses a button its
	// platform does not have rather than approximating it.
	Press(ctx context.Context, button string) error
	// SupportedButtons lists what this platform actually has, so a caller can
	// ask before being refused.
	SupportedButtons() []string
}

// ScreenLock is implemented by backends that can lock and unlock the screen,
// and report which it is.
//
// Named as a state rather than a "power button" press on purpose: power is a
// toggle, so asking for it twice leaves the device where it started and the
// caller cannot tell which way it went. Lock and unlock are idempotent and
// can be confirmed.
type ScreenLock interface {
	ScreenLocked(ctx context.Context) (bool, error)
	SetScreenLocked(ctx context.Context, locked bool) error
}

// androidKeycodes maps the vocabulary to what `input keyevent` understands.
var androidKeycodes = map[string]string{
	ButtonBack:       "KEYCODE_BACK",
	ButtonHome:       "KEYCODE_HOME",
	ButtonRecents:    "KEYCODE_APP_SWITCH",
	ButtonVolumeUp:   "KEYCODE_VOLUME_UP",
	ButtonVolumeDown: "KEYCODE_VOLUME_DOWN",

	ButtonDpadUp:    "KEYCODE_DPAD_UP",
	ButtonDpadDown:  "KEYCODE_DPAD_DOWN",
	ButtonDpadLeft:  "KEYCODE_DPAD_LEFT",
	ButtonDpadRight: "KEYCODE_DPAD_RIGHT",
	ButtonSelect:    "KEYCODE_DPAD_CENTER",

	ButtonPlayPause:   "KEYCODE_MEDIA_PLAY_PAUSE",
	ButtonStop:        "KEYCODE_MEDIA_STOP",
	ButtonNext:        "KEYCODE_MEDIA_NEXT",
	ButtonPrevious:    "KEYCODE_MEDIA_PREVIOUS",
	ButtonRewind:      "KEYCODE_MEDIA_REWIND",
	ButtonFastForward: "KEYCODE_MEDIA_FAST_FORWARD",
}

// ButtonNames lists the vocabulary, sorted, for error messages.
func ButtonNames(supported []string) string {
	out := append([]string(nil), supported...)
	sort.Strings(out)
	for i, s := range out {
		out[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(out, ", ")
}

// AllButtons is Mobium's whole vocabulary, which is not the same as what any
// one platform has. The tool layer checks against this so a typo never reaches
// a device; each driver checks against its own list so the refusal can say why
// that platform lacks it, which is the part a caller can act on.
func AllButtons() []string { return androidButtons() }

// androidButtons is what both Android backends support: every button in
// the vocabulary, since `input keyevent` sends any of them.
func androidButtons() []string {
	return []string{ButtonBack, ButtonHome, ButtonRecents, ButtonVolumeUp, ButtonVolumeDown,
		ButtonDpadUp, ButtonDpadDown, ButtonDpadLeft, ButtonDpadRight, ButtonSelect,
		ButtonPlayPause, ButtonStop, ButtonNext, ButtonPrevious, ButtonRewind, ButtonFastForward}
}

type keyPresser interface {
	PressKey(context.Context, string) error
}

func pressAndroid(ctx context.Context, adb keyPresser, button string) error {
	code, ok := androidKeycodes[button]
	if !ok {
		return mobiumerr.New(mobiumerr.InvalidArgument, "unknown button %q (Android has %s)",
			button, ButtonNames(androidButtons()))
	}
	return adb.PressKey(ctx, code)
}

// Press sends a hardware button.
func (a *Android) Press(ctx context.Context, button string) error {
	return pressAndroid(ctx, a.adb, button)
}

// SupportedButtons lists Android's hardware buttons.
func (a *Android) SupportedButtons() []string { return androidButtons() }

// ScreenLocked reports whether the screen is off or showing the lock screen.
func (a *Android) ScreenLocked(ctx context.Context) (bool, error) {
	return a.adb.ScreenLocked(ctx)
}

// SetScreenLocked locks or unlocks the screen and confirms it.
func (a *Android) SetScreenLocked(ctx context.Context, locked bool) error {
	return a.adb.SetScreenLocked(ctx, locked)
}

// Press sends a hardware button.
func (u *UIA2) Press(ctx context.Context, button string) error {
	return pressAndroid(ctx, u.adb, button)
}

// SupportedButtons lists Android's hardware buttons.
func (u *UIA2) SupportedButtons() []string { return androidButtons() }

// ScreenLocked reports whether the screen is off or showing the lock screen.
func (u *UIA2) ScreenLocked(ctx context.Context) (bool, error) {
	return u.adb.ScreenLocked(ctx)
}

// SetScreenLocked locks or unlocks the screen and confirms it.
func (u *UIA2) SetScreenLocked(ctx context.Context, locked bool) error {
	return u.adb.SetScreenLocked(ctx, locked)
}

// iosButtons is what WebDriverAgent can press on a simulator.
//
// **No back.** iOS has no back button: navigation back is a per-app
// affordance — a chevron in a navigation bar, or an edge swipe an app may
// decline. Sending an edge swipe in its place would be a different event, and
// an app can tell, so `press back` is refused here with that explanation
// rather than quietly approximated.
//
// No recents either: the app switcher is reached by a gesture or a double
// home press, not a button, and WebDriverAgent exposes no endpoint for it.
func iosButtons() []string {
	return []string{ButtonHome, ButtonVolumeUp, ButtonVolumeDown}
}

// iosButtonNames maps the vocabulary onto WebDriverAgent's spelling.
var iosButtonNames = map[string]string{
	ButtonHome:       "home",
	ButtonVolumeUp:   "volumeUp",
	ButtonVolumeDown: "volumeDown",
}

// tvButtons is what the Apple TV remote has, as WebDriverAgent presses it
// on a tvOS simulator. Back is the remote's own Menu button — the platform's
// back, which every tvOS app answers — and the Siri Remote's only media key
// is Play/Pause.
func tvButtons() []string {
	return []string{ButtonBack, ButtonHome, ButtonDpadUp, ButtonDpadDown, ButtonDpadLeft, ButtonDpadRight,
		ButtonSelect, ButtonPlayPause}
}

// tvButtonNames maps the vocabulary onto WebDriverAgent's tvOS spelling, an
// XCUIRemote button each.
var tvButtonNames = map[string]string{
	ButtonBack:      "menu",
	ButtonHome:      "home",
	ButtonDpadUp:    "up",
	ButtonDpadDown:  "down",
	ButtonDpadLeft:  "left",
	ButtonDpadRight: "right",
	ButtonSelect:    "select",
	ButtonPlayPause: "playpause",
}

// pressTV sends one of the Apple TV remote's buttons.
func (w *WDA) pressTV(ctx context.Context, button string) error {
	name, ok := tvButtonNames[button]
	if !ok {
		return mobiumerr.New(mobiumerr.Unsupported, "the Apple TV remote has no %q button — it has %s",
			button, ButtonNames(tvButtons()))
	}
	return w.w3c.do(ctx, "POST", w.w3c.sessionPath("/wda/pressButton"),
		map[string]interface{}{"name": name}, nil)
}

// Press sends a hardware button through WebDriverAgent.
func (w *WDA) Press(ctx context.Context, button string) error {
	if w.tv() {
		return w.pressTV(ctx, button)
	}
	name, ok := iosButtonNames[button]
	if !ok {
		if button == ButtonBack {
			return mobiumerr.New(mobiumerr.Unsupported, "iOS has no back button — navigating back is a per-app "+
				"affordance there, usually a chevron in the navigation bar. Mobium will not "+
				"send a swipe in place of a button press: they are different events and an "+
				"app can tell them apart. Tap the chevron, or ask for the swipe by name — "+
				"`mobium press back --gesture`, app_press back with gesture true").
				WithRemedy("map the navigation bar's back button and tap it, or ask for the swipe by " +
					"name: app_press back with gesture true, which starts in the navigation bar, " +
					"where a list row's swipe actions cannot take it")
		}
		if button == ButtonSelect || strings.HasPrefix(button, "dpad-") {
			return mobiumerr.New(mobiumerr.Unsupported, "an iPhone has no D-pad — that is the Apple TV "+
				"remote, which Mobium presses on an Apple TV simulator. Tap the element instead; it has %s",
				ButtonNames(iosButtons()))
		}
		return mobiumerr.New(mobiumerr.Unsupported, "iOS has no %q button — it has %s",
			button, ButtonNames(iosButtons()))
	}
	if button == ButtonHome {
		w.expectApp(ctx, springboardBundleID)
	}
	err := w.w3c.do(ctx, "POST", w.w3c.sessionPath("/wda/pressButton"),
		map[string]interface{}{"name": name}, nil)
	if err != nil {
		w.clearExpected(ctx)
	}
	return err
}

// SupportedButtons lists what WebDriverAgent can press.
func (w *WDA) SupportedButtons() []string {
	if w.tv() {
		return tvButtons()
	}
	return iosButtons()
}

// ScreenLocked reports whether the device is locked.
func (w *WDA) ScreenLocked(ctx context.Context) (bool, error) {
	var out struct {
		Value bool `json:"value"`
	}
	if err := w.w3c.do(ctx, "GET", w.w3c.sessionPath("/wda/locked"), nil, &out); err != nil {
		return false, err
	}
	return out.Value, nil
}

// SetScreenLocked locks or unlocks the device and confirms it.
func (w *WDA) SetScreenLocked(ctx context.Context, locked bool) error {
	path := "/wda/unlock"
	if locked {
		path = "/wda/lock"
	}
	if err := w.w3c.do(ctx, "POST", w.w3c.sessionPath(path), map[string]interface{}{}, nil); err != nil {
		return err
	}
	got, err := w.ScreenLocked(ctx)
	if err != nil {
		return err
	}
	if got != locked {
		return mobiumerr.New(mobiumerr.NotConfirmed, "asked to %s the screen and the device reports it %s",
			map[bool]string{true: "lock", false: "unlock"}[locked],
			map[bool]string{true: "locked", false: "unlocked"}[got])
	}
	return nil
}
