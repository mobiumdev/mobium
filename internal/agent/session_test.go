package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// twoSessions is a Handlers with sessions open on two devices, each with a
// ref table and a dialog rule, as a caller who had used both would leave it.
func twoSessions() *Handlers {
	h := NewHandlers()
	for _, serial := range []string{"emulator-5554", "emulator-5556"} {
		h.sessions[serial] = &session{dev: &device.Device{Serial: serial}, driver: &fakeDriver{}, backend: BackendUIA2}
		h.refs[serial] = &refTable{}
		h.dialogRules[serial] = []dialogRule{{When: "Allow", Press: "OK"}}
	}
	return h
}

func sessionCall(t *testing.T, h *Handlers, args map[string]interface{}) (SessionView, error) {
	t.Helper()
	res, err := h.sessionTool(context.Background(), args)
	if err != nil {
		return SessionView{}, err
	}
	v, ok := res.StructuredContent.(SessionView)
	if !ok {
		t.Fatalf("app_session answered %T, not a SessionView", res.StructuredContent)
	}
	return v, nil
}

func TestEndingASessionClosesThatDeviceOnly(t *testing.T) {
	h := twoSessions()
	v, err := sessionCall(t, h, map[string]interface{}{"action": "end", "device": "emulator-5554"})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Ended || v.Device != "emulator-5554" || v.Platform != "android" || v.Driver != "uiautomator2" {
		t.Errorf("end answered %+v", v)
	}
	if _, open := h.sessions["emulator-5554"]; open {
		t.Error("the session is still open")
	}
	// The next start on this device begins clean: no stale refs, no rules
	// declared for a session that has ended.
	if _, ok := h.refs["emulator-5554"]; ok {
		t.Error("its refs were kept")
	}
	if _, ok := h.dialogRules["emulator-5554"]; ok {
		t.Error("its dialog rules were kept")
	}
	// And the other device is untouched.
	if _, open := h.sessions["emulator-5556"]; !open || h.refs["emulator-5556"] == nil || len(h.dialogRules["emulator-5556"]) != 1 {
		t.Error("ending one device's session touched the other's")
	}
	if len(v.Sessions) != 1 || v.Sessions[0].Device != "emulator-5556" {
		t.Errorf("sessions still open = %+v, want only emulator-5556", v.Sessions)
	}
}

func TestEndingWithTwoOpenAndNoDeviceIsRefused(t *testing.T) {
	// Ending "the" session when there are two would be a guess, and a guess
	// that ends the wrong device's session is worse than a refusal.
	h := twoSessions()
	_, err := sessionCall(t, h, map[string]interface{}{"action": "end"})
	if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Fatalf("err = %v, want invalid_argument", err)
	}
	for _, want := range []string{"emulator-5554", "emulator-5556", "device"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
	if len(h.sessions) != 2 {
		t.Error("a refused end closed a session anyway")
	}
}

func TestEndingASessionThatIsNotOpenSucceeds(t *testing.T) {
	// Idempotent: a session already gone is a session ended.
	h := NewHandlers()
	v, err := sessionCall(t, h, map[string]interface{}{"action": "end", "device": "emulator-5554"})
	if err != nil {
		t.Fatalf("ending nothing failed: %v", err)
	}
	if v.Ended {
		t.Error("it reported ending a session that was not open")
	}
}

func TestTheOnlySessionEndsWithoutNamingIt(t *testing.T) {
	h := twoSessions()
	delete(h.sessions, "emulator-5556")
	v, err := sessionCall(t, h, map[string]interface{}{"action": "quit"})
	if err != nil || !v.Ended || v.Device != "emulator-5554" {
		t.Fatalf("end with one session open = %+v, %v", v, err)
	}
}

func TestStatusListsTheOpenSessions(t *testing.T) {
	h := twoSessions()
	v, err := sessionCall(t, h, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if v.Action != "status" || len(v.Sessions) != 2 || v.Sessions[0].Device != "emulator-5554" {
		t.Errorf("status = %+v", v)
	}
	if v, _ := sessionCall(t, NewHandlers(), map[string]interface{}{"action": "status"}); v.Sessions == nil {
		t.Error("no sessions is null on the wire, not an empty list")
	}
}

func TestPlatformPicksTheDriver(t *testing.T) {
	for _, c := range []struct {
		platform, driver string
		want             string
		refused          bool
	}{
		{"ios", "", "wda", false},
		{"iOS", "", "wda", false},
		{"android", "", "", false},
		{"ios", "wda", "wda", false},
		{"ios", "uiautomator2", "", true},
		{"android", "wda", "", true},
		{"windows", "", "", true},
	} {
		args, err := platformBackend(map[string]interface{}{"platform": c.platform, "driver": c.driver})
		if c.refused {
			if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
				t.Errorf("platform %q, driver %q: err = %v, want invalid_argument", c.platform, c.driver, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("platform %q, driver %q: %v", c.platform, c.driver, err)
			continue
		}
		if got := stringArg(args, "driver"); got != c.want {
			t.Errorf("platform %q, driver %q: driver = %q, want %q", c.platform, c.driver, got, c.want)
		}
	}
}

func TestTheOldIOSDriverNameIsRefusedWithTheNewOne(t *testing.T) {
	// Unrefused it would be taken for a third-party driver and fail later,
	// looking on PATH for mobium-driver-webdriveragent: the wrong cause.
	_, err := ParseBackend("webdriveragent")
	if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument || !strings.Contains(err.Error(), `"wda"`) {
		t.Errorf("err = %v, want invalid_argument naming \"wda\"", err)
	}
	if b, err := ParseBackend("wda"); err != nil || b != BackendWDA {
		t.Errorf("wda = %q, %v", b, err)
	}
}

func TestAnUnknownActionIsRefused(t *testing.T) {
	_, err := sessionCall(t, NewHandlers(), map[string]interface{}{"action": "restart"})
	var e *mobiumerr.Error
	if !errors.As(err, &e) || e.Code != mobiumerr.InvalidArgument {
		t.Errorf("err = %v, want invalid_argument", err)
	}
}

func TestACallNamingNoDriverUsesTheSessionOpen(t *testing.T) {
	// A session started with platform "ios" is followed by calls that name
	// no driver — from every client, since platform goes only with start.
	// Before this, those calls took Android's default and failed asking for
	// "wda". The session open is the context.
	h := NewHandlers()
	ios := &session{dev: &device.Device{Serial: "457C7DC2-C706-45D9-8D68-1D26953E28B1"}, driver: &fakeDriver{}, backend: BackendWDA}
	h.sessions[ios.dev.Serial] = ios
	ctx := context.Background()

	for name, args := range map[string]map[string]interface{}{
		"naming the device": {"device": ios.dev.Serial},
		"naming nothing":    {},
	} {
		got, err := h.sessionFor(ctx, args)
		if err != nil || got != ios {
			t.Errorf("%s: got %v, %v; want the iOS session already open", name, got, err)
		}
	}

	// With two open and no device named there is no one session to mean, so
	// the call is not quietly sent to either.
	other := &session{dev: &device.Device{Serial: "emulator-5554"}, driver: &fakeDriver{}, backend: BackendUIA2}
	h.sessions[other.dev.Serial] = other
	if s := h.openSessionFor(""); s != nil {
		t.Errorf("with two sessions open, a call naming no device went to %s", s.dev.Serial)
	}
	if s := h.openSessionFor("emulator-5554"); s != other {
		t.Error("naming the device did not find its session")
	}
}

// appDriver is a fakeDriver that manages apps, recording what it stopped.
type appDriver struct {
	fakeDriver
	terminated []string
	failWith   error
}

func (d *appDriver) Launch(ctx context.Context, app string) error { return nil }
func (d *appDriver) Terminate(ctx context.Context, app string) error {
	if d.failWith != nil {
		return d.failWith
	}
	d.terminated = append(d.terminated, app)
	return nil
}
func (d *appDriver) Install(ctx context.Context, path string) error { return nil }
func (d *appDriver) OpenURL(ctx context.Context, url string) error  { return nil }

// A browser a session launched and left running keeps the pages it opened,
// and the next session's contexts listed them — measured with Safari, whose
// "fresh" launch restores every tab. So the end stops what the start
// launched, and nothing else.
func TestEndingStopsTheAppTheStartLaunched(t *testing.T) {
	h := NewHandlers()
	d := &appDriver{}
	h.sessions["emulator-5554"] = &session{dev: &device.Device{Serial: "emulator-5554"}, driver: d, backend: BackendUIA2, launched: "com.android.chrome"}
	v, err := sessionCall(t, h, map[string]interface{}{"action": "end", "device": "emulator-5554"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(d.terminated, ",") != "com.android.chrome" || v.Terminated != "com.android.chrome" {
		t.Errorf("stopped %v, reported %q; want com.android.chrome both times", d.terminated, v.Terminated)
	}

	// A session that launched nothing stops nothing.
	d = &appDriver{}
	h.sessions["emulator-5554"] = &session{dev: &device.Device{Serial: "emulator-5554"}, driver: d, backend: BackendUIA2}
	if v, err = sessionCall(t, h, map[string]interface{}{"action": "end", "device": "emulator-5554"}); err != nil || len(d.terminated) != 0 || v.Terminated != "" {
		t.Errorf("a session that launched nothing stopped %v (reported %q, %v)", d.terminated, v.Terminated, err)
	}

	// One that cannot be stopped does not keep the session open: ending it
	// is what was asked, and the result says what was left running.
	d = &appDriver{failWith: errors.New("device offline")}
	h.sessions["emulator-5554"] = &session{dev: &device.Device{Serial: "emulator-5554"}, driver: d, backend: BackendUIA2, launched: "com.android.chrome"}
	res, err := h.sessionTool(context.Background(), map[string]interface{}{"action": "end", "device": "emulator-5554"})
	if err != nil {
		t.Fatal(err)
	}
	if _, open := h.sessions["emulator-5554"]; open {
		t.Error("the session stayed open because its app could not be stopped")
	}
	if text := res.Content[0].Text; !strings.Contains(text, "could not be stopped: device offline") {
		t.Errorf("the result does not say the app was left running: %q", text)
	}
	if res.StructuredContent.(SessionView).Terminated != "" {
		t.Error("an app that was not stopped was reported as stopped")
	}
}

// routeDriver plays routes the way simctl does — itself — and counts clears.
type routeDriver struct {
	fakeDriver
	clears int
}

func (d *routeDriver) SetLocation(ctx context.Context, lat, lon float64) error { return nil }
func (d *routeDriver) ClearLocation(ctx context.Context) error {
	d.clears++
	return nil
}
func (d *routeDriver) StartRoute(ctx context.Context, pts []device.Point, speed float64) error {
	return nil
}

// simctl plays a route itself, so ending the session left it playing while
// the result said "a route stopped". The end now clears it — simctl's only
// way to stop one — unless it has already finished.
func TestEndingStopsARouteTheSimulatorIsPlaying(t *testing.T) {
	for _, c := range []struct {
		name   string
		speed  float64
		clears int
	}{
		{"still playing", 1, 1},    // about 20 minutes of route
		{"finished", 1_000_000, 0}, // over in about a millisecond
	} {
		h := NewHandlers()
		d := &routeDriver{}
		s := &session{dev: &device.Device{Serial: "SIM-1"}, driver: d, backend: BackendWDA}
		h.sessions["SIM-1"] = s
		_, err := h.locationOn(context.Background(), s, map[string]interface{}{
			"waypoints": []interface{}{[]interface{}{51.5007, -0.1246}, []interface{}{51.5081, -0.1100}},
			"speed":     c.speed,
		})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		time.Sleep(20 * time.Millisecond)
		if _, err := sessionCall(t, h, map[string]interface{}{"action": "end", "device": "SIM-1"}); err != nil {
			t.Fatal(err)
		}
		if d.clears != c.clears {
			t.Errorf("%s: the end cleared the location %d times, want %d", c.name, d.clears, c.clears)
		}
	}
}

// Only a UiAutomator2 session on a real Android phone names an exposure: an
// emulator is behind its own NAT, and the dump driver listens nowhere.
func TestSessionExposureOnlyOnARealPhonesServer(t *testing.T) {
	for _, c := range []struct {
		backend  Backend
		emulator bool
		want     bool
	}{
		{BackendUIA2, false, true},
		{BackendUIA2, true, false},
		{BackendDump, false, false},
		{BackendWDA, false, false},
	} {
		s := &session{backend: c.backend, dev: &device.Device{Serial: "x", Emulator: c.emulator}}
		if got := sessionExposure(s) != ""; got != c.want {
			t.Errorf("%s, emulator=%v: exposure named %v, want %v", c.backend, c.emulator, got, c.want)
		}
	}
}
