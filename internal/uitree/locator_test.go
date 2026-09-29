package uitree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLocator(t *testing.T) {
	tests := []struct {
		in      string
		want    Locator
		wantErr bool
	}{
		{"text=Sign In", Locator{Kind: KindText, Value: "Sign In"}, false},
		{"testid=submit", Locator{Kind: KindTestID, Value: "submit"}, false},
		{"role=button", Locator{Kind: KindRole, Value: "button"}, false},
		{"label=Continue with Google", Locator{Kind: KindLabel, Value: "Continue with Google"}, false},
		{"path=0/1/3/0", Locator{Kind: KindPath, Value: "0/1/3/0"}, false},
		// A bare string is the common case an agent types.
		{"Sign In", Locator{Kind: KindText, Value: "Sign In"}, false},
		// Role qualifier.
		{"text=Continue,role=button", Locator{Kind: KindText, Value: "Continue", Role: "button"}, false},
		// A value containing a comma must survive.
		{"text=Yes, continue", Locator{Kind: KindText, Value: "Yes, continue"}, false},
		{"nonsense=x", Locator{}, true},
	}
	for _, tc := range tests {
		got, err := ParseLocator(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseLocator(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("ParseLocator(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestLocatorStringRoundTrips(t *testing.T) {
	for _, l := range []Locator{
		{Kind: KindText, Value: "Sign In"},
		{Kind: KindTestID, Value: "com.example.shop:id/submit"},
		{Kind: KindText, Value: "Continue", Role: "button"},
	} {
		got, err := ParseLocator(l.String())
		if err != nil {
			t.Fatalf("ParseLocator(%q): %v", l.String(), err)
		}
		if got.Kind != l.Kind || got.Value != l.Value || got.Role != l.Role {
			t.Errorf("round trip of %+v gave %+v", l, got)
		}
	}
}

func TestResolveByKind(t *testing.T) {
	tree := loadFixture(t, "login.xml")

	tests := []struct {
		name string
		loc  Locator
		want int
	}{
		{"testid full", Locator{Kind: KindTestID, Value: "com.example.shop:id/submit", Exact: true}, 1},
		// submit_wrapper matches too by substring, and the exact ID wins.
		{"testid short form", Locator{Kind: KindTestID, Value: "submit"}, 1},
		{"testid substring", Locator{Kind: KindTestID, Value: "subm"}, 2}, // submit + submit_wrapper
		{"label exact", Locator{Kind: KindLabel, Value: "Continue with Google", Exact: true}, 1},
		{"text substring", Locator{Kind: KindText, Value: "sign"}, 2}, // "Sign in" title + "Sign In" button
		{"text exact", Locator{Kind: KindText, Value: "Sign In", Exact: true}, 1},
		{"ambiguous text", Locator{Kind: KindText, Value: "Continue", Exact: true}, 2},
		{"role input", Locator{Kind: KindRole, Value: "input"}, 2},
		{"role checkbox", Locator{Kind: KindRole, Value: "checkbox"}, 1},
		{"class", Locator{Kind: KindClass, Value: "RecyclerView"}, 1},
		{"path", Locator{Kind: KindPath, Value: "0/1/3/0", Exact: true}, 1},
		{"no match", Locator{Kind: KindText, Value: "Log out", Exact: true}, 0},
		// An empty value must never match everything.
		{"empty value", Locator{Kind: KindText, Value: ""}, 0},
	}
	for _, tc := range tests {
		if got := len(tc.loc.Resolve(tree)); got != tc.want {
			t.Errorf("%s: resolved %d nodes, want %d", tc.name, got, tc.want)
		}
	}
}

func TestRoleQualifierNarrows(t *testing.T) {
	tree := loadFixture(t, "login.xml")
	// "Sign In"/"Sign in" is ambiguous as a substring across a title and a
	// button; the role qualifier is what makes it resolvable.
	broad := Locator{Kind: KindText, Value: "sign"}
	if got := len(broad.Resolve(tree)); got != 2 {
		t.Fatalf("precondition: %d matches, want 2", got)
	}
	narrow := Locator{Kind: KindText, Value: "sign", Role: "button"}
	if got := len(narrow.Resolve(tree)); got != 1 {
		t.Errorf("role-qualified locator matched %d nodes, want 1", got)
	}
}

func TestHasRole(t *testing.T) {
	tree := loadFixture(t, "login.xml")
	byID := map[string]*Node{}
	for _, n := range tree.All() {
		if n.TestID != "" {
			byID[n.ShortTestID()] = n
		}
	}

	if !HasRole(byID["submit"], "button") {
		t.Error("AppCompatButton should satisfy role=button")
	}
	if !HasRole(byID["email"], "input") {
		t.Error("EditText should satisfy role=input")
	}
	if HasRole(byID["email"], "button") {
		t.Error("a clickable EditText must not be treated as a button")
	}
	if !HasRole(byID["remember"], "checkbox") {
		t.Error("CheckBox should satisfy role=checkbox")
	}
	if !HasRole(byID["providers"], "list") {
		t.Error("RecyclerView should satisfy role=list")
	}
	// A clickable ViewGroup with no button class is still a button to a user.
	if !HasRole(byID["submit_wrapper"], "button") {
		t.Error("clickable container should satisfy role=button")
	}
}

func TestDerivePrefersDurableLocators(t *testing.T) {
	tree := loadFixture(t, "login.xml")
	byNode := map[string]*Node{}
	for _, n := range tree.All() {
		if n.TestID != "" {
			byNode[n.ShortTestID()] = n
		}
	}

	// A unique resource-id wins outright.
	if got := Derive(byNode["submit"], tree); got.Kind != KindTestID || got.Value != "com.example.shop:id/submit" {
		t.Errorf("submit derived %+v, want a testid locator", got)
	}

	// No resource-id, but a unique accessibility label.
	var google *Node
	for _, n := range tree.All() {
		if n.Label == "Continue with Google" {
			google = n
		}
	}
	got := Derive(google, tree)
	if got.Kind != KindLabel || got.Value != "Continue with Google" {
		t.Errorf("google button derived %+v, want a label locator", got)
	}

	// Neither id nor label: falls through to unique text.
	var forgot *Node
	for _, n := range tree.All() {
		if n.Text == "Forgot password?" {
			forgot = n
		}
	}
	if got := Derive(forgot, tree); got.Kind != KindText || got.Value != "Forgot password?" {
		t.Errorf("forgot link derived %+v, want a text locator", got)
	}
}

func TestDerivedLocatorsAreUnique(t *testing.T) {
	tree := loadFixture(t, "login.xml")
	// Every locator map hands out must resolve to exactly the node it names,
	// or `tap @eN` touches the wrong thing.
	for _, e := range tree.Map() {
		matches := e.Locator.Resolve(tree)
		if len(matches) != 1 {
			t.Errorf("%s (%s) derived %s resolving to %d nodes",
				e.Ref, e.Label, e.Locator, len(matches))
			continue
		}
		if matches[0] != e.Node {
			t.Errorf("%s derived %s resolving to a different node (%q)",
				e.Ref, e.Locator, matches[0].Text)
		}
	}
}

// A hand-written testid resolves exact first. The iOS login screen is the
// case that forced it: a label's ID is its own text, so substring matching
// found the field, its heading and a hint that mentions it.
func TestTestIDResolvesExactFirst(t *testing.T) {
	tree, err := ParseIOS([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="App" x="0" y="0" width="400" height="800">
  <XCUIElementTypeStaticText type="XCUIElementTypeStaticText" value="Password" name="Password" label="Password" visible="true" x="0" y="0" width="300" height="20"/>
  <XCUIElementTypeStaticText type="XCUIElementTypeStaticText" value="Password: hunter2" name="Password: hunter2" label="Password: hunter2" visible="true" x="0" y="20" width="300" height="20"/>
  <XCUIElementTypeSecureTextField type="XCUIElementTypeSecureTextField" value="password" name="password" label="password" placeholderValue="password" visible="true" accessible="true" x="0" y="50" width="300" height="40"/>
  <XCUIElementTypeStaticText type="XCUIElementTypeStaticText" value="Too short" name="passwordError" label="Too short" visible="true" x="0" y="90" width="300" height="20"/>
</XCUIElementTypeApplication>`))
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := ParseLocator("testid=password")
	got := loc.Resolve(tree)
	if len(got) != 1 || got[0].Class != "XCUIElementTypeSecureTextField" {
		t.Fatalf("testid=password resolved to %d nodes, want the one field", len(got))
	}
	// No exact match: substring, as before — and ambiguous, as before.
	loc, _ = ParseLocator("testid=pass")
	if n := len(loc.Resolve(tree)); n != 4 {
		t.Errorf("testid=pass resolved to %d nodes, want every substring match (4)", n)
	}
	// Case matters for an ID: "Password" is the heading's, not the field's.
	loc, _ = ParseLocator("testid=Password")
	if got := loc.Resolve(tree); len(got) != 1 || got[0].Class != "XCUIElementTypeStaticText" {
		t.Errorf("testid=Password resolved to %d nodes, want the heading", len(got))
	}
}

// The role map prints is a role a locator can find the node by — or, for a
// row around a real button, the button inside it. On iOS it was not: every
// Cell, keyboard key and React Native view printed as (button) and matched
// no role=button — 93 entries across these hierarchies, and one Android
// GridView printed as a list. CHALLENGES 136.
func TestTheRoleMapPrintsIsOneALocatorFinds(t *testing.T) {
	files, err := filepath.Glob("testdata/*.xml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no captured hierarchies: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		parse := ParseAndroid
		if strings.Contains(string(data), "XCUIElementType") {
			parse = ParseIOS
		}
		tree, err := parse(data)
		if err != nil {
			continue // a fragment kept for another test
		}
		for _, e := range tree.Map() {
			if e.Role != "" && !HasRole(e.Node, e.Role) && !hasNamedDescendant(e.Node, e.Role) {
				t.Errorf("%s: %s prints as (%s), and role=%s does not find it", filepath.Base(f), e.Label, e.Role, e.Role)
			}
		}
	}
	// And the row that exposed it gets a locator a person could write.
	for _, e := range loadIOS(t, "ios26-share-sheet-expanded.xml").Map() {
		if e.Label == "Add to Home Screen" && e.Locator.String() != "label=Add to Home Screen,role=button" {
			t.Errorf("Add to Home Screen's locator is %s", e.Locator)
		}
	}
}

// text= finds the same control on both platforms. On iOS a node's text is its
// value, and a React Native button has only a label, so text=Dialog Demo
// found nothing on MobiumApp's home screen while Android, where the label is
// a child TextView's text, found the button. An iOS node with no text of its
// own matches by its label — unless something inside it matches by its own
// text, which is what keeps Settings' General row, a labeled button around
// a text node reading "General", to one match. CHALLENGES 166.
func TestTextFindsALabelOnlyControlOnIOS(t *testing.T) {
	home := loadIOS(t, "ios26-mobiumapp-home.xml")
	got := Locator{Kind: KindText, Value: "Dialog Demo"}.Resolve(home)
	if len(got) != 1 || got[0].TestID != "dialogsBtn" {
		t.Errorf("text=Dialog Demo on MobiumApp's home: %d matches, want the dialogsBtn button", len(got))
	}
	settings := loadIOS(t, "ios26-settings-root.xml")
	got = Locator{Kind: KindText, Value: "General", Exact: true}.Resolve(settings)
	if len(got) != 1 || got[0].Class != "XCUIElementTypeStaticText" {
		t.Errorf("text=General in Settings: %d matches, want the one text node inside the row", len(got))
	}
	// React Native nests a Text in a Text, and iOS reports both at the same
	// bounds: text=Back found two on the Dialog Demo, one thing drawn once.
	dialogs := loadIOS(t, "ios26-mobiumapp-dialogs.xml")
	if n := len(Locator{Kind: KindText, Value: "Back", Exact: true}.Resolve(dialogs)); n != 1 {
		t.Errorf("text=Back on the Dialog Demo: %d matches, want 1", n)
	}
	// Android is untouched: its label is content-desc, which text= has never
	// matched there, and an Android node is never a label-only fallback.
	login := loadFixture(t, "login.xml")
	if n := len(Locator{Kind: KindText, Value: "Continue with Google", Exact: true}.Resolve(login)); n != 0 {
		t.Errorf("text= matched an Android content-desc: %d nodes", n)
	}
}
