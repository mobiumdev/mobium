package mobiumdriver

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
)

// What WDA does differently on a real iPhone. Everything that goes through
// WebDriverAgent itself — the hierarchy, taps, gestures, text, alerts, buttons
// — is the simulator code unchanged; only the things simctl did are replaced.
//
// Measured on an iPhone 15 Plus, iOS 26.6.2, over USB: Settings' hierarchy in
// 1.95s and the home screen's in about 6s, against well under a second on a
// simulator; a screenshot in 0.5s.

// phoneReadyTimeout bounds the wait for the runner after xcodebuild starts
// it. Measured at 2s for a runner already installed; a first run installs it
// over the cable first.
const phoneReadyTimeout = 2 * time.Minute

// OnPhone reports whether this driver is on a real iPhone rather than a
// simulator — for advice that differs, like how to turn Web Inspector on.
func (w *WDA) OnPhone() bool { return w.phone != nil }

// phoneBase is the runner's address through CoreDevice's tunnel. An IPv6
// literal needs brackets in a URL.
func phoneBase(tunnelIP string) string {
	return fmt.Sprintf("http://[%s]:%d", tunnelIP, device.WDAPort)
}

// startPhone brings WebDriverAgent up on the phone and points the client at
// it: reusing a runner that is already answering, otherwise building one if
// needed and starting it through xcodebuild.
func (w *WDA) startPhone(ctx context.Context, progress func(string)) error {
	p := w.phone.Phone
	if err := p.Usable(); err != nil {
		return err
	}

	// A runner someone else started — Xcode, a person at a terminal — is
	// used as it is. It is not ours, so Close leaves it running. One a daemon
	// that died left behind is ours after all, and is taken over, so this
	// session's end stops it.
	if p.TunnelIP != "" {
		w.w3c.setBase(phoneBase(p.TunnelIP))
		quick, cancel := context.WithTimeout(ctx, healthTimeout)
		ready := w.w3c.ready(quick)
		cancel()
		if ready {
			w.runner = device.AdoptPhoneRunner(ctx, p.UDID)
			if !w.answersOnWiFi(ctx) {
				return nil
			}
			// It listens on the phone's network as well as the cable, as every
			// runner did before CHALLENGES 153. One of ours is replaced by one
			// bound to the tunnel; someone else's is not ours to stop.
			if w.runner == nil {
				return mobiumerr.New(mobiumerr.DeviceNotReady, "a WebDriverAgent that Mobium did not start is "+
					"running on %s and answers on the phone's Wi-Fi, so anyone on that network could drive the "+
					"phone through it; Mobium will not use it", p.Label()).
					WithRemedy("stop it — end the Xcode test or the xcodebuild that runs it — and run the " +
						"command again; Mobium then starts its own, reachable only over the cable")
			}
			w.runner.Stop()
			w.runner = nil
			w.w3c.setBase("")
		}
	}

	team, err := device.SigningTeam(ctx)
	if err != nil {
		return err
	}
	xctestrun, err := device.EnsurePhoneWDA(ctx, team, p.UDID, progress)
	if err != nil {
		return err
	}
	if progress != nil {
		// xcodebuild installs the runner if it is missing and says nothing,
		// so ask the phone first. A listing that fails costs only the notice.
		if apps, err := w.phone.ListApps(ctx, false); err == nil {
			if notice := device.PhoneWDAInstallNotice(team, p, apps); notice != "" {
				progress(notice)
			}
		}
		progress("starting WebDriverAgent on " + p.Label())
	}
	// Bound to the tunnel, not every interface: on Wi-Fi an unbound runner
	// answered the whole network (CHALLENGES 153). With no tunnel address to
	// bind to, it does not start at all rather than start open.
	bindIP, err := w.phone.WakeTunnel(ctx)
	if err != nil {
		return err
	}
	runner, err := device.StartPhoneWDA(xctestrun, p.UDID, device.PhoneWDALog(team, p.UDID), bindIP)
	if err != nil {
		return err
	}
	w.runner = runner
	w.w3c.setBase(phoneBase(bindIP))
	relaunched := false

	deadline := time.Now().Add(phoneReadyTimeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			w.teardownLocked(context.Background())
			return ctx.Err()
		}
		if runner.Exited() {
			w.runner = nil
			return mobiumerr.New(mobiumerr.DeviceNotReady, "WebDriverAgent did not start on %s: %s. The full log is %s",
				p.Label(), runner.Failure(), runner.Log)
		}
		if why := runner.Blocked(); why != "" {
			w.teardownLocked(ctx)
			return mobiumerr.New(mobiumerr.DeviceNotReady, "WebDriverAgent cannot start on %s: %s", p.Label(), why)
		}
		if w.w3c.ready(ctx) {
			device.MarkPhoneWDAInstalled(team, p.UDID)
			return nil
		}
		// The tunnel's address is per connection. If it reconnected while
		// the runner started, the runner is bound to an address that no
		// longer exists and will never answer: start it again, once, on the
		// new one.
		if fresh, err := w.phone.Refresh(ctx); err == nil && fresh.TunnelIP != "" && fresh.TunnelIP != bindIP && !relaunched {
			relaunched = true
			runner.Stop()
			bindIP = fresh.TunnelIP
			if runner, err = device.StartPhoneWDA(xctestrun, p.UDID, device.PhoneWDALog(team, p.UDID), bindIP); err != nil {
				w.runner = nil
				return err
			}
			w.runner = runner
			w.w3c.setBase(phoneBase(bindIP))
		}
		time.Sleep(500 * time.Millisecond)
	}
	w.teardownLocked(ctx)
	return mobiumerr.New(mobiumerr.Timeout, "WebDriverAgent did not answer on %s within %s. Keep the phone "+
		"unlocked while it starts; the runner's log is %s", p.Label(), phoneReadyTimeout, runner.Log)
}

// phoneScreenshot captures the screen through WebDriverAgent: devicectl has no
// screenshot, and the runner's is in pixels, like simctl's.
func (w *WDA) phoneScreenshot(ctx context.Context) ([]byte, error) {
	var resp struct {
		Value string `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, "/screenshot", nil, &resp); err != nil {
		return nil, fmt.Errorf("capture iPhone screen: %w", err)
	}
	png, err := base64.StdEncoding.DecodeString(resp.Value)
	if err != nil || !bytes.HasPrefix(png, pngMagic) {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "capture iPhone screen: WebDriverAgent returned something other than a PNG")
	}
	return png, nil
}

// phoneTerminate stops an app through WebDriverAgent, which knows apps by
// bundle id; devicectl only terminates a process id. Stopping an app that is
// not running is not an error, the same as simctl.
func (w *WDA) phoneTerminate(ctx context.Context, appID string) error {
	return w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/wda/apps/terminate"),
		map[string]interface{}{"bundleId": appID}, nil)
}

// phoneOpenURL opens a URL through WebDriverAgent. devicectl can only hand a
// URL to an app it is told the bundle id of, and a URL names no app.
func (w *WDA) phoneOpenURL(ctx context.Context, url string) error {
	return w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/url"),
		map[string]interface{}{"url": url}, nil)
}

// phoneScreenRecord records from the runner's screen stream, at the tunnel
// address the runner answers on. The stream is read on the Mac and nothing
// is written on the phone.
func (w *WDA) phoneScreenRecord(ctx context.Context) (device.Recording, error) {
	base, _ := w.w3c.endpoint()
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" {
		return nil, mobiumerr.New(mobiumerr.DeviceNotReady, "WebDriverAgent has no address on %s to record from",
			w.phone.Phone.Label())
	}
	return device.StartPhoneScreenRecord(ctx, u.Hostname())
}

// simulatorOnly is what a real iPhone lacks, and why. Each is done by simctl
// on a simulator, and devicectl has no counterpart.
var simulatorOnly = map[string]string{
	CapPermissions:   "grant or revoke permissions (simctl privacy)",
	CapAppearance:    "switch light and dark (simctl ui)",
	CapClipboard:     "write the clipboard (simctl pbcopy)",
	CapClipboardRead: "read the clipboard (simctl pbpaste)",
	CapGeolocation:   "simulate a location (simctl location)",
	CapRoutes:        "simulate a route (simctl location)",
	CapClearData:     "clear an app's data in place (simctl; devicectl cannot delete from an app's container)",
}

// phoneRemedies replaces "that needs a simulator" where a phone has a route
// of its own that works. Accessibility settings had one here until the
// route was built in (wdaaxphone.go).
var phoneRemedies = map[string]string{
	CapClearData: "a real iPhone cannot clear an app's data in place — devicectl cannot delete from an app's " +
		"container. Give the app's own .app or .ipa — path in app_clear_data, --bundle on the CLI — and the app " +
		"is uninstalled and installed again from it, which is the reset a phone has",
}

// HasCapability declines, on a phone, what only a simulator can do. On a
// simulator every method is real, so the answer is always yes.
func (w *WDA) HasCapability(name string) bool {
	if w.phone == nil {
		return true
	}
	_, simOnly := simulatorOnly[name]
	return !simOnly
}

// iosNotBuilt is what WebDriverAgent does not do on a simulator or a phone,
// and why — each worded for what is known: "not built" where the platform
// has a way nobody here has wired up, "cannot" only where it has none.
var iosNotBuilt = map[string]string{
	CapLocalization: "per-app language is not built for iOS yet: a simulator takes AppleLanguages at " +
		"launch, and nothing here passes it",
	CapClock:         "the timezone is not built for iOS yet",
	CapNotifications: "reading or posting notifications is not built for iOS yet",
	CapInterruptions: "iOS has no call or message to simulate: a simulator has no telephony, and a real " +
		"iPhone cannot be made to ring from outside",
}

// DeclineReason names why this device lacks a capability, so the tool
// layer's refusal carries it rather than blaming the backend: what only a
// simulator does, on a phone, and what is not built for iOS at all.
func (w *WDA) DeclineReason(capability string) error {
	if reason, ok := iosNotBuilt[capability]; ok {
		return mobiumerr.New(mobiumerr.Unsupported, "%s", reason)
	}
	if w.phone == nil {
		return nil
	}
	if _, simOnly := simulatorOnly[capability]; !simOnly {
		return nil
	}
	return w.simOnly(capability)
}

// simOnly guards the simulator-only methods. HasCapability already keeps
// callers away on a phone; this is the backstop, and names the reason.
func (w *WDA) simOnly(capability string) error {
	if w.sim != nil {
		return nil
	}
	if remedy, ok := phoneRemedies[capability]; ok {
		return mobiumerr.New(mobiumerr.Unsupported, "%s", remedy)
	}
	return mobiumerr.New(mobiumerr.Unsupported, "a real iPhone cannot %s here — that needs a simulator",
		simulatorOnly[capability])
}

// springboardBundleID is the home screen's bundle id.
const springboardBundleID = "com.apple.springboard"

// expectApp tells WebDriverAgent which app is about to come forward, before
// the switch, on a phone.
//
// **Without it, the first read after an app switch stalled for 61 seconds and
// returned the previous app's screen** — four times out of four, Settings to
// Calendar, on an iPhone 15 Plus on iOS 26.6.2; the same stall followed a
// Home press. For a while after a switch iOS lists both apps as active and
// reports both in the foreground state, and WebDriverAgent, left to choose,
// snapshots the one being suspended: its log shows a single attribute fetch
// taking 60.009s on that app's elements. Its `accessibilityDeadline` setting
// did not shorten it — the fetch that hangs is not one it guards. Naming the
// app with `defaultActiveApplication` did: nine switches across three app
// pairs read the right app in 0.8–3.4s, and Home read the home screen in 5.9s.
//
// **And the hint has to come off again once the switch has landed**, which
// settleExpected does on the first read that shows the expected app. Left in
// place it pins every read to that app, and a system dialog is not the app's
// window: MobiumApp's location prompt was on screen, `alert` saw it, and `map`
// showed only the app underneath, so nothing could tap its buttons. Back on
// "auto", the same read mapped Allow Once, Allow While Using App and Don't
// Allow. The stall is a property of the switch, not of the hint's absence —
// reads after it settled were measured fine on "auto".
//
// Only where the next app is known: a launch, and Home. A tap that opens
// another app gives no such warning. Phone only, because the stall was
// measured only there. A failure to set it is not an error: the switch still
// happens, and at worst the next read is slow.
func (w *WDA) expectApp(ctx context.Context, bundleID string) {
	if w.phone == nil {
		return
	}
	if w.setActiveAppHint(ctx, bundleID) == nil {
		w.hintMu.Lock()
		w.expecting = bundleID
		w.hintMu.Unlock()
	}
}

// settleExpected clears the hint once a read shows the app it named.
func (w *WDA) settleExpected(ctx context.Context, foreground string) {
	w.hintMu.Lock()
	want := w.expecting
	if want == "" || foreground != want {
		w.hintMu.Unlock()
		return
	}
	w.expecting = ""
	w.hintMu.Unlock()
	_ = w.setActiveAppHint(ctx, "auto")
}

// clearExpected takes the hint off when the switch it announced did not
// happen, so it cannot go on hiding dialogs.
func (w *WDA) clearExpected(ctx context.Context) {
	w.hintMu.Lock()
	had := w.expecting != ""
	w.expecting = ""
	w.hintMu.Unlock()
	if had {
		_ = w.setActiveAppHint(ctx, "auto")
	}
}

func (w *WDA) setActiveAppHint(ctx context.Context, bundleID string) error {
	return w.w3c.do(ctx, http.MethodPost, w.w3c.sessionPath("/appium/settings"),
		map[string]interface{}{"settings": map[string]interface{}{
			"defaultActiveApplication": bundleID,
		}}, nil)
}

// answersOnWiFi reports whether the runner in front of this session also
// answers at the phone's Wi-Fi address, which it reports in its status. A
// phone with no Wi-Fi address, or a runner that does not answer there within
// a moment, is not reachable that way.
func (w *WDA) answersOnWiFi(ctx context.Context) bool {
	var st struct {
		Value struct {
			IOS struct {
				IP string `json:"ip"`
			} `json:"ios"`
		} `json:"value"`
	}
	if err := w.w3c.do(ctx, http.MethodGet, "/status", nil, &st); err != nil || st.Value.IOS.IP == "" {
		return false
	}
	quick, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(quick, http.MethodGet,
		fmt.Sprintf("http://%s/status", net.JoinHostPort(st.Value.IOS.IP, fmt.Sprint(device.WDAPort))), nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
