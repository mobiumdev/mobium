package mobiumdriver

import (
	"bytes"
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// pngMagic is the PNG file signature, used to tell a real capture from a
// device that printed an error where the image should be.
var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// dumpPath is where uiautomator writes on the device. Dumping to a file and
// reading it back is slower than `dump /dev/tty` but it is the only form that
// survives every API level without the status line contaminating the XML.
const dumpPath = "/data/local/tmp/mobium-dump.xml"

// snapshotRetries covers uiautomator's transient "null root node" failure,
// which happens while the screen is mid-animation. Retrying is the documented
// remedy and costs less than a false negative.
const snapshotRetries = 3

// snapshotRetryDelay lets an in-flight transition settle before retrying.
const snapshotRetryDelay = 700 * time.Millisecond

// Android drives an emulator (or handset) through adb and uiautomator.
type Android struct {
	adb *device.ADB
}

// NewAndroid returns a driver bound to an already-selected device.
func NewAndroid(adb *device.ADB) *Android { return &Android{adb: adb} }

func (a *Android) Name() string { return "android/uiautomator" }

// Snapshot dumps and parses the current UI hierarchy.
func (a *Android) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	raw, err := a.dump(ctx)
	if err != nil {
		return nil, err
	}
	return uitree.ParseAndroid(raw)
}

// Source is the hierarchy as `uiautomator dump` wrote it, in pixels.
func (a *Android) Source(ctx context.Context) (Source, error) {
	raw, err := a.dump(ctx)
	return Source{XML: raw, Units: UnitsPixels}, err
}

// dump runs `uiautomator dump`, retrying a dump that failed.
func (a *Android) dump(ctx context.Context) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < snapshotRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(snapshotRetryDelay):
			}
		}
		raw, err := a.dumpOnce(ctx)
		if err == nil {
			return raw, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("could not capture the UI hierarchy after %d attempts: %w",
		snapshotRetries, lastErr)
}

func (a *Android) dumpOnce(ctx context.Context) ([]byte, error) {
	out, diag, err := a.adb.ShellDiagnostics(ctx, "uiautomator", "dump", dumpPath)
	if err != nil {
		return nil, err
	}
	// uiautomator reports its own failures without a non-zero exit — the exit
	// code says nothing about whether a dump was produced. It splits them
	// across both streams, so both are read: "killed" goes to stdout, while
	// "ERROR: could not get idle state." goes to stderr.
	if s := strings.TrimSpace(string(out)); !strings.Contains(s, dumpPath) {
		reason := strings.TrimSpace(strings.Join([]string{s, strings.TrimSpace(string(diag))}, " "))
		if strings.Contains(reason, "idle state") {
			// The screen never stops moving, so uiautomator never takes its
			// snapshot. An animation or a ticking clock is enough; the About
			// screen on a Pixel 7 has one, and this backend cannot read it at
			// all. UiAutomator2 reads it: Mobium caps its idle wait at half a
			// second (CHALLENGES 109), where uiautomator waits for idle forever.
			return nil, mobiumerr.New(mobiumerr.Timeout, "uiautomator dump gave up waiting for the screen to stop "+
				"changing (%s) — something on it animates or updates continuously. "+
				"Use --driver uiautomator2, which waits for idle at most half a second.", reason)
		}
		if reason == "" {
			reason = "no output"
		}
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "uiautomator dump produced no file: %s", reason)
	}

	xml, err := a.adb.ExecOut(ctx, "cat", dumpPath)
	// Delete it whether or not the read worked. The file is a full
	// serialization of whatever was on screen — on a real phone that means
	// message previews, calendar entries, whatever the user was looking at —
	// and leaving it in /data/local/tmp indefinitely is not ours to do. Found
	// on a Pixel 8 Pro after a session: 46KB of somebody's screen, still
	// sitting there.
	//
	// Best effort, and deliberately not reported: a snapshot that succeeded
	// should not fail because the tidying up did.
	_, _ = a.adb.Shell(ctx, "rm", "-f", dumpPath)

	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(xml)) == 0 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "uiautomator dump wrote an empty hierarchy")
	}
	return xml, nil
}

// Screenshot captures the framebuffer as PNG.
func (a *Android) Screenshot(ctx context.Context) ([]byte, error) {
	png, err := a.adb.ExecOut(ctx, "screencap", "-p")
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(png, pngMagic) {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "screencap returned %d bytes that are not a PNG", len(png))
	}
	return png, nil
}

// Tap touches a point in device pixels.
func (a *Android) Tap(ctx context.Context, x, y int) error {
	_, err := a.adb.Shell(ctx, "input", "tap", fmt.Sprint(x), fmt.Sprint(y))
	return err
}

// Swipe drags between two points using `adb shell input swipe`, whose
// duration argument is milliseconds.
func (a *Android) Swipe(ctx context.Context, x1, y1, x2, y2 int, d time.Duration) error {
	_, err := a.adb.Shell(ctx, "input", "swipe",
		fmt.Sprint(x1), fmt.Sprint(y1), fmt.Sprint(x2), fmt.Sprint(y2),
		fmt.Sprint(d.Milliseconds()))
	return err
}

// LongPress is a swipe that does not move, which is how `input` expresses a
// press-and-hold.
func (a *Android) LongPress(ctx context.Context, x, y int, d time.Duration) error {
	return a.Swipe(ctx, x, y, x, y, d)
}

// Launch brings an app to the foreground by package name.
func (a *Android) Launch(ctx context.Context, appID string) error {
	return a.adb.LaunchApp(ctx, appID)
}

// Terminate force-stops an app.
func (a *Android) Terminate(ctx context.Context, appID string) error {
	return a.adb.TerminateApp(ctx, appID)
}

// Install adds an APK.
func (a *Android) Install(ctx context.Context, path string) error {
	return a.adb.InstallApp(ctx, path)
}

// OpenURL opens a URL or deep link.
func (a *Android) OpenURL(ctx context.Context, url string) error {
	return a.adb.OpenURL(ctx, url)
}

// SetPermission grants or revokes one runtime permission.
func (a *Android) SetPermission(ctx context.Context, appID, permission string, grant bool) error {
	return a.adb.SetPermission(ctx, appID, permission, grant)
}

// ResetPermissions reverts runtime permissions to their defaults.
//
// `pm reset-permissions` is device-wide and has no per-package form, so a
// request to reset one app is refused rather than quietly clobbering every
// other app's permissions too.
func (a *Android) ResetPermissions(ctx context.Context, appID string) error {
	if appID != "" {
		return mobiumerr.New(mobiumerr.Unsupported, "Android cannot reset permissions for one app — "+
			"`pm reset-permissions` is device-wide. Reset every app by omitting the app, "+
			"or revoke the specific permissions you want back to denied.")
	}
	return a.adb.ResetPermissions(ctx)
}

// PermissionState reports what the app declares and what it has.
func (a *Android) PermissionState(ctx context.Context, appID string) (map[string]bool, error) {
	return a.adb.RuntimePermissions(ctx, appID)
}

// Appearance vocabulary. Android speaks yes/no/auto and iOS speaks
// light/dark, so both are translated at the driver rather than leaking a
// platform's spelling into the tool layer.
const (
	appearanceLight = "light"
	appearanceDark  = "dark"
	appearanceAuto  = "auto"
)

func nightToAppearance(night string) string {
	switch night {
	case "yes":
		return appearanceDark
	case "no":
		return appearanceLight
	default:
		// "auto", or a named custom schedule, both of which mean the system
		// is deciding. Reporting the platform's own word is more use than
		// flattening it to light or dark and being wrong half the day.
		return night
	}
}

func appearanceToNight(mode string) (string, error) {
	switch mode {
	case appearanceDark:
		return "yes", nil
	case appearanceLight:
		return "no", nil
	case appearanceAuto:
		return "auto", nil
	}
	return "", mobiumerr.New(mobiumerr.InvalidArgument, "unknown appearance %q (want %q, %q or %q)",
		mode, appearanceLight, appearanceDark, appearanceAuto)
}

// Appearance reports whether the device is in dark mode.
func (a *Android) Appearance(ctx context.Context) (string, error) {
	night, err := a.adb.NightMode(ctx)
	if err != nil {
		return "", err
	}
	return nightToAppearance(night), nil
}

// SetAppearance switches the device between light and dark.
func (a *Android) SetAppearance(ctx context.Context, mode string) error {
	night, err := appearanceToNight(mode)
	if err != nil {
		return err
	}
	return a.adb.SetNightMode(ctx, night)
}

// ListApps reports installed packages.
func (a *Android) ListApps(ctx context.Context, includeSystem bool) ([]device.InstalledApp, error) {
	return a.adb.ListPackages(ctx, includeSystem)
}

// ClearData deletes a package's data.
func (a *Android) ClearData(ctx context.Context, appID string) (device.ClearedData, error) {
	return a.adb.ClearAppData(ctx, appID)
}

// Uninstall removes a package.
func (a *Android) Uninstall(ctx context.Context, appID string) error {
	return a.adb.UninstallApp(ctx, appID)
}

// dumpLacks is what the uiautomator backend cannot do for want of a
// device-side server: it reads the screen with `uiautomator dump` and acts
// through `adb shell input`, and neither reaches a dialog's buttons or the
// clipboard.
var dumpLacks = map[string]string{
	CapClipboard: "the uiautomator backend cannot write the clipboard: it has no server on the device " +
		"to do it — use uiautomator2, the default",
	CapAlerts: "the uiautomator backend cannot read or answer dialogs through an alert endpoint: it " +
		"has no server on the device — use uiautomator2, the default, or tap the dialog's buttons " +
		"by their refs from map",
}

// DeclineReason names why this backend lacks a capability.
func (a *Android) DeclineReason(capability string) error {
	if reason, ok := dumpLacks[capability]; ok {
		return mobiumerr.New(mobiumerr.Unsupported, "%s", reason)
	}
	return nil
}
