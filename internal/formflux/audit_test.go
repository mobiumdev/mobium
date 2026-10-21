package formflux

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// The audit's fixtures are the same real captures inspect's are: whether a
// rule fires on a screen somebody shipped, and how much noise it makes. Every
// rule has a capture it fires on, so a zero elsewhere is an answer.

func audit(t *testing.T, name string) []AuditFinding {
	t.Helper()
	tr := load(t, name)
	return AuditAndroid(tr, screenOf(tr), 420)
}

func rules(fs []AuditFinding) map[AuditRule][]AuditFinding {
	out := map[AuditRule][]AuditFinding{}
	for _, f := range fs {
		out[f.Rule] = append(out[f.Rule], f)
	}
	return out
}

func TestAuditIsQuietOnScreensThatAreFine(t *testing.T) {
	for _, name := range []string{"fdroid-list-uia2.xml", "seal-look-uia2.xml", "android-alert-two-uia2.xml", "compose-dropdown-menu-uia2.xml"} {
		if fs := audit(t, name); len(fs) != 0 {
			t.Errorf("%s: %d findings on a screen with none: %v", name, len(fs), fs)
		}
	}
}

// Aegis's master-password screen: its Previous and Next are icon buttons with
// no description, which TalkBack can only call "button" — a resource id is
// not spoken. And the typed password is in the capture, and never in a
// finding.
func TestAuditNamesAegisIconButtonsAndNeverItsPassword(t *testing.T) {
	fs := audit(t, "aegis-password-uia2.xml")
	got := rules(fs)[RuleSpeakableTextPresent]
	var ids []string
	for _, f := range got {
		ids = append(ids, f.Locator())
	}
	if strings.Join(ids, ",") != "testid=btnPrevious,testid=btnNext" {
		t.Errorf("nameless controls %v, want Aegis's Previous and Next", ids)
	}
	for _, f := range fs {
		if s := fmt.Sprint(f.Element(), f.Detail, f.Summary, f.Locator()); strings.Contains(s, "PASSWORD-MUST-NOT-APPEAR") {
			t.Fatalf("a finding carries the password: %s", s)
		}
	}
}

// MobiumApp's Table Demo: every row has its own More, Delete and Starred,
// which is one thing to fix — so one finding each, with the count, and the
// rows' small targets collapsed the same way. Row by row it was 46 findings.
func TestAuditGroupsATablesRepeatedControls(t *testing.T) {
	got := rules(audit(t, "mobiumapp-table-uia2.xml"))
	dup := got[RuleDuplicateSpeakableText]
	if len(dup) != 3 {
		t.Fatalf("%d duplicate findings, want one each for More, Delete and Starred: %v", len(dup), dup)
	}
	if !strings.Contains(dup[0].Detail, `controls on this screen say "More"`) {
		t.Errorf("first duplicate %q, want a count of More", dup[0].Detail)
	}
	small := got[RuleTouchTargetSize]
	if len(small) != 3 || !strings.Contains(small[0].Detail, "controls like it on this screen") {
		t.Errorf("touch targets %v, want three, each with its count", small)
	}
}

// The launcher's At a Glance is a named control around a nameless one on the
// same rectangle: one finding, two controls on one rectangle — not a nameless
// control, since the rectangle's name is spoken.
func TestAuditReportsANestedPairOnce(t *testing.T) {
	got := rules(audit(t, "launcher-uia2.xml"))
	if len(got[RuleSpeakableTextPresent]) != 0 {
		t.Errorf("a nameless card inside a named one on its rectangle was reported nameless: %v", got[RuleSpeakableTextPresent])
	}
	if len(got[RuleDuplicateClickableBounds]) != 1 {
		t.Errorf("%d duplicate-bounds findings, want the At a Glance pair once", len(got[RuleDuplicateClickableBounds]))
	}
}

func button(class, desc, text, id string) string {
	return fmt.Sprintf(`<node class="%s" content-desc="%s" text="%s" resource-id="%s" clickable="true" enabled="true" bounds="[0,%%d][1080,%%d]" />`,
		class, desc, text, id)
}

// A description that ends with the control's own kind is said twice; one
// that names a kind of something else, or another kind, is a name.
// MobiumApp's Dialog Demo is the negative control: "One-button alert" and
// "Prompt with a text field" are buttons' names, and a first version of the
// rule flagged both.
func TestAuditRedundantDescriptionIsTheControlsOwnKindAtTheEnd(t *testing.T) {
	cases := []struct {
		class, desc string
		want        bool
	}{
		{"android.widget.Button", "Save button", true},
		{"android.widget.Button", "Save Button.", true},
		{"android.widget.CheckBox", "Remember me checkbox", true},
		{"android.widget.Switch", "Wi-Fi toggle", true},
		{"android.widget.Button", "One-button alert", false},
		{"android.widget.Button", "Three-button alert", false},
		{"android.widget.Button", "Prompt with a text field", false},
		{"android.widget.Button", "Turn on switch", false},
		{"android.widget.CheckBox", "Button colors", false},
		{"android.view.ViewGroup", "Save button", false},
	}
	for _, c := range cases {
		xml := `<hierarchy><node class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
			fmt.Sprintf(button(c.class, c.desc, "", "x"), 100, 300) + `</node></hierarchy>`
		tr, err := uitree.ParseAndroid([]byte(xml))
		if err != nil {
			t.Fatal(err)
		}
		got := len(rules(AuditAndroid(tr, uitree.Rect{X2: 1080, Y2: 2400}, 420))[RuleRedundantDescription]) == 1
		if got != c.want {
			t.Errorf("%s %q: redundant %v, want %v", c.class, c.desc, got, c.want)
		}
	}
}

func TestAuditEditableContentDescAndTheSpokenHint(t *testing.T) {
	xml := `<hierarchy><node class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		fmt.Sprintf(button("android.widget.EditText", "Email address", "", "described"), 100, 300) +
		`<node class="android.widget.EditText" content-desc="" text="" hint="Email address" resource-id="hinted" clickable="true" enabled="true" bounds="[0,400][1080,600]" />` +
		`</node></hierarchy>`
	tr, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	got := rules(AuditAndroid(tr, uitree.Rect{X2: 1080, Y2: 2400}, 420))
	if e := got[RuleEditableContentDesc]; len(e) != 1 || e[0].Locator() != "testid=described" {
		t.Errorf("editable with a description: %v, want the described field alone", e)
	}
	// A hint is spoken, so the hinted field has a name: no nameless finding.
	if n := got[RuleSpeakableTextPresent]; len(n) != 0 {
		t.Errorf("a field with a hint was reported nameless: %v", n)
	}
}

// With no density there is no dp: the touch target rule is left out, never
// judged against a guess.
func TestAuditLeavesTouchTargetsOutWithNoDensity(t *testing.T) {
	tr := load(t, "mobiumapp-table-uia2.xml")
	if got := rules(AuditAndroid(tr, screenOf(tr), 0))[RuleTouchTargetSize]; len(got) != 0 {
		t.Errorf("judged %d touch targets with no density", len(got))
	}
}

// A row the list holds below the screen is no finding about this screen.
func TestAuditSkipsWhatIsOffScreen(t *testing.T) {
	xml := `<hierarchy><node class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		fmt.Sprintf(button("android.widget.Button", "", "", "below"), 2500, 2520) + `</node></hierarchy>`
	tr, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if fs := AuditAndroid(tr, uitree.Rect{X2: 1080, Y2: 2400}, 420); len(fs) != 0 {
		t.Errorf("findings about a control below the screen: %v", fs)
	}
}
