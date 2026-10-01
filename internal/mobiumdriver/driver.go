// Package mobiumdriver turns a virtual device into a UI you can read and touch.
//
// Driver is the seam that vibium fills with WebDriver BiDi. Two backends
// implement it today — `uiautomator dump` over adb, which installs nothing,
// and the UiAutomator2 server, which is faster and can do more — and
// WebDriverAgent for the iOS simulator will implement the same interface.
package mobiumdriver

import (
	"context"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// Driver reads and drives one device's UI. Every backend implements this.
type Driver interface {
	// Snapshot captures the current UI hierarchy.
	Snapshot(ctx context.Context) (*uitree.Tree, error)
	// Screenshot captures the screen as PNG bytes.
	Screenshot(ctx context.Context) ([]byte, error)
	// Tap touches a point in device pixels.
	Tap(ctx context.Context, x, y int) error
	// Name identifies the backend in diagnostics.
	Name() string
}

// Gesturer is implemented by backends that can do more than a plain tap.
// Both Android backends can: one through W3C pointer actions, the other
// through `adb shell input`.
type Gesturer interface {
	Swipe(ctx context.Context, x1, y1, x2, y2 int, d time.Duration) error
	LongPress(ctx context.Context, x, y int, d time.Duration) error
}

// DoubleTapper is implemented by backends that can place two taps close
// enough together that the platform reads them as one gesture.
//
// Separate from Driver.Tap, and not merely "call Tap twice", because the
// interval is the gesture. AOSP's ViewConfiguration puts the window between
// DOUBLE_TAP_MIN_TIME (40ms) and DOUBLE_TAP_TIMEOUT (300ms): sooner than the
// first and the second tap is discarded as a bounce, later than the second
// and it is two taps. Nothing above this interface can control that interval,
// so nothing above it should try.
type DoubleTapper interface {
	DoubleTap(ctx context.Context, x, y int) error
}

// Dragger is implemented by backends that can press, hold, move and release
// as one gesture.
//
// Separate from Gesturer.Swipe for the same reason DoubleTapper is separate
// from Tap: the holds are the gesture. A drag-to-reorder list arms on a long
// press, so a move that begins in the same frame as the touch is a fling and
// nothing is ever picked up; and a release in the same frame as the arrival
// is dropped before the target has seen a hover. Swipe has neither hold and
// must not grow them — a swipe that pauses at both ends is no longer a swipe.
type Dragger interface {
	// Drag presses at (x1, y1), holds for `hold`, travels to (x2, y2) over
	// `move`, holds again, and releases. Coordinates are device pixels.
	Drag(ctx context.Context, x1, y1, x2, y2 int, hold, move time.Duration) error
}

// TextEntry is implemented by backends that can put text into a specific
// element.
//
// Deliberately not implemented by the dump backend: `adb shell input text`
// types into whatever holds focus, mangles quotes, spaces and non-ASCII, and
// reports success either way. Refusing is better than silently entering the
// wrong string, so that backend says to switch instead.
type TextEntry interface {
	SetText(ctx context.Context, n *uitree.Node, text string) error
	Clear(ctx context.Context, n *uitree.Node) error
}

// Starter is implemented by backends that need to bring something up before
// use — installing and launching a device-side server, say.
type Starter interface {
	Start(ctx context.Context, progress func(string)) error
	Close() error
}

// Health is implemented by backends holding a connection that can go stale.
//
// Emulator serials are deterministic, so quitting an emulator and starting it
// again hands back the same serial with a different device behind it. Without
// this check a cached session would keep pointing at a server that died with
// the old emulator, and every later command would fail until the daemon was
// restarted.
type Health interface {
	Healthy(ctx context.Context) bool
}

// AppControl is implemented by backends that can manage app lifecycle.
//
// Without it mobium can only drive whatever happens to be on screen, which
// makes "open the app and check the login screen" impossible to express.
// Both platforms support all of it: Android through adb, iOS through simctl.
type AppControl interface {
	// Launch brings an app to the foreground by package or bundle id.
	Launch(ctx context.Context, appID string) error
	// Terminate stops a running app.
	Terminate(ctx context.Context, appID string) error
	// Install adds an app from a local .apk or .app path.
	Install(ctx context.Context, path string) error
	// OpenURL opens a URL or deep link.
	OpenURL(ctx context.Context, url string) error
}

// Permissions is implemented by backends that can decide what an app is
// allowed to do without going through its dialogs.
//
// This exists because a first-launch permission dialog stops automation dead:
// the flow under test is behind it, and tapping "Allow" is both platform- and
// language-specific. Granting up front is how every other tool handles it.
//
// Names here are the platform's own — `android.permission.CAMERA`, or `photos`
// on iOS. The neutral vocabulary is compiled to these one layer up, the same
// way locators are.
type Permissions interface {
	// SetPermission grants or revokes one permission for one app.
	SetPermission(ctx context.Context, appID, permission string, grant bool) error
	// ResetPermissions reverts permissions to their defaults. An empty appID
	// means every app; a backend that cannot scope to one app must return an
	// error for a non-empty appID rather than silently resetting the device.
	ResetPermissions(ctx context.Context, appID string) error
}

// PermissionReader is implemented by backends that can report what an app
// declares and what it currently has.
//
// Android can; iOS cannot — `simctl privacy` writes but never reads. This is
// separate from Permissions for exactly that reason, so a caller can ask
// whether the answer is available rather than being handed a guess.
type PermissionReader interface {
	// PermissionState maps every runtime permission the app declares to
	// whether it is granted.
	PermissionState(ctx context.Context, appID string) (map[string]bool, error)
}

// Pincher is implemented by backends that can put two fingers on the screen.
//
// Separate from Gesturer rather than added to it, because an external driver
// already implements that interface and adding a method would break every one
// of them at once — the capability handshake exists so a backend can say what
// it grew without anything else having to change.
type Pincher interface {
	// Pinch moves two fingers along the horizontal about (cx, cy), from a
	// half-gap of `from` pixels to a half-gap of `to`. Spreading zooms in;
	// closing zooms out. Coordinates are device pixels, like every other
	// gesture here.
	Pinch(ctx context.Context, cx, cy, from, to int, d time.Duration) error
	// Rotate turns two fingers about (cx, cy) at the given radius, by
	// `degrees` — positive clockwise, since that is the direction the screen's
	// y axis makes positive.
	Rotate(ctx context.Context, cx, cy, radius int, degrees float64, d time.Duration) error
}

// Point is a place on the screen, in device pixels.
type Point struct{ X, Y int }

// MultiToucher is implemented by backends that can put several fingers down
// that do *different* things: several tapping at once, or one holding still
// while another taps or moves. Pincher's fingers move as a mirrored pair;
// these do not, which is why this is a separate capability rather than more
// of Pincher.
//
// The gestures are the ones the touch-gesture charts name that the other
// capabilities cannot express: a two- or three-finger tap, press and tap,
// and press and drag. See docs/GESTURES.md.
type MultiToucher interface {
	// MultiTap puts one finger on each point at the same instant and lifts
	// them together — a two-finger tap with two points, three with three.
	MultiTap(ctx context.Context, fingers []Point) error
	// PressTap presses hold with one finger, waits lead, taps tap with a
	// second finger, and lifts the first only after the second is up.
	PressTap(ctx context.Context, hold, tap Point, lead time.Duration) error
	// PressDrag presses hold with one finger, waits lead, then puts a second
	// finger down at from and moves it to to over move, lifting the second
	// before the first.
	PressDrag(ctx context.Context, hold, from, to Point, lead, move time.Duration) error
}

// Alerts is implemented by backends that can answer a system dialog.
//
// A permission prompt is not the app's UI. It is a different window owned by
// another process — `com.google.android.permissioncontroller` on Android,
// SpringBoard on iOS, both measured — so its buttons are reachable through the
// hierarchy but its *identity* is not something the app's tree can tell you.
//
// The W3C alert endpoints answer it without knowing what the buttons say,
// which is the point: a check that taps "While using the app" works until the
// device is in Japanese, and this project runs apps in Japanese on purpose.
type Alerts interface {
	// AlertText reports what the dialog says, or ErrNoAlert if there is none.
	AlertText(ctx context.Context) (string, error)
	// AnswerAlert accepts or dismisses it.
	AnswerAlert(ctx context.Context, accept bool) error
	// SendAlertText types into a prompt's field. An alert with no field has
	// nothing to type into, and the platform says so rather than this
	// guessing.
	SendAlertText(ctx context.Context, text string) error
}

// ErrNoAlert means nothing is asking the user anything. It is an answer, not a
// failure, and callers are expected to tell the two apart.
var ErrNoAlert = errNoAlert

// Clipboard is implemented by backends that can write the device clipboard.
//
// Split from ClipboardReader for the reason Geolocation is split from
// GeolocationReader, and it is worth noticing that the asymmetry runs the
// *other* way here. iOS can read and write; Android can only write, because
// reading requires the requesting app to have focus and the UiAutomator2
// server has no activity. A tool that reported "" for an unreadable clipboard
// would be claiming it is empty, which is a different statement and often a
// false one.
type Clipboard interface {
	// SetClipboard writes the device clipboard.
	SetClipboard(ctx context.Context, text string) error
}

// ClipboardReader is implemented by backends that can also read it back.
type ClipboardReader interface {
	// ClipboardText reports what the device clipboard holds.
	ClipboardText(ctx context.Context) (string, error)
}

// Geolocation is implemented by backends that can place a device at a
// coordinate.
//
// Split from GeolocationReader for the same reason Permissions is split from
// PermissionReader: on iOS the write is real and the state is invisible.
// `simctl location` has set, clear, run and start, and **no get** — so a
// backend that can place a device is not necessarily one that can say where it
// is, and a caller has to be able to ask which it has rather than assume.
type Geolocation interface {
	// SetLocation places the device at a coordinate and confirms what it can.
	SetLocation(ctx context.Context, lat, lon float64) error
	// ClearLocation returns the device to reporting its own position.
	ClearLocation(ctx context.Context) error
}

// GeolocationReader is implemented by backends that can also report where a
// device currently is.
//
// The `Mock` field is the load-bearing part rather than the coordinates: a
// device holds its last position across boots, so reading back the place you
// just asked for proves nothing unless it also says the fix was injected.
type GeolocationReader interface {
	// Location reports the device's position, or nil if it holds none.
	Location(ctx context.Context) (*device.Fix, error)
}

// RouteRunner is implemented by backends that can follow a series of
// waypoints *themselves*.
//
// simctl interpolates natively, so on iOS the motion is handed over and
// nothing here keeps running. Android has no such command — the Extended
// Controls panel imports GPX, and the console does not — so there the tool
// layer steps a test provider on a timer instead. One route concept, two
// transports: the same seam as everything else here, and the reason this is a
// capability rather than a second tool.
type RouteRunner interface {
	// StartRoute begins moving along the waypoints at the given speed in
	// meters per second, and returns once the platform has taken ownership.
	StartRoute(ctx context.Context, pts []device.Point, speedMPS float64) error
}

// Appearance is implemented by backends that can switch a device between
// light and dark mode.
//
// Both platforms can, and both can be read back, which is what makes this
// worth exposing: a tool that sets dark mode and cannot confirm it would be
// one more thing that succeeds while doing nothing.
type Appearance interface {
	// Appearance reports the current mode, in Mobium's vocabulary.
	Appearance(ctx context.Context) (string, error)
	// SetAppearance switches mode, in Mobium's vocabulary, and confirms it.
	SetAppearance(ctx context.Context, mode string) error
}

// Accessibility is implemented by backends that can read and change the
// device's accessibility settings, in device's shared vocabulary.
type Accessibility interface {
	// AccessibilitySetting reads one setting.
	AccessibilitySetting(ctx context.Context, name string) (string, error)
	// SetAccessibilitySetting changes one, confirms it, and returns how to
	// put it back exactly as it was found.
	SetAccessibilitySetting(ctx context.Context, name, value string) (device.AXUndo, error)
}

// Orientation is implemented by backends that can read and change which way
// the screen is turned.
//
// Worth having because a rotation is not a cosmetic change: it re-lays out
// every screen, and a layout that only exists in landscape is a layout nothing
// has ever tested. Both platforms can do it, and both can be read back — which
// is what makes it worth exposing at all, rather than a command that reports
// success and moves nothing.
//
// The vocabulary is Mobium's, compiled per platform the same way locators are.
type Orientation interface {
	// Orientation reports which way the screen is turned, and whether that is
	// pinned or following the sensor.
	Orientation(ctx context.Context) (mode string, locked bool, err error)
	// SetOrientation turns the screen and confirms it moved. The value
	// "auto" hands rotation back to the sensor.
	SetOrientation(ctx context.Context, mode string) error
}

// Localization is implemented by backends that can run one app in a chosen
// language without disturbing the rest of the device.
//
// Per-app rather than device-wide deliberately. A device-wide change means a
// framework restart on Android and moves every app at once; per-app is
// reversible, immediate, and readable back. It is also the question actually
// being asked — "does this screen work in Japanese" — rather than a proxy for
// it.
//
// What can be confirmed is that the device stored the language, not that the
// app has a translation for it. Neither platform reports the difference, and
// implementations must say so rather than implying the stronger claim.
type Localization interface {
	// AppLocales reports the language tags pinned for an app. Empty means it
	// follows the device.
	AppLocales(ctx context.Context, appID string) ([]string, error)
	// SetAppLocales pins an app's language, or clears it with an empty list,
	// and confirms the device stored it.
	SetAppLocales(ctx context.Context, appID string, tags []string) error
	// DeviceLocale reports what an unpinned app follows.
	DeviceLocale(ctx context.Context) (string, error)
}

// AppInventory is implemented by backends that can say what is installed and
// remove it.
//
// Separate from AppControl because the questions differ: AppControl drives an
// app you already know about, this one answers "what is on here" and "take it
// off". Both platforms can do both.
type AppInventory interface {
	// ListApps reports installed apps. Without includeSystem it reports only
	// what someone installed, which is almost always the question.
	ListApps(ctx context.Context, includeSystem bool) ([]device.InstalledApp, error)
	// Uninstall removes an app.
	Uninstall(ctx context.Context, appID string) error
}

// DeviceLogs is implemented by backends that can read the device's own log —
// logcat, or the iOS unified log — as opposed to a WebView's console, which
// the webview package reads and which is only the page's.
type DeviceLogs interface {
	DeviceLogs(ctx context.Context, q device.LogQuery) (device.LogResult, error)
}

// CrashReports is implemented by backends that can read the crashes a device
// recorded. Separate from DeviceLogs because the sources differ and so does
// what they keep: a log is a ring that noise overruns, while a crash report is
// kept on its own, across reboots, until it ages out.
type CrashReports interface {
	// Crashes lists recorded crashes newest first, without their full text.
	// An empty app lists every process's.
	Crashes(ctx context.Context, app string) ([]device.CrashReport, error)
	// Crash reads one report in full, by the id Crashes gave it.
	Crash(ctx context.Context, id string) (device.CrashReport, error)
}

// Keyboard is implemented by backends that can read and drive the soft
// keyboard: whether it is up, which field it is typing into, typing at that
// field's cursor, a named key, and hiding it — each confirmed where the
// platform can report the result.
type Keyboard interface {
	KeyboardShown(ctx context.Context) (bool, error)
	// FocusedField is nil, with no error, when nothing has focus.
	FocusedField(ctx context.Context) (*FocusedField, error)
	TypeIntoFocus(ctx context.Context, text string) (*FocusedField, error)
	PressKeyboardKey(ctx context.Context, key string) error
	// HideKeyboard does nothing when the keyboard is already hidden.
	HideKeyboard(ctx context.Context) error
}

// KeyboardRegioner is implemented by backends that can say where the
// keyboard takes touches when it is not in the hierarchy they read — Android,
// where it is another window. iOS puts the keyboard in the app's own tree, so
// uitree.Tree.Keyboard answers there.
type KeyboardRegioner interface {
	KeyboardRegions(ctx context.Context) ([]uitree.Rect, error)
}

// FocusReader is implemented by backends that can say whether a node has
// keyboard focus. Android's tree says so itself; WebDriverAgent's does not —
// its `focused` is false on a field with the cursor in it — so it is asked
// which element is active. A driver that cannot tell does not implement it,
// and a wait for focus is refused there rather than never satisfied.
type FocusReader interface {
	HasFocus(ctx context.Context, n *uitree.Node) (bool, error)
}

// ClipboardPreviewer is implemented by backends that can say where a
// system preview of the clipboard takes touches — Android 13 and later, which
// put one up after every write, as another window.
type ClipboardPreviewer interface {
	ClipboardPreviewRegions(ctx context.Context) ([]uitree.Rect, error)
}

// ScreenRecorder is implemented by backends that can record the screen to a
// video. The recording is finished cleanly — both platforms' recorders write
// the header a player needs only when interrupted, not killed.
type ScreenRecorder interface {
	StartRecording(ctx context.Context) (device.Recording, error)
}

// Units a raw source is in.
const (
	UnitsPixels = "px"
	UnitsPoints = "pt"
)

// Source is a hierarchy as the device-side server sent it, before any
// parsing: the page source. Units says what its geometry
// is in, since on iOS that is points and not the pixels map and taps use;
// Scale is pixels per point there.
type Source struct {
	XML   []byte
	Units string
	Scale float64
}

// SourceReader is implemented by backends that can hand over the raw
// hierarchy. A driver process sends Mobium's own tree, not a platform's
// XML, so it has nothing to hand over.
type SourceReader interface {
	Source(ctx context.Context) (Source, error)
}

// AppPermissionResetter is implemented by backends that reset one app's
// permissions and can say what the reset left as it was. Built-in only: an
// external driver resets through Permissions.
type AppPermissionResetter interface {
	ResetAppPermissions(ctx context.Context, appID string) (device.PermissionReset, error)
}

// AppStates is implemented by backends that can say what state any app is
// in, and send the app in front away for a while.
type AppStates interface {
	// AppState reports one app's state: not installed, not running, in the
	// background or in front.
	AppState(ctx context.Context, appID string) (device.AppState, error)
	// Background sends appID, which is in front, to the background for d, and
	// brings the same app back — resumed where it was, not relaunched.
	Background(ctx context.Context, appID string, d time.Duration) error
}

// BatteryReader is implemented by backends that can read the battery.
type BatteryReader interface {
	Battery(ctx context.Context) (device.Battery, error)
}

// DeviceClock is implemented by backends that can say what time the device
// thinks it is, and in what zone.
type DeviceClock interface {
	Now(ctx context.Context) (device.ClockReading, error)
}

// FileTransfer is implemented by backends that can move a file between this
// machine and the folder the device keeps downloads in: Android's shared
// Download folder, an iOS app's Documents. appID names the app on iOS, where
// every app has its own; Android has one folder, and ignores it.
type FileTransfer interface {
	UploadFile(ctx context.Context, local, name, appID string) (device.Transfer, error)
	DownloadFile(ctx context.Context, name, appID, local string) (device.Transfer, error)
	ListFiles(ctx context.Context, appID string) ([]device.DeviceFile, error)
}

// DataClearer is implemented by backends that can delete an app's data and
// leave it installed — the state of a fresh install, without reinstalling.
type DataClearer interface {
	ClearData(ctx context.Context, appID string) (device.ClearedData, error)
}

// LightReader is implemented by backends that can read the screen without
// working out which elements are visible, and ask that of one element
// alone. Built in only, on the WDA driver, where visible is most of a read.
type LightReader interface {
	// LightSnapshot reads the screen with every element taken as shown. ok is
	// false when it should not be used now, and the caller reads in full.
	LightSnapshot(ctx context.Context) (t *uitree.Tree, ok bool, err error)
	// ElementVisible asks whether one node of a light read is visible. ok is
	// false when the node cannot be asked about alone.
	ElementVisible(ctx context.Context, n *uitree.Node, t *uitree.Tree) (visible, ok bool, err error)
}

// ForegroundReader is implemented by backends that can say which app is in
// front more cheaply than by reading the screen. Built in only, on the WDA
// driver.
type ForegroundReader interface {
	ForegroundApp(ctx context.Context) (string, error)
}

// ElementBounder is implemented by backends that can read one element's
// rectangle without reading the whole screen. Built in only, on the WDA
// driver, where a full read is the slow part of every action.
type ElementBounder interface {
	// ElementBounds reads the rectangle, in device pixels, of the element a
	// node of t names. ok is false when it cannot be read that way — nothing
	// names it uniquely in t — and the caller reads the screen instead.
	ElementBounds(ctx context.Context, n *uitree.Node, t *uitree.Tree) (r uitree.Rect, ok bool, err error)
}

// BundleResetter is implemented by backends that reset an app by installing
// it again from its bundle, where nothing can clear it in place — a real
// iPhone.
type BundleResetter interface {
	ResetFromBundle(ctx context.Context, appID, bundle string) (device.ClearedData, error)
}
