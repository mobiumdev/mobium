package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// openDriver is a device whose foreground app changes a little while after a
// URL is dispatched, which is what `am start` actually does: it returns once
// the intent is sent, not once the app is on screen.
type openDriver struct {
	fakeDriver
	pkg      string
	opened   bool
	openedAt time.Time
	// appears is how long the new app takes to reach the foreground.
	appears time.Duration
	// becomes is the package that eventually shows up; empty means the URL
	// changes nothing at all.
	becomes string
}

func (d *openDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	pkg := d.pkg
	if d.opened && d.becomes != "" && time.Since(d.openedAt) >= d.appears {
		pkg = d.becomes
	}
	xml := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">`+
		`<node index="0" package="%s" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">`+
		`<node index="0" text="%s" class="android.widget.TextView" bounds="[0,0][500,100]" />`+
		`</node></hierarchy>`, pkg, pkg)
	return uitree.ParseAndroid([]byte(xml))
}

func (d *openDriver) Launch(ctx context.Context, app string) error    { return nil }
func (d *openDriver) Terminate(ctx context.Context, app string) error { return nil }
func (d *openDriver) Install(ctx context.Context, path string) error  { return nil }
func (d *openDriver) OpenURL(ctx context.Context, url string) error {
	d.opened, d.openedAt = true, time.Now()
	return nil
}

var _ mobiumdriver.AppControl = (*openDriver)(nil)

func withOpener(t *testing.T, d *openDriver) (*Handlers, *session) {
	t.Helper()
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendDump}
	h.sessions["fake"] = s
	return h, s
}

func openURL(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.openURLOn(ctx, s, args)
}

func TestOpenURLWaitsForTheAppToReachTheForeground(t *testing.T) {
	// Found on the emulator: opening a link and immediately asking what was
	// in the foreground answered with the *previous* app, because am start
	// returns when the intent is dispatched rather than when the app is up.
	d := &openDriver{pkg: "com.launcher", becomes: "com.android.chrome", appears: 300 * time.Millisecond}
	h, sess := withOpener(t, d)

	res, err := openURL(h, sess, map[string]interface{}{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	view := res.StructuredContent.(OpenURLView)
	if view.App != "com.android.chrome" {
		t.Errorf("reported app %q; it returned before the app was on screen", view.App)
	}
}

func TestOpenURLDoesNotWaitTheWholeBudget(t *testing.T) {
	// An app that arrives quickly must not cost the full settle window.
	d := &openDriver{pkg: "com.launcher", becomes: "com.android.chrome", appears: 50 * time.Millisecond}
	h, sess := withOpener(t, d)

	started := time.Now()
	if _, err := openURL(h, sess, map[string]interface{}{"url": "https://example.com"}); err != nil {
		t.Fatalf("open: %v", err)
	}
	if took := time.Since(started); took > settleAfterStart/2 {
		t.Errorf("took %s of a %s budget for an app that appeared immediately", took, settleAfterStart)
	}
}

func TestOpenURLThatChangesNothingIsNotAnError(t *testing.T) {
	// Verified on the emulator against Chrome sitting on its first-run
	// dialog: the hierarchy was byte-identical before and after. A link that
	// visibly does nothing is not a failure, it is just a link.
	d := &openDriver{pkg: "com.android.chrome"}
	h, sess := withOpener(t, d)

	res, err := openURL(h, sess, map[string]interface{}{"url": "https://example.org"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := res.StructuredContent.(OpenURLView).App; got != "com.android.chrome" {
		t.Errorf("app = %q, want the app that was already there", got)
	}
}

func TestOpenURLSettleStaysWithinTheCallBudget(t *testing.T) {
	if settleAfterStart >= callTimeout {
		t.Errorf("settleAfterStart (%s) is not under callTimeout (%s)", settleAfterStart, callTimeout)
	}
}

func launchOn(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.launchAppOn(ctx, s, args)
}

func TestLaunchWaitsForTheAppToBeInFront(t *testing.T) {
	// Found through the Python client: launching Settings over a running
	// Chrome and immediately asking what was in the foreground answered
	// "chrome", because am start returns before the app is on screen.
	d := &openDriver{pkg: "com.android.chrome", becomes: "com.android.settings",
		appears: 300 * time.Millisecond}
	h, sess := withOpener(t, d)
	// Launch on this fake moves the app immediately; make it behave like the
	// device, where the switch trails the call.
	d.opened, d.openedAt = true, time.Now()

	res, err := launchOn(h, sess, map[string]interface{}{"app": "com.android.settings"})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if got := textOf(res); !strings.Contains(got, "launched com.android.settings") ||
		strings.Contains(got, "in the foreground") {
		t.Errorf("result %q; it returned before the app was in front", got)
	}
}

func TestLaunchSaysWhenSomethingElseIsInFront(t *testing.T) {
	// A launch that is accepted while a permission dialog or a chooser takes
	// the screen should say so, not report plain success.
	d := &openDriver{pkg: "com.android.permissioncontroller"}
	h, sess := withOpener(t, d)

	res, err := launchOn(h, sess, map[string]interface{}{"app": "com.example.shop"})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	got := textOf(res)
	if !strings.Contains(got, "com.android.permissioncontroller") {
		t.Errorf("result %q does not say what is actually on screen", got)
	}
}

// textOf is the prose half of a tool result, which is what a CLI user reads.
func textOf(res *ToolsCallResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}
