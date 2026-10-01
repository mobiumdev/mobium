package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/paths"
)

// app_boot and app_shutdown: starting and stopping the virtual devices, an
// Android emulator by its AVD's name and an iOS simulator by its name or
// UDID. An agent driving Mobium over MCP has no shell, and without these
// could not start a device at all. A real phone is refused by both: it is
// somebody's.

// BootView is app_boot's answer.
type BootView = device.Booted

// bootAVDs and bootSimulators are what a name could be, replaceable in tests.
var (
	bootAVDs      = device.AVDs
	bootSimulator = func(ctx context.Context, name string) bool {
		if runtime.GOOS != "darwin" {
			return false
		}
		sims, _ := device.Simulators(ctx)
		for _, s := range sims {
			if s.UDID == name || strings.EqualFold(s.Name, name) {
				return true
			}
		}
		return false
	}
)

func (h *Handlers) boot(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	name := strings.TrimSpace(stringArg(args, "name"))
	if name == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_boot needs a name: an AVD's (Android) or a "+
			"simulator's name or UDID (iOS)")
	}
	avds, avdErr := bootAVDs(ctx)
	isAVD := avdErr == nil && containsString(avds, name)
	isSim := bootSimulator(ctx, name)
	var b device.Booted
	var err error
	switch {
	case isAVD && isSim:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%q names both an AVD and a simulator — give the "+
			"simulator's UDID for iOS", name)
	case isAVD:
		log := filepath.Join(paths.Root(), "emulator", name+".log")
		b, err = device.BootEmulator(ctx, name, boolArg(args, "window"), log)
	case isSim:
		b, err = device.BootSimulator(ctx, name)
	case avdErr != nil:
		// The emulator could not be found or asked, and the name is not a
		// simulator: say that, not "no AVDs" — a daemon started without
		// ANDROID_HOME said this machine had none, while it had several.
		return nil, avdErr
	default:
		have := "no AVDs"
		if avdErr == nil && len(avds) > 0 {
			have = "AVDs " + strings.Join(avds, ", ")
		}
		return nil, mobiumerr.New(mobiumerr.NoDevice, "nothing to boot is named %q: this machine has %s; "+
			"simulators by name or UDID are in `xcrun simctl list devices`", name, have)
	}
	if err != nil {
		return nil, err
	}
	msg := fmt.Sprintf("booted %s as %s in %s", b.Name, b.Serial, b.Took)
	if b.Already {
		msg = fmt.Sprintf("%s was already running, as %s", b.Name, b.Serial)
	}
	return Result(msg, BootView(b)), nil
}

// ShutdownView is app_shutdown's answer.
type ShutdownView struct {
	Device string `json:"device"`
	// SessionEnded says this daemon had a session on it, ended first.
	SessionEnded bool `json:"session_ended"`
}

// shutdown ends this daemon's session on the device, then the device: the
// order docs/SHUTDOWN.md gives, since a session left driving a device that
// vanishes leaves its server running against nothing, and the next emulator
// reuses the serial.
//
// The device is named by `name`, as app_boot's is, never by `device`: the
// pipe a client speaks through sets `device` to the device it was started
// for, so a client on one emulator asking to shut down another would have
// shut down its own.
func (h *Handlers) shutdown(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	name := strings.TrimSpace(stringArg(args, "name"))
	if name == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_shutdown needs a name: an emulator's serial or "+
			"AVD name, or a simulator's UDID or name (see app_devices)")
	}
	serial, isSim, err := shutdownTarget(ctx, name)
	if err != nil {
		return nil, err
	}
	view := ShutdownView{Device: serial}
	for key, s := range h.sessions {
		if key == serial || s.dev.Serial == serial {
			s.close()
			delete(h.sessions, key)
			view.SessionEnded = true
		}
	}
	delete(h.refs, serial)
	if isSim {
		err = device.ShutdownSimulator(ctx, serial)
	} else {
		err = device.ShutdownEmulator(ctx, serial)
	}
	if err != nil {
		return nil, err
	}
	return Result(fmt.Sprintf("shut down %s", serial), view), nil
}

// shutdownTarget resolves a name to a running emulator's serial or a booted
// simulator's UDID. A phone is refused: it is somebody's.
var shutdownTarget = func(ctx context.Context, name string) (serial string, isSim bool, err error) {
	if kind := iosKindOf(ctx, name); strings.HasPrefix(kind, "an iPhone") {
		return "", false, mobiumerr.New(mobiumerr.InvalidArgument, "%s is %s, and mobium does not power off a "+
			"phone — it is somebody's", name, kind)
	}
	if devs, derr := device.Devices(ctx); derr == nil {
		for _, d := range devs {
			if d.Serial == name {
				if !d.Emulator {
					return "", false, mobiumerr.New(mobiumerr.InvalidArgument, "%s is a real Android device, and "+
						"mobium does not power off a phone — it is somebody's", name)
				}
				return d.Serial, false, nil
			}
		}
		if s := device.RunningAVD(ctx, name); s != "" {
			return s, false, nil
		}
	}
	if runtime.GOOS == "darwin" {
		sims, _ := device.Simulators(ctx)
		for _, s := range sims {
			if (s.UDID == name || strings.EqualFold(s.Name, name)) && s.Booted() {
				return s.UDID, true, nil
			}
		}
	}
	return "", false, mobiumerr.New(mobiumerr.NoDevice, "nothing running is named %q — give a running "+
		"emulator's serial or AVD name, or a booted simulator's UDID or name (see app_devices)", name)
}

func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
