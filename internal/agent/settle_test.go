package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// slidingDriver returns a button that moves for the first `moves` snapshots
// and then holds still — a sheet animating into place.
type slidingDriver struct {
	fakeDriver
	y      int
	moves  int
	calls  int
	tapped []string
}

func (d *slidingDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	d.calls++
	if d.moves > 0 {
		d.moves--
		d.y += 100
	}
	xml := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">`+
		`<node index="0" package="com.example" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">`+
		`<node index="0" text="Continue" class="android.widget.Button" clickable="true" `+
		`enabled="true" bounds="[100,%d][300,%d]" />`+
		`</node></hierarchy>`, d.y, d.y+80)
	return uitree.ParseAndroid([]byte(xml))
}

func (d *slidingDriver) Tap(ctx context.Context, x, y int) error {
	d.tapped = append(d.tapped, fmt.Sprintf("%d,%d", x, y))
	return nil
}

func withSliding(t *testing.T, moves int) (*Handlers, *session, *slidingDriver) {
	t.Helper()
	d := &slidingDriver{y: 200, moves: moves}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 10 * time.Millisecond
	h.settleTimeout = 500 * time.Millisecond
	s := &session{dev: fakeDevice(), driver: d, backend: BackendDump}
	h.sessions["fake"] = s
	return h, s, d
}

func TestActionsWaitForTheElementToStopMoving(t *testing.T) {
	// An element mid-animation resolves cleanly and reports bounds that are
	// stale by the time the tap lands.
	h, sess, d := withSliding(t, 3)
	if _, err := tap(h, sess, map[string]interface{}{"target": "text=Continue"}); err != nil {
		t.Fatalf("tap: %v", err)
	}
	if len(d.tapped) != 1 {
		t.Fatalf("tapped %v", d.tapped)
	}
	// It starts at y=200 and moves 100 on each of three snapshots, so it
	// comes to rest at 500 and the center is 540. Tapping at any earlier
	// point would have hit where the button used to be — the first snapshot
	// alone would have aimed at 340.
	if d.tapped[0] != "200,540" {
		t.Errorf("tapped %s, want the button's resting place", d.tapped[0])
	}
}

func TestAStillElementIsTappedWithoutWaitingLong(t *testing.T) {
	h, sess, d := withSliding(t, 0)
	started := time.Now()
	if _, err := tap(h, sess, map[string]interface{}{"target": "text=Continue"}); err != nil {
		t.Fatalf("tap: %v", err)
	}
	// One settle window, not the whole timeout.
	if took := time.Since(started); took > h.settleTimeout/2 {
		t.Errorf("took %s for an element that was never moving", took)
	}
	if d.tapped[0] != "200,240" {
		t.Errorf("tapped %s", d.tapped[0])
	}
}

func TestSomethingThatNeverStopsMovingIsReported(t *testing.T) {
	// Better to say so than to tap a coin flip.
	h, sess, d := withSliding(t, 10000)
	_, err := tap(h, sess, map[string]interface{}{"target": "text=Continue"})
	if err == nil {
		t.Fatal("tapped an element that never stopped moving")
	}
	if !strings.Contains(err.Error(), "still moving") {
		t.Errorf("error %q does not say what happened", err)
	}
	if len(d.tapped) != 0 {
		t.Errorf("tapped anyway: %v", d.tapped)
	}
}

func TestSettlingCanBeTurnedOff(t *testing.T) {
	// The cost is a snapshot per action, which is 0.04s on UiAutomator2 and
	// about two seconds on the dump backend. A caller may reasonably prefer
	// the speed.
	h, sess, d := withSliding(t, 3)
	h.settleWindow = 0
	if _, err := tap(h, sess, map[string]interface{}{"target": "text=Continue"}); err != nil {
		t.Fatalf("tap: %v", err)
	}
	if d.tapped[0] == "200,540" {
		t.Error("settled despite the check being disabled")
	}
}

func TestSettleDefaultsAreSane(t *testing.T) {
	h := NewHandlers()
	if h.settleWindow <= 0 {
		t.Error("settling is off by default")
	}
	if h.settleTimeout <= h.settleWindow {
		t.Errorf("timeout %s does not exceed the window %s", h.settleTimeout, h.settleWindow)
	}
	// It must not outlast the implicit wait's own budget by so much that a
	// tool call dies inside it.
	if h.settleTimeout >= callTimeout {
		t.Errorf("settleTimeout %s is not under callTimeout %s", h.settleTimeout, callTimeout)
	}
}

// flakyDriver fails to read the hierarchy for the first `fails` attempts, the
// way UiAutomator2 does while a toast is animating.
type flakyDriver struct {
	slidingDriver
	fails int
}

func (d *flakyDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	if d.fails > 0 {
		d.fails--
		return nil, fmt.Errorf("unknown error: Cannot set AccessibilityNodeInfo's field 'mSealed' to 'true'")
	}
	return d.slidingDriver.Snapshot(ctx)
}

func TestATransientSnapshotFailureIsRetried(t *testing.T) {
	// Found on the emulator: UiAutomator2 cannot read the hierarchy while a
	// toast animates, and succeeds a moment later. Treating that as final
	// turned a hiccup into a failed tap — and settling made it more likely,
	// because it takes an extra snapshot at exactly the moment something is
	// moving.
	d := &flakyDriver{slidingDriver: slidingDriver{y: 200}, fails: 2}
	h := NewHandlers()
	h.implicitWait = time.Second
	h.settleWindow = 10 * time.Millisecond
	h.settleTimeout = 500 * time.Millisecond
	s := &session{dev: fakeDevice(), driver: d, backend: BackendDump}

	if _, err := tap(h, s, map[string]interface{}{"target": "text=Continue"}); err != nil {
		t.Fatalf("a transient read failure was treated as final: %v", err)
	}
	if len(d.tapped) != 1 {
		t.Errorf("tapped %v", d.tapped)
	}
}

func TestAPersistentSnapshotFailureStillReportsItself(t *testing.T) {
	// Retrying must not bury a device that is genuinely unreachable.
	d := &flakyDriver{slidingDriver: slidingDriver{y: 200}, fails: 1000}
	h := NewHandlers()
	h.implicitWait = 200 * time.Millisecond
	h.settleWindow = 10 * time.Millisecond
	h.settleTimeout = 200 * time.Millisecond
	s := &session{dev: fakeDevice(), driver: d, backend: BackendDump}

	_, err := tap(h, s, map[string]interface{}{"target": "text=Continue"})
	if err == nil {
		t.Fatal("a device that never answered was reported as tapped")
	}
	if !strings.Contains(err.Error(), "mSealed") {
		t.Errorf("error %q lost what the device actually said", err)
	}
}

// boundedSliding can read the button's rectangle without reading the screen,
// as WebDriverAgent can by a test id. The button is where it is now: a
// moving one is reported moved.
type boundedSliding struct {
	slidingDriver
	bounds int
}

func (d *boundedSliding) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	d.calls++
	if d.moves > 0 {
		d.moves--
		d.y += 100
	}
	xml := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">`+
		`<node index="0" package="com.example" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">`+
		`<node index="0" text="Continue" resource-id="com.example:id/go" class="android.widget.Button" `+
		`clickable="true" enabled="true" bounds="[100,%d][300,%d]" />`+
		`</node></hierarchy>`, d.y, d.y+80)
	return uitree.ParseAndroid([]byte(xml))
}

func (d *boundedSliding) ElementBounds(ctx context.Context, n *uitree.Node, t *uitree.Tree) (uitree.Rect, bool, error) {
	d.bounds++
	y := d.y
	if d.moves > 0 {
		y += 100
	}
	return uitree.Rect{X1: 100, Y1: y, X2: 300, Y2: y + 80}, true, nil
}

func withBounded(t *testing.T, moves int) (*Handlers, *boundedSliding) {
	t.Helper()
	d := &boundedSliding{slidingDriver: slidingDriver{y: 200, moves: moves}}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 10 * time.Millisecond
	h.settleTimeout = 500 * time.Millisecond
	h.sessions["fake"] = &session{dev: fakeDevice(), driver: d, backend: BackendDump}
	return h, d
}

// A still element is confirmed by reading it alone: one read of the screen,
// not two. On an iPhone the second was 167ms of every tap.
func TestAStillElementIsConfirmedWithoutASecondRead(t *testing.T) {
	h, d := withBounded(t, 0)
	if _, err := tap(h, h.sessions["fake"], map[string]interface{}{"target": "testid=go"}); err != nil {
		t.Fatalf("tap: %v", err)
	}
	if d.calls != 1 || d.bounds != 1 {
		t.Errorf("read the screen %d times and the element %d, want once each", d.calls, d.bounds)
	}
	if d.tapped[0] != "200,240" {
		t.Errorf("tapped %s", d.tapped[0])
	}
}

// The element read only ever confirms: a moving element sends it back to
// reading the screen until it holds still, and the tap lands where it rests.
func TestAMovingElementStillWaitsWithTheElementRead(t *testing.T) {
	h, d := withBounded(t, 3)
	if _, err := tap(h, h.sessions["fake"], map[string]interface{}{"target": "testid=go"}); err != nil {
		t.Fatalf("tap: %v", err)
	}
	if d.tapped[0] != "200,540" {
		t.Errorf("tapped %s, want the button's resting place", d.tapped[0])
	}
}
