package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	// Idempotent, as Appium's quit on a session already gone.
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
	// "wda". The session open is the context, as in Appium.
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
