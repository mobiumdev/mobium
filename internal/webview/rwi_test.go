package webview

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
)

// These drive a real booted simulator. There is no useful fixture for a
// protocol conversation: the whole difficulty of Remote Web Inspector is
// timing and message ordering, and a recording would assert only that today's
// code reproduces yesterday's transcript. They skip with an actionable
// message when no simulator is running, which is the same bargain
// internal/plist's webinspectord test makes.

func bootedSimulator(t *testing.T) *device.Simctl {
	t.Helper()
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("no xcrun, so no simulator")
	}
	out, err := exec.Command("xcrun", "simctl", "list", "devices", "booted").Output()
	if err != nil || !strings.Contains(string(out), "Booted") {
		t.Skip("no booted simulator — `xcrun simctl boot <udid>`, then open a page in Safari")
	}
	path, err := device.FindSimctl()
	if err != nil {
		t.Skip(err.Error())
	}
	return &device.Simctl{Path: path, UDID: "booted"}
}

func TestInspectorListsAndDrivesAPage(t *testing.T) {
	sim := bootedSimulator(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	in, err := OpenInspector(ctx, sim)
	if err != nil {
		t.Skipf("no web inspector on the simulator: %v", err)
	}
	defer in.Close()

	contexts, err := in.Contexts(ctx)
	if err != nil {
		t.Fatalf("Contexts: %v", err)
	}
	if len(contexts) == 0 {
		t.Skip("no inspectable page — open one in Safari and enable Web Inspector " +
			"(see docs/checks/ios-webview-probe.sh)")
	}
	for _, c := range contexts {
		if !strings.HasPrefix(c.ID, "WEBVIEW_") {
			t.Errorf("context id %q does not match the shape Android uses", c.ID)
		}
		if c.Socket == "" {
			t.Errorf("%s carries no page identifier, so it cannot be attached to", c.ID)
		}
	}

	// Listing twice on one connection must work. It did not at first: the
	// application list is sent once per connection, so re-announcing produced
	// nothing and the second listing came back empty.
	again, err := in.Contexts(ctx)
	if err != nil {
		t.Fatalf("second Contexts on the same connection: %v", err)
	}
	if len(again) != len(contexts) {
		t.Errorf("first listing had %d contexts, second had %d", len(contexts), len(again))
	}

	page, err := AttachIOS(ctx, in, contexts[0])
	if err != nil {
		t.Fatalf("AttachIOS: %v", err)
	}

	// The load-bearing assertion: a value the *page* computed, not one we put
	// there. Everything up to here could pass with a broken target wrapping.
	got, err := page.Evaluate(ctx, "1 + 1")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got != "2" {
		t.Errorf("1 + 1 came back as %q", got)
	}

	m, err := page.LayoutMetrics(ctx)
	if err != nil {
		t.Fatalf("LayoutMetrics: %v", err)
	}
	if m.CSSWidth <= 0 || m.CSSHeight <= 0 {
		t.Errorf("viewport = %vx%v", m.CSSWidth, m.CSSHeight)
	}

	if _, err := page.Text(ctx); err != nil {
		t.Errorf("Text: %v", err)
	}
	if _, err := page.Map(ctx); err != nil {
		t.Errorf("Map: %v", err)
	}

	// Detaching and re-attaching on the same connection has to work. It did
	// not until Close started sending _rpc_forwardDidClose:. WebKit announces
	// Target.targetCreated once per page per connection, so a page left open
	// could never be attached to again — the second setup was accepted and
	// silently never answered.
	if err := page.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	again2, err := AttachIOS(ctx, in, contexts[0])
	if err != nil {
		t.Fatalf("re-attaching after a clean detach: %v", err)
	}
	defer again2.Close()
	if got, err := again2.Evaluate(ctx, "1 + 1"); err != nil || got != "2" {
		t.Errorf("after re-attaching, 1 + 1 = %q (err %v)", got, err)
	}
}

func TestInspectorSocketExplainsItselfWhenAbsent(t *testing.T) {
	// A path that is not there must not look like a protocol failure.
	t.Setenv("MOBIUM_RWI_SOCKET", "/no/such/socket")
	_, err := dialInspector("/no/such/socket", "mobium-test")
	if err == nil {
		t.Fatal("dialled a socket that does not exist")
	}
	if !strings.Contains(err.Error(), "web inspector") {
		t.Errorf("error = %v", err)
	}
}

func TestConnectionIDNamesUs(t *testing.T) {
	// It shows up in Safari's develop menu, so it should say who is driving.
	if id := connectionID(); !strings.HasPrefix(id, "mobium-") {
		t.Errorf("connection id = %q", id)
	}
	if os.Getpid() <= 0 {
		t.Skip()
	}
}

// A relaunched app is a new process: webinspectord announces it connecting
// and the old one disconnecting, and the held connection's set has to follow
// or it keeps asking a dead process for pages. This is bookkeeping rather
// than protocol timing, so it is tested apart from a simulator.
func TestInspectorFollowsAppsThatComeAndGo(t *testing.T) {
	i := &Inspector{apps: map[string]string{"PID:100": "dev.mobium.mobiumapp"}}
	msgs := make(chan rwiMessage, 4)
	msgs <- rwiMessage{Selector: "_rpc_applicationConnected:", Argument: map[string]any{
		"WIRApplicationIdentifierKey":       "PID:200",
		"WIRApplicationBundleIdentifierKey": "dev.mobium.mobiumapp",
	}}
	msgs <- rwiMessage{Selector: "_rpc_applicationDisconnected:", Argument: map[string]any{
		"WIRApplicationIdentifierKey": "PID:100",
	}}
	msgs <- rwiMessage{Selector: "_rpc_applicationSentListing:", Argument: map[string]any{
		"WIRApplicationIdentifierKey": "PID:999",
	}}
	close(msgs)
	i.watch(msgs, func() {})

	got, _ := i.knownApps()
	if len(got) != 1 || got["PID:200"] != "dev.mobium.mobiumapp" {
		t.Errorf("apps are %v, want only the relaunched PID:200", got)
	}
}

// The states measured on an iPhone 15 Plus, iOS 26.6.2, after MobiumApp
// opened a page in Safari and was brought back: MobiumApp 2, Safari 1 — left
// there, its page still published. Safari's page is behind; MobiumApp's is not.
func TestBehindFollowsWebKitsActiveFlag(t *testing.T) {
	states := statesIn(map[string]any{"WIRApplicationDictionaryKey": map[string]any{
		"PID:8053": map[string]any{"WIRApplicationBundleIdentifierKey": "dev.mobium.mobiumapp", "WIRIsApplicationActiveKey": uint64(2)},
		"PID:7927": map[string]any{"WIRApplicationBundleIdentifierKey": "com.apple.mobilesafari", "WIRIsApplicationActiveKey": uint64(1)},
		"PID:3153": map[string]any{"WIRApplicationBundleIdentifierKey": "com.apple.email.maild", "WIRIsApplicationActiveKey": uint64(0)},
		"PID:9000": map[string]any{"WIRApplicationBundleIdentifierKey": "com.apple.SafariViewService",
			"WIRIsApplicationActiveKey": uint64(0), "WIRHostApplicationIdentifierKey": "PID:8053"},
		"PID:9001": map[string]any{"WIRApplicationBundleIdentifierKey": "no.flag.reported"},
	}})
	for id, want := range map[string]bool{
		"PID:8053": false, // in front
		"PID:7927": true,  // inactive
		"PID:3153": true,  // background
		"PID:9000": false, // a proxy hosted by the app in front
		"PID:9001": false, // no flag: not hidden on a guess
		"PID:none": false, // unknown
	} {
		if got := behind(id, states); got != want {
			t.Errorf("behind(%s) = %v, want %v", id, got, want)
		}
	}

	// Mid-switch nothing is in front, and nothing is hidden: the positive
	// control that the rule is not simply "active != 2".
	mid := statesIn(map[string]any{"WIRApplicationDictionaryKey": map[string]any{
		"PID:8053": map[string]any{"WIRIsApplicationActiveKey": uint64(1)},
		"PID:7927": map[string]any{"WIRIsApplicationActiveKey": uint64(1)},
	}})
	if behind("PID:7927", mid) || behind("PID:8053", mid) {
		t.Error("with nothing in front, a page was hidden")
	}

	// And an update moves an application: _rpc_applicationUpdated: carries
	// one inline.
	one := statesIn(map[string]any{"WIRApplicationIdentifierKey": "PID:7927", "WIRIsApplicationActiveKey": uint64(2)})
	if st := one["PID:7927"]; !st.known || st.active != appActive {
		t.Errorf("an inline update read as %+v", st)
	}
}
