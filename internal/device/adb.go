// Package device locates and drives the host-side tooling for a virtual
// device. Step 1 covers Android emulators via adb; the iOS simulator
// equivalent (simctl) will sit beside it behind the same Target interface.
package device

import (
	"bytes"
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNoADB is returned when the Android platform-tools are not installed.
var ErrNoADB = mobiumerr.New(mobiumerr.ToolchainMissing,
	"adb not found — install Android platform-tools and either put adb on PATH "+
		"or set ANDROID_HOME (e.g. `brew install --cask android-platform-tools`, "+
		"or Android Studio → SDK Manager → Android SDK Platform-Tools)")

// ErrNoDevice is returned when adb works but nothing is attached.
var ErrNoDevice = mobiumerr.New(mobiumerr.NoDevice,
	"no Android device or emulator is running — start one with "+
		"`emulator -avd <name>` or from Android Studio's Device Manager")

// defaultTimeout bounds any single adb invocation. A wedged adb server is the
// most common way a mobile run hangs forever, so nothing here runs unbounded.
const defaultTimeout = 30 * time.Second

// ADB is a located adb binary, optionally pinned to one device serial.
type ADB struct {
	Path   string
	Serial string
}

// FindADB locates adb via MOBIUM_ADB_PATH, PATH, then the conventional SDK
// locations. The explicit override comes first so a test run can point at a
// specific platform-tools build, mirroring vibium's VIBIUM_BIN_PATH.
func FindADB() (string, error) {
	if p := EnvPath("MOBIUM_ADB_PATH"); p != "" {
		if isExec(p) {
			return p, nil
		}
		// `C:\...\platform-tools\adb` is how the Unix spelling reads on
		// Windows, and CreateProcess would find adb.exe from it; os.Stat
		// does not.
		if exe := p + exeSuffix(); exe != p && isExec(exe) {
			return exe, nil
		}
		return "", mobiumerr.New(mobiumerr.ToolchainMissing, "MOBIUM_ADB_PATH=%s is not an executable file", p)
	}
	if p, err := exec.LookPath("adb"); err == nil {
		return p, nil
	}
	for _, root := range sdkRoots() {
		p := filepath.Join(root, "platform-tools", "adb"+exeSuffix())
		if isExec(p) {
			return p, nil
		}
	}
	return "", ErrNoADB
}

func sdkRoots() []string {
	var roots []string
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if v := EnvPath(env); v != "" {
			roots = append(roots, v)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		switch runtime.GOOS {
		case "darwin":
			roots = append(roots, filepath.Join(home, "Library", "Android", "sdk"))
		case "windows":
			roots = append(roots, filepath.Join(home, "AppData", "Local", "Android", "Sdk"))
		default:
			roots = append(roots, filepath.Join(home, "Android", "Sdk"))
		}
	}
	return roots
}

// EnvPath reads an environment variable that holds a path.
//
// On Windows, cmd.exe's `set ANDROID_HOME="C:\Program Files\Android"` keeps
// the quotes as part of the value, so the variable names a directory that
// cannot exist — a double quote is not allowed in a Windows file name — and
// every lookup under it fails as though the SDK were missing. The quotes are
// dropped there and nowhere else: on Unix a quote is a legal character in a
// path.
func EnvPath(name string) string {
	return cleanEnvPath(os.Getenv(name), runtime.GOOS)
}

func cleanEnvPath(v, goos string) string {
	if goos != "windows" {
		return v
	}
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	return v
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || fi.Mode()&0o111 != 0
}

// New locates adb and pins it to serial (empty selects the only device).
func New(serial string) (*ADB, error) {
	p, err := FindADB()
	if err != nil {
		return nil, err
	}
	return &ADB{Path: p, Serial: serial}, nil
}

func (a *ADB) args(rest ...string) []string {
	if a.Serial != "" {
		return append([]string{"-s", a.Serial}, rest...)
	}
	return rest
}

// Run executes an adb subcommand and returns stdout. Stderr is folded into
// the error, because adb reports most real failures there while exiting 0 —
// the exit code alone is not evidence the command did anything.
func (a *ADB) Run(ctx context.Context, rest ...string) ([]byte, error) {
	out, _, err := a.run(ctx, rest...)
	return out, err
}

// run is Run, also handing back stderr for callers that need to read the
// device command's own diagnostics.
func (a *ADB) run(ctx context.Context, rest ...string) (stdout, stderr []byte, err error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, a.Path, a.args(rest...)...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return nil, errBuf.Bytes(), mobiumerr.New(mobiumerr.Timeout, "adb %s timed out after %s",
			strings.Join(rest, " "), defaultTimeout)
	}
	if runErr != nil {
		// The reason is on whichever stream adb felt like using. A failing
		// `adb uninstall` exits 1 with "Failure [DELETE_FAILED_INTERNAL_ERROR]"
		// on **stdout** and nothing on stderr, so reporting stderr alone gave
		// an error message that ended in a bare colon.
		reason := strings.TrimSpace(errBuf.String())
		if reason == "" {
			reason = strings.TrimSpace(outBuf.String())
		}
		if reason == "" {
			reason = "no output"
		}
		return outBuf.Bytes(), errBuf.Bytes(), fmt.Errorf("adb %s: %w: %s",
			strings.Join(rest, " "), runErr, reason)
	}
	// adb prints "error: ..." to stderr and still exits 0 for a surprising
	// number of conditions (device offline, unauthorized).
	if e := strings.TrimSpace(errBuf.String()); strings.HasPrefix(e, "error:") {
		return outBuf.Bytes(), errBuf.Bytes(), mobiumerr.New(mobiumerr.DeviceServer, "adb %s: %s",
			strings.Join(rest, " "), e)
	}
	return outBuf.Bytes(), errBuf.Bytes(), nil
}

// Shell runs a command on the device via `adb shell`.
func (a *ADB) Shell(ctx context.Context, rest ...string) ([]byte, error) {
	return a.Run(ctx, append([]string{"shell"}, rest...)...)
}

// ShellDiagnostics runs a device command once and returns its stdout together
// with whatever it wrote to stderr.
//
// Run deliberately keeps the two apart and only reports stderr that adb itself
// marks with a lowercase "error:" prefix. A command running *on the device*
// does not follow that convention: `uiautomator dump` writes
// "ERROR: could not get idle state." to stderr, so the caller was left showing
// an empty reason for a failure that had a perfectly good explanation. Use
// this wherever the device command's own diagnostics are the thing worth
// reading.
func (a *ADB) ShellDiagnostics(ctx context.Context, rest ...string) (stdout, stderr []byte, err error) {
	return a.run(ctx, append([]string{"shell"}, rest...)...)
}

// ExecOut runs a command via `adb exec-out`, which does not translate line
// endings — required for anything binary, such as a PNG from screencap.
func (a *ADB) ExecOut(ctx context.Context, rest ...string) ([]byte, error) {
	return a.Run(ctx, append([]string{"exec-out"}, rest...)...)
}

// Device is one attached emulator or handset.
type Device struct {
	Serial   string `json:"serial"`
	State    string `json:"state"`
	Model    string `json:"model,omitempty"`
	Emulator bool   `json:"emulator"`
}

// Ready reports whether the device can accept commands.
func (d Device) Ready() bool { return d.State == "device" }

// Devices lists attached devices, parsed from `adb devices -l`.
func Devices(ctx context.Context) ([]Device, error) {
	a, err := New("")
	if err != nil {
		return nil, err
	}
	out, err := a.Run(ctx, "devices", "-l")
	if err != nil {
		return nil, err
	}
	return parseDevices(string(out)), nil
}

// parseDevices reads the `adb devices -l` table, skipping its header and the
// daemon-startup chatter adb emits on a cold start.
func parseDevices(out string) []Device {
	var devices []Device
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") ||
			strings.HasPrefix(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		d := Device{
			Serial:   fields[0],
			State:    fields[1],
			Emulator: strings.HasPrefix(fields[0], "emulator-"),
		}
		for _, f := range fields[2:] {
			if v, ok := strings.CutPrefix(f, "model:"); ok {
				d.Model = v
			}
		}
		devices = append(devices, d)
	}
	return devices
}

// Select resolves the device a command should target. An explicit serial must
// match exactly; otherwise exactly one ready device is required, so a run
// never silently picks a different emulator than the one the user meant.
func Select(ctx context.Context, serial string) (*ADB, *Device, error) {
	devices, err := Devices(ctx)
	if err != nil {
		return nil, nil, err
	}
	if serial != "" {
		for i := range devices {
			if devices[i].Serial == serial {
				if !devices[i].Ready() {
					return nil, nil, mobiumerr.New(mobiumerr.DeviceNotReady, "device %s is %s, not ready", serial, devices[i].State)
				}
				a, err := New(serial)
				return a, &devices[i], err
			}
		}
		return nil, nil, mobiumerr.New(mobiumerr.NoDevice, "no device with serial %q (see `mobium devices`)", serial)
	}

	var ready []Device
	for _, d := range devices {
		if d.Ready() {
			ready = append(ready, d)
		}
	}
	switch len(ready) {
	case 0:
		if len(devices) > 0 {
			return nil, nil, mobiumerr.New(mobiumerr.DeviceNotReady, "device %s is %s, not ready", devices[0].Serial, devices[0].State)
		}
		return nil, nil, ErrNoDevice
	case 1:
		a, err := New(ready[0].Serial)
		return a, &ready[0], err
	default:
		var names []string
		for _, d := range ready {
			names = append(names, d.Serial)
		}
		return nil, nil, mobiumerr.New(mobiumerr.InvalidArgument, "%d devices are ready (%s) — pick one with --device <serial>",
			len(ready), strings.Join(names, ", "))
	}
}

// Start launches an adb subcommand without waiting for it, for commands that
// are meant to keep running — `am instrument` hosting the UiAutomator2 server,
// for one. The caller owns the process and must kill it.
//
// Output is captured into the returned buffer so a server that dies on startup
// can say why; instrumentation reports its failures there and nowhere else.
func (a *ADB) Start(ctx context.Context, rest ...string) (*exec.Cmd, *SyncBuffer, error) {
	cmd := exec.CommandContext(ctx, a.Path, a.args(rest...)...)
	buf := &SyncBuffer{}
	cmd.Stdout = buf
	cmd.Stderr = buf
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start adb %s: %w", strings.Join(rest, " "), err)
	}
	return cmd, buf, nil
}

// SyncBuffer is a bytes.Buffer safe for a process writing while a reader
// inspects it.
type SyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *SyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns what has been written so far.
func (b *SyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Forward maps a free local TCP port to a port on the device and returns the
// local port. Asking adb for port 0 lets it allocate, so two devices can each
// have a server without the caller inventing a port scheme.
func (a *ADB) Forward(ctx context.Context, devicePort int) (int, error) {
	out, err := a.Run(ctx, "forward", "tcp:0", fmt.Sprintf("tcp:%d", devicePort))
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, mobiumerr.New(mobiumerr.DeviceServer, "adb forward returned %q, not a port", strings.TrimSpace(string(out)))
	}
	return port, nil
}

// RemoveForward tears down a forward created by Forward.
func (a *ADB) RemoveForward(ctx context.Context, localPort int) error {
	_, err := a.Run(ctx, "forward", "--remove", fmt.Sprintf("tcp:%d", localPort))
	return err
}

// RemoveForwardsTo tears down every forward from this host to devicePort on
// this device, and says how many there were.
//
// Forward allocates a fresh local port each time, so a daemon that died
// without tearing down — killed, or crashed — left its forward behind, and
// the next session added another beside it. Measured after a kill -9 on an
// Android 15 emulator: two forwards to UiAutomator2's port, the dead one
// outliving even the next daemon's clean stop. Only devicePort's: another
// tool's forwards on the same device are not ours to remove.
func (a *ADB) RemoveForwardsTo(ctx context.Context, devicePort int) (int, error) {
	out, err := a.Run(ctx, "forward", "--list")
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, local := range forwardsTo(string(out), a.Serial, fmt.Sprintf("tcp:%d", devicePort)) {
		if _, err := a.Run(ctx, "forward", "--remove", local); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// forwardsTo reads `adb forward --list` — one "serial local remote" line per
// forward, for every device — into the local ends of serial's forwards to
// remote.
func forwardsTo(list, serial, remote string) []string {
	var out []string
	for _, line := range strings.Split(list, "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == serial && f[2] == remote {
			out = append(out, f[1])
		}
	}
	return out
}

// ForwardAbstract maps a free local TCP port to an abstract unix socket on the
// device, which is how WebView and Chrome devtools endpoints are published.
func (a *ADB) ForwardAbstract(ctx context.Context, socketName string) (int, error) {
	out, err := a.Run(ctx, "forward", "tcp:0", "localabstract:"+socketName)
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, mobiumerr.New(mobiumerr.DeviceServer, "adb forward returned %q, not a port", strings.TrimSpace(string(out)))
	}
	return port, nil
}

// LaunchApp brings an app to the foreground by package name.
//
// The launcher activity is resolved rather than assumed: `am start` needs a
// component, and only the package is known. `monkey` is the usual shortcut for
// this but it synthesises input events and reports success even when it
// launched nothing.
func (a *ADB) LaunchApp(ctx context.Context, pkg string) error {
	out, err := a.Shell(ctx, "cmd", "package", "resolve-activity", "--brief", pkg)
	if err != nil {
		return fmt.Errorf("resolve launcher activity for %s: %w", pkg, err)
	}
	component := ""
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "/") && !strings.HasPrefix(line, "No ") {
			component = line
		}
	}
	if component == "" {
		return mobiumerr.New(mobiumerr.InvalidArgument, "%s has no launchable activity (is it installed?)", pkg)
	}

	started, err := a.Shell(ctx, "am", "start", "-n", component)
	if err != nil {
		return err
	}
	// `am start` prints its failures on stdout and still exits 0.
	if e := string(started); strings.Contains(e, "Error:") {
		return mobiumerr.New(mobiumerr.DeviceServer, "launch %s: %s", pkg, strings.TrimSpace(firstLine(e)))
	}
	return nil
}

// TerminateApp force-stops an app.
func (a *ADB) TerminateApp(ctx context.Context, pkg string) error {
	_, err := a.Shell(ctx, "am", "force-stop", pkg)
	return err
}

// InstallApp installs an APK, replacing any existing copy.
func (a *ADB) InstallApp(ctx context.Context, path string) error {
	out, err := a.Run(ctx, "install", "-r", "-g", path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(out), "Success") {
		return mobiumerr.New(mobiumerr.DeviceServer, "install %s: %s", filepath.Base(path), strings.TrimSpace(string(out)))
	}
	return nil
}

// OpenURL opens a URL or deep link.
func (a *ADB) OpenURL(ctx context.Context, url string) error {
	out, err := a.Shell(ctx, "am", "start", "-a", "android.intent.action.VIEW", "-d", url)
	if err != nil {
		return err
	}
	if e := string(out); strings.Contains(e, "Error:") {
		return mobiumerr.New(mobiumerr.DeviceServer, "open %s: %s", url, strings.TrimSpace(firstLine(e)))
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// runtimePermsRe pulls one line out of `dumpsys package`'s runtime
// permissions block: the permission name and whether it is granted.
var runtimePermsRe = regexp.MustCompile(`^\s+(android\.permission\.[A-Z_0-9]+|[a-zA-Z0-9._]+\.permission\.[A-Z_0-9]+):\s+granted=(true|false)`)

// RuntimePermissions reports every runtime permission the app declares and
// whether each is currently granted.
//
// This is the only honest way to know whether a grant worked. `pm grant`
// exits 0 and prints nothing at all when the app never declared the
// permission, having changed nothing — verified on the emulator against
// com.android.settings and BODY_SENSORS. Anything that trusts the command
// instead of reading the state back is reporting its own hope.
func (a *ADB) RuntimePermissions(ctx context.Context, pkg string) (map[string]bool, error) {
	out, err := a.Shell(ctx, "dumpsys", "package", pkg)
	if err != nil {
		return nil, err
	}
	text := string(out)
	if strings.Contains(text, "Unable to find package") {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed", pkg)
	}

	perms := map[string]bool{}
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "runtime permissions:") {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		m := runtimePermsRe.FindStringSubmatch(line)
		if m == nil {
			// The block ends at the first line that is not a permission. It
			// appears once per user on a multi-user device; taking only the
			// first is right, since that is the user adb is acting as.
			if strings.TrimSpace(line) != "" {
				break
			}
			continue
		}
		perms[m[1]] = m[2] == "true"
	}
	if len(perms) == 0 {
		return nil, mobiumerr.Wrap(mobiumerr.InvalidArgument, ErrNoRuntimePermissions,
			"%s declares no runtime permissions", pkg)
	}
	return perms, nil
}

// ErrNoRuntimePermissions is the cause when an app declares none: a grant
// has nothing to grant, and a reset had nothing to reset.
var ErrNoRuntimePermissions = mobiumerr.New(mobiumerr.InvalidArgument, "no runtime permissions declared")

// SetPermission grants or revokes one runtime permission.
//
// `pm` reports its refusals as a Java exception on **stderr** while exiting 0,
// which ADB.Run does not surface because adb's own convention is a lowercase
// "error:" prefix. Both streams are read here for that reason.
func (a *ADB) SetPermission(ctx context.Context, pkg, perm string, grant bool) error {
	verb := "revoke"
	if grant {
		verb = "grant"
	}
	out, diag, err := a.ShellDiagnostics(ctx, "pm", verb, pkg, perm)
	if err != nil {
		return err
	}
	for _, s := range []string{string(diag), string(out)} {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.Contains(s, "Exception") || strings.Contains(s, "Error:") ||
			strings.Contains(s, "Failure") {
			return mobiumerr.New(mobiumerr.DeviceServer, "pm %s %s %s: %s", verb, pkg, perm, lastLine(s))
		}
	}
	return nil
}

// ResetPermissions reverts runtime permissions to their defaults.
//
// Device-wide, and there is no per-package form: `pm reset-permissions` takes
// no package argument and its own help says "Revert all runtime permissions to
// their default state". Callers must not present this as resetting one app.
func (a *ADB) ResetPermissions(ctx context.Context) error {
	_, err := a.Shell(ctx, "pm", "reset-permissions")
	return err
}

// lastLine is the most specific part of a Java stack trace's preamble: the
// exception line, rather than the "Exception occurred while executing" header
// above it.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return s
}

// nightModeRe reads `cmd uimode night`, which answers "Night mode: yes".
var nightModeRe = regexp.MustCompile(`(?i)night mode:\s*(\w+)`)

// NightMode reports the device's dark-mode setting: "yes", "no", "auto" or a
// schedule name.
func (a *ADB) NightMode(ctx context.Context) (string, error) {
	out, err := a.Shell(ctx, "cmd", "uimode", "night")
	if err != nil {
		return "", err
	}
	m := nightModeRe.FindStringSubmatch(string(out))
	if m == nil {
		return "", mobiumerr.New(mobiumerr.DeviceServer, "could not read the night mode setting from %q",
			strings.TrimSpace(string(out)))
	}
	return strings.ToLower(m[1]), nil
}

// SetNightMode turns dark mode on ("yes"), off ("no") or hands it back to the
// system ("auto"), and confirms it took.
//
// `cmd uimode` echoes the value it thinks it set, which is not the same as the
// setting having changed — the same shape as `pm grant`. Reading it back is
// cheap and is the only thing that makes the answer worth anything.
func (a *ADB) SetNightMode(ctx context.Context, mode string) error {
	if _, err := a.Shell(ctx, "cmd", "uimode", "night", mode); err != nil {
		return err
	}
	got, err := a.NightMode(ctx)
	if err != nil {
		return err
	}
	if got != mode {
		return mobiumerr.New(mobiumerr.NotConfirmed, "asked for night mode %q but the device reports %q", mode, got)
	}
	return nil
}

// InstalledApp is one app on a device, in the fields both platforms can
// answer for. Name is empty on Android, where reading a package's label costs
// a `dumpsys` per app and is not worth it for a listing.
type InstalledApp struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	System  bool   `json:"system"`
}

// packageLineRe reads `pm list packages --show-versioncode`, which answers
// "package:com.example versionCode:12".
var packageLineRe = regexp.MustCompile(`^package:(\S+)(?:\s+versionCode:(\S+))?`)

// ListPackages lists installed packages. Without includeSystem it lists only
// what someone installed, which on a stock emulator is a handful of names
// rather than the 240 the platform ships.
func (a *ADB) ListPackages(ctx context.Context, includeSystem bool) ([]InstalledApp, error) {
	args := []string{"pm", "list", "packages", "--show-versioncode"}
	if !includeSystem {
		args = append(args, "-3")
	}
	out, err := a.Shell(ctx, args...)
	if err != nil {
		return nil, err
	}

	system := map[string]bool{}
	if includeSystem {
		// -s lists the system packages, which is how a combined listing can
		// still say which is which.
		if sys, err := a.Shell(ctx, "pm", "list", "packages", "-s"); err == nil {
			for _, line := range strings.Split(string(sys), "\n") {
				if m := packageLineRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
					system[m[1]] = true
				}
			}
		}
	}

	var apps []InstalledApp
	for _, line := range strings.Split(string(out), "\n") {
		m := packageLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		apps = append(apps, InstalledApp{ID: m[1], Version: m[2], System: system[m[1]]})
	}
	return apps, nil
}

// UninstallApp removes an app.
//
// `adb uninstall` answers "Success" or "Failure [REASON]" on stdout and exits
// 0 either way, so the word is the only thing worth reading.
func (a *ADB) UninstallApp(ctx context.Context, pkg string) error {
	out, err := a.Run(ctx, "uninstall", pkg)
	if err != nil {
		return err
	}
	if !strings.Contains(string(out), "Success") {
		return mobiumerr.New(mobiumerr.DeviceServer, "uninstall %s: %s", pkg, strings.TrimSpace(firstLine(string(out))))
	}
	return nil
}

// rotationRe reads the rotation a display is actually showing.
//
// `dumpsys window` offers three spellings of this — `mRotation=1`,
// `mRotation=ROTATION_90` and `mCurrentRotation=ROTATION_90` — in the same
// output, on different objects, and one of them is `undefined` on a display
// that is not the real one. `dumpsys window displays` narrows it to the
// display, and mCurrentRotation is the one that means "what is on screen now"
// rather than "what was requested".
var rotationRe = regexp.MustCompile(`mCurrentRotation=ROTATION_(\d+)`)

// userRotationRe reads `cmd window user-rotation`, which answers either
// "free" or "lock <n>" — the request, not the result. The two differ whenever
// auto-rotation is on, which is why both are read.
var userRotationRe = regexp.MustCompile(`^\s*(free|lock)\s*(\d*)`)

// Rotation reports the rotation the display is actually showing, as a quarter
// turn count: 0, 1, 2 or 3.
//
// The number in `mCurrentRotation=ROTATION_90` is **degrees**, while
// `user_rotation` and `cmd window user-rotation lock <n>` are **quarter
// turns**. The two are trivially convertible and trivially confusable: reading
// the degrees as quarters accepts 0 and rejects everything else, so a portrait
// screen looks fine and the first rotation fails.
func (a *ADB) Rotation(ctx context.Context) (int, error) {
	out, err := a.Shell(ctx, "dumpsys", "window", "displays")
	if err != nil {
		return 0, err
	}
	m := rotationRe.FindSubmatch(out)
	if m == nil {
		return 0, mobiumerr.New(mobiumerr.DeviceServer, "could not read the display rotation from dumpsys")
	}
	degrees, err := strconv.Atoi(string(m[1]))
	if err != nil || degrees%90 != 0 || degrees < 0 || degrees > 270 {
		return 0, mobiumerr.New(mobiumerr.DeviceServer, "the display reported rotation %q, which is not a quarter turn", m[1])
	}
	return degrees / 90, nil
}

// RotationLocked reports whether the rotation is pinned rather than following
// the sensor.
func (a *ADB) RotationLocked(ctx context.Context) (bool, error) {
	out, err := a.Shell(ctx, "cmd", "window", "user-rotation")
	if err != nil {
		return false, err
	}
	m := userRotationRe.FindSubmatch(bytes.TrimSpace(out))
	if m == nil {
		return false, mobiumerr.New(mobiumerr.DeviceServer, "could not read the rotation lock from %q",
			strings.TrimSpace(string(out)))
	}
	return string(m[1]) == "lock", nil
}

// SetRotation pins the display to a quarter turn and confirms it took.
//
// Pinning is deliberate rather than incidental: a rotation that the sensor can
// undo a moment later is not a rotation a test can rely on. Handing it back is
// `SetRotationAuto`.
func (a *ADB) SetRotation(ctx context.Context, quarter int) error {
	if quarter < 0 || quarter > 3 {
		return mobiumerr.New(mobiumerr.InvalidArgument, "rotation must be 0, 1, 2 or 3, not %d", quarter)
	}
	if _, err := a.Shell(ctx, "cmd", "window", "user-rotation", "lock",
		strconv.Itoa(quarter)); err != nil {
		return err
	}
	// The command says nothing about whether the screen moved, and the screen
	// takes a moment to get there. Poll the display rather than believe it.
	return a.awaitRotation(ctx, quarter)
}

// SetRotationAuto hands rotation back to the sensor.
func (a *ADB) SetRotationAuto(ctx context.Context) error {
	if _, err := a.Shell(ctx, "cmd", "window", "user-rotation", "free"); err != nil {
		return err
	}
	locked, err := a.RotationLocked(ctx)
	if err != nil {
		return err
	}
	if locked {
		return mobiumerr.New(mobiumerr.NotConfirmed, "asked to release the rotation lock and the device still reports it locked")
	}
	return nil
}

// awaitRotation waits for the display to actually reach a rotation.
func (a *ADB) awaitRotation(ctx context.Context, want int) error {
	deadline := time.Now().Add(5 * time.Second)
	var last int
	for {
		got, err := a.Rotation(ctx)
		if err != nil {
			return err
		}
		if got == want {
			return nil
		}
		last = got
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	// Stated as a fact with the likeliest cause offered, not asserted. When
	// this fired during development the real cause was a unit mismatch in the
	// reader, and a message that confidently blamed the app would have sent
	// the reader looking in the wrong place — the same trap as an error whose
	// suggested remedy cannot work.
	return mobiumerr.New(mobiumerr.NotConfirmed, "asked for rotation %d and the display is still showing %d. "+
		"The usual cause is an activity that pins its own orientation, which cannot be "+
		"rotated from outside; check with `adb shell dumpsys window displays`", want, last)
}

// appLocalesRe reads `cmd locale get-app-locales`, which answers
// "Locales for <pkg> for user 0 are [ja-JP]" — or "[]" for an app that
// follows the device.
var appLocalesRe = regexp.MustCompile(`are \[([^\]]*)\]`)

// AppLocales reports the language tags pinned for one app. An empty result
// means the app follows the device.
//
// Per-app rather than device-wide on purpose. Changing the device locale means
// writing `persist.sys.locale` and restarting the framework — slow, disruptive,
// and it moves everything. Per-app is reversible, needs no restart, and can be
// read back, which is the only reason it is worth offering at all.
func (a *ADB) AppLocales(ctx context.Context, pkg string) ([]string, error) {
	out, diag, err := a.ShellDiagnostics(ctx, "cmd", "locale", "get-app-locales", pkg)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(out) + " " + string(diag))
	// `cmd locale` reports an unknown package in prose and exits 0, so the
	// message is the only signal there is.
	if strings.Contains(text, "Unknown package") {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed", pkg)
	}
	m := appLocalesRe.FindStringSubmatch(text)
	if m == nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "could not read the locale for %s from %q", pkg, text)
	}
	var tags []string
	for _, t := range strings.Split(m[1], ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return tags, nil
}

// SetAppLocales pins an app's language, or clears it with an empty list, and
// confirms the setting took.
//
// What it confirms is that **the device stored the tag**, not that the app has
// a translation for it: `zz-ZZ` is accepted and stored just as happily as
// `ja-JP`, and the app then quietly renders in its default language. Android
// exposes nothing that would distinguish the two, so callers are told which
// question has been answered rather than left to assume the stronger one.
func (a *ADB) SetAppLocales(ctx context.Context, pkg string, tags []string) error {
	args := []string{"cmd", "locale", "set-app-locales", pkg, "--locales",
		strings.Join(tags, ",")}
	out, diag, err := a.ShellDiagnostics(ctx, args...)
	if err != nil {
		return err
	}
	if text := string(out) + string(diag); strings.Contains(text, "Unknown package") {
		return mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed", pkg)
	}

	got, err := a.AppLocales(ctx, pkg)
	if err != nil {
		return err
	}
	if strings.Join(got, ",") != strings.Join(tags, ",") {
		return mobiumerr.New(mobiumerr.NotConfirmed, "asked to set %s to %q but the device reports %q",
			pkg, strings.Join(tags, ","), strings.Join(got, ","))
	}
	return nil
}

// DeviceLocale reports the device's own locale, which an app follows unless it
// has been pinned.
func (a *ADB) DeviceLocale(ctx context.Context) (string, error) {
	for _, prop := range []string{"persist.sys.locale", "ro.product.locale"} {
		out, err := a.Shell(ctx, "getprop", prop)
		if err != nil {
			return "", err
		}
		if v := strings.TrimSpace(string(out)); v != "" {
			return v, nil
		}
	}
	return "", mobiumerr.New(mobiumerr.DeviceServer, "the device reports no locale")
}

// wakefulnessRe reads whether the device is awake. `dumpsys power` prints
// mWakefulness=Awake / Asleep / Dozing.
var wakefulnessRe = regexp.MustCompile(`mWakefulness=([A-Za-z]+)`)

// keyguardRe reads whether the lock screen is up. A device can be awake and
// still locked, so both this and wakefulness are needed to answer "is it
// locked" — checking only one gives the right answer on a device with no
// lock set and the wrong one on anybody's phone.
var keyguardRe = regexp.MustCompile(`(?i)isKeyguardShowing=([a-z]+)`)

// PressKey sends one hardware key event.
//
// `input keyevent` accepts any name it knows and says nothing about whether
// anything happened — there is no outcome to read for most keys, which is why
// the layer above confirms the ones it can and says so for the ones it cannot.
func (a *ADB) PressKey(ctx context.Context, keycode string) error {
	out, diag, err := a.ShellDiagnostics(ctx, "input", "keyevent", keycode)
	if err != nil {
		return err
	}
	// An unknown keycode is reported on stderr with a zero exit, as usual.
	if text := strings.TrimSpace(string(out) + " " + string(diag)); text != "" {
		if strings.Contains(text, "Error") || strings.Contains(text, "Exception") ||
			strings.Contains(text, "Unknown") {
			return mobiumerr.New(mobiumerr.DeviceServer, "input keyevent %s: %s", keycode, firstLine(text))
		}
	}
	return nil
}

// HomePackage resolves which app is the launcher, so pressing home can be
// confirmed rather than assumed.
func (a *ADB) HomePackage(ctx context.Context) (string, error) {
	out, err := a.Shell(ctx, "cmd", "package", "resolve-activity", "--brief",
		"-c", "android.intent.category.HOME", "-a", "android.intent.action.MAIN")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "/") && !strings.HasPrefix(line, "No ") {
			if pkg, _, ok := strings.Cut(line, "/"); ok {
				return pkg, nil
			}
		}
	}
	return "", mobiumerr.New(mobiumerr.DeviceServer, "could not work out which app is the launcher")
}

// ScreenLocked reports whether the screen is off or showing the lock screen.
func (a *ADB) ScreenLocked(ctx context.Context) (bool, error) {
	power, err := a.Shell(ctx, "dumpsys", "power")
	if err != nil {
		return false, err
	}
	m := wakefulnessRe.FindSubmatch(power)
	if m == nil {
		return false, mobiumerr.New(mobiumerr.DeviceServer, "could not read whether the device is awake")
	}
	if !strings.EqualFold(string(m[1]), "Awake") {
		return true, nil
	}
	// Awake is not the same as unlocked: a phone with a PIN wakes to the lock
	// screen. The emulator usually has none, which is exactly why this is easy
	// to get wrong and only wrong on real hardware.
	win, err := a.Shell(ctx, "dumpsys", "window")
	if err != nil {
		return false, err
	}
	if k := keyguardRe.FindSubmatch(win); k != nil {
		return strings.EqualFold(string(k[1]), "true"), nil
	}
	return false, nil
}

// SetScreenLocked puts the screen to sleep or wakes it, and confirms it.
//
// KEYCODE_SLEEP and KEYCODE_WAKEUP are used rather than KEYCODE_POWER because
// power is a *toggle*: asking for it twice leaves the device where it started,
// and a caller who wanted it locked cannot tell which way it went. These two
// name the state they want and are idempotent — verified: sleeping an already
// asleep device leaves it asleep.
func (a *ADB) SetScreenLocked(ctx context.Context, locked bool) error {
	key := "KEYCODE_WAKEUP"
	if locked {
		key = "KEYCODE_SLEEP"
	}
	if err := a.PressKey(ctx, key); err != nil {
		return err
	}
	if !locked {
		// Waking leaves the lock screen up on any device that has one. This
		// dismisses it where no credential is required, and is harmless where
		// one is — it simply will not work, and the check below says so.
		_, _ = a.Shell(ctx, "wm", "dismiss-keyguard")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := a.ScreenLocked(ctx)
		if err != nil {
			return err
		}
		if got == locked {
			return nil
		}
		if time.Now().After(deadline) {
			if locked {
				return mobiumerr.New(mobiumerr.NotConfirmed, "asked to lock the screen and the device is still awake")
			}
			return mobiumerr.New(mobiumerr.Unsupported, "the screen woke but is still locked — a device with a PIN, "+
				"pattern or password cannot be unlocked from outside, and mobium will not "+
				"pretend otherwise")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// callStateRe reads the telephony call state: 0 idle, 1 ringing, 2 in a call.
var callStateRe = regexp.MustCompile(`mCallState=(\d)`)

// IsEmulator reports whether this device is an emulator.
//
// It matters because the console commands below — `adb emu …` — exist only on
// one. A real phone cannot be made to ring from outside, and saying so is much
// better than a console connection failing with something obscure.
func (a *ADB) IsEmulator(ctx context.Context) bool {
	out, err := a.Shell(ctx, "getprop", "ro.boot.qemu")
	if err == nil && strings.TrimSpace(string(out)) == "1" {
		return true
	}
	out, err = a.Shell(ctx, "getprop", "ro.kernel.qemu")
	return err == nil && strings.TrimSpace(string(out)) == "1"
}

// CallState reports the telephony state: 0 idle, 1 ringing, 2 in a call.
func (a *ADB) CallState(ctx context.Context) (int, error) {
	out, err := a.Shell(ctx, "dumpsys", "telephony.registry")
	if err != nil {
		return 0, err
	}
	m := callStateRe.FindSubmatch(out)
	if m == nil {
		return 0, mobiumerr.New(mobiumerr.DeviceServer, "this device reports no telephony state")
	}
	n, _ := strconv.Atoi(string(m[1]))
	return n, nil
}

// emuConsole runs one emulator console command, refusing on real hardware.
func (a *ADB) emuConsole(ctx context.Context, args ...string) error {
	out, diag, err := a.run(ctx, append([]string{"emu"}, args...)...)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(string(out) + " " + string(diag))
	// The console answers "OK" or "KO: <reason>", and exits 0 either way.
	if strings.Contains(text, "KO") {
		return mobiumerr.New(mobiumerr.Unsupported, "the emulator refused `%s`: %s",
			strings.Join(args, " "), firstLine(text))
	}
	return nil
}

// Call drives a simulated incoming call. action is "ring", "accept" or "hang".
//
// Emulator only, and the caller is told so rather than being left with a
// console error: a real phone cannot be made to ring from outside, which is a
// property of phones rather than a gap in mobium.
func (a *ADB) Call(ctx context.Context, action, number string) error {
	verb := map[string]string{"ring": "call", "accept": "accept", "hang": "cancel"}[action]
	if verb == "" {
		return mobiumerr.New(mobiumerr.InvalidArgument, "unknown call action %q (want \"ring\", \"accept\" or \"hang\")", action)
	}
	if err := a.emuConsole(ctx, "gsm", verb, number); err != nil {
		return err
	}

	// Confirmed against the telephony state rather than the console's "OK",
	// which only means the command was understood.
	want := map[string]int{"ring": 1, "accept": 2, "hang": 0}[action]
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := a.CallState(ctx)
		if err != nil {
			return err
		}
		if got == want {
			return nil
		}
		if time.Now().After(deadline) {
			return mobiumerr.New(mobiumerr.NotConfirmed, "asked to %s a call and the device reports call state %d, not %d",
				action, got, want)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// SendSMS delivers a simulated text message. Emulator only.
func (a *ADB) SendSMS(ctx context.Context, from, text string) error {
	return a.emuConsole(ctx, "sms", "send", from, text)
}

// Timezone reports the device's timezone.
func (a *ADB) Timezone(ctx context.Context) (string, error) {
	out, err := a.Shell(ctx, "getprop", "persist.sys.timezone")
	if err != nil {
		return "", err
	}
	if tz := strings.TrimSpace(string(out)); tz != "" {
		return tz, nil
	}
	return "", mobiumerr.New(mobiumerr.DeviceServer, "the device reports no timezone")
}

// SetTimezone changes the device timezone and confirms it.
//
// Through `service call alarm` rather than `setprop`, because the property is
// only where the value is stored: setting it directly leaves every running app
// on the old zone until something restarts. The alarm service tells the
// framework, so clocks move immediately — verified by reading the device's own
// `date` afterwards.
func (a *ADB) SetTimezone(ctx context.Context, tz string) error {
	if strings.TrimSpace(tz) == "" {
		return mobiumerr.New(mobiumerr.InvalidArgument, "a timezone is needed, e.g. \"Asia/Tokyo\"")
	}
	if _, err := a.Shell(ctx, "service", "call", "alarm", "3", "s16", tz); err != nil {
		return err
	}
	got, err := a.Timezone(ctx)
	if err != nil {
		return err
	}
	if got != tz {
		return mobiumerr.New(mobiumerr.NotConfirmed, "asked for timezone %q and the device reports %q — "+
			"an unknown zone name is accepted and ignored", tz, got)
	}
	return nil
}

// notificationKeyRe finds the start of each notification record. The dump also
// contains `groupKey=`, which must not match, hence the line anchor.
var notificationKeyRe = regexp.MustCompile(`(?m)^\s*key=(\S+)`)

// notificationTitleRe and notificationTextRe read the two fields worth having.
// The value runs to the last `)` on the line, because a notification body may
// contain one and a lazy match would truncate it.
var notificationTitleRe = regexp.MustCompile(`android\.title=String \((.*)\)`)
var notificationTextRe = regexp.MustCompile(`android\.text=String \((.*)\)`)

// Notification is one entry in the shade.
type Notification struct {
	Package string `json:"package"`
	Title   string `json:"title,omitempty"`
	Text    string `json:"text,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Key     string `json:"key"`
}

// Notifications lists what is currently posted.
//
// `--noredact` matters: without it the text of anything the system considers
// sensitive is replaced, and an assertion against it would pass or fail for
// reasons that have nothing to do with the app.
func (a *ADB) Notifications(ctx context.Context) ([]Notification, error) {
	out, err := a.Shell(ctx, "dumpsys", "notification", "--noredact")
	if err != nil {
		return nil, err
	}
	text := string(out)
	locs := notificationKeyRe.FindAllStringSubmatchIndex(text, -1)

	var list []Notification
	for i, loc := range locs {
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		key := text[loc[2]:loc[3]]
		record := text[loc[1]:end]

		n := Notification{Key: key}
		// key is user|package|id|tag|uid, with an extra field on the ranker's
		// own synthetic entry.
		if parts := strings.Split(key, "|"); len(parts) >= 4 {
			n.Package, n.Tag = parts[1], parts[3]
		}
		if m := notificationTitleRe.FindStringSubmatch(record); m != nil {
			n.Title = m[1]
		}
		if m := notificationTextRe.FindStringSubmatch(record); m != nil {
			n.Text = m[1]
		}
		// The ranker posts a synthetic grouping record with no content; it is
		// bookkeeping rather than something a user ever sees.
		if n.Title == "" && n.Text == "" {
			continue
		}
		list = append(list, n)
	}
	return list, nil
}

// shellQuote wraps a value for `adb shell`, which runs its argument through a
// shell on the device.
//
// Single quotes, with embedded single quotes closed and reopened. Without this
// a notification body of "Another body" arrives as "Another" — the space ends
// the argument — and one containing an apostrophe or a dollar sign is worse.
// Verified against text with spaces, apostrophes, double quotes, `$` and `%`.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// PostNotification posts a notification as the shell user and confirms it
// arrived, which is the only reason it is worth offering: `cmd notification
// post` prints the notification it thinks it built and says nothing about
// whether the system accepted it.
func (a *ADB) PostNotification(ctx context.Context, tag, title, text string) error {
	if tag == "" {
		tag = "mobium"
	}
	cmd := fmt.Sprintf("cmd notification post -S bigtext -t %s %s %s",
		shellQuote(title), shellQuote(tag), shellQuote(text))
	if _, err := a.Shell(ctx, cmd); err != nil {
		return err
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		list, err := a.Notifications(ctx)
		if err != nil {
			return err
		}
		for _, n := range list {
			if n.Tag == tag && n.Title == title && n.Text == text {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return mobiumerr.New(mobiumerr.NotConfirmed, "posted a notification tagged %q and it is not in the shade", tag)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// SetShade opens or closes the notification panel.
//
// Worth having because a notification cannot be tapped until it is on screen:
// posting one is half of the interruption, and interacting with it is the
// other half.
func (a *ADB) SetShade(ctx context.Context, open bool) error {
	verb := "collapse"
	if open {
		verb = "expand-notifications"
	}
	_, err := a.Shell(ctx, "cmd", "statusbar", verb)
	return err
}

// Fix is a position read back off the device.
type Fix struct {
	Lat, Lon float64
	// Mock is true when this fix came from a test provider rather than from
	// whatever the device last believed on its own. Without it a read is not
	// evidence that a set took: an AVD holds its previous position across
	// boots, so asking for a place it is already at reads back identically
	// whether or not anything happened.
	Mock bool
	// Mocking is true when a test provider is installed *now*. It is a
	// different question from Mock and they disagree in a way that matters:
	// removing the provider does not clear Android's last-known location, so
	// straight after a clear the device still reports the injected fix, still
	// tagged mock, from a provider that no longer exists. Reporting only Mock
	// would say "injected" about a device that is no longer being lied to.
	Mocking bool
}

// mockProviderRe reads whether a test provider is installed. `dumpsys
// location` labels the line `gps provider [mock]:` while one is, and plain
// `gps provider:` once it is removed — which is the only part of that output
// that changes when the provider goes away.
var mockProviderRe = regexp.MustCompile(`gps provider \[mock\]`)

// mockFixRe reads a fix out of `dumpsys location`. The line is
//
//	last location=Location[gps 51.507400,-0.127800 hAcc=100.0 et=+18m mock]
//
// and the trailing `mock` is what separates an injected fix from a real one.
var mockFixRe = regexp.MustCompile(`last location=Location\[gps (-?\d+(?:\.\d+)?),(-?\d+(?:\.\d+)?)([^\]]*)\]`)

// SetMockLocation places the device at a coordinate and confirms it took.
//
// Through a **test provider**, not `adb emu geo fix`. That command is
// emulator-only and pushes a fix into the emulated GPS, where it lands nowhere
// until something requests location: the provider sits at
// `ProviderRequest[OFF]` with `last location=null`, which reads exactly like
// "this cannot be verified" and actually means "nobody asked". A test provider
// is set, moved, read back and removed observably, and works on real hardware
// as well as on an emulator. See CHALLENGES 50.
func (a *ADB) SetMockLocation(ctx context.Context, lat, lon float64) error {
	// The shell uid must be allowed to mock before the provider will take one.
	if _, _, err := a.ShellDiagnostics(ctx, "appops", "set", "2000",
		"android:mock_location", "allow"); err != nil {
		return err
	}
	// Adding a provider that is already there is not an error worth failing
	// on -- the next two calls are what decide whether this worked, and they
	// are checked by reading the position back.
	_, _, _ = a.ShellDiagnostics(ctx, "cmd", "location", "providers",
		"add-test-provider", "gps")
	if _, _, err := a.ShellDiagnostics(ctx, "cmd", "location", "providers",
		"set-test-provider-enabled", "gps", "true"); err != nil {
		return err
	}

	coord := strconv.FormatFloat(lat, 'f', 6, 64) + "," + strconv.FormatFloat(lon, 'f', 6, 64)
	out, diag, err := a.ShellDiagnostics(ctx, "cmd", "location", "providers",
		"set-test-provider-location", "gps", "--location", coord)
	if err != nil {
		return err
	}
	if text := string(out) + string(diag); strings.Contains(text, "Error") ||
		strings.Contains(text, "Exception") {
		return mobiumerr.New(mobiumerr.DeviceServer, "the device refused the position %s: %s", coord, strings.TrimSpace(text))
	}

	got, err := a.MockLocation(ctx)
	if err != nil {
		return err
	}
	if got == nil {
		return mobiumerr.New(mobiumerr.NotConfirmed, "set the position to %s but the device reports no location at all", coord)
	}
	// Read back rather than trusting the command, and require the `mock` tag:
	// a position that matches because the device was already there is not
	// evidence of anything.
	if !got.Mock {
		return mobiumerr.New(mobiumerr.NotConfirmed, "set the position to %s and the device reports %.6f,%.6f "+
			"from a real provider, not a test one -- the injection did not take",
			coord, got.Lat, got.Lon)
	}
	const tolerance = 1e-4
	if diff(got.Lat, lat) > tolerance || diff(got.Lon, lon) > tolerance {
		return mobiumerr.New(mobiumerr.NotConfirmed, "asked for %s but the device reports %.6f,%.6f",
			coord, got.Lat, got.Lon)
	}
	return nil
}

// MockLocation reports the device's current position, and whether it is one
// somebody injected. Nil means the device holds no position at all.
func (a *ADB) MockLocation(ctx context.Context) (*Fix, error) {
	out, err := a.Shell(ctx, "dumpsys", "location")
	if err != nil {
		return nil, err
	}
	return parseFix(string(out))
}

// parseFix reads a position out of `dumpsys location`.
func parseFix(out string) (*Fix, error) {
	m := mockFixRe.FindStringSubmatch(out)
	if m == nil {
		return nil, nil
	}
	lat, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the device reported an unreadable latitude %q", m[1])
	}
	lon, err := strconv.ParseFloat(m[2], 64)
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the device reported an unreadable longitude %q", m[2])
	}
	return &Fix{
		Lat:     lat,
		Lon:     lon,
		Mock:    strings.Contains(m[3], "mock"),
		Mocking: mockProviderRe.MatchString(out),
	}, nil
}

// ClearMockLocation removes the test provider, so the device goes back to
// reporting its own position the next time anything asks for one.
//
// It does **not** clear the last known location, and cannot: Android keeps
// that cache, so `dumpsys` still reports the injected fix — tagged mock, from
// a provider that no longer exists — until something requests a fresh one.
// Verified by the provider label rather than by the position, because the
// position is exactly the thing that does not change.
func (a *ADB) ClearMockLocation(ctx context.Context) error {
	if _, _, err := a.ShellDiagnostics(ctx, "cmd", "location", "providers",
		"remove-test-provider", "gps"); err != nil {
		return err
	}
	out, err := a.Shell(ctx, "dumpsys", "location")
	if err != nil {
		return err
	}
	if mockProviderRe.MatchString(string(out)) {
		return mobiumerr.New(mobiumerr.NotConfirmed, "asked to remove the test provider and the device still "+
			"reports `gps provider [mock]`")
	}
	return nil
}

func diff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
