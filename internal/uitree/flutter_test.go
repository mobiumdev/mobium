package uitree

import (
	"strings"
	"testing"
)

// Flutter's text fields, as UiAutomator2 reported them on the Pixel 7 AVD:
// an empty field carries its label in hint with no text and showing-hint
// false, where a native field puts the hint in the text. Named by the hint,
// not the class. CHALLENGES 206.
func TestAFlutterFieldIsNamedByItsHint(t *testing.T) {
	tree, err := ParseAndroid([]byte(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" package="dev.mobium.mobium_flutter" class="android.view.View" bounds="[0,0][1080,2400]">` +
		`<node index="0" package="dev.mobium.mobium_flutter" class="android.widget.EditText" text="" hint="Username" ` +
		`showing-hint="false" clickable="true" focusable="true" enabled="true" displayed="true" bounds="[42,325][1038,472]" />` +
		`</node></hierarchy>`))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, e := range tree.Map() {
		lines = append(lines, e.Label+" ("+e.Role+")")
	}
	if got := strings.Join(lines, "; "); !strings.Contains(got, "Username (input)") {
		t.Errorf("map = %q, want the field named Username", got)
	}
}

// Flutter's Semantics(identifier:) on iOS: an Other with the identifier, then
// the button it wraps in the same frame. The button is the control the
// identifier names, not a cover over it; a real cover is still one.
func TestAFlutterIdentifierIsNotCoveredByItsOwnButton(t *testing.T) {
	tree, err := ParseIOS([]byte(`<?xml version="1.0" encoding="UTF-8"?><AppiumAUT>` +
		`<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Mobium Flutter" enabled="true" visible="true" accessible="false" x="0" y="0" width="402" height="874">` +
		`<XCUIElementTypeOther type="XCUIElementTypeOther" enabled="true" visible="false" accessible="false" x="0" y="0" width="402" height="874">` +
		`<XCUIElementTypeOther type="XCUIElementTypeOther" name="signIn" enabled="true" visible="true" accessible="false" x="16" y="254" width="370" height="48"/>` +
		`<XCUIElementTypeButton type="XCUIElementTypeButton" name="Sign In" label="Sign In" enabled="true" visible="true" accessible="true" x="16" y="254" width="370" height="48"/>` +
		`</XCUIElementTypeOther>` +
		`<XCUIElementTypeButton type="XCUIElementTypeButton" name="Toast" label="Toast" enabled="true" visible="true" accessible="true" x="0" y="240" width="402" height="80"/>` +
		`</XCUIElementTypeApplication></AppiumAUT>`))
	if err != nil {
		t.Fatal(err)
	}
	var target *Node
	for _, n := range tree.All() {
		if n.TestID == "signIn" {
			target = n
		}
	}
	if target == nil {
		t.Fatal("no signIn")
	}
	x, y := target.Bounds.Center()
	var names []string
	for _, n := range tree.DrawnOver(target, x, y) {
		names = append(names, n.Label)
	}
	if got := strings.Join(names, ","); got != "Toast" {
		t.Errorf("drawn over signIn: %q, want only the toast — the Sign In button is the control the identifier names", got)
	}
}

// The name map prints for a Flutter field, its hint, is a name label= takes.
func TestAFlutterFieldIsFoundByItsHintAsALabel(t *testing.T) {
	tree, err := ParseAndroid([]byte(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" package="p" class="android.view.View" bounds="[0,0][1080,2400]">` +
		`<node index="0" package="p" class="android.widget.EditText" text="" hint="Username" showing-hint="false" ` +
		`clickable="true" focusable="true" enabled="true" displayed="true" bounds="[42,325][1038,472]" />` +
		`<node index="1" package="p" class="android.widget.EditText" text="lana" hint="Nickname" showing-hint="false" ` +
		`clickable="true" focusable="true" enabled="true" displayed="true" bounds="[42,480][1038,600]" />` +
		`</node></hierarchy>`))
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := ParseLocator("label=Username")
	if got := loc.Resolve(tree); len(got) != 1 || got[0].Hint != "Username" {
		t.Errorf("label=Username resolved to %d nodes", len(got))
	}
	// A field with text is named by its text, so its hint is not its label.
	loc, _ = ParseLocator("label=Nickname")
	if got := loc.Resolve(tree); len(got) != 0 {
		t.Error("label= found a field by the hint of a field that holds text")
	}
}
