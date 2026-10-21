package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

func spokenTree(t *testing.T, nodes string) *uitree.Tree {
	t.Helper()
	tr, err := uitree.ParseAndroid([]byte(`<hierarchy><node class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">` +
		nodes + `</node></hierarchy>`))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func textNode(class, desc, text string, y int, inner string) string {
	return fmt.Sprintf(`<node class="%s" content-desc="%s" text="%s" clickable="true" enabled="true" bounds="[0,%d][1080,%d]">%s</node>`,
		class, desc, text, y, y+100, inner)
}

// VoiceOver says the element's name and then its kind and state, so the
// element is the one whose name the sentence starts with — the longest: the
// Form Demo button, not the Form heading.
func TestSpokenNodeIsTheLongestNameTheSentenceStartsWith(t *testing.T) {
	tr := spokenTree(t, textNode("android.widget.TextView", "", "Form", 100, "")+
		textNode("android.widget.Button", "Form Demo", "", 300, ""))
	if n := spokenNode(tr, "Form Demo Button"); n == nil || n.Label != "Form Demo" {
		t.Errorf("Form Demo Button named %v", n)
	}
	if n := spokenNode(tr, "Form, Heading"); n == nil || n.Text != "Form" {
		t.Errorf("Form, Heading named %v", n)
	}
	// Whole words: "Format" is not "Form".
	if n := spokenNode(tr, "Format Button"); n != nil {
		t.Errorf("Format Button named %v", n)
	}
}

// Look-alikes name nobody: a guess would put a sentence beside the wrong row.
func TestSpokenNodeNamesNoneOfSeveral(t *testing.T) {
	tr := spokenTree(t, textNode("android.widget.Button", "More", "", 100, "")+
		textNode("android.widget.Button", "More", "", 300, ""))
	if n := spokenNode(tr, "More Button"); n != nil {
		t.Errorf("one of two More buttons was named: %v", n)
	}
}

// VoiceOver drops a label's own trailing kind: "Save button" and "Save" both
// say "Save Button", so neither is named — the first version named the
// redundant one for both (the iPhone, MobiumApp's Screen Reader Demo).
func TestSpokenNodeNamesNeitherOfTwoNamesSpokenAlike(t *testing.T) {
	tr := spokenTree(t, textNode("android.widget.Button", "Save button", "", 100, "")+
		textNode("android.widget.Button", "Save", "", 300, ""))
	if n := spokenNode(tr, "Save Button"); n != nil {
		t.Errorf("Save Button named %q, one of two that say it", n.Label)
	}
}

// A back button holds its own title: the sentence is the button's.
func TestSpokenNodeTakesTheControlOfANest(t *testing.T) {
	tr := spokenTree(t, textNode("android.widget.Button", "MobiumApp", "", 100,
		`<node class="android.widget.TextView" content-desc="" text="MobiumApp" clickable="false" enabled="true" bounds="[10,110][500,190]" />`))
	n := spokenNode(tr, "MobiumApp Back Button")
	if n == nil || n.Class != "android.widget.Button" {
		t.Errorf("MobiumApp Back Button named %v, want the back button", n)
	}
}

// One name on a view and on what it wraps is one element: the innermost.
func TestSpokenNodeTakesTheInnermostOfANest(t *testing.T) {
	tr := spokenTree(t, textNode("android.view.ViewGroup", "Save", "", 100,
		textNode("android.widget.Button", "Save", "", 110, "")))
	n := spokenNode(tr, "Save Button")
	if n == nil || n.Class != "android.widget.Button" {
		t.Errorf("Save Button named %v, want the inner button", n)
	}
}

// A password field's text is never a name to match, and what is said about
// it is never shown.
func TestSpeechKeepsAPasswordOut(t *testing.T) {
	tr := spokenTree(t, `<node class="android.widget.EditText" content-desc="" text="hunter22" password="true" resource-id="pw" clickable="true" enabled="true" bounds="[0,100][1080,200]" />`)
	if n := spokenNode(tr, "hunter22 Edit box"); n != nil {
		t.Errorf("a sentence was matched to a password field by its contents")
	}
	var pw *uitree.Node
	tr.Walk(func(n *uitree.Node) bool {
		if n.Password {
			pw = n
		}
		return pw == nil
	})
	st := spokenStep("hunter22. Edit box", pw, tr)
	if strings.Contains(st.Said, "hunter22") || st.Said != passwordSaid {
		t.Errorf("said %q about a password field", st.Said)
	}
	if st.Element == nil || strings.Contains(st.Element.Label, "hunter22") {
		t.Errorf("the element %+v shows the password", st.Element)
	}
	// Unpaired, VoiceOver's own word for the field keeps it out.
	if st := spokenStep("password Secure text field Is editing ••••••••", nil, tr); st.Said != passwordSaid {
		t.Errorf("said %q about a secure text field nothing was paired with", st.Said)
	}
}

func TestSpeechTextSaysWhyAWalkEnded(t *testing.T) {
	v := &SpeechView{ScreenReader: "TalkBack", Ended: "wrapped", Steps: []SpeechStep{
		{Said: "Form", Element: &SpokenElement{Label: "Form"}},
		{Said: "", Element: &SpokenElement{Label: "Pro", Role: "radio", InMap: true}},
	}}
	got := v.text()
	for _, want := range []string{`1. "Form" — Form, not in map`, "2. (nothing) — Pro (radio)", "came back round"} {
		if !strings.Contains(got, want) {
			t.Errorf("text %q lacks %q", got, want)
		}
	}
}

// The application is named for the app, and the cursor is never on it: the
// back button VoiceOver calls "MobiumApp Back Button" is labeled Back, and
// the first version paired the sentence with the whole screen.
func TestSpokenNodeIsNeverTheApplication(t *testing.T) {
	tr, err := uitree.ParseIOS([]byte(`<?xml version="1.0" encoding="UTF-8"?><AppiumAUT>` +
		`<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="MobiumApp" label="MobiumApp" enabled="true" visible="true" accessible="false" x="0" y="0" width="430" height="932">` +
		`<XCUIElementTypeButton type="XCUIElementTypeButton" name="Back" label="Back" enabled="true" visible="true" accessible="true" x="0" y="60" width="120" height="44"/>` +
		`</XCUIElementTypeApplication></AppiumAUT>`))
	if err != nil {
		t.Fatal(err)
	}
	if n := spokenNode(tr, "MobiumApp Back Button"); n != nil {
		t.Errorf("MobiumApp Back Button named %v", n)
	}
}

// A read of the tree can lag TalkBack's cursor: the node is taken only when
// the sentence holds its name.
func TestSaysName(t *testing.T) {
	tr := spokenTree(t, textNode("android.widget.CheckBox", "", "Email me", 100, "")+
		textNode("android.widget.TextView", "", "Log in", 300, "")+
		textNode("android.widget.ImageButton", "", "", 500, ""))
	byText := map[string]*uitree.Node{}
	tr.Walk(func(n *uitree.Node) bool { byText[n.Text] = n; return true })
	cases := []struct {
		said, node string
		want       bool
	}{
		{"not checked. Email me. Check box", "Email me", true},
		{"Back. Button", "Log in", false},
		{"Log in, Heading", "Log in", true},
		{"Button", "", true},
	}
	for _, c := range cases {
		n := byText[c.node]
		if c.node == "" {
			n = nil
			tr.Walk(func(m *uitree.Node) bool {
				if m.Class == "android.widget.ImageButton" {
					n = m
				}
				return n == nil
			})
		}
		if got := saysName(c.said, n); got != c.want {
			t.Errorf("%q about %q: %v, want %v", c.said, c.node, got, c.want)
		}
	}
}

// Listening names what made the screen reader speak, from TalkBack's event.
func TestListenCauses(t *testing.T) {
	for event, want := range map[string]string{
		"TYPE_VIEW_ACCESSIBILITY_FOCUSED": "focus",
		"TYPE_ANNOUNCEMENT":               "announcement",
		"TYPE_WINDOW_CONTENT_CHANGED":     "change",
		"TYPE_VIEW_CLICKED":               "view_clicked",
		"":                                "",
	} {
		if got := cause(event); got != want {
			t.Errorf("cause(%q) = %q, want %q", event, got, want)
		}
	}
}

func TestListenText(t *testing.T) {
	quiet := (&SpeechView{ScreenReader: "TalkBack", Ended: "listened"}).text()
	if quiet != "TalkBack said nothing since listening started" {
		t.Errorf("nothing said reads %q", quiet)
	}
	v := &SpeechView{ScreenReader: "TalkBack", Ended: "listened", Steps: []SpeechStep{
		{Said: "Live cart: 1", Cause: "change", Element: &SpokenElement{Label: "Live cart: 1"}},
	}}
	if got := v.text(); !strings.Contains(got, `"Live cart: 1"`) || !strings.Contains(got, "[a change on screen]") ||
		strings.Contains(got, "ended:") {
		t.Errorf("a listen reads %q", got)
	}
}

func TestListenTextSaysItWasStillTalking(t *testing.T) {
	v := &SpeechView{ScreenReader: "TalkBack", Ended: "listened", StillTalking: true}
	if got := v.text(); !strings.Contains(got, "still talking") {
		t.Errorf("a listen cut off mid-speech reads %q", got)
	}
}
