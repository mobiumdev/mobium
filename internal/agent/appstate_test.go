package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// stateDriver is a device with one app, whose state it reports and whose
// backgrounding it records. Its screen is always that app's.
type stateDriver struct {
	boxDriver
	app      string
	state    device.AppState
	away     time.Duration
	sentAway int
	// staysAway means the app does not come back.
	staysAway bool
}

func (d *stateDriver) AppState(ctx context.Context, appID string) (device.AppState, error) {
	if appID != d.app {
		return device.AppState{State: device.AppNotInstalled}, nil
	}
	return d.state, nil
}

func (d *stateDriver) Background(ctx context.Context, appID string, dur time.Duration) error {
	d.sentAway++
	d.away = dur
	if d.staysAway {
		d.state = device.AppState{State: device.AppBackground}
	}
	return nil
}

func (d *stateDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	t, err := d.boxDriver.Snapshot(ctx)
	if err == nil {
		t.Root.Package = d.app
		t.Root.Children[0].Package = d.app
	}
	return t, err
}

func withState(t *testing.T, state string) (*Handlers, *stateDriver) {
	t.Helper()
	d := &stateDriver{boxDriver: boxDriver{class: "android.widget.Button"}, app: "org.example",
		state: device.AppState{State: state}}
	h := NewHandlers()
	h.implicitWait, h.settleWindow = 0, 0
	h.sessions["fake"] = &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	return h, d
}

func TestAppStateSaysWhatTheDeviceSays(t *testing.T) {
	yes := true
	for _, c := range []struct {
		state     device.AppState
		app, want string
	}{
		{device.AppState{State: device.AppNotRunning}, "org.example", "installed and not running"},
		{device.AppState{State: device.AppBackground}, "org.example", "running in the background"},
		{device.AppState{State: device.AppBackground, Suspended: &yes}, "org.example", "background, suspended"},
		{device.AppState{State: device.AppForeground}, "org.example", "is in front"},
		{device.AppState{}, "org.other", "not installed"},
	} {
		h, d := withState(t, "")
		d.state = c.state
		res, err := h.Call("app_state", map[string]interface{}{"app": c.app})
		if err != nil || !strings.Contains(res.Content[0].Text, c.want) {
			t.Errorf("%+v: %v, %v, want %q", c.state, res, err, c.want)
		}
	}
}

// An app in front under another process's window — a permission prompt — is
// in front, and the answer names what covers it.
func TestAppStateNamesWhatCoversTheApp(t *testing.T) {
	h, d := withState(t, device.AppForeground)
	res, err := h.Call("app_state", map[string]interface{}{"app": "org.example"})
	if err != nil {
		t.Fatal(err)
	}
	if v := res.StructuredContent.(AppStateView); v.CoveredBy != "" {
		t.Errorf("an uncovered app was reported covered by %q", v.CoveredBy)
	}
	h.sessions["fake"].driver = &coveredDriver{stateDriver: d, front: "com.android.permissioncontroller"}
	res, err = h.Call("app_state", map[string]interface{}{"app": "org.example"})
	if err != nil {
		t.Fatal(err)
	}
	if v := res.StructuredContent.(AppStateView); v.State != device.AppForeground ||
		v.CoveredBy != "com.android.permissioncontroller" {
		t.Errorf("view = %+v, want in front and covered by the prompt", v)
	}
}

// coveredDriver shows another process's window over the app.
type coveredDriver struct {
	*stateDriver
	front string
}

func (c *coveredDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	t, err := c.boxDriver.Snapshot(ctx)
	if err == nil {
		t.Root.Package = c.front
		t.Root.Children[0].Package = c.front
	}
	return t, err
}

func TestBackgroundSendsTheAppAwayAndConfirmsItBack(t *testing.T) {
	h, d := withState(t, device.AppForeground)
	res, err := h.Call("app_background", map[string]interface{}{"seconds": 1.5})
	if err != nil {
		t.Fatal(err)
	}
	if d.sentAway != 1 || d.away != 1500*time.Millisecond {
		t.Errorf("sent away %d times for %s", d.sentAway, d.away)
	}
	if !strings.Contains(res.Content[0].Text, "org.example was in the background for 1.5s and is in front again") {
		t.Errorf("text = %q", res.Content[0].Text)
	}
}

func TestBackgroundRefuses(t *testing.T) {
	for _, c := range []struct {
		name  string
		state string
		args  map[string]interface{}
		want  string
	}{
		{"no seconds", device.AppForeground, map[string]interface{}{}, "seconds"},
		{"zero", device.AppForeground, map[string]interface{}{"seconds": 0.0}, "more than 0"},
		{"too long", device.AppForeground, map[string]interface{}{"seconds": 181.0}, "at most 180"},
		{"not in front", device.AppBackground, map[string]interface{}{"seconds": 1.0}, "not in front"},
		{"not installed", device.AppForeground, map[string]interface{}{"seconds": 1.0, "app": "org.other"}, "not in front (not_installed)"},
	} {
		h, d := withState(t, c.state)
		_, err := h.Call("app_background", c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) || mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
			t.Errorf("%s: err = %v, want invalid_argument with %q", c.name, err, c.want)
		}
		if d.sentAway != 0 {
			t.Errorf("%s: the app was sent away", c.name)
		}
	}
}

// Reported as not confirmed when the app does not come back, rather than as
// done because the request was.
func TestBackgroundNoticesAnAppThatStaysAway(t *testing.T) {
	h, d := withState(t, device.AppForeground)
	d.staysAway = true
	_, err := h.Call("app_background", map[string]interface{}{"seconds": 1.0})
	if mobiumerr.CodeOf(err) != mobiumerr.NotConfirmed {
		t.Errorf("err = %v, want not_confirmed", err)
	}
}
