package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// fakeDriver plays back a scripted sequence of screens, one per Snapshot, so
// a wait can be tested against a screen that changes without a device.
type fakeDriver struct {
	screens []*uitree.Tree
	calls   atomic.Int32
	err     error
	tapped  []string
}

func (f *fakeDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	if f.err != nil {
		return nil, f.err
	}
	n := int(f.calls.Add(1)) - 1
	if len(f.screens) == 0 {
		// Nothing scripted: an empty screen, for a test about something else.
		return &uitree.Tree{Root: &uitree.Node{Class: "hierarchy", Displayed: true, Enabled: true}}, nil
	}
	if n >= len(f.screens) {
		n = len(f.screens) - 1 // the last screen persists
	}
	return f.screens[n], nil
}

func (f *fakeDriver) Screenshot(ctx context.Context) ([]byte, error) { return nil, nil }
func (f *fakeDriver) Tap(ctx context.Context, x, y int) error {
	f.tapped = append(f.tapped, fmt.Sprintf("%d,%d", x, y))
	return nil
}
func (f *fakeDriver) Name() string { return "fake" }

// fakeDevice is the device every scripted session claims to be.
func fakeDevice() *device.Device { return &device.Device{Serial: "fake"} }

// screen builds a one-button screen. An empty label produces a screen with
// nothing on it, which is what "not there yet" and "gone" both look like.
func screen(t *testing.T, label string) *uitree.Tree {
	t.Helper()
	body := ""
	if label != "" {
		body = fmt.Sprintf(`<node index="0" text="%s" resource-id="app:id/b" class="android.widget.Button" `+
			`content-desc="" clickable="true" enabled="true" bounds="[100,200][300,280]" />`, label)
	}
	xml := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">`+
		`<node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">%s</node></hierarchy>`, body)
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return tree
}

// withFake builds a session backed by a scripted driver and turns the
// implicit wait off, so a test measures only the wait it is exercising.
//
// The tools are called below sessionFor, which would otherwise go looking for
// a real emulator over adb.
func withFake(t *testing.T, screens ...*uitree.Tree) (*Handlers, *session, *fakeDriver) {
	t.Helper()
	f := &fakeDriver{screens: screens}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: f, backend: BackendDump}
	h.sessions["fake"] = s
	return h, s, f
}

// wait calls app_wait_for on the scripted session.
func wait(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.waitOn(ctx, s, args)
}

// tap calls app_tap on the scripted session.
func tap(h *Handlers, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.tapOn(ctx, s, args)
}

func TestPollUntilRunsOnceWithZeroTimeout(t *testing.T) {
	// A zero timeout must mean "try once", not "fail without trying" —
	// otherwise turning the implicit wait off would break every action.
	calls := 0
	err := pollUntil(context.Background(), 0, func(context.Context) (bool, error) {
		calls++
		return false, nil
	})
	if !errors.Is(err, errPollTimeout) {
		t.Errorf("err = %v, want a poll timeout", err)
	}
	if calls != 1 {
		t.Errorf("attempted %d times, want exactly 1", calls)
	}
}

func TestPollUntilStopsOnFatalError(t *testing.T) {
	// A device that cannot be asked is not a condition that will come true.
	boom := errors.New("adb: device offline")
	calls := 0
	err := pollUntil(context.Background(), time.Second, func(context.Context) (bool, error) {
		calls++
		return false, boom
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the fatal error", err)
	}
	if calls != 1 {
		t.Errorf("retried a fatal error %d times", calls)
	}
}

func TestPollUntilHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := pollUntil(ctx, time.Minute, func(context.Context) (bool, error) { return false, nil })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestWaitForVisibleSucceedsOnceItAppears(t *testing.T) {
	h, sess, f := withFake(t, screen(t, ""), screen(t, ""), screen(t, "Welcome"))
	res, err := wait(h, sess, map[string]interface{}{
		"target": "text=Welcome", "timeout_ms": 5000,
	})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if f.calls.Load() < 3 {
		t.Errorf("gave up after %d snapshots", f.calls.Load())
	}
	view, ok := res.StructuredContent.(WaitView)
	if !ok {
		t.Fatalf("structured content = %T", res.StructuredContent)
	}
	if view.Condition != condVisible {
		t.Errorf("condition = %q", view.Condition)
	}
	// The point of remapping on success: the caller can tap without a
	// separate app_map.
	if view.Element == nil {
		t.Fatal("no element reported; the caller has nothing to tap")
	}
	if view.Element.Ref == "" {
		t.Errorf("element has no ref: %+v", view.Element)
	}
	if _, err := h.locatorFor("fake", view.Element.Ref); err != nil {
		t.Errorf("the ref the wait handed back does not resolve: %v", err)
	}
}

func TestWaitForTimesOutSayingWhatWasOnScreen(t *testing.T) {
	h, sess, _ := withFake(t, screen(t, "Loading"))
	_, err := wait(h, sess, map[string]interface{}{
		"target": "text=Welcome", "timeout_ms": 300,
	})
	if err == nil {
		t.Fatal("waited for something that never appeared and reported success")
	}
	for _, want := range []string{"timed out", "Welcome", "not on screen"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestWaitForHidden(t *testing.T) {
	h, sess, _ := withFake(t, screen(t, "Loading"), screen(t, "Loading"), screen(t, ""))
	res, err := wait(h, sess, map[string]interface{}{
		"target": "text=Loading", "condition": "hidden", "timeout_ms": 5000,
	})
	if err != nil {
		t.Fatalf("wait hidden: %v", err)
	}
	view := res.StructuredContent.(WaitView)
	// Nothing is left to point at once it is gone, and inventing an element
	// would be worse than omitting one.
	if view.Element != nil {
		t.Errorf("a hidden element was reported as present: %+v", view.Element)
	}
}

func TestWaitForHiddenTreatsZeroSizedAsGone(t *testing.T) {
	// An element still in the hierarchy but collapsed to nothing has gone as
	// far as the user is concerned, and it cannot be tapped.
	xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		`<node index="0" text="Loading" class="android.widget.TextView" bounds="[100,200][100,200]" />` +
		`</node></hierarchy>`
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h, sess, _ := withFake(t, tree)
	if _, err := wait(h, sess, map[string]interface{}{
		"target": "text=Loading", "condition": "hidden", "timeout_ms": 100,
	}); err != nil {
		t.Errorf("zero-sized element counted as visible: %v", err)
	}
}

func TestWaitForText(t *testing.T) {
	h, sess, _ := withFake(t,
		screen(t, "Sending"), screen(t, "Sending"), screen(t, "Sent"))
	// The locator has to match across the change, so match on the id rather
	// than on the text that is changing.
	res, err := wait(h, sess, map[string]interface{}{
		"target": "testid=b", "condition": "text", "text": "Sent", "timeout_ms": 5000,
	})
	if err != nil {
		t.Fatalf("wait text: %v", err)
	}
	if got := res.StructuredContent.(WaitView).Element; got == nil || got.Label != "Sent" {
		t.Errorf("element = %+v", got)
	}
}

func TestWaitForTextReportsTheTextItFound(t *testing.T) {
	h, sess, _ := withFake(t, screen(t, "Sending"))
	_, err := wait(h, sess, map[string]interface{}{
		"target": "testid=b", "condition": "text", "text": "Sent", "timeout_ms": 200,
	})
	if err == nil {
		t.Fatal("text never changed but the wait succeeded")
	}
	if !strings.Contains(err.Error(), `"Sending"`) {
		t.Errorf("error %q does not report the text that was actually there", err)
	}
}

func TestWaitForRejectsBadArguments(t *testing.T) {
	h, sess, _ := withFake(t, screen(t, "Go"))
	cases := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"no target", map[string]interface{}{}, "needs a target"},
		{"unknown condition", map[string]interface{}{
			"target": "text=Go", "condition": "twinkling"}, "unknown condition"},
		{"text condition without text", map[string]interface{}{
			"target": "text=Go", "condition": "text"}, "needs the text"},
		{"timeout beyond the call budget", map[string]interface{}{
			"target": "text=Go", "timeout_ms": 999999}, "ceiling"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := wait(h, sess, c.args)
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

func TestWaitTimeoutStaysUnderTheCallBudget(t *testing.T) {
	// A wait allowed to outlast the call would be killed by callTimeout and
	// report a context deadline instead of naming what never appeared.
	if maxWaitTimeout >= callTimeout {
		t.Errorf("maxWaitTimeout (%s) is not under callTimeout (%s)", maxWaitTimeout, callTimeout)
	}
	if defaultWaitTimeout > maxWaitTimeout {
		t.Errorf("default wait (%s) exceeds the maximum (%s)", defaultWaitTimeout, maxWaitTimeout)
	}
}

func TestActionsRetryWhileTheScreenSettles(t *testing.T) {
	// The implicit wait is what removes the sleep before a tap: the element
	// is not there for the first two snapshots and the tap must still land.
	h, sess, f := withFake(t, screen(t, ""), screen(t, ""), screen(t, "Continue"))
	h.implicitWait = 2 * time.Second

	if _, err := tap(h, sess, map[string]interface{}{"target": "text=Continue"}); err != nil {
		t.Fatalf("tap gave up while the screen was still arriving: %v", err)
	}
	if len(f.tapped) != 1 || f.tapped[0] != "200,240" {
		t.Errorf("tapped %v, want the button's center once", f.tapped)
	}
}

func TestActionsStillFailWhenTheElementNeverArrives(t *testing.T) {
	h, sess, _ := withFake(t, screen(t, "Something else"))
	h.implicitWait = 200 * time.Millisecond

	_, err := tap(h, sess, map[string]interface{}{"target": "text=Continue"})
	if err == nil {
		t.Fatal("tapped an element that was never on screen")
	}
	// The retry must not swallow the original explanation.
	if !strings.Contains(err.Error(), "no element matches") {
		t.Errorf("error %q lost the reason resolution failed", err)
	}
}

func TestWaitReportsTheMappedAncestorNotTheRawMatch(t *testing.T) {
	// Found on a real device: waiting for text="Network & internet" matched
	// the TextView carrying that text, but the map collapses the row and
	// hands out a ref for the clickable parent. Matching nodes exactly meant
	// the wait reported no element at all, even though the ref table it had
	// just written did contain the row.
	xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		`<node index="0" class="android.widget.LinearLayout" clickable="true" enabled="true" bounds="[0,778][1080,1009]">` +
		`<node index="0" text="Network &amp; internet" class="android.widget.TextView" bounds="[60,800][600,900]" />` +
		`</node></node></hierarchy>`
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h, sess, _ := withFake(t, tree)

	res, err := wait(h, sess, map[string]interface{}{
		"target": "text=Network & internet", "timeout_ms": 1000,
	})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	view := res.StructuredContent.(WaitView)
	if view.Element == nil {
		t.Fatal("no element reported for a match nested inside the mapped row")
	}
	if view.Element.Ref == "" {
		t.Fatalf("element has no ref: %+v", view.Element)
	}
	// The ref must name the row that can be tapped, not the inert label.
	if view.Element.Bounds.Y1 != 778 || view.Element.Bounds.Y2 != 1009 {
		t.Errorf("ref points at %+v, want the clickable row", view.Element.Bounds)
	}
}

// What a dialog covers is not on screen to wait_for, as it is not to an
// action: on the iPhone 15 Plus a target under "Save Password?" read visible
// in 816ms while text refused it. The captured sheet, with the covered target
// marked visible as the phone reported it.
func TestWaitDoesNotSeeUnderADialog(t *testing.T) {
	raw, err := os.ReadFile("../uitree/testdata/ios-save-password.xml")
	if err != nil {
		t.Fatal(err)
	}
	flipped := strings.Replace(string(raw),
		`name="welcomeText" label="Welcome, mobium!" enabled="true" visible="false"`,
		`name="welcomeText" label="Welcome, mobium!" enabled="true" visible="true"`, 1)
	if flipped == string(raw) {
		t.Fatal("the fixture no longer has the covered welcome text to flip")
	}
	tree, err := uitree.ParseIOS([]byte(flipped))
	if err != nil {
		t.Fatal(err)
	}
	h, s, _ := withFake(t, tree)
	_, err = wait(h, s, map[string]interface{}{"target": "testid=welcomeText", "timeout_ms": 0})
	if mobiumerr.CodeOf(err) != mobiumerr.Timeout || !strings.Contains(err.Error(), "under a dialog") {
		t.Errorf("waiting for a target under the sheet: %v, want a timeout naming the dialog", err)
	}
	if _, err := wait(h, s, map[string]interface{}{"target": "testid=welcomeText", "condition": "hidden", "timeout_ms": 0}); err != nil {
		t.Errorf("a target under the sheet was not hidden, as Android reports it: %v", err)
	}
}
