package formflux

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// iOS screens are devices, not settings.
//
// Nothing resizes a booted simulator, so a profile here means *a simulator of
// that device type* — found if one exists, created if not. That makes iOS
// coverage cost tens of seconds a profile against Android's half second, and
// it is why formflux does not pretend the two platforms are one mechanism.
//
// Two rules this file will not break:
//
//   - **Never delete a simulator we did not create.** Somebody's simulator has
//     their apps, their logins and their state in it. Release shuts down what
//     it booted and deletes only what it made.
//   - **Never reuse a booted simulator silently.** If one of the right type is
//     already running it is used as-is and left running afterwards, because
//     shutting down a device somebody else is driving is the same mistake as
//     killing an emulator out from under a live session.

// simDevice is the part of `simctl list devices --json` this package needs.
// internal/device parses the same output for a different purpose and does not
// read the device type; duplicating four fields is cheaper than widening a
// type the driver layer depends on.
type simDevice struct {
	UDID       string `json:"udid"`
	Name       string `json:"name"`
	State      string `json:"state"`
	DeviceType string `json:"deviceTypeIdentifier"`
	Available  bool   `json:"isAvailable"`

	runtime string // the key it was listed under
}

func (d simDevice) booted() bool { return d.State == "Booted" }

// Simulator is a simulator formflux is using for a profile.
type Simulator struct {
	UDID    string
	Name    string
	Runtime string

	// Created is true when formflux made this simulator and may delete it.
	Created bool
	// WasBooted is true when it was already running before formflux arrived,
	// in which case it is left running.
	WasBooted bool
}

func simctl(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "xcrun", append([]string{"simctl"}, args...)...).Output()
	if err != nil {
		detail := ""
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			detail = ": " + strings.TrimSpace(lastLine(string(ee.Stderr)))
		}
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "simctl %s%s", strings.Join(args, " "), detail)
	}
	return out, nil
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// devicesOfType lists simulators of one device type, newest runtime first.
func devicesOfType(ctx context.Context, deviceType string) ([]simDevice, error) {
	out, err := simctl(ctx, "list", "devices", "available", "--json")
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Devices map[string][]simDevice `json:"devices"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("could not read the simulator list: %w", err)
	}
	var found []simDevice
	for runtime, list := range parsed.Devices {
		for _, d := range list {
			if d.Available && d.DeviceType == deviceType {
				d.runtime = runtime
				found = append(found, d)
			}
		}
	}
	// Newest runtime first, and a booted one ahead of a shut-down one so an
	// already-running simulator is preferred over booting another.
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].booted() != found[j].booted() {
			return found[i].booted()
		}
		return found[i].runtime > found[j].runtime
	})
	return found, nil
}

// Ensure returns a booted simulator for an iOS profile, creating one only if
// no simulator of that device type exists.
func Ensure(ctx context.Context, p Profile) (*Simulator, error) {
	if p.Platform != IOS {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "profile %q is an Android profile — apply it to a running device with Apply instead", p.Name)
	}
	if p.DeviceType == "" {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "profile %q has no simctl device type, so nothing can boot it", p.Name)
	}

	existing, err := devicesOfType(ctx, p.DeviceType)
	if err != nil {
		return nil, err
	}

	if len(existing) > 0 {
		d := existing[0]
		sim := &Simulator{UDID: d.UDID, Name: d.Name, Runtime: shortRuntime(d.runtime)}
		if d.booted() {
			sim.WasBooted = true
			return sim, nil
		}
		if err := bootAndWait(ctx, d.UDID); err != nil {
			return nil, err
		}
		return sim, nil
	}

	// None exists. Create one on the newest runtime that offers this type.
	runtime, err := newestRuntimeFor(ctx, p.DeviceType)
	if err != nil {
		return nil, err
	}
	name := "formflux-" + p.Name
	out, err := simctl(ctx, "create", name, p.DeviceType, runtime)
	if err != nil {
		return nil, err
	}
	udid := strings.TrimSpace(string(out))
	if udid == "" {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "simctl create returned no udid for %s", p.Name)
	}
	if err := bootAndWait(ctx, udid); err != nil {
		// Made it, could not boot it: take it away again rather than leaving
		// a broken simulator with our name on it.
		_, _ = simctl(context.Background(), "delete", udid)
		return nil, err
	}
	return &Simulator{UDID: udid, Name: name, Runtime: shortRuntime(runtime), Created: true}, nil
}

// newestRuntimeFor picks a runtime that actually supports this device type.
// Asking for one that does not is how `simctl create` fails with a message
// about an "invalid device type", which names the wrong half of the problem.
func newestRuntimeFor(ctx context.Context, deviceType string) (string, error) {
	out, err := simctl(ctx, "list", "runtimes", "--json")
	if err != nil {
		return "", err
	}
	var parsed struct {
		Runtimes []struct {
			Identifier   string `json:"identifier"`
			Version      string `json:"version"`
			IsAvailable  bool   `json:"isAvailable"`
			Platform     string `json:"platform"`
			SupportedDTs []struct {
				Identifier string `json:"identifier"`
			} `json:"supportedDeviceTypes"`
		} `json:"runtimes"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return "", fmt.Errorf("could not read the runtime list: %w", err)
	}
	best, bestVer := "", ""
	for _, r := range parsed.Runtimes {
		if !r.IsAvailable {
			continue
		}
		for _, dt := range r.SupportedDTs {
			if dt.Identifier == deviceType && r.Version > bestVer {
				best, bestVer = r.Identifier, r.Version
			}
		}
	}
	if best == "" {
		return "", mobiumerr.New(mobiumerr.DeviceServer,
			"no installed iOS runtime supports %s — install one with `xcodebuild -downloadPlatform iOS`", deviceType)
	}
	return best, nil
}

// bootAndWait boots a simulator and waits for it to be usable.
//
// `simctl boot` returns when the boot is requested, not when it is finished —
// the same lie `am start` and `simctl launch` tell, and the same rule applies:
// wait for the outcome. `bootstatus -b` is simctl's own answer to it.
func bootAndWait(ctx context.Context, udid string) error {
	if _, err := simctl(ctx, "boot", udid); err != nil {
		// Already booted is success, not failure.
		if !strings.Contains(err.Error(), "Booted") {
			return err
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if _, err := simctl(waitCtx, "bootstatus", udid, "-b"); err != nil {
		return fmt.Errorf("simulator %s did not finish booting: %w", udid, err)
	}
	return nil
}

// Release undoes what Ensure did, and nothing more.
//
// A simulator that was already running is left running. One that formflux
// booted is shut down. One that formflux created is deleted — and only that
// one, because somebody else's simulator holds their apps and their state.
func Release(ctx context.Context, sim *Simulator) error {
	if sim == nil || sim.WasBooted {
		return nil
	}
	if _, err := simctl(ctx, "shutdown", sim.UDID); err != nil &&
		!strings.Contains(err.Error(), "Shutdown") {
		return err
	}
	if sim.Created {
		if _, err := simctl(ctx, "delete", sim.UDID); err != nil {
			return fmt.Errorf("could not delete the simulator formflux created (%s): %w", sim.UDID, err)
		}
	}
	return nil
}

// shortRuntime turns com.apple.CoreSimulator.SimRuntime.iOS-26-5 into
// "iOS 26.5": the first dash separates the platform from its version, the rest
// are decimal points. Replacing every dash gives "iOS.26.5", which is what
// this did until it was printed and read.
func shortRuntime(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		id = id[i+1:]
	}
	if i := strings.Index(id, "-"); i >= 0 {
		return id[:i] + " " + strings.ReplaceAll(id[i+1:], "-", ".")
	}
	return id
}
