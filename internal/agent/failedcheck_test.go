package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
	"github.com/mobiumdev/mobium/internal/webview"
)

// Every refusal from a check an action makes has one shape, native or web:
// "X failed check C: reason", with C and the reason in details, and C spelled
// as the page spells it — the cover's was receives_events natively and
// receivesEvents in a WebView until they shared this.
func TestCheckRefusalsShareOneShape(t *testing.T) {
	loc := uitree.Locator{Kind: uitree.KindTestID, Value: "submit"}
	cases := []struct {
		check string
		err   error
	}{
		{checkEnabled, notEnabled(loc, 2*time.Second)},
		{checkEnabled, notEnabledInPage(loc, 2*time.Second)},
		{checkReceivesEvents, keyboardOver(loc, true)},
		{checkReceivesEvents, dialogOver("Allow?", loc, true)},
		{checkReceivesEvents, webCheckFailed("#go", &webview.Actionability{Status: "failed", Check: "receivesEvents", Reason: "covered"}, time.Second)},
	}
	for _, c := range cases {
		e, ok := mobiumerr.As(c.err)
		if !ok {
			t.Fatalf("%v is not a coded error", c.err)
		}
		if !strings.Contains(e.Message, " failed check "+c.check+": ") {
			t.Errorf("message %q is not in the failed-check shape for %s", e.Message, c.check)
		}
		if e.Details["check"] != c.check {
			t.Errorf("details check = %v, want %s (%q)", e.Details["check"], c.check, e.Message)
		}
		if r, _ := e.Details["reason"].(string); r == "" || !strings.Contains(e.Message, r) {
			t.Errorf("details reason %q is missing or not what the message says (%q)", r, e.Message)
		}
	}
}

// What is not a failed check keeps its own shape: nothing matched at all, and
// the keyboard is only the likely reason. A dialog that matched nothing is a
// failed check since mobiumerr.ElementNotReachable took every cover
// (mobiumdev/mobium#318): on Android the hierarchy is the dialog's window
// alone, so what it covers is never found, and the refusal still names both
// readings — behind it, or one of its own buttons misnamed.
func TestAMissIsNotAFailedCheck(t *testing.T) {
	loc := uitree.Locator{Kind: uitree.KindTestID, Value: "submit"}
	if err := keyboardOver(loc, false); strings.Contains(err.Error(), "failed check") {
		t.Errorf("a miss read as a failed check: %v", err)
	}
	err := dialogOver("Allow?", loc, false)
	if !covered(err) || !strings.Contains(err.Error(), "one of the dialog's buttons") {
		t.Errorf("a dialog that matched nothing: %v", err)
	}
}

// A field of a WebView's page reported disabled may be read-only, which
// nothing enables: its remedy names the WebView's context, where the page
// tells the two apart, never a wait (CHALLENGES 300).
func TestADisabledPageFieldIsSentToItsWebView(t *testing.T) {
	e, _ := mobiumerr.As(notEnabledInPage(uitree.Locator{Kind: uitree.KindText, Value: "fixed"}, 2*time.Second))
	if !strings.Contains(e.Message, "read-only") || !strings.Contains(e.Remedy, "app_context") {
		t.Errorf("message %q / remedy %q do not name read-only and the WebView's context", e.Message, e.Remedy)
	}
	if strings.Contains(e.Remedy, "app_wait_for") {
		t.Errorf("the remedy offers a wait, which a read-only field never ends: %q", e.Remedy)
	}
}

// Everything over a target is one refusal, on both platforms: the code
// element_not_reachable, the check receivesEvents, and what covers it in
// details. A dialog, the keyboard and the rest were device_not_ready, which a
// frozen emulator also is, so a client could not tell "a dialog is in the
// way" from "the device is hung" by the code (mobiumdev/mobium#318). None is
// a miss, so none is scrolled for.
func TestEveryCoverIsElementNotReachable(t *testing.T) {
	loc := uitree.Locator{Kind: uitree.KindTestID, Value: "submit"}
	for _, c := range []struct {
		name   string
		err    error
		detail string
	}{
		{"a dialog, the target underneath (iOS)", dialogOver("Discard changes?", loc, true), "dialog"},
		{"a dialog, the target not found (Android)", dialogOver("Discard changes?", loc, false), "dialog"},
		{"an app's own dialog", appDialogOver("User guide", loc, true), "dialog"},
		{"the keyboard", keyboardOver(loc, true), "keyboard"},
		{"another screen", screenOver(loc, nil), "screen"},
		{"Siri's panel", siriOver(loc, 0), "siri"},
		{"an assistant's overlay", assistantOver("Gemini", loc), "assistant"},
		{"a dialog over a page", webDialogOver("Allow?", "WEBVIEW_x"), "dialog"},
	} {
		e, ok := mobiumerr.As(c.err)
		if !ok || e.Code != mobiumerr.ElementNotReachable {
			t.Errorf("%s: %v, want element_not_reachable", c.name, c.err)
			continue
		}
		if e.Details["check"] != checkReceivesEvents || e.Details[c.detail] == nil {
			t.Errorf("%s: details %v, want check receivesEvents and %s", c.name, e.Details, c.detail)
		}
		if matchedNothing(c.err) {
			t.Errorf("%s: read as a miss, which would be scrolled for", c.name)
		}
		if mobiumerr.ExitCode(e.Code) != 4 {
			t.Errorf("%s: exit %d, want 4", c.name, mobiumerr.ExitCode(e.Code))
		}
	}
	// The keyboard over a target Android left out of the tree is still a miss,
	// and says the keyboard was up.
	if e, _ := mobiumerr.As(keyboardOver(loc, false)); e.Code != mobiumerr.NoSuchElement || e.Details["keyboard"] != true {
		t.Errorf("the keyboard, target not found: %v %v", e.Code, e.Details)
	}
}
