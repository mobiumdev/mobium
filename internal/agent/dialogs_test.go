package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// A rule is added, replaced by one with the same when, listed and cleared,
// per device; half a rule is refused.
func TestDialogRules(t *testing.T) {
	h := NewHandlers()
	s := &session{dev: &device.Device{Serial: "A"}}
	if _, err := h.dialogsOn(s, map[string]interface{}{"when": "Save Password"}); err == nil {
		t.Error("a rule with no press was accepted")
	}
	h.dialogsOn(s, map[string]interface{}{"when": "Save Password", "press": "Save"})
	h.dialogsOn(s, map[string]interface{}{"when": "save password", "press": "Not Now"})
	res, _ := h.dialogsOn(s, map[string]interface{}{})
	view := res.StructuredContent.(DialogsView)
	if len(view.Rules) != 1 || view.Rules[0].Press != "Not Now" {
		t.Errorf("rules = %+v, want the one, replaced", view.Rules)
	}
	other := &session{dev: &device.Device{Serial: "B"}}
	if res, _ := h.dialogsOn(other, map[string]interface{}{}); len(res.StructuredContent.(DialogsView).Rules) != 0 {
		t.Error("a rule leaked to another device")
	}
	h.dialogsOn(s, map[string]interface{}{"clear": true})
	if len(h.dialogRules["A"]) != 0 {
		t.Error("clear left rules")
	}
}

// A caption is found on both platforms: the text on Android, in capitals or
// not, and the label on iOS — and only among the dialog's own buttons, never
// the app's beneath it.
func TestButtonByCaption(t *testing.T) {
	ios := func(name string) *uitree.Tree {
		raw, err := os.ReadFile("../uitree/testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		tree, err := uitree.ParseIOS(raw)
		if err != nil {
			t.Fatal(err)
		}
		return tree
	}
	if b, _ := buttonByCaption(ios("ios-save-password.xml"), "not now"); b == nil || b.Label != "Not Now" {
		t.Errorf("Not Now on the iOS sheet: %+v", b)
	}
	// Log Out is the app's, under the sheet: not a caption of the dialog.
	b, captions := buttonByCaption(ios("ios-save-password.xml"), "Log Out")
	if b != nil {
		t.Error("found a button under the dialog")
	}
	if !strings.Contains(strings.Join(captions, ","), "Not Now") {
		t.Errorf("the refusal would not list the dialog's buttons: %v", captions)
	}

	raw, err := os.ReadFile("../uitree/testdata/android-permission-uia2.xml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseAndroid(raw)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := buttonByCaption(tree, "WHILE USING THE APP"); b == nil {
		t.Error("an Android caption, in capitals, was not found")
	}
}

// What a rule answered is reported in both halves of the result.
func TestHandledDialogsAreReported(t *testing.T) {
	h := NewHandlers()
	h.handled = []HandledDialog{{Dialog: "Save Password?", Press: "Not Now"}}
	res, err := h.reportHandled(Result("tapped testid=logoutBtn", ActionView{Action: "tap"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content[0].Text, `"Save Password?" — pressed "Not Now"`) {
		t.Errorf("text: %q", res.Content[0].Text)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(raw), `"dialogs_handled"`) {
		t.Errorf("structured: %s", raw)
	}
}

// ruleDriver is iOS's "Save Password?" sheet over the screen a wait is
// waiting for, gone once Not Now is tapped.
type ruleDriver struct {
	alertDriver
	covered, after *uitree.Tree
}

func (d *ruleDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	if len(d.tapped) > 0 {
		return d.after, nil
	}
	return d.covered, nil
}

func (d *ruleDriver) AlertText(ctx context.Context) (string, error) {
	if len(d.tapped) > 0 {
		return "", mobiumdriver.ErrNoAlert
	}
	return d.text, nil
}

// A wait answers a declared rule for the dialog over its target, as an
// action does; it timed out in front of the sheet on a real iPhone. A wait
// for the target to go is left alone, since the sheet already covers it.
func TestAWaitAnswersADialogRule(t *testing.T) {
	raw, err := os.ReadFile("../uitree/testdata/ios-save-password.xml")
	if err != nil {
		t.Fatal(err)
	}
	covered, err := uitree.ParseIOS(raw)
	if err != nil {
		t.Fatal(err)
	}
	fresh := func() (*Handlers, *session, *ruleDriver) {
		d := &ruleDriver{alertDriver: alertDriver{text: "Save Password?\nSecurely store your password"},
			covered: covered, after: screen(t, "Log Out")}
		h := NewHandlers()
		h.implicitWait, h.settleWindow = 0, 0
		s := &session{dev: fakeDevice(), driver: d, backend: BackendWDA}
		return h, s, d
	}

	h, s, d := fresh()
	if _, err := h.dialogsOn(s, map[string]interface{}{"when": "Save Password", "press": "Not Now"}); err != nil {
		t.Fatal(err)
	}
	res, err := h.reportHandled(wait(h, s, map[string]interface{}{"target": "text=Log Out", "timeout_ms": float64(2000)}))
	if err != nil {
		t.Fatalf("the wait did not answer the rule: %v", err)
	}
	if len(d.tapped) != 1 || !strings.Contains(res.Content[0].Text, `pressed "Not Now"`) {
		t.Errorf("tapped %v, and said %q", d.tapped, res.Content[0].Text)
	}

	// No rule: the wait says the dialog is in the way, and taps nothing.
	h, s, d = fresh()
	_, err = wait(h, s, map[string]interface{}{"target": "text=Log Out", "timeout_ms": float64(300)})
	if err == nil || !strings.Contains(err.Error(), "under a dialog") || len(d.tapped) != 0 {
		t.Errorf("with no rule: %v, tapped %v", err, d.tapped)
	}

	// Waiting for the target to go: the sheet covering it is the answer.
	h, s, d = fresh()
	h.dialogsOn(s, map[string]interface{}{"when": "Save Password", "press": "Not Now"})
	if _, err := wait(h, s, map[string]interface{}{"target": "text=Log Out", "not": true, "timeout_ms": float64(300)}); err != nil || len(d.tapped) != 0 {
		t.Errorf("a wait for it to go: %v, tapped %v", err, d.tapped)
	}
}
