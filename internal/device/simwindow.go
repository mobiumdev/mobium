package device

import (
	"context"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// ShowSimulator opens the app that shows a simulator's screen, on the
// simulator udid, and says what a person must still do, or "".
//
// Xcode 27.1 has no Simulator.app: DeviceHub, inside Xcode.app, shows
// simulators, and ignores Simulator.app's -CurrentDeviceUDID. It opens on
// the device it last showed, which it keeps as lastSelectedDeviceIdentifier,
// the UDID as a JSON string; written before it starts, it opened on that
// simulator (CHALLENGES 405). A DeviceHub already running reads it no more,
// and is not quit: it is brought forward, and the note names the device to
// pick in its sidebar.
func ShowSimulator(ctx context.Context, udid, name string) (string, error) {
	dev, err := exec.CommandContext(ctx, "xcode-select", "-p").Output()
	if err != nil {
		return "", mobiumerr.New(mobiumerr.ToolchainMissing, "xcode-select -p: %v", err).
			WithRemedy("install Xcode and run `sudo xcode-select -s /Applications/Xcode.app`")
	}
	developer := strings.TrimSpace(string(dev))
	if sim := filepath.Join(developer, "Applications", "Simulator.app"); isDir(sim) {
		return "", openApp(ctx, sim, "--args", "-CurrentDeviceUDID", udid)
	}
	hub := filepath.Join(filepath.Dir(developer), "Applications", "DeviceHub.app")
	if !isDir(hub) {
		return "", mobiumerr.New(mobiumerr.ToolchainMissing, "neither Simulator.app nor DeviceHub.app is in %s's Xcode",
			developer)
	}
	if exec.CommandContext(ctx, "pgrep", "-x", "DeviceHub").Run() == nil {
		if err := openApp(ctx, hub); err != nil {
			return "", err
		}
		return "DeviceHub was already open, so it shows the device it showed: select " + name +
			" in its sidebar", nil
	}
	id := hex.EncodeToString([]byte(`"` + udid + `"`))
	if out, err := exec.CommandContext(ctx, "defaults", "write", "com.apple.dt.Devices",
		"lastSelectedDeviceIdentifier", "-data", id).CombinedOutput(); err != nil {
		return "", mobiumerr.New(mobiumerr.DeviceServer, "set DeviceHub's device: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return "", openApp(ctx, hub)
}

func openApp(ctx context.Context, app string, args ...string) error {
	out, err := exec.CommandContext(ctx, "open", append([]string{"-a", app}, args...)...).CombinedOutput()
	if err != nil {
		return mobiumerr.New(mobiumerr.DeviceServer, "open %s: %v: %s", filepath.Base(app), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
