package agent

import (
	"errors"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

func front(app string) func() (string, error) { return func() (string, error) { return app, nil } }

func noFront(t *testing.T) func() (string, error) {
	return func() (string, error) {
		t.Helper()
		t.Error("the app in front was asked for when the call could be decided without it")
		return "", errors.New("asked")
	}
}

func TestReadOnlyRefusesWhatActsAndAllowsWhatReads(t *testing.T) {
	p := Policy{ReadOnly: true}
	for _, c := range []struct {
		name string
		args map[string]interface{}
		ok   bool
	}{
		{"app_map", nil, true},
		{"app_text", nil, true},
		{"app_alert", map[string]interface{}{}, true}, // reports the dialog
		{"app_alert", map[string]interface{}{"action": "accept"}, false},
		{"app_tap", map[string]interface{}{"target": "@e1"}, false},
		{"app_uninstall", map[string]interface{}{"app": "x"}, false},
		{"app_batch", map[string]interface{}{}, true}, // its steps are held one by one
	} {
		err := p.permit(c.name, c.args, noFront(t))
		if c.ok != (err == nil) {
			t.Errorf("%s %v: %v", c.name, c.args, err)
		}
		if err != nil && mobiumerr.CodeOf(err) != mobiumerr.NotPermitted {
			t.Errorf("%s refused as %s, want not_permitted", c.name, mobiumerr.CodeOf(err))
		}
	}
}

func TestAnAppListHoldsWhatACallNamesAndWhatIsInFront(t *testing.T) {
	p := Policy{Apps: []string{"dev.mobium.shop"}}
	if err := p.permit("app_launch", map[string]interface{}{"app": "dev.mobium.shop"}, noFront(t)); err != nil {
		t.Errorf("launching the allowed app: %v", err)
	}
	if err := p.permit("app_clear_data", map[string]interface{}{"app": "com.android.chrome"}, noFront(t)); mobiumerr.CodeOf(err) != mobiumerr.NotPermitted {
		t.Errorf("clearing another app's data: %v", err)
	}
	if err := p.permit("app_install", map[string]interface{}{"path": "x.apk"}, noFront(t)); mobiumerr.CodeOf(err) != mobiumerr.NotPermitted {
		t.Errorf("installing under an app list: %v", err)
	}
	if err := p.permit("app_tap", map[string]interface{}{"target": "@e1"}, front("dev.mobium.shop")); err != nil {
		t.Errorf("a tap in the allowed app: %v", err)
	}
	if err := p.permit("app_tap", map[string]interface{}{"target": "@e1"}, front("com.android.settings")); mobiumerr.CodeOf(err) != mobiumerr.NotPermitted {
		t.Errorf("a tap in Settings: %v", err)
	}
	// A permission prompt over the allowed app is answered on its behalf.
	if err := p.permit("app_alert", map[string]interface{}{"action": "accept"}, front("com.google.android.permissioncontroller")); err != nil {
		t.Errorf("answering a permission prompt: %v", err)
	}
	// Reads and device settings are not the app list's business.
	if err := p.permit("app_map", nil, noFront(t)); err != nil {
		t.Errorf("a read: %v", err)
	}
	if err := p.permit("app_appearance", map[string]interface{}{"appearance": "dark"}, noFront(t)); err != nil {
		t.Errorf("a device setting: %v", err)
	}
}

func TestPoliciesOnlyNarrow(t *testing.T) {
	a := Policy{Apps: []string{"a", "b"}}
	if m := a.Merge(Policy{ReadOnly: true}); !m.ReadOnly || len(m.Apps) != 2 {
		t.Errorf("merge = %+v", m)
	}
	if m := a.Merge(Policy{Apps: []string{"b", "c"}}); len(m.Apps) != 1 || m.Apps[0] != "b" {
		t.Errorf("two lists = %+v, want their common app", m)
	}
	if m := a.Merge(Policy{Apps: []string{"c"}}); m.allows("a") || m.allows("c") || len(m.Apps) == 0 {
		t.Errorf("two lists with nothing in common = %+v, want nothing allowed", m)
	}
	if got := policyOfMeta(map[string]interface{}{MetaPolicy: PolicyMeta(Policy{ReadOnly: true, Apps: []string{"x"}})}); !got.ReadOnly || len(got.Apps) != 1 {
		t.Errorf("the policy across _meta = %+v", got)
	}
	if PolicyMeta(Policy{}) != nil {
		t.Error("an empty policy travels")
	}
}

// The policy is held before anything reaches a device: a read-only Mobium
// refuses a tap with no device connected at all.
func TestARefusedCallNeverReachesTheDevice(t *testing.T) {
	h := NewHandlers()
	h.SetPolicy(Policy{ReadOnly: true})
	_, err := h.Call("app_tap", map[string]interface{}{"target": "@e1", "device": "emulator-9999"})
	if mobiumerr.CodeOf(err) != mobiumerr.NotPermitted {
		t.Fatalf("tap under read-only: %v", err)
	}
	// The call's own policy narrows the handlers': a daemon started without
	// one still holds a call that carries it.
	h2 := NewHandlers()
	_, err = h2.CallMeta("app_uninstall", map[string]interface{}{"app": "x", "device": "emulator-9999"},
		map[string]interface{}{MetaPolicy: PolicyMeta(Policy{ReadOnly: true})})
	if mobiumerr.CodeOf(err) != mobiumerr.NotPermitted {
		t.Fatalf("uninstall with a read-only _meta: %v", err)
	}
}

func TestReadOnlyListsOnlyWhatCanRead(t *testing.T) {
	names := map[string]bool{}
	for _, tl := range ToolsFor(Policy{ReadOnly: true}) {
		names[tl.Name] = true
	}
	for _, want := range []string{"app_map", "app_text", "app_alert", "app_screenshot"} {
		if !names[want] {
			t.Errorf("%s not listed under read-only", want)
		}
	}
	for _, not := range []string{"app_tap", "app_uninstall", "app_launch"} {
		if names[not] {
			t.Errorf("%s listed under read-only", not)
		}
	}
	if len(ToolsFor(Policy{})) != len(GetToolSchemas()) {
		t.Error("no policy hid a tool")
	}
}

func TestAnnotationsSayWhatACallMayDo(t *testing.T) {
	for _, c := range []struct {
		name              string
		destructive, idem interface{}
	}{
		{"app_uninstall", true, true},
		{"app_launch", false, true},
		{"app_set", nil, true},
		{"app_tap", nil, nil}, // a tap can press Delete: MCP's default stands
	} {
		a := Annotations(c.name)
		if a["readOnlyHint"] != false || a["destructiveHint"] != c.destructive || a["idempotentHint"] != c.idem {
			t.Errorf("%s: %v", c.name, a)
		}
	}
	for _, tl := range GetToolSchemas() {
		a := tl.Annotations
		if a["readOnlyHint"] == true && (a["destructiveHint"] != nil || a["idempotentHint"] != nil) {
			t.Errorf("%s reads only and says what an action would do: %v", tl.Name, a)
		}
		if destructiveTools[tl.Name] && additiveTools[tl.Name] {
			t.Errorf("%s is both destructive and additive", tl.Name)
		}
	}
	for name := range destructiveTools {
		if !actingTools[name] {
			t.Errorf("%s is destructive and not an acting tool", name)
		}
	}
}
