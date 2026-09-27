package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
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
	BackendWDA Backend = "webdriveragent"
)

// DefaultBackend is UiAutomator2, on the same reasoning as vibium downloading
// Chrome on first run: the good experience should be what happens by default,
// and the install is a one-time cost the tool explains while it happens.
const DefaultBackend = BackendUIA2

// builtinBackends is every backend compiled into this binary. Any other name
// is looked for on PATH as `mobium-driver-<name>`, which is how a third party
// adds a platform without forking — see
// docs/decisions/0003-drivers-are-processes-not-plugins.md.
var builtinBackends = []Backend{BackendUIA2, BackendDump, BackendWDA}

// ParseBackend validates a backend name.
//
// An unrecognised name is not an error here. It is a candidate external
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
			return "", mobiumerr.New(mobiumerr.InvalidArgument, "no backend is called %q — did you mean %q? "+
				"(backend names are lower case)", s, b)
		}
	}
	if strings.ContainsAny(string(name), " /\\") {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "%q is not a backend name (want %q, %q, %q, or the name of a "+
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

	// insp is the iOS session's Remote Web Inspector connection, held open
	// for the life of the session. webinspectord answers only the first
	// connection promptly — every later one waits a measured 10.2 seconds
	// before its first byte — so reopening per command would put a
	// ten-second floor under listing contexts and switching between them.
	insp *webview.Inspector

	// route cancels a route this session is stepping itself. Only Android
	// uses it: simctl owns the timer on iOS, so there is nothing here to
	// stop. One route at a time per device — starting another replaces it,
	// because two things driving one position is not a state anyone can
	// reason about.
	route context.CancelFunc

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
	serial := stringArg(args, "device")
	if serial == "" {
		serial = h.defaultDevice
	}

	backend := h.backend
	if b := stringArg(args, "backend"); b != "" {
		parsed, err := ParseBackend(b)
		if err != nil {
			return nil, err
		}
		backend = parsed
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
		return nil, onOtherPlatform(ctx, serial, err, true)
	}

	// A cached session is reused only when it is both the backend asked for
	// and still alive. Emulator serials are reused across restarts, so the
	// serial matching is not evidence the device behind it is the same one.
	if s, ok := h.sessions[dev.Serial]; ok {
		if s.backend == backend && s.healthy(ctx) {
			return s, nil
		}
		s.close()
		delete(h.sessions, dev.Serial)
	}

	s := &session{dev: dev, backend: backend}
	switch backend {
	case BackendDump:
		s.driver = mobiumdriver.NewAndroid(adb)
	default:
		d := mobiumdriver.NewUIA2(adb)
		if err := d.Start(ctx, h.progress); err != nil {
			return nil, fmt.Errorf("%w\n\nTo run without the UiAutomator2 server, "+
				"use --backend uiautomator (slower, and it cannot type).", err)
		}
		s.driver = d
	}

	h.sessions[dev.Serial] = s
	return s, nil
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
		s.close()
		delete(h.sessions, key)
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
	h.sessions[key] = s
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
		s.close()
		delete(h.sessions, serial)
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
	h.sessions[serial] = s
	return s, nil
}

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
		if kind := iosKind(ctx, serial); kind != "" {
			return mobiumerr.New(mobiumerr.InvalidArgument, "%s is %s, and iOS is driven by the %s backend — "+
				"pass backend %q (on the CLI, --backend %s)", serial, kind, BackendWDA, BackendWDA, BackendWDA)
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
			return mobiumerr.New(mobiumerr.InvalidArgument, "%s is %s, which the %s backend cannot drive — "+
				"leave the backend unset for Android", serial, kind, BackendWDA)
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
			return "an iPhone (" + p.Name + ")"
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
	return mobiumerr.New(mobiumerr.Unsupported, "the %s backend cannot %s", s.backend, what)
}
