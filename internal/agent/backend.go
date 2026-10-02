package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/grid"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/paths"
	"runtime"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/webview"
)

// Backend names the driver implementation to use for Android.
type Backend string

const (
	// BackendUIA2 is the UiAutomator2 server: fast, and the only one that can
	// type into a named element. Installs two APKs on first use.
	BackendUIA2 Backend = "uiautomator2"
	// BackendDump is `uiautomator dump` over adb: installs nothing, but each
	// snapshot costs a couple of seconds and it cannot type.
	BackendDump Backend = "uiautomator"
	// BackendWDA is WebDriverAgent on an iOS simulator or a real iPhone.
	BackendWDA Backend = "wda"
)

// DefaultBackend is UiAutomator2, on the same reasoning as vibium downloading
// Chrome on first run: the good experience should be what happens by default,
// and the install is a one-time cost the tool explains while it happens.
const DefaultBackend = BackendUIA2

// builtinBackends is every backend compiled into this binary. Any other name
// is looked for on PATH as `mobium-driver-<name>`, which is how a third party
// adds a platform without forking — see examples/drivers/PROTOCOL.md.
var builtinBackends = []Backend{BackendUIA2, BackendDump, BackendWDA}

// ParseBackend validates a backend name.
//
// An unrecognized name is not an error here. It is a candidate external
// driver, and whether one exists is decided when a session is opened — that is
// where PATH can be reported in the failure, and where the user finds out
// while doing something rather than while typing.
func ParseBackend(s string) (Backend, error) {
	name := Backend(strings.TrimSpace(s))
	if name == "" {
		return DefaultBackend, nil
	}
	for _, b := range builtinBackends {
		if name == b {
			return b, nil
		}
	}
	// A near miss on a built-in name is a typo, not a request for a driver
	// called "UIAutomator2". Saying so beats sending them to look on their
	// PATH for something that was never going to be there.
	for _, b := range builtinBackends {
		if strings.EqualFold(string(name), string(b)) {
			return "", mobiumerr.New(mobiumerr.InvalidArgument, "no driver is called %q — did you mean %q? "+
				"(driver names are lower case)", s, b)
		}
	}
	// The iOS driver's name before it was shortened. Unrefused, it would be
	// taken for a third-party driver and fail later, looking for
	// mobium-driver-webdriveragent on PATH — the wrong cause.
	if strings.EqualFold(string(name), "webdriveragent") {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "the iOS driver is called %q (WebDriverAgent)", BackendWDA)
	}
	if strings.ContainsAny(string(name), " /\\") {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "%q is not a driver name (want %q, %q, %q, or the name of a "+
			"driver installed as mobium-driver-<name>)", s, BackendUIA2, BackendDump, BackendWDA)
	}
	return name, nil
}

// isBuiltin reports whether a backend is compiled in.
func (b Backend) isBuiltin() bool {
	for _, known := range builtinBackends {
		if b == known {
			return true
		}
	}
	return false
}

// session is a device plus the driver bound to it, cached so a UiAutomator2
// server is started once rather than per command. This is what the daemon
// exists to hold.
type session struct {
	dev     *device.Device
	driver  mobiumdriver.Driver
	backend Backend

	// webCtx is the WebView context this session is switched into, empty for
	// the native shell; web is its attachment.
	webCtx string
	web    webview.Page
	// webApp is the app the attached page belongs to, and on iOS webAppID
	// its web inspector identifier. A page stays attached while its app
	// leaves the screen, so every action that aims at it asks again.
	webApp, webAppID string

	// insp is the iOS session's Remote Web Inspector connection, held open
	// for the life of the session. webinspectord answers only the first
	// connection promptly — every later one waits a measured 10.2 seconds
	// before its first byte — so reopening per command would put a
	// ten-second floor under listing contexts and switching between them.
	insp *webview.Inspector

	// route stops a route in progress: on Android it cancels the stepping
	// this session does itself, and on iOS, where simctl owns the timer, it
	// clears the simulated location, which is simctl's only way to stop one.
	// One route at a time per device — starting another replaces it, because
	// two things driving one position is not a state anyone can reason about.
	route context.CancelFunc

	// launched is the app app_session start launched, which the session's
	// end stops again: a browser left running keeps the pages the session
	// opened, and the next session's contexts list them. Apps the session
	// did not launch are not its to stop.
	launched string

	// openedTabs are the browser tabs app_open_url opened in the app start
	// launched, on Android, which the end closes before stopping it: a
	// browser restores its tabs on the next launch, measured with Chrome.
	// Only these — a tab the user already had is not the session's.
	openedTabs []webview.Context

	// ctxNames holds the name each listed web page was given, by the page's
	// own identity, so a page keeps its name while it is there. Numbering by
	// position renamed pages whenever a tab opened, and a name read from one
	// listing attached a different page in the next.
	ctxNames map[string]string

	// logMarks is how far app_logs has read the device log, keyed by the
	// filter it read with, as the device's clock reported it. Per filter
	// because a read narrowed to one app must not mark the rest of the
	// device as read — the same promise the WebView console keeps by
	// filtering in the page and leaving the other levels buffered.
	logMarks map[string]time.Time

	// axUndo puts back each accessibility setting this session changed, as
	// it was found; run when the session ends (app_accessibility).
	axUndo map[string]device.AXUndo

	// recording is a screen recording in progress, or nil. One per device.
	recording device.Recording

	// trace is a session trace in progress (app_trace), or nil. Ending the
	// session discards it, as it does a recording.
	trace *sessionTrace

	// fullReadTook is how long the last full read of the screen took, which
	// decides whether an action tries a light read first (lightResolve).
	fullReadTook time.Duration
}

// stopRoute ends any route this session is stepping. Safe to call when none is.
func (s *session) stopRoute() {
	if s.route != nil {
		s.route()
		s.route = nil
	}
}

// closeWeb detaches from a WebView, leaving the session on the native shell.
func (s *session) closeWeb() {
	if s.web != nil {
		s.web.Close()
		s.web = nil
	}
	s.webCtx = ""
	s.webApp, s.webAppID = "", ""
}

// close releases a backend that owns a device-side process.
func (s *session) close() {
	// Finished cleanly and kept nowhere: a recorder that is killed leaves an
	// unplayable file, and one left running goes on recording a device no
	// session holds any more.
	if s.recording != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		s.recording.Discard(ctx)
		cancel()
		s.recording = nil
	}
	s.stopRoute()
	// Before the driver closes: an undo needs the device to reach.
	s.restoreAccessibility()
	s.closeWeb()
	if s.insp != nil {
		s.insp.Close()
		s.insp = nil
	}
	if starter, ok := mobiumdriver.AsStarter(s.driver); ok {
		starter.Close()
	}
}

// healthy reports whether the cached driver can still be used. A backend that
// holds no connection — the dump backend runs adb afresh each time — is always
// healthy.
func (s *session) healthy(ctx context.Context) bool {
	h, ok := mobiumdriver.AsHealth(s.driver)
	if !ok {
		return true
	}
	return h.Healthy(ctx)
}

// sessionFor returns the cached session for a device, creating it if needed.
func (h *Handlers) sessionFor(ctx context.Context, args map[string]interface{}) (*session, error) {
	s, err := h.resolveSession(ctx, args)
	if err != nil || s == nil || s.dev == nil {
		return s, err
	}
	// A device a grid has leased to one run is refused to every other, on
	// every call: each grid run has a daemon of its own on the node, named
	// by its lease, and any other daemon here — a run that bypassed the
	// grid included — would otherwise take the device from under it.
	if holder, leased := grid.Holder(s.dev.Serial); leased && holder != paths.SessionName() {
		return nil, mobiumerr.New(mobiumerr.DeviceNotReady, "%s is leased to a grid run (%s) and belongs to it until "+
			"that run ends", s.dev.Serial, holder).
			WithRemedy("wait for the run to finish, or reach a free device through MOBIUM_GRID").
			WithDetail("holder", holder)
	}
	return s, nil
}

// resolveSession finds or opens the session a call is for, before any lease
// is consulted.
func (h *Handlers) resolveSession(ctx context.Context, args map[string]interface{}) (*session, error) {
	serial := stringArg(args, "device")
	if serial == "" {
		serial = h.defaultDevice
	}

	backend := h.backend
	if b := stringArg(args, "driver"); b != "" {
		parsed, err := ParseBackend(b)
		if err != nil {
			return nil, err
		}
		backend = parsed
	} else if open := h.openSessionFor(serial); open != nil && backend == DefaultBackend {
		// No driver named: the session already open is the context. Without
		// this, a session started with platform "ios" was
		// followed by a call with no driver, which took Android's default
		// and failed asking for "wda" — from every client, since platform is
		// sent only with start.
		if open.healthy(ctx) {
			return open, nil
		}
		backend, serial = open.backend, open.dev.Serial
	}

	if backend == BackendWDA {
		s, err := h.iosSessionFor(ctx, serial)
		return s, onOtherPlatform(ctx, serial, err, false)
	}
	if !backend.isBuiltin() {
		// Before adb: an external driver may be for a platform adb has never
		// heard of, and requiring a connected Android device to use one would
		// defeat the point.
		return h.externalSessionFor(ctx, backend, serial)
	}

	adb, dev, err := device.Select(ctx, serial)
	if err != nil {
		// Named by a device that is not Android's, with no driver named:
		// when it is an iPhone or a simulator there is one driver for it,
		// and asking for that driver by name told the caller nothing they
		// had not already said by naming the device. Only after Android's
		// lookup fails, so an Android call pays nothing for it.
		if h.iosByReference(ctx, args, serial) {
			return h.iosSessionFor(ctx, serial)
		}
		if s, ok, ierr := h.onlyIOS(ctx, args, serial, err); ok {
			return s, ierr
		}
		return nil, onOtherPlatform(ctx, serial, err, true)
	}

	// A cached session is reused only when it is both the backend asked for
	// and still alive. Emulator serials are reused across restarts, so the
	// serial matching is not evidence the device behind it is the same one.
	if s, ok := h.sessions[dev.Serial]; ok {
		if s.backend == backend && s.healthy(ctx) {
			return s, nil
		}
		h.retire(dev.Serial, s)
	}

	s := &session{dev: dev, backend: backend}
	switch backend {
	case BackendDump:
		s.driver = mobiumdriver.NewAndroid(adb)
	default:
		d := mobiumdriver.NewUIA2(adb)
		if err := d.Start(ctx, h.progress); err != nil {
			// UiAutomation belongs to one client at a time, and the dump
			// backend needs it as much as UiAutomator2 does — so switching
			// backend, which this error used to suggest, cannot help. Name
			// what holds it instead (CHALLENGES 182).
			if strings.Contains(err.Error(), "UiAutomation not connected") {
				return nil, uiAutomationHeld(ctx, adb, dev.Serial, err)
			}
			// Nor for a device that cannot be reached: the other backend
			// needs the same link. A session that never started sent the
			// call nothing, so it may be made again (device.ReachedKey).
			if e, ok := mobiumerr.As(err); ok && e.Code == mobiumerr.DeviceNotReady {
				e = e.WithDetail(device.ReachedKey, false)
				e.Retryable = true
				return nil, e
			}
			return nil, fmt.Errorf("%w\n\nTo run without the UiAutomator2 server, "+
				"use --driver uiautomator (slower, and it cannot type).", err)
		}
		s.driver = d
	}

	h.adopt(dev.Serial, s)
	return s, nil
}

// openSessionFor is the session a call naming no driver belongs to: the one
// open on the device it names, or, when it names none, the only one open.
// With several open and no device named there is no such session, and the
// call is resolved as it always was.
func (h *Handlers) openSessionFor(serial string) *session {
	if serial != "" {
		for key, s := range h.sessions {
			if key == serial || s.dev.Serial == serial {
				return s
			}
		}
		return nil
	}
	if len(h.sessions) == 1 {
		for _, s := range h.sessions {
			return s
		}
	}
	return nil
}

// externalSessionFor spawns a third-party driver and caches the session.
//
// The device string is passed through verbatim rather than resolved: only the
// driver knows what names a device on its platform, and a Roku is not on adb.
// The cache is keyed by backend and device together, so two external drivers
// can be in use at once and neither displaces a UiAutomator2 session.
func (h *Handlers) externalSessionFor(ctx context.Context, backend Backend, ref string) (*session, error) {
	path, err := mobiumdriver.FindDriver(string(backend))
	if err != nil {
		return nil, err
	}

	key := "external:" + string(backend) + ":" + ref
	if s, ok := h.sessions[key]; ok {
		if s.healthy(ctx) {
			return s, nil
		}
		h.retire(key, s)
	}

	d := mobiumdriver.NewExternal(string(backend), path, ref)
	if err := d.Start(ctx, h.progress); err != nil {
		return nil, err
	}

	name := ref
	if name == "" {
		name = string(backend)
	}
	s := &session{
		dev:     &device.Device{Serial: name, Model: d.Name()},
		driver:  d,
		backend: backend,
	}
	h.adopt(key, s)
	return s, nil
}

// closeSessions tears down every cached driver.
func (h *Handlers) closeSessions() {
	for serial, s := range h.sessions {
		s.close()
		delete(h.sessions, serial)
	}
}

// iosSessionFor resolves an iOS simulator or a real iPhone and its
// WebDriverAgent session.
func (h *Handlers) iosSessionFor(ctx context.Context, ref string) (*session, error) {
	target, err := device.SelectIOS(ctx, ref)
	if err != nil {
		return nil, err
	}
	serial := target.Serial()

	if s, ok := h.sessions[serial]; ok {
		if s.backend == BackendWDA && s.healthy(ctx) {
			return s, nil
		}
		h.retire(serial, s)
	}

	var d *mobiumdriver.WDA
	if target.Phone != nil {
		d = mobiumdriver.NewWDAPhone(target.Phone)
	} else {
		d = mobiumdriver.NewWDA(target.Sim)
	}
	if err := d.Start(ctx, h.progress); err != nil {
		return nil, err
	}

	s := &session{
		dev:     &device.Device{Serial: serial, Model: target.Model()},
		driver:  d,
		backend: BackendWDA,
	}
	h.adopt(serial, s)
	return s, nil
}

// iosByReference reports whether a call that Android could not place is for
// an iOS device it named, with no driver named to contradict that. A driver
// named explicitly is the caller's choice, and one that cannot drive the
// device is still refused (onOtherPlatform), as is a server started with
// another default.
func (h *Handlers) iosByReference(ctx context.Context, args map[string]interface{}, serial string) bool {
	return serial != "" && stringArg(args, "driver") == "" && h.backend == DefaultBackend &&
		iosKindOf(ctx, serial) != ""
}

// onlyIOS answers a call that names no device and no driver when no
// Android device is connected: the iOS device, when there is exactly one.
// Android stays the default whenever one is connected; this is only what
// was a dead end. A Mac with a simulator booted and nothing else answered
// "no Android device or emulator is running — start one", which named
// neither the device in front of the caller nor the way to it. With several
// iOS devices it refuses, naming them, as --driver wda does; with none on
// either platform, it says both. handled is false when the call is not this
// case, and the caller answers as before.
func (h *Handlers) onlyIOS(ctx context.Context, args map[string]interface{}, serial string, androidErr error) (s *session, handled bool, err error) {
	if serial != "" || stringArg(args, "driver") != "" || h.backend != DefaultBackend ||
		mobiumerr.CodeOf(androidErr) != mobiumerr.NoDevice || runtime.GOOS != "darwin" {
		return nil, false, nil
	}
	target, err := selectIOSDevice(ctx, "")
	switch {
	case err == nil:
		s, serr := h.iosSessionFor(ctx, target.Serial())
		return s, true, serr
	case mobiumerr.CodeOf(err) == mobiumerr.NoDevice:
		return nil, true, mobiumerr.New(mobiumerr.NoDevice, "no device is connected: no Android device or emulator is "+
			"running, no iOS simulator is booted and no iPhone is connected — start an emulator (`emulator -avd "+
			"<name>`), boot a simulator (`xcrun simctl boot <udid>`), or connect a phone, then see `mobium devices`")
	default:
		// Several iOS devices, and nothing to choose between them by.
		return nil, true, err
	}
}

// selectIOSDevice is device.SelectIOS, replaceable in tests, which have no
// simulators or phones to list.
var selectIOSDevice = device.SelectIOS

// iosKindOf is iosKind, replaceable in tests, which have no simulators or
// phones to list.
var iosKindOf = iosKind

// onOtherPlatform replaces "no such device" with the remedy that works when
// the device exists and the backend is for the other platform.
//
// Each platform's lookup knows only its own devices, so an iPhone simulator's
// UDID on the default Android backend failed with "no device with serial …
// (see `mobium devices`)" — and `mobium devices` lists that very simulator,
// because it asks both platforms. Advice that sends the caller to a listing
// which contradicts the error loops rather than helps. The reverse, an
// emulator serial on the iOS backend, failed the same way.
func onOtherPlatform(ctx context.Context, serial string, err error, wasAndroid bool) error {
	if err == nil || serial == "" || mobiumerr.CodeOf(err) != mobiumerr.NoDevice {
		return err
	}
	if wasAndroid {
		if kind := iosKindOf(ctx, serial); kind != "" {
			return mobiumerr.New(mobiumerr.InvalidArgument, "%s is %s, and iOS is driven by the %s driver — "+
				"pass driver %q or platform \"ios\" (on the CLI, --driver %s), or name no driver", serial, kind, BackendWDA, BackendWDA, BackendWDA)
		}
		return err
	}
	androids, _ := device.Devices(ctx)
	for _, d := range androids {
		if d.Serial == serial {
			kind := "an Android device"
			if d.Emulator {
				kind = "an Android emulator"
			}
			return mobiumerr.New(mobiumerr.InvalidArgument, "%s is %s, which the %s driver cannot drive — "+
				"pass platform \"android\", or leave the platform and driver unset", serial, kind, BackendWDA)
		}
	}
	return err
}

// iosKind says what an iOS reference names, or "" when it names nothing.
func iosKind(ctx context.Context, ref string) string {
	sims, _ := device.Simulators(ctx)
	for _, sim := range sims {
		if sim.UDID == ref || strings.EqualFold(sim.Name, ref) {
			return "an iOS simulator (" + sim.Name + ")"
		}
	}
	phones, _ := device.Phones(ctx)
	for _, p := range phones {
		if p.Matches(ref) {
			return "an iPhone (" + p.Label() + ")"
		}
	}
	return ""
}

// cannot is the refusal for a capability the session's driver lacks: the
// driver's own reason when it gives one — a real iPhone names what only a
// simulator can do — and otherwise that the backend cannot.
func cannot(s *session, capability, what string) error {
	if err := mobiumdriver.Declined(s.driver, capability); err != nil {
		return err
	}
	return mobiumerr.New(mobiumerr.Unsupported, "the %s driver cannot %s", s.backend, what)
}

// uiAutomationHeld is the refusal for a device whose UiAutomation another
// process holds. Measured: another tool's on-device server, run from the
// shell as app_process and left running after its tool exited, kept it, and
// UiAutomator2's session and even `uiautomator dump` failed until that one
// process was stopped.
func uiAutomationHeld(ctx context.Context, adb *device.ADB, serial string, cause error) error {
	holders := adb.UIAutomationHolders(ctx)
	if len(holders) == 0 {
		return mobiumerr.New(mobiumerr.DeviceNotReady, "another client holds UiAutomation on %s, which "+
			"one client may use at a time, so nothing here can read the screen — stop any other automation "+
			"tool using this device (another automation framework, a test runner, an inspector) and try again: %w", serial, cause).
			WithRemedy("stop the other automation tool using this device")
	}
	var names []string
	for _, p := range holders {
		names = append(names, fmt.Sprintf("%s (pid %d)", p.Args, p.PID))
	}
	return mobiumerr.New(mobiumerr.DeviceNotReady, "UiAutomation on %s is held by %s — another tool's process, "+
		"and one client may use UiAutomation at a time; stop it with `adb -s %s shell kill %d`, or quit the "+
		"tool that started it: %w", serial, strings.Join(names, ", "), serial, holders[0].PID, cause).
		WithRemedy(fmt.Sprintf("adb -s %s shell kill %d", serial, holders[0].PID)).
		WithDetail("holder", holders[0].Args)
}

// retire replaces a session that is no longer usable — its device dropped
// off the network, its server died, another backend was asked for — keeping
// any trace it was recording for the session that replaces it. A trace is
// the caller's, not the session's: on a Fire TV whose Wi-Fi link dropped
// mid-recording, the trace went with the session and the stop that
// followed found none (CHALLENGES 212).
func (h *Handlers) retire(key string, s *session) {
	if s.trace != nil {
		h.heldTraces[key] = s.trace
		s.trace = nil
	}
	s.close()
	delete(h.sessions, key)
}

// adopt caches a new session, and carries on a trace a retired session on
// the same device was recording.
func (h *Handlers) adopt(key string, s *session) {
	if t, ok := h.heldTraces[key]; ok {
		s.trace = t
		delete(h.heldTraces, key)
	}
	h.sessions[key] = s
}
