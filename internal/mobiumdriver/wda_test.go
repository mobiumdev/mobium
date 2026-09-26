package mobiumdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// wdaFor points a WDA driver at a fake server with a session established, so
// its protocol handling is exercised without a simulator.
func wdaFor(f *fakeServer) *WDA {
	c := newW3CClient(5 * time.Second)
	c.setBase(f.URL)
	c.sessionID = "S1"
	return &WDA{w3c: c}
}

// Shaped after a real WebDriverAgent capture: `accessible` is what marks an
// element as a distinct thing, and `hittable` — which WDA documents — is not
// emitted at all by a simulator.
const miniIOSSource = `<?xml version="1.0" encoding="UTF-8"?>
<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="App" enabled="true" visible="true" accessible="false" x="0" y="0" width="390" height="844" index="0">
  <XCUIElementTypeTextField type="XCUIElementTypeTextField" name="field" label="Email" value="" enabled="true" visible="true" accessible="true" x="10" y="20" width="100" height="40" index="0"/>
  <XCUIElementTypeButton type="XCUIElementTypeButton" label="Go" enabled="true" visible="true" accessible="true" x="10" y="80" width="100" height="40" index="1"/>
</XCUIElementTypeApplication>`

func TestWDASnapshotParsesIOSSource(t *testing.T) {
	f := newFakeServer(t)
	f.source = miniIOSSource

	tree, err := wdaFor(f).Snapshot(context.Background())
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
}

func TestWDASharesTheW3CActionShape(t *testing.T) {
	// Both servers take the same pointer chain; that is why the client is
	// shared. A divergence here would mean one of them is being sent
	// something it does not understand.
	f := newFakeServer(t)
	if err := wdaFor(f).Tap(context.Background(), 195, 344); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	call := f.calls()[0]
	if call.path != "/session/S1/actions" || call.method != http.MethodPost {
		t.Fatalf("tap sent %s %s", call.method, call.path)
	}
	steps := call.body["actions"].([]interface{})[0].(map[string]interface{})["actions"].([]interface{})
	first := steps[0].(map[string]interface{})
	if first["x"].(float64) != 195 || first["y"].(float64) != 344 {
		t.Errorf("moved to (%v,%v)", first["x"], first["y"])
	}
}

func TestWDAUsesAccessibilityIDWhenPresent(t *testing.T) {
	f := newFakeServer(t)
	f.source = miniIOSSource
	d := wdaFor(f)

	tree, _ := d.Snapshot(context.Background())
	var field *uitree.Node
	for _, n := range tree.All() {
		if n.TestID == "field" {
			field = n
		}
	}
	if field == nil {
		t.Fatal("field not found")
	}
	if err := d.SetText(context.Background(), field, "a@b.com"); err != nil {
		t.Fatalf("SetText: %v", err)
	}

	calls := f.calls()

	// Found by role rather than by position. SetText now reads the value back
	// to catch the keystrokes XCUITest drops (CHALLENGES 61), so the type is
	// no longer the last call — and a test that assumes it is would break on
	// any future step added after it.
	var lookup, typed *recorded
	for i := range calls {
		c := &calls[i]
		switch {
		case strings.HasSuffix(c.path, "/element") && c.method == http.MethodPost:
			lookup = c
		case strings.HasSuffix(c.path, "/value") && c.method == http.MethodPost:
			typed = c
		}
	}
	if lookup == nil || typed == nil {
		t.Fatalf("expected a lookup and a type among %d calls", len(calls))
	}
	// iOS names this strategy differently from Android's "id".
	if lookup.body["using"] != "accessibility id" || lookup.body["value"] != "field" {
		t.Errorf("looked up by %v=%v", lookup.body["using"], lookup.body["value"])
	}
	if got := typed.body["text"]; got != "a@b.com" {
		t.Errorf("text = %v", got)
	}
}

func TestIOSXPathHasNoHierarchyRoot(t *testing.T) {
	// WDA's document is rooted at the application element; Android's has a
	// <hierarchy> wrapper. Reusing the Android form would resolve nothing.
	tests := []struct{ path, want string }{
		{"", "/*"},
		{"0", "/*[1]"},
		{"0/1/2", "/*[1]/*[2]/*[3]"},
	}
	for _, tc := range tests {
		if got := iosXPathFor(&uitree.Node{Path: tc.path}); got != tc.want {
			t.Errorf("iosXPathFor(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
	if strings.Contains(iosXPathFor(&uitree.Node{Path: "0"}), "hierarchy") {
		t.Error("the iOS XPath carried Android's hierarchy root")
	}
}

func TestWDAHealthTracksTheServer(t *testing.T) {
	f := newFakeServer(t)
	d := wdaFor(f)
	if !d.Healthy(context.Background()) {
		t.Fatal("a live server reported unhealthy")
	}
	f.Close()
	if d.Healthy(context.Background()) {
		t.Error("a dead server reported healthy")
	}
}

func TestWDARefusesBeforeStart(t *testing.T) {
	if _, err := NewWDA(nil).Snapshot(context.Background()); err == nil {
		t.Error("an unstarted driver answered a snapshot")
	}
}

func TestWDAConvertsPixelsToPointsForInput(t *testing.T) {
	// The tree is scaled up to pixels on the way in, so gestures arrive in
	// pixels and have to be converted back. Getting this wrong lands every
	// tap at three times the intended offset — silently.
	f := newFakeServer(t)
	d := wdaFor(f)
	d.scale = 3

	if err := d.Tap(context.Background(), 471, 2416); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	steps := f.calls()[0].body["actions"].([]interface{})[0].(map[string]interface{})["actions"].([]interface{})
	move := steps[0].(map[string]interface{})
	if move["x"].(float64) != 157 || move["y"].(float64) != 805 {
		t.Errorf("sent (%v,%v) to WebDriverAgent, want (157,805) points",
			move["x"], move["y"])
	}
}

func TestWDAScaleOfOneLeavesCoordinatesAlone(t *testing.T) {
	f := newFakeServer(t)
	d := wdaFor(f)
	d.scale = 1

	if err := d.Tap(context.Background(), 100, 200); err != nil {
		t.Fatal(err)
	}
	steps := f.calls()[0].body["actions"].([]interface{})[0].(map[string]interface{})["actions"].([]interface{})
	move := steps[0].(map[string]interface{})
	if move["x"].(float64) != 100 || move["y"].(float64) != 200 {
		t.Errorf("coordinates were altered at scale 1: (%v,%v)", move["x"], move["y"])
	}
}

func TestWDASwipeConvertsBothEnds(t *testing.T) {
	f := newFakeServer(t)
	d := wdaFor(f)
	d.scale = 3

	if err := d.Swipe(context.Background(), 300, 600, 300, 1200, 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	steps := f.calls()[0].body["actions"].([]interface{})[0].(map[string]interface{})["actions"].([]interface{})
	start := steps[0].(map[string]interface{})
	end := steps[2].(map[string]interface{})
	if start["y"].(float64) != 200 || end["y"].(float64) != 400 {
		t.Errorf("swipe sent y %v -> %v, want 200 -> 400 points", start["y"], end["y"])
	}
}

func TestWDASnapshotReturnsPixels(t *testing.T) {
	f := newFakeServer(t)
	f.source = miniIOSSource
	d := wdaFor(f)
	d.scale = 3

	tree, err := d.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// The fixture's button is at y=80 in points.
	for _, e := range tree.Map() {
		if e.Label == "Go" && e.Bounds.Y1 != 240 {
			t.Errorf("button y1 = %d, want 240 pixels", e.Bounds.Y1)
		}
	}
}

func TestWDARejectsImplausibleScale(t *testing.T) {
	// A nonsense scale would multiply every coordinate silently, so it is
	// refused and the driver stays at 1 rather than adopting it.
	for _, body := range []string{
		`{"value":{"scale":0}}`,
		`{"value":{"scale":99}}`,
		`{"value":{}}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(body))
		}))
		c := newW3CClient(5 * time.Second)
		c.setBase(srv.URL)
		c.sessionID = "S1"
		d := &WDA{w3c: c, scale: 1}

		if _, err := d.readScale(context.Background()); err == nil {
			t.Errorf("accepted %s", body)
		}
		if d.scale != 1 {
			t.Errorf("scale changed to %v on a bad response", d.scale)
		}
		srv.Close()
	}
}

// A keystroke dropped by XCUITest must be caught and retried, not reported as
// success. Four runs in five dropped one character from the middle of a string
// when typing into a second field straight after a first — measured on an
// iPhone 17 Pro simulator, iOS 26.5 — while the call said it had typed the
// whole thing. See CHALLENGES 61.
func TestWDARetriesADroppedKeystroke(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/attribute/value"):
			// The first type loses a character, exactly as the device does;
			// the second lands whole.
			v := "a@b.com"
			if posts < 2 {
				v = "ab.com"
			}
			out, _ := json.Marshal(map[string]string{"value": v})
			w.Write(out)
		case strings.HasSuffix(r.URL.Path, "/value") && r.Method == http.MethodPost:
			posts++
			w.Write([]byte(`{"value":null}`))
		case strings.HasSuffix(r.URL.Path, "/element"):
			w.Write([]byte(`{"value":{"ELEMENT":"EL1","element-6066-11e4-a52e-4f735466cecf":"EL1"}}`))
		default:
			w.Write([]byte(`{"value":null}`))
		}
	}))
	defer srv.Close()

	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &WDA{w3c: c, scale: 1}

	node := &uitree.Node{TestID: "field"}
	if err := d.SetText(context.Background(), node, "a@b.com"); err != nil {
		t.Fatalf("a dropped keystroke was not recovered by retrying: %v", err)
	}
	if posts != 2 {
		t.Errorf("typed %d times, want 2 — one attempt plus one retry", posts)
	}
}

// And when retrying does not help, it must say so rather than claim success.
// A type that silently leaves the wrong text in a field is the failure this
// whole project is organized against.
func TestWDARefusesWhenTheTextNeverLands(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/attribute/value"):
			out, _ := json.Marshal(map[string]string{"value": "wrong"})
			w.Write(out)
		case strings.HasSuffix(r.URL.Path, "/element"):
			w.Write([]byte(`{"value":{"ELEMENT":"EL1","element-6066-11e4-a52e-4f735466cecf":"EL1"}}`))
		default:
			w.Write([]byte(`{"value":null}`))
		}
	}))
	defer srv.Close()

	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &WDA{w3c: c, scale: 1}

	err := d.SetText(context.Background(), &uitree.Node{TestID: "field"}, "a@b.com")
	if err == nil {
		t.Fatal("a field holding the wrong text was reported as typed successfully")
	}
	for _, want := range []string{"a@b.com", "wrong"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not say what it found (%q): %v", want, err)
		}
	} // Reported as the failure this project exists to catch: it said it
	// typed, and reading back said otherwise. docs/decisions/0005.
	if mobiumerr.CodeOf(err) != mobiumerr.NotConfirmed {
		t.Errorf("code = %s, want not_confirmed", mobiumerr.CodeOf(err))
	}
}

// A password field reads back as bullets, so it cannot be confirmed this way
// and must not be tried — the check found this on a login screen, "hunter2"
// against "•••••••".
func TestWDADoesNotVerifyAPasswordField(t *testing.T) {
	f := newFakeServer(t)
	d := wdaFor(f)
	node := &uitree.Node{TestID: "field", Password: true}
	if err := d.SetText(context.Background(), node, "hunter2"); err != nil {
		t.Fatalf("typing into a password field failed: %v", err)
	}
	cleared, typedAfter := false, false
	for _, c := range f.calls() {
		if strings.HasSuffix(c.path, "/attribute/value") {
			t.Error("a password field was read back, which can only ever disagree")
		}
		if strings.HasSuffix(c.path, "/clear") {
			cleared = true
		}
		if strings.HasSuffix(c.path, "/value") && cleared {
			typedAfter = true
		}
	}
	// WebDriverAgent appends, and a password field has no read-back to put
	// that right, so it is cleared first: ten characters then seven more
	// held seventeen (CHALLENGES 103).
	if !cleared || !typedAfter {
		t.Errorf("the field was not cleared before typing (cleared %v, typed after %v)", cleared, typedAfter)
	}
}

// On a phone the active-app hint goes on before a switch Mobium makes and
// comes off on the first read that shows the app it named. Left on, it pinned
// every read to that app and hid a system dialog from `map` (CHALLENGES 76);
// never set, the first read after the switch stalled for 61s (71).
func TestWDAPhoneHintLastsOneSwitch(t *testing.T) {
	var mu sync.Mutex
	var hints []string
	foreground := "com.example.old"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/appium/settings"):
			var body struct {
				Settings map[string]string `json:"settings"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			hints = append(hints, body.Settings["defaultActiveApplication"])
			mu.Unlock()
			w.Write([]byte(`{"value":null}`))
		case strings.HasSuffix(r.URL.Path, "/source"):
			mu.Lock()
			app := foreground
			mu.Unlock()
			src := `<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="A" bundleId="` +
				app + `" enabled="true" visible="true" accessible="false" x="0" y="0" width="10" height="10" index="0"/>`
			out, _ := json.Marshal(map[string]string{"value": src})
			w.Write(out)
		default:
			w.Write([]byte(`{"value":null}`))
		}
	}))
	defer srv.Close()

	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &WDA{w3c: c, scale: 1, phone: &device.Devicectl{}}
	ctx := context.Background()
	got := func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(hints, ",")
	}

	d.expectApp(ctx, "com.example.new")
	if got() != "com.example.new" {
		t.Fatalf("hints = %s, want the new app named before the switch", got())
	}

	// A read that still shows the old app — the switch has not landed — must
	// leave the hint on, or the stall comes back.
	if _, err := d.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	if got() != "com.example.new" {
		t.Errorf("hint came off before the expected app was read: %s", got())
	}

	// The first read of the new app takes it off, once.
	mu.Lock()
	foreground = "com.example.new"
	mu.Unlock()
	d.Snapshot(ctx)
	d.Snapshot(ctx)
	if got() != "com.example.new,auto" {
		t.Errorf("hints = %s, want it cleared exactly once when the app arrived", got())
	}

	// A switch that did not happen must not leave it on either.
	d.expectApp(ctx, "com.example.never")
	d.clearExpected(ctx)
	if got() != "com.example.new,auto,com.example.never,auto" {
		t.Errorf("hints = %s, want a failed switch's hint cleared", got())
	}

	// And a simulator is never hinted: the stall was measured only on a phone.
	sim := &WDA{w3c: c, scale: 1}
	sim.expectApp(ctx, "com.example.sim")
	if strings.Contains(got(), "sim") {
		t.Errorf("a simulator was hinted: %s", got())
	}
}

// A W3C server's own error code is kept, and mapped where Mobium has a code
// for the same thing: the three decisions that once matched text now read it.
func TestServerErrorsKeepTheirW3CCode(t *testing.T) {
	cases := []struct{ w3c, want string }{
		{"no such element", "no_such_element"},
		{"no such alert", "no_such_alert"},
		{"unknown command", "unsupported"},
		{"timeout", "timeout"},
		{"invalid session id", "device_server"},
		{"stale element reference", "device_server"},
	}
	for _, tc := range cases {
		raw := json.RawMessage(`{"value":{"error":"` + tc.w3c + `","message":"because\nmore"}}`)
		e := errorFrom(raw)
		if e == nil {
			t.Fatalf("%s: no error read", tc.w3c)
		}
		if string(e.Code) != tc.want || e.Details["w3c"] != tc.w3c {
			t.Errorf("%s: code %s, w3c %v; want %s", tc.w3c, e.Code, e.Details["w3c"], tc.want)
		}
		if e.Error() != tc.w3c+": because" {
			t.Errorf("%s: message %q", tc.w3c, e.Error())
		}
	}
	if errorFrom(json.RawMessage(`{"value":{"ready":true}}`)) != nil {
		t.Error("a success was read as an error")
	}
	// The session check reads the code, and still reads a bare status line.
	if !staleSession(errorFrom(json.RawMessage(`{"value":{"error":"invalid session id","message":"x"}}`))) {
		t.Error("an invalid session id was not recognized by its code")
	}
	if !noAlert(errorFrom(json.RawMessage(`{"value":{"error":"no such alert","message":"x"}}`))) {
		t.Error("a missing alert was not recognized by its code")
	}
}

// A phone's refusal of a simulator-only capability names why, so the tool
// layer does not answer "the webdriveragent backend cannot …" — true of the
// phone, false of the backend, and silent on the reason.
func TestAPhoneSaysWhyItDeclines(t *testing.T) {
	phone := &WDA{phone: &device.Devicectl{}}
	for _, c := range []string{CapAppearance, CapPermissions, CapRecording, CapClearData} {
		err := Declined(phone, c)
		if err == nil || !strings.Contains(err.Error(), "real iPhone") {
			t.Errorf("%s: %v", c, err)
		}
	}
	if err := Declined(phone, CapGestures); err != nil {
		t.Errorf("a capability the phone has was declined: %v", err)
	}
	if err := Declined(&WDA{}, CapRecording); err != nil {
		t.Errorf("a simulator declined: %v", err)
	}
}

// Every capability whose tool refuses through the generic "the X backend
// cannot" must, on a built-in backend that lacks it, say why instead —
// defect 101 found five on the phone that did not, and the audit behind it
// found eight more. The optional readers are absent from the list because
// their absence is not a refusal, and the rest refuse with a reason inline.
func TestEveryBuiltInRefusalSaysWhy(t *testing.T) {
	lacks := map[string]func(Driver) bool{
		CapAppearance:    func(d Driver) bool { _, ok := AsAppearance(d); return !ok },
		CapPermissions:   func(d Driver) bool { _, ok := AsPermissions(d); return !ok },
		CapOrientation:   func(d Driver) bool { _, ok := AsOrientation(d); return !ok },
		CapLocalization:  func(d Driver) bool { _, ok := AsLocalization(d); return !ok },
		CapInterruptions: func(d Driver) bool { _, ok := AsInterruptions(d); return !ok },
		CapClock:         func(d Driver) bool { _, ok := AsClock(d); return !ok },
		CapNotifications: func(d Driver) bool { _, ok := AsNotifications(d); return !ok },
		CapClipboard:     func(d Driver) bool { _, ok := AsClipboard(d); return !ok },
		CapAlerts:        func(d Driver) bool { _, ok := AsAlerts(d); return !ok },
		CapRecording:     func(d Driver) bool { _, ok := AsScreenRecorder(d); return !ok },
		CapClearData:     func(d Driver) bool { _, ok := AsDataClearer(d); return !ok },
	}
	drivers := map[string]Driver{
		"simulator": &WDA{},
		"phone":     &WDA{phone: &device.Devicectl{}},
		"dump":      NewAndroid(nil),
	}
	for name, d := range drivers {
		for c, lacking := range lacks {
			if !lacking(d) {
				continue
			}
			if err := Declined(d, c); err == nil {
				t.Errorf("%s lacks %s and gives no reason", name, c)
			}
		}
	}
}

// Each retry types more slowly than the last. At one speed, a form that
// re-renders on every keystroke lost the same keystroke on every attempt —
// "nobody" arrived as "nbody" twice on MobiumApp's login — so a retry at that
// speed could only repeat it.
func TestWDARetriesMoreSlowly(t *testing.T) {
	var mu sync.Mutex
	var speeds []interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/attribute/value"):
			w.Write([]byte(`{"value":"nbody"}`))
		case strings.HasSuffix(r.URL.Path, "/value"):
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			speeds = append(speeds, body["frequency"])
			mu.Unlock()
			w.Write([]byte(`{"value":null}`))
		case strings.HasSuffix(r.URL.Path, "/element"):
			w.Write([]byte(`{"value":{"ELEMENT":"EL1","element-6066-11e4-a52e-4f735466cecf":"EL1"}}`))
		default:
			w.Write([]byte(`{"value":null}`))
		}
	}))
	defer srv.Close()
	c := newW3CClient(5 * time.Second)
	c.setBase(srv.URL)
	c.sessionID = "S1"
	d := &WDA{w3c: c, scale: 1}

	err := d.SetText(context.Background(), &uitree.Node{TestID: "field"}, "nobody")
	if err == nil || !strings.Contains(err.Error(), "6 keys a second") {
		t.Errorf("the error does not say how slowly it last tried: %v", err)
	}
	want := []interface{}{nil, float64(20), float64(6)}
	if fmt.Sprint(speeds) != fmt.Sprint(want) {
		t.Errorf("typed at %v, want the server's speed then slower: %v", speeds, want)
	}
}
