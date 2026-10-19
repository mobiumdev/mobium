package device

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// The hit test below accessibility, on a real iPhone. See hitprobe/phone.py
// for how lldb is driven there and why the probe is an expression rather
// than a library.
//
// Measured on an iPhone 15 Plus, iOS 26.6.2, with MobiumApp built for it
// (development-signed, so get-task-allow): the overlay hidden from
// accessibility on the Obstruction Demo answered covered, hidden, by
// RCTParagraphComponentView "hidden overlay" — the simulator's answer — in
// about nine seconds, five of them the attach. The app is stopped for that
// long, and goes on as before.

//go:embed hitprobe/phone.py
var phoneHitScript []byte

// phoneHitTimeout bounds one attach, expression and detach on a phone.
const phoneHitTimeout = 60 * time.Second

// phoneNoStop is the probe's answer when the attach never stopped the app,
// as hitprobe/phone.py words it.
const phoneNoStop = "the app did not stop for the debugger"

// hasDeviceSupport reports whether Xcode keeps the shared cache of iOS
// version on this Mac, under ~/Library/Developer/Xcode/iOS DeviceSupport,
// in a folder named for a model, the version and its build: "iPhone15,5
// 26.6.2 (23G90)".
func hasDeviceSupport(home, version string) bool {
	if version == "" {
		return true
	}
	ents, err := os.ReadDir(filepath.Join(home, "Library", "Developer", "Xcode", "iOS DeviceSupport"))
	if err != nil {
		return false
	}
	for _, e := range ents {
		if e.IsDir() && strings.Contains(e.Name(), " "+version+" (") {
			return true
		}
	}
	return false
}

// noDeviceSupport is the refusal for an attach that did not stop because
// Xcode has no copy of the phone's shared cache. lldb then reads every
// system library out of the app's memory over the cable before the attach
// completes: on the iPhone 15 Plus on iOS 27.0.1, with Xcode 27.1 and only
// iOS 26.6.2's cache on the Mac, the stop came after 215 seconds, and the
// expression and the detach then worked (CHALLENGES 343). Xcode copies the
// cache when it prepares the phone for a new iOS.
func noDeviceSupport(p Phone) *mobiumerr.Error {
	return mobiumerr.New(mobiumerr.DeviceNotReady, "the hit test on %s could not stop the app: Xcode keeps no "+
		"copy of iOS %s's shared cache on this Mac, so lldb reads the system libraries out of the app over "+
		"the cable first, measured at three and a half minutes, past the %s a hit test is given", p.Label(),
		p.OSVersion, phoneHitTimeout).
		WithRemedy("open Xcode's Window > Devices and Simulators with the phone connected and unlocked, select " +
			"it, and wait while Xcode copies its shared cache symbols; then run the hit test again")
}

// phoneHitModule writes the lldb module once per source, under a name of its
// own, and returns its path.
func phoneHitModule() (string, error) {
	sum := sha256.Sum256(phoneHitScript)
	name := "mobiumhit_" + hex.EncodeToString(sum[:6])
	dir := filepath.Join(cacheRoot(), "hitprobe")
	path := filepath.Join(dir, name+".py")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, phoneHitScript, 0o644)
}

// AppPID is the process id of an app running on the phone, found by where it
// is installed: the app list gives each app's bundle path, and the process
// list each process's executable.
func (d *Devicectl) AppPID(ctx context.Context, bundleID string) (int, error) {
	raw, err := d.run(ctx, "device", "info", "apps", "--device", d.Phone.UDID, "--include-all-apps")
	if err != nil {
		return 0, err
	}
	var apps struct {
		Apps []struct {
			BundleIdentifier string `json:"bundleIdentifier"`
			URL              string `json:"url"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(raw, &apps); err != nil {
		return 0, fmt.Errorf("parse devicectl app list: %w", err)
	}
	var bundle string
	for _, a := range apps.Apps {
		if a.BundleIdentifier == bundleID {
			bundle = strings.TrimSuffix(a.URL, "/") + "/"
		}
	}
	if bundle == "/" || bundle == "" {
		return 0, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed on %s", bundleID, d.Phone.Label())
	}
	raw, err = d.run(ctx, "device", "info", "processes", "--device", d.Phone.UDID)
	if err != nil {
		return 0, err
	}
	return pidUnder(raw, bundle, bundleID)
}

func pidUnder(raw json.RawMessage, bundle, bundleID string) (int, error) {
	var procs struct {
		RunningProcesses []struct {
			PID        int    `json:"processIdentifier"`
			Executable string `json:"executable"`
		} `json:"runningProcesses"`
	}
	if err := json.Unmarshal(raw, &procs); err != nil {
		return 0, fmt.Errorf("parse devicectl process list: %w", err)
	}
	for _, p := range procs.RunningProcesses {
		if strings.HasPrefix(p.Executable, bundle) {
			return p.PID, nil
		}
	}
	return 0, mobiumerr.New(mobiumerr.DeviceNotReady, "%s is not running", bundleID)
}

// HitTest asks UIKit, inside the app on the phone, which view a touch at
// (x, y) would go to, and whether that is the element accessibility reports
// at frame with identifier id. All in points.
func (d *Devicectl) HitTest(ctx context.Context, bundleID string, x, y float64, frame [4]float64, id string) (Hit, error) {
	module, err := phoneHitModule()
	if err != nil {
		return Hit{}, err
	}
	pid, err := d.AppPID(ctx, bundleID)
	if err != nil {
		return Hit{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, phoneHitTimeout)
	defer cancel()
	call := fmt.Sprintf("mobium_hit %s %d %g %g %g %g %g %g %s", d.Phone.UDID, pid, x, y,
		frame[0], frame[1], frame[2], frame[3], strconv.Quote(id))
	out, err := exec.CommandContext(ctx, d.Path, "lldb", "--batch", "--no-lldbinit",
		"-o", "command script import "+strconv.Quote(module), "-o", call).CombinedOutput()
	if ctx.Err() != nil {
		return Hit{}, mobiumerr.New(mobiumerr.Timeout, "the hit test on %s did not finish within %s", d.Phone.Label(), phoneHitTimeout)
	}
	for _, line := range bytes.Split(out, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("MOBIUM_HIT\t")); ok {
			hit, err := parseHitLine(string(rest))
			if err == nil && hit.Verdict == "unknown" && hit.Reason == phoneNoStop {
				if home, herr := os.UserHomeDir(); herr == nil && !hasDeviceSupport(home, d.Phone.OSVersion) {
					return Hit{}, noDeviceSupport(d.Phone)
				}
			}
			return hit, err
		}
	}
	if err != nil {
		return Hit{}, mobiumerr.New(mobiumerr.DeviceServer, "run lldb against %s: %v: %s", bundleID, err, lldbError(out))
	}
	return Hit{}, mobiumerr.New(mobiumerr.DeviceServer, "the hit probe did not answer: %s", lldbError(out))
}
