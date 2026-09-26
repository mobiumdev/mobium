package device

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrNoXcode is returned when the full Xcode toolchain is missing.
//
// The Command Line Tools alone are not enough: simctl and the simulator
// runtimes ship with Xcode itself, and `xcrun simctl` under CLT-only fails
// with "unable to find utility", which reads like a PATH problem rather than
// a missing install.
var ErrNoXcode = mobiumerr.New(mobiumerr.ToolchainMissing,
	"Xcode is required for iOS simulators and was not found — the Command Line Tools "+
		"alone do not include simctl. Install Xcode (`xcodes install --latest`, or the "+
		"App Store), then run `sudo xcode-select -s /Applications/Xcode.app`")

// ErrNoSimulator is returned when Xcode is present but nothing is booted.
var ErrNoSimulator = mobiumerr.New(mobiumerr.NoDevice,
	"no iOS simulator is booted — start one with `xcrun simctl boot <udid>` "+
		"(see `mobium devices`) or from Xcode's Devices window")

// Simctl is a located xcrun, optionally pinned to one simulator.
type Simctl struct {
	Path string
	UDID string
}

// FindSimctl checks that a usable simctl exists, returning the xcrun path.
func FindSimctl() (string, error) {
	if p := os.Getenv("MOBIUM_XCRUN_PATH"); p != "" {
		if isExec(p) {
			return p, nil
		}
		return "", mobiumerr.New(mobiumerr.ToolchainMissing, "MOBIUM_XCRUN_PATH=%s is not an executable file", p)
	}
	xcrun, err := exec.LookPath("xcrun")
	if err != nil {
		return "", ErrNoXcode
	}

	// xcrun exists under the Command Line Tools too, so its presence proves
	// nothing; asking it for simctl is the actual test.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, xcrun, "simctl", "help").Run(); err != nil {
		return "", ErrNoXcode
	}
	return xcrun, nil
}

// NewSimctl locates the toolchain and pins it to a simulator UDID.
func NewSimctl(udid string) (*Simctl, error) {
	p, err := FindSimctl()
	if err != nil {
		return nil, err
	}
	return &Simctl{Path: p, UDID: udid}, nil
}

// RunStdin is Run with something on standard input, for the one command that
// needs it: a route's waypoints are too many to pass as arguments.
func (s *Simctl) RunStdin(ctx context.Context, in io.Reader, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.Path, append([]string{"simctl"}, args...)...)
	cmd.Stdin = in
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return mobiumerr.New(mobiumerr.Timeout, "simctl %s timed out after %s", strings.Join(args, " "), defaultTimeout)
	}
	if err != nil {
		return fmt.Errorf("simctl %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	// simctl reports failures inside a zero exit as readily as anything else
	// here does; "Parsed N waypoints" on stdout is the only success signal.
	if out := stdout.String(); strings.Contains(out, "rror") {
		return mobiumerr.New(mobiumerr.DeviceServer, "simctl %s: %s", strings.Join(args, " "), strings.TrimSpace(out))
	}
	return nil
}

// Run executes `xcrun simctl <args>` and returns stdout.
func (s *Simctl) Run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.Path, append([]string{"simctl"}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return nil, mobiumerr.New(mobiumerr.Timeout, "simctl %s timed out after %s", strings.Join(args, " "), defaultTimeout)
	}
	if err != nil {
		return stdout.Bytes(), fmt.Errorf("simctl %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	// simctl reports some refusals on stderr while exiting 0 — verified with
	// `simctl privacy <udid> grant nonsense <bundle>`, which prints "An error
	// was encountered processing the command" and exits 0. Trusting the exit
	// code alone means those failures are invisible, which is the same trap
	// adb sets one layer over.
	if e := strings.TrimSpace(stderr.String()); strings.Contains(e, "An error was encountered") {
		return stdout.Bytes(), mobiumerr.New(mobiumerr.DeviceServer, "simctl %s: %s",
			strings.Join(args, " "), lastLine(e))
	}
	return stdout.Bytes(), nil
}

// Simulator is one iOS simulator.
type Simulator struct {
	UDID    string `json:"udid"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Runtime string `json:"runtime"`
}

// Booted reports whether the simulator can accept commands.
func (s Simulator) Booted() bool { return s.State == "Booted" }

// simctlList mirrors `simctl list devices --json`.
type simctlList struct {
	Devices map[string][]struct {
		UDID        string `json:"udid"`
		Name        string `json:"name"`
		State       string `json:"state"`
		IsAvailable bool   `json:"isAvailable"`
	} `json:"devices"`
}

// Simulators lists available simulators, newest runtime first.
func Simulators(ctx context.Context) ([]Simulator, error) {
	s, err := NewSimctl("")
	if err != nil {
		return nil, err
	}
	out, err := s.Run(ctx, "list", "devices", "available", "--json")
	if err != nil {
		return nil, err
	}
	return parseSimulators(out)
}

func parseSimulators(data []byte) ([]Simulator, error) {
	var list simctlList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse simctl device list: %w", err)
	}

	var out []Simulator
	for runtime, devices := range list.Devices {
		for _, d := range devices {
			if !d.IsAvailable {
				continue
			}
			out = append(out, Simulator{
				UDID:    d.UDID,
				Name:    d.Name,
				State:   d.State,
				Runtime: shortRuntime(runtime),
			})
		}
	}
	// Booted first, then by runtime descending, so the newest iOS wins a tie
	// and anything already running is offered before something that needs a
	// 30-second boot.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Booted() != out[j].Booted() {
			return out[i].Booted()
		}
		if out[i].Runtime != out[j].Runtime {
			return out[i].Runtime > out[j].Runtime
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// shortRuntime turns "com.apple.CoreSimulator.SimRuntime.iOS-18-0" into
// "iOS 18.0".
func shortRuntime(id string) string {
	name := id
	if i := strings.LastIndex(id, "SimRuntime."); i >= 0 {
		name = id[i+len("SimRuntime."):]
	}
	platform, version, ok := strings.Cut(name, "-")
	if !ok {
		return name
	}
	return platform + " " + strings.ReplaceAll(version, "-", ".")
}

// SelectSimulator resolves the simulator a command should target.
//
// An explicit UDID or name must match; otherwise exactly one booted simulator
// is required, so a run never silently drives a different device than meant.
func SelectSimulator(ctx context.Context, ref string) (*Simctl, *Simulator, error) {
	sims, err := Simulators(ctx)
	if err != nil {
		return nil, nil, err
	}

	if ref != "" {
		for i := range sims {
			if sims[i].UDID == ref || strings.EqualFold(sims[i].Name, ref) {
				s, err := NewSimctl(sims[i].UDID)
				return s, &sims[i], err
			}
		}
		return nil, nil, mobiumerr.New(mobiumerr.NoDevice, "no simulator matching %q (see `mobium devices`)", ref)
	}

	var booted []Simulator
	for _, sim := range sims {
		if sim.Booted() {
			booted = append(booted, sim)
		}
	}
	switch len(booted) {
	case 0:
		return nil, nil, ErrNoSimulator
	case 1:
		s, err := NewSimctl(booted[0].UDID)
		return s, &booted[0], err
	default:
		var names []string
		for _, b := range booted {
			names = append(names, fmt.Sprintf("%s (%s)", b.Name, b.UDID))
		}
		return nil, nil, mobiumerr.New(mobiumerr.InvalidArgument, "%d simulators are booted (%s) — pick one with --device",
			len(booted), strings.Join(names, ", "))
	}
}

// Boot starts a simulator and waits for it to finish booting.
func (s *Simctl) Boot(ctx context.Context) error {
	if _, err := s.Run(ctx, "boot", s.UDID); err != nil {
		// Booting an already-booted simulator is not a failure.
		if !strings.Contains(err.Error(), "current state: Booted") {
			return err
		}
	}
	_, err := s.Run(ctx, "bootstatus", s.UDID, "-b")
	return err
}

// InstallApp installs a .app bundle.
func (s *Simctl) InstallApp(ctx context.Context, path string) error {
	_, err := s.Run(ctx, "install", s.UDID, path)
	return err
}

// LaunchApp starts an installed app by bundle id.
func (s *Simctl) LaunchApp(ctx context.Context, bundleID string, args ...string) error {
	_, err := s.Run(ctx, append([]string{"launch", s.UDID, bundleID}, args...)...)
	return err
}

// TerminateApp stops a running app, ignoring one that is not running.
func (s *Simctl) TerminateApp(ctx context.Context, bundleID string) error {
	_, err := s.Run(ctx, "terminate", s.UDID, bundleID)
	if err != nil && strings.Contains(err.Error(), "found nothing to terminate") {
		return nil
	}
	return err
}

// AppInstalled reports whether a bundle id is installed.
func (s *Simctl) AppInstalled(ctx context.Context, bundleID string) bool {
	_, err := s.Run(ctx, "get_app_container", s.UDID, bundleID)
	return err == nil
}

// Screenshot captures the simulator screen as PNG.
//
// simctl writes to a file rather than stdout, so this goes through a temp file
// and reads it back.
func (s *Simctl) Screenshot(ctx context.Context) ([]byte, error) {
	f, err := os.CreateTemp("", "mobium-shot-*.png")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	if _, err := s.Run(ctx, "io", s.UDID, "screenshot", path); err != nil {
		return nil, err
	}
	png, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(png) == 0 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "simctl wrote an empty screenshot")
	}
	return png, nil
}

// OpenURL opens a URL or deep link on the simulator.
func (s *Simctl) OpenURL(ctx context.Context, url string) error {
	_, err := s.Run(ctx, "openurl", s.UDID, url)
	return err
}

// PrivacyServices are the permissions `simctl privacy` can change.
//
// Taken from `xcrun simctl help privacy` rather than from documentation.
// Camera and notifications are **not** on the list, which is the source of the
// asymmetry with Android — there is no simulator equivalent for either.
var PrivacyServices = []string{
	"all", "calendar", "contacts-limited", "contacts", "location",
	"location-always", "photos-add", "photos", "media-library",
	"microphone", "motion", "reminders", "siri",
}

// SetPrivacy grants or revokes one service for an app.
//
// Apple's own warning is worth repeating: bypassing the usage-description
// requirements this way can mask a bug in the app under test, because a real
// user would have seen a prompt the app must be able to handle.
func (s *Simctl) SetPrivacy(ctx context.Context, service, bundleID string, grant bool) error {
	// simctl accepts a bundle id for an app that is not installed and does
	// nothing, silently and successfully — verified against a booted iPhone 17
	// Pro. Unlike Android there is no privacy state to read back and catch it
	// with, so the check has to happen before the write.
	if !s.AppInstalled(ctx, bundleID) {
		return mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed on this simulator, so its permissions "+
			"cannot be set (simctl would report success and do nothing)", bundleID)
	}
	action := "revoke"
	if grant {
		action = "grant"
	}
	_, err := s.Run(ctx, "privacy", s.UDID, action, service, bundleID)
	return err
}

// ResetPrivacy reverts services to prompting on next use. An empty bundleID
// resets every app on the simulator.
func (s *Simctl) ResetPrivacy(ctx context.Context, service, bundleID string) error {
	args := []string{"privacy", s.UDID, "reset", service}
	if bundleID != "" {
		args = append(args, bundleID)
	}
	_, err := s.Run(ctx, args...)
	return err
}

// Appearance reports the simulator's interface style: "light", "dark", or
// "unsupported" on a runtime too old to have them.
func (s *Simctl) Appearance(ctx context.Context) (string, error) {
	out, err := s.Run(ctx, "ui", s.UDID, "appearance")
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(string(out))), nil
}

// SetAppearance switches between light and dark, and confirms it took.
func (s *Simctl) SetAppearance(ctx context.Context, mode string) error {
	if _, err := s.Run(ctx, "ui", s.UDID, "appearance", mode); err != nil {
		return err
	}
	got, err := s.Appearance(ctx)
	if err != nil {
		return err
	}
	if got != mode {
		return mobiumerr.New(mobiumerr.NotConfirmed, "asked for %s appearance but the simulator reports %q", mode, got)
	}
	return nil
}

// ListApps lists installed apps. Without includeSystem it lists only what
// someone installed, which is the useful question — a simulator ships with
// two dozen system apps nobody asked about.
//
// `simctl listapps` answers with an old-style property list and has no JSON
// option, so the output goes through `plutil`, which ships with macOS and is
// therefore already present anywhere simctl is.
func (s *Simctl) ListApps(ctx context.Context, includeSystem bool) ([]InstalledApp, error) {
	raw, err := s.Run(ctx, "listapps", s.UDID)
	if err != nil {
		return nil, err
	}
	data, err := plistToJSON(ctx, raw)
	if err != nil {
		return nil, err
	}

	var parsed map[string]struct {
		ApplicationType     string `json:"ApplicationType"`
		CFBundleIdentifier  string `json:"CFBundleIdentifier"`
		CFBundleName        string `json:"CFBundleName"`
		CFBundleDisplayName string `json:"CFBundleDisplayName"`
		CFBundleVersion     string `json:"CFBundleVersion"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("could not read the app list: %w", err)
	}

	var apps []InstalledApp
	for bundle, info := range parsed {
		isSystem := info.ApplicationType == "System"
		if isSystem && !includeSystem {
			continue
		}
		id := info.CFBundleIdentifier
		if id == "" {
			id = bundle
		}
		name := info.CFBundleDisplayName
		if name == "" {
			name = info.CFBundleName
		}
		apps = append(apps, InstalledApp{
			ID: id, Name: name, Version: info.CFBundleVersion, System: isSystem,
		})
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })
	return apps, nil
}

// plistToJSON converts a property list using plutil, which is part of macOS.
func plistToJSON(ctx context.Context, plist []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "plutil", "-convert", "json", "-o", "-", "--", "-")
	cmd.Stdin = bytes.NewReader(plist)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("plutil: %w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return out.Bytes(), nil
}

// UninstallApp removes an app from the simulator.
func (s *Simctl) UninstallApp(ctx context.Context, bundleID string) error {
	if !s.AppInstalled(ctx, bundleID) {
		return mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed on this simulator", bundleID)
	}
	_, err := s.Run(ctx, "uninstall", s.UDID, bundleID)
	return err
}

// Pasteboard reads the simulator's clipboard.
//
// `simctl pbpaste` writes it to stdout. Unlike the Android side this is a
// plain read with no focus rules attached, which is why iOS can answer the
// question and Android cannot.
func (s *Simctl) Pasteboard(ctx context.Context) (string, error) {
	out, err := s.Run(ctx, "pbpaste", s.UDID)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// SetPasteboard writes the simulator's clipboard, reading the text from stdin
// so that quotes, newlines and non-ASCII never touch a command line.
func (s *Simctl) SetPasteboard(ctx context.Context, text string) error {
	return s.RunStdin(ctx, strings.NewReader(text), "pbcopy", s.UDID)
}

// SetLocation places the simulator at a coordinate.
//
// There is no counterpart that reads it back. `simctl location` has list,
// clear, set, run and start, and no get — checked again under Xcode 26. Same
// shape as `simctl privacy` (defect 27): the write is real and the state is
// invisible, so anything built on this says which question it answered.
func (s *Simctl) SetLocation(ctx context.Context, lat, lon float64) error {
	coord := strconv.FormatFloat(lat, 'f', 6, 64) + "," + strconv.FormatFloat(lon, 'f', 6, 64)
	_, err := s.Run(ctx, "location", s.UDID, "set", coord)
	return err
}

// Point is one waypoint.
type Point struct{ Lat, Lon float64 }

// StartRoute walks the simulator along a series of waypoints.
//
// simctl interpolates between them itself, so this hands the motion over and
// returns — there is nothing here to keep running. Waypoints go in on stdin
// rather than as arguments: a real track is hundreds of points and would
// otherwise hit the command-line length limit.
//
// It reports that the waypoints parsed and nothing more, which is the same
// honesty problem as SetLocation. Whether an app sees the device move is
// observable only from inside an app.
func (s *Simctl) StartRoute(ctx context.Context, pts []Point, speedMPS float64) error {
	if len(pts) < 2 {
		return mobiumerr.New(mobiumerr.InvalidArgument, "a route needs at least two waypoints, got %d", len(pts))
	}
	var in bytes.Buffer
	for _, p := range pts {
		fmt.Fprintf(&in, "%s,%s\n",
			strconv.FormatFloat(p.Lat, 'f', 6, 64),
			strconv.FormatFloat(p.Lon, 'f', 6, 64))
	}
	args := []string{"location", s.UDID, "start"}
	if speedMPS > 0 {
		args = append(args, "--speed="+strconv.FormatFloat(speedMPS, 'f', -1, 64))
	}
	args = append(args, "-")
	return s.RunStdin(ctx, &in, args...)
}

// ClearLocation stops any running scenario and clears the simulated position.
func (s *Simctl) ClearLocation(ctx context.Context) error {
	_, err := s.Run(ctx, "location", s.UDID, "clear")
	return err
}
