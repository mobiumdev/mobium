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
			return parseHitLine(string(rest))
		}
	}
	if err != nil {
		return Hit{}, mobiumerr.New(mobiumerr.DeviceServer, "run lldb against %s: %v: %s", bundleID, err, lldbError(out))
	}
	return Hit{}, mobiumerr.New(mobiumerr.DeviceServer, "the hit probe did not answer: %s", lldbError(out))
}
