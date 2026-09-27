package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/paths"
)

// doctor checks the environment and says what is wrong with it.
//
// Every trap it looks for cost real time to diagnose while building this,
// and every one of them reports something that names the wrong cause. The
// emulator dies with "Broken AVD system path" when the problem is a missing
// platform-tools directory, while `adb` sits on PATH working perfectly. That
// is the kind of thing this exists to shortcut.
//
// It deliberately does not need a device. Its whole job is to be runnable
// when nothing works yet.
func (h *Handlers) doctor(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	view := DoctorView{Checks: []DoctorCheck{}}
	add := func(c DoctorCheck) { view.Checks = append(view.Checks, c) }

	add(checkADB(ctx))
	add(checkSDKRoot())
	add(checkAndroidDevices(ctx))
	if runtime.GOOS == "darwin" {
		add(checkXcode(ctx))
		add(checkSimulators(ctx))
	}
	add(checkSocketPath())
	add(checkAgentCache())
	add(checkExternalDrivers())

	var problems int
	var lines []string
	for _, c := range view.Checks {
		mark := "ok  "
		switch c.Status {
		case statusProblem:
			mark = "FAIL"
			problems++
		case statusNote:
			mark = "note"
		}
		line := fmt.Sprintf("%s  %-22s %s", mark, c.Name, c.Detail)
		if c.Fix != "" {
			line += "\n          → " + c.Fix
		}
		lines = append(lines, line)
	}
	view.Problems = problems

	summary := "everything mobium needs is here"
	if problems == 1 {
		summary = "1 problem"
	} else if problems > 1 {
		summary = fmt.Sprintf("%d problems", problems)
	}
	return Result(strings.Join(lines, "\n")+"\n\n"+summary, view), nil
}

const (
	statusOK      = "ok"
	statusProblem = "problem"
	statusNote    = "note"
)

func ok(name, detail string) DoctorCheck {
	return DoctorCheck{Name: name, Status: statusOK, Detail: detail}
}

func note(name, detail string) DoctorCheck {
	return DoctorCheck{Name: name, Status: statusNote, Detail: detail}
}

func bad(name, detail, fix string) DoctorCheck {
	return DoctorCheck{Name: name, Status: statusProblem, Detail: detail, Fix: fix}
}

func checkADB(ctx context.Context) DoctorCheck {
	path, err := device.FindADB()
	if err != nil {
		return bad("adb", err.Error(),
			"Install Android platform-tools and put adb on PATH, or set MOBIUM_ADB_PATH.")
	}
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return bad("adb", path+" will not run: "+err.Error(),
			"Check the file is the real adb and is executable.")
	}
	return ok("adb", firstLineOf(string(out))+" at "+path)
}

// checkSDKRoot is the trap that cost the most time here. The emulator
// validates its SDK root by looking for a platform-tools directory inside it,
// and dies with "Broken AVD system path" when it is missing — while adb is on
// PATH and working, which sends you looking somewhere else entirely.
func checkSDKRoot() DoctorCheck {
	root := device.EnvPath("ANDROID_SDK_ROOT")
	if root == "" {
		root = device.EnvPath("ANDROID_HOME")
	}
	if root == "" {
		return note("android sdk root",
			"ANDROID_SDK_ROOT is not set — only needed to start emulators, not to drive them")
	}
	if _, err := os.Stat(filepath.Join(root, "platform-tools")); err != nil {
		return bad("android sdk root", root+" has no platform-tools directory",
			"Run `sdkmanager platform-tools` so it installs *into the SDK root*. Having adb on "+
				"PATH is not enough: the emulator checks for this directory and fails with "+
				"\"Broken AVD system path\", which names the wrong cause.")
	}
	return ok("android sdk root", root)
}

func checkAndroidDevices(ctx context.Context) DoctorCheck {
	if _, err := device.FindADB(); err != nil {
		// The adb check above already said this at length. Repeating it here
		// makes the report longer without making it clearer.
		return note("android devices", "skipped — no adb")
	}
	devices, err := device.Devices(ctx)
	if err != nil {
		return note("android devices", err.Error())
	}
	if len(devices) == 0 {
		// A phone on USB that adb cannot see is a different problem from no
		// phone at all, and the difference is entirely in the phone's
		// settings. Saying "none running" there sends someone to check the
		// cable, which is the one thing already proven fine.
		if usb := usbAndroidDevices(); len(usb) > 0 {
			return bad("android devices",
				strings.Join(usb, ", ")+" is plugged in but adb cannot see it",
				"On the phone: Settings → About phone → tap Build number seven times, then "+
					"Settings → System → Developer options → enable USB debugging. Accept the "+
					"\"Allow USB debugging?\" prompt when it appears. If nothing appears, change "+
					"the USB mode from charging to file transfer.")
		}
		return note("android devices", "none running — start an emulator or plug in a phone")
	}
	var names []string
	var stuck []string
	for _, d := range devices {
		names = append(names, d.Serial+" ("+d.State+")")
		// "device" is the only state that can be driven. Listing an
		// unauthorized phone as ok is worse than not listing it: the whole
		// point of asking is to find out why nothing works.
		if d.State != "device" {
			stuck = append(stuck, d.Serial+" is "+d.State)
		}
	}
	if len(stuck) > 0 {
		return bad("android devices", strings.Join(names, ", "),
			strings.Join(stuck, "; ")+". \"unauthorized\" means the \"Allow USB "+
				"debugging?\" prompt is waiting on the phone — accept it, and tick "+
				"\"Always allow from this computer\" so an adb restart does not ask again. "+
				"\"offline\" usually clears with `adb kill-server && adb start-server`.")
	}
	return ok("android devices", strings.Join(names, ", "))
}

// usbAndroidDevices lists Android hardware the OS can see on USB, whatever
// adb thinks. Best effort and macOS-only: it exists to tell "your phone is
// not plugged in" apart from "your phone is plugged in and not listening",
// so returning nothing simply means the better message is unavailable.
func usbAndroidDevices() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	out, err := exec.Command("ioreg", "-p", "IOUSB", "-w0", "-l").Output()
	if err != nil {
		return nil
	}
	var names []string
	var vendor string
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.Contains(line, `"USB Vendor Name"`):
			vendor = quotedValue(line)
		case strings.Contains(line, `"USB Product Name"`):
			product := quotedValue(line)
			if isAndroidVendor(vendor) && product != "" {
				names = append(names, product)
			}
			vendor = ""
		}
	}
	return names
}

// isAndroidVendor covers the makers whose phones someone is plausibly
// automating. A miss only costs the better error message.
func isAndroidVendor(v string) bool {
	v = strings.ToLower(v)
	for _, known := range []string{"google", "samsung", "oneplus", "xiaomi", "motorola",
		"sony", "lg electronics", "huawei", "oppo", "vivo", "nothing", "fairphone", "asus"} {
		if strings.Contains(v, known) {
			return true
		}
	}
	return false
}

// quotedValue reads the right-hand side of an ioreg line like
//
//	"USB Product Name" = "Pixel 8 Pro"
func quotedValue(line string) string {
	i := strings.Index(line, "=")
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(line[i+1:])
	return strings.Trim(rest, `"`)
}

func checkXcode(ctx context.Context) DoctorCheck {
	path, err := device.FindSimctl()
	if err != nil {
		return note("xcode", err.Error()+" — only needed for iOS simulators")
	}
	return ok("xcode", path)
}

func checkSimulators(ctx context.Context) DoctorCheck {
	if _, err := device.FindSimctl(); err != nil {
		return note("ios simulators", "skipped — no Xcode")
	}
	sims, err := device.Simulators(ctx)
	if err != nil {
		return note("ios simulators", err.Error())
	}
	var booted []string
	for _, s := range sims {
		if s.Booted() {
			booted = append(booted, s.Name)
		}
	}
	if len(booted) == 0 {
		return note("ios simulators",
			fmt.Sprintf("%d available, none booted — `xcrun simctl boot <udid>`", len(sims)))
	}
	return ok("ios simulators", "booted: "+strings.Join(booted, ", "))
}

// checkSocketPath guards the 104-byte limit the OS puts on a Unix socket
// path, which is short enough to hit with an ordinary MOBIUM_HOME and gives a
// baffling error when it does.
func checkSocketPath() DoctorCheck {
	p, err := paths.SocketPath()
	if err != nil {
		return bad("daemon socket", err.Error(),
			"Shorten MOBIUM_HOME or the session name: the OS caps a Unix socket path at ~104 bytes.")
	}
	if runtime.GOOS == "windows" {
		// A named pipe has no sun_path, so there is no limit to report
		// against — "of ~104" would describe a constraint that is not there.
		return ok("daemon socket", "named pipe "+p)
	}
	return ok("daemon socket", fmt.Sprintf("%s (%d bytes of ~104)", p, len(p)))
}

// checkAgentCache reports what has already been downloaded, so a slow first
// run is explicable before it happens rather than after.
func checkAgentCache() DoctorCheck {
	home := paths.Root()
	var have []string
	for name, dir := range map[string]string{
		"uiautomator2":   "uiautomator2",
		"webdriveragent": "webdriveragent",
	} {
		if entries, err := os.ReadDir(filepath.Join(home, dir)); err == nil && len(entries) > 0 {
			have = append(have, name)
		}
	}
	if len(have) == 0 {
		return note("device agents",
			"nothing cached yet — the first run downloads a server for the platform, which takes a moment")
	}
	return ok("device agents", "cached: "+strings.Join(have, ", "))
}

// checkExternalDrivers reports third-party drivers found on PATH.
//
// Discovery is by convention — an executable named mobium-driver-<name> — so
// without this the only way to answer "is my driver installed" is to run it
// and read the failure. It is a note rather than a check that can fail: having
// no third-party drivers is the normal case.
func checkExternalDrivers() DoctorCheck {
	var found []string
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			// A directory called mobium-driver-x is not a driver.
			info, err := e.Info()
			if err != nil || info.IsDir() {
				continue
			}
			name, isDriver := driverName(e.Name(), info.Mode(), runtime.GOOS, os.Getenv("PATHEXT"))
			if !isDriver || seen[name] {
				continue
			}
			seen[name] = true
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return note("third-party drivers",
			"none on PATH — a driver is an executable named mobium-driver-<name>, "+
				"used with --driver <name>")
	}
	sort.Strings(found)
	return ok("third-party drivers", "on PATH: "+strings.Join(found, ", "))
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// DoctorView is the result of app_doctor.
type DoctorView struct {
	Checks   []DoctorCheck `json:"checks"`
	Problems int           `json:"problems"`
}

// DoctorCheck is one thing that was looked at. Status is "ok", "note" or
// "problem"; a note is something absent that is only needed for part of the
// job, such as Xcode on a machine doing Android work.
type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// driverName is the backend name a file on PATH provides, if it is a driver.
//
// What makes a file executable differs by platform, and so does its name. On
// Unix it is the mode bits and the file is called exactly mobium-driver-x. On
// Windows there are no execute bits — Go reports every file as 0666 or 0444 —
// and executability is the extension, from PATHEXT: mobium-driver-x.exe or
// .cmd is the driver `--driver x` finds, because that is what exec.LookPath
// does in FindDriver. Checking mode bits there reported "none on PATH" for a
// driver `--driver` would have run.
func driverName(file string, mode os.FileMode, goos, pathext string) (string, bool) {
	if !strings.HasPrefix(file, "mobium-driver-") {
		return "", false
	}
	name := strings.TrimPrefix(file, "mobium-driver-")
	if goos != "windows" {
		if mode.Perm()&0o111 == 0 {
			return "", false
		}
		return name, true
	}
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD" // exec.LookPath's own default
	}
	ext := filepath.Ext(name)
	for _, e := range strings.Split(pathext, ";") {
		if ext != "" && strings.EqualFold(ext, e) {
			return strings.TrimSuffix(name, ext), true
		}
	}
	return "", false
}
