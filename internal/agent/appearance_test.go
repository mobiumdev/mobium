package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// themeDriver is a device with a light/dark setting that can be read back,
// and which records what it was asked to do.
type themeDriver struct {
	fakeDriver
	mode  string
	sets  []string
	stuck bool // reports success and changes nothing
}

func (d *themeDriver) Appearance(ctx context.Context) (string, error) { return d.mode, nil }

func (d *themeDriver) SetAppearance(ctx context.Context, mode string) error {
	d.sets = append(d.sets, mode)
	if d.stuck {
		return nil
	}
	d.mode = mode
	return nil
}

var _ mobiumdriver.Appearance = (*themeDriver)(nil)

func withTheme(t *testing.T, mode string) (*Handlers, *session, *themeDriver) {
	t.Helper()
	d := &themeDriver{mode: mode}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	h.sessions["fake"] = s
	return h, s, d
}

func appearance(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.appearanceOn(ctx, s, args)
}

func TestAppearanceReadsWithoutChanging(t *testing.T) {
	h, sess, d := withTheme(t, "light")
	res, err := appearance(h, sess, map[string]interface{}{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := res.StructuredContent.(AppearanceView).Appearance; got != "light" {
		t.Errorf("appearance = %q", got)
	}
	if len(d.sets) != 0 {
		t.Errorf("a read changed the setting: %v", d.sets)
	}
}

func TestAppearanceChangesAndReportsWhatItWas(t *testing.T) {
	h, sess, d := withTheme(t, "light")
	res, err := appearance(h, sess, map[string]interface{}{"appearance": "dark"})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	view := res.StructuredContent.(AppearanceView)
	if view.Appearance != "dark" || view.Previous != "light" {
		t.Errorf("view = %+v", view)
	}
	if d.mode != "dark" {
		t.Errorf("device is %q", d.mode)
	}
}

func TestAppearanceDoesNothingWhenAlreadyThere(t *testing.T) {
	// Switching costs a re-render and invalidates every ref, so asking for
	// the mode the device is already in should be free.
	h, sess, d := withTheme(t, "dark")
	res, err := appearance(h, sess, map[string]interface{}{"appearance": "dark"})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if len(d.sets) != 0 {
		t.Errorf("re-set a mode that was already active: %v", d.sets)
	}
	if !strings.Contains(textOf(res), "already") {
		t.Errorf("result %q does not say it was already there", textOf(res))
	}
}

func TestChangingAppearanceInvalidatesRefs(t *testing.T) {
	// The refs describe a differently rendered screen. Bounds usually survive
	// a theme change and sometimes do not, and a stale ref taps the wrong
	// thing silently.
	h, sess, _ := withTheme(t, "light")
	h.refs["fake"] = &refTable{entries: map[string]uitree.Locator{
		"@e1": {Kind: uitree.KindText, Value: "Sign in"},
	}}
	if _, err := appearance(h, sess, map[string]interface{}{"appearance": "dark"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := h.locatorFor("fake", "@e1"); err == nil {
		t.Error("a ref from before the theme change still resolved")
	}
}

func TestAppearanceRefusesAnUnknownMode(t *testing.T) {
	h, sess, _ := withTheme(t, "light")
	_, err := appearance(h, sess, map[string]interface{}{"appearance": "sideways"})
	if err == nil {
		t.Fatal("accepted a mode that does not exist")
	}
}

func TestAppearanceBackendWithoutTheCapabilityRefuses(t *testing.T) {
	h, sess, _ := withFake(t, screen(t, "Hello"))
	_, err := appearance(h, sess, map[string]interface{}{})
	if err == nil {
		t.Fatal("a backend with no appearance support answered anyway")
	}
	if !strings.Contains(err.Error(), "cannot read or change the appearance") {
		t.Errorf("error = %v", err)
	}
}

// localeDriver is a stand-in for the Android backends' language support.
type localeDriver struct {
	tags   []string
	device string
	setTo  []string
}

func (l *localeDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) { return nil, nil }
func (l *localeDriver) Screenshot(ctx context.Context) ([]byte, error)     { return nil, nil }
func (l *localeDriver) Tap(ctx context.Context, x, y int) error            { return nil }
func (l *localeDriver) Name() string                                       { return "fake" }
func (l *localeDriver) AppLocales(ctx context.Context, app string) ([]string, error) {
	return l.tags, nil
}
func (l *localeDriver) SetAppLocales(ctx context.Context, app string, tags []string) error {
	l.setTo, l.tags = tags, tags
	return nil
}
func (l *localeDriver) DeviceLocale(ctx context.Context) (string, error) { return l.device, nil }

var _ mobiumdriver.Localization = (*localeDriver)(nil)

func withLocale(t *testing.T) (*Handlers, *session, *localeDriver) {
	t.Helper()
	d := &localeDriver{device: "en-US"}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	h.sessions["fake"] = s
	return h, s, d
}

func callLocale(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.localeOn(ctx, s, args)
}

// TestLocaleArgumentAcceptsBothShapes: a single tag is the common case, a list
// in order of preference is a real thing apps honor, and an empty string is
// how a caller says "follow the device again" — which must be distinguishable
// from not passing the argument at all, since that means "read".
func TestLocaleArgumentAcceptsBothShapes(t *testing.T) {
	for _, tc := range []struct {
		in   interface{}
		want []string
	}{
		{"ja-JP", []string{"ja-JP"}},
		{"en-GB,ja-JP", []string{"en-GB", "ja-JP"}},
		{" ja-JP , en ", []string{"ja-JP", "en"}},
		{"", nil},
		{[]interface{}{"ja-JP", "en"}, []string{"ja-JP", "en"}},
		{[]interface{}{"ja-JP", 7, ""}, []string{"ja-JP"}},
	} {
		got := parseLocaleArg(tc.in)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("parseLocaleArg(%#v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestLocaleNeedsAnApp(t *testing.T) {
	h, s, _ := withLocale(t)
	_, err := callLocale(h, s, map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "per app") {
		t.Errorf("err = %v; the message should say the language is per app", err)
	}
}

// TestLocaleSaysWhatItActuallyConfirmed is the honest-reporting test. Android
// stores "zz-ZZ" as willingly as "ja-JP" and the app then renders in its
// default language, so the result must claim the setting was stored and not
// that the app speaks it.
func TestLocaleSaysWhatItActuallyConfirmed(t *testing.T) {
	h, s, d := withLocale(t)

	res, err := callLocale(h, s, map[string]interface{}{"app": "com.example", "locale": "zz-ZZ"})
	if err != nil {
		t.Fatal(err)
	}
	text := textOf(res)
	if !strings.Contains(text, "stored") {
		t.Errorf("the result does not say what was confirmed: %q", text)
	}
	if !strings.Contains(text, "check the screen") {
		t.Errorf("the result does not say how to find out the rest: %q", text)
	}
	if len(d.setTo) != 1 || d.setTo[0] != "zz-ZZ" {
		t.Errorf("driver received %v", d.setTo)
	}

	// Clearing is a different message, not the same one with an empty tag.
	res, err = callLocale(h, s, map[string]interface{}{"app": "com.example", "locale": ""})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(res), "follows the device") {
		t.Errorf("clearing did not say so: %q", textOf(res))
	}
}

// TestLocaleChangeDropsRefs: every ref names a screen in the old language.
func TestLocaleChangeDropsRefs(t *testing.T) {
	h, s, _ := withLocale(t)
	h.refs[s.dev.Serial] = &refTable{entries: map[string]uitree.Locator{}}
	if _, err := callLocale(h, s, map[string]interface{}{
		"app": "com.example", "locale": "ja-JP",
	}); err != nil {
		t.Fatal(err)
	}
	if _, still := h.refs[s.dev.Serial]; still {
		t.Error("refs survived a language change; they name a screen that no longer exists")
	}
}

// rotateDriver is a device whose orientation can be read back.
type rotateDriver struct {
	mode   string
	locked bool
	sets   []string
	setErr error
}

func (r *rotateDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) { return nil, nil }
func (r *rotateDriver) Screenshot(ctx context.Context) ([]byte, error)     { return nil, nil }
func (r *rotateDriver) Tap(ctx context.Context, x, y int) error            { return nil }
func (r *rotateDriver) Name() string                                       { return "fake" }
func (r *rotateDriver) Orientation(ctx context.Context) (string, bool, error) {
	return r.mode, r.locked, nil
}
func (r *rotateDriver) SetOrientation(ctx context.Context, mode string) error {
	if r.setErr != nil {
		return r.setErr
	}
	r.sets = append(r.sets, mode)
	if mode == mobiumdriver.OrientationAuto {
		r.locked = false
	} else {
		r.mode, r.locked = mode, true
	}
	return nil
}

var _ mobiumdriver.Orientation = (*rotateDriver)(nil)

func withRotation(t *testing.T, mode string, locked bool) (*Handlers, *session, *rotateDriver) {
	t.Helper()
	d := &rotateDriver{mode: mode, locked: locked}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	h.sessions["fake"] = s
	return h, s, d
}

func callOrientation(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.orientationOn(ctx, s, args)
}

// TestOrientationReadsBothFacts: which way the screen points and whether it
// will stay there are different questions, and a test that rotates a device
// which is still following the sensor has not pinned anything.
func TestOrientationReadsBothFacts(t *testing.T) {
	h, s, _ := withRotation(t, mobiumdriver.OrientationPortrait, false)
	res, err := callOrientation(h, s, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(res), "sensor") {
		t.Errorf("an unpinned screen did not say so: %q", textOf(res))
	}

	h, s, _ = withRotation(t, mobiumdriver.OrientationPortrait, true)
	res, _ = callOrientation(h, s, map[string]interface{}{})
	if !strings.Contains(textOf(res), "locked") {
		t.Errorf("a pinned screen did not say so: %q", textOf(res))
	}
}

// TestOrientationAutoIsNeverANoOp: "auto" is about the lock, not the angle, so
// asking for it while already pointing that way must still release the lock.
func TestOrientationAutoIsNeverANoOp(t *testing.T) {
	h, s, d := withRotation(t, mobiumdriver.OrientationPortrait, true)
	if _, err := callOrientation(h, s, map[string]interface{}{
		"orientation": mobiumdriver.OrientationAuto,
	}); err != nil {
		t.Fatal(err)
	}
	if len(d.sets) != 1 {
		t.Fatalf("auto was treated as a no-op: %v", d.sets)
	}
	if d.locked {
		t.Error("the lock survived auto")
	}
}

// TestOrientationSkipsAnAlreadyPinnedMatch, but only when it is pinned: a
// screen that merely happens to point the right way can turn under you.
func TestOrientationSkipsAnAlreadyPinnedMatch(t *testing.T) {
	h, s, d := withRotation(t, mobiumdriver.OrientationLandscape, true)
	res, _ := callOrientation(h, s, map[string]interface{}{"orientation": "landscape"})
	if len(d.sets) != 0 {
		t.Errorf("re-set an orientation already pinned: %v", d.sets)
	}
	if !strings.Contains(textOf(res), "already") {
		t.Errorf("result = %q", textOf(res))
	}

	h, s, d = withRotation(t, mobiumdriver.OrientationLandscape, false)
	if _, err := callOrientation(h, s, map[string]interface{}{"orientation": "landscape"}); err != nil {
		t.Fatal(err)
	}
	if len(d.sets) != 1 {
		t.Error("an unpinned screen pointing the right way was left unpinned")
	}
}

func TestOrientationRejectsLeftAndRight(t *testing.T) {
	h, s, d := withRotation(t, mobiumdriver.OrientationPortrait, true)
	_, err := callOrientation(h, s, map[string]interface{}{"orientation": "landscape-left"})
	if err == nil {
		t.Fatal("accepted landscape-left, which the two platforms name differently")
	}
	if len(d.sets) != 0 {
		t.Error("a rejected orientation still reached the device")
	}
}

// TestOrientationChangeDropsRefs: bounds do not survive a rotation, so a stale
// ref taps a coordinate that now names something else.
func TestOrientationChangeDropsRefs(t *testing.T) {
	h, s, _ := withRotation(t, mobiumdriver.OrientationPortrait, true)
	h.refs[s.dev.Serial] = &refTable{entries: map[string]uitree.Locator{}}
	if _, err := callOrientation(h, s, map[string]interface{}{"orientation": "landscape"}); err != nil {
		t.Fatal(err)
	}
	if _, still := h.refs[s.dev.Serial]; still {
		t.Error("refs survived a rotation")
	}
}

// buttonDriver is a device with hardware buttons and a lock.
type buttonDriver struct {
	pressed   []string
	buttons   []string
	locked    bool
	setLocked []bool
	tree      *uitree.Tree
	// afterBack, when set, is the screen once back has been pressed.
	afterBack *uitree.Tree
	// after is the screen once a button has been pressed, by button.
	after map[string]*uitree.Tree
}

func (b *buttonDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) { return b.tree, nil }
func (b *buttonDriver) Screenshot(ctx context.Context) ([]byte, error)     { return nil, nil }
func (b *buttonDriver) Tap(ctx context.Context, x, y int) error            { return nil }
func (b *buttonDriver) Name() string                                       { return "fake" }
func (b *buttonDriver) Press(ctx context.Context, button string) error {
	for _, s := range b.buttons {
		if s == button {
			b.pressed = append(b.pressed, button)
			if button == mobiumdriver.ButtonBack && b.afterBack != nil {
				b.tree = b.afterBack
			}
			if t, ok := b.after[button]; ok {
				b.tree = t
			}
			return nil
		}
	}
	return errors.New("this platform has no " + button)
}
func (b *buttonDriver) SupportedButtons() []string                     { return b.buttons }
func (b *buttonDriver) ScreenLocked(ctx context.Context) (bool, error) { return b.locked, nil }
func (b *buttonDriver) SetScreenLocked(ctx context.Context, v bool) error {
	b.setLocked = append(b.setLocked, v)
	b.locked = v
	return nil
}

var (
	_ mobiumdriver.Buttons    = (*buttonDriver)(nil)
	_ mobiumdriver.ScreenLock = (*buttonDriver)(nil)
)

func withButtons(t *testing.T, buttons ...string) (*Handlers, *session, *buttonDriver) {
	t.Helper()
	d := &buttonDriver{buttons: buttons}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	h.sessions["fake"] = s
	return h, s, d
}

func callPress(h *Handlers, s *session, button string) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.pressOn(ctx, s, map[string]interface{}{"button": button})
}

// TestPressRejectsATypoBeforeTouchingTheDevice: a name outside the vocabulary
// is a mistake, and catching it here means it never reaches a device.
func TestPressRejectsATypoBeforeTouchingTheDevice(t *testing.T) {
	h, s, d := withButtons(t, mobiumdriver.AllButtons()...)
	_, err := callPress(h, s, "middle")
	if err == nil {
		t.Fatal("accepted a button that does not exist")
	}
	if len(d.pressed) != 0 {
		t.Error("a nonsense button still reached the device")
	}
	if !strings.Contains(err.Error(), "back") {
		t.Errorf("the error does not list the vocabulary: %v", err)
	}
}

// TestPressLetsTheDriverExplainAPlatformGap: "iOS has no back button" is worth
// far more than "this device has back, home, recents" — it says what to do
// instead — so a real button the platform lacks must reach the driver.
func TestPressLetsTheDriverExplainAPlatformGap(t *testing.T) {
	h, s, _ := withButtons(t, mobiumdriver.ButtonHome)
	_, err := callPress(h, s, mobiumdriver.ButtonBack)
	if err == nil {
		t.Fatal("pressed a button the platform does not have")
	}
	if !strings.Contains(err.Error(), "this platform has no back") {
		t.Errorf("the driver's own explanation was replaced: %v", err)
	}
}

func TestPressDropsRefs(t *testing.T) {
	h, s, _ := withButtons(t, mobiumdriver.AllButtons()...)
	h.refs[s.dev.Serial] = &refTable{entries: map[string]uitree.Locator{}}
	if _, err := callPress(h, s, mobiumdriver.ButtonBack); err != nil {
		t.Fatal(err)
	}
	if _, still := h.refs[s.dev.Serial]; still {
		t.Error("refs survived a button press that can move the screen")
	}
}

// TestPressReportsOnlyWhatItChecked: home and back have outcomes worth
// reading; volume has none, and claiming one would be the same overreach as
// reporting a set that was never read back.
func TestPressReportsOnlyWhatItChecked(t *testing.T) {
	h, s, d := withButtons(t, mobiumdriver.AllButtons()...)
	d.tree = screenOf(t, "dev.mobium.mobiumapp", "Login")
	res, err := callPress(h, s, mobiumdriver.ButtonVolumeUp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(textOf(res), "foreground") {
		t.Errorf("volume claimed a confirmed outcome: %q", textOf(res))
	}
}

func screenOf(t *testing.T, pkg, text string) *uitree.Tree {
	t.Helper()
	tree, err := uitree.ParseAndroid([]byte(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" package="` + pkg + `" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		`<node index="0" package="` + pkg + `" class="android.widget.TextView" text="` + text + `" ` +
		`bounds="[100,200][900,280]" /></node></hierarchy>`))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// Back says what it did. It closing the app and it going back a screen are
// the difference MobiumApp's report was about — every back from a demo
// closed the app, and Mobium answered "pressed back" (docs/BACK.md).
func TestBackSaysWhetherItLeftTheApp(t *testing.T) {
	h, s, d := withButtons(t, mobiumdriver.AllButtons()...)
	d.tree = screenOf(t, "dev.mobium.mobiumapp", "Login")
	d.afterBack = screenOf(t, "com.google.android.apps.nexuslauncher", "Clock")
	res, err := callPress(h, s, mobiumdriver.ButtonBack)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(res), "it left dev.mobium.mobiumapp") {
		t.Errorf("a back that closed the app did not say so: %q", textOf(res))
	}

	d.tree = screenOf(t, "dev.mobium.mobiumapp", "Login")
	d.afterBack = screenOf(t, "dev.mobium.mobiumapp", "Home")
	res, err = callPress(h, s, mobiumdriver.ButtonBack)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(res), "dev.mobium.mobiumapp is still in the foreground") {
		t.Errorf("a back inside the app did not say it stayed: %q", textOf(res))
	}
}

// Only back has a gesture, and asking for one elsewhere is refused before
// anything is touched. Where the device navigates with buttons the gesture
// is not back at all, and that is refused too — tested on the device, since
// the mode is read from it.
func TestOnlyBackHasAGesture(t *testing.T) {
	h, s, d := withButtons(t, mobiumdriver.AllButtons()...)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	_, err := h.pressOn(ctx, s, map[string]interface{}{"button": "home", "gesture": true})
	if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument || len(d.pressed) != 0 {
		t.Errorf("a home gesture was not refused before touching the device: %v, pressed %v", err, d.pressed)
	}
}

func callLock(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.lockOn(ctx, s, args)
}

func TestLockIsAStateNotAToggle(t *testing.T) {
	h, s, d := withButtons(t, mobiumdriver.AllButtons()...)

	// Asking twice for the same state must not flip it back — the whole
	// reason this is not modeled as a power-button press.
	for i := 0; i < 2; i++ {
		if _, err := callLock(h, s, map[string]interface{}{"state": "lock"}); err != nil {
			t.Fatal(err)
		}
	}
	if !d.locked {
		t.Fatal("locking twice left the screen unlocked")
	}
	if len(d.setLocked) != 1 {
		t.Errorf("the driver was asked %d times for a state it already had", len(d.setLocked))
	}

	res, err := callLock(h, s, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(res), "locked") {
		t.Errorf("read = %q", textOf(res))
	}
}

func TestLockRejectsNonsense(t *testing.T) {
	h, s, _ := withButtons(t, mobiumdriver.AllButtons()...)
	if _, err := callLock(h, s, map[string]interface{}{"state": "sideways"}); err == nil {
		t.Error("accepted a state that is neither lock nor unlock")
	}
}

// notifyDriver is a device with a notification shade.
type notifyDriver struct {
	list     []device.Notification
	posted   []string
	shade    []bool
	postFail error
}

func (n *notifyDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) { return nil, nil }
func (n *notifyDriver) Screenshot(ctx context.Context) ([]byte, error)     { return nil, nil }
func (n *notifyDriver) Tap(ctx context.Context, x, y int) error            { return nil }
func (n *notifyDriver) Name() string                                       { return "fake" }
func (n *notifyDriver) Notifications(ctx context.Context) ([]device.Notification, error) {
	return n.list, nil
}
func (n *notifyDriver) PostNotification(ctx context.Context, tag, title, text string) error {
	if n.postFail != nil {
		return n.postFail
	}
	n.posted = append(n.posted, tag+"|"+title+"|"+text)
	n.list = append(n.list, device.Notification{Package: "com.android.shell", Tag: tag, Title: title, Text: text})
	return nil
}
func (n *notifyDriver) SetShade(ctx context.Context, open bool) error {
	n.shade = append(n.shade, open)
	return nil
}

var _ mobiumdriver.Notifications = (*notifyDriver)(nil)

func withNotifications(t *testing.T, list ...device.Notification) (*Handlers, *session, *notifyDriver) {
	t.Helper()
	d := &notifyDriver{list: list}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	h.sessions["fake"] = s
	return h, s, d
}

func callNotifications(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.notificationsOn(ctx, s, args)
}

// TestNotificationsReadIsTheAssertion: listing the shade is how a test checks
// an app posted what it should, so the package, title and text all have to
// come back rather than a count.
func TestNotificationsReadIsTheAssertion(t *testing.T) {
	h, s, _ := withNotifications(t, device.Notification{
		Package: "com.example.shop", Title: "Order shipped", Text: "Arriving Friday",
	})
	res, err := callNotifications(h, s, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"com.example.shop", "Order shipped", "Arriving Friday"} {
		if !strings.Contains(textOf(res), want) {
			t.Errorf("the listing does not carry %q: %q", want, textOf(res))
		}
	}
}

func TestNotificationsEmptySaysSo(t *testing.T) {
	h, s, _ := withNotifications(t)
	res, err := callNotifications(h, s, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(res), "no notifications") {
		t.Errorf("an empty shade reported %q", textOf(res))
	}
}

func TestNotificationsPostDefaultsATitle(t *testing.T) {
	h, s, d := withNotifications(t)
	if _, err := callNotifications(h, s, map[string]interface{}{"text": "hello"}); err != nil {
		t.Fatal(err)
	}
	if len(d.posted) != 1 || !strings.Contains(d.posted[0], "|Mobium|hello") {
		t.Errorf("posted %v", d.posted)
	}
}

// TestNotificationsShadeDropsRefs: the shade covers the app, so every ref from
// the last map names something that is no longer reachable.
func TestNotificationsShadeDropsRefs(t *testing.T) {
	h, s, d := withNotifications(t)
	h.refs[s.dev.Serial] = &refTable{entries: map[string]uitree.Locator{}}
	if _, err := callNotifications(h, s, map[string]interface{}{"shade": "open"}); err != nil {
		t.Fatal(err)
	}
	if _, still := h.refs[s.dev.Serial]; still {
		t.Error("refs survived the shade opening over the app")
	}
	if len(d.shade) != 1 || !d.shade[0] {
		t.Errorf("shade calls = %v", d.shade)
	}

	// Reading alone must not touch the shade or the refs.
	h.refs[s.dev.Serial] = &refTable{entries: map[string]uitree.Locator{}}
	if _, err := callNotifications(h, s, map[string]interface{}{}); err != nil {
		t.Fatal(err)
	}
	if _, still := h.refs[s.dev.Serial]; !still {
		t.Error("merely reading the shade discarded the refs")
	}
}

func TestNotificationsRejectsAnUnknownShadeState(t *testing.T) {
	h, s, d := withNotifications(t)
	if _, err := callNotifications(h, s, map[string]interface{}{"shade": "sideways"}); err == nil {
		t.Error("accepted a shade state that is neither open nor close")
	}
	if len(d.shade) != 0 {
		t.Error("a rejected state still reached the device")
	}
}

// TestNotificationsPostFailureSurfaces: posting is confirmed by reading the
// shade back, so a post that never arrives must be an error rather than a
// cheerful listing of everything else.
func TestNotificationsPostFailureSurfaces(t *testing.T) {
	h, s, d := withNotifications(t)
	d.postFail = errors.New("it is not in the shade")
	_, err := callNotifications(h, s, map[string]interface{}{"text": "hello"})
	if err == nil || !strings.Contains(err.Error(), "not in the shade") {
		t.Errorf("err = %v", err)
	}
}

// A launch onto a locked device used to report success, naming an app that
// was behind the lock screen (measured on a Pixel 8 Pro whose screen had
// gone to sleep). It is refused now, as device_not_ready, and the remedy it
// gives has to be one the schema accepts — the first draft named a `lock
// off` command and a `locked` argument, neither of which exists.
func TestALaunchOntoALockedDeviceIsRefused(t *testing.T) {
	h, s, d := withButtons(t)
	ctx := context.Background()

	if err := h.lockedInstead(ctx, s, "launched x"); err != nil {
		t.Errorf("an unlocked device was refused: %v", err)
	}
	d.locked = true
	err := h.lockedInstead(ctx, s, "launched x")
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady {
		t.Fatalf("a locked device gave %v, want device_not_ready", err)
	}
	for _, want := range []string{"mobium lock unlock", `app_lock with state "unlock"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the remedy lacks %q: %v", want, err)
		}
	}
	for _, tool := range GetToolSchemas() {
		if tool.Name != "app_lock" {
			continue
		}
		props := tool.InputSchema["properties"].(map[string]interface{})
		state, ok := props["state"].(map[string]interface{})
		if !ok || !strings.Contains(fmt.Sprint(state["enum"]), "unlock") {
			t.Errorf("app_lock no longer takes state \"unlock\", which the remedy names: %v", props)
		}
	}

	// A backend that cannot report a lock is not second-guessed.
	plain := &session{dev: fakeDevice(), driver: &fakeDriver{}, backend: BackendUIA2}
	if err := h.lockedInstead(ctx, plain, "launched x"); err != nil {
		t.Errorf("a driver with no lock state was refused: %v", err)
	}
}

// tvRow is a row of tiles as a Fire TV's Settings draws them — a focusable
// container whose child carries the words — with focus on one, or on none
// when focused is "".
func tvRow(t *testing.T, focused string) *uitree.Tree {
	t.Helper()
	xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" package="com.amazon.tv.launcher" class="android.widget.FrameLayout" bounds="[0,0][1920,1080]">`
	for i, label := range []string{"Network", "Applications"} {
		x := 100 + i*400
		xml += fmt.Sprintf(`<node index="%d" package="com.amazon.tv.launcher" class="android.widget.LinearLayout" `+
			`clickable="true" focusable="true" focused="%v" bounds="[%d,800][%d,1000]">`+
			`<node index="0" package="com.amazon.tv.launcher" class="android.widget.TextView" text="%s" `+
			`bounds="[%d,900][%d,960]" /></node>`, i, label == focused, x, x+300, label, x, x+300)
	}
	tree, err := uitree.ParseAndroid([]byte(xml + `</node></hierarchy>`))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// A D-pad press is read back by focus, the one thing a TV reports about where
// the user is: moved is confirmed, staying put at the end of a row is said
// to have not moved, and a screen where nothing reports focus — an app that
// draws to one surface — is not passed off as a success.
func TestDpadSaysWhereFocusWent(t *testing.T) {
	h, s, d := withButtons(t, mobiumdriver.AllButtons()...)
	d.tree = tvRow(t, "Network")
	d.after = map[string]*uitree.Tree{mobiumdriver.ButtonDpadRight: tvRow(t, "Applications")}
	res, err := callPress(h, s, mobiumdriver.ButtonDpadRight)
	if err != nil {
		t.Fatal(err)
	}
	view := res.StructuredContent.(PressView)
	if !strings.Contains(textOf(res), "focus moved to Applications") || !view.Confirmed || view.Focus != "Applications" {
		t.Errorf("focus moving was not reported: %q, %+v", textOf(res), view)
	}

	// At the end of the row the screen does not change.
	res, err = callPress(h, s, mobiumdriver.ButtonDpadRight)
	if err != nil {
		t.Fatal(err)
	}
	view = res.StructuredContent.(PressView)
	if !strings.Contains(textOf(res), "focus did not move from Applications") || view.Confirmed {
		t.Errorf("focus staying put was reported as moving: %q, %+v", textOf(res), view)
	}

	d.tree, d.after = tvRow(t, ""), nil
	res, err = callPress(h, s, mobiumdriver.ButtonDpadDown)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(res), "nothing on screen reports focus") || res.StructuredContent.(PressView).Confirmed {
		t.Errorf("a screen without focus was reported as a move: %q", textOf(res))
	}
}

// Select and the media keys have no platform-wide outcome, so they are
// reported as sent and never claim a focus or a foreground.
func TestSelectAndMediaKeysAreReportedAsSent(t *testing.T) {
	h, s, d := withButtons(t, mobiumdriver.AllButtons()...)
	d.tree = tvRow(t, "Network")
	for _, b := range []string{mobiumdriver.ButtonSelect, mobiumdriver.ButtonPlayPause, mobiumdriver.ButtonFastForward} {
		res, err := callPress(h, s, b)
		if err != nil {
			t.Fatal(err)
		}
		if textOf(res) != "pressed "+b || res.StructuredContent.(PressView).Confirmed {
			t.Errorf("%s: %q", b, textOf(res))
		}
	}
}
