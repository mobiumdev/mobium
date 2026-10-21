package agent

import (
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// A request that acts for the device's owner is refused without an app;
// asking something, or opening an app, is not.
func TestActsForSomeone(t *testing.T) {
	for req, want := range map[string]string{
		"Text Mom that I am late":      "text",
		"Hey Google, call Alex":        "call",
		"OK Siri please send an email": "send",
		"Buy more milk":                "buy",
		"Open Clock":                   "",
		"What is 15 times 3":           "",
		"Show my texts":                "",
		"Calculate 15 times 3":         "",
	} {
		if got := actsForSomeone(req); got != want {
			t.Errorf("actsForSomeone(%q) = %q, want %q", req, got, want)
		}
	}
}

// Reached is the assistant handing the request to the app, never the app
// being in front once the panel closes: "Open MobiumApp", a web search
// signed out, read as MobiumApp coming forward when it had been under the
// panel all along.
func TestReachedNeedsAHandOff(t *testing.T) {
	v := &AssistantView{Front: "dev.mobium.mobiumapp"}
	v.setReached("dev.mobium.mobiumapp", false)
	if v.Reached == nil || *v.Reached {
		t.Error("an app uncovered by closing the panel was reported reached")
	}
	v.setReached("dev.mobium.mobiumapp", true)
	if v.Reached == nil || !*v.Reached {
		t.Error("an app the assistant handed off to was not reported reached")
	}
	v = &AssistantView{Front: "x"}
	v.setReached("", true)
	if v.Reached != nil {
		t.Error("reached was reported with no app named")
	}
}

// The answer is new text that is not a control's: Google Assistant's
// suggestion chips and the request itself are left out.
func TestNewTextsLeavesOutControlsAndTheRequest(t *testing.T) {
	xml := `<hierarchy><node class="android.widget.FrameLayout" package="` + assistantPackage + `" bounds="[0,0][1080,2400]">` +
		`<node class="android.widget.TextView" package="` + assistantPackage + `" text="What is 15 times 3" bounds="[0,100][1080,200]" />` +
		`<node class="android.widget.TextView" package="` + assistantPackage + `" text="45" bounds="[0,200][1080,300]" />` +
		`<node class="android.widget.Button" package="` + assistantPackage + `" text="Learn English" clickable="true" bounds="[0,300][1080,400]" />` +
		`<node class="android.widget.TextView" package="com.example" text="under the panel" bounds="[0,400][1080,500]" />` +
		`</node></hierarchy>`
	tr, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	got := newTexts(tr, assistantPackage, map[string]bool{"what is 15 times 3": true})
	if len(got) != 1 || got[0] != "45" {
		t.Errorf("answer %v, want [45]", got)
	}
}
