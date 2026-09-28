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

// WDA drives an iOS simulator or a real iPhone through Appium's
// WebDriverAgent.
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
	xml, err := w.w3c.source(ctx)
	if err != nil {
		return nil, err
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

	// A password field cannot be confirmed this way and must not be tried: it
	// reads back as bullets, so the comparison below would fail on a perfectly
	// good type, forever. Found by this check on the login screen — "hunter2"
	// against "•••••••" — which is the same masking `uitree.Redact` exists for.
	//
	// It is cleared first, because WebDriverAgent types through the keyboard
	// and so appends: a field holding ten characters held seventeen after
	// seven more, while the call reported typing seven. An ordinary field is
	// put right by the read-back below, which clears and retries; a password
	// field has no read-back, so it is cleared before rather than after —
	// which makes app_type replace a field's contents on iOS as it does on
	// Android, password or not (CHALLENGES 103).
	if n.Password {
		if err := w.w3c.clearElement(ctx, elID); err != nil {
			return err
		}
		return w.w3c.setElementValue(ctx, elID, text)
	}

	var got string
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
					return spreadError(text, sp)
				}
			}
		}
		if attempt < setTextAttempts {
			if err := w.w3c.clearElement(ctx, elID); err != nil {
				return err
			}
		}
	}
	return mobiumerr.New(mobiumerr.NotConfirmed, "typed %q and the field holds %q — iOS dropped a keystroke, and "+
		"retrying %d times, down to %d keys a second, did not recover it", text, got, setTextAttempts-1,
		typingFrequencies[setTextAttempts-1])
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
func (w *WDA) elementFor(ctx context.Context, n *uitree.Node) (string, error) {
	if n.TestID != "" {
		return w.w3c.findElement(ctx, "accessibility id", n.TestID)
	}
	return w.w3c.findElement(ctx, "xpath", iosXPathFor(n))
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

// Launch brings an app to the foreground by bundle id.
func (w *WDA) Launch(ctx context.Context, appID string) error {
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

// ClearData deletes an app's data on the simulator.
func (w *WDA) ClearData(ctx context.Context, appID string) (device.ClearedData, error) {
	if err := w.simOnly(CapClearData); err != nil {
		return device.ClearedData{}, err
	}
	return w.sim.ClearAppData(ctx, appID)
}

// Uninstall removes an app from the simulator.
func (w *WDA) Uninstall(ctx context.Context, appID string) error {
	if w.phone != nil {
		return w.phone.UninstallApp(ctx, appID)
	}
	return w.sim.UninstallApp(ctx, appID)
}
