package device

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Booting and shutting down the virtual devices: an Android emulator by its
// AVD's name, an iOS simulator by its name or UDID. Each waits for the
// outcome, not for the command: an emulator's process is up long before
// Android has booted, and `simctl shutdown` returns while the simulator is
// still going down.

// emulatorBootTimeout bounds a cold boot of an emulator, inside the
// daemon's own limit on a call (240s), so a slow boot is answered as one
// rather than cut off.
var emulatorBootTimeout = 210 * time.Second

// goneTimeout bounds waiting for a device to be gone after it was told to go.
var goneTimeout = time.Minute

// emuKillWait is how long an emulator's console has to take a kill, and the
// emulator to leave adb's list after it, before its process is ended.
var emuKillWait = 10 * time.Second

// FindEmulator finds the SDK's emulator binary: under ANDROID_HOME or
// ANDROID_SDK_ROOT, the SDK's usual place, or on PATH.
func FindEmulator() (string, error) {
	for _, root := range sdkRoots() {
		p := filepath.Join(root, "emulator", "emulator"+exeSuffix())
		if isExec(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath("emulator"); err == nil {
		return p, nil
	}
	return "", mobiumerr.New(mobiumerr.ToolchainMissing, "cannot find the Android emulator: none under "+
		"ANDROID_HOME or ANDROID_SDK_ROOT, nor on PATH, as the daemon sees them — set ANDROID_HOME to the SDK and "+
		"restart the daemon (mobium daemon stop), or install it with `sdkmanager emulator` (mobium doctor checks the rest)")
}

// AVDs lists the Android virtual devices this machine has.
func AVDs(ctx context.Context) ([]string, error) {
	emu, err := FindEmulator()
	if err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, emu, "-list-avds").Output()
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.ToolchainMissing, "list the AVDs: %v", err)
	}
	var avds []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		// The emulator prints its own notes on stdout too, "INFO | ...".
		if l := strings.TrimSpace(sc.Text()); l != "" && !strings.Contains(l, "|") && !strings.Contains(l, " ") {
			avds = append(avds, l)
		}
	}
	return avds, nil
}

// RunningAVD is the serial of the emulator running avd, or "".
func RunningAVD(ctx context.Context, avd string) string {
	devs, err := Devices(ctx)
	if err != nil {
		return ""
	}
	for _, d := range devs {
		if d.Emulator && emulatorAVD(ctx, d.Serial) == avd {
			return d.Serial
		}
	}
	return ""
}

// emulatorAVD asks a running emulator which AVD it is. Its console answers
// "<name>" then "OK".
func emulatorAVD(ctx context.Context, serial string) string {
	adb, err := FindADB()
	if err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, adb, "-s", serial, "emu", "avd", "name").Output()
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" && l != "OK" {
			return l
		}
	}
	return ""
}

// Booted is a virtual device a boot answered with.
type Booted struct {
	Serial   string `json:"device"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	// Already says it was running before it was asked for.
	Already bool   `json:"already"`
	Took    string `json:"took,omitempty"`
}

// BootEmulator starts the AVD named avd and waits until Android has booted:
// adb sees it and sys.boot_completed is 1. It cold-boots: the quick-boot
// snapshot has brought back an app days older than the one installed
// (docs/SETUP.md). window shows it; without, it runs headless, as checks do.
// The emulator's output goes to log, whose last lines say why when it dies.
func BootEmulator(ctx context.Context, avd string, window bool, log string) (Booted, error) {
	start := time.Now()
	if s := RunningAVD(ctx, avd); s != "" {
		return Booted{Serial: s, Name: avd, Platform: "android", Already: true}, nil
	}
	avds, err := AVDs(ctx)
	if err != nil {
		return Booted{}, err
	}
	if !contains(avds, avd) {
		return Booted{}, mobiumerr.New(mobiumerr.NoDevice, "no AVD named %q — this machine has %s", avd, listOrNone(avds))
	}
	emu, _ := FindEmulator()
	before := map[string]bool{}
	if devs, err := Devices(ctx); err == nil {
		for _, d := range devs {
			before[d.Serial] = true
		}
	}
	args := []string{"-avd", avd, "-no-snapshot-load", "-no-boot-anim"}
	if !window {
		args = append(args, "-no-window", "-no-audio")
	}
	if err := os.MkdirAll(filepath.Dir(log), 0o755); err != nil {
		return Booted{}, err
	}
	lf, err := os.Create(log)
	if err != nil {
		return Booted{}, err
	}
	// The child has its own descriptor; this one is only the parent's.
	defer func() { _ = lf.Close() }()
	cmd := exec.Command(emu, args...)
	cmd.Stdout, cmd.Stderr = lf, lf
	Detach(cmd)
	if err := cmd.Start(); err != nil {
		return Booted{}, mobiumerr.New(mobiumerr.ToolchainMissing, "start the emulator: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	ctx, cancel := context.WithTimeout(ctx, emulatorBootTimeout)
	defer cancel()
	serial := ""
	for {
		select {
		case err := <-exited:
			return Booted{}, mobiumerr.New(mobiumerr.DeviceNotReady, "the emulator for %s exited before it booted (%v): %s",
				avd, err, logTail(log))
		case <-ctx.Done():
			return Booted{}, mobiumerr.New(mobiumerr.Timeout, "%s did not finish booting within %s — it is still "+
				"starting, or stuck; its output is in %s", avd, emulatorBootTimeout, log)
		case <-time.After(time.Second):
		}
		if serial == "" {
			devs, _ := Devices(ctx)
			for _, d := range devs {
				if d.Emulator && !before[d.Serial] && emulatorAVD(ctx, d.Serial) == avd {
					serial = d.Serial
				}
			}
			continue
		}
		// Booted is not yet usable: for a second or two after Android says
		// boot completed no window has focus — the launcher is still coming
		// up — and the first call made then failed reading a screen that was
		// changing. So it also waits for something on screen to have focus,
		// which came 1.2 to 1.9s later on the Pixel 7 AVD.
		if a, err := New(serial); err == nil {
			if out, err := a.Shell(ctx, "getprop", "sys.boot_completed"); err == nil && strings.TrimSpace(string(out)) == "1" &&
				windowFocused(ctx, a) {
				return Booted{Serial: serial, Name: avd, Platform: "android",
					Took: time.Since(start).Round(time.Second).String()}, nil
			}
		}
	}
}

// windowFocused reports whether a window has input focus: something is on
// screen. `dumpsys window` says mCurrentFocus=null until then.
func windowFocused(ctx context.Context, a *ADB) bool {
	out, err := a.Shell(ctx, "dumpsys", "window")
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "mCurrentFocus=") {
			return !strings.Contains(l, "=null")
		}
	}
	return false
}

// ShutdownEmulator stops a running emulator and waits until adb no longer
// lists it. A device that is not an emulator is refused: a phone is
// somebody's.
func ShutdownEmulator(ctx context.Context, serial string) error {
	devs, err := Devices(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, d := range devs {
		if d.Serial == serial {
			if !d.Emulator {
				return mobiumerr.New(mobiumerr.InvalidArgument, "%s is a real Android device, and mobium does not "+
					"power off a phone — it is somebody's", serial)
			}
			found = true
		}
	}
	if !found {
		return mobiumerr.New(mobiumerr.NoDevice, "no emulator %s is running (see `mobium devices`)", serial)
	}
	adb, err := FindADB()
	if err != nil {
		return err
	}
	listed := func(ctx context.Context) bool {
		devs, err := Devices(ctx)
		if err != nil {
			return true
		}
		for _, d := range devs {
			if d.Serial == serial {
				return true
			}
		}
		return false
	}
	// A frozen emulator does not answer its console, and `adb emu kill`
	// waited on it without end: `mobium shutdown` on one stopped with
	// SIGSTOP ran six minutes and left it running. So the console gets
	// emuKillWait, and an emulator still listed after it is ended through
	// its own process, named by its discovery file. CHALLENGES 285.
	kctx, cancel := context.WithTimeout(ctx, emuKillWait)
	out, killErr := exec.CommandContext(kctx, adb, "-s", serial, "emu", "kill").CombinedOutput()
	cancel()
	if killErr == nil {
		gctx, cancel := context.WithTimeout(ctx, emuKillWait)
		for listed(gctx) && gctx.Err() == nil {
			time.Sleep(250 * time.Millisecond)
		}
		cancel()
	}
	if listed(ctx) {
		if err := killEmulatorProcess(serial, emulatorDiscoveryDirs()); err != nil {
			return mobiumerr.New(mobiumerr.DeviceServer, "%s did not answer its console (adb emu kill: %v %s), "+
				"and %v", serial, killErr, strings.TrimSpace(string(out)), err)
		}
	}
	return waitGone(ctx, func(ctx context.Context) bool {
		devs, err := Devices(ctx)
		if err != nil {
			return false
		}
		for _, d := range devs {
			if d.Serial == serial {
				return false
			}
		}
		return true
	}, serial)
}

// BootSimulator boots the simulator named ref — its name or UDID — and waits
// for it to finish (Simctl.Boot). A name several simulators share is refused
// with their UDIDs, unless exactly one of them is booted.
func BootSimulator(ctx context.Context, ref string) (Booted, error) {
	start := time.Now()
	sims, err := Simulators(ctx)
	if err != nil {
		return Booted{}, err
	}
	var matches []Simulator
	for _, s := range sims {
		if s.UDID == ref || strings.EqualFold(s.Name, ref) {
			matches = append(matches, s)
		}
	}
	if len(matches) == 0 {
		return Booted{}, mobiumerr.New(mobiumerr.NoDevice, "no simulator named %q (see `mobium devices`, or "+
			"`xcrun simctl list devices` for the ones not booted)", ref)
	}
	if len(matches) > 1 {
		var booted []Simulator
		var udids []string
		for _, m := range matches {
			udids = append(udids, m.UDID)
			if m.Booted() {
				booted = append(booted, m)
			}
		}
		if len(booted) != 1 {
			return Booted{}, mobiumerr.New(mobiumerr.InvalidArgument, "%d simulators are named %q (%s) — give a UDID",
				len(matches), ref, strings.Join(udids, ", "))
		}
		matches = booted
	}
	sim := matches[0]
	if sim.Booted() {
		return Booted{Serial: sim.UDID, Name: sim.Name, Platform: "ios", Already: true}, nil
	}
	sc, err := NewSimctl(sim.UDID)
	if err != nil {
		return Booted{}, err
	}
	if err := sc.Boot(ctx); err != nil {
		return Booted{}, mobiumerr.New(mobiumerr.DeviceNotReady, "boot %s: %w", sim.Name, err)
	}
	return Booted{Serial: sim.UDID, Name: sim.Name, Platform: "ios", Took: time.Since(start).Round(time.Second).String()}, nil
}

// ShutdownSimulator shuts a simulator down and waits until simctl reports
// it shut down.
func ShutdownSimulator(ctx context.Context, udid string) error {
	sc, err := NewSimctl(udid)
	if err != nil {
		return err
	}
	if _, err := sc.Run(ctx, "shutdown", udid); err != nil && !strings.Contains(err.Error(), "current state: Shutdown") {
		return mobiumerr.New(mobiumerr.DeviceServer, "shut down %s: %w", udid, err)
	}
	return waitGone(ctx, func(ctx context.Context) bool {
		sims, err := Simulators(ctx)
		if err != nil {
			return false
		}
		for _, s := range sims {
			if s.UDID == udid {
				return !s.Booted() && s.State == "Shutdown"
			}
		}
		return true
	}, udid)
}

func waitGone(ctx context.Context, gone func(context.Context) bool, what string) error {
	ctx, cancel := context.WithTimeout(ctx, goneTimeout)
	defer cancel()
	for !gone(ctx) {
		select {
		case <-ctx.Done():
			return mobiumerr.New(mobiumerr.Timeout, "%s was told to shut down and is still there after %s", what, goneTimeout)
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil
}

// logTail is the last non-empty line of a log file, for an error.
func logTail(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "(no output)"
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return "(no output)"
}

func listOrNone(xs []string) string {
	if len(xs) == 0 {
		return "none"
	}
	return strings.Join(xs, ", ")
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// killEmulatorProcess ends the emulator adb calls serial through its process,
// the one its discovery file names — only once ps says that process is an
// emulator, since a file left by one that died can name a number reused.
func killEmulatorProcess(serial string, dirs []string) error {
	if runtime.GOOS == "windows" {
		return mobiumerr.New(mobiumerr.Unsupported, "ending an emulator by its process is not done on Windows")
	}
	pid := emulatorPID(serial, dirs)
	if pid == 0 {
		return mobiumerr.New(mobiumerr.DeviceServer, "its process is not named by any discovery file")
	}
	// The program's name, not its path: macOS's ps gives the whole path, and
	// a directory named for an emulator made a plain sleep look like one.
	comm, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	name := strings.ToLower(filepath.Base(strings.TrimSpace(string(comm))))
	if err != nil || !strings.Contains(name, "qemu") && !strings.Contains(name, "emulator") {
		return mobiumerr.New(mobiumerr.DeviceServer, "its discovery file names pid %d, which is not an emulator", pid)
	}
	p, err := os.FindProcess(pid)
	if err == nil {
		err = p.Kill()
	}
	if err != nil {
		return mobiumerr.New(mobiumerr.DeviceServer, "its process, pid %d, could not be ended: %v", pid, err)
	}
	return nil
}

// emulatorPID is the pid in the newest discovery file for serial, or 0.
func emulatorPID(serial string, dirs []string) int {
	console, ok := strings.CutPrefix(serial, "emulator-")
	if !ok {
		return 0
	}
	best, bestTime := 0, time.Time{}
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "pid_*.ini"))
		for _, f := range files {
			if readIni(f)["port.serial"] != console {
				continue
			}
			pid, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "pid_"), ".ini"))
			info, serr := os.Stat(f)
			if err != nil || serr != nil || !info.ModTime().After(bestTime) {
				continue
			}
			best, bestTime = pid, info.ModTime()
		}
	}
	return best
}
