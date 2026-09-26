package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// button builds a one-button screen whose button is enabled or not.
func button(t *testing.T, enabled bool) *uitree.Tree {
	t.Helper()
	xml := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">`+
		`<node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]" enabled="true">`+
		`<node index="0" text="Log In" resource-id="app:id/loginBtn" class="android.widget.Button" `+
		`clickable="true" enabled="%t" bounds="[100,200][300,280]" /></node></hierarchy>`, enabled)
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// An action waits for its target to be enabled, as Playwright's and Vibium's
// do, and taps once it is: MobiumApp's Log In is disabled while it signs in.
func TestATapWaitsForItsTargetToBeEnabled(t *testing.T) {
	h, s, f := withFake(t, button(t, false), button(t, false), button(t, true))
	h.implicitWait = 2 * time.Second
	if _, err := h.tapOn(context.Background(), s, map[string]interface{}{"target": "testid=loginBtn"}); err != nil {
		t.Fatalf("a button enabled mid-wait was not tapped: %v", err)
	}
	if len(f.tapped) != 1 {
		t.Errorf("tapped %d times, want once", len(f.tapped))
	}
}

// One that stays disabled is refused, not tapped for nothing, not scrolled
// for, and the refusal names the wait that works.
func TestATapOnAStillDisabledTargetIsRefused(t *testing.T) {
	h, s, f := withFake(t, button(t, false))
	h.implicitWait = 300 * time.Millisecond
	_, err := h.tapOn(context.Background(), s, map[string]interface{}{"target": "testid=loginBtn"})
	if err == nil || len(f.tapped) != 0 {
		t.Fatalf("a disabled button was tapped (%d taps): %v", len(f.tapped), err)
	}
	if !strings.Contains(err.Error(), "disabled") || !strings.Contains(err.Error(), `condition "enabled"`) {
		t.Errorf("the refusal does not say disabled, or name the wait: %v", err)
	}
	if matchedNothing(err) || mobiumerr.CodeOf(err) != mobiumerr.Timeout {
		t.Errorf("code %s; a disabled control is not a miss", mobiumerr.CodeOf(err))
	}
}

// app_wait_for waits for enabled and for disabled.
func TestWaitForEnabledAndDisabled(t *testing.T) {
	h, s, _ := withFake(t, button(t, false), button(t, true))
	if _, err := h.waitOn(context.Background(), s, map[string]interface{}{
		"target": "testid=loginBtn", "condition": "enabled", "timeout_ms": 2000}); err != nil {
		t.Errorf("waiting for enabled: %v", err)
	}
	h, s, _ = withFake(t, button(t, true))
	_, err := h.waitOn(context.Background(), s, map[string]interface{}{
		"target": "testid=loginBtn", "condition": "disabled", "timeout_ms": 300})
	if err == nil || !strings.Contains(err.Error(), "it is enabled") {
		t.Errorf("a wait for disabled on an enabled button: %v", err)
	}
}

// A driver process that never sends enabled has its nodes enabled, as it
// has them displayed; otherwise every tap it serves would now be refused.
func TestTheWireDefaultsToEnabled(t *testing.T) {
	tree, err := uitree.UnmarshalWire([]byte(`{"class":"android.widget.FrameLayout","bounds":[0,0,100,100],` +
		`"children":[{"class":"android.widget.Button","text":"Go","clickable":true,"bounds":[0,0,50,50]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	tree.Walk(func(n *uitree.Node) bool {
		if n.Text == "Go" {
			found = true
			if !n.Enabled {
				t.Error("a node sent without enabled reads as disabled")
			}
		}
		return true
	})
	if !found {
		t.Fatal("the wire node was not parsed")
	}
}

// Typing is refused only into what is certainly not a text field. A custom
// view no table knows is let through: refusing a real field would be worse
// than the server's own error.
func TestNotEditable(t *testing.T) {
	xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]" enabled="true">` +
		`<node index="0" text="Back" resource-id="app:id/button" class="android.widget.Button" clickable="true" enabled="true" bounds="[0,0][100,100]" />` +
		`<node index="1" text="" resource-id="app:id/edit" class="android.widget.EditText" enabled="true" bounds="[0,100][100,200]" />` +
		`<node index="2" text="" resource-id="app:id/auto" class="android.widget.AutoCompleteTextView" enabled="true" bounds="[0,200][100,300]" />` +
		`<node index="3" text="" resource-id="app:id/pass" class="android.widget.EditText" password="true" enabled="true" bounds="[0,300][100,400]" />` +
		`<node index="4" text="" resource-id="app:id/ink" class="com.example.InkView" clickable="true" enabled="true" bounds="[0,400][100,500]" />` +
		`</node></hierarchy>`
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"button": "button", "edit": "", "auto": "", "pass": "", "ink": ""}
	tree.Walk(func(n *uitree.Node) bool {
		if w, ok := want[n.ShortTestID()]; ok {
			if got := notEditable(n); got != w {
				t.Errorf("%s: notEditable = %q, want %q", n.ShortTestID(), got, w)
			}
		}
		return true
	})
}

// typingDriver refuses every type the way UiAutomator2 refused typing into a
// button: the W3C code "invalid element state".
type typingDriver struct{ fakeDriver }

func (d *typingDriver) SetText(ctx context.Context, n *uitree.Node, text string) error {
	return mobiumerr.New(mobiumerr.DeviceServer, "invalid element state: Cannot set the element to %q", text).
		WithDetail("w3c", "invalid element state")
}
func (d *typingDriver) Clear(ctx context.Context, n *uitree.Node) error { return nil }

// Where no class table knows a node is not a field, the device's own refusal
// is translated — by its W3C code, not its wording — into the same answer.
func TestTheDevicesNotEditableIsTranslated(t *testing.T) {
	xml := `<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">` +
		`<node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]" enabled="true">` +
		`<node index="0" text="Ink" resource-id="app:id/ink" class="com.example.InkView" clickable="true" ` +
		`enabled="true" bounds="[100,200][300,280]" /></node></hierarchy>`
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	d := &typingDriver{fakeDriver{screens: []*uitree.Tree{tree}}}
	h := NewHandlers()
	h.implicitWait, h.settleWindow = 0, 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	_, err = h.typeTextOn(context.Background(), s, map[string]interface{}{"target": "testid=ink", "text": "hello"})
	if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument || !strings.Contains(err.Error(), "not a text field") {
		t.Errorf("the device's refusal was passed through raw: %v", err)
	}
}
