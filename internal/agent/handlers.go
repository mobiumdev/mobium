package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/uitree"
	"github.com/mobiumdev/mobium/internal/webview"
)

// callTimeout bounds one tool call.
//
// It has to cover the one-time setup a first call may trigger — downloading
// and installing a device-side server, launching it, waiting for it to answer
// — and still leave room for the work itself. It must therefore stay
// comfortably above every readiness timeout in internal/mobiumdriver; a guard test
// asserts that ordering, because when the two were equal the first call after
// a daemon restart died with "context deadline exceeded" having done nothing
// wrong.
const callTimeout = 240 * time.Second

// refTable is one screen's worth of refs, held in memory by the daemon.
//
// Step 1 persisted this to disk because each CLI invocation was its own
// process. The daemon outlives a command, so the table lives here instead —
// which is also where a UiAutomator2 or WebDriverAgent session will live.
type refTable struct {
	entries map[string]uitree.Locator
	// seen is what each ref was when the map was taken: what it said and
	// where it was. A ref resolves by its locator, and after the screen
	// changed that locator can find another element — home's "Login Demo"
	// and the Login screen's "Log In" share a test id in MobiumApp, and a ref
	// for the first pressed the second, reporting success.
	seen map[string]refSeen
	// web holds refs taken inside a WebView, which resolve by re-running the
	// page map rather than against a native snapshot.
	web   map[string]webRef
	lines []string
	taken time.Time
}

// Handlers executes tools. One instance is shared by the daemon and the MCP
// server; the daemon serializes access, and the mutex here covers the direct
// in-process path the CLI uses when no daemon is wanted.
type Handlers struct {
	mu sync.Mutex

	// refs are per device serial: two emulators must not share a ref table.
	refs map[string]*refTable

	// lastMaps is each device's latest native map, what map --diff compares
	// with. Unlike refs it survives an action: showing what the action did
	// is its whole purpose.
	lastMaps map[string]*lastMap

	// sessions caches one driver per device so a UiAutomator2 server is
	// started once rather than per command.
	sessions map[string]*session

	// defaultDevice pins every call to one serial when set from the command
	// line, so a --device flag survives into the daemon for that call.
	defaultDevice string

	// backend selects the Android driver implementation.
	backend Backend

	// implicitWait is how long an action retries to resolve its target
	// before failing. A field rather than a constant so tests can turn it
	// off and not pay for it.
	implicitWait time.Duration

	// settleWindow is how long an element's rectangle must hold still before
	// it is acted on, and settleTimeout bounds the wait for that. Zero
	// settleWindow disables the check.
	settleWindow  time.Duration
	settleTimeout time.Duration

	// lightAbove is how slow a session's last full read must have been for
	// an action to try a light one first; see lightReadAbove.
	lightAbove time.Duration

	// progress reports slow one-time work (downloading and installing the
	// UiAutomator2 server) so a first run does not look like a hang.
	progress func(string)

	// dialogRules are the declared answers to dialogs, by device serial —
	// kept here rather than on a session so they outlive one being
	// restarted. handled is what they answered during the current call,
	// reported in its result; Call holds mu, so there is one call at a time.
	dialogRules map[string][]dialogRule
	// netBaseline is each device's network before app_network first changed
	// it, which the end of the session puts back.
	netBaseline map[string]networkBaseline
	handled     []HandledDialog
}

// NewHandlers creates the tool layer.
func NewHandlers() *Handlers {
	return &Handlers{
		refs:          map[string]*refTable{},
		lastMaps:      map[string]*lastMap{},
		sessions:      map[string]*session{},
		dialogRules:   map[string][]dialogRule{},
		netBaseline:   map[string]networkBaseline{},
		backend:       DefaultBackend,
		implicitWait:  implicitWait,
		settleWindow:  settleWindowFromEnv(),
		lightAbove:    lightAboveFromEnv(),
		settleTimeout: settleTimeout,
	}
}

// SetBackend chooses the Android driver implementation for calls that do not
// name one.
func (h *Handlers) SetBackend(b Backend) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if b != h.backend {
		// Cached sessions belong to the old backend.
		h.closeSessions()
		h.backend = b
	}
}

// SetProgress installs a callback for slow one-time setup.
func (h *Handlers) SetProgress(fn func(string)) {
	h.mu.Lock()
	h.progress = fn
	h.mu.Unlock()
}

// Close stops every device-side server the handlers started.
func (h *Handlers) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Shutting down ends every session, so each stops what its start
	// launched, as app_session end does. Not in closeSessions: switching
	// drivers closes sessions too, and is not the end of anything.
	for _, s := range h.sessions {
		_, _ = h.stopLaunched(s)
		h.restoreNetwork(s)
	}
	h.closeSessions()
}

// settleWindowFromEnv reads MOBIUM_SETTLE_MS.
//
// The stable-bounds check costs a snapshot per action, which is nothing on
// UiAutomator2 and about two seconds on the dump backend. Somewhere between
// those a caller stops wanting it, so there has to be a way to say so — and
// an environment variable rather than a flag because the daemon, the MCP
// server and every client have to be able to agree on it.
//
// Unset means the default. "0" turns the check off.
// lightAboveFromEnv reads MOBIUM_LIGHT_READ_MS, which moves the line above
// which an action tries a light read. 0 tries it on every action, which is
// how a check holds it to the same answers on a small screen; a typo keeps
// the default.
func lightAboveFromEnv() time.Duration {
	raw, ok := os.LookupEnv("MOBIUM_LIGHT_READ_MS")
	if !ok {
		return lightReadAbove
	}
	ms, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || ms < 0 {
		return lightReadAbove
	}
	return time.Duration(ms) * time.Millisecond
}

func settleWindowFromEnv() time.Duration {
	raw, ok := os.LookupEnv("MOBIUM_SETTLE_MS")
	if !ok {
		return settleWindow
	}
	ms, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || ms < 0 {
		// A typo here would silently change how every action behaves, so it
		// is louder to keep the default than to guess at what was meant.
		return settleWindow
	}
	return time.Duration(ms) * time.Millisecond
}

// Call dispatches a tool by name.
func (h *Handlers) Call(name string, args map[string]interface{}) (*ToolsCallResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// A batch has no deadline of its own: each of its steps has the whole
	// call timeout, as it would called alone.
	ctx, cancel := context.WithCancel(context.Background())
	if name != "app_batch" {
		ctx, cancel = context.WithTimeout(ctx, callTimeout)
	}
	defer cancel()

	h.handled = nil
	// While a device has a trace running, each call on it is recorded, and
	// the screen after it (app_trace).
	if name != "app_trace" {
		if s := h.tracedSession(args); s != nil {
			t := s.trace
			id := traceBefore(t, name, args)
			res, err := h.dispatch(ctx, name, args)
			res, err = h.reportHandled(res, err)
			// A call that ended the session ended the trace with it.
			if s.trace == t {
				h.traceAfter(s, t, id, res, err)
			}
			return res, err
		}
	}
	res, err := h.dispatch(ctx, name, args)
	return h.reportHandled(res, err)
}

// dispatch runs one tool by name.
func (h *Handlers) dispatch(ctx context.Context, name string, args map[string]interface{}) (*ToolsCallResult, error) {
	switch name {
	case "app_devices":
		return h.devices(ctx)
	case "app_map":
		return h.mapScreen(ctx, args)
	case "app_tap":
		return h.tap(ctx, args)
	case "app_text":
		return h.text(ctx, args)
	case "app_screenshot":
		return h.screenshot(ctx, args)
	case "app_find":
		return h.find(ctx, args)
	case "app_type":
		return h.typeText(ctx, args, false)
	case "app_fill":
		return h.typeText(ctx, args, true)
	case "app_swipe":
		return h.swipe(ctx, args)
	case "app_long_press":
		return h.longPress(ctx, args)
	case "app_drag":
		return h.drag(ctx, args)
	case "app_contexts":
		return h.contexts(ctx, args)
	case "app_context":
		return h.switchContext(ctx, args)
	case "app_launch":
		return h.launchApp(ctx, args)
	case "app_terminate":
		return h.terminateApp(ctx, args)
	case "app_install":
		return h.installApp(ctx, args)
	case "app_upload":
		return h.upload(ctx, args)
	case "app_download":
		return h.download(ctx, args)
	case "app_open_url":
		return h.openURL(ctx, args)
	case "app_current":
		return h.currentApp(ctx, args)
	case "app_wait_for":
		return h.waitFor(ctx, args)
	case "app_scroll_to":
		return h.scrollTo(ctx, args)
	case "app_grant":
		return h.grantPermissions(ctx, args)
	case "app_revoke":
		return h.revokePermissions(ctx, args)
	case "app_reset_permissions":
		return h.resetPermissions(ctx, args)
	case "app_appearance":
		return h.appearance(ctx, args)
	case "app_accessibility":
		return h.accessibility(ctx, args)
	case "app_orientation":
		return h.orientation(ctx, args)
	case "app_screen":
		return h.screen(ctx, args)
	case "app_locale":
		return h.locale(ctx, args)
	case "app_press":
		return h.press(ctx, args)
	case "app_lock":
		return h.lock(ctx, args)
	case "app_session":
		return h.sessionTool(ctx, args)
	case "app_call":
		return h.call(ctx, args)
	case "app_sms":
		return h.sms(ctx, args)
	case "app_timezone":
		return h.timezone(ctx, args)
	case "app_location":
		return h.location(ctx, args)
	case "app_clipboard":
		return h.clipboard(ctx, args)
	case "app_alert":
		return h.alert(ctx, args)
	case "app_check":
		return h.check(ctx, args)
	case "app_zoom":
		return h.zoom(ctx, args)
	case "app_rotate":
		return h.rotate(ctx, args)
	case "app_press_tap":
		return h.pressTap(ctx, args)
	case "app_press_drag":
		return h.pressDrag(ctx, args)
	case "app_notifications":
		return h.notifications(ctx, args)
	case "app_logs":
		return h.consoleLogs(ctx, args)
	case "app_crashes":
		return h.crashes(ctx, args)
	case "app_keyboard":
		return h.keyboard(ctx, args)
	case "app_record":
		return h.record(ctx, args)
	case "app_trace":
		return h.traceTool(ctx, args)
	case "app_eval":
		return h.evalPage(ctx, args)
	case "app_cookies":
		return h.cookiesTool(ctx, args)
	case "app_storage":
		return h.storageTool(ctx, args)
	case "app_list_apps":
		return h.listApps(ctx, args)
	case "app_uninstall":
		return h.uninstallApp(ctx, args)
	case "app_clear_data":
		return h.clearData(ctx, args)
	case "app_source":
		return h.source(ctx, args)
	case "app_dialogs":
		return h.dialogs(ctx, args)
	case "app_doctor":
		return h.doctor(ctx, args)
	case "app_batch":
		return h.batch(ctx, args)
	case "app_battery":
		return h.battery(ctx, args)
	case "app_time":
		return h.deviceTime(ctx, args)
	case "app_shake":
		return h.shake(ctx, args)
	case "app_biometric":
		return h.biometric(ctx, args)
	case "app_hit_test":
		return h.hitTest(ctx, args)
	case "app_audit":
		return h.audit(ctx, args)
	case "app_network":
		return h.network(ctx, args)
	case "app_state":
		return h.appState(ctx, args)
	case "app_background":
		return h.background(ctx, args)
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown tool %q (have: %s)", name, strings.Join(ToolNames(), ", "))
	}
}

// session resolves the device a call targets and returns its cached driver.
func (h *Handlers) session(ctx context.Context, args map[string]interface{}) (*device.Device, mobiumdriver.Driver, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, nil, err
	}
	return s.dev, s.driver, nil
}

// SetDefaultDevice pins calls that do not name a device.
func (h *Handlers) SetDefaultDevice(serial string) {
	h.mu.Lock()
	h.defaultDevice = serial
	h.mu.Unlock()
}

// devices lists both platforms. Either toolchain being absent is normal — most
// machines have one — so a missing adb or Xcode is reported as a note beneath
// the devices that were found, not as a failure.
func (h *Handlers) devices(ctx context.Context) (*ToolsCallResult, error) {
	var lines, notes []string
	view := DevicesView{Devices: []DeviceView{}}

	androids, err := device.Devices(ctx)
	if err != nil {
		notes = append(notes, "Android: "+err.Error())
	}
	for _, d := range androids {
		kind := "device"
		if d.Emulator {
			kind = "emulator"
		}
		line := fmt.Sprintf("%-38s %-10s (android %s", d.Serial, d.State, kind)
		if d.Model != "" {
			line += ", model: " + d.Model
		}
		lines = append(lines, line+")")
		view.Devices = append(view.Devices, DeviceView{
			ID: d.Serial, Platform: "android", State: d.State,
			Model: d.Model, Emulator: d.Emulator,
		})
	}

	sims, err := device.Simulators(ctx)
	if err != nil {
		notes = append(notes, "iOS: "+err.Error())
	}
	for _, sim := range sims {
		lines = append(lines, fmt.Sprintf("%-38s %-10s (ios simulator, %s, %s)",
			sim.UDID, strings.ToLower(sim.State), sim.Name, sim.Runtime))
		view.Devices = append(view.Devices, DeviceView{
			ID: sim.UDID, Platform: "ios", State: strings.ToLower(sim.State),
			Model: sim.Name, Runtime: sim.Runtime, Emulator: true,
		})
	}

	// A Mac without devicectl simply lists no phones; the simulators above
	// already said whether Xcode is there at all.
	phones, _ := device.Phones(ctx)
	for _, p := range phones {
		state := "connected"
		if !p.Connected() {
			state = "offline"
		}
		line := fmt.Sprintf("%-38s %-10s (ios device, %s, %s, iOS %s", p.UDID, state,
			p.Name, p.Model, p.OSVersion)
		if p.Connected() {
			if err := p.Usable(); err != nil {
				line += " — not ready: " + err.Error()
			}
		}
		lines = append(lines, line+")")
		// The model, never the name: a phone's name is its owner's —
		// "<somebody>'s iPhone" — and this view reaches every grid user's
		// `grid status` and every client. The CLI line above keeps the name
		// for the person at this machine.
		view.Devices = append(view.Devices, DeviceView{
			ID: p.UDID, Platform: "ios", State: state,
			Model: p.Model, Runtime: "iOS " + p.OSVersion,
		})
	}
	view.Notes = notes

	if len(lines) == 0 {
		msg := "No devices found. Start an Android emulator or an iOS simulator, then try again."
		if len(notes) > 0 {
			msg += "\n\n" + strings.Join(notes, "\n")
		}
		return Result(msg, view), nil
	}
	out := strings.Join(lines, "\n")
	if len(notes) > 0 {
		out += "\n\n" + strings.Join(notes, "\n")
	}
	return Result(out, view), nil
}

func (h *Handlers) mapScreen(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	diff := boolArg(args, "diff")
	if s.web != nil {
		if diff {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "map --diff compares native maps; a WebView's map "+
				"is not kept to compare with").WithRemedy("app_context NATIVE_APP, then app_map with diff")
		}
		return h.mapWeb(ctx, s)
	}
	dev, driver := s.dev, s.driver
	tree, err := driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}

	entries := tree.Map()
	table := &refTable{entries: map[string]uitree.Locator{}, taken: time.Now()}
	view := MapView{Elements: []ElementView{}, Context: webview.NativeContext, Device: dev.Serial}
	for _, e := range entries {
		table.add(e)
		view.Elements = append(view.Elements, elementView(e))
	}
	h.refs[dev.Serial] = table
	prev := h.lastMaps[dev.Serial]
	h.lastMaps[dev.Serial] = &lastMap{elements: view.Elements, taken: table.taken}

	if diff {
		if prev == nil {
			// Compared with nothing, everything appeared: a client reading
			// only the diff still learns the screen.
			view.Diff = &MapDiffView{First: true, Added: view.Elements, Removed: []ElementView{},
				Changed: []ChangeView{}}
			whole := strings.Join(table.lines, "\n")
			if whole == "" {
				whole = "No actionable elements found"
			}
			return Result("no earlier map of this device to compare with, so here is all of it:\n"+whole, view), nil
		}
		d := diffMaps(prev.elements, view.Elements)
		d.Since = prev.taken
		view.Diff = &d
		return Result(diffText(d, len(view.Elements)), view), nil
	}
	if len(table.lines) == 0 {
		return Result("No actionable elements found", view), nil
	}
	return Result(strings.Join(table.lines, "\n"), view), nil
}

func (h *Handlers) tap(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.tapOn(ctx, s, args)
}

// tapOn is app_tap once the device is resolved. Split out so the retry
// behavior can be tested against a scripted screen without a device.
//
// A double tap is this tool with one argument set, not a tool of its own.
// Everything that makes app_tap what it is — resolving a ref against a fresh
// snapshot, reaching through a WebView, refusing a stale ref rather than
// tapping where it used to be — is identical, and the only difference is
// which driver method receives the same two coordinates. A second tool would
// have been this file again with one line changed.
func (h *Handlers) tapOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	driver := s.driver

	target := stringArg(args, "target")
	x, hasX := intArg(args, "x")
	y, hasY := intArg(args, "y")

	switch {
	case target != "" && (hasX || hasY):
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "give either target or x/y, not both")
	case hasX != hasY:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "x and y must be given together")
	case target == "" && !hasX:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_tap needs a target (\"@e5\" or \"text=Sign In\") or x and y")
	}

	// An invalid request is refused as one before any capability is asked
	// about — otherwise a backend that cannot double-tap would answer this
	// with its own limitation, which is true and beside the point.
	fingers := intArgOr(args, "fingers", 1)
	if fingers != 1 && boolArg(args, "double") {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "a double tap with several fingers is not supported — "+
			"give double or fingers, not both")
	}

	// touch is the one line that differs, resolved once so every path below
	// reads the same.
	touch := driver.Tap
	verb, action := "tapped", "tap"
	if boolArg(args, "double") {
		dt, ok := mobiumdriver.AsDoubleTapper(driver)
		if !ok {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "the %s backend cannot double-tap: it taps through "+
				"one adb call at a time, and nothing there controls the interval "+
				"between two of them — the platform reads taps 40 to 300ms apart as "+
				"one gesture and anything else as two. Switch to --driver uiautomator2",
				s.backend)
		}
		touch = dt.DoubleTap
		verb, action = "double-tapped", "double_tap"
	}

	// Several fingers at once is this tool too, for the reason a double tap
	// is: everything but the touch itself is the same. The fingers land side
	// by side about the point a one-finger tap would have used.
	if fingers != 1 {
		if fingers < 2 || fingers > 5 {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "fingers is how many touch at once, 1 to 5, got %d", fingers)
		}
		mt, ok := mobiumdriver.AsMultiToucher(driver)
		if !ok {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "the %s backend cannot put several fingers down at once — "+
				"switch to --driver uiautomator2 on Android, or use an iOS device", s.backend)
		}
		tree, err := driver.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		width := tree.Root.Bounds.X2 - tree.Root.Bounds.X1
		touch = func(ctx context.Context, x, y int) error {
			return mt.MultiTap(ctx, fingerRow(x, y, fingers, width))
		}
		verb, action = fmt.Sprintf("tapped with %d fingers", fingers), "multi_tap"
	}

	if hasX {
		// A point off the screen touches nothing, and an iPhone simulator
		// answered a tap at (50000, 50000) as done (CHALLENGES 187).
		if tree, err := driver.Snapshot(ctx); err == nil && tree.Root != nil {
			if b := tree.Root.Bounds; !b.Empty() && (x < b.X1 || y < b.Y1 || x >= b.X2 || y >= b.Y2) {
				return nil, mobiumerr.New(mobiumerr.InvalidArgument, "(%d, %d) is off the screen, which is %s in device "+
					"pixels — a touch there reaches nothing", x, y, b).
					WithRemedy("give a point on the screen, or tap an element by its ref or locator")
			}
		}
		if err := touch(ctx, x, y); err != nil {
			return nil, err
		}
		return Result(fmt.Sprintf("%s (%d, %d)", verb, x, y),
			ActionView{Action: action, X: x, Y: y}), nil
	}

	// A ref taken in a WebView resolves through the page, then is tapped
	// natively: the driver already knows how to touch a point, so none of
	// CDP's input domain is needed.
	if s.web != nil {
		// Only once the page says it can be touched: visible and in view,
		// enabled, still, and not covered — and aimed around a cover over its
		// center (webview.CheckActionable).
		wx, wy, cover, err := h.aimWeb(ctx, s, target)
		if err != nil {
			return nil, err
		}
		if err := touch(ctx, wx, wy); err != nil {
			return nil, err
		}
		note := ""
		if cover != nil {
			note = fmt.Sprintf("; its center is covered by %q, so it was touched at a clear point", cover.Label)
		}
		return Result(fmt.Sprintf("%s %s at (%d, %d) in %s%s", verb, target, wx, wy, s.webCtx, note),
			ActionView{Action: action, Target: target, X: wx, Y: wy, Context: s.webCtx, Cover: cover}), nil
	}

	// Re-snapshot and re-resolve rather than replaying the coordinates the
	// map recorded: the screen moves between calls, and a tap at a stale
	// point silently hits whatever now occupies it.
	//
	// And aim where the touch will reach it: the app may have drawn
	// something over its center (CHALLENGES 115).
	_, aim, err := h.resolveAim(ctx, s, target)
	if err != nil {
		return nil, err
	}
	tx, ty := aim.X, aim.Y
	if err := touch(ctx, tx, ty); err != nil {
		return nil, err
	}
	note, cover := aimNote(aim)
	return Result(fmt.Sprintf("%s %s at (%d, %d)%s", verb, target, tx, ty, note),
		ActionView{Action: action, Target: target, X: tx, Y: ty, Cover: cover}), nil
}

// fingerRow places n fingers side by side about (x, y), a tenth of the screen
// width apart — about 43pt on a phone, about 41dp on Android, so the fingers
// are far enough apart to be separate touches and close enough to land on
// one control. Kept inside the screen: a finger past the edge is not a touch.
func fingerRow(x, y, n, width int) []mobiumdriver.Point {
	gap := width / 10
	if gap < 1 {
		gap = 1
	}
	pts := make([]mobiumdriver.Point, n)
	start := x - gap*(n-1)/2
	for i := range pts {
		px := start + i*gap
		if px < 1 {
			px = 1
		}
		if width > 2 && px > width-2 {
			px = width - 2
		}
		pts[i] = mobiumdriver.Point{X: px, Y: y}
	}
	return pts
}

func (h *Handlers) text(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	if s.web != nil {
		if stringArg(args, "target") != "" {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "app_text with a target is not supported in a WebView yet — "+
				"call it without one for the page text")
		}
		page, err := s.web.Text(ctx)
		if err != nil {
			return nil, err
		}
		return Result(page, TextView{Text: page}), nil
	}
	dev, driver := s.dev, s.driver
	tree, err := driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}

	target := stringArg(args, "target")
	if target == "" {
		all := tree.Text()
		return Result(all, TextView{Text: all}), nil
	}

	loc, err := h.locatorFor(dev.Serial, target)
	if err != nil {
		return nil, err
	}
	node, err := pickToRead(loc, tree)
	if err != nil {
		// Android leaves out what the keyboard covers, so a label under it
		// is a plain miss there, and "run app_map again" cannot find it;
		// hiding the keyboard can.
		if kb, ok := mobiumdriver.AsKeyboard(s.driver); ok && mobiumerr.CodeOf(err) == mobiumerr.NoSuchElement && !isNearMiss(err) {
			if shown, kerr := kb.KeyboardShown(ctx); kerr == nil && shown {
				return nil, keyboardOver(loc, false)
			}
		}
		return nil, err
	}
	if err := h.staleRef(dev.Serial, target, node); err != nil {
		return nil, err
	}
	value := node.Text
	// A password field's text is the password, and this value goes straight
	// into a transcript or a log. The length survives so "did my typing land"
	// is still answerable.
	if masked, yes := uitree.Redact(node); yes {
		value = masked
	} else if value == "" {
		value = node.Label
	}
	return Result(value, TextView{Text: value}), nil
}

func (h *Handlers) find(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	dev, driver, err := h.session(ctx, args)
	if err != nil {
		return nil, err
	}
	raw := stringArg(args, "locator")
	if raw == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_find needs a locator, e.g. \"text=Sign In\"")
	}
	loc, err := uitree.ParseLocator(raw)
	if err != nil {
		return nil, err
	}
	tree, err := driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}

	// Report matches using the refs of the current screen, so a find result
	// can be handed straight to app_tap.
	entries := tree.Map()
	table := &refTable{entries: map[string]uitree.Locator{}, taken: time.Now()}
	byNode := map[*uitree.Node]uitree.Entry{}
	for _, e := range entries {
		table.add(e)
		byNode[e.Node] = e
	}
	h.refs[dev.Serial] = table

	var lines []string
	view := MapView{Elements: []ElementView{}, Context: webview.NativeContext, Device: dev.Serial}
	for _, n := range loc.Resolve(tree) {
		if e, ok := byNode[n]; ok {
			lines = append(lines, e.Line())
			view.Elements = append(view.Elements, elementView(e))
			continue
		}
		// Matched something the map filters out — a label, say. Still worth
		// reporting, but it has no ref to act on.
		lines = append(lines, fmt.Sprintf("(no ref) %s %s", strings.TrimSpace(n.Text+" "+n.Label), n.Bounds))
		view.Elements = append(view.Elements, ElementView{
			Label:  strings.TrimSpace(n.Text + " " + n.Label),
			Bounds: boundsView(n.Bounds),
		})
	}
	if len(lines) == 0 {
		return Result(fmt.Sprintf("No element matches %s", loc), view), nil
	}
	return Result(strings.Join(lines, "\n"), view), nil
}

func (h *Handlers) screenshot(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	_, driver, err := h.session(ctx, args)
	if err != nil {
		return nil, err
	}
	png, err := driver.Screenshot(ctx)
	if err != nil {
		return nil, err
	}

	path := stringArg(args, "path")
	if path == "" {
		// No path: hand the image back inline, which is what an MCP client
		// wants. The CLI always passes a path, so a 1.4MB screenshot is not
		// base64'd through the socket for every terminal capture.
		return &ToolsCallResult{Content: []Content{{
			Type:     "image",
			Data:     base64.StdEncoding.EncodeToString(png),
			MimeType: "image/png",
		}}}, nil
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(path, png, 0o644); err != nil {
		return nil, fmt.Errorf("write screenshot: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return Result(ScreenshotSavedMessage(abs, len(png)),
		ScreenshotView{Path: abs, Bytes: len(png)}), nil
}

// locatorFor turns a tool argument into a locator: "@e3" comes from the ref
// table for that device, anything else parses as a locator directly.
func (h *Handlers) locatorFor(serial, target string) (uitree.Locator, error) {
	if !strings.HasPrefix(target, "@") || len(target) < 2 {
		return uitree.ParseLocator(target)
	}
	table, ok := h.refs[serial]
	if !ok {
		return uitree.Locator{}, mobiumerr.New(mobiumerr.InvalidArgument, "no map for %s yet — run app_map first", serial)
	}
	loc, ok := table.entries[target]
	if !ok {
		return uitree.Locator{}, mobiumerr.New(mobiumerr.InvalidArgument, "unknown ref %s — %s", target, knownRefs(table))
	}
	return loc, nil
}

// refSeen is a ref's element as the map saw it.
type refSeen struct {
	name   string
	bounds uitree.Rect
}

// add records one map entry under its ref.
func (t *refTable) add(e uitree.Entry) {
	t.entries[e.Ref] = e.Locator
	t.lines = append(t.lines, e.Line())
	if t.seen == nil {
		t.seen = map[string]refSeen{}
	}
	if e.Node != nil {
		t.seen[e.Ref] = refSeen{name: uitree.Describe(e.Node), bounds: e.Node.Bounds}
	}
}

// staleRef refuses a ref whose locator now finds a different element: one
// that neither says what the mapped one said nor sits where it sat. Either
// alone is allowed — a button's words change as it counts down, and a list
// scrolls — so only both at once count as another element, which is what
// a new screen with a reused id looks like.
func (h *Handlers) staleRef(serial, ref string, node *uitree.Node) error {
	table, ok := h.refs[serial]
	if !ok || !strings.HasPrefix(ref, "@") {
		return nil
	}
	was, ok := table.seen[ref]
	if !ok {
		return nil
	}
	now := uitree.Describe(node)
	if now == was.name || nearlySameRect(was.bounds, node.Bounds) {
		return nil
	}
	return mobiumerr.New(mobiumerr.NoSuchElement, "%s was %q at %s when the map was taken, and its locator now "+
		"finds %q at %s — the screen has changed; run app_map again", ref, was.name, was.bounds, now, node.Bounds).
		WithRemedy("run app_map again, and use the ref it gives").
		WithDetail("ref", ref)
}

// nearlySameRect allows the two pixels a node nested at its parent's bounds
// can be off by (CHALLENGES 176).
func nearlySameRect(a, b uitree.Rect) bool {
	d := func(x, y int) bool { return x-y <= 2 && y-x <= 2 }
	return d(a.X1, b.X1) && d(a.Y1, b.Y1) && d(a.X2, b.X2) && d(a.Y2, b.Y2)
}

func knownRefs(t *refTable) string {
	if len(t.entries) == 0 {
		return "the last map found no elements"
	}
	refs := make([]string, 0, len(t.entries))
	for r := range t.entries {
		refs = append(refs, r)
	}
	sort.Slice(refs, func(i, j int) bool {
		if len(refs[i]) != len(refs[j]) {
			return len(refs[i]) < len(refs[j])
		}
		return refs[i] < refs[j]
	})
	if len(refs) > 12 {
		return fmt.Sprintf("the last map has %d refs, %s through %s",
			len(refs), refs[0], refs[len(refs)-1])
	}
	return "the last map has " + strings.Join(refs, " ")
}

// matchedNothing reports a locator that found nothing on screen, as opposed to
// one that matched too much. The two want opposite responses: nothing found
// may be below the fold and worth scrolling for, while an ambiguous match is
// already on screen twice and scrolling can only make it worse. It was a type
// of its own here until docs/decisions/0005; it is the code now, so a client
// can tell the two apart as well.
func matchedNothing(err error) bool {
	if isNearMiss(err) {
		return false
	}
	switch mobiumerr.CodeOf(err) {
	case mobiumerr.NoSuchElement, mobiumerr.ElementNotReachable:
		return true
	}
	return false
}

// The checks an action makes before it touches anything, named as a
// WebView's checks are and as Playwright and Vibium name them.
const (
	checkVisible        = "visible"
	checkEnabled        = "enabled"
	checkStable         = "stable"
	checkReceivesEvents = "receivesEvents"
	checkEditable       = "editable"
)

// failedCheck is the refusal for a target that failed one of those checks,
// in one shape on native screens and in WebViews: "X failed check C: reason",
// then what to do about it. The check and the reason are in details as well,
// so a client decides by them rather than by the wording. A WebView's
// refusals had this shape from CHALLENGES 118; native ones each had their
// own until 2026-09-28, and the cover's check was spelled receives_events.
func failedCheck(code mobiumerr.Code, target any, check, reason, advice string) *mobiumerr.Error {
	msg := fmt.Sprintf("%s failed check %s: %s", target, check, reason)
	if advice != "" {
		msg += " — " + advice
	}
	return mobiumerr.New(code, "%s", msg).
		WithDetail("check", check).
		WithDetail("reason", reason)
}

// dialogOver is the refusal for a target while a dialog is up. The remedy
// works whatever the dialog says, which matters in a tool that pins apps to
// other languages: answer it, by app_alert or by one of its buttons.
//
// covered says the target was found underneath (iOS keeps it in the tree).
// Otherwise nothing matched at all, and whether the caller meant something
// behind the dialog or misnamed one of its buttons cannot be told from here —
// `label=CANCEL` for a button whose caption is its text read as "under the
// dialog" until this said both.
func dialogOver(dialog string, loc uitree.Locator, covered bool) error {
	dialog = strings.TrimSpace(strings.SplitN(dialog, "\n", 2)[0])
	if covered {
		return failedCheck(mobiumerr.DeviceNotReady, loc, checkReceivesEvents,
			fmt.Sprintf("a dialog is over the app — %q — and it is underneath", dialog), "answer the dialog first").
			WithRemedy("app_alert with accept or dismiss, or tap one of the dialog's buttons from app_map").
			WithDetail("locator", loc.String()).
			WithDetail("dialog", dialog)
	}
	msg := "a dialog is over the app — %q — and nothing on it matches %s; if the target is behind it, " +
		"answer the dialog first, and if it is one of the dialog's buttons, take its ref from app_map"
	return mobiumerr.New(mobiumerr.DeviceNotReady, msg, dialog, loc).
		WithRemedy("app_map for the dialog's buttons, or app_alert with accept or dismiss").
		WithDetail("locator", loc.String()).
		WithDetail("dialog", dialog)
}

// appDialogOver is dialogOver for a dialog the platform's alert endpoint does
// not know, an app's own. app_alert's accept and dismiss press the buttons
// the platform picks, and this dialog has none it picks, so the remedy names
// only what works: its buttons, by ref or by a rule.
func appDialogOver(dialog string, loc uitree.Locator) error {
	dialog = strings.TrimSpace(strings.SplitN(dialog, "\n", 2)[0])
	msg := "a dialog is over the app — %q — and nothing on it matches %s; if the target is behind it, " +
		"answer the dialog first, and if it is one of the dialog's buttons, take its ref from app_map"
	return mobiumerr.New(mobiumerr.DeviceNotReady, msg, dialog, loc).
		WithRemedy("tap one of the dialog's buttons from app_map, or declare an answer with app_dialogs").
		WithDetail("locator", loc.String()).
		WithDetail("dialog", dialog)
}

// hideKeyboard is the remedy for a target the keyboard covers, in both
// spellings. An iPhone's keyboard has no key that hides it, so enter is named
// too; that is what `app_keyboard` itself says when asked to hide one.
const hideKeyboard = "`mobium keyboard --hide` (app_keyboard with hide); on an iPhone, which has no " +
	"hide key, `mobium keyboard --key enter` (app_keyboard with key \"enter\")"

// keyboardOver is the refusal for a target the keyboard is over. covered
// says it was found underneath, as on iOS; otherwise it was not found at all
// while the keyboard was up — which is how Android shows the same thing, since
// UiAutomator2 leaves out what the keyboard covers — and the keyboard is only
// the likely reason, so it is said as one.
func keyboardOver(loc uitree.Locator, covered bool) error {
	remedy := "hide the keyboard: app_keyboard with hide, or with key \"enter\" on an iPhone"
	if covered {
		return failedCheck(mobiumerr.DeviceNotReady, loc, checkReceivesEvents, "the keyboard is over it",
			"hide it first: "+hideKeyboard).
			WithRemedy(remedy).
			WithDetail("locator", loc.String())
	}
	msg := "no element matches %s, and the keyboard is up — it may be covering it; hide it and try again: " +
		hideKeyboard
	return mobiumerr.New(mobiumerr.NoSuchElement, msg, loc).
		WithRemedy("hide the keyboard: app_keyboard with hide, or with key \"enter\" on an iPhone").
		WithDetail("locator", loc.String())
}

// nonEditableRoles are the roles that are certainly not a text field. Typing
// into one was passed to the server, which answered UiAutomator2's "invalid
// element state: Cannot set the element to 'hello'" — measured on a Pixel 8
// Pro, typing into a button. Playwright and Vibium refuse before trying.
var nonEditableRoles = []string{"button", "checkbox", "radio", "switch", "link", "image", "text", "tab"}

// notEditable names the role that makes a node certainly not a text field, or
// "" when it is one or might be. Only certainty refuses: a custom input view
// whose class no table knows is let through for the platform to try, because
// refusing a real field would be worse than one server error. An input or a
// password role wins over everything else, since AutoCompleteTextView is an
// input whose class also ends in TextView.
func notEditable(n *uitree.Node) string {
	if n.Password || uitree.HasRole(n, "input") {
		return ""
	}
	for _, r := range nonEditableRoles {
		// By class, not by clickability: a clickable custom view counts as a
		// button to a locator, and may be exactly the text field it names.
		if uitree.HasClassRole(n, r) {
			return r
		}
	}
	return ""
}

// notEnabled is the refusal for a target that stayed disabled. It is not a
// miss, so nothing scrolls for it, and its remedy works: wait for the app to
// enable it, or do what enables it.
func notEnabled(loc uitree.Locator, waited time.Duration) error {
	return failedCheck(mobiumerr.Timeout, loc, checkEnabled, fmt.Sprintf("it is disabled, and stayed disabled for %s", waited),
		"a disabled control ignores input; do what enables it, or wait for it with app_wait_for and condition \"enabled\"").
		WithRemedy("app_wait_for with condition \"enabled\", or do what enables it first").
		WithDetail("locator", loc.String())
}

// nearMiss is the locator that finds exactly one element by the same words
// under another of text, label and testid, when loc finds none. map prints a
// node's text when it has any, so a field shown as "URL (input)" — its
// placeholder — has no label "URL"; NetNewsWire's Add Feed sheet was refused
// for label=URL, saying the keyboard might be covering a field at the top of
// the screen. A remedy that names the locator that works is one that can.
func nearMiss(loc uitree.Locator, tree *uitree.Tree) *uitree.Locator {
	kinds := []uitree.Kind{uitree.KindText, uitree.KindLabel, uitree.KindTestID}
	if !slices.Contains(kinds, loc.Kind) || loc.Value == "" {
		return nil
	}
	for _, k := range kinds {
		if k == loc.Kind {
			continue
		}
		alt := loc
		alt.Kind = k
		if len(alt.Resolve(tree)) == 1 {
			return &alt
		}
	}
	return nil
}

// isNearMiss reports a miss that nearMiss explained: the element is on
// screen under other words, so neither scrolling nor the keyboard is why.
func isNearMiss(err error) bool {
	var e *mobiumerr.Error
	if !errors.As(err, &e) {
		return false
	}
	_, ok := e.Details["near_miss"]
	return ok
}

// pickOne resolves a locator to exactly one node, refusing an ambiguous match
// rather than guessing which element the caller meant.
func pickOne(loc uitree.Locator, tree *uitree.Tree) (*uitree.Node, error) {
	matches := loc.Resolve(tree)
	switch len(matches) {
	case 0:
		if alt := nearMiss(loc, tree); alt != nil {
			return nil, mobiumerr.New(mobiumerr.NoSuchElement, "no element matches %s, but %s does: those words are "+
				"its %s, not its %s — map prints an element's text when it has any, and a field's placeholder is its text",
				loc, alt, alt.Kind, loc.Kind).
				WithRemedy("use "+alt.String()+", or the element's ref from app_map").
				WithDetail("locator", loc.String()).
				WithDetail("near_miss", alt.String())
		}
		return nil, mobiumerr.New(mobiumerr.NoSuchElement, "no element matches %s on the current screen — "+
			"the screen may have changed, run app_map again", loc).
			WithRemedy("run app_map again, or app_scroll_to if it may be off screen").
			WithDetail("locator", loc.String())
	case 1:
		// An element with no bounds is off screen, which is the same answer
		// as "not here yet" and wants the same response: scroll. A real
		// iPhone reports an off-screen table row's button at 0,0 with no
		// size while the cell around it keeps its real frame, so refusing
		// here stopped scroll-to before its first swipe — iPhone 15 Plus,
		// iOS 26.6.2, Settings > General.
		// Under a dialog is not reachable either, and not something to scroll
		// for: the tap lands on the dialog. iOS keeps the covered screen in
		// the tree, so this is the only way a locator finds out.
		if d := tree.Dialog(); d != nil && !matches[0].Within(d) {
			return nil, dialogOver(d.Label, loc, true)
		}
		// The keyboard is the same case one layer over: on iOS what it covers
		// stays in the tree, marked not visible, and a locator resolved it —
		// so a tap landed on a key and reported success. Refused only where
		// scrolling cannot help: a target its scroll container is not showing
		// is brought into view instead, which is what a form that moves out
		// of the keyboard's way relies on — MobiumApp's Log In, below a
		// scroll view shrunk above the keyboard, was refused until this.
		if k := tree.Keyboard(); k != nil && !matches[0].Within(k) && k.Bounds.Covers(matches[0]) {
			if c, v := viewOf(tree, matches[0]); c == nil || encloses(v, matches[0].Bounds) {
				return nil, keyboardOver(loc, true)
			}
		}
		if matches[0].Bounds.Empty() {
			return nil, failedCheck(mobiumerr.ElementNotReachable, loc, checkVisible,
				"it is in the hierarchy but not on screen", "app_scroll_to with a direction brings it into view").
				WithRemedy("app_scroll_to with a direction").
				WithDetail("locator", loc.String())
		}
		return matches[0], nil
	default:
		hint := "use a ref from app_map"
		if loc.Kind != uitree.KindRole && loc.Role == "" {
			hint = `narrow it by appending ",role=button" (or whichever role), or ` + hint
		}
		return nil, mobiumerr.New(mobiumerr.AmbiguousLocator, "%s matches %d elements — %s", loc, len(matches), hint).
			WithRemedy(hint).
			WithDetail("locator", loc.String()).
			WithDetail("matches", len(matches))
	}
}

// pickToRead resolves a locator to exactly one node for reading it. What a
// touch must be refused for — a dialog or the keyboard over it, no bounds on
// screen — does not stop a read: the text is in the hierarchy either way.
// Reading the label under an iPhone's number pad was refused as "the
// keyboard is over it" while the text was right there (CHALLENGES 184).
func pickToRead(loc uitree.Locator, tree *uitree.Tree) (*uitree.Node, error) {
	if matches := loc.Resolve(tree); len(matches) == 1 {
		return matches[0], nil
	}
	// None, or several: pickOne says which, with its remedies.
	return pickOne(loc, tree)
}

func stringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// intArg reads a numeric argument. JSON numbers arrive as float64 over the
// wire and as int from the CLI, so both are accepted.
func intArg(args map[string]interface{}, key string) (int, bool) {
	switch v := args[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	}
	return 0, false
}

// floatArg reads a coordinate. JSON numbers arrive as float64 over the wire,
// as int from a CLI flag when the value happens to be whole, and as a string
// from a client that hand-writes its JSON — .NET and Java both do.
func floatArg(args map[string]interface{}, key string) (float64, error) {
	switch v := args[key].(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, mobiumerr.New(mobiumerr.InvalidArgument, "%s must be a number, got %q", key, v)
		}
		return f, nil
	}
	return 0, mobiumerr.New(mobiumerr.InvalidArgument, "%s must be a number", key)
}

// resolveNode re-snapshots and resolves a target to exactly one node. Every
// action goes through this: acting on a node from an older snapshot is how a
// gesture lands on whatever has since moved into its place.
//
// It retries for h.implicitWait, because the two ways resolution fails are
// both usually transient. "No match" is a screen that has not finished
// arriving. "Matches 2 elements" is more surprising, but during a transition
// the outgoing and incoming screens are genuinely both in the hierarchy, and
// a locator that is unique on either one matches twice for those few frames.
// Retrying both is what lets a caller tap straight after an action instead of
// sleeping first; the cost is that a locator that really is ambiguous takes
// the full implicit wait before saying so.
//
// An unresolvable ref is not retried: that is a mistake in the call, not a
// screen that has yet to settle.
func (h *Handlers) resolveNode(ctx context.Context, s *session, target string) (*uitree.Node, *uitree.Tree, error) {
	// A dialog in the way is refused below; a declared rule for it answers
	// it instead, and the target is resolved again. Bounded, because a rule
	// whose button raises another dialog must not loop forever.
	for i := 0; ; i++ {
		n, t, err := h.resolveNodeOnce(ctx, s, target)
		if err == nil {
			if kerr := h.keyboardOverTarget(ctx, s, n, t, target); kerr != nil {
				return nil, nil, kerr
			}
			if cerr := h.waitOutClipboardPreview(ctx, s, n, target); cerr != nil {
				return nil, nil, cerr
			}
			return n, t, nil
		}
		if i >= maxDialogsPerCall || !dialogInTheWay(err) {
			return n, t, err
		}
		handled, herr := h.answerByRule(ctx, s)
		if herr != nil {
			return nil, nil, herr
		}
		if !handled {
			return nil, nil, err
		}
	}
}

// keyboardOverTarget refuses a target the Android keyboard covers. The
// keyboard is another window there, missing from the tree an action resolves
// against, and what it covers depends on its mode: the full keyboard, or with
// a hardware keyboard attached only Gboard's floating toolbar at the left
// edge. Its touchable region says which, and costs a dumpsys, so it is asked
// only while something on screen has focus. iOS is answered in pickOne, from
// the keyboard node in its own tree.
func (h *Handlers) keyboardOverTarget(ctx context.Context, s *session, n *uitree.Node, t *uitree.Tree, target string) error {
	kr, ok := mobiumdriver.AsKeyboardRegioner(s.driver)
	if !ok || n == nil || t == nil {
		return nil
	}
	focused := false
	t.Walk(func(x *uitree.Node) bool {
		if x.Focused {
			focused = true
		}
		return !focused
	})
	if !focused {
		return nil
	}
	regions, err := kr.KeyboardRegions(ctx)
	if err != nil {
		return nil // not knowing is not a reason to refuse
	}
	for _, r := range regions {
		if r.Covers(n) {
			loc, lerr := h.locatorFor(s.dev.Serial, target)
			if lerr != nil {
				loc = uitree.Locator{Kind: uitree.KindText, Value: target}
			}
			return keyboardOver(loc, true)
		}
	}
	return nil
}

// clipboardPreviewPoll is how often a target under the clipboard's preview
// looks again to see whether it has gone, and clipboardPreviewTimeout how
// long it waits: its own budget, like settleTimeout's, because the preview
// stays about seven seconds and an action's implicit wait is two. A
// variable so a test can shorten it.
const clipboardPreviewPoll = 300 * time.Millisecond

var clipboardPreviewTimeout = 10 * time.Second

// waitOutClipboardPreview waits, up to clipboardPreviewTimeout, for Android's
// clipboard preview to leave a target it covers. It is another window, so
// the tree says nothing of it, and a tap under it lands on it — on its share
// chip, Quick Share opened. It goes by itself in about seven seconds and
// nothing but time closes it, so this waits as for a target still moving,
// rather than refusing at once as for the keyboard, which stays until
// hidden. CHALLENGES 113.
func (h *Handlers) waitOutClipboardPreview(ctx context.Context, s *session, n *uitree.Node, target string) error {
	cp, ok := mobiumdriver.AsClipboardPreviewer(s.driver)
	if !ok || n == nil {
		return nil
	}
	deadline := time.Now().Add(clipboardPreviewTimeout)
	for {
		regions, err := cp.ClipboardPreviewRegions(ctx)
		if err != nil {
			return nil // not knowing is not a reason to refuse
		}
		covered := false
		for _, r := range regions {
			if r.Covers(n) {
				covered = true
			}
		}
		if !covered {
			return nil
		}
		if !time.Now().Before(deadline) {
			loc, lerr := h.locatorFor(s.dev.Serial, target)
			if lerr != nil {
				loc = uitree.Locator{Kind: uitree.KindText, Value: target}
			}
			return failedCheck(mobiumerr.DeviceNotReady, loc, checkReceivesEvents,
				fmt.Sprintf("Android's clipboard preview is over it, and was still there after %s", clipboardPreviewTimeout),
				"it closes by itself a few seconds after the clipboard is written, and back does not close it").
				WithRemedy("wait a few seconds and try again").
				WithDetail("locator", loc.String())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(clipboardPreviewPoll):
		}
	}
}

// resolveNodeOnce is resolveNode without the dialog rules.
func (h *Handlers) resolveNodeOnce(ctx context.Context, s *session, target string) (*uitree.Node, *uitree.Tree, error) {
	loc, err := h.locatorFor(s.dev.Serial, target)
	if err != nil {
		return nil, nil, err
	}

	var node *uitree.Node
	var tree *uitree.Tree
	// resolveErr is the last reason the locator did not resolve, kept so a
	// timeout reports it rather than a generic one.
	var resolveErr error
	// readTook is how long the last read of the screen took, which settle
	// counts toward its window.
	var readTook time.Duration

	if n, t, took, ok := h.lightResolve(ctx, s, loc, target); ok {
		return h.settle(ctx, s, loc, n, t, took)
	}

	err = pollUntil(ctx, h.implicitWait, func(ctx context.Context) (bool, error) {
		readAt := time.Now()
		t, err := s.driver.Snapshot(ctx)
		readTook = time.Since(readAt)
		if err == nil {
			s.fullReadTook = readTook
		}
		if err != nil {
			// Not fatal. UiAutomator2 fails to read the hierarchy while the
			// screen is animating — "Cannot set AccessibilityNodeInfo's field
			// 'mSealed' to 'true'" — and reproducibly succeeds a moment
			// later. Treating a snapshot error as final turned a hiccup into
			// a failed action; the reason is kept so a real outage still says
			// what it was.
			resolveErr = err
			return false, nil
		}
		tree = t
		node, resolveErr = pickOne(loc, t)
		// Not waited for: a ref's element does not come back once its
		// screen has gone.
		if resolveErr == nil {
			if err := h.staleRef(s.dev.Serial, target, node); err != nil {
				return false, err
			}
		}
		// Enabled is part of being actionable, as it is in Playwright and
		// Vibium: a disabled control ignores a tap, which then reports
		// success for nothing. Waited for within the same budget, since a
		// button the app is about to enable is the ordinary case.
		if resolveErr == nil && !node.Enabled {
			resolveErr = notEnabled(loc, h.implicitWait)
			return false, nil
		}
		return resolveErr == nil, nil
	})
	switch {
	case err != nil && !errors.Is(err, errPollTimeout):
		return nil, nil, err

	case err != nil:
		// Nothing matched. It may simply be below the fold, so scrolling is
		// worth a try — but only for a miss: a locator that already matches
		// twice is on screen twice, and scrolling can only make it worse
		// while leaving the list somewhere else.
		if !matchedNothing(resolveErr) {
			return nil, nil, resolveErr
		}
		// On Android, and under SpringBoard's prompts on iOS, the hierarchy
		// is the dialog's window alone, so what it covers is simply not
		// found — and "run app_map again", or a scroll, is the wrong answer
		// to that. One question, asked only on a miss: is something asking?
		if a, ok := mobiumdriver.AsAlerts(s.driver); ok {
			if text, aerr := a.AlertText(ctx); aerr == nil {
				return nil, nil, dialogOver(text, loc, false)
			}
		}
		// An app's own dialog, which the platform's alert endpoint does not
		// know: a Jetpack Compose dialog is a window of its own that the
		// hierarchy holds alone (CHALLENGES 177).
		if text := appDialogText(tree); text != "" {
			return nil, nil, appDialogOver(text, loc)
		}

	default:
		// It resolved, which is not the same as being reachable. A row
		// scrolled past the bottom of its list is in the hierarchy with real
		// bounds, and tapping its center would touch a point off screen.
		//
		// The container that matters is the one the element is *inside*. An
		// element with no scrollable ancestor cannot be scrolled to, however
		// many scrollables the screen has elsewhere.
		container := scrollContainerOf(node)
		// Nor is being inside a container that is on screen the same as being
		// on screen. A link in Wikipedia's feed on an iPhone 15 Plus, its
		// center at y=2833 on a screen 2796 tall, had no scrolling ancestor
		// in the tree, and was tapped there and reported done (CHALLENGES
		// 190). It was the hidden rest of an extract its card clips, which
		// no scroll brings into view — swiping for it ran a call out of
		// time — so with nothing around it that scrolls, it is refused.
		if on, _ := centerOnScreen(tree, node); !on && container == nil {
			return nil, nil, failedCheck(mobiumerr.ElementNotReachable, loc, checkVisible,
				fmt.Sprintf("its center is off the screen, at %d,%d, and nothing it is inside scrolls",
					(node.Bounds.X1+node.Bounds.X2)/2, (node.Bounds.Y1+node.Bounds.Y2)/2),
				"scroll the screen to it with app_scroll_to or app_swipe, then act on it").
				WithRemedy("app_scroll_to with a direction, or app_swipe, then act on it").
				WithDetail("locator", loc.String())
		}
		if container == nil || encloses(tree.Viewport(container), node.Bounds) {
			return h.settle(ctx, s, loc, node, tree, readTook)
		}
	}

	// Scrolling changes the screen on the way to succeeding or failing: a tap
	// for something that exists nowhere leaves the list scrolled. Bounded by
	// maxScrolls, and skipped on a screen with nothing scrollable, so an
	// ordinary miss stays cheap.
	n, t, _, serr := h.scrollIntoView(ctx, s, loc, "down")
	if serr != nil {
		if resolveErr != nil {
			// Android leaves out what the keyboard covers, so a target under
			// it is a plain miss there; say the keyboard is up when it is.
			if kb, ok := mobiumdriver.AsKeyboard(s.driver); ok && mobiumerr.CodeOf(resolveErr) == mobiumerr.NoSuchElement && !isNearMiss(resolveErr) {
				if shown, kerr := kb.KeyboardShown(ctx); kerr == nil && shown {
					return nil, nil, keyboardOver(loc, false)
				}
			}
			return nil, nil, resolveErr
		}
		return nil, nil, serr
	}
	// A list that has just been scrolled is the most likely thing on any
	// screen to still be moving, so it waits the whole window.
	return h.settle(ctx, s, loc, n, t, 0)
}

// settle waits until the element's rectangle stops moving.
//
// Resolving an element and finding it enabled says nothing about whether it
// is where it will be in a moment. A row sliding in, a sheet still animating
// up, a list finishing a fling — all of them resolve cleanly and report
// bounds that are already stale by the time the tap lands. This is the last
// piece of what Playwright calls actionability, and the one Mobium was
// missing.
//
// It costs one extra snapshot and one settleWindow per action: about 140ms on
// UiAutomator2, where a snapshot is 0.04s, and about two seconds on the dump
// backend, where a snapshot is 1.96s. That is the price of not tapping a
// moving target, and h.settleWindow of zero turns it off for a caller who
// would rather have the speed.
//
// The window is the time between two readings, and a reading that takes
// longer than the window already spans it: an iPhone's read of even a small
// screen took 167-194ms, so the two were 100ms further apart than asked, for
// nothing. lastRead, how long the reading that found node took, is counted
// toward the window, and only the rest is waited. And where the driver can
// read the one element instead of the screen — WebDriverAgent, by a test id
// or a label unique on screen — the second reading is that, and only an
// exact match is taken from it: anything else reads the screen as before.
func (h *Handlers) settle(ctx context.Context, s *session, loc uitree.Locator,
	node *uitree.Node, tree *uitree.Tree, lastRead time.Duration) (*uitree.Node, *uitree.Tree, error) {

	if h.settleWindow <= 0 {
		return node, tree, nil
	}
	deadline := time.Now().Add(h.settleTimeout)
	was := node.Bounds

	wait := h.settleWindow - lastRead
	if b, ok := mobiumdriver.AsElementBounder(s.driver); ok {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(wait):
			}
		}
		readAt := time.Now()
		r, read, err := b.ElementBounds(ctx, node, tree)
		if err == nil && read && r == was {
			return node, tree, nil
		}
		// Moved, or not readable that way: the screen decides, as before.
		wait = h.settleWindow - time.Since(readAt)
	}

	for {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(wait):
			}
		}
		readAt := time.Now()
		t, err := s.driver.Snapshot(ctx)
		wait = h.settleWindow - time.Since(readAt)
		if err != nil {
			// Same hiccup as in resolveNode, and settling is more exposed to
			// it: this is the snapshot taken *because* something might be
			// moving, which is exactly when the hierarchy is unreadable.
			if time.Now().After(deadline) {
				return nil, nil, err
			}
			continue
		}
		n, perr := pickOne(loc, t)
		if perr != nil {
			// It resolved a moment ago and does not now. Something is still
			// changing, and the resolution error is the honest report.
			return nil, nil, perr
		}
		if n.Bounds == was {
			return n, t, nil
		}
		if time.Now().After(deadline) {
			return nil, nil, failedCheck(mobiumerr.Timeout, loc, checkStable,
				fmt.Sprintf("it is still moving after %s — it was at %s and is now at %s", h.settleTimeout, was, n.Bounds),
				"something on this screen animates continuously, so act on it with app_tap x/y if that is expected").
				WithDetail("locator", loc.String())
		}
		was = n.Bounds
	}
}

// lightReadAbove is how slow a session's last full read must have been for
// an action to try a light one first. A light read and the one element's
// visibility cost more than a small screen's full read — 26ms and 29ms
// against 42ms for MobiumApp's Layout Demo on a simulator — and far less
// than a large one's: 110ms and 124ms against 426ms for Settings, and on an
// iPhone 15 Plus 0.29s against 1.81s.
const lightReadAbove = 250 * time.Millisecond

// lightResolve resolves a locator on a read without visible, and answers
// only when that read decides the action exactly as a full one would: no
// dialog or keyboard on screen, one enabled match inside its scroll
// container, nothing drawn over it, and the element itself visible when
// asked alone. A light read takes every element as shown, so it can only
// add matches and covers, never remove them; with none added, and the one
// element confirmed, the full read would say the same. Anything else
// returns false, and the screen is read in full as before.
func (h *Handlers) lightResolve(ctx context.Context, s *session, loc uitree.Locator,
	target string) (*uitree.Node, *uitree.Tree, time.Duration, bool) {

	lr, ok := mobiumdriver.AsLightReader(s.driver)
	if !ok || s.fullReadTook < h.lightAbove {
		return nil, nil, 0, false
	}
	readAt := time.Now()
	t, ok, err := lr.LightSnapshot(ctx)
	took := time.Since(readAt)
	if err != nil || !ok || hasDialogOrKeyboard(t) {
		return nil, nil, 0, false
	}
	n, err := pickOne(loc, t)
	if err != nil || !n.Enabled || h.staleRef(s.dev.Serial, target, n) != nil {
		return nil, nil, 0, false
	}
	if c, v := viewOf(t, n); c != nil && !encloses(v, n.Bounds) {
		return nil, nil, 0, false
	}
	if on, _ := centerOnScreen(t, n); !on {
		return nil, nil, 0, false
	}
	if aim := t.AimAt(n); aim.Moved || aim.Blocker != nil || aim.Over != nil {
		return nil, nil, 0, false
	}
	if visible, asked, err := lr.ElementVisible(ctx, n, t); err != nil || !asked || !visible {
		return nil, nil, 0, false
	}
	return n, t, took, true
}

// centerOnScreen says whether a node's center is on the screen a tree was
// read from, and if not, whether it lies above it. The screen is the root's
// rectangle, or its first child's where the root has none, as an iOS
// hierarchy's does; with neither known, a node counts as on it.
func centerOnScreen(t *uitree.Tree, n *uitree.Node) (on, above bool) {
	if t == nil || t.Root == nil {
		return true, false
	}
	screen := t.Root.Bounds
	if screen.Empty() && len(t.Root.Children) > 0 {
		screen = t.Root.Children[0].Bounds
	}
	if screen.Empty() {
		return true, false
	}
	x, y := (n.Bounds.X1+n.Bounds.X2)/2, (n.Bounds.Y1+n.Bounds.Y2)/2
	if x >= screen.X1 && x < screen.X2 && y >= screen.Y1 && y < screen.Y2 {
		return true, false
	}
	return false, y < screen.Y1
}

// hasDialogOrKeyboard says whether a read holds anything whose visibility
// the action's own checks turn on.
func hasDialogOrKeyboard(t *uitree.Tree) bool {
	found := false
	t.Walk(func(n *uitree.Node) bool {
		switch n.Class {
		case "XCUIElementTypeAlert", "XCUIElementTypeSheet", "XCUIElementTypeKeyboard":
			found = true
		}
		return !found
	})
	return found
}

// typeText puts text into a specific element: after what it holds for
// app_type, in place of it for app_fill.
func (h *Handlers) typeText(ctx context.Context, args map[string]interface{}, replace bool) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.typeTextOn(ctx, s, args, replace)
}

// typeTextOn is app_type, or app_fill with replace, once the device is
// resolved. The two differ only in whether the field is cleared first, so
// they are one implementation, and every refusal is the same for both.
func (h *Handlers) typeTextOn(ctx context.Context, s *session, args map[string]interface{}, replace bool) (*ToolsCallResult, error) {
	// Inside a WebView the page takes the text itself (webType), so no
	// native text entry is needed — and the native ref table is the wrong
	// place to look a web ref up: it answered "unknown ref @e1 — the last map
	// found no elements" for a field the page's map had just listed.
	tool := "app_type"
	if replace {
		tool = "app_fill"
	}
	if s.web != nil {
		target := stringArg(args, "target")
		text, hasText := args["text"].(string)
		if target == "" || !hasText {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s needs a target (a @ref from app_map) and text", tool)
		}
		return h.webType(ctx, s, target, text, replace)
	}
	typer, ok := mobiumdriver.AsTextEntry(s.driver)
	if !ok {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "the %s backend cannot type into an element — "+
			"use the uiautomator2 backend (`--driver uiautomator2`, the default)", s.backend)
	}

	target := stringArg(args, "target")
	if target == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s needs a target (\"@e3\" or \"testid=search\")", tool)
	}
	text, hasText := args["text"].(string)
	if !hasText {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s needs text (pass \"\" to clear the field)", tool)
	}

	node, _, err := h.resolveNode(ctx, s, target)
	if err != nil {
		return nil, err
	}
	if role := notEditable(node); role != "" {
		return nil, failedCheck(mobiumerr.InvalidArgument, target, checkEditable,
			fmt.Sprintf("it is a %s, not a text field", role), "app_type types into a field; to press it, use app_tap").
			WithRemedy("app_tap for a " + role + "; app_type for a text field")
	}

	if text == "" {
		if err := typer.Clear(ctx, node); err != nil {
			return nil, err
		}
		return Result(fmt.Sprintf("cleared %s", target),
			ActionView{Action: "clear", Target: target}), nil
	}
	// app_type adds to what the field holds, as Vibium's type does; app_fill
	// replaces it. Neither platform can type at the cursor through its
	// server — UiAutomator2 replaces a field whatever it is asked, and
	// WebDriverAgent's read-back retries by clearing — so an append is the
	// field set to what it held plus the text, which SetText then confirms.
	// What it held is read from the node, never its placeholder, which both
	// platforms report where an empty field's text goes.
	want := text
	held := node.Text
	if node.ShowingHint || (node.Hint != "" && held == node.Hint) {
		held = ""
	}
	if !replace {
		if node.Password && held != "" {
			// It cannot be read back, so adding to it would replace it.
			return nil, mobiumerr.New(mobiumerr.Unsupported, "%s is a password field that already holds "+
				"something, which cannot be read back, so app_type cannot add to it — app_fill replaces the "+
				"whole password", target).
				WithRemedy("app_fill to replace the whole password")
		}
		want = held + text
	}
	if replace || held != "" {
		if err := typer.Clear(ctx, node); err != nil {
			return nil, err
		}
	}
	if err := typer.SetText(ctx, node, want); err != nil {
		// What no class table can know, the server can: React Native's
		// buttons are plain ViewGroups, and UiAutomator2 answered typing into
		// one with the W3C code "invalid element state". Decided by the code
		// the server sent, which is kept in details, never by its wording.
		if e, ok := mobiumerr.As(err); ok && e.Details["w3c"] == "invalid element state" {
			return nil, failedCheck(mobiumerr.InvalidArgument, target, checkEditable,
				"the device says it is not a text field", "app_type types into a field, and app_tap presses anything else").
				WithRemedy("app_tap to press it; app_type for a text field").
				WithDetail("w3c", "invalid element state")
		}
		return nil, err
	}
	if node.Password {
		// The caller's own input, but echoing it puts the password into
		// every transcript and log this result reaches — which is what the
		// rule against printing a password field exists to stop.
		return Result(fmt.Sprintf("typed %d characters into %s, a password field — not echoed",
			len([]rune(text)), target), ActionView{Action: "type", Target: target}), nil
	}
	return Result(fmt.Sprintf("typed %q into %s", text, target),
		ActionView{Action: "type", Target: target}), nil
}

// swipeDirections maps a named direction to a drag across the middle of the
// screen, expressed as fractions of its size.
//
// The gesture runs from 70% to 30% rather than edge to edge: a swipe starting
// at the very edge is a system back or notification gesture on modern Android,
// not a scroll of the app's content.
var swipeDirections = map[string][4]float64{
	"up":    {0.5, 0.7, 0.5, 0.3},
	"down":  {0.5, 0.3, 0.5, 0.7},
	"left":  {0.7, 0.5, 0.3, 0.5},
	"right": {0.3, 0.5, 0.7, 0.5},
}

func (h *Handlers) swipe(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	gest, ok := mobiumdriver.AsGesturer(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapGestures, "swipe")
	}

	duration := time.Duration(intArgOr(args, "duration_ms", 300)) * time.Millisecond

	// Explicit coordinates win over a direction.
	x1, has1 := intArg(args, "x1")
	y1, has2 := intArg(args, "y1")
	x2, has3 := intArg(args, "x2")
	y2, has4 := intArg(args, "y2")
	if has1 && has2 && has3 && has4 {
		if err := gest.Swipe(ctx, x1, y1, x2, y2, duration); err != nil {
			return nil, err
		}
		return Result(fmt.Sprintf("swiped (%d,%d) to (%d,%d) over %s", x1, y1, x2, y2, duration),
			ActionView{Action: "swipe", X: x2, Y: y2}), nil
	}

	dir := strings.ToLower(stringArg(args, "direction"))
	frac, ok := swipeDirections[dir]
	if !ok {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_swipe needs a direction (up, down, left, right) "+
			"or all four of x1, y1, x2, y2; got direction=%q", dir)
	}

	if target := stringArg(args, "target"); target != "" {
		return h.swipeOn(ctx, s, gest, target, dir, duration)
	}

	// Swipe within the screen the device actually reports, not an assumed size.
	tree, err := s.driver.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	w, hgt := tree.Root.Bounds.Width(), tree.Root.Bounds.Height()
	if w <= 0 || hgt <= 0 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "could not determine the screen size to swipe across")
	}
	sx, sy := int(float64(w)*frac[0]), int(float64(hgt)*frac[1])
	ex, ey := int(float64(w)*frac[2]), int(float64(hgt)*frac[3])

	if err := gest.Swipe(ctx, sx, sy, ex, ey, duration); err != nil {
		return nil, err
	}
	return Result(fmt.Sprintf("swiped %s: (%d,%d) to (%d,%d)", dir, sx, sy, ex, ey),
		ActionView{Action: "swipe", Target: dir, X: ex, Y: ey}), nil
}

// swipeFrom and swipeTo are where a swipe on an element starts and ends, as
// fractions of the element's extent along the direction it goes: from near
// the far edge, a little over a third of the way back. On NetNewsWire's
// article rows on an iPhone 17 Pro simulator that much revealed the row's
// actions, and a swipe across the whole row performed the first of them —
// starred an article — which is the app's choice, not the caller's. So the
// actions are revealed and then tapped by name.
const swipeFrom, swipeTo = 0.9, 0.5

// swipeOn swipes across target, after the checks a tap makes: it is there,
// enabled and not covered. A row's swipe actions were reachable only by
// coordinates taken from an earlier map, which the screen does not keep.
func (h *Handlers) swipeOn(ctx context.Context, s *session, gest mobiumdriver.Gesturer, target, dir string, duration time.Duration) (*ToolsCallResult, error) {
	if s.web != nil {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "a swipe on an element in a WebView is not built: the "+
			"page decides what a swipe does — switch to NATIVE_APP with app_context, or give x1, y1, x2, y2")
	}
	node, aim, err := h.resolveAim(ctx, s, target)
	if err != nil {
		return nil, err
	}
	b := node.Bounds
	along := func(lo, hi int, f float64) int { return lo + int(float64(hi-lo)*f) }
	sx, sy, ex, ey := aim.X, aim.Y, aim.X, aim.Y
	switch dir {
	case "left":
		sx, ex = along(b.X1, b.X2, swipeFrom), along(b.X1, b.X2, swipeTo)
	case "right":
		sx, ex = along(b.X2, b.X1, swipeFrom), along(b.X2, b.X1, swipeTo)
	case "up":
		sy, ey = along(b.Y1, b.Y2, swipeFrom), along(b.Y1, b.Y2, swipeTo)
	case "down":
		sy, ey = along(b.Y2, b.Y1, swipeFrom), along(b.Y2, b.Y1, swipeTo)
	}
	if err := gest.Swipe(ctx, sx, sy, ex, ey, duration); err != nil {
		return nil, err
	}
	delete(h.refs, s.dev.Serial)
	return Result(fmt.Sprintf("swiped %s %s: (%d,%d) to (%d,%d) — map again to see what it revealed", target, dir, sx, sy, ex, ey),
		ActionView{Action: "swipe", Target: target, X: ex, Y: ey}), nil
}

func (h *Handlers) longPress(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	gest, ok := mobiumdriver.AsGesturer(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapGestures, "long-press")
	}
	duration := time.Duration(intArgOr(args, "duration_ms", 800)) * time.Millisecond

	target := stringArg(args, "target")
	if target == "" {
		x, hasX := intArg(args, "x")
		y, hasY := intArg(args, "y")
		if !hasX || !hasY {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_long_press needs a target or both x and y")
		}
		if err := gest.LongPress(ctx, x, y, duration); err != nil {
			return nil, err
		}
		return Result(fmt.Sprintf("long-pressed (%d, %d) for %s", x, y, duration),
			ActionView{Action: "long_press", X: x, Y: y}), nil
	}

	_, aim, err := h.resolveAim(ctx, s, target)
	if err != nil {
		return nil, err
	}
	x, y := aim.X, aim.Y
	if err := gest.LongPress(ctx, x, y, duration); err != nil {
		return nil, err
	}
	note, cover := aimNote(aim)
	return Result(fmt.Sprintf("long-pressed %s at (%d, %d) for %s%s", target, x, y, duration, note),
		ActionView{Action: "long_press", Target: target, X: x, Y: y, Cover: cover}), nil
}

func boolArg(args map[string]interface{}, key string) bool {
	v, _ := args[key].(bool)
	return v
}

func intArgOr(args map[string]interface{}, key string, fallback int) int {
	if v, ok := intArg(args, key); ok {
		return v
	}
	return fallback
}
