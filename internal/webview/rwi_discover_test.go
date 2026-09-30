package webview

import "testing"

// On the home screen webinspectord names no application active, and behind
// cannot call a page behind; FrontKnown is what says so.
func TestFrontKnown(t *testing.T) {
	home := &Inspector{states: map[string]appState{"PID:1": {active: 1, known: true}}}
	if home.FrontKnown() {
		t.Error("an app leaving or arriving was taken for the one in front")
	}
	if behind("PID:1", home.states) {
		t.Error("behind claimed to know, with no app in front")
	}
	app := &Inspector{states: map[string]appState{"PID:1": {active: 1, known: true}, "PID:2": {active: appActive, known: true}}}
	if !app.FrontKnown() {
		t.Error("an app in front was not seen")
	}
}
