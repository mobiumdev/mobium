package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

func TestParseBackend(t *testing.T) {
	tests := []struct {
		in      string
		want    Backend
		wantErr bool
	}{
		{"", DefaultBackend, false},
		{"uiautomator2", BackendUIA2, false},
		{"uiautomator", BackendDump, false},
		{"  uiautomator2  ", BackendUIA2, false},
		// An unrecognised name is not an error here any more: it is the name
		// of a possible third-party driver, and whether one exists is decided
		// when a session opens, where PATH can be named in the failure. See
		// docs/decisions/0003.
		{"appium", "appium", false},
		{"roku", "roku", false},
		// A near miss on a built-in name is still an error, because it is a
		// typo rather than a driver nobody has installed.
		{"UIAutomator2", "", true},
		{"UiAutomator", "", true},
		// Not a name at all.
		{"some driver", "", true},
		{"./driver", "", true},
	}
	for _, tc := range tests {
		got, err := ParseBackend(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseBackend(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("ParseBackend(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestUnknownBackendNamesTheValidOnes(t *testing.T) {
	_, err := ParseBackend("some driver")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"uiautomator2", "uiautomator", "mobium-driver-"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestDefaultBackendIsUIA2(t *testing.T) {
	// The fast, more capable backend is what an unconfigured user gets; the
	// one-time APK install is the price, the same trade vibium makes by
	// downloading Chrome.
	if DefaultBackend != BackendUIA2 {
		t.Errorf("default backend = %q", DefaultBackend)
	}
	if NewHandlers().backend != BackendUIA2 {
		t.Error("new handlers did not adopt the default backend")
	}
}

func TestSetBackendDropsCachedSessions(t *testing.T) {
	// A cached session belongs to the backend that made it; keeping it across
	// a switch would silently ignore the flag.
	h := NewHandlers()
	h.sessions["emulator-5554"] = &session{backend: BackendUIA2}
	h.SetBackend(BackendDump)
	if len(h.sessions) != 0 {
		t.Errorf("%d sessions survived a backend switch", len(h.sessions))
	}
	if h.backend != BackendDump {
		t.Errorf("backend = %q", h.backend)
	}
}

func TestSetBackendToSameValueKeepsSessions(t *testing.T) {
	h := NewHandlers()
	h.sessions["emulator-5554"] = &session{backend: BackendUIA2}
	h.SetBackend(BackendUIA2)
	if len(h.sessions) != 1 {
		t.Error("a no-op backend switch tore down the session")
	}
}

func TestSwipeDirectionsAvoidScreenEdges(t *testing.T) {
	// A gesture starting at the very edge is a system back or notification
	// swipe on modern Android, not a scroll of the app's content.
	for name, f := range swipeDirections {
		for i, v := range f {
			if v <= 0.05 || v >= 0.95 {
				t.Errorf("direction %q component %d is %v, too close to the edge", name, i, v)
			}
		}
	}
	// Swiping up must move the finger up the screen (y decreasing).
	up := swipeDirections["up"]
	if up[3] >= up[1] {
		t.Errorf("swipe up does not move upward: %v", up)
	}
	down := swipeDirections["down"]
	if down[3] <= down[1] {
		t.Errorf("swipe down does not move downward: %v", down)
	}
}

// stubDriver is a driver whose health can be flipped, standing in for a
// backend whose device went away.
type stubDriver struct {
	alive  bool
	closed bool
}

func (s *stubDriver) Snapshot(context.Context) (*uitree.Tree, error) { return nil, nil }
func (s *stubDriver) Screenshot(context.Context) ([]byte, error)     { return nil, nil }
func (s *stubDriver) Tap(context.Context, int, int) error            { return nil }
func (s *stubDriver) Name() string                                   { return "stub" }
func (s *stubDriver) Healthy(context.Context) bool                   { return s.alive }
func (s *stubDriver) Start(context.Context, func(string)) error      { return nil }
func (s *stubDriver) Close() error                                   { s.closed = true; return nil }

func TestSessionHealthGatesReuse(t *testing.T) {
	alive := &stubDriver{alive: true}
	s := &session{driver: alive, backend: BackendUIA2}
	if !s.healthy(context.Background()) {
		t.Error("a live session reported unhealthy")
	}

	// The emulator was quit and restarted: same serial, dead server.
	alive.alive = false
	if s.healthy(context.Background()) {
		t.Error("a session whose device vanished reported healthy")
	}
}

func TestBackendWithoutHealthIsAlwaysUsable(t *testing.T) {
	// The dump backend holds no connection — it shells out to adb each time —
	// so it must not be torn down for failing a check it does not implement.
	s := &session{driver: &plainDriver{}, backend: BackendDump}
	if !s.healthy(context.Background()) {
		t.Error("a connectionless backend was treated as unhealthy")
	}
}

type plainDriver struct{}

func (plainDriver) Snapshot(context.Context) (*uitree.Tree, error) { return nil, nil }
func (plainDriver) Screenshot(context.Context) ([]byte, error)     { return nil, nil }
func (plainDriver) Tap(context.Context, int, int) error            { return nil }
func (plainDriver) Name() string                                   { return "plain" }

func TestWebContextIsClosedWithTheSession(t *testing.T) {
	// A CDP attachment holds a forwarded host port. Closing a session without
	// detaching leaks one per WebView ever entered.
	s := &session{driver: &stubDriver{alive: true}, backend: BackendUIA2, webCtx: "WEBVIEW_x"}
	s.closeWeb()
	if s.webCtx != "" {
		t.Errorf("context still set after closeWeb: %q", s.webCtx)
	}
}

func TestSwitchingBackToNativeClearsTheContext(t *testing.T) {
	s := &session{driver: &stubDriver{alive: true}, backend: BackendUIA2, webCtx: "WEBVIEW_x"}
	s.close()
	if s.webCtx != "" {
		t.Error("close left a web context set")
	}
}

// TestNearMissSuggestsTheRealName: with unknown names now routed to PATH, a
// mistyped built-in used to produce "there is no mobium-driver-UIAutomator2",
// which sends the reader to look for a file that was never going to exist.
func TestNearMissSuggestsTheRealName(t *testing.T) {
	_, err := ParseBackend("UIAutomator2")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "did you mean \"uiautomator2\"") {
		t.Errorf("error = %v", err)
	}
}

// TestExternalBackendIsNotBuiltIn guards the branch in sessionFor: an external
// backend must be handled before adb is consulted, because a driver may be for
// a platform adb has never heard of.
func TestExternalBackendIsNotBuiltIn(t *testing.T) {
	for _, b := range []Backend{BackendUIA2, BackendDump, BackendWDA} {
		if !b.isBuiltin() {
			t.Errorf("%q should be built in", b)
		}
	}
	for _, b := range []Backend{"roku", "flutter", ""} {
		if b.isBuiltin() {
			t.Errorf("%q should not be built in", b)
		}
	}
}

// TestMissingDriverExplainsItself checks the message a user gets for a backend
// nobody has installed — the commonest first encounter with this feature.
func TestMissingDriverExplainsItself(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	h := &Handlers{sessions: map[string]*session{}}
	_, err := h.externalSessionFor(context.Background(), "roku", "")
	if err == nil {
		t.Fatal("expected an error for a driver that is not installed")
	}
	for _, want := range []string{"mobium-driver-roku", "MOBIUM_DRIVER_ROKU", "0003"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message does not mention %q:\n%v", want, err)
		}
	}
}

// Only a missing device for a named serial is reconsidered; anything else,
// and a lookup with no serial, passes through untouched. The branches that
// rewrite the advice need a device on the other platform and are driven
// live: a simulator's UDID on the Android backend, and an emulator's serial
// on the iOS one.
func TestOnOtherPlatformLeavesOtherErrorsAlone(t *testing.T) {
	ctx := context.Background()
	notReady := mobiumerr.New(mobiumerr.DeviceNotReady, "device x is offline, not ready")
	if got := onOtherPlatform(ctx, "x", notReady, true); got != notReady {
		t.Errorf("a not-ready device was rewritten: %v", got)
	}
	missing := mobiumerr.New(mobiumerr.NoDevice, "no device")
	if got := onOtherPlatform(ctx, "", missing, true); got != missing {
		t.Errorf("a lookup with no serial was rewritten: %v", got)
	}
	if got := onOtherPlatform(ctx, "x", nil, false); got != nil {
		t.Errorf("success became %v", got)
	}
}
