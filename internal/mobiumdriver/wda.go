package mobiumdriver

import (
	"bytes"
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// wdaReadyTimeout bounds how long to wait for WebDriverAgent to answer after
// launch. Longer than the Android equivalent: the runner is an XCTest host,
// and the first launch on a cold simulator includes its own startup.
const wdaReadyTimeout = 90 * time.Second

// WDA drives an iOS simulator or a real iPhone through WebDriverAgent.
//
// It shares the W3C client with the UiAutomator2 backend — the two servers
// expose the same endpoints — so this file is only the parts that differ:
// bringing the runner up, and reading an iOS hierarchy. Exactly one of sim and
// phone is set; what a phone does differently is in wdaphone.go.
type WDA struct {
	sim   *device.Simctl
	phone *device.Devicectl
	// runner is the xcodebuild process keeping WebDriverAgent alive on a
	// phone. Nil on a simulator, and nil on a phone whose runner was already
	// answering when the session started — that one is not ours to stop.
	runner *device.PhoneRunner
	w3c    *w3cClient

	// plog captures a phone's log for the life of the session, since the
	// phone keeps none to ask for later. Nil on a simulator, and nil on a
	// phone whose relay would not start — which DeviceLogs then reports.
	plog    *device.PhoneLog
	plogErr error

	// expecting is the app a phone was told is coming, until a read shows
	// it. See expectApp.
	hintMu    sync.Mutex
	expecting string
	// leaving is the app that was in front when expecting was set.
	leaving string
	// shadeHint is set while Notification Center was opened here, which
	// points every read at SpringBoard so map shows it. See wdanotify.go.
	shadeHint bool

	// found is the element findUnique found last, by how it was found, until
	// the next read of the screen.
	foundMu sync.Mutex
	found   map[string]string

	// locales is the language each app is pinned to, by bundle id, for the
	// life of the session. iOS stores no per-app language Mobium could set,
	// so it is a launch argument, and this is what every launch here passes.
	// See wdalocale.go.
	localeMu sync.Mutex
	locales  map[string][]string
	// zone is the time zone every launch here is given, as TZ in its
	// environment, for the life of the session; empty follows the device.
	// Guarded by localeMu. See wdatimezone.go.
	zone string
	// probed is the apps whose last launch here loaded the hit probe, so a
	// launch Mobium makes again — for a language or a zone — loads it too.
	// Guarded by localeMu. See hittest.go.
	probed map[string]bool

	// axSeen is what the last visit to a phone's Settings read, which a
	// read of every accessibility setting answers from. See phoneAX.
	axSeen axSeen
	// axPend is what each accessibility setting changed on a phone was
	// before, which restoreAX puts back all at once.
	axPend axPending

	mu sync.Mutex
	// scale converts WebDriverAgent's points to device pixels. Mobium's
	// coordinate space is pixels on both platforms, so this is applied to
	// every tree read and undone for every gesture sent.
	scale float64
}

// NewWDA prepares a driver for a simulator. Nothing touches the simulator
// until Start.
func NewWDA(sim *device.Simctl) *WDA {
	return &WDA{sim: sim, w3c: newW3CClient(requestTimeout), scale: 1}
}

// NewWDAPhone prepares a driver for a real iPhone. Nothing touches the phone
// until Start.
func NewWDAPhone(phone *device.Devicectl) *WDA {
	return &WDA{phone: phone, w3c: newW3CClient(requestTimeout), scale: 1}
}

func (w *WDA) Name() string { return "ios/wda" }

// Start installs and launches the runner, then opens a session.
func (w *WDA) Start(ctx context.Context, progress func(string)) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, sid := w.w3c.endpoint(); sid != "" {
		return nil
	}
	if w.phone != nil {
		if err := w.startPhone(ctx, progress); err != nil {
			return err
		}
		if err := w.openLocked(ctx); err != nil {
			return err
		}
		// Not fatal: a session that cannot read the log can still drive
		// the phone, and DeviceLogs says why it cannot.
		w.plog, w.plogErr = device.StartPhoneLog(ctx, w.phone.Phone.UDID)
		return nil
	}

	if err := w.sim.EnsureWDAInstalled(ctx, progress); err != nil {
		return err
	}
	// A runner already running here was left by a daemon that died without
	// tearing down — killed, or crashed. Launching it again does not restart
	// it: it brings its empty window to the front and leaves it there, so
	// every read after the crash saw a black screen and reported the runner
	// as the foreground app. Stopped first, it starts in the background as
	// it always does. Not running is the ordinary case, and not an error.
	_ = w.sim.TerminateApp(ctx, device.WDABundleID)
	// A simulator shares the host network stack, so WDA's port is reachable
	// directly — and so is every other simulator's. At the default 8100, a
	// second simulator's runner lost the port to the first, and a second
	// daemon drove the first simulator while reporting it as its own. Each
	// runner gets free ports of its own: USE_PORT for the server, and
	// MJPEG_SERVER_PORT for its video stream, which collided the same way at
	// 9100. CHALLENGES 148.
	ports, err := device.FreePorts(2)
	if err != nil {
		return err
	}
	if err := w.sim.LaunchAppWithEnv(ctx, device.WDABundleID, map[string]string{
		"USE_PORT":          fmt.Sprint(ports[0]),
		"MJPEG_SERVER_PORT": fmt.Sprint(ports[1]),
		"USE_IP":            "127.0.0.1",
	}); err != nil {
		return fmt.Errorf("launch WebDriverAgent: %w", err)
	}
	w.w3c.setBase(fmt.Sprintf("http://127.0.0.1:%d", ports[0]))

	// Waiting for an XCTest host to come up is the slowest step here and
	// reports nothing while it happens. Saying so lets the CLI explain the
	// pause, and extends the client's read deadline past it.
	if progress != nil {
		progress("waiting for WebDriverAgent to start")
	}
	// On an iPad the runner's test does not start until the runner leaves
	// the foreground: on iPadOS 26 it stays in front as a window, and XCTest
	// logged "Running tests..." and nothing after it, where an iPhone's goes
	// to the background by itself and carries on. Measured on the iPad mini
	// (A17 Pro) and iPad Air simulators, iOS 26.5: the server came up three
	// seconds after another app was brought forward, and never before. So
	// if it is not answering soon, Settings is opened and closed, which
	// leaves the home screen in front — the runner's cue. An iPhone answers
	// before this and never sees it. CHALLENGES 147.
	if !w.readyWithin(ctx, wdaNudgeAfter) {
		_ = w.sim.LaunchApp(ctx, "com.apple.Preferences")
		_ = w.sim.TerminateApp(ctx, "com.apple.Preferences")
	}
	if err := w.waitReady(ctx); err != nil {
		w.teardownLocked(ctx)
		return err
	}
	return w.openLocked(ctx)
}

// wdaNudgeAfter is how long a simulator's runner gets to answer on its own
// before it is sent to the background. An iPhone's answered in 6s, cold.
const wdaNudgeAfter = 10 * time.Second

// readyWithin reports whether the runner answers within d.
func (w *WDA) readyWithin(ctx context.Context, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if w.w3c.ready(ctx) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// openLocked opens a session on a runner that is answering, and reads the
// screen scale. Shared by simulator and phone.
func (w *WDA) openLocked(ctx context.Context) error {
	if err := w.w3c.openSession(ctx, nil); err != nil {
		w.teardownLocked(ctx)
		return err
	}
	w.w3c.reopen = func(ctx context.Context) error { return w.w3c.openSession(ctx, nil) }

	// Read the device scale once. A failure here is not fatal — a scale of 1
	// simply leaves coordinates in points, which is wrong but usable, and
	// worth reporting rather than refusing to start over.
	if s, err := w.readScale(ctx); err == nil {
		w.scale = s
	}
	return nil
}

// readScale asks WebDriverAgent for the device's point-to-pixel ratio.
func (w *WDA) readScale(ctx context.Context) (float64, error) {
	var resp struct {
		Value struct {
			Scale float64 `json:"scale"`
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, "/wda/screen", nil, &resp); err != nil {
		return 0, err
	}
	// Guard against a nonsense value silently multiplying every coordinate.
	if resp.Value.Scale < 1 || resp.Value.Scale > 5 {
		return 0, mobiumerr.New(mobiumerr.DeviceServer, "WebDriverAgent reported an implausible screen scale of %v", resp.Value.Scale)
	}
	return resp.Value.Scale, nil
}

// toPoints converts a device-pixel coordinate to the points WebDriverAgent
// expects for input.
func (w *WDA) toPoints(v int) int {
	if w.scale <= 0 || w.scale == 1 {
		return v
	}
	return int(float64(v)/w.scale + 0.5)
}

func (w *WDA) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(wdaReadyTimeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if w.w3c.ready(ctx) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	// Two causes, and the first one is both more common and invisible from
	// here. A system modal — a permission prompt left unanswered — blocks the
	// runner from starting at all, so a check that raised a dialog and then
	// died leaves the simulator unusable for every run after it. Naming only
	// the installation sent me to look at the wrong thing for twenty minutes,
	// which is what the rule about remedies that cannot work is about.
	// CHALLENGES 62.
	return mobiumerr.New(mobiumerr.Timeout, "WebDriverAgent did not become ready within %s. Two things stop "+
		"it, and the likelier one is not installation: a system dialog left on screen "+
		"blocks the runner from launching, so look at the simulator — an unanswered "+
		"permission prompt from an earlier run will be sitting there, and "+
		"`xcrun simctl shutdown <udid> && xcrun simctl boot <udid>` clears it. "+
		"Otherwise check that %s is installed and can launch",
		wdaReadyTimeout, device.WDABundleID)
}

// Healthy reports whether the session is still usable.
func (w *WDA) Healthy(ctx context.Context) bool {
	base, sid := w.w3c.endpoint()
	if base == "" || sid == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	return w.w3c.ready(ctx)
}

// Close ends the session and stops the runner.
func (w *WDA) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w.teardownLocked(ctx)
	return nil
}

func (w *WDA) teardownLocked(ctx context.Context) {
	w.w3c.closeSession(ctx)
	if w.plog != nil {
		w.plog.Close()
		w.plog = nil
	}
	if w.phone != nil {
		w.runner.Stop()
		w.runner = nil
	} else {
		w.sim.TerminateApp(ctx, device.WDABundleID)
	}
	w.w3c.setBase("")
}

// Snapshot fetches and parses the UI hierarchy.
func (w *WDA) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	w.forgetFound()
	xml, err := w.w3c.source(ctx)
	if err != nil {
		return nil, err
	}
	// Notification Center opened here and gone since — a notification
	// tapped opens its app — leaves reads pointed at SpringBoard; the app
	// that came forward is what is read instead.
	if w.dropShadeHint(ctx, xml) {
		if xml, err = w.w3c.source(ctx); err != nil {
			return nil, err
		}
	}
	tree, err := uitree.ParseIOS([]byte(xml))
	if err != nil {
		return nil, err
	}
	if w.phone != nil {
		w.settleExpected(ctx, tree.Package())
	}
	// A notification banner makes SpringBoard what WebDriverAgent reads, for
	// seconds, over an app that is still on screen (CHALLENGES 155).
	if app, banner := bannerOver(tree); app != "" {
		if under, ok := w.underBanner(ctx, app, banner); ok {
			tree = under
		}
	}
	// WDA reports points; everything above this layer works in device pixels,
	// which is also what screenshots are in.
	tree.Scale(w.scale)
	return tree, nil
}

// Source is the hierarchy as WebDriverAgent sent it — in points, not the
// pixels everything else here is in, because it is not scaled on the way
// out as Snapshot's tree is.
func (w *WDA) Source(ctx context.Context) (Source, error) {
	xml, err := w.w3c.source(ctx)
	if err != nil {
		return Source{}, err
	}
	if w.phone != nil {
		// The same bookkeeping a Snapshot does on a phone: take back the
		// expected app once it is the one on screen (CHALLENGES 76).
		if tree, err := uitree.ParseIOS([]byte(xml)); err == nil {
			w.settleExpected(ctx, tree.Package())
		}
	}
	return Source{XML: []byte(xml), Units: UnitsPoints, Scale: w.scale}, nil
}

// Screenshot captures the screen as PNG.
//
// Through simctl rather than WDA's endpoint, for the same reason the Android
// backend uses `adb exec-out screencap`: it writes the bytes directly instead
// of base64-ing them through HTTP.
func (w *WDA) Screenshot(ctx context.Context) ([]byte, error) {
	if w.phone != nil {
		return w.phoneScreenshot(ctx)
	}
	png, err := w.sim.Screenshot(ctx)
	if err == nil && bytes.HasPrefix(png, pngMagic) {
		return png, nil
	}
	return nil, fmt.Errorf("capture simulator screen: %w", err)
}

// Tap touches a point given in device pixels.
func (w *WDA) Tap(ctx context.Context, x, y int) error {
	return w.w3c.pointerSequence(ctx, tapActions(w.toPoints(x), w.toPoints(y)))
}

// LongPress holds a point down. Coordinates are device pixels.
func (w *WDA) LongPress(ctx context.Context, x, y int, d time.Duration) error {
	return w.w3c.pointerSequence(ctx, pressActions(w.toPoints(x), w.toPoints(y), d))
}

// Swipe drags between two points, in device pixels.
func (w *WDA) Swipe(ctx context.Context, x1, y1, x2, y2 int, d time.Duration) error {
	return w.w3c.pointerSequence(ctx,
		dragActions(w.toPoints(x1), w.toPoints(y1), w.toPoints(x2), w.toPoints(y2), d))
}

// setTextAttempts is how many times a type is tried before giving up.
//
// The failure this guards against is a race rather than a mistake: the value
// is sent correctly and the keyboard drops a keystroke. Two attempts at the
// same speed were enough on a still form; on a form that re-renders on every
// keystroke — MobiumApp's login, validating as it goes — the second attempt
// lost the same keystroke as the first, so the later ones type more slowly.
const setTextAttempts = 3

// typingFrequencies is how fast each attempt types, in keys per second: the
// server's own speed first, then slower. Retrying at the speed that lost a
// keystroke repeated the loss — "nobody" arrived as "nbody" on both attempts,
// on the login demo, where each keystroke re-renders the form — so a retry
// gives the app more time between keys instead. Zero is the server's default,
// 60.
var typingFrequencies = [setTextAttempts]int{0, 20, 6}

// ForegroundApp says which app is in front from a read of the screen without
// visible and accessible, which WebDriverAgent otherwise works out for every
// element: Settings' read took 1.81s with them and 0.22s without, on an
// iPhone 15 Plus, and visible alone was 1.5s of it. Only the root's bundle id
// is wanted here. SpringBoard in front may be a notification banner over the
// app (CHALLENGES 155), which the full read sees through, so that answer is
// left to the full read.
//
// On a phone the read that confirms a switch is also what takes the
// active-app hint off (CHALLENGES 76), and taking it off mid-switch lets the
// next read hang for a minute (CHALLENGES 71). The light read is pinned to
// the new app by the hint, so it can see it while iOS still reports the app
// being left in front too: on the iPhone 15 Plus both read state 4 from
// about 0.33s to 0.64-0.89s into a switch, and the two hangs measured, one
// in eleven launches and one in fifty, were both launches confirmed inside
// that window, at 0.45s. So on a phone the hint comes off only once the app
// being left is no longer in front, which WebDriverAgent answers in about
// 11ms; SpringBoard, which always reads as in front, is not waited for. If
// it has not gone within switchSettleWait, the full read decides, as it did
// before.
func (w *WDA) ForegroundApp(ctx context.Context) (string, error) {
	xml, err := w.w3c.sourceWithout(ctx, "visible,accessible")
	if err != nil {
		return "", err
	}
	tree, err := uitree.ParseIOS([]byte(xml))
	if err != nil {
		return "", err
	}
	app := tree.Package()
	if app == "" || app == springboardBundleID {
		full, err := w.Snapshot(ctx)
		if err != nil {
			return "", err
		}
		return full.Package(), nil
	}
	if w.phone != nil {
		if !w.switchSettled(ctx, app) {
			full, err := w.Snapshot(ctx)
			if err != nil {
				return "", err
			}
			return full.Package(), nil
		}
		w.settleExpected(ctx, app)
	}
	return app, nil
}

// switchSettleWait bounds the wait for the app being left to leave the front.
const switchSettleWait = 2 * time.Second

// switchSettled says whether a switch to app is far enough along that the
// hint can come off: it is not the app expected, or nothing is being left
// that could still be in front, or what is being left no longer is.
func (w *WDA) switchSettled(ctx context.Context, app string) bool {
	w.hintMu.Lock()
	expecting, leaving := w.expecting, w.leaving
	w.hintMu.Unlock()
	if expecting == "" || app != expecting || leaving == "" || leaving == app || leaving == springboardBundleID {
		return true
	}
	deadline := time.Now().Add(switchSettleWait)
	for time.Now().Before(deadline) {
		var resp struct {
			Value int `json:"value"`
		}
		err := w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/wda/apps/state"),
			map[string]interface{}{"bundleId": leaving}, &resp)
		if err == nil && resp.Value != 4 {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// ElementBounds reads one element's rectangle by its test id or label, when
// that is unique on screen — two small
// requests, against a read of the whole screen. Measured on an iPhone 15
// Plus: 148ms for MobiumApp's tiny target, against 167ms for MobiumApp's
// whole screen and about 2s for Settings'. Converted to device pixels with
// the tree's own rounding, so an element that has not moved compares equal.
func (w *WDA) ElementBounds(ctx context.Context, n *uitree.Node, t *uitree.Tree) (uitree.Rect, bool, error) {
	id, ok, err := w.findUnique(ctx, n, t)
	if !ok || err != nil {
		return uitree.Rect{}, ok, err
	}
	var resp struct {
		Value struct {
			X, Y, Width, Height float64
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, w.w3c.sessionPath("/element/"+id+"/rect"), nil, &resp); err != nil {
		return uitree.Rect{}, true, err
	}
	v := resp.Value
	r := uitree.Rect{X1: int(v.X), Y1: int(v.Y), X2: int(v.X + v.Width), Y2: int(v.Y + v.Height)}
	scaled := &uitree.Tree{Root: &uitree.Node{Bounds: r}}
	scaled.Scale(w.scale)
	return scaled.Root.Bounds, true, nil
}

// PointScale is how many device pixels a point is, as WebDriverAgent
// reported when the session opened — 3 on an iPhone 15 Plus or 17 Pro. It is
// the scale the tree was converted with, so a threshold in points times this
// is in the tree's own units even if the read failed and it stayed 1.
func (w *WDA) PointScale() float64 { return w.scale }

// SetText types into the element a node names, and confirms what landed.
//
// **XCUITest's keyboard drops characters.** Typing into one field and then
// immediately into another dropped one character from the middle of the string
// in **four runs out of five** — "lose-1" arriving as "lse-1" — while the call
// reported success. Measured on an iPhone 17 Pro simulator, iOS 26.5; the same
// strings typed with a pause between them were correct every time, so this is
// a race with the focus change rather than anything about the text.
//
// Reporting success for a field that holds something else is the shape this
// project exists to not have, so the value is read back and the type retried.
// Android is unaffected — measured, not assumed — which is why this sits here
// rather than in the shared client. See CHALLENGES 61.
func (w *WDA) SetText(ctx context.Context, n *uitree.Node, text string) error {
	elID, err := w.elementFor(ctx, n)
	if err != nil {
		return err
	}

	// A password field cannot be compared: it reads back as bullets, so
	// "hunter2" against "•••••••" would fail on a perfectly good type,
	// forever. But one bullet is one character, so its length is confirmed
	// instead — on a real iPhone, ten characters typed into MobiumApp's
	// password field left one, or none, while the call reported all ten
	// (CHALLENGES 159). Only lengths are ever reported.
	//
	// It is cleared first, because WebDriverAgent types through the keyboard
	// and so appends: a field holding ten characters held seventeen after
	// seven more, while the call reported typing seven. That makes app_type
	// replace a field's contents on iOS as it does on Android, password or
	// not (CHALLENGES 103).
	if n.Password {
		want := len([]rune(text))
		held := 0
		for attempt := 1; attempt <= setTextAttempts; attempt++ {
			if err := w.w3c.clearElement(ctx, elID); err != nil {
				return err
			}
			if err := w.w3c.setElementValueAt(ctx, elID, text, typingFrequencies[attempt-1]); err != nil {
				return err
			}
			if held, err = w.secureLength(ctx, elID); err != nil {
				return mobiumerr.New(mobiumerr.NotConfirmed, "typed into the password field and could not read "+
					"its length back to confirm it: %w", err)
			}
			if held == want {
				return nil
			}
		}
		if err := w.keyboardLacks(ctx, text, want, 0, held); err != nil {
			return err
		}
		return mobiumerr.New(mobiumerr.NotConfirmed, "typed %d characters into the password field and it holds %d — "+
			"the keystrokes did not all arrive, and retrying %d times, down to %d keys a second, did not recover "+
			"them", want, held, setTextAttempts-1, typingFrequencies[setTextAttempts-1])
	}

	var got string
	masked := -1
	for attempt := 1; attempt <= setTextAttempts; attempt++ {
		if err := w.w3c.setElementValueAt(ctx, elID, text, typingFrequencies[attempt-1]); err != nil {
			return err
		}
		got, err = w.w3c.elementValue(ctx, elID)
		if err != nil {
			// The type itself succeeded; only the confirmation failed. Say
			// which, rather than implying nothing was typed.
			return mobiumerr.New(mobiumerr.NotConfirmed, "typed into the element and could not read it back to "+
				"confirm it: %w", err)
		}
		if got == text {
			return nil
		}
		// A field that reads back as nothing but bullets is a password field,
		// whatever it was when it was resolved: Flutter's obscured field is a
		// plain TextField until it holds something, and then a
		// SecureTextField. Confirmed by length, as a password is, and marked
		// on the node so the caller never echoes the text — the first run
		// printed the password in this function's own error (CHALLENGES 206).
		if isMasked(got) {
			n.Password = true
			masked = len([]rune(got))
			if masked == len([]rune(text)) {
				return nil
			}
		}
		// The app may have moved focus on as the text arrived — a one-time
		// code's boxes do — and then the rest is in the fields after this
		// one. Retrying would clear this field and type the whole text again
		// into the next ones, so look before retrying (CHALLENGES 156).
		if attempt == 1 {
			if tree, err := w.Snapshot(ctx); err == nil {
				if sp, ok := findSpread(tree, n, text); ok {
					if sp.Complete() {
						return nil
					}
					return w.retypeSpread(ctx, text, sp)
				}
			}
		}
		if attempt < setTextAttempts {
			if err := w.w3c.clearElement(ctx, elID); err != nil {
				return err
			}
		}
	}
	if masked >= 0 {
		return mobiumerr.New(mobiumerr.NotConfirmed, "typed %d characters into what turned out to be a password "+
			"field and it holds %d — the keystrokes did not all arrive, and retrying %d times, down to %d keys a "+
			"second, did not recover them", len([]rune(text)), masked, setTextAttempts-1,
			typingFrequencies[setTextAttempts-1])
	}
	return mobiumerr.New(mobiumerr.NotConfirmed, "typed %q and the field holds %q — iOS dropped a keystroke, and "+
		"retrying %d times, down to %d keys a second, did not recover it", text, got, setTextAttempts-1,
		typingFrequencies[setTextAttempts-1])
}

// isMasked reports a value that is nothing but the bullets a secure field
// reads back as.
func isMasked(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if r != '•' && r != '●' {
			return false
		}
	}
	return true
}

// retypeSpread types text again one character to a box, after typing it
// whole into the first box lost a character as the app moved focus on.
//
// On an iPhone 17 Pro simulator, typing a six-digit code into MobiumApp's
// first OTP box lost a digit at a focus change in two runs of ten: the
// keyboard types faster than the app moves focus. The iPhone 15 Plus, which
// types at half the speed, lost none in ten. Typing into each box in turn
// cannot race the focus change, because each keystroke goes to the element
// it names. So the boxes are cleared, last first, since clearing one can
// move focus back, and each gets its character and is read back; then the
// row is read again and must hold the text in order. Only on a loss: when
// the whole code arrives, it costs nothing.
func (w *WDA) retypeSpread(ctx context.Context, text string, sp spread) error {
	chars := []rune(text)
	if len(sp.Fields) < len(chars) {
		return spreadError(text, sp)
	}
	boxes := sp.Fields[:len(chars)]
	ids := make([]string, len(boxes))
	for i, b := range boxes {
		id, err := w.elementFor(ctx, b)
		if err != nil {
			return spreadError(text, sp)
		}
		ids[i] = id
	}
	for i := len(ids) - 1; i >= 0; i-- {
		if err := w.w3c.clearElement(ctx, ids[i]); err != nil {
			return err
		}
	}
	for i, r := range chars {
		if err := w.w3c.setElementValueAt(ctx, ids[i], string(r), 0); err != nil {
			return err
		}
		if got, err := w.w3c.elementValue(ctx, ids[i]); err != nil || got != string(r) {
			break
		}
	}
	tree, err := w.Snapshot(ctx)
	if err != nil {
		return mobiumerr.New(mobiumerr.NotConfirmed, "typed %q one character to a box after a character was "+
			"lost, and could not read the boxes back to confirm it: %w", text, err)
	}
	again, ok := findSpread(tree, sp.Fields[0], text)
	if !ok {
		return spreadError(text, sp)
	}
	if again.Complete() && len(again.Parts) == len(chars) {
		return nil
	}
	return spreadError(text, again)
}

// keyboardLacks is the refusal for a password that came up short because the
// keyboard on screen has no keys for some of it, or nil when that is not why.
func (w *WDA) keyboardLacks(ctx context.Context, text string, typed, had, held int) error {
	tree, err := w.Snapshot(ctx)
	if err != nil {
		return nil
	}
	missing, sample := keyless(tree, text)
	if missing == 0 {
		return nil
	}
	return mobiumerr.New(mobiumerr.NotConfirmed, "typed %d characters into the password field, which held %d, "+
		"and it holds %d — %d of them have no key on the keyboard on screen (its keys include %s), and on a "+
		"real iPhone a password field is typed key by key on that keyboard, so they were dropped", typed, had,
		held, missing, sample).
		WithRemedy("switch the phone to a keyboard that has those characters — the globe key on the keyboard — "+
			"and type it again").
		WithDetail("keyless", missing)
}

// secureLength is how many characters a password field holds, read from the
// bullets it shows. An empty one reports its placeholder, in clear, as its
// value — "password", eight characters, on the simulator and the phone — so a
// value equal to the placeholder is none.
func (w *WDA) secureLength(ctx context.Context, elID string) (int, error) {
	v, err := w.w3c.elementValue(ctx, elID)
	if err != nil {
		return 0, err
	}
	if ph, _ := w.w3c.elementAttribute(ctx, elID, "placeholderValue"); ph != "" && ph == v {
		return 0, nil
	}
	return len([]rune(v)), nil
}

// Clear empties a text field.
func (w *WDA) Clear(ctx context.Context, n *uitree.Node) error {
	elID, err := w.elementFor(ctx, n)
	if err != nil {
		return err
	}
	return w.w3c.clearElement(ctx, elID)
}

// elementFor resolves one of our nodes to a server-side element handle.
//
// An accessibility identifier is preferred, exactly as a resource-id is on
// Android. WDA's XPath is over the same document /source returned, so the
// node's sibling path reconstructs it.
//
// A lookup that fails is asked again as the node's own app: under a
// notification banner WebDriverAgent searches SpringBoard, and a field the
// app's tree had just shown was "no such element" (CHALLENGES 155).
func (w *WDA) elementFor(ctx context.Context, n *uitree.Node) (string, error) {
	find := func() (string, error) {
		if n.TestID != "" {
			return w.w3c.findElement(ctx, "accessibility id", n.TestID)
		}
		return w.w3c.findElement(ctx, "xpath", iosXPathFor(n))
	}
	id, err := find()
	if err == nil || ctx.Err() != nil {
		return id, err
	}
	app := appOf(n)
	if app == "" || app == "com.apple.springboard" {
		return id, err
	}
	var again string
	if w.asApp(ctx, app, func() (e error) { again, e = find(); return e }) != nil {
		return id, err
	}
	return again, nil
}

// iosXPathFor rebuilds an absolute XPath from a node's sibling path. WDA's
// document has no "hierarchy" wrapper: the application element is the root.
func iosXPathFor(n *uitree.Node) string {
	if n.Path == "" {
		return "/*"
	}
	var sb strings.Builder
	for _, seg := range strings.Split(n.Path, "/") {
		idx := 0
		fmt.Sscanf(seg, "%d", &idx)
		fmt.Fprintf(&sb, "/*[%d]", idx+1)
	}
	return sb.String()
}

// Launch brings an app to the foreground by bundle id — in the language it
// is pinned to, if it is.
func (w *WDA) Launch(ctx context.Context, appID string) error {
	w.setProbed(appID, false)
	if tags, zone := w.pinnedLocale(appID), w.sessionZone(); len(tags) > 0 || zone != "" {
		return w.launchWith(ctx, appID, tags, zone)
	}
	if w.phone != nil {
		w.expectApp(ctx, appID)
		if err := w.phone.LaunchApp(ctx, appID); err != nil {
			w.clearExpected(ctx)
			return err
		}
		return nil
	}
	return w.sim.LaunchApp(ctx, appID)
}

// Terminate stops a running app.
func (w *WDA) Terminate(ctx context.Context, appID string) error {
	if w.phone != nil {
		return w.phoneTerminate(ctx, appID)
	}
	return w.sim.TerminateApp(ctx, appID)
}

// Install adds an app bundle.
func (w *WDA) Install(ctx context.Context, path string) error {
	if w.phone != nil {
		return w.phone.InstallApp(ctx, path)
	}
	return w.sim.InstallApp(ctx, path)
}

// OpenURL opens a URL or deep link.
func (w *WDA) OpenURL(ctx context.Context, url string) error {
	if w.phone != nil {
		return w.phoneOpenURL(ctx, url)
	}
	return w.sim.OpenURL(ctx, url)
}

// SetPermission grants or revokes one privacy service.
func (w *WDA) SetPermission(ctx context.Context, appID, permission string, grant bool) error {
	if err := w.simOnly(CapPermissions); err != nil {
		return err
	}
	return w.sim.SetPrivacy(ctx, permission, appID, grant)
}

// ResetPermissions reverts privacy services to prompting on next use. Unlike
// Android, the simulator can scope this to one app.
func (w *WDA) ResetPermissions(ctx context.Context, appID string) error {
	if err := w.simOnly(CapPermissions); err != nil {
		return err
	}
	return w.sim.ResetPrivacy(ctx, "all", appID)
}

// Appearance reports the simulator's interface style.
func (w *WDA) Appearance(ctx context.Context) (string, error) {
	if err := w.simOnly(CapAppearance); err != nil {
		return "", err
	}
	return w.sim.Appearance(ctx)
}

// SetAppearance switches the simulator between light and dark.
//
// iOS has no "auto": the simulator is one or the other, and asking for a
// third thing is refused rather than quietly picking one.
func (w *WDA) SetAppearance(ctx context.Context, mode string) error {
	if mode == appearanceAuto {
		return mobiumerr.New(mobiumerr.Unsupported, "the iOS simulator has no automatic appearance — set %q or %q",
			appearanceLight, appearanceDark)
	}
	if mode != appearanceLight && mode != appearanceDark {
		return mobiumerr.New(mobiumerr.InvalidArgument, "unknown appearance %q (want %q or %q)",
			mode, appearanceLight, appearanceDark)
	}
	if err := w.simOnly(CapAppearance); err != nil {
		return err
	}
	return w.sim.SetAppearance(ctx, mode)
}

// ListApps reports installed apps on the simulator.
func (w *WDA) ListApps(ctx context.Context, includeSystem bool) ([]device.InstalledApp, error) {
	if w.phone != nil {
		return w.phone.ListApps(ctx, includeSystem)
	}
	return w.sim.ListApps(ctx, includeSystem)
}

// UploadFile puts a file in an app's Documents: simctl on a simulator,
// CoreDevice on a phone.
func (w *WDA) UploadFile(ctx context.Context, local, name, appID string) (device.Transfer, error) {
	if w.phone != nil {
		return w.phone.UploadFile(ctx, local, name, appID)
	}
	return w.sim.UploadFile(ctx, local, name, appID)
}

// DownloadFile copies a file from an app's Documents.
func (w *WDA) DownloadFile(ctx context.Context, name, appID, local string) (device.Transfer, error) {
	if w.phone != nil {
		return w.phone.DownloadFile(ctx, name, appID, local)
	}
	return w.sim.DownloadFile(ctx, name, appID, local)
}

// ListFiles lists an app's Documents.
func (w *WDA) ListFiles(ctx context.Context, appID string) ([]device.DeviceFile, error) {
	if w.phone != nil {
		return w.phone.ListFiles(ctx, appID)
	}
	return w.sim.ListFiles(ctx, appID)
}

// ClearData deletes an app's data on the simulator.
func (w *WDA) ClearData(ctx context.Context, appID string) (device.ClearedData, error) {
	if err := w.simOnly(CapClearData); err != nil {
		return device.ClearedData{}, err
	}
	return w.sim.ClearAppData(ctx, appID)
}

// ResetFromBundle resets an app on a phone by uninstalling it and installing
// the bundle again. A simulator clears in place, and is refused a bundle
// rather than given a different reset from the one it was asked for.
func (w *WDA) ResetFromBundle(ctx context.Context, appID, bundle string) (device.ClearedData, error) {
	if w.phone == nil {
		return device.ClearedData{}, mobiumerr.New(mobiumerr.InvalidArgument, "a simulator clears an app's data in "+
			"place, so it takes no bundle — leave out path (--bundle on the CLI)")
	}
	return w.phone.ResetFromBundle(ctx, appID, bundle)
}

// Uninstall removes an app from the simulator.
func (w *WDA) Uninstall(ctx context.Context, appID string) error {
	if w.phone != nil {
		return w.phone.UninstallApp(ctx, appID)
	}
	return w.sim.UninstallApp(ctx, appID)
}
