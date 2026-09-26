package device

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// A real iPhone is reached through `xcrun devicectl`, Xcode's CoreDevice
// command line, the way a simulator is reached through `simctl`. The two cover
// different ground: devicectl has no screenshot, no privacy database, no
// appearance switch, no pasteboard and no simulated location, and the driver
// declines those on a phone rather than approximating them.
//
// The transport turned out to be the easy part. CoreDevice keeps a tunnel open
// to a paired, connected phone and gives it an IPv6 address — `tunnelIPAddress`
// in `devicectl list devices` — and WebDriverAgent's port is plain HTTP over
// it. No usbmux protocol, no port forwarder. Measured on an iPhone 15 Plus on
// iOS 26.6.2 over USB, where devicectl reports the transport as `wired`.

// Phone is one real iOS device as CoreDevice reports it.
type Phone struct {
	// UDID is the hardware identifier — what xcodebuild's -destination and
	// Finder call the device, and what Mobium uses as its serial.
	UDID string `json:"udid"`
	// Identifier is CoreDevice's own UUID for the pairing.
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
	Model      string `json:"model"`
	OSVersion  string `json:"osVersion"`
	// Transport is `wired` or `localNetwork` while the phone is reachable,
	// and empty when it is only remembered from an earlier pairing.
	Transport     string `json:"transport"`
	TunnelIP      string `json:"tunnelIP"`
	Paired        bool   `json:"paired"`
	DeveloperMode bool   `json:"developerMode"`
}

// Connected reports whether the phone can be driven right now. A phone
// devicectl remembers but cannot reach is listed with no transport.
func (p Phone) Connected() bool { return p.Paired && p.Transport != "" }

// Devicectl is a located xcrun, pinned to one phone.
type Devicectl struct {
	Path string
	// Phone is the device as it was when it was selected. TunnelIP can
	// change on reconnect; Refresh reads it again.
	Phone Phone
}

// FindDevicectl checks that xcrun can run devicectl. It ships with Xcode 15
// and later, so this fails on an older Xcode and under the Command Line Tools.
func FindDevicectl() (string, error) {
	xcrun, err := FindSimctl()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, xcrun, "devicectl", "--version").Run(); err != nil {
		return "", mobiumerr.New(mobiumerr.ToolchainMissing, "`xcrun devicectl` is unavailable — real iPhones need "+
			"Xcode 15 or later")
	}
	return xcrun, nil
}

// devicectlResult is the envelope every `--json-output -` answer arrives in.
type devicectlResult struct {
	Info struct {
		Outcome string `json:"outcome"`
	} `json:"info"`
	Error *struct {
		Code     int    `json:"code"`
		Domain   string `json:"domain"`
		UserInfo map[string]struct {
			String string `json:"string"`
		} `json:"userInfo"`
	} `json:"error"`
	Result json.RawMessage `json:"result"`
}

// message picks the most readable description out of an NSError-shaped error.
func (r *devicectlResult) message() string {
	if r.Error == nil {
		return ""
	}
	for _, k := range []string{"NSLocalizedDescription", "NSLocalizedFailureReason"} {
		if v, ok := r.Error.UserInfo[k]; ok && v.String != "" {
			return v.String
		}
	}
	return fmt.Sprintf("%s error %d", r.Error.Domain, r.Error.Code)
}

// runDevicectl executes `xcrun devicectl <args> --json-output -` and returns
// the `result` object.
//
// The outcome is read from the JSON rather than trusted from the exit code:
// with `--json-output -` stdout carries only the JSON, the human table goes to
// stderr, and `info.outcome` is the verdict. A launch of an unknown bundle id
// exits 1 with an error object; an uninstall of one exits 0 and says
// "success", which is why UninstallApp checks the app is there first.
func runDevicectl(ctx context.Context, xcrun string, args ...string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	full := append([]string{"devicectl"}, args...)
	full = append(full, "--json-output", "-")
	cmd := exec.CommandContext(ctx, xcrun, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	what := "devicectl " + strings.Join(args, " ")
	if ctx.Err() == context.DeadlineExceeded {
		return nil, mobiumerr.New(mobiumerr.Timeout, "%s timed out after %s", what, defaultTimeout)
	}
	var res devicectlResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		if runErr != nil {
			return nil, fmt.Errorf("%s: %w: %s", what, runErr, lastLine(strings.TrimSpace(stderr.String())))
		}
		return nil, fmt.Errorf("%s: unreadable output: %w", what, err)
	}
	if res.Info.Outcome != "success" || runErr != nil {
		msg := res.message()
		if msg == "" {
			msg = lastLine(strings.TrimSpace(stderr.String()))
		}
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "%s: %s", what, msg)
	}
	return res.Result, nil
}

// devicectlDevices mirrors the part of `devicectl list devices` read here.
type devicectlDevices struct {
	Devices []struct {
		Identifier           string `json:"identifier"`
		ConnectionProperties struct {
			PairingState  string `json:"pairingState"`
			TransportType string `json:"transportType"`
			TunnelIPAddr  string `json:"tunnelIPAddress"`
		} `json:"connectionProperties"`
		DeviceProperties struct {
			Name                string `json:"name"`
			OSVersionNumber     string `json:"osVersionNumber"`
			DeveloperModeStatus string `json:"developerModeStatus"`
		} `json:"deviceProperties"`
		HardwareProperties struct {
			UDID        string `json:"udid"`
			Platform    string `json:"platform"`
			Reality     string `json:"reality"`
			MarketingNm string `json:"marketingName"`
			ProductType string `json:"productType"`
		} `json:"hardwareProperties"`
	} `json:"devices"`
}

// Phones lists real iOS devices CoreDevice knows, connected ones first.
func Phones(ctx context.Context) ([]Phone, error) {
	xcrun, err := FindDevicectl()
	if err != nil {
		return nil, err
	}
	raw, err := runDevicectl(ctx, xcrun, "list", "devices")
	if err != nil {
		return nil, err
	}
	return parsePhones(raw)
}

func parsePhones(raw json.RawMessage) ([]Phone, error) {
	var list devicectlDevices
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parse devicectl device list: %w", err)
	}
	var out []Phone
	for _, d := range list.Devices {
		hw := d.HardwareProperties
		// devicectl also lists watches, Vision Pros and paired Macs. Only a
		// physical iPhone or iPad is something WebDriverAgent runs on.
		if hw.Platform != "iOS" || hw.Reality != "physical" {
			continue
		}
		model := hw.MarketingNm
		if model == "" {
			model = hw.ProductType
		}
		out = append(out, Phone{
			UDID:          hw.UDID,
			Identifier:    d.Identifier,
			Name:          d.DeviceProperties.Name,
			Model:         model,
			OSVersion:     d.DeviceProperties.OSVersionNumber,
			Transport:     d.ConnectionProperties.TransportType,
			TunnelIP:      d.ConnectionProperties.TunnelIPAddr,
			Paired:        d.ConnectionProperties.PairingState == "paired",
			DeveloperMode: d.DeviceProperties.DeveloperModeStatus == "enabled",
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Connected() != out[j].Connected() {
			return out[i].Connected()
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Matches reports whether ref names this phone, by UDID, CoreDevice
// identifier or name.
func (p Phone) Matches(ref string) bool {
	return ref == p.UDID || strings.EqualFold(ref, p.Identifier) || strings.EqualFold(ref, p.Name)
}

// NewDevicectl pins the toolchain to one phone.
func NewDevicectl(p Phone) (*Devicectl, error) {
	xcrun, err := FindDevicectl()
	if err != nil {
		return nil, err
	}
	return &Devicectl{Path: xcrun, Phone: p}, nil
}

// Usable explains why a connected phone cannot be driven, or returns nil.
// Each of these fails later with an error naming something else entirely, so
// they are checked up front.
func (p Phone) Usable() error {
	if !p.Paired {
		return mobiumerr.New(mobiumerr.DeviceNotReady, "%s is not paired with this Mac — unlock it, tap Trust This "+
			"Computer, and enter the passcode", p.Name)
	}
	if !p.Connected() {
		return mobiumerr.New(mobiumerr.DeviceNotReady, "%s is paired but not reachable — connect it with a cable "+
			"and unlock it", p.Name)
	}
	if !p.DeveloperMode {
		return mobiumerr.New(mobiumerr.DeviceNotReady, "Developer Mode is off on %s — open Xcode's Devices and "+
			"Simulators window once with the phone connected, then turn it on in "+
			"Settings > Privacy & Security > Developer Mode (the phone restarts)", p.Name)
	}
	return nil
}

func (d *Devicectl) run(ctx context.Context, args ...string) (json.RawMessage, error) {
	return runDevicectl(ctx, d.Path, args...)
}

// Refresh re-reads the phone's state, chiefly its tunnel address, which is
// assigned per connection and can be empty until something asks for it.
func (d *Devicectl) Refresh(ctx context.Context) (Phone, error) {
	phones, err := Phones(ctx)
	if err != nil {
		return d.Phone, err
	}
	for _, p := range phones {
		if p.UDID == d.Phone.UDID {
			d.Phone = p
			return p, nil
		}
	}
	return d.Phone, mobiumerr.New(mobiumerr.NoDevice, "%s is no longer listed by devicectl", d.Phone.Name)
}

// LaunchApp starts or activates an app. Like `simctl launch`, it returns when
// the launch is dispatched, not when the app is on screen — measured: Calendar
// was reported launched in 0.2s while Settings was still in the foreground.
func (d *Devicectl) LaunchApp(ctx context.Context, bundleID string) error {
	_, err := d.run(ctx, "device", "process", "launch", "--device", d.Phone.UDID, bundleID)
	return err
}

// InstallApp installs a signed .app bundle. An app built for the simulator,
// or signed for a different device, is refused by the phone.
func (d *Devicectl) InstallApp(ctx context.Context, path string) error {
	_, err := d.run(ctx, "device", "install", "app", "--device", d.Phone.UDID, path)
	return err
}

// UninstallApp removes an app.
//
// devicectl reports success, exit 0 and `"outcome": "success"`, for a bundle
// id that is not installed, so that is checked first: the caller confirms the
// app is gone afterwards, which a missing app passes trivially, and Mobium said
// "uninstalled com.example.not.installed" until it did.
func (d *Devicectl) UninstallApp(ctx context.Context, bundleID string) error {
	apps, err := d.ListApps(ctx, true)
	if err != nil {
		return err
	}
	installed := false
	for _, a := range apps {
		if a.ID == bundleID {
			installed = true
			break
		}
	}
	if !installed {
		return mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed on %s — `mobium apps --system` lists what is",
			bundleID, d.Phone.Name)
	}
	_, err = d.run(ctx, "device", "uninstall", "app", "--device", d.Phone.UDID, bundleID)
	return err
}

// devicectlApps mirrors `devicectl device info apps`.
type devicectlApps struct {
	Apps []struct {
		BundleIdentifier string `json:"bundleIdentifier"`
		Name             string `json:"name"`
		Version          string `json:"version"`
		DefaultApp       bool   `json:"defaultApp"`
		Hidden           bool   `json:"hidden"`
	} `json:"apps"`
}

// ListApps reports installed apps: without includeSystem, the user's — App
// Store apps and anything a developer installed — and with it, Apple's too,
// marked as system.
//
// Always fetched with --include-all-apps and filtered here. devicectl's
// default listing, and --include-removable-apps with it, returned only the 6
// developer-built apps on a phone holding 16 App Store apps; only
// --include-default-apps or --include-all-apps included those, alongside 69
// of Apple's. Measured on an iPhone 15 Plus, iOS 26.6.2.
func (d *Devicectl) ListApps(ctx context.Context, includeSystem bool) ([]InstalledApp, error) {
	raw, err := d.run(ctx, "device", "info", "apps", "--device", d.Phone.UDID, "--include-all-apps")
	if err != nil {
		return nil, err
	}
	return parsePhoneApps(raw, includeSystem)
}

func parsePhoneApps(raw json.RawMessage, includeSystem bool) ([]InstalledApp, error) {
	var list devicectlApps
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parse devicectl app list: %w", err)
	}
	var out []InstalledApp
	for _, a := range list.Apps {
		if a.Hidden {
			continue
		}
		if a.DefaultApp && !includeSystem {
			continue
		}
		out = append(out, InstalledApp{
			ID: a.BundleIdentifier, Name: a.Name, Version: a.Version, System: a.DefaultApp,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
