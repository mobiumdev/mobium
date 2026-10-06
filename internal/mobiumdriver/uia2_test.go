package mobiumdriver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// fakeServer stands in for the UiAutomator2 server so the driver's protocol
// handling is tested without an emulator.
type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recorded
	source   string
	// typed is what the fake field holds, so a read-back sees what a write put
	// there.
	typed string
}

type recorded struct {
	method string
	path   string
	body   map[string]interface{}
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{source: miniUIA2Source}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		f.mu.Lock()
		f.requests = append(f.requests, recorded{r.Method, r.URL.Path, body})
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/status":
			w.Write([]byte(`{"value":{"ready":true}}`))
		case r.URL.Path == "/session" && r.Method == http.MethodPost:
			w.Write([]byte(`{"value":{"sessionId":"S1"}}`))
		case strings.HasSuffix(r.URL.Path, "/source"):
			out, _ := json.Marshal(map[string]string{"value": f.source})
			w.Write(out)
		case strings.HasSuffix(r.URL.Path, "/attribute/value"):
			// The real server echoes what the field now holds. Modeled rather
			// than stubbed empty, because WDA.SetText reads this back to catch
			// the keystrokes XCUITest drops (CHALLENGES 61) -- a fake that
			// always answered "" would fail every type.
			f.mu.Lock()
			v := f.typed
			f.mu.Unlock()
			out, _ := json.Marshal(map[string]string{"value": v})
			w.Write(out)
		case strings.HasSuffix(r.URL.Path, "/value") && r.Method == http.MethodPost:
			f.mu.Lock()
			if t, ok := body["text"].(string); ok {
				f.typed = t
			}
			f.mu.Unlock()
			w.Write([]byte(`{"value":null}`))
		case strings.HasSuffix(r.URL.Path, "/clear"):
			f.mu.Lock()
			f.typed = ""
			f.mu.Unlock()
			w.Write([]byte(`{"value":null}`))
		case strings.HasSuffix(r.URL.Path, "/element"):
			w.Write([]byte(`{"value":{"ELEMENT":"EL1","element-6066-11e4-a52e-4f735466cecf":"EL1"}}`))
		default:
			w.Write([]byte(`{"value":null}`))
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeServer) calls() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.requests...)
}

// driverFor returns a UIA2 driver pointed at the fake server, with the session
// already established so tests skip the adb-dependent Start path.
func driverFor(f *fakeServer) *UIA2 {
	c := newW3CClient(5 * time.Second)
	c.setBase(f.URL)
	c.sessionID = "S1"
	return &UIA2{w3c: c}
}

const miniUIA2Source = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<hierarchy index="0" class="hierarchy" rotation="0" width="1080" height="2400">
<android.widget.FrameLayout index="0" class="android.widget.FrameLayout" package="com.x" bounds="[0,0][1080,2400]" enabled="true">
<android.widget.EditText index="0" class="android.widget.EditText" package="com.x" resource-id="com.x:id/field" text="" bounds="[10,20][110,80]" clickable="true" focusable="true" enabled="true" displayed="true" />
<android.widget.Button index="1" class="android.widget.Button" package="com.x" text="Go" bounds="[10,120][110,180]" clickable="true" enabled="true" displayed="true" />
</android.widget.FrameLayout>
</hierarchy>`

func TestUIA2Snapshot(t *testing.T) {
	f := newFakeServer(t)
	tree, err := driverFor(f).Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	entries := tree.Map()
	if len(entries) != 2 {
		t.Fatalf("mapped %d entries, want 2", len(entries))
	}
	if entries[1].Line() != "@e2 Go (button)" {
		t.Errorf("entry = %q", entries[1].Line())
	}
	if got := f.calls()[0].path; got != "/session/S1/source" {
		t.Errorf("requested %q", got)
	}
}

func TestUIA2SnapshotRejectsEmptySource(t *testing.T) {
	f := newFakeServer(t)
	f.source = "   "
	if _, err := driverFor(f).Snapshot(context.Background()); err == nil {
		t.Error("expected an error for an empty hierarchy")
	}
}

func TestUIA2TapSendsPointerSequence(t *testing.T) {
	f := newFakeServer(t)
	if err := driverFor(f).Tap(context.Background(), 540, 930); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	call := f.calls()[0]
	if call.path != "/session/S1/actions" || call.method != http.MethodPost {
		t.Fatalf("tap sent %s %s", call.method, call.path)
	}

	actions := call.body["actions"].([]interface{})[0].(map[string]interface{})
	if actions["type"] != "pointer" {
		t.Errorf("action type = %v", actions["type"])
	}
	steps := actions["actions"].([]interface{})
	first := steps[0].(map[string]interface{})
	if first["x"].(float64) != 540 || first["y"].(float64) != 930 {
		t.Errorf("moved to (%v,%v)", first["x"], first["y"])
	}
	// down then up, or the tap never completes.
	var kinds []string
	for _, s := range steps {
		kinds = append(kinds, s.(map[string]interface{})["type"].(string))
	}
	if strings.Join(kinds, ",") != "pointerMove,pointerDown,pause,pointerUp" {
		t.Errorf("sequence = %v", kinds)
	}
}

func TestUIA2SwipeCarriesDuration(t *testing.T) {
	f := newFakeServer(t)
	// The duration is the difference between a fling and a drag, so it has to
	// reach the move step rather than the pause.
	if err := driverFor(f).Swipe(context.Background(), 10, 20, 30, 40, 750*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	steps := f.calls()[0].body["actions"].([]interface{})[0].(map[string]interface{})["actions"].([]interface{})
	move := steps[2].(map[string]interface{})
	if move["type"] != "pointerMove" {
		t.Fatalf("step 2 is %v", move["type"])
	}
	if move["duration"].(float64) != 750 {
		t.Errorf("duration = %v, want 750", move["duration"])
	}
}

func TestUIA2SetTextTargetsTheElement(t *testing.T) {
	f := newFakeServer(t)
	tree, err := driverFor(f).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var field *uitree.Node
	for _, n := range tree.All() {
		if n.TestID == "com.x:id/field" {
			field = n
		}
	}
	if field == nil {
		t.Fatal("field not found")
	}

	// A real view, unfocused, takes text as it is: no click, which would
	// start an autofill session on a phone.
	before := len(f.calls())
	if err := driverFor(f).SetText(context.Background(), field, "real"); err != nil {
		t.Fatalf("SetText: %v", err)
	}
	for _, c := range f.calls()[before:] {
		if strings.HasSuffix(c.path, "/click") {
			t.Errorf("a real, unfocused field was clicked: %s", c.path)
		}
	}

	field.Virtual = true
	if err := driverFor(f).SetText(context.Background(), field, "hello & 'quoted'"); err != nil {
		t.Fatalf("SetText: %v", err)
	}

	calls := f.calls()
	lookup := calls[len(calls)-3]
	if lookup.body["strategy"] != "id" || lookup.body["selector"] != "com.x:id/field" {
		t.Errorf("looked up by %v=%v, want the resource-id", lookup.body["strategy"], lookup.body["selector"])
	}
	// An unfocused virtual field is clicked first: Flutter takes text only
	// into the field that has focus (CHALLENGES 206).
	if click := calls[len(calls)-2]; click.path != "/session/S1/element/EL1/click" {
		t.Errorf("before setting text, %q, want a click on the field", click.path)
	}
	set := calls[len(calls)-1]
	if set.path != "/session/S1/element/EL1/value" {
		t.Errorf("set text at %q", set.path)
	}
	// The text must arrive as JSON, not through a shell, which is the whole
	// reason this path exists.
	if set.body["text"] != "hello & 'quoted'" {
		t.Errorf("text = %q", set.body["text"])
	}

	// A field that already has focus is not clicked again.
	field.Focused = true
	before = len(f.calls())
	if err := driverFor(f).SetText(context.Background(), field, "again"); err != nil {
		t.Fatalf("SetText: %v", err)
	}
	for _, c := range f.calls()[before:] {
		if strings.HasSuffix(c.path, "/click") {
			t.Errorf("a focused field was clicked: %s", c.path)
		}
	}
}

func TestUIA2FallsBackToXPathWithoutAnID(t *testing.T) {
	f := newFakeServer(t)
	tree, _ := driverFor(f).Snapshot(context.Background())
	var button *uitree.Node
	for _, n := range tree.All() {
		if n.Text == "Go" {
			button = n
		}
	}
	if _, err := driverFor(f).elementFor(context.Background(), button); err != nil {
		t.Fatalf("elementFor: %v", err)
	}
	call := f.calls()[len(f.calls())-1]
	if call.body["strategy"] != "xpath" {
		t.Fatalf("strategy = %v", call.body["strategy"])
	}
	// Our paths are 0-based; XPath positions are 1-based.
	if call.body["selector"] != "/hierarchy/*[1]/*[2]" {
		t.Errorf("selector = %q", call.body["selector"])
	}
}

func TestXPathFor(t *testing.T) {
	tests := []struct{ path, want string }{
		{"", "/hierarchy"},
		{"0", "/hierarchy/*[1]"},
		{"0/1/3/0", "/hierarchy/*[1]/*[2]/*[4]/*[1]"},
	}
	for _, tc := range tests {
		if got := xpathFor(&uitree.Node{Path: tc.path}); got != tc.want {
			t.Errorf("xpathFor(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestUIA2SurfacesServerErrors(t *testing.T) {
	// The server reports failures inside a 200 as often as with a status
	// code, so a driver that trusts the status code reports success.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"value":{"error":"no such element","message":"could not find it\nstack line"}}`))
	}))
	defer srv.Close()

	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &UIA2{w3c: c}
	err := d.Tap(context.Background(), 1, 2)
	if err == nil {
		t.Fatal("a 200 carrying an error payload was treated as success")
	}
	if !strings.Contains(err.Error(), "no such element") {
		t.Errorf("error = %v", err)
	}
	// Only the first line of the stack trace, or the message is unreadable.
	if strings.Contains(err.Error(), "stack line") {
		t.Errorf("error carried the stack trace: %v", err)
	}
}

func TestUIA2RefusesBeforeStart(t *testing.T) {
	d := NewUIA2(nil)
	if _, err := d.Snapshot(context.Background()); err == nil {
		t.Error("an unstarted driver answered a snapshot")
	}
}

func TestUIA2HealthyTracksTheServer(t *testing.T) {
	f := newFakeServer(t)
	d := driverFor(f)

	if !d.Healthy(context.Background()) {
		t.Fatal("a live server reported unhealthy")
	}

	// Quitting the emulator takes the server with it; the driver keeps its
	// forwarded port and session id, both of which now point at nothing.
	f.Close()
	if d.Healthy(context.Background()) {
		t.Error("a dead server reported healthy")
	}
}

func TestUIA2HealthyFalseBeforeStart(t *testing.T) {
	if NewUIA2(nil).Healthy(context.Background()) {
		t.Error("an unstarted driver reported healthy")
	}
}

func TestStaleSessionIsRecognized(t *testing.T) {
	// The exact wording differs between servers and versions, so the check is
	// deliberately broad. Getting this wrong means either never recovering,
	// or reopening the session on every ordinary failure.
	stale := []string{
		"invalid session id: The session identified by abc is not known",
		"no such session",
		"Invalid Session ID",
	}
	for _, msg := range stale {
		if !staleSession(errors.New(msg)) {
			t.Errorf("%q was not recognized as a stale session", msg)
		}
	}
	fresh := []string{
		"no such element",
		"invalid element state: Cannot set the element",
		"",
	}
	for _, msg := range fresh {
		if staleSession(errors.New(msg)) {
			t.Errorf("%q was wrongly treated as a stale session", msg)
		}
	}
	if staleSession(nil) {
		t.Error("nil was treated as a stale session")
	}
}

func TestSessionIsReopenedAndTheCallRetried(t *testing.T) {
	// Another process attaching to the device-side server invalidates our
	// session. Without recovery every later command fails until the daemon
	// is restarted — which is exactly what happened when a client started a
	// second session alongside the CLI's.
	var mu sync.Mutex
	var paths []string
	sessionValid := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/session" && r.Method == http.MethodPost {
			mu.Lock()
			sessionValid = true
			mu.Unlock()
			w.Write([]byte(`{"value":{"sessionId":"NEW"}}`))
			return
		}
		mu.Lock()
		ok := sessionValid
		mu.Unlock()
		if !ok {
			w.Write([]byte(`{"value":{"error":"invalid session id","message":"not known"}}`))
			return
		}
		w.Write([]byte(`{"value":null}`))
	}))
	defer srv.Close()

	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "OLD"
	c.reopen = func(ctx context.Context) error { return c.openSession(ctx, nil) }

	if err := c.pointerSequence(context.Background(), tapActions(1, 2)); err != nil {
		t.Fatalf("the call was not recovered: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 3 {
		t.Fatalf("requests were %v, want the failed call, a new session, then a retry", paths)
	}
	if paths[0] != "/session/OLD/actions" {
		t.Errorf("first request = %s", paths[0])
	}
	if paths[1] != "/session" {
		t.Errorf("second request = %s, want a new session", paths[1])
	}
	// The retry must carry the new session id, not the dead one.
	if paths[2] != "/session/NEW/actions" {
		t.Errorf("retry = %s, want it against the new session", paths[2])
	}
}

func TestNoRecoveryWithoutAReopenHook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"value":{"error":"invalid session id","message":"not known"}}`))
	}))
	defer srv.Close()

	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "OLD"
	if err := c.pointerSequence(context.Background(), tapActions(1, 2)); err == nil {
		t.Error("a stale session without a reopen hook reported success")
	}
}

// TestTransientSnapshotErrors pins which hierarchy-read failures are worth
// retrying and which must surface immediately.
//
// Found by opening the notification shade and mapping straight away: reading
// the tree mid-animation fails with "Cannot set AccessibilityNodeInfo's field
// 'mSealed' to 'true'" because accessibility nodes are being recycled
// underneath the read. Nobody can wait that out deliberately — a caller has no
// way to know an animation is running — and it succeeds a moment later. The
// dump backend had always retried its own equivalent; this one had none, so
// the error reached the user looking like a broken device.
func TestTransientSnapshotErrors(t *testing.T) {
	retry := []string{
		"unknown error: Cannot set AccessibilityNodeInfo's field 'mSealed' to 'true'",
		"AccessibilityNodeInfo is not sealed",
		"UiAutomation not connected",
	}
	for _, msg := range retry {
		if !transientSnapshot(errors.New(msg)) {
			t.Errorf("should retry: %q", msg)
		}
	}

	// These mean the read cannot succeed, and retrying would turn a clear
	// failure into a slow one.
	permanent := []string{
		"connection refused",
		"no such device",
		"parse ui hierarchy: no XML found",
		"invalid session id",
	}
	for _, msg := range permanent {
		if transientSnapshot(errors.New(msg)) {
			t.Errorf("should not retry: %q", msg)
		}
	}
	if transientSnapshot(nil) {
		t.Error("a nil error was treated as retryable")
	}
}

// A bare resource-id is what React Native reports: a testID of "password"
// arrives as resource-id="password", not "<pkg>:id/password". UiAutomator2's
// "id" strategy qualifies a bare name with the package under test, so it
// searches for "<pkg>:id/password", finds nothing, and text entry failed on a
// field `map` had just handed out a ref for -- while `tap` on the same ref
// worked, because that uses coordinates from the snapshot. CHALLENGES 55.
func TestUIA2SetTextFallsBackWhenABareIDIsNotFound(t *testing.T) {
	var strategies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/source"):
			out, _ := json.Marshal(map[string]string{"value": bareIDSource})
			w.Write(out)
		case strings.HasSuffix(r.URL.Path, "/element"):
			s, _ := body["strategy"].(string)
			strategies = append(strategies, s)
			// The device reports this inside a 200, as it does everything.
			if s == "id" {
				w.Write([]byte(`{"value":{"error":"no such element","message":"could not be located"}}`))
				return
			}
			w.Write([]byte(`{"value":{"ELEMENT":"EL1","element-6066-11e4-a52e-4f735466cecf":"EL1"}}`))
		default:
			w.Write([]byte(`{"value":null}`))
		}
	}))
	defer srv.Close()

	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	u := &UIA2{w3c: c}

	tree, err := u.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var field *uitree.Node
	for _, n := range tree.All() {
		if n.TestID == "password" {
			field = n
		}
	}
	if field == nil {
		t.Fatal("the bare-id field is not in the tree")
	}

	if err := u.SetText(context.Background(), field, "hunter2"); err != nil {
		t.Fatalf("SetText: %v", err)
	}
	if len(strategies) != 2 || strategies[0] != "id" || strategies[1] != "xpath" {
		t.Errorf("lookup strategies = %v, want [id xpath]", strategies)
	}
}

const bareIDSource = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<hierarchy index="0" class="hierarchy" rotation="0" width="1080" height="2400">
<android.widget.FrameLayout index="0" class="android.widget.FrameLayout" package="com.x" bounds="[0,0][1080,2400]" enabled="true">
<android.widget.EditText index="0" class="android.widget.EditText" package="com.x" resource-id="password" text="" password="true" bounds="[10,20][110,80]" clickable="true" focusable="true" enabled="true" displayed="true" />
</android.widget.FrameLayout>
</hierarchy>`

// The idle wait is capped and the toast listener turned off, and both are
// read back: a server that accepted a setting and kept its own is reported,
// not believed. At the default idle wait every read of an animating screen
// blocked until the animation ended (CHALLENGES 109); with the listener on,
// no read got through while a system toast was up (CHALLENGES 254).
func TestUIA2ConfiguresTheServer(t *testing.T) {
	for _, tc := range []struct {
		readBack string
		ok       bool
	}{
		{`{"waitForIdleTimeout":500,"enableNotificationListener":false}`, true},
		{`{"waitForIdleTimeout":10000,"enableNotificationListener":false}`, false},
		{`{"waitForIdleTimeout":0,"enableNotificationListener":false}`, false},
		{`{"waitForIdleTimeout":500,"enableNotificationListener":true}`, false},
		{`{"waitForIdleTimeout":500}`, false},
	} {
		var posted map[string]interface{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost {
				var body map[string]map[string]interface{}
				json.NewDecoder(r.Body).Decode(&body)
				posted = body["settings"]
				w.Write([]byte(`{"value":null}`))
				return
			}
			w.Write([]byte(`{"value":` + tc.readBack + `}`))
		}))
		c := newW3CClient(5 * time.Second)
		c.setBase(srv.URL)
		c.sessionID = "S1"
		err := (&UIA2{w3c: c}).configure(context.Background())
		srv.Close()
		if posted["waitForIdleTimeout"] != float64(idleWaitCap) || posted["enableNotificationListener"] != false {
			t.Errorf("posted %v, want waitForIdleTimeout %d and enableNotificationListener false", posted, idleWaitCap)
		}
		if (err == nil) != tc.ok {
			t.Errorf("read back %s: err = %v", tc.readBack, err)
		}
	}
}
