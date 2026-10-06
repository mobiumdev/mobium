// Package mobium drives mobile apps on Android emulators, Android phones, iOS
// simulators and iPhones.
//
// It speaks to the same tool layer the CLI and the MCP server use, over
// `mobium pipe`, so a Go program and a command cannot drift apart. The mobium
// binary has to be on PATH, or named by MOBIUM_BIN_PATH:
//
//	go install github.com/mobiumdev/mobium/cmd/mobium@latest
//
// Start opens a session on the device and launches the app fresh; Quit ends
// it:
//
//	ctx := context.Background()
//	dev, err := mobium.Start(ctx, mobium.WithPlatform("android"), mobium.WithApp("com.example.shop"))
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer dev.Quit(ctx)
//
//	el, err := dev.WaitFor(ctx, "text=Sign in", nil)
//	if err != nil {
//		log.Fatal(err)
//	}
//	if err := dev.Tap(ctx, el.Ref); err != nil {
//		log.Fatal(err)
//	}
//
// Connect opens a connection without touching the device, and its Close
// leaves the session open for whoever started it.
//
// Refs like "@e1" are only valid for the screen they were taken from. Every
// action re-resolves its target immediately before acting and retries briefly
// while the screen settles, so a tap can follow another tap without a sleep.
package mobium

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Bounds is an on-screen rectangle in device pixels, on both platforms.
type Bounds struct {
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
	X2 int `json:"x2"`
	Y2 int `json:"y2"`
}

// Center is the point a tap targets.
func (b Bounds) Center() (int, int) { return (b.X1 + b.X2) / 2, (b.Y1 + b.Y2) / 2 }

// Width and Height are the rectangle's size in device pixels.
func (b Bounds) Width() int  { return b.X2 - b.X1 }
func (b Bounds) Height() int { return b.Y2 - b.Y1 }

// Locator is how a ref resolves on a later screen.
type Locator struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Exact bool   `json:"exact,omitempty"`
	Role  string `json:"role,omitempty"`
}

// String renders the locator the way the tools accept it.
func (l Locator) String() string {
	if l.Kind == "" {
		return ""
	}
	return l.Kind + "=" + l.Value
}

// Element is one actionable thing on screen.
type Element struct {
	Ref     string   `json:"ref"`
	Label   string   `json:"label"`
	Role    string   `json:"role,omitempty"`
	Locator *Locator `json:"locator,omitempty"`
	Bounds  Bounds   `json:"bounds"`
	// Context names the WebView an element came from, empty for native ones.
	Context string `json:"context,omitempty"`
	// Checked is a checkbox, radio or switch's state; nil for anything with
	// no such state, which is a different answer from unchecked.
	Checked *bool `json:"checked,omitempty"`
	// Selected is true for what the platform reports chosen: the current
	// tab, the chosen segment of a segmented control.
	Selected bool `json:"selected,omitempty"`
	// Disabled is true for what the platform reports not enabled: an action
	// on it waits for it to be enabled, and is refused if it stays disabled.
	Disabled bool `json:"disabled,omitempty"`
	// Value is what a slider reads, as the app states it ("80%", "1.2");
	// empty for everything else. Fill a slider with a position from 0 to 1.
	Value string `json:"value,omitempty"`
}

// DeviceInfo is one attached device or simulator.
type DeviceInfo struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	State    string `json:"state"`
	Model    string `json:"model,omitempty"`
	Runtime  string `json:"runtime,omitempty"`
	Emulator bool   `json:"emulator"`
}

// Device is a connection to mobium. It is safe for concurrent use: calls are
// serialized, because there is one pipe underneath.
type Device struct {
	conn *conn
	// started is what Start opened, so Quit ends that device's session even
	// when several are running. Nil after Connect.
	started *Session
	quit    bool
}

// Option configures Connect and Start.
type Option func(*settings)

type settings struct {
	binary   string
	serial   string
	driver   string
	platform string
	app      string
	session  string
	args     []string
}

// WithBinary pins the mobium executable, ahead of MOBIUM_BIN_PATH and PATH.
func WithBinary(path string) Option { return func(s *settings) { s.binary = path } }

// WithDevice targets one device by serial or UDID. Omit it when only one
// device is running.
func WithDevice(serial string) Option { return func(s *settings) { s.serial = serial } }

// WithDriver chooses the driver: "uiautomator2" (default on Android),
// "uiautomator" (installs nothing, slower, cannot type) or "wda"
// (iOS simulators and iPhones).
func WithDriver(name string) Option { return func(s *settings) { s.driver = name } }

// WithPlatform names the platform for Start: "android" or "ios". "ios" picks
// wda, so the driver need not be named.
func WithPlatform(name string) Option { return func(s *settings) { s.platform = name } }

// WithSession gives this connection a daemon of its own, named name, as
// MOBIUM_SESSION does. One daemon serves one call at a time across every
// device, so two test runs driving two devices at once should each have one:
// sharing, an emulator's 15 maps took 22.3s behind a simulator's, against
// 0.3s on a daemon of its own. The name is part of a socket path, so keep it
// short.
func WithSession(name string) Option { return func(s *settings) { s.session = name } }

// WithApp is an app for Start to launch once the session is up, by package
// name (Android) or bundle id (iOS).
func WithApp(id string) Option { return func(s *settings) { s.app = id } }

// Session is what Start found: the device and how it is driven.
type Session struct {
	Device   string `json:"device"`
	Platform string `json:"platform"`
	Driver   string `json:"driver"`
	// Reused says a session was already open on the device and was kept.
	Reused bool `json:"reused"`
	// App is the app Start launched, if one was asked for.
	App string `json:"app"`
}

// Start connects and opens the session on the device: the device-side
// server is started now, and the app, if one
// was named with WithApp, launched and in front. End it with Quit.
//
// Nothing requires it — every call opens a session on first use — but it puts
// the slow first start (installing UiAutomator2, building WebDriverAgent on
// an iPhone) where it was asked for, and says which device it got.
func Start(ctx context.Context, opts ...Option) (*Device, error) {
	var s settings
	for _, opt := range opts {
		opt(&s)
	}
	d, err := Connect(opts...)
	if err != nil {
		return nil, err
	}
	args := map[string]any{"action": "start"}
	if s.platform != "" {
		args["platform"] = s.platform
	}
	if s.app != "" {
		args["app"] = s.app
	}
	var out Session
	if err := d.data(ctx, "app_session", args, &out); err != nil {
		d.Close()
		return nil, err
	}
	d.started = &out
	return d, nil
}

// Session is what Start opened — the device, platform and driver it got — or
// nil for a Device from Connect.
func (d *Device) Session() *Session { return d.started }

// Quit ends the session on the device and closes the connection. The session's teardown is the daemon's own: accessibility
// settings put back, a recording or route stopped, WebViews detached, the
// device-side server stopped, and the app Start launched, if any, stopped
// too. Quitting a session that is not open succeeds, and a second Quit — a
// deferred one after an explicit one, say — does nothing.
//
// A program that exits without Quit or Close has the sessions it started
// ended for it, the same way: mobium sees the client go.
func (d *Device) Quit(ctx context.Context) error {
	if d.quit {
		return nil
	}
	d.quit = true
	args := map[string]any{"action": "end"}
	if d.started != nil {
		args["device"] = d.started.Device
	}
	err := d.act(ctx, "app_session", args)
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}

// Sessions lists the sessions open on the daemon: device, platform, driver.
func (d *Device) Sessions(ctx context.Context) ([]Session, error) {
	var out struct {
		Sessions []Session `json:"sessions"`
	}
	if err := d.data(ctx, "app_session", map[string]any{"action": "status"}, &out); err != nil {
		return nil, err
	}
	return out.Sessions, nil
}

// Connect opens a connection to mobium. It does not touch the device: the
// session there opens on the first call that needs it, or with Start.
//
// The transport is `mobium pipe`, which forwards to the shared daemon rather
// than starting a session of its own: a device-side server holds one session
// at a time, so a client with its own would invalidate the CLI's.
func Connect(opts ...Option) (*Device, error) {
	var s settings
	for _, opt := range opts {
		opt(&s)
	}
	binary, err := FindBinary(s.binary)
	if err != nil {
		return nil, err
	}
	if s.serial != "" {
		s.args = append(s.args, "--device", s.serial)
	}
	if s.driver != "" {
		s.args = append(s.args, "--driver", s.driver)
	}
	c, err := dial(binary, s.args, s.session)
	if err != nil {
		return nil, err
	}
	return &Device{conn: c}, nil
}

// Close closes the connection. The device's session lives in the daemon and
// stays open, for the next Connect or the CLI; Quit ends it.
func (d *Device) Close() error { return d.conn.Close() }

// -- reading ---------------------------------------------------------------

// Devices lists every attached device and simulator.
func (d *Device) Devices(ctx context.Context) ([]DeviceInfo, error) {
	var out struct {
		Devices []DeviceInfo `json:"devices"`
	}
	if err := d.data(ctx, "app_devices", nil, &out); err != nil {
		return nil, err
	}
	return out.Devices, nil
}

// Booted is a virtual device Boot started, or found already running.
type Booted struct {
	// Device is its serial (an emulator) or UDID (a simulator).
	Device   string `json:"device"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	// Already says it was running before Boot was asked.
	Already bool   `json:"already"`
	Took    string `json:"took,omitempty"`
}

// Boot starts an Android emulator by its AVD's name, or an iOS simulator by
// its name or UDID, and returns once it has booted. One already running is
// returned as it is. An emulator cold-boots headless unless window is true.
func (d *Device) Boot(ctx context.Context, name string, window bool) (Booted, error) {
	var out Booted
	args := map[string]any{"name": name}
	if window {
		args["window"] = true
	}
	err := d.data(ctx, "app_boot", args, &out)
	return out, err
}

// Shutdown shuts down an emulator or a simulator — by serial, AVD name, UDID
// or simulator name — after ending the daemon's session on it, and returns
// once it is gone. A real phone is refused.
func (d *Device) Shutdown(ctx context.Context, name string) error {
	return d.act(ctx, "app_shutdown", map[string]any{"name": name})
}

// Map returns the actionable elements on the current screen.
//
// Refs are only valid for this screen; call Map again after anything that
// changes it, or just act — actions re-resolve their target anyway.
func (d *Device) Map(ctx context.Context) ([]Element, error) {
	return d.elements(ctx, "app_map", nil)
}

// MapDiff is what changed on the screen since the last map of this device:
// what appeared, what went away, and what changed its label, its checked
// state or its place. Taken right after an action, it is what that action
// just did, without the rest of the screen that stayed put.
//
// Refs on Added and on a change's After are the new map's, and can be acted
// on. Removed and a change's Before are the earlier map's elements as they
// were, refs included, and those refs no longer resolve: every map renumbers.
type MapDiff struct {
	// First says there was no earlier map of this device to compare with.
	// Compared with nothing, everything appeared: the whole screen is in
	// Added, and the next MapDiff compares with this map.
	First bool `json:"first,omitempty"`
	// Since is when the map compared with was taken; zero when First.
	Since   time.Time   `json:"since,omitempty"`
	Added   []Element   `json:"added"`
	Removed []Element   `json:"removed"`
	Changed []MapChange `json:"changed"`
}

// MapChange is one element in both maps that differs between them.
type MapChange struct {
	Before Element `json:"before"`
	After  Element `json:"after"`
	// What names each difference: "label", "checked", "selected", "disabled", "value",
	// "moved" or "resized".
	What []string `json:"what"`
}

// MapDiff maps the current screen and reports what changed since the last
// map of this device, which it then replaces: the next MapDiff compares with
// this one.
func (d *Device) MapDiff(ctx context.Context) (*MapDiff, error) {
	var out struct {
		Diff *MapDiff `json:"diff"`
	}
	if err := d.data(ctx, "app_map", map[string]any{"diff": true}, &out); err != nil {
		return nil, err
	}
	if out.Diff == nil {
		return nil, fmt.Errorf("app_map sent no diff: the daemon predates map diffs, so rebuild or update mobium")
	}
	return out.Diff, nil
}

// Find returns the elements matching a locator, without acting on them.
func (d *Device) Find(ctx context.Context, locator string) ([]Element, error) {
	return d.elements(ctx, "app_find", map[string]any{"locator": locator})
}

// Text reads everything on screen. Pass a ref or locator to read one element
// instead, or "" for the whole screen.
func (d *Device) Text(ctx context.Context, target string) (string, error) {
	args := map[string]any{}
	if target != "" {
		args["target"] = target
	}
	res, err := d.conn.call(ctx, "app_text", args)
	if err != nil {
		return "", err
	}
	return res.text(), nil
}

// Current reports the package name or bundle id of the foreground app.
//
// It costs no extra device call: it reads the hierarchy a snapshot fetches
// anyway. Use it to confirm a tap went where you expected.
func (d *Device) Current(ctx context.Context) (string, error) {
	var out struct {
		App string `json:"app"`
	}
	if err := d.data(ctx, "app_current", nil, &out); err != nil {
		return "", err
	}
	return out.App, nil
}

// Screenshot captures the screen as PNG. With a path it also writes the file;
// without one the bytes come back over the wire.
func (d *Device) Screenshot(ctx context.Context, path string) ([]byte, error) {
	if path == "" {
		res, err := d.conn.call(ctx, "app_screenshot", nil)
		if err != nil {
			return nil, err
		}
		return res.image()
	}
	if _, err := d.conn.call(ctx, "app_screenshot", map[string]any{"path": path}); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// -- waiting and scrolling -------------------------------------------------

// Wait conditions.
const (
	// Visible waits for the element to be on screen. The default.
	Visible = "visible"
	// Hidden waits for it to go away — a spinner, say.
	Hidden = "hidden"
	// HasText waits until it contains the text given in WaitOptions.Text.
	HasText = "text"
	// HasValue waits until a field holds exactly WaitOptions.Text; "" waits
	// for it to be empty. A password field is refused.
	HasValue = "value"
	// Enabled and Disabled wait for a control to be one or the other.
	Enabled  = "enabled"
	Disabled = "disabled"
	// Checked and Unchecked wait for a checkbox, radio or switch.
	Checked   = "checked"
	Unchecked = "unchecked"
	// Focused waits for a field to have keyboard focus.
	Focused = "focused"
	// Count waits until the locator matches WaitOptions.Count elements.
	Count = "count"
)

// WaitOptions tunes WaitFor. A nil *WaitOptions means visible, ten seconds.
type WaitOptions struct {
	// Condition is Visible (the default), Hidden, HasText, HasValue,
	// Enabled, Disabled, Checked, Unchecked or Focused.
	Condition string
	// Text is what to wait for, with HasText and HasValue; with HasValue an
	// empty Text waits for an empty field.
	Text string
	// Timeout defaults to ten seconds and may not exceed two minutes.
	Timeout time.Duration
	// Not waits for the opposite of Condition: HasText with Not waits for
	// the text to change.
	Not bool
	// Exact makes HasText match the whole text rather than a part.
	Exact bool
	// Count is how many matches Count waits for.
	Count int
}

// WaitFor blocks until the screen agrees, instead of sleeping.
//
// On success the screen is remapped, so the returned element already has a ref
// that can be tapped without calling Map first. Waiting for something to go
// away returns a nil element and a nil error, since there is nothing left to
// point at. If the condition never holds the error says what was on screen
// instead, which is usually the answer.
func (d *Device) WaitFor(ctx context.Context, target string, opts *WaitOptions) (*Element, error) {
	if opts == nil {
		opts = &WaitOptions{}
	}
	args := map[string]any{"target": target}
	if opts.Condition != "" {
		args["condition"] = opts.Condition
	}
	if opts.Text != "" || opts.Condition == HasValue {
		args["text"] = opts.Text
	}
	if opts.Not {
		args["not"] = true
	}
	if opts.Exact {
		args["exact"] = true
	}
	if opts.Condition == Count {
		args["count"] = opts.Count
	}
	if opts.Timeout > 0 {
		args["timeout_ms"] = int(opts.Timeout / time.Millisecond)
	}
	return d.element(ctx, "app_wait_for", args)
}

// Scroll directions.
const (
	// Down looks further down the list. The default.
	Down = "down"
	// Up looks back towards the top.
	Up = "up"
	// Right looks further right along a horizontal list or pager, as Down
	// looks further down — the finger travels right to left — and Left looks
	// back.
	Left  = "left"
	Right = "right"
)

// ScrollTo scrolls until an element is on screen and returns it with a ref.
//
// Map only sees what is currently visible. Tap, Type and LongPress already
// scroll to a target that is not, so reach for this to look without acting, or
// to scroll back up. direction is Down, Up, Left or Right, and "" is Down:
// nothing on screen says which way a container scrolls, so a horizontal pager
// needs Left or Right. Swiping the wrong way is not a no-op, so if a swipe
// navigates instead of scrolling, it stops after one.
func (d *Device) ScrollTo(ctx context.Context, target, direction string) (*Element, error) {
	args := map[string]any{"target": target}
	if direction != "" {
		args["direction"] = direction
	}
	return d.element(ctx, "app_scroll_to", args)
}

// -- acting ----------------------------------------------------------------

// Tap taps a ref ("@e5") or a locator ("text=Sign In").
//
// The element is re-resolved immediately before the tap, and the tap scrolls
// to it first if it is below the fold.
func (d *Device) Tap(ctx context.Context, target string) error {
	return d.act(ctx, "app_tap", map[string]any{"target": target})
}

// TapPoint taps a point in device pixels.
func (d *Device) TapPoint(ctx context.Context, x, y int) error {
	return d.act(ctx, "app_tap", map[string]any{"x": x, "y": y})
}

// DoubleTap taps an element twice, close enough together that the platform
// reads one gesture rather than two taps.
//
// The same tool as Tap with one argument set, so the target is resolved the
// same way and refused the same way when the screen has moved. The uiautomator
// dump driver refuses it: nothing there controls the interval.
func (d *Device) DoubleTap(ctx context.Context, target string) error {
	return d.act(ctx, "app_tap", map[string]any{"target": target, "double": true})
}

// DoubleTapPoint double-taps a point in device pixels.
func (d *Device) DoubleTapPoint(ctx context.Context, x, y int) error {
	return d.act(ctx, "app_tap", map[string]any{"x": x, "y": y, "double": true})
}

// Drag picks one element up, carries it onto another, and drops it.
//
// Not Swipe with two targets: a swipe has no hold at either end, so pointed at
// a reorderable row it scrolls the list instead of moving the row. Both ends
// are resolved from one snapshot before anything is touched.
//
// What it reports is that the gesture was delivered. Whether the drop was
// accepted is the app's own state — call Map again to see it.
func (d *Device) Drag(ctx context.Context, from, to string) error {
	return d.act(ctx, "app_drag", map[string]any{"from": from, "to": to})
}

// DragFor is Drag with the hold at each end spelled out. Raise it first when
// a drag picks nothing up: the default is 700ms, above Android's 500ms
// long-press timeout, and some lists arm slower than that.
func (d *Device) DragFor(ctx context.Context, from, to string, hold time.Duration) error {
	return d.act(ctx, "app_drag", map[string]any{
		"from": from, "to": to, "hold_ms": int(hold.Milliseconds()),
	})
}

// TapFingers taps an element with several fingers at once, side by side — a
// two-finger tap with 2, a three-finger tap with 3 (up to 5). On iOS three
// fingers can reach the system instead of the app: three-finger gestures are
// undo, redo, copy and paste there.
func (d *Device) TapFingers(ctx context.Context, target string, fingers int) error {
	return d.act(ctx, "app_tap", map[string]any{"target": target, "fingers": fingers})
}

// PressTap holds one element with a finger while a second finger taps
// another, and lifts the first only after the second. Both are resolved
// before anything is touched. It reports that the gesture was delivered; what
// it meant is the app's own state, so call Map again to see it. Android 15 and
// earlier only: on iOS XCTest adds a zero-length touch at the second finger's
// target when the gesture starts, and on Android 16 and later UiAutomator2's
// down times are rejected, so both refuse (ErrUnsupported).
func (d *Device) PressTap(ctx context.Context, hold, tap string) error {
	return d.act(ctx, "app_press_tap", map[string]any{"hold": hold, "tap": tap})
}

// PressDrag holds one element with a finger while a second finger drags from
// one element to another. Not Drag, which is one finger carrying something:
// here one finger anchors and the other moves. Android 15 and earlier only,
// for PressTap's reasons.
func (d *Device) PressDrag(ctx context.Context, hold, from, to string) error {
	return d.act(ctx, "app_press_drag", map[string]any{"hold": hold, "from": from, "to": to})
}

// Type puts text into an element, after what it holds. Pass "" to clear it.
func (d *Device) Type(ctx context.Context, target, text string) error {
	return d.act(ctx, "app_type", map[string]any{"target": target, "text": text})
}

// Fill clears an element and types into it, replacing what it held.
func (d *Device) Fill(ctx context.Context, target, text string) error {
	return d.act(ctx, "app_fill", map[string]any{"target": target, "text": text})
}

// Swipe drags across the middle of the screen in a direction: "up", "down",
// "left" or "right". The finger moves that way, so "up" scrolls a page down.
func (d *Device) Swipe(ctx context.Context, direction string) error {
	return d.act(ctx, "app_swipe", map[string]any{"direction": direction})
}

// SwipeOn swipes across an element in a direction, after the checks a tap
// makes — part of the way, which reveals a list row's swipe actions without
// performing the first. Map again and tap the action you mean.
func (d *Device) SwipeOn(ctx context.Context, target, direction string) error {
	return d.act(ctx, "app_swipe", map[string]any{"target": target, "direction": direction})
}

// SwipePoints drags between two points in device pixels.
func (d *Device) SwipePoints(ctx context.Context, x1, y1, x2, y2 int, duration time.Duration) error {
	args := map[string]any{"x1": x1, "y1": y1, "x2": x2, "y2": y2}
	if duration > 0 {
		args["duration_ms"] = int(duration / time.Millisecond)
	}
	return d.act(ctx, "app_swipe", args)
}

// LongPress presses and holds an element. A zero duration uses the default.
func (d *Device) LongPress(ctx context.Context, target string, duration time.Duration) error {
	args := map[string]any{"target": target}
	if duration > 0 {
		args["duration_ms"] = int(duration / time.Millisecond)
	}
	return d.act(ctx, "app_long_press", args)
}

// -- app lifecycle ---------------------------------------------------------

// Launch brings an app to the foreground by package name or bundle id, and
// waits for it to actually be in front.
//
// Every ref from the previous screen is discarded.
func (d *Device) Launch(ctx context.Context, app string) error {
	return d.act(ctx, "app_launch", map[string]any{"app": app})
}

// LaunchWithHitTest launches an app on an iOS simulator with the hit probe
// loaded in it, so every action on an element in it first asks UIKit where
// the touch goes, and is refused when it would land elsewhere — an overlay
// hidden from accessibility included. A real iPhone and Android refuse.
func (d *Device) LaunchWithHitTest(ctx context.Context, app string) error {
	return d.act(ctx, "app_launch", map[string]any{"app": app, "hit_test": true})
}

// LaunchWithGrayBox launches an app with Mobium's gray-box library
// turned on: the app says when it is busy, and every action waits for it to
// be idle before finding its target. It needs an app built with the library.
func (d *Device) LaunchWithGrayBox(ctx context.Context, app string) error {
	return d.act(ctx, "app_launch", map[string]any{"app": app, "gray_box": true})
}

// Terminate stops a running app.
func (d *Device) Terminate(ctx context.Context, app string) error {
	return d.act(ctx, "app_terminate", map[string]any{"app": app})
}

// Install adds an app from a local .apk (Android) or .app bundle (iOS), and
// returns the absolute path that was installed.
func (d *Device) Install(ctx context.Context, path string) (string, error) {
	var out struct {
		Path string `json:"path"`
	}
	if err := d.data(ctx, "app_install", map[string]any{"path": path}, &out); err != nil {
		return "", err
	}
	return out.Path, nil
}

// App is one installed app. Name is empty on Android, where reading a
// package's label costs a dumpsys per app.
type App struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	System  bool   `json:"system"`
}

// Apps lists installed apps. With system false — the usual case — it lists
// only what someone installed; a stock Android emulator ships about 240
// system packages.
func (d *Device) Apps(ctx context.Context, system bool) ([]App, error) {
	var out struct {
		Apps []App `json:"apps"`
	}
	if err := d.data(ctx, "app_list_apps", map[string]any{"system": system}, &out); err != nil {
		return nil, err
	}
	return out.Apps, nil
}

// Uninstall removes an app, verified by listing afterwards. `adb uninstall`
// reports success when it has only removed the updates to a system app, so
// this returns an error rather than a lie.
func (d *Device) Uninstall(ctx context.Context, app string) error {
	return d.act(ctx, "app_uninstall", map[string]any{"app": app})
}

// PageSource is the raw hierarchy, as the device-side server sent it.
type PageSource struct {
	// Source is the platform's XML, or in a WebView the page's markup.
	Source string `json:"source"`
	// Format is "xml" or "html".
	Format string `json:"format"`
	// Units is "px" on Android and "pt" on iOS, where map, taps and
	// screenshots are in pixels, Scale times as many. Empty for a page.
	Units string  `json:"units,omitempty"`
	Scale float64 `json:"scale,omitempty"`
	// Redacted is how many password fields had their contents hidden.
	Redacted int `json:"redacted"`
}

// Source returns the raw hierarchy — the page source — for when Map leaves
// out the thing you need to see. Map is what to act on.
func (d *Device) Source(ctx context.Context) (PageSource, error) {
	var out PageSource
	err := d.data(ctx, "app_source", map[string]any{}, &out)
	return out, err
}

// DialogRule is a declared answer to a dialog: when one whose text contains
// When is in an action's way, press the button captioned Press.
type DialogRule struct {
	When  string `json:"when"`
	Press string `json:"press"`
	Hits  int    `json:"hits"`
}

// AddDialogRule declares how to answer a dialog, so an action that meets it
// carries on. It names a button, not accept or dismiss: which button those
// press differs by platform and by dialog. Captions match ignoring case.
func (d *Device) AddDialogRule(ctx context.Context, when, press string) error {
	return d.act(ctx, "app_dialogs", map[string]any{"when": when, "press": press})
}

// DialogRules lists the declared rules, with how often each has answered.
func (d *Device) DialogRules(ctx context.Context) ([]DialogRule, error) {
	var out struct {
		Rules []DialogRule `json:"rules"`
	}
	err := d.data(ctx, "app_dialogs", map[string]any{}, &out)
	return out.Rules, err
}

// ClearDialogRules removes every rule for this device.
func (d *Device) ClearDialogRules(ctx context.Context) error {
	return d.act(ctx, "app_dialogs", map[string]any{"clear": true})
}

// ClearedData is what ClearData did: the stores read back empty, what was
// kept, on Android the runtime permissions still granted afterwards, and on a
// real iPhone what the reset changed that nothing can read back.
type ClearedData struct {
	Emptied      []string `json:"emptied"`
	Kept         []string `json:"kept,omitempty"`
	StillGranted []string `json:"still_granted"`
	NotReadBack  []string `json:"not_read_back,omitempty"`
}

// ClearData deletes an app's data and leaves it installed — the state of a
// fresh install, without reinstalling. Android's `pm clear` also revokes the
// runtime permissions the user granted; an iOS simulator keeps its privacy
// grants and keychain. A real iPhone refuses.
func (d *Device) ClearData(ctx context.Context, app string) (ClearedData, error) {
	var out ClearedData
	err := d.data(ctx, "app_clear_data", map[string]any{"app": app}, &out)
	return out, err
}

// ResetFromBundle resets an app on a real iPhone, which cannot clear one in
// place: it is uninstalled and installed again from bundle, its own .app or
// .ipa. Its data container is read back empty; its privacy permissions,
// which nothing outside the app can read, are in NotReadBack. Any other
// device refuses a bundle, since it clears in place.
func (d *Device) ResetFromBundle(ctx context.Context, app, bundle string) (ClearedData, error) {
	var out ClearedData
	err := d.data(ctx, "app_clear_data", map[string]any{"app": app, "path": bundle}, &out)
	return out, err
}

// -- files -----------------------------------------------------------------

// Transfer is one file moved between this machine and the device, as read
// back at both ends.
type Transfer struct {
	Device string `json:"device"`
	// App is the app whose Documents it was, on iOS; empty on Android, which
	// has one Download folder for every app.
	App  string `json:"app,omitempty"`
	Name string `json:"name"`
	// Where is the file on the device, as a person would find it:
	// "Download/report.txt", or "MobiumApp's Documents/report.txt".
	Where string `json:"where"`
	Bytes int64  `json:"bytes"`
	// Checked says how the transfer was confirmed at both ends.
	Checked string `json:"checked"`
	// Path is the file on this machine: what was sent, or where it was saved.
	Path string `json:"path,omitempty"`
	// Files and Folder are set by PushPath and PullPath: how many files
	// moved, and whether they were a folder.
	Files  int  `json:"files,omitempty"`
	Folder bool `json:"folder,omitempty"`
}

// PushPath sends a local file or folder to a path on the device and reads
// every file's size back there. On Android devicePath is absolute
// (/sdcard/..., /data/local/tmp/...), or with app it is a path in that app's
// private data, which needs a debuggable build. On iOS it is a path in an
// app's data container — app, or the one in front when app is empty.
func (d *Device) PushPath(ctx context.Context, local, devicePath, app string) (*Transfer, error) {
	args := map[string]any{"path": local, "device_path": devicePath}
	if app != "" {
		args["app"] = app
	}
	var out Transfer
	if err := d.data(ctx, "app_upload", args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PullPath brings back the file or folder at a path on the device — the paths
// PushPath takes — to local, which must not exist yet for a folder, and checks
// every file's size against the device's.
func (d *Device) PullPath(ctx context.Context, devicePath, local, app string) (*Transfer, error) {
	if local == "" {
		return nil, &Error{Tool: "app_download", Code: CodeInvalidArgument,
			Reason: "PullPath needs where to save it on this machine"}
	}
	args := map[string]any{"device_path": devicePath, "path": local}
	if app != "" {
		args["app"] = app
	}
	var out Transfer
	if err := d.data(ctx, "app_download", args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeviceFile is one file in the folder the device keeps downloads in.
type DeviceFile struct {
	Name     string    `json:"name"`
	Bytes    int64     `json:"bytes"`
	Modified time.Time `json:"modified"`
}

// TransferOptions tunes Upload. A nil *TransferOptions uploads under the
// file's own name to the app in front.
type TransferOptions struct {
	// Name is what to call the file on the device — a name, not a path.
	// Empty keeps the local file's name.
	Name string
	// App is, on iOS, the bundle id whose Documents the file goes to; empty
	// means the app in front. Android ignores it.
	App string
}

// Upload puts a file from this machine where the device keeps downloads, so
// an app's file picker finds it. On Android that is the shared Download
// folder, and the file is indexed in MediaStore — which is what the picker
// reads — and read back there. On an iOS simulator it is an app's own
// Documents folder, which the Files app shows under On My iPhone: the app
// named in opts, or the one in front. On a real iPhone it is the same folder,
// through CoreDevice, and the upload is confirmed by reading its bytes back.
//
// A relative path is this process's: `mobium pipe` resolves it before the
// daemon sees it.
func (d *Device) Upload(ctx context.Context, path string, opts *TransferOptions) (*Transfer, error) {
	if opts == nil {
		opts = &TransferOptions{}
	}
	args := map[string]any{"path": path}
	if opts.Name != "" {
		args["name"] = opts.Name
	}
	if opts.App != "" {
		args["app"] = opts.App
	}
	var out Transfer
	if err := d.data(ctx, "app_upload", args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Download saves the file called name, from where the device keeps downloads,
// to path on this machine, and checks the copy's size against the device's.
// On Android the folder is the shared Download folder; on an iOS simulator it
// is an app's Documents — app, or the one in front when app is empty — and on
// a real iPhone the same. A relative path is this process's.
func (d *Device) Download(ctx context.Context, name, path, app string) (*Transfer, error) {
	if name == "" || path == "" {
		return nil, &Error{Tool: "app_download", Code: CodeInvalidArgument,
			Reason: "Download needs the file's name and where to save it; Downloads lists the folder"}
	}
	args := map[string]any{"name": name, "path": path}
	if app != "" {
		args["app"] = app
	}
	var out Transfer
	if err := d.data(ctx, "app_download", args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadBytes is Download without touching this machine's disk: the file
// comes back in the answer and is returned as it was on the device.
func (d *Device) DownloadBytes(ctx context.Context, name, app string) ([]byte, error) {
	if name == "" {
		return nil, &Error{Tool: "app_download", Code: CodeInvalidArgument,
			Reason: "DownloadBytes needs the file's name; Downloads lists the folder"}
	}
	args := map[string]any{"name": name}
	if app != "" {
		args["app"] = app
	}
	var out struct {
		Bytes int64  `json:"bytes"`
		Data  string `json:"data"`
	}
	if err := d.data(ctx, "app_download", args, &out); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		return nil, fmt.Errorf("mobium returned an unreadable file: %w", err)
	}
	if int64(len(raw)) != out.Bytes {
		return nil, &Error{Tool: "app_download", Code: CodeNotConfirmed,
			Reason: fmt.Sprintf("the device reported %d bytes and %d arrived", out.Bytes, len(raw))}
	}
	return raw, nil
}

// Downloads lists what the folder the device keeps downloads in holds:
// Android's shared Download folder, or on an iOS simulator an app's
// Documents — app, or the one in front when app is empty — and on a real
// iPhone the same. An empty folder is an empty list, not an error.
func (d *Device) Downloads(ctx context.Context, app string) ([]DeviceFile, error) {
	args := map[string]any{}
	if app != "" {
		args["app"] = app
	}
	var out struct {
		Files []DeviceFile `json:"files"`
	}
	if err := d.data(ctx, "app_download", args, &out); err != nil {
		return nil, err
	}
	return out.Files, nil
}

// Step is one call in a Batch: a tool and the arguments it takes on its own.
type Step struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// StepResult is one step's answer in a Batch: its text, and its data as the
// tool's own result, to decode with json.Unmarshal into what that tool
// returns. Image says it answered with an image — a screenshot with no path.
type StepResult struct {
	Name  string          `json:"name"`
	Text  string          `json:"text"`
	Data  json.RawMessage `json:"data,omitempty"`
	Image bool            `json:"image,omitempty"`
}

// Batch runs several tools in order, on this device, in one call:
//
//	d.Batch(ctx,
//		mobium.Step{Name: "app_tap", Arguments: map[string]any{"target": "text=Sign in"}},
//		mobium.Step{Name: "app_wait_for", Arguments: map[string]any{"target": "text=Welcome"}},
//	)
//
// Every step is checked before the first runs, and the batch stops at the
// first failure with that step's own error; its Details hold "step" and what
// "completed" before it.
func (d *Device) Batch(ctx context.Context, steps ...Step) ([]StepResult, error) {
	var out struct {
		Steps []StepResult `json:"steps"`
	}
	err := d.data(ctx, "app_batch", map[string]any{"steps": steps}, &out)
	return out.Steps, err
}

// BatteryStatus is the battery, from Battery.
type BatteryStatus struct {
	// Present is false on a device with no battery — an iOS simulator.
	Present bool `json:"present"`
	// Level is the charge in percent; nil when there is no battery.
	Level *int `json:"level,omitempty"`
	// State is "charging", "discharging", "not_charging", "full" or "unknown".
	State string `json:"state"`
	// Plugged is what powers it, on Android; empty on iOS.
	Plugged string `json:"plugged,omitempty"`
}

// Battery reads the battery.
func (d *Device) Battery(ctx context.Context) (BatteryStatus, error) {
	var out BatteryStatus
	err := d.data(ctx, "app_battery", map[string]any{}, &out)
	return out, err
}

// DeviceClock is the device's clock, from DeviceTime.
type DeviceClock struct {
	// Time is RFC 3339 with milliseconds, in the device's own offset.
	Time string `json:"time"`
	// Zone is the device's IANA timezone.
	Zone string `json:"zone,omitempty"`
	// Clock is "device", or "mac" on an iOS simulator, which has none.
	Clock string `json:"clock"`
}

// DeviceTime reads what time the device thinks it is.
func (d *Device) DeviceTime(ctx context.Context) (DeviceClock, error) {
	var out DeviceClock
	err := d.data(ctx, "app_time", map[string]any{}, &out)
	return out, err
}

// NetworkStatus is the device's network, read back by Network and each call
// that changes it.
type NetworkStatus struct {
	// Airplane is airplane mode; Online whether the device has a network.
	Airplane bool `json:"airplane"`
	Online   bool `json:"online"`
	// LatencyMs is added to each round trip; the rates are kbit/s. Zero is none.
	LatencyMs    int `json:"latency_ms"`
	DownloadKbps int `json:"download_kbps"`
	UploadKbps   int `json:"upload_kbps"`
	// Changed says whether the call changed anything.
	Changed bool `json:"changed"`
}

// Network reads the device's network conditions. Android only.
func (d *Device) Network(ctx context.Context) (NetworkStatus, error) {
	return d.network(ctx, map[string]any{})
}

// SetOffline turns airplane mode on or off and waits for the network to
// follow — on an emulator or a real Android phone.
func (d *Device) SetOffline(ctx context.Context, offline bool) (NetworkStatus, error) {
	return d.network(ctx, map[string]any{"offline": offline})
}

// ShapeNetwork sets the latency added to each round trip and the download
// and upload limits in kbit/s, replacing any set before; zero is none. It
// needs root, so an emulator.
func (d *Device) ShapeNetwork(ctx context.Context, latencyMs, downloadKbps, uploadKbps int) (NetworkStatus, error) {
	return d.network(ctx, map[string]any{"latency_ms": latencyMs, "download_kbps": downloadKbps,
		"upload_kbps": uploadKbps})
}

// ResetNetwork removes the shaping and turns airplane mode off.
func (d *Device) ResetNetwork(ctx context.Context) (NetworkStatus, error) {
	return d.network(ctx, map[string]any{"reset": true})
}

func (d *Device) network(ctx context.Context, args map[string]any) (NetworkStatus, error) {
	var out NetworkStatus
	err := d.data(ctx, "app_network", args, &out)
	return out, err
}

// Shake shakes an emulator or simulator — what shake-to-undo and
// shake-to-report listen for. Whether the app reacts is up to its own
// detector, so check the screen after. A real phone returns an error.
func (d *Device) Shake(ctx context.Context) error {
	return d.act(ctx, "app_shake", map[string]any{})
}

// BiometricStatus is what Biometric found or did.
type BiometricStatus struct {
	// Kind is "face" or "fingerprint": the one this device can be shown.
	Kind     string `json:"kind"`
	Enrolled bool   `json:"enrolled"`
	// LockedOut is an emulator's sensor refusing every touch for now, after
	// too many that did not match.
	LockedOut bool `json:"locked_out,omitempty"`
	// Outcome is what the device made of a face or finger presented by
	// "match" or "nomatch": "accepted", "not recognized", "failed" (the
	// prompt gave up) or "locked out" — empty when nothing on screen could
	// say. Whether the app signed in is the app's to show.
	Outcome string `json:"outcome,omitempty"`
	Note    string `json:"note,omitempty"`
}

// Biometric reads or sets the enrollment of an emulator's fingerprint or a
// simulator's face or finger — action "status", "enroll" or "unenroll" —
// or presents a matching ("match") or a stranger's ("nomatch") one to the
// prompt that is up. With no prompt up it returns an error rather than
// sending to nothing. A real phone returns an error.
func (d *Device) Biometric(ctx context.Context, action string) (BiometricStatus, error) {
	var out BiometricStatus
	err := d.data(ctx, "app_biometric", map[string]any{"action": action}, &out)
	return out, err
}

// HitTestResult is HitTest's answer when the touch reaches its target.
type HitTestResult struct {
	Target string `json:"target"`
	// X and Y are the point Tap would touch, in device pixels.
	X       int  `json:"x"`
	Y       int  `json:"y"`
	Reaches bool `json:"reaches"`
}

// HitTest asks UIKit's own hit test, below accessibility, whether a tap on
// target would reach it. It returns an error when the touch would go
// elsewhere, naming what would take it and whether accessibility can see
// it. iOS only, and opt-in: it attaches lldb to the app, about two seconds
// on a simulator and nine on an iPhone, where the app must be a development
// build.
func (d *Device) HitTest(ctx context.Context, target string) (HitTestResult, error) {
	var out HitTestResult
	err := d.data(ctx, "app_hit_test", map[string]any{"target": target}, &out)
	return out, err
}

// Hook calls a hook the app registered with Mobium's gray-box library, by
// name, and returns what it answered, decoded from JSON — sign in, seed
// data, raise a toast, without walking the UI. The app must have been
// launched with LaunchWithGrayBox. A name the app never registered is an
// ErrInvalidArgument naming the ones it did.
func (d *Device) Hook(ctx context.Context, name string, args ...string) (any, error) {
	if args == nil {
		args = []string{}
	}
	var out struct {
		Result any `json:"result"`
	}
	err := d.data(ctx, "app_hook", map[string]any{"hook": name, "args": args}, &out)
	return out.Result, err
}

// AuditFinding is one thing the platform's accessibility audit found.
type AuditFinding struct {
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
	Element string `json:"element,omitempty"`
	// Locator names the element, when it has a test id or a label.
	Locator string `json:"locator,omitempty"`
	// Bounds are device pixels, or nil when the audit names no element.
	Bounds *Bounds `json:"bounds,omitempty"`
}

// AuditResult is Audit's answer.
type AuditResult struct {
	Findings []AuditFinding `json:"findings"`
}

// Audit runs the platform's own accessibility audit on the screen in front:
// on iOS, Apple's, on a simulator or an iPhone (iOS 17 and later). Android
// refuses: its audits run inside the app.
func (d *Device) Audit(ctx context.Context) (AuditResult, error) {
	var out AuditResult
	err := d.data(ctx, "app_audit", map[string]any{}, &out)
	return out, err
}

// AppStatus is one app's state, from AppState.
type AppStatus struct {
	// State is "not_installed", "not_running", "background" or "foreground".
	State string `json:"state"`
	// Suspended is, for a background app on iOS, whether it is suspended;
	// nil on Android, which has nothing that says.
	Suspended *bool `json:"suspended,omitempty"`
	// CoveredBy is, for an app in front, the process whose window is over
	// it — a permission prompt's.
	CoveredBy string `json:"covered_by,omitempty"`
}

// AppState reports one app's state, for any app: not installed, not
// running, in the background or in front.
func (d *Device) AppState(ctx context.Context, app string) (AppStatus, error) {
	var out AppStatus
	err := d.data(ctx, "app_state", map[string]any{"app": app}, &out)
	return out, err
}

// Background sends the app in front away for seconds and brings it back,
// resumed rather than relaunched, confirmed in front again. An empty app
// means the one in front. At most 180 seconds.
func (d *Device) Background(ctx context.Context, seconds float64, app string) error {
	args := map[string]any{"seconds": seconds}
	if app != "" {
		args["app"] = app
	}
	return d.act(ctx, "app_background", args)
}

// OpenURL opens a URL or deep link — the quickest way to a specific screen —
// and returns the app that ended up in the foreground.
func (d *Device) OpenURL(ctx context.Context, url string) (string, error) {
	var out struct {
		App string `json:"app"`
	}
	if err := d.data(ctx, "app_open_url", map[string]any{"url": url}, &out); err != nil {
		return "", err
	}
	return out.App, nil
}

// -- permissions -----------------------------------------------------------

// Grant allows permissions up front, so no dialog blocks the flow.
//
// Names are cross-platform ("camera", "location", "contacts", …); "all"
// grants everything the app declares, and a platform name such as
// "android.permission.NFC" also works. On Android the result is verified by
// reading the state back, because `pm grant` reports success for permissions
// the app never declared.
func (d *Device) Grant(ctx context.Context, app string, permissions ...string) error {
	return d.act(ctx, "app_grant", map[string]any{"app": app, "permissions": permissions})
}

// Revoke denies permissions, to test how the app behaves without them.
func (d *Device) Revoke(ctx context.Context, app string, permissions ...string) error {
	return d.act(ctx, "app_revoke", map[string]any{"app": app, "permissions": permissions})
}

// ResetPermissions puts permissions back to their defaults, so the app
// prompts again on next use.
//
// Naming an app resets only that app's, on both platforms; on Android that
// stops the app if it had a permission granted. Pass "" to reset every app
// on the device.
func (d *Device) ResetPermissions(ctx context.Context, app string) error {
	args := map[string]any{}
	if app != "" {
		args["app"] = app
	}
	return d.act(ctx, "app_reset_permissions", args)
}

// Appearance reads the light/dark setting, or changes it and returns the new
// one.
//
// mode is "light", "dark", or "auto" (Android only; iOS returns an error).
// Pass "" to read without changing. Dark mode is a different rendering of
// every screen, so a flow is worth running in both; changing it discards the
// refs from the last Map.
func (d *Device) Appearance(ctx context.Context, mode string) (string, error) {
	args := map[string]any{}
	if mode != "" {
		args["appearance"] = mode
	}
	var out struct {
		Appearance string `json:"appearance"`
	}
	if err := d.data(ctx, "app_appearance", args, &out); err != nil {
		return "", err
	}
	return out.Appearance, nil
}

// Accessibility reads every accessibility setting the device has, by name —
// reduce_motion, bold_text, increase_contrast and the rest; see
// SetAccessibility. A setting the platform lacks is simply absent.
func (d *Device) Accessibility(ctx context.Context) (map[string]string, error) {
	var out struct {
		Settings map[string]string `json:"settings"`
	}
	if err := d.data(ctx, "app_accessibility", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Settings, nil
}

// AccessibilitySetting reads one accessibility setting.
func (d *Device) AccessibilitySetting(ctx context.Context, setting string) (string, error) {
	var out struct {
		Value string `json:"value"`
	}
	if err := d.data(ctx, "app_accessibility", map[string]any{"setting": setting}, &out); err != nil {
		return "", err
	}
	return out.Value, nil
}

// SetAccessibility changes one accessibility setting for the rest of the
// session and returns its new value, confirmed by reading it back; the device
// is put back as it was when the session ends. A switch takes "on" or "off";
// text_size a category such as "accessibility-large" (iOS); text_scale a
// number such as "1.3" (Android). A real iPhone refuses. Changing one
// discards the refs from the last Map.
func (d *Device) SetAccessibility(ctx context.Context, setting, value string) (string, error) {
	var out struct {
		Value string `json:"value"`
	}
	if err := d.data(ctx, "app_accessibility", map[string]any{"setting": setting, "value": value}, &out); err != nil {
		return "", err
	}
	return out.Value, nil
}

// Screen is one device's screen, and what is wrong with the layout on it.
type Screen struct {
	// WidthPx and HeightPx are device pixels, on both platforms — what map
	// bounds, taps and screenshots are in. They were read as "width" and
	// "height", which the daemon never sent, so every Screen was 0x0.
	WidthPx  int `json:"width_px"`
	HeightPx int `json:"height_px"`
	// DPI is Android's density. Zero on iOS, where bounds are device pixels
	// and there is no density to read.
	DPI int `json:"dpi,omitempty"`
	// WidthDP and HeightDP are what a layout is chosen by — dp on Android,
	// points on iOS — and SmallestWidthDP is Android's `sw` qualifier, the
	// shorter edge in dp. Zero when the conversion is unknown.
	WidthDP         int `json:"width_dp,omitempty"`
	HeightDP        int `json:"height_dp,omitempty"`
	SmallestWidthDP int `json:"smallest_width_dp,omitempty"`

	PhysicalWidthPx  int `json:"physical_width_px"`
	PhysicalHeightPx int `json:"physical_height_px"`
	PhysicalDPI      int `json:"physical_dpi,omitempty"`

	// Overridden says the device is pretending, which is a state somebody has
	// to put back with Screen(ctx, "reset", false).
	Overridden bool `json:"overridden"`
	// Applied names what the call did, empty when it only read.
	Applied string `json:"applied,omitempty"`

	Profiles []string  `json:"profiles"`
	Findings []Finding `json:"findings,omitempty"`
	Device   string    `json:"device"`
}

// Finding is one thing wrong with a layout at one screen size.
type Finding struct {
	// Kind is "overflow", "tiny-target", "truncated" or "unlabeled".
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Detail  string `json:"detail"`
	Locator string `json:"locator,omitempty"`
	// Path identifies the element within this snapshot when it gave no
	// locator. It does not survive a change of screen.
	Path   string `json:"path,omitempty"`
	Bounds Bounds `json:"bounds"`
}

// Screen reads the screen, or makes an Android device pretend to be another.
//
// A flow that works on the screen you happen to have is a flow tested once.
// Pass a profile name to apply it, or "reset" to put the device back — an
// override outlives this session. Applying one discards the refs from the last
// map, because nothing is where it was. The answer always lists the profiles
// this platform knows.
//
// On iOS the screen is fixed when the simulator is created, so this reads only
// and names the simulator to boot instead.
//
// With inspect, the answer also carries Findings. Treat a tiny-target finding
// as worth a look rather than a defect: Android can enlarge a tap area without
// changing an element's bounds.
func (d *Device) Screen(ctx context.Context, profile string, inspect bool) (Screen, error) {
	args := map[string]any{}
	if profile != "" {
		args["profile"] = profile
	}
	if inspect {
		args["inspect"] = true
	}
	var out Screen
	if err := d.data(ctx, "app_screen", args, &out); err != nil {
		return Screen{}, err
	}
	return out, nil
}

// Orientation reports which way the screen is turned and whether that is
// pinned. A screen that merely happens to be portrait can rotate under you, so
// the two are separate answers.
func (d *Device) Orientation(ctx context.Context) (mode string, locked bool, err error) {
	var out struct {
		Orientation string `json:"orientation"`
		Locked      bool   `json:"locked"`
	}
	if err := d.data(ctx, "app_orientation", map[string]any{}, &out); err != nil {
		return "", false, err
	}
	return out.Orientation, out.Locked, nil
}

// SetOrientation turns the screen and pins it there. Pass "auto" to hand it
// back to the sensor.
//
// A rotation re-lays out every screen, so the refs from the last Map are
// discarded — their bounds describe a layout that no longer exists. An
// activity that locks its own orientation cannot be turned from outside, and
// that returns an error rather than reporting success.
func (d *Device) SetOrientation(ctx context.Context, mode string) error {
	var out struct{}
	return d.data(ctx, "app_orientation", map[string]any{"orientation": mode}, &out)
}

// AppLocale reports the language tags pinned for an app. Empty means it
// follows the device.
func (d *Device) AppLocale(ctx context.Context, appID string) ([]string, error) {
	var out struct {
		Locales []string `json:"locales"`
	}
	if err := d.data(ctx, "app_locale", map[string]any{"app": appID}, &out); err != nil {
		return nil, err
	}
	return out.Locales, nil
}

// SetAppLocale runs one app in a chosen language. Pass no tags to follow the
// device again. Android 13 and later.
//
// What this confirms is that the device stored the tag, not that the app has a
// translation for it — Android reports no difference between the two, so check
// the screen. Relaunch the app for it to re-render.
func (d *Device) SetAppLocale(ctx context.Context, appID string, tags ...string) error {
	var out struct{}
	return d.data(ctx, "app_locale", map[string]any{
		"app": appID, "locale": strings.Join(tags, ","),
	}, &out)
}

// Rotate turns two fingers about an element or the middle of the screen,
// positive degrees clockwise.
//
// Like Zoom it reports that the gesture was delivered and nothing more, and
// this one is harder still to confirm: nothing in either hierarchy reports a
// rotation, and there is no WebView property to ask either.
func (d *Device) Rotate(ctx context.Context, degrees float64, target string) error {
	args := map[string]any{"degrees": degrees}
	if target != "" {
		args["target"] = target
	}
	var out struct{}
	return d.data(ctx, "app_rotate", args, &out)
}

// Zoom pinches two fingers apart, or together when in is false, about an
// element or the middle of the screen.
//
// It reports that the gesture was delivered and nothing more: neither platform
// exposes a zoom level in the accessibility hierarchy, so confirming a zoom
// means asking whatever was zoomed. A WebView can answer with
// visualViewport.scale through Eval.
func (d *Device) Zoom(ctx context.Context, in bool, target string) error {
	args := map[string]any{"direction": "out"}
	if in {
		args["direction"] = "in"
	}
	if target != "" {
		args["target"] = target
	}
	var out struct{}
	return d.data(ctx, "app_zoom", args, &out)
}

// Check puts a checkbox or switch into a state, rather than toggling it.
//
// Idempotent: asking for a state it is already in does nothing, which is what
// makes it safe to call without reading first. Anything with no checked state
// is refused rather than tapped, and a radio cannot be unchecked — a group is
// cleared by choosing a different member.
func (d *Device) Check(ctx context.Context, target string, checked bool) error {
	var out struct{}
	return d.data(ctx, "app_check", map[string]any{
		"target": target, "checked": checked,
	}, &out)
}

// Alert reports what a system dialog says, or "" when none is up.
//
// A permission prompt is another process's window, not the app's. Reading it
// needs no knowledge of what the buttons say, which is the point.
func (d *Device) Alert(ctx context.Context) (string, error) {
	var out struct {
		Text string `json:"text"`
	}
	if err := d.data(ctx, "app_alert", map[string]any{}, &out); err != nil {
		return "", err
	}
	return out.Text, nil
}

// AnswerAlert accepts or dismisses a system dialog.
//
// These answer a dialog; they do not choose an outcome. On a permission prompt
// they do not mean grant and deny, and on iOS they are the other way round —
// accept leaves it denied, dismiss leaves it granted, because W3C accept
// presses the affirmative button and Apple puts "Don't Allow" last. Tap the
// button by ref if you need a particular answer.
func (d *Device) AnswerAlert(ctx context.Context, accept bool) error {
	action := "dismiss"
	if accept {
		action = "accept"
	}
	var out struct{}
	return d.data(ctx, "app_alert", map[string]any{"action": action}, &out)
}

// AnswerPrompt types text into a dialog's field, then accepts or dismisses it,
// in one call. A plain alert has no field, and the platform refuses the text.
// What accept and dismiss press is as for AnswerAlert.
func (d *Device) AnswerPrompt(ctx context.Context, text string, accept bool) error {
	action := "dismiss"
	if accept {
		action = "accept"
	}
	var out struct{}
	return d.data(ctx, "app_alert", map[string]any{"action": action, "text": text}, &out)
}

// Clipboard reads the device clipboard.
//
// iOS only. On Android 10 and later only an app with focus may read the
// clipboard and the UiAutomator2 server has no activity, so it would answer
// "empty" for a clipboard that is full — this returns an error there rather
// than that.
func (d *Device) Clipboard(ctx context.Context) (string, error) {
	var out struct {
		Text string `json:"text"`
	}
	if err := d.data(ctx, "app_clipboard", map[string]any{}, &out); err != nil {
		return "", err
	}
	return out.Text, nil
}

// SetClipboard writes the device clipboard. On iOS the write is confirmed by
// reading it back; on Android it is reported as sent, since nothing there can
// read it. Paste into a field to check an Android write.
func (d *Device) SetClipboard(ctx context.Context, text string) error {
	var out struct{}
	return d.data(ctx, "app_clipboard", map[string]any{"text": text}, &out)
}

// Location is where the device believes it is.
//
// Mock says the fix was injected; Mocking says a test provider is installed
// now. They differ after ClearLocation, because Android keeps the last known
// position after the provider that supplied it is gone.
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Mock      bool    `json:"mock"`
	Mocking   bool    `json:"mocking"`
	// Known is false when the platform cannot report a position at all, as on
	// iOS, which is different from the device having none.
	Known bool `json:"known"`
}

// Location reports where the device is. Android only: `simctl location` has no
// `get`, so on iOS this returns an error saying so rather than a position it
// never read.
func (d *Device) Location(ctx context.Context) (Location, error) {
	var out Location
	if err := d.data(ctx, "app_location", map[string]any{}, &out); err != nil {
		return Location{}, err
	}
	return out, nil
}

// SetLocation places the device at a coordinate.
//
// On Android this goes through a test provider, is read back, and works on
// real hardware. On iOS it goes through simctl and cannot be confirmed — the
// call succeeding means the request was accepted, not that an app will read it.
func (d *Device) SetLocation(ctx context.Context, lat, lon float64) error {
	var out struct{}
	return d.data(ctx, "app_location", map[string]any{
		"latitude": lat, "longitude": lon,
	}, &out)
}

// ClearLocation removes the injected position.
//
// It does not clear the device's last known location, which Android caches, so
// a Location straight afterwards still reports the injected fix with Mocking
// false.
func (d *Device) ClearLocation(ctx context.Context) error {
	var out struct{}
	return d.data(ctx, "app_location", map[string]any{"clear": true}, &out)
}

// FollowRoute moves the device along waypoints at speedMPS meters per second.
// Two or more points; pass 0 for the default speed.
//
// On iOS the simulator interpolates the route itself. On Android the daemon
// steps a test provider once a second, since the platform has no route
// command — either way this returns as soon as the route starts.
func (d *Device) FollowRoute(ctx context.Context, points [][2]float64, speedMPS float64) error {
	wp := make([]any, 0, len(points))
	for _, p := range points {
		wp = append(wp, []any{p[0], p[1]})
	}
	args := map[string]any{"waypoints": wp}
	if speedMPS > 0 {
		args["speed"] = speedMPS
	}
	var out struct{}
	return d.data(ctx, "app_location", args, &out)
}

// FollowGPX follows a GPX file, reading it on the machine running the daemon.
func (d *Device) FollowGPX(ctx context.Context, path string, speedMPS float64) error {
	args := map[string]any{"gpx": path}
	if speedMPS > 0 {
		args["speed"] = speedMPS
	}
	var out struct{}
	return d.data(ctx, "app_location", args, &out)
}

// Press sends a hardware button: "back", "home", "recents", "volume-up" or
// "volume-down"; a remote's D-pad, "dpad-up", "dpad-down", "dpad-left",
// "dpad-right" and "select"; or a media key, "play-pause", "stop", "next",
// "previous", "rewind" or "fast-forward". A D-pad press reports where focus
// went.
//
// On Android back is primary navigation. iOS has no back button by design and
// returns an error saying what to do instead, rather than sending an edge
// swipe — a different event that an app can tell apart. Any press can move the
// screen, so the refs from the last Map are discarded.
func (d *Device) Press(ctx context.Context, button string) error {
	var out struct{}
	return d.data(ctx, "app_press", map[string]any{"button": button}, &out)
}

// BackResult is what a back did.
type BackResult struct {
	// Foreground is the app in front afterwards.
	Foreground string `json:"foreground"`
	// Left is the app back took the user out of; empty when it stayed in
	// front and back went somewhere inside it.
	Left string `json:"left"`
	// Title is what an iOS navigation bar says afterwards.
	Title string `json:"title"`
	// Confirmed says the outcome was read rather than assumed.
	Confirmed bool `json:"confirmed"`
}

// Back goes back and says where it went: whether it left the app, and on
// iOS what the navigation bar says. With gesture it is a swipe in from the
// left edge rather than the key — the only back iOS has, and refused on an
// Android device that navigates with buttons.
func (d *Device) Back(ctx context.Context, gesture bool) (BackResult, error) {
	var out BackResult
	args := map[string]any{"button": "back"}
	if gesture {
		args["gesture"] = true
	}
	err := d.data(ctx, "app_press", args, &out)
	return out, err
}

// ScreenLocked reports whether the screen is locked.
func (d *Device) ScreenLocked(ctx context.Context) (bool, error) {
	var out struct {
		Locked bool `json:"locked"`
	}
	if err := d.data(ctx, "app_lock", map[string]any{}, &out); err != nil {
		return false, err
	}
	return out.Locked, nil
}

// SetScreenLocked locks or unlocks the screen and confirms it.
//
// A state rather than a power-button press: power is a toggle, so asking twice
// leaves the device where it started. A device with a PIN, pattern or password
// cannot be unlocked from outside and returns an error rather than pretending.
func (d *Device) SetScreenLocked(ctx context.Context, locked bool) error {
	state := "unlock"
	if locked {
		state = "lock"
	}
	var out struct{}
	return d.data(ctx, "app_lock", map[string]any{"state": state}, &out)
}

// IncomingCall drives a simulated incoming call: "ring", "accept" or "hang",
// and "" is "ring". number is the caller's; "" leaves the emulator's default.
// Emulator only — a real phone cannot be made to ring from outside.
//
// Not called Call: that is already the raw tool-call escape hatch below.
func (d *Device) IncomingCall(ctx context.Context, action, number string) error {
	args := map[string]any{}
	if action != "" {
		args["action"] = action
	}
	if number != "" {
		args["number"] = number
	}
	var out struct{}
	return d.data(ctx, "app_call", args, &out)
}

// SendSMS delivers a simulated text message. Emulator only.
func (d *Device) SendSMS(ctx context.Context, from, text string) error {
	args := map[string]any{"text": text}
	if from != "" {
		args["from"] = from
	}
	var out struct{}
	return d.data(ctx, "app_sms", args, &out)
}

// Doctor checks the environment and returns its report.
//
// Needs no device: its whole job is to be runnable when nothing works yet, so
// it is the first thing to call when something fails for a reason that makes
// no sense.
func (d *Device) Doctor(ctx context.Context) (string, error) {
	// The report is the tool's text, as `mobium doctor` prints it. This read
	// a "report" field the tool never sends, and so returned "" every time.
	res, err := d.conn.call(ctx, "app_doctor", map[string]any{})
	if err != nil {
		return "", err
	}
	return res.text(), nil
}

// ConsoleEntry is one line a page logged.
type ConsoleEntry struct {
	Level string `json:"level"`
	Text  string `json:"text"`
	Time  int64  `json:"time"`
}

// Logs returns what the current WebView has written to its console since the
// last call, including uncaught errors and unhandled promise rejections.
// Pass "" for every level.
//
// Each read drains what it returns, so it reports what happened since the last
// call — which is what makes "nothing was logged during this step" assertable.
// Capture starts when the context is entered, so a page's initial load is
// already over by then.
func (d *Device) Logs(ctx context.Context, level string) ([]ConsoleEntry, error) {
	// Named, because with no source the tool follows the context and would
	// read the device log on the native shell — entries of another shape.
	args := map[string]any{"source": "webview"}
	if level != "" {
		args["level"] = level
	}
	var out struct {
		Entries []ConsoleEntry `json:"entries"`
	}
	if err := d.data(ctx, "app_logs", args, &out); err != nil {
		return nil, err
	}
	return out.Entries, nil
}

// DeviceLogEntry is one line of the device's own log.
type DeviceLogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Tag     string `json:"tag,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Message string `json:"message"`
}

// DeviceLogs reads the device's own log — logcat on Android, the unified log
// on an iOS simulator — since the last read. The first read returns the most
// recent lines. An empty app reads the whole device; an empty level keeps
// every level; lines of zero takes the tool's default.
//
// skipped counts lines newer than the last read that the limit dropped. They
// will not come back, so a caller waiting for one line should narrow by app
// or level rather than read again.
func (d *Device) DeviceLogs(ctx context.Context, app, level string, lines int) (entries []DeviceLogEntry, skipped int, err error) {
	args := map[string]any{"source": "device"}
	if app != "" {
		args["app"] = app
	}
	if level != "" {
		args["level"] = level
	}
	if lines > 0 {
		args["lines"] = lines
	}
	var out struct {
		Entries []DeviceLogEntry `json:"entries"`
		Skipped int              `json:"skipped"`
	}
	if err := d.data(ctx, "app_logs", args, &out); err != nil {
		return nil, 0, err
	}
	return out.Entries, out.Skipped, nil
}

// Recording is what app_record reports: whether one is running, and on stop
// the saved file's frames and duration, read from its own header.
type Recording struct {
	Recording bool   `json:"recording"`
	Path      string `json:"path,omitempty"`
	Frames    int    `json:"frames,omitempty"`
	// Duration and Elapsed are nanoseconds.
	Duration int64 `json:"duration,omitempty"`
	Elapsed  int64 `json:"elapsed,omitempty"`
}

// Record starts ("start") or stops ("stop", saving to path) a screen
// recording, or with an empty action asks whether one is running. A relative
// path is this process's: `mobium pipe` resolves it before the daemon sees it.
func (d *Device) Record(ctx context.Context, action, path string) (Recording, error) {
	args := map[string]any{}
	if action != "" {
		args["action"] = action
	}
	if path != "" {
		args["path"] = path
	}
	var out Recording
	err := d.data(ctx, "app_record", args, &out)
	return out, err
}

// Trace is what app_trace reports: whether a trace is running on the device,
// how many calls it holds and for how long, and on stop where the zip went
// and its size. Elapsed is a duration as Go prints one, "1m2.5s".
type Trace struct {
	Device  string `json:"device"`
	Tracing bool   `json:"tracing"`
	Calls   int    `json:"calls"`
	Elapsed string `json:"elapsed,omitempty"`
	Path    string `json:"path,omitempty"`
	Bytes   int    `json:"bytes,omitempty"`
}

// TraceOptions tunes TraceStart. A nil *TraceOptions keeps a screenshot after
// every call with the map drawn over it, under no title.
type TraceOptions struct {
	// Name is the trace's title, as the viewers show it.
	Name string
	// NoScreenshots keeps no screen after each call. On a real phone the
	// screenshots are its owner's screen, which is the reason to set it.
	NoScreenshots bool
	// NoMaps leaves the map's elements off each screenshot.
	NoMaps bool
}

// TraceStart records every call on this device from now until TraceStop as a
// step: before and after, the point an action touched, a failure's error, and
// after each the screen with the map's elements drawn over it. Text typed
// into a field is not recorded, only its length. One trace per device, and
// ending the session discards it.
func (d *Device) TraceStart(ctx context.Context, opts *TraceOptions) (*Trace, error) {
	if opts == nil {
		opts = &TraceOptions{}
	}
	args := map[string]any{"action": "start"}
	if opts.Name != "" {
		args["name"] = opts.Name
	}
	if opts.NoScreenshots {
		args["screenshots"] = false
	}
	if opts.NoMaps {
		args["maps"] = false
	}
	var out Trace
	if err := d.data(ctx, "app_trace", args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TraceStop ends the trace and saves it to path as a zip in Vibium's record
// format, which player.vibium.dev opens. A relative path is this process's: `mobium pipe` resolves it before the
// daemon sees it.
func (d *Device) TraceStop(ctx context.Context, path string) (*Trace, error) {
	if path == "" {
		return nil, &Error{Tool: "app_trace", Code: CodeInvalidArgument,
			Reason: "TraceStop needs a path to save the trace to, a .zip"}
	}
	var out Trace
	if err := d.data(ctx, "app_trace", map[string]any{"action": "stop", "path": path}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TraceStatus asks whether a trace is running on this device, and if so how
// many calls it holds so far.
func (d *Device) TraceStatus(ctx context.Context) (*Trace, error) {
	var out Trace
	if err := d.data(ctx, "app_trace", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// KeyboardField is the field with keyboard focus. A password's Value is
// masked, never the password.
type KeyboardField struct {
	ID       string `json:"id,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Value    string `json:"value"`
	Password bool   `json:"password,omitempty"`
	Empty    bool   `json:"empty,omitempty"`
}

// KeyboardState is the soft keyboard after an app_keyboard call.
type KeyboardState struct {
	Shown   bool           `json:"shown"`
	Focused *KeyboardField `json:"focused,omitempty"`
	Hidden  bool           `json:"hidden,omitempty"`
}

// KeyboardOptions says what to do with the keyboard. The zero value reads it.
type KeyboardOptions struct {
	// Text is added to the end of the focused field and confirmed. Nil types
	// nothing; a pointer so "" can be told apart from no text.
	Text *string
	// Key is "enter", "delete" or "space", pressed after any text.
	Key string
	// Hide hides the keyboard, confirmed. It cannot be combined with Text or Key.
	Hide bool
}

// Keyboard reads, types into, presses a key on, or hides the soft keyboard.
// It fails with ErrNoSuchElement when text is given and nothing has focus.
func (d *Device) Keyboard(ctx context.Context, opts KeyboardOptions) (KeyboardState, error) {
	args := map[string]any{}
	if opts.Text != nil {
		args["text"] = *opts.Text
	}
	if opts.Key != "" {
		args["key"] = opts.Key
	}
	if opts.Hide {
		args["hide"] = true
	}
	var out KeyboardState
	err := d.data(ctx, "app_keyboard", args, &out)
	return out, err
}

// CrashReport is one crash the device recorded.
type CrashReport struct {
	ID      string `json:"id"`
	Time    string `json:"time"`
	Kind    string `json:"kind"`
	App     string `json:"app,omitempty"`
	Summary string `json:"summary,omitempty"`
	// Text is the whole report, filled only by Crash.
	Text string `json:"text,omitempty"`
}

// Crashes lists the crashes the device recorded, newest first. An empty app
// lists every process's; limit of zero takes the tool's default.
func (d *Device) Crashes(ctx context.Context, app string, limit int) ([]CrashReport, error) {
	args := map[string]any{}
	if app != "" {
		args["app"] = app
	}
	if limit > 0 {
		args["limit"] = limit
	}
	var out struct {
		Crashes []CrashReport `json:"crashes"`
	}
	if err := d.data(ctx, "app_crashes", args, &out); err != nil {
		return nil, err
	}
	return out.Crashes, nil
}

// Crash reads one crash report in full, by an id from Crashes.
func (d *Device) Crash(ctx context.Context, id string) (CrashReport, error) {
	var out struct {
		Crashes []CrashReport `json:"crashes"`
	}
	if err := d.data(ctx, "app_crashes", map[string]any{"id": id}, &out); err != nil {
		return CrashReport{}, err
	}
	if len(out.Crashes) == 0 {
		return CrashReport{}, &Error{Tool: "app_crashes", Code: CodeInternal, Reason: "answered an id with no report"}
	}
	return out.Crashes[0], nil
}

// Eval runs a JavaScript expression in the current WebView and returns its
// value as a string. Objects come back as JSON.
func (d *Device) Eval(ctx context.Context, expression string) (string, error) {
	var out struct {
		Value string `json:"value"`
	}
	if err := d.data(ctx, "app_eval", map[string]any{"expression": expression}, &out); err != nil {
		return "", err
	}
	return out.Value, nil
}

// Cookie is one of a page's cookies. The JSON keys are Vibium's, so a
// storage state Vibium saved restores here.
type Cookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain,omitempty"`
	Path   string `json:"path,omitempty"`
	// Expires is seconds since the epoch; 0 is a session cookie.
	Expires  float64 `json:"expires,omitempty"`
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	// SameSite is "Strict", "Lax" or "None"; empty leaves the browser's own.
	SameSite string `json:"sameSite,omitempty"`
}

// StorageItem is one key of localStorage or sessionStorage.
type StorageItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// OriginStorage is one origin's web storage.
type OriginStorage struct {
	Origin         string        `json:"origin"`
	LocalStorage   []StorageItem `json:"localStorage"`
	SessionStorage []StorageItem `json:"sessionStorage"`
}

// StorageState is a page's cookies and web storage, in the shape Vibium
// saves.
type StorageState struct {
	Cookies []Cookie        `json:"cookies"`
	Origins []OriginStorage `json:"origins"`
}

// Cookies lists the current WebView's cookies — the ones its page's URL is
// sent, HttpOnly ones included. Needs a web context: Context first.
func (d *Device) Cookies(ctx context.Context) ([]Cookie, error) {
	var out struct {
		Cookies []Cookie `json:"cookies"`
	}
	if err := d.data(ctx, "app_cookies", map[string]any{"action": "get"}, &out); err != nil {
		return nil, err
	}
	return out.Cookies, nil
}

// SetCookies sets each cookie on the current page and reads the store back,
// so one the browser accepted and stored expired fails as NotConfirmed.
func (d *Device) SetCookies(ctx context.Context, cookies ...Cookie) error {
	return d.data(ctx, "app_cookies", map[string]any{"action": "set", "cookies": cookies}, nil)
}

// ClearCookies deletes the current page's cookies, or with a name only those
// called that.
func (d *Device) ClearCookies(ctx context.Context, name ...string) error {
	args := map[string]any{"action": "clear"}
	if len(name) > 0 && name[0] != "" {
		args["name"] = name[0]
	}
	return d.data(ctx, "app_cookies", args, nil)
}

// Storage answers the current page's storage state: its cookies and its
// origin's localStorage and sessionStorage.
func (d *Device) Storage(ctx context.Context) (*StorageState, error) {
	var out struct {
		State StorageState `json:"state"`
	}
	if err := d.data(ctx, "app_storage", map[string]any{"action": "get"}, &out); err != nil {
		return nil, err
	}
	return &out.State, nil
}

// SetStorage restores a saved state: its cookies, and each origin's storage
// into the page only if the page is on that origin.
func (d *Device) SetStorage(ctx context.Context, state StorageState) error {
	return d.data(ctx, "app_storage", map[string]any{"action": "restore", "state": state}, nil)
}

// ClearStorage empties the current page's cookies, localStorage and
// sessionStorage.
func (d *Device) ClearStorage(ctx context.Context) error {
	return d.data(ctx, "app_storage", map[string]any{"action": "clear"}, nil)
}

// Notification is one entry in the shade.
type Notification struct {
	Package string `json:"package"`
	Title   string `json:"title,omitempty"`
	Text    string `json:"text,omitempty"`
	Tag     string `json:"tag,omitempty"`
}

// Notifications lists what is in the shade — how a test asserts an app posted
// what it should.
func (d *Device) Notifications(ctx context.Context) ([]Notification, error) {
	var out struct {
		Notifications []Notification `json:"notifications"`
	}
	if err := d.data(ctx, "app_notifications", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Notifications, nil
}

// PostNotification puts a notification in the shade as an interruption, and
// confirms it arrived by reading the shade back.
func (d *Device) PostNotification(ctx context.Context, title, text string) error {
	args := map[string]any{"text": text}
	if title != "" {
		args["title"] = title
	}
	var out struct{}
	return d.data(ctx, "app_notifications", args, &out)
}

// SetShade opens or closes the notification panel. A notification cannot be
// tapped until the shade is open: until then it is not on screen and Map
// cannot see it. Opening it discards the refs from the last Map.
func (d *Device) SetShade(ctx context.Context, open bool) error {
	state := "close"
	if open {
		state = "open"
	}
	var out struct{}
	return d.data(ctx, "app_notifications", map[string]any{"shade": state}, &out)
}

// Timezone reports the device timezone.
func (d *Device) Timezone(ctx context.Context) (string, error) {
	var out struct {
		Timezone string `json:"timezone"`
	}
	if err := d.data(ctx, "app_timezone", map[string]any{}, &out); err != nil {
		return "", err
	}
	return out.Timezone, nil
}

// SetTimezone changes the device timezone, confirmed by reading it back. Takes
// an IANA name such as "Asia/Tokyo"; an unknown one is accepted by the device
// and ignored, which the check catches. Works on real hardware.
func (d *Device) SetTimezone(ctx context.Context, tz string) error {
	var out struct{}
	return d.data(ctx, "app_timezone", map[string]any{"timezone": tz}, &out)
}

// -- contexts --------------------------------------------------------------

// Contexts lists the automatable contexts: the native shell plus any WebViews.
func (d *Device) Contexts(ctx context.Context) ([]string, error) {
	var out struct {
		Contexts []struct {
			ID string `json:"id"`
		} `json:"contexts"`
	}
	if err := d.data(ctx, "app_contexts", nil, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Contexts))
	for _, c := range out.Contexts {
		names = append(names, c.ID)
	}
	return names, nil
}

// Context switches context, or reads the current one when name is "".
func (d *Device) Context(ctx context.Context, name string) (string, error) {
	args := map[string]any{}
	if name != "" {
		args["context"] = name
	}
	res, err := d.conn.call(ctx, "app_context", args)
	if err != nil {
		return "", err
	}
	return res.text(), nil
}

// -- plumbing --------------------------------------------------------------

// Call runs any tool by name, for anything this package does not wrap yet.
// The tool's structured answer is decoded into out, which may be nil.
func (d *Device) Call(ctx context.Context, tool string, args map[string]any, out any) error {
	return d.data(ctx, tool, args, out)
}

func (d *Device) act(ctx context.Context, tool string, args map[string]any) error {
	_, err := d.conn.call(ctx, tool, args)
	return err
}

func (d *Device) data(ctx context.Context, tool string, args map[string]any, out any) error {
	res, err := d.conn.call(ctx, tool, args)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return res.into(out)
}

func (d *Device) elements(ctx context.Context, tool string, args map[string]any) ([]Element, error) {
	var out struct {
		Elements []Element `json:"elements"`
	}
	if err := d.data(ctx, tool, args, &out); err != nil {
		return nil, err
	}
	return out.Elements, nil
}

// element reads a tool that answers with a single element, which may legally
// be absent — a wait for something to disappear has nothing to return.
func (d *Device) element(ctx context.Context, tool string, args map[string]any) (*Element, error) {
	var out struct {
		Element *Element `json:"element"`
	}
	if err := d.data(ctx, tool, args, &out); err != nil {
		return nil, err
	}
	return out.Element, nil
}

// image lifts the PNG out of a screenshot result.
func (r *toolResult) image() ([]byte, error) {
	for _, c := range r.Content {
		if c.Type == "image" {
			data, err := base64.StdEncoding.DecodeString(c.Data)
			if err != nil {
				return nil, fmt.Errorf("mobium returned an unreadable image: %w", err)
			}
			return data, nil
		}
	}
	return nil, errors.New("mobium returned no image")
}
