package mobiumdriver

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// UiAutomator2 is the device-side server, driven directly over HTTP with no
// other process in between. It replaces `uiautomator dump`, which costs a
// couple of seconds per snapshot and cannot type or gesture at all.
const (
	// uia2DevicePort is the port the server listens on inside the device.
	uia2DevicePort = 6790
	// instrumentTarget is the instrumentation that hosts the server.
	instrumentTarget = "io.appium.uiautomator2.server.test/androidx.test.runner.AndroidJUnitRunner"
	// serverReadyTimeout bounds how long to wait for the server to answer
	// /status after instrumentation starts.
	serverReadyTimeout = 45 * time.Second
	// requestTimeout bounds a single HTTP call to the server.
	requestTimeout = 60 * time.Second
	// healthTimeout bounds the readiness ping. It runs against a forwarded
	// local port, so a healthy server answers in about a millisecond.
	healthTimeout = 2 * time.Second
)

// UIA2 drives an Android device through the UiAutomator2 server.
type UIA2 struct {
	adb *device.ADB
	w3c *w3cClient

	mu       sync.Mutex
	port     int
	instrum  *exec.Cmd
	instrLog *device.SyncBuffer
	cancel   context.CancelFunc
}

// NewUIA2 prepares a driver. Nothing touches the device until Start.
func NewUIA2(adb *device.ADB) *UIA2 {
	return &UIA2{adb: adb, w3c: newW3CClient(requestTimeout)}
}

func (u *UIA2) Name() string { return "android/uiautomator2" }

// Start installs the server if needed, launches it, forwards a port and opens
// a session. It is safe to call on an already-started driver.
func (u *UIA2) Start(ctx context.Context, progress func(string)) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, sid := u.w3c.endpoint(); sid != "" {
		return nil
	}

	if err := u.adb.EnsureUIA2Installed(ctx, progress); err != nil {
		return err
	}
	// A server already running here was left by a daemon that died without
	// tearing down — killed, or crashed. Starting the instrumentation again
	// does replace it, but not at once: the old one went on answering its
	// port for a moment, the new session opened on it, and the next call
	// read EOF as it went — 2 calls in 15 straight after a kill -9, on an
	// Android 15 emulator. Stopped first, as teardown stops it, the session
	// opens on the server this call started. Its forward would otherwise
	// stay until adb restarted. Neither is fatal: nothing running is the
	// ordinary case.
	u.adb.StopUIA2(ctx)
	_, _ = u.adb.RemoveForwardsTo(ctx, uia2DevicePort)

	// The instrumentation must outlive this call, so it gets its own context
	// rather than the per-command one.
	runCtx, cancel := context.WithCancel(context.Background())
	cmd, log, err := u.adb.Start(runCtx, "shell", "am", "instrument", "-w",
		"-e", "disableAnalytics", "true", instrumentTarget)
	if err != nil {
		cancel()
		return err
	}
	u.instrum, u.instrLog, u.cancel = cmd, log, cancel

	port, err := u.adb.Forward(ctx, uia2DevicePort)
	if err != nil {
		u.teardownLocked(ctx)
		return fmt.Errorf("forward UiAutomator2 port: %w", err)
	}
	u.port = port
	u.w3c.setBase(fmt.Sprintf("http://127.0.0.1:%d", port))

	if progress != nil {
		progress("waiting for the UiAutomator2 server to start")
	}
	if err := u.waitReady(ctx); err != nil {
		u.teardownLocked(ctx)
		return err
	}
	if err := u.w3c.openSession(ctx, nil); err != nil {
		u.teardownLocked(ctx)
		return err
	}
	if err := u.capIdleWait(ctx); err != nil {
		u.teardownLocked(ctx)
		return err
	}
	// Anything else that attaches to the device-side server invalidates this
	// session; recover rather than failing every later command — with the
	// idle wait capped again, since a new session starts from the default.
	u.w3c.reopen = func(ctx context.Context) error {
		if err := u.w3c.openSession(ctx, nil); err != nil {
			return err
		}
		return u.capIdleWait(ctx)
	}
	return nil
}

// idleWaitCap is how long UiAutomator2 may wait for the app to go idle
// before a read, in milliseconds.
//
// The server's default is 10000: on a screen that animates, every read
// blocked until the animation ended — MobiumApp's three-second confetti took
// 3.73s to read and was always read as over, and a find during a slower burst
// took 11s. Zero, which is how WebDriverAgent behaves, broke launch instead:
// a tap right after one landed on a row still moving into place, reaching
// its screen 1 time in 5. Measured on a Pixel 7 AVD, 500 did both — 5 of 5
// after launch, and reads mid-burst in 1.3–1.7s that saw it falling
// (CHALLENGES 109).
const idleWaitCap = 500

// capIdleWait sets the server's idle wait to idleWaitCap and confirms it did.
func (u *UIA2) capIdleWait(ctx context.Context) error {
	path := u.w3c.sessionPath("/appium/settings")
	if err := u.w3c.do(ctx, http.MethodPost, path, map[string]interface{}{
		"settings": map[string]interface{}{"waitForIdleTimeout": idleWaitCap},
	}, nil); err != nil {
		return fmt.Errorf("cap the UiAutomator2 idle wait: %w", err)
	}
	var got struct {
		Value map[string]interface{} `json:"value"`
	}
	if err := u.w3c.do(ctx, http.MethodGet, path, nil, &got); err != nil {
		return fmt.Errorf("read back the UiAutomator2 idle wait: %w", err)
	}
	if v, ok := got.Value["waitForIdleTimeout"].(float64); !ok || v != idleWaitCap {
		return mobiumerr.New(mobiumerr.NotConfirmed, "UiAutomator2 accepted waitForIdleTimeout %d and reads back %v",
			idleWaitCap, got.Value["waitForIdleTimeout"])
	}
	return nil
}

// waitReady polls /status until the server answers.
func (u *UIA2) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(serverReadyTimeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if u.w3c.ready(ctx) {
			return nil
		}
		// The instrumentation exiting means the server will never come up;
		// its output is the only place the reason appears.
		if u.instrum != nil && u.instrum.ProcessState != nil {
			return mobiumerr.New(mobiumerr.DeviceServer, "the UiAutomator2 server exited during startup:\n%s",
				strings.TrimSpace(u.instrLog.String()))
		}
		time.Sleep(150 * time.Millisecond)
	}
	return mobiumerr.New(mobiumerr.Timeout, "the UiAutomator2 server did not become ready within %s:\n%s",
		serverReadyTimeout, strings.TrimSpace(u.instrLog.String()))
}

// Healthy reports whether the server is still answering on this session.
//
// Emulator serials are reused across restarts, so a cached session can outlive
// the device it was made for.
func (u *UIA2) Healthy(ctx context.Context) bool {
	base, sid := u.w3c.endpoint()
	if base == "" || sid == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	return u.w3c.ready(ctx)
}

// Close ends the session and stops the server.
func (u *UIA2) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u.teardownLocked(ctx)
	return nil
}

func (u *UIA2) teardownLocked(ctx context.Context) {
	u.w3c.closeSession(ctx)
	if u.port != 0 {
		u.adb.RemoveForward(ctx, u.port)
		u.port = 0
	}
	// Before the host is canceled: see StopUIA2 for the crash report the
	// other order leaves on the device.
	if u.instrum != nil {
		u.adb.StopUIA2(ctx)
	}
	if u.cancel != nil {
		u.cancel()
		u.cancel = nil
	}
	if u.instrum != nil {
		u.instrum.Wait()
		u.instrum = nil
	}
	u.w3c.setBase("")
}

// transientSnapshot reports whether a failed hierarchy read is worth retrying.
//
// Reading the tree while the screen is mid-animation fails with "Cannot set
// AccessibilityNodeInfo's field 'mSealed' to 'true'" — accessibility nodes are
// being recycled underneath the read. It is not a state anyone can wait out
// deliberately, because the caller has no way to know an animation is running;
// it simply succeeds a moment later. Found by opening the notification shade
// and mapping immediately, which is exactly what a caller does.
//
// The dump backend has always retried its own transient failure ("null root
// node"); this one had no equivalent, so the error reached the user looking
// like a broken device.
func transientSnapshot(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, marker := range []string{
		"mSealed",                    // nodes recycled mid-read, during an animation
		"AccessibilityNodeInfo",      // the same family of failure
		"UiAutomation not connected", // the service is still coming up
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// Snapshot fetches and parses the UI hierarchy.
func (u *UIA2) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	xml, err := u.source(ctx)
	if err != nil {
		return nil, err
	}
	return uitree.ParseAndroid([]byte(xml))
}

// Source is the hierarchy as the UiAutomator2 server sent it, in pixels.
func (u *UIA2) Source(ctx context.Context) (Source, error) {
	xml, err := u.source(ctx)
	return Source{XML: []byte(xml), Units: UnitsPixels}, err
}

// source fetches the hierarchy, retrying the failures that mean "the screen
// was moving" rather than "the screen cannot be read".
func (u *UIA2) source(ctx context.Context) (string, error) {
	deadline := time.Now().Add(uia2MovingScreenBudget)
	var lastErr error
	for attempt := 1; ; attempt++ {
		xml, err := u.w3c.source(ctx)
		if err == nil {
			return xml, nil
		}
		lastErr = err
		if !transientSnapshot(err) {
			return "", err
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("could not read the UI hierarchy after %d attempts over %s, "+
				"each failing while the screen was changing: %w", attempt, uia2MovingScreenBudget, lastErr)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(uia2RetryDelay):
		}
	}
}

// uia2MovingScreenBudget is how long a read keeps retrying the failures that
// mean the screen was changing, and uia2RetryDelay how far apart.
//
// Three tries 700ms apart were enough while UiAutomator2 waited up to 10s
// for the app to go idle before every read; with that capped at half a second
// (CHALLENGES 109) a read lands in the motion itself, and on a Pixel 8 Pro a
// pager still coasting after a swipe failed all three. A budget in time, not
// a count, is what the motion is measured in; only these failures are
// retried, and every other one still surfaces at once (CHALLENGES 51).
const (
	uia2MovingScreenBudget = 6 * time.Second
	uia2RetryDelay         = 300 * time.Millisecond
)

// Screenshot captures the screen as PNG.
//
// This goes through adb rather than the server's own /screenshot endpoint,
// which was measured at 0.67s against 0.13s for `adb exec-out screencap`: the
// server base64s the image through HTTP, while adb streams the bytes. The
// server endpoint remains the fallback for a device where screencap fails.
func (u *UIA2) Screenshot(ctx context.Context) ([]byte, error) {
	png, err := u.adb.ExecOut(ctx, "screencap", "-p")
	if err == nil && bytes.HasPrefix(png, pngMagic) {
		return png, nil
	}
	return u.screenshotViaServer(ctx)
}

func (u *UIA2) screenshotViaServer(ctx context.Context) ([]byte, error) {
	var resp struct {
		Value string `json:"value"`
	}
	if err := u.w3c.do(ctx, http.MethodGet, u.w3c.sessionPath("/screenshot"), nil, &resp); err != nil {
		return nil, err
	}
	png, err := base64.StdEncoding.DecodeString(resp.Value)
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}
	if !bytes.HasPrefix(png, pngMagic) {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "screenshot is %d bytes that are not a PNG", len(png))
	}
	return png, nil
}

// Tap touches a point using a W3C pointer action.
func (u *UIA2) Tap(ctx context.Context, x, y int) error {
	return u.w3c.pointerSequence(ctx, tapActions(x, y))
}

// LongPress holds a point down.
func (u *UIA2) LongPress(ctx context.Context, x, y int, d time.Duration) error {
	return u.w3c.pointerSequence(ctx, pressActions(x, y, d))
}

// Swipe drags from one point to another over the given duration.
func (u *UIA2) Swipe(ctx context.Context, x1, y1, x2, y2 int, d time.Duration) error {
	return u.w3c.pointerSequence(ctx, dragActions(x1, y1, x2, y2, d))
}

// SetText types into the element a node names.
//
// The node is located server-side by its resource-id, or by an XPath rebuilt
// from its position in the snapshot it came from, so the value goes to the
// element the caller resolved rather than to whatever currently holds focus.
func (u *UIA2) SetText(ctx context.Context, n *uitree.Node, text string) error {
	elID, err := u.elementFor(ctx, n)
	if err != nil {
		return err
	}
	return u.w3c.setElementValue(ctx, elID, text)
}

// Clear empties a text field.
func (u *UIA2) Clear(ctx context.Context, n *uitree.Node) error {
	elID, err := u.elementFor(ctx, n)
	if err != nil {
		return err
	}
	return u.w3c.clearElement(ctx, elID)
}

func (u *UIA2) elementFor(ctx context.Context, n *uitree.Node) (string, error) {
	if n.TestID != "" {
		if id, err := u.w3c.findElement(ctx, "id", n.TestID); err == nil {
			return id, nil
		}
		// The "id" strategy qualifies a bare name with the package under
		// test, so it looks for <pkg>:id/<name>. Most Android apps report a
		// resource-id that is already qualified and this is exact. React
		// Native reports a bare one -- a testID of "password" arrives as
		// resource-id="password" -- so the server searches for
		// "<pkg>:id/password", finds nothing, and text entry fails on a
		// field `map` had just handed out a ref for.
		//
		// Falling through to the position is not a guess: it is the same
		// lookup used for every node with no resource-id at all, against
		// the snapshot the caller already resolved. See CHALLENGES 55.
	}
	return u.w3c.findElement(ctx, "xpath", xpathFor(n))
}

// xpathFor rebuilds an absolute XPath from a node's sibling path. Our paths
// are 0-based and XPath positions are 1-based.
func xpathFor(n *uitree.Node) string {
	if n.Path == "" {
		return "/hierarchy"
	}
	var sb strings.Builder
	sb.WriteString("/hierarchy")
	for _, seg := range strings.Split(n.Path, "/") {
		idx := 0
		fmt.Sscanf(seg, "%d", &idx)
		fmt.Fprintf(&sb, "/*[%d]", idx+1)
	}
	return sb.String()
}

// Launch brings an app to the foreground by package name.
func (u *UIA2) Launch(ctx context.Context, appID string) error {
	return u.adb.LaunchApp(ctx, appID)
}

// Terminate force-stops an app.
func (u *UIA2) Terminate(ctx context.Context, appID string) error {
	return u.adb.TerminateApp(ctx, appID)
}

// Install adds an APK.
func (u *UIA2) Install(ctx context.Context, path string) error {
	return u.adb.InstallApp(ctx, path)
}

// OpenURL opens a URL or deep link.
func (u *UIA2) OpenURL(ctx context.Context, url string) error {
	return u.adb.OpenURL(ctx, url)
}

// SetPermission grants or revokes one runtime permission.
func (u *UIA2) SetPermission(ctx context.Context, appID, permission string, grant bool) error {
	return u.adb.SetPermission(ctx, appID, permission, grant)
}

// ResetPermissions reverts runtime permissions to asking. With an app, that
// app's alone — revoked, and the person's answers cleared (see
// device.ADB.ResetAppPermissions); without one, every app's, which is what
// `pm reset-permissions` does and the only form it has.
func (u *UIA2) ResetPermissions(ctx context.Context, appID string) error {
	if appID != "" {
		_, err := u.adb.ResetAppPermissions(ctx, appID)
		return err
	}
	return u.adb.ResetPermissions(ctx)
}

// ResetAppPermissions resets one app's, and says what it left as it was.
func (u *UIA2) ResetAppPermissions(ctx context.Context, appID string) (device.PermissionReset, error) {
	return u.adb.ResetAppPermissions(ctx, appID)
}

// PermissionState reports what the app declares and what it has.
func (u *UIA2) PermissionState(ctx context.Context, appID string) (map[string]bool, error) {
	return u.adb.RuntimePermissions(ctx, appID)
}

// Appearance reports whether the device is in dark mode.
func (u *UIA2) Appearance(ctx context.Context) (string, error) {
	night, err := u.adb.NightMode(ctx)
	if err != nil {
		return "", err
	}
	return nightToAppearance(night), nil
}

// SetAppearance switches the device between light and dark.
func (u *UIA2) SetAppearance(ctx context.Context, mode string) error {
	night, err := appearanceToNight(mode)
	if err != nil {
		return err
	}
	return u.adb.SetNightMode(ctx, night)
}

// ListApps reports installed packages.
func (u *UIA2) ListApps(ctx context.Context, includeSystem bool) ([]device.InstalledApp, error) {
	return u.adb.ListPackages(ctx, includeSystem)
}

// UploadFile puts a file in the Download folder; Android has one, for every app.
func (u *UIA2) UploadFile(ctx context.Context, local, name, _ string) (device.Transfer, error) {
	return u.adb.UploadFile(ctx, local, name)
}

// DownloadFile copies a file from the Download folder.
func (u *UIA2) DownloadFile(ctx context.Context, name, _ string, local string) (device.Transfer, error) {
	return u.adb.DownloadFile(ctx, name, local)
}

// ListFiles lists the Download folder.
func (u *UIA2) ListFiles(ctx context.Context, _ string) ([]device.DeviceFile, error) {
	return u.adb.ListFiles(ctx)
}

// ClearData deletes a package's data.
func (u *UIA2) ClearData(ctx context.Context, appID string) (device.ClearedData, error) {
	return u.adb.ClearAppData(ctx, appID)
}

// Uninstall removes a package.
func (u *UIA2) Uninstall(ctx context.Context, appID string) error {
	return u.adb.UninstallApp(ctx, appID)
}
