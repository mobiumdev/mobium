package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/uitree"
)

func TestLocatorForParsesNonRefs(t *testing.T) {
	h := NewHandlers()
	loc, err := h.locatorFor("emulator-5554", "text=Sign In")
	if err != nil {
		t.Fatalf("locatorFor: %v", err)
	}
	if loc.Kind != uitree.KindText || loc.Value != "Sign In" {
		t.Errorf("locator = %+v", loc)
	}
}

func TestLocatorForRefWithoutMap(t *testing.T) {
	h := NewHandlers()
	_, err := h.locatorFor("emulator-5554", "@e1")
	if err == nil {
		t.Fatal("expected an error with no map taken")
	}
	if !strings.Contains(err.Error(), "app_map") {
		t.Errorf("error %q does not point at the fix", err)
	}
}

func TestRefTablesAreIsolatedPerDevice(t *testing.T) {
	// Two emulators must not share refs: @e1 means something different on
	// each screen, and crossing them taps the wrong device's element.
	h := NewHandlers()
	h.refs["emulator-5554"] = &refTable{entries: map[string]uitree.Locator{
		"@e1": {Kind: uitree.KindTestID, Value: "a"},
	}}

	if _, err := h.locatorFor("emulator-5554", "@e1"); err != nil {
		t.Fatalf("first device: %v", err)
	}
	if _, err := h.locatorFor("emulator-5556", "@e1"); err == nil {
		t.Error("second device resolved the first device's ref")
	}
}

func TestUnknownRefListsKnownOnes(t *testing.T) {
	h := NewHandlers()
	h.refs["dev"] = &refTable{entries: map[string]uitree.Locator{
		"@e1": {Kind: uitree.KindText, Value: "a"},
		"@e2": {Kind: uitree.KindText, Value: "b"},
	}}
	_, err := h.locatorFor("dev", "@e7")
	if err == nil {
		t.Fatal("expected an error for an unknown ref")
	}
	if !strings.Contains(err.Error(), "@e1") || !strings.Contains(err.Error(), "@e2") {
		t.Errorf("error %q does not list the known refs", err)
	}
}

func TestKnownRefsSummarizesLargeTables(t *testing.T) {
	entries := map[string]uitree.Locator{}
	for _, r := range []string{"@e1", "@e2", "@e3", "@e4", "@e5", "@e6", "@e7",
		"@e8", "@e9", "@e10", "@e11", "@e12", "@e13"} {
		entries[r] = uitree.Locator{Kind: uitree.KindText, Value: r}
	}
	got := knownRefs(&refTable{entries: entries})
	if !strings.Contains(got, "13 refs") || !strings.Contains(got, "@e13") {
		t.Errorf("summary = %q", got)
	}
}

func TestPickOne(t *testing.T) {
	tree := loadLauncher(t)

	// Unique.
	loc := uitree.Locator{Kind: uitree.KindLabel, Value: "Gmail", Exact: true}
	if _, err := pickOne(loc, tree); err != nil {
		t.Errorf("unique locator failed: %v", err)
	}

	// Ambiguous: must refuse rather than pick one.
	loc = uitree.Locator{Kind: uitree.KindRole, Value: "button"}
	_, err := pickOne(loc, tree)
	if err == nil {
		t.Fatal("ambiguous locator was resolved")
	}
	if !strings.Contains(err.Error(), "matches") {
		t.Errorf("error = %v", err)
	}
	// It already is a role locator, so suggesting role= would be nonsense.
	if strings.Contains(err.Error(), `appending ",role=`) {
		t.Errorf("role locator told to narrow with role=: %v", err)
	}

	// Missing.
	loc = uitree.Locator{Kind: uitree.KindText, Value: "Nowhere", Exact: true}
	if _, err := pickOne(loc, tree); err == nil {
		t.Error("missing locator resolved")
	}
}

// A role is offered to narrow an ambiguous locator only where the matches
// differ by role, and named. The launcher shows "Gmail" twice, the app icon
// and the predicted-app slot, both buttons: "append ,role=button" was
// offered there and left text=Gmail,role=button matching both. Where one
// match is shown and another iOS reports hidden — Pocket Casts' onboarding
// sheet over Discover, on an iPad — that is the difference, and it is said.
// CHALLENGES 242.
func TestPickOneSuggestsRoleOnlyWhereItNarrows(t *testing.T) {
	loc := uitree.Locator{Kind: uitree.KindText, Value: "Gmail"}
	_, err := pickOne(loc, loadLauncher(t))
	if err == nil {
		t.Fatal("precondition: text=Gmail should be ambiguous on the launcher")
	}
	if strings.Contains(err.Error(), `,role=`) || !strings.Contains(err.Error(), "use a ref from app_map") {
		t.Errorf("two buttons were told to narrow by role: %v", err)
	}

	const ios = `<?xml version="1.0" encoding="UTF-8"?><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="App" x="0" y="0" width="402" height="874" visible="true" enabled="true">` +
		`<XCUIElementTypeButton type="XCUIElementTypeButton" name="More" label="More" x="10" y="100" width="100" height="44" visible="true" enabled="true" accessible="true"/>` +
		`<XCUIElementTypeLink type="XCUIElementTypeLink" name="More" label="More" x="10" y="200" width="100" height="44" visible="true" enabled="true" accessible="true"/>` +
		`<XCUIElementTypeButton type="XCUIElementTypeButton" name="Technology" label="Technology" x="682" y="135" width="127" height="37" visible="false" enabled="true" accessible="true"/>` +
		`<XCUIElementTypeButton type="XCUIElementTypeButton" name="Technology" label="Technology" x="347" y="522" width="137" height="24" visible="true" enabled="true" accessible="true"/>` +
		`</XCUIElementTypeApplication>`
	tree, err := uitree.ParseIOS([]byte(ios))
	if err != nil {
		t.Fatal(err)
	}
	loc, _ = uitree.ParseLocator("label=More")
	if _, err := pickOne(loc, tree); err == nil || !strings.Contains(err.Error(), `",role=" and one of button, link`) {
		t.Errorf("a button and a link were not told to narrow by one of their roles: %v", err)
	}
	loc, _ = uitree.ParseLocator("label=Technology")
	_, err = pickOne(loc, tree)
	if err == nil || strings.Contains(err.Error(), `,role=`) || !strings.Contains(err.Error(), "1 of them iOS reports hidden") {
		t.Errorf("a shown and a hidden button were not told apart: %v", err)
	}
}

func TestIntArgAcceptsJSONAndNativeNumbers(t *testing.T) {
	// x/y arrive as float64 over JSON-RPC and as int from the CLI.
	if v, ok := intArg(map[string]interface{}{"x": float64(540)}, "x"); !ok || v != 540 {
		t.Errorf("float64 arg = %d, %v", v, ok)
	}
	if v, ok := intArg(map[string]interface{}{"x": 540}, "x"); !ok || v != 540 {
		t.Errorf("int arg = %d, %v", v, ok)
	}
	if _, ok := intArg(map[string]interface{}{"x": "540"}, "x"); ok {
		t.Error("a string was accepted as a coordinate")
	}
	if _, ok := intArg(map[string]interface{}{}, "x"); ok {
		t.Error("a missing arg reported present")
	}
}

func TestStringArgTrims(t *testing.T) {
	if got := stringArg(map[string]interface{}{"t": "  @e1 "}, "t"); got != "@e1" {
		t.Errorf("stringArg = %q", got)
	}
	if got := stringArg(map[string]interface{}{"t": 5}, "t"); got != "" {
		t.Errorf("non-string arg = %q", got)
	}
}

func TestCallTimeoutExceedsEveryReadinessWait(t *testing.T) {
	// A tool call must be able to outlast the slowest thing it can trigger.
	// When callTimeout equalled the WebDriverAgent readiness wait, the first
	// call after a daemon restart failed with "context deadline exceeded"
	// having done nothing wrong: the inner wait consumed the whole budget.
	//
	// The readiness values live in internal/mobiumdriver and are unexported, so
	// they are restated here; the margin is what this guards.
	readiness := map[string]time.Duration{
		"uiautomator2 server": 45 * time.Second,
		"webdriveragent":      90 * time.Second,
	}
	for name, wait := range readiness {
		if callTimeout <= wait {
			t.Errorf("callTimeout (%s) does not exceed the %s readiness wait (%s)",
				callTimeout, name, wait)
		}
		// Readiness is only one step of a cold start; leave room for the
		// install before it and the work after.
		if callTimeout < wait*2 {
			t.Errorf("callTimeout (%s) leaves too little margin over the %s wait (%s)",
				callTimeout, name, wait)
		}
	}
}

// An off-screen table row on a real iPhone: the cell keeps its frame, marked
// not visible, and the button inside it is reported at 0,0 with no size. The
// shape is WebDriverAgent's on an iPhone 15 Plus, iOS 26.6.2, Settings >
// General, trimmed to the rows that matter.
const offscreenRowIOS = `<?xml version="1.0" encoding="UTF-8"?>
<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Settings" label="Settings" enabled="true" visible="true" accessible="false" x="0" y="0" width="430" height="932" index="0">
  <XCUIElementTypeWindow type="XCUIElementTypeWindow" enabled="true" visible="true" accessible="false" x="0" y="0" width="430" height="932" index="0">
    <XCUIElementTypeTable type="XCUIElementTypeTable" enabled="true" visible="true" accessible="false" x="0" y="0" width="430" height="932" index="0">
      <XCUIElementTypeCell type="XCUIElementTypeCell" name="About" label="About" enabled="true" visible="true" accessible="true" x="20" y="366" width="390" height="54" index="0">
        <XCUIElementTypeButton type="XCUIElementTypeButton" name="About" label="About" enabled="true" visible="true" accessible="false" x="38" y="379" width="88" height="28" index="0"/>
      </XCUIElementTypeCell>
      <XCUIElementTypeCell type="XCUIElementTypeCell" name="Legal &amp; Regulatory" label="Legal &amp; Regulatory" enabled="true" visible="false" accessible="true" x="20" y="1530" width="390" height="54" index="1">
        <XCUIElementTypeButton type="XCUIElementTypeButton" name="Legal &amp; Regulatory" label="Legal &amp; Regulatory" enabled="true" visible="false" accessible="false" x="0" y="0" width="0" height="0" index="0"/>
      </XCUIElementTypeCell>
    </XCUIElementTypeTable>
  </XCUIElementTypeWindow>
</XCUIElementTypeApplication>`

func TestPickOneTreatsNoBoundsAsOffScreen(t *testing.T) {
	tree, err := uitree.ParseIOS([]byte(offscreenRowIOS))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Positive control: the on-screen row's button resolves.
	on, err := uitree.ParseLocator("label=About,role=button")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pickOne(on, tree); err != nil {
		t.Fatalf("on-screen button did not resolve: %v", err)
	}

	// The off-screen one must read as "not here yet", which is what makes
	// scroll-to and the implicit scroll swipe for it. As a plain error it
	// stopped scroll-to before its first swipe.
	off, err := uitree.ParseLocator("label=Legal & Regulatory,role=button")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pickOne(off, tree)
	if err == nil {
		t.Fatal("a button with no bounds resolved as if it were on screen")
	}
	if !matchedNothing(err) {
		t.Errorf("no bounds is not treated as off screen, so nothing will scroll for it: %v", err)
	}
	if strings.Contains(err.Error(), "run app_map again") {
		t.Errorf("suggests re-mapping, which cannot bring an off-screen row into view: %v", err)
	}
}
