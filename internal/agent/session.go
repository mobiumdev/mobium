package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/webview"
)

// SessionView is what app_session answers: the session it started, ended or
// found, and with status, every session open.
type SessionView struct {
	Action   string `json:"action"`
	Device   string `json:"device,omitempty"`
	Platform string `json:"platform,omitempty"`
	Driver   string `json:"driver,omitempty"`
	// Reused says start found a session already open for the device and kept
	// it, rather than starting a device-side server again.
	Reused bool `json:"reused,omitempty"`
	// App is the app start launched, when one was asked for.
	App string `json:"app,omitempty"`
	// Terminated is the app end stopped: the one start launched.
	Terminated string `json:"terminated,omitempty"`
	// ClosedTabs counts the browser tabs end closed, the ones the session
	// opened, as the browser confirmed each close.
	ClosedTabs int `json:"closed_tabs,omitempty"`
	// Exposure says what else can reach the device while the session is
	// open, where something can: a real Android phone's UiAutomator2 server
	// listens on the phone's network as well as adb (CHALLENGES 154).
	Exposure string `json:"exposure,omitempty"`
	// Ended says end closed a session; false means there was none to close.
	Ended    bool          `json:"ended"`
	Sessions []SessionInfo `json:"sessions"`
}

// SessionInfo is one open session.
type SessionInfo struct {
	Device   string `json:"device"`
	Platform string `json:"platform"`
	Driver   string `json:"driver"`
}

// sessionTool is app_session: the explicit start and end of a device session, as
// Appium's new session and quit are.
//
// Every other tool opens a session on first use and nothing closed one short
// of stopping the daemon, which ends every device's at once. start opens it
// now, so a slow first start (installing UiAutomator2, building
// WebDriverAgent) happens where the caller asked for it and not inside their
// first tap; end closes one device's, with the same teardown the daemon's
// shutdown does — accessibility settings put back, a recording or route
// stopped, WebViews detached, the device-side server stopped — and forgets
// its refs and dialog rules, so the next start begins clean.
func (h *Handlers) sessionTool(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action")))
	switch action {
	case "", "status":
		return h.sessionStatus(), nil
	case "start":
		return h.sessionStart(ctx, args)
	case "end", "quit", "stop":
		return h.sessionEnd(args)
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument,
			"unknown action %q (want \"start\", \"end\" or \"status\")", action)
	}
}

// platformBackend reads platform into the driver it implies. iOS has one
// driver, so naming the platform is enough; Android keeps its default.
func platformBackend(args map[string]interface{}) (map[string]interface{}, error) {
	platform := strings.ToLower(strings.TrimSpace(stringArg(args, "platform")))
	backend := strings.ToLower(strings.TrimSpace(stringArg(args, "driver")))
	out := map[string]interface{}{}
	for k, v := range args {
		out[k] = v
	}
	switch platform {
	case "":
	case "ios":
		if backend == "" {
			out["driver"] = string(BackendWDA)
		} else if Backend(backend) == BackendUIA2 || Backend(backend) == BackendDump {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument,
				"platform \"ios\" and driver %q disagree: %s drives Android", backend, backend)
		}
	case "android":
		if Backend(backend) == BackendWDA {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument,
				"platform \"android\" and driver \"wda\" disagree: WebDriverAgent drives iOS")
		}
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument,
			"unknown platform %q (want \"android\" or \"ios\")", platform)
	}
	return out, nil
}

func (h *Handlers) sessionStart(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	args, err := platformBackend(args)
	if err != nil {
		return nil, err
	}
	open := map[*session]bool{}
	for _, s := range h.sessions {
		open[s] = true
	}
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	view := SessionView{
		Action:   "start",
		Device:   s.dev.Serial,
		Platform: sessionPlatform(s),
		Driver:   string(s.backend),
		Reused:   open[s],
		Ended:    false,
		Sessions: h.openSessions(),
	}
	verb := "started"
	if view.Reused {
		verb = "already open"
	}
	text := fmt.Sprintf("session %s on %s (%s, %s)", verb, view.Device, view.Platform, view.Driver)
	if exposure := sessionExposure(s); exposure != "" {
		view.Exposure = exposure
		text += "\nnote: " + exposure
	}

	if app := stringArg(args, "app"); app != "" {
		// Stopped first, as Appium's UiAutomator2 driver stops the app before
		// a session launches it, so the session begins at the app's first
		// screen rather than wherever it was left. Settings resumes on the
		// last page it showed, and a quick start that tapped a row on the
		// main screen found the page it had opened last time instead. Data is
		// kept: this is a relaunch, not a reset.
		ctrl, err := h.appControl(s)
		if err != nil {
			return nil, err
		}
		s.closeWeb()
		if err := ctrl.Terminate(ctx, app); err != nil {
			return nil, fmt.Errorf("the session on %s is open, but stopping %s before launching it failed: %w", view.Device, app, err)
		}
		delete(h.refs, s.dev.Serial)
		if _, err := h.launchAppOn(ctx, s, map[string]interface{}{"app": app}); err != nil {
			// The session is open and stays open: the caller asked for both,
			// and a launch that failed is theirs to retry or to quit.
			return nil, fmt.Errorf("the session on %s is open, but launching %s failed: %w", view.Device, app, err)
		}
		view.App = app
		s.launched = app
		text += "; " + app + " was launched fresh and is in the foreground"
	}
	return Result(text, view), nil
}

func (h *Handlers) sessionEnd(args map[string]interface{}) (*ToolsCallResult, error) {
	want := stringArg(args, "device")
	if want == "" {
		want = h.defaultDevice
	}
	var keys []string
	for key, s := range h.sessions {
		if want == "" || key == want || s.dev.Serial == want {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	if len(keys) == 0 {
		// Idempotent, as Appium's quit on a session already gone: the state
		// asked for is the state the device is in.
		where := "any device"
		if want != "" {
			where = want
		}
		return Result("no session was open on "+where,
			SessionView{Action: "end", Device: want, Ended: false, Sessions: h.openSessions()}), nil
	}
	if want == "" && len(keys) > 1 {
		var names []string
		for _, k := range keys {
			names = append(names, h.sessions[k].dev.Serial)
		}
		return nil, mobiumerr.New(mobiumerr.InvalidArgument,
			"sessions are open on %d devices (%s); name one with device, or stop the daemon to end them all",
			len(keys), strings.Join(names, ", "))
	}

	s := h.sessions[keys[0]]
	view := SessionView{Action: "end", Device: s.dev.Serial, Platform: sessionPlatform(s), Driver: string(s.backend), Ended: true}
	closed, stopped := h.stopLaunched(s)
	view.ClosedTabs = closed
	s.close()
	delete(h.sessions, keys[0])
	delete(h.refs, s.dev.Serial)
	delete(h.dialogRules, s.dev.Serial)
	view.Sessions = h.openSessions()
	text := fmt.Sprintf("session ended on %s; anything it changed for the session is put back", view.Device)
	if closed > 0 {
		text += fmt.Sprintf("; closed the %d tab(s) it opened", closed)
	}
	switch {
	case stopped == nil && s.launched != "":
		view.Terminated = s.launched
		text += "; " + s.launched + ", which the session launched, was stopped"
	case stopped != nil:
		// The session still ends: that is what was asked, and it did.
		text += fmt.Sprintf("; %s, which the session launched, could not be stopped: %v", s.launched, stopped)
	}
	return Result(text, view), nil
}

// endTerminateTimeout bounds stopping the launched app on the way out, so an
// unresponsive device cannot hold up the end of a session. Half the daemon's
// budget for closing every session at shutdown, so the rest of the teardown
// still runs within it.
const endTerminateTimeout = 10 * time.Second

// stopLaunched stops the app the session's start launched, before the driver
// goes: terminating needs the device, and on iOS the driver is how.
func (h *Handlers) stopLaunched(s *session) (closedTabs int, err error) {
	if s.launched == "" {
		return 0, nil
	}
	ctrl, err := h.appControl(s)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), endTerminateTimeout)
	defer cancel()
	s.closeWeb()
	closedTabs = h.closeOpenedTabs(ctx, s)
	return closedTabs, ctrl.Terminate(ctx, s.launched)
}

// tabsWait bounds how long an open waits for the tab it made to be listed.
// Chrome publishes the target after the page starts loading, which is after
// the browser is in front.
const tabsWait = 3 * time.Second

// tabsBefore lists the page targets before an open, when the open may land
// in a browser the session launched, so the tabs it adds can be told apart
// from the ones already there. Android only: Safari's tabs cannot be closed
// from outside, and the list is not worth reading for them.
func (h *Handlers) tabsBefore(ctx context.Context, s *session) map[string]bool {
	if s.launched == "" || (s.backend != BackendUIA2 && s.backend != BackendDump) {
		return nil
	}
	seen := map[string]bool{}
	if ctxs, err := h.webContexts(ctx, s); err == nil {
		for _, c := range ctxs {
			seen[c.Socket+" "+c.TargetID] = true
		}
	}
	return seen
}

// trackOpenedTabs records the tabs an open added to the browser the session
// launched. before is nil when there is nothing to track.
func (h *Handlers) trackOpenedTabs(ctx context.Context, s *session, app string, before map[string]bool) {
	if before == nil || app != s.launched {
		return
	}
	for deadline := time.Now().Add(tabsWait); ; time.Sleep(200 * time.Millisecond) {
		ctxs, err := h.webContexts(ctx, s)
		var added []webview.Context
		for _, c := range ctxs {
			if c.TargetID != "" && !before[c.Socket+" "+c.TargetID] {
				added = append(added, c)
			}
		}
		if err == nil && len(added) > 0 {
			s.openedTabs = append(s.openedTabs, added...)
			return
		}
		// Chrome reuses a tab for a URL it already has open, and then there
		// is nothing new to wait for.
		if time.Now().After(deadline) || ctx.Err() != nil {
			return
		}
	}
}

// closeOpenedTabs closes the tabs the session opened. Best effort: the
// browser is stopped next either way, and a tab already closed by the
// caller is not a failure of the session's end.
func (h *Handlers) closeOpenedTabs(ctx context.Context, s *session) int {
	if len(s.openedTabs) == 0 {
		return 0
	}
	adb, err := h.adbFor(ctx, s)
	if err != nil {
		return 0
	}
	closed := 0
	for _, t := range s.openedTabs {
		if webview.CloseTarget(ctx, adb, t.Socket, t.TargetID) == nil {
			closed++
		}
	}
	s.openedTabs = nil
	return closed
}

func (h *Handlers) sessionStatus() *ToolsCallResult {
	open := h.openSessions()
	view := SessionView{Action: "status", Sessions: open}
	if len(open) == 0 {
		return Result("no session is open", view)
	}
	var lines []string
	for _, s := range open {
		lines = append(lines, fmt.Sprintf("%s (%s, %s)", s.Device, s.Platform, s.Driver))
	}
	return Result("open: "+strings.Join(lines, "; "), view)
}

func (h *Handlers) openSessions() []SessionInfo {
	out := []SessionInfo{}
	for _, s := range h.sessions {
		out = append(out, SessionInfo{Device: s.dev.Serial, Platform: sessionPlatform(s), Driver: string(s.backend)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Device < out[j].Device })
	return out
}

// sessionPlatform names the platform a session drives: the backend decides
// it for the built-in drivers, and a third-party driver is its own platform.
func sessionPlatform(s *session) string {
	switch s.backend {
	case BackendWDA:
		return "ios"
	case BackendUIA2, BackendDump:
		return "android"
	default:
		return string(s.backend)
	}
}

// sessionExposure names what can reach the device while this session is open,
// beyond Mobium. On a real Android phone, UiAutomator2's server and its screen
// stream listen on every interface, and on the Pixel 8 Pro both answered from
// another machine on its Wi-Fi with no credential (CHALLENGES 154). The server
// has no setting that binds it, so the exposure is named rather than hidden,
// with the one driver that opens no port. An emulator's network is behind
// its own NAT, and the other drivers listen nowhere a network reaches.
func sessionExposure(s *session) string {
	if s.backend != BackendUIA2 || s.dev == nil || s.dev.Emulator {
		return ""
	}
	return "until this session ends, the UiAutomator2 server on the phone answers on the phone's network " +
		"too — anyone on the same Wi-Fi can read its screen and drive it. --driver uiautomator opens no port, " +
		"at about 2s a screen read and without typing"
}
