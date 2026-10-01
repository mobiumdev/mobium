package mobiumdriver

// A driver's capabilities are discovered by type assertion: a backend that can
// swipe implements Gesturer, and one that cannot simply does not. That works
// perfectly for the backends compiled in, and not at all for one that arrives
// as a subprocess — a single Go type either has the Swipe method or it does
// not, and the external driver has to be one type whatever the driver behind
// it can do.
//
// So the assertion is wrapped. AsGesturer and its siblings do the same type
// assertion, and additionally ask a driver that reports its own capabilities
// whether it really has this one. Everything above mobiumdriver asks through these
// rather than asserting directly, so a built-in and an external backend answer
// "can you type" the same way.

// Capable is implemented by a driver that decides at runtime what it can do.
// Two do: the external one, whose answer comes from the handshake, and WDA,
// which on a real iPhone declines what only simctl could do on a simulator.
type Capable interface {
	// HasCapability reports whether this driver supports a named capability.
	// The names are the ones in docs/decisions/0003.
	HasCapability(name string) bool
}

// Capability names, shared between the handshake and these helpers so a typo
// cannot make a capability silently unavailable.
const (
	CapGestures         = "gestures"
	CapText             = "text"
	CapApps             = "apps"
	CapAppearance       = "appearance"
	CapInventory        = "inventory"
	CapPermissions      = "permissions"
	CapPermissionState  = "permissionState"
	CapHealth           = "health"
	CapOrientation      = "orientation"
	CapLocalization     = "localization"
	CapButtons          = "buttons"
	CapScreenLock       = "screenLock"
	CapInterruptions    = "interruptions"
	CapClock            = "clock"
	CapNotifications    = "notifications"
	CapGeolocation      = "geolocation"
	CapGeolocationState = "geolocationState"
	CapRoutes           = "routes"
	CapClipboard        = "clipboard"
	CapClipboardRead    = "clipboardRead"
	CapAlerts           = "alerts"
	CapPinch            = "pinch"
	CapDoubleTap        = "doubleTap"
	CapDrag             = "drag"
	CapMultiTouch       = "multiTouch"
	CapDeviceLogs       = "deviceLogs"
	CapCrashes          = "crashes"
	CapKeyboard         = "keyboard"
	CapRecording        = "recording"
	CapClearData        = "clearData"
	CapSource           = "source"
	CapAccessibility    = "accessibility"
	CapAppState         = "appState"
	CapBattery          = "battery"
	CapDeviceClock      = "deviceClock"
	CapShake            = "shake"
	CapNetwork          = "network"
	CapFiles            = "files"
	CapBiometric        = "biometric"
	CapHitTest          = "hitTest"
)

// KnownCapabilities is every capability Mobium understands, for diagnostics
// and for `doctor` to report what a driver advertised that Mobium ignored.
var KnownCapabilities = []string{
	CapGestures, CapText, CapApps, CapAppearance,
	CapInventory, CapPermissions, CapPermissionState, CapHealth, CapOrientation,
	CapLocalization, CapButtons, CapScreenLock, CapInterruptions, CapClock, CapNotifications,
	CapGeolocation, CapGeolocationState, CapRoutes,
	CapClipboard, CapClipboardRead, CapAlerts, CapPinch,
	CapDoubleTap, CapDrag, CapMultiTouch, CapDeviceLogs, CapCrashes, CapKeyboard, CapRecording,
	CapClearData, CapSource, CapAccessibility, CapAppState, CapBattery, CapDeviceClock, CapShake,
	CapNetwork, CapFiles, CapBiometric, CapHitTest,
}

// has reports whether d claims the capability. A driver that does not report
// capabilities at all is a compiled-in one, whose methods are the truth.
func has(d Driver, name string) bool {
	c, ok := d.(Capable)
	if !ok {
		return true
	}
	return c.HasCapability(name)
}

// AsGesturer returns the driver's gesture support, if it has any.
func AsGesturer(d Driver) (Gesturer, bool) {
	g, ok := d.(Gesturer)
	return g, ok && has(d, CapGestures)
}

// AsTextEntry returns the driver's text entry support, if it has any.
func AsTextEntry(d Driver) (TextEntry, bool) {
	t, ok := d.(TextEntry)
	return t, ok && has(d, CapText)
}

// AsAppControl returns the driver's app lifecycle support, if it has any.
func AsAppControl(d Driver) (AppControl, bool) {
	a, ok := d.(AppControl)
	return a, ok && has(d, CapApps)
}

// AsAppearance returns the driver's light/dark support, if it has any.
func AsAppearance(d Driver) (Appearance, bool) {
	a, ok := d.(Appearance)
	return a, ok && has(d, CapAppearance)
}

// AsAccessibility returns the driver's accessibility-settings support, if it
// has any.
func AsAccessibility(d Driver) (Accessibility, bool) {
	a, ok := d.(Accessibility)
	return a, ok && has(d, CapAccessibility)
}

// AsAppInventory returns the driver's install listing support, if it has any.
func AsAppInventory(d Driver) (AppInventory, bool) {
	i, ok := d.(AppInventory)
	return i, ok && has(d, CapInventory)
}

// AsPermissions returns the driver's permission writing support, if it has any.
func AsPermissions(d Driver) (Permissions, bool) {
	p, ok := d.(Permissions)
	return p, ok && has(d, CapPermissions)
}

// AsPermissionReader returns the driver's permission reading support, if any.
func AsPermissionReader(d Driver) (PermissionReader, bool) {
	p, ok := d.(PermissionReader)
	return p, ok && has(d, CapPermissionState)
}

// AsHealth returns the driver's liveness check, if it has one.
func AsHealth(d Driver) (Health, bool) {
	h, ok := d.(Health)
	return h, ok && has(d, CapHealth)
}

// AsOrientation returns the driver's rotation support, if it has any.
func AsOrientation(d Driver) (Orientation, bool) {
	o, ok := d.(Orientation)
	return o, ok && has(d, CapOrientation)
}

// AsPincher returns the driver's two-finger gesture support, if it has any.
func AsPincher(d Driver) (Pincher, bool) {
	p, ok := d.(Pincher)
	return p, ok && has(d, CapPinch)
}

// AsMultiToucher returns the driver's support for fingers that do different
// things at once, if it has any.
func AsMultiToucher(d Driver) (MultiToucher, bool) {
	m, ok := d.(MultiToucher)
	return m, ok && has(d, CapMultiTouch)
}

// AsDoubleTapper returns the driver's double-tap support, if it has any.
//
// Asked separately from AsGesturer, and not implied by Tap, because placing
// two taps inside the platform's 40-300ms window is a thing a backend can
// fail to do — see DoubleTapper.
func AsDoubleTapper(d Driver) (DoubleTapper, bool) {
	t, ok := d.(DoubleTapper)
	return t, ok && has(d, CapDoubleTap)
}

// AsDragger returns the driver's press-hold-move-release support, if it has any.
//
// Asked separately from AsGesturer for the same reason: a backend can swipe
// and still have no way to hold at either end, which is the whole of a drag.
func AsDragger(d Driver) (Dragger, bool) {
	g, ok := d.(Dragger)
	return g, ok && has(d, CapDrag)
}

// AsAlerts returns the driver's ability to answer a system dialog, if it has any.
func AsAlerts(d Driver) (Alerts, bool) {
	a, ok := d.(Alerts)
	return a, ok && has(d, CapAlerts)
}

// AsClipboard returns the driver's ability to write the clipboard, if it has any.
func AsClipboard(d Driver) (Clipboard, bool) {
	c, ok := d.(Clipboard)
	return c, ok && has(d, CapClipboard)
}

// AsClipboardReader returns the driver's ability to read the clipboard back.
//
// Asked separately because Android cannot, and a caller that assumed both
// would report an empty clipboard it never read.
func AsClipboardReader(d Driver) (ClipboardReader, bool) {
	r, ok := d.(ClipboardReader)
	return r, ok && has(d, CapClipboardRead)
}

// AsGeolocation returns the driver's ability to place a device, if it has any.
func AsGeolocation(d Driver) (Geolocation, bool) {
	g, ok := d.(Geolocation)
	return g, ok && has(d, CapGeolocation)
}

// AsGeolocationReader returns the driver's ability to say where a device is.
//
// Asked separately from AsGeolocation because iOS can do the first and not the
// second, and a caller that assumed both would report a position it never read.
func AsGeolocationReader(d Driver) (GeolocationReader, bool) {
	r, ok := d.(GeolocationReader)
	return r, ok && has(d, CapGeolocationState)
}

// AsRouteRunner returns the driver's ability to follow waypoints on its own.
//
// A backend without it is not incapable of routes — the tool layer steps one
// through SetLocation instead. This only asks who owns the timer.
func AsRouteRunner(d Driver) (RouteRunner, bool) {
	r, ok := d.(RouteRunner)
	return r, ok && has(d, CapRoutes)
}

// AsLocalization returns the driver's per-app language support, if it has any.
func AsLocalization(d Driver) (Localization, bool) {
	l, ok := d.(Localization)
	return l, ok && has(d, CapLocalization)
}

// AsButtons returns the driver's hardware button support, if it has any.
func AsButtons(d Driver) (Buttons, bool) {
	b, ok := d.(Buttons)
	return b, ok && has(d, CapButtons)
}

// AsScreenLock returns the driver's screen lock support, if it has any.
func AsScreenLock(d Driver) (ScreenLock, bool) {
	s, ok := d.(ScreenLock)
	return s, ok && has(d, CapScreenLock)
}

// AsInterruptions returns the driver's call and message support, if it has any.
func AsInterruptions(d Driver) (Interruptions, bool) {
	i, ok := d.(Interruptions)
	return i, ok && has(d, CapInterruptions)
}

// AsClock returns the driver's timezone support, if it has any.
func AsClock(d Driver) (Clock, bool) {
	c, ok := d.(Clock)
	return c, ok && has(d, CapClock)
}

// AsNotifications returns the driver's notification support, if it has any.
func AsNotifications(d Driver) (Notifications, bool) {
	n, ok := d.(Notifications)
	return n, ok && has(d, CapNotifications)
}

// AsStarter returns the driver's lifecycle hooks, if it has any. Deliberately
// not gated on a capability: an external driver always needs starting and
// stopping, whatever it advertises.
func AsStarter(d Driver) (Starter, bool) {
	s, ok := d.(Starter)
	return s, ok
}

// AsDeviceLogs returns the driver's device log support, if it has any.
func AsDeviceLogs(d Driver) (DeviceLogs, bool) {
	l, ok := d.(DeviceLogs)
	return l, ok && has(d, CapDeviceLogs)
}

// AsCrashReports returns the driver's crash report support, if it has any.
func AsCrashReports(d Driver) (CrashReports, bool) {
	c, ok := d.(CrashReports)
	return c, ok && has(d, CapCrashes)
}

// AsKeyboard returns the driver's soft keyboard support, if it has any.
func AsKeyboard(d Driver) (Keyboard, bool) {
	k, ok := d.(Keyboard)
	return k, ok && has(d, CapKeyboard)
}

// PointScaler is implemented by backends whose device measures in points and
// reports them to Mobium as pixels — iOS — and can say how many pixels a
// point is. Built in only: nothing else needs converting.
type PointScaler interface {
	PointScale() float64
}

// AsPointScaler returns the driver's pixels-per-point, if it has one.
func AsPointScaler(d Driver) (PointScaler, bool) {
	p, ok := d.(PointScaler)
	return p, ok
}

// AsForegroundReader returns the driver's cheap foreground read, if any.
func AsForegroundReader(d Driver) (ForegroundReader, bool) {
	f, ok := d.(ForegroundReader)
	return f, ok
}

// AsElementBounder returns the driver's single-element read, if it has one.
func AsElementBounder(d Driver) (ElementBounder, bool) {
	b, ok := d.(ElementBounder)
	return b, ok
}

// AsBundleResetter returns the driver's reset-by-reinstall, if it has one.
// Built in only, on the WDA driver.
func AsBundleResetter(d Driver) (BundleResetter, bool) {
	r, ok := d.(BundleResetter)
	return r, ok
}

// AsScreenRecorder returns the driver's screen recording support, if any.
func AsScreenRecorder(d Driver) (ScreenRecorder, bool) {
	r, ok := d.(ScreenRecorder)
	return r, ok && has(d, CapRecording)
}

// AsFileTransfer returns the driver's support for moving files to and from
// the device, if any.
func AsFileTransfer(d Driver) (FileTransfer, bool) {
	f, ok := d.(FileTransfer)
	return f, ok && has(d, CapFiles)
}

// AsDataClearer returns the driver's support for clearing an app's data, if
// any.
func AsDataClearer(d Driver) (DataClearer, bool) {
	c, ok := d.(DataClearer)
	return c, ok && has(d, CapClearData)
}

// AsAppPermissionResetter returns the driver's per-app permission reset with
// its report, if any; it goes with the permissions capability.
func AsAppPermissionResetter(d Driver) (AppPermissionResetter, bool) {
	r, ok := d.(AppPermissionResetter)
	return r, ok && has(d, CapPermissions)
}

// AsAppStates returns the driver's support for reading an app's state and
// backgrounding it, if any.
func AsAppStates(d Driver) (AppStates, bool) {
	a, ok := d.(AppStates)
	return a, ok && has(d, CapAppState)
}

// AsBatteryReader returns the driver's battery reading, if any.
func AsBatteryReader(d Driver) (BatteryReader, bool) {
	b, ok := d.(BatteryReader)
	return b, ok && has(d, CapBattery)
}

// AsDeviceClock returns the driver's reading of the device's clock, if any.
func AsDeviceClock(d Driver) (DeviceClock, bool) {
	c, ok := d.(DeviceClock)
	return c, ok && has(d, CapDeviceClock)
}

// AsNetworker returns the driver's network conditions, if any.
func AsNetworker(d Driver) (Networker, bool) {
	n, ok := d.(Networker)
	return n, ok && has(d, CapNetwork)
}

// AsBiometrics returns the driver's biometrics, if any.
func AsBiometrics(d Driver) (Biometrics, bool) {
	b, ok := d.(Biometrics)
	return b, ok && has(d, CapBiometric)
}

// AsHitTester returns the driver's hit test, if any.
func AsHitTester(d Driver) (HitTester, bool) {
	t, ok := d.(HitTester)
	return t, ok && has(d, CapHitTest)
}

// AsShaker returns the driver's shake, if any.
func AsShaker(d Driver) (Shaker, bool) {
	s, ok := d.(Shaker)
	return s, ok && has(d, CapShake)
}

// AsKeyboardRegioner returns the driver's view of where the keyboard is, if
// any; it goes with the keyboard capability.
func AsKeyboardRegioner(d Driver) (KeyboardRegioner, bool) {
	k, ok := d.(KeyboardRegioner)
	return k, ok && has(d, CapKeyboard)
}

// AsFocusReader returns the driver's view of which node has keyboard focus,
// if any; it goes with the keyboard capability.
func AsFocusReader(d Driver) (FocusReader, bool) {
	f, ok := d.(FocusReader)
	return f, ok && has(d, CapKeyboard)
}

// AsClipboardPreviewer returns the driver's view of the clipboard's
// preview, if any; it goes with the clipboard capability.
func AsClipboardPreviewer(d Driver) (ClipboardPreviewer, bool) {
	c, ok := d.(ClipboardPreviewer)
	return c, ok && has(d, CapClipboard)
}

// AsSourceReader returns the driver's raw hierarchy support, if any.
func AsSourceReader(d Driver) (SourceReader, bool) {
	r, ok := d.(SourceReader)
	return r, ok && has(d, CapSource)
}

// Decliner is implemented by a driver that can say why it declines a
// capability it reports not having — WebDriverAgent on a real iPhone, for
// what only simctl does on a simulator.
type Decliner interface {
	DeclineReason(capability string) error
}

// Declined is the driver's own reason for lacking a capability, or nil when
// it gives none and the caller's generic refusal stands. Without it a phone
// answered "the wda backend cannot record the screen" — true of
// the phone, false of the backend, and silent on why.
func Declined(d Driver, capability string) error {
	if dec, ok := d.(Decliner); ok {
		return dec.DeclineReason(capability)
	}
	return nil
}
