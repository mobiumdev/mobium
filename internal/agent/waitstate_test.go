package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// field builds a screen with one node of the given class and attributes, so a
// state can change between screens while the locator, testid=f, does not.
func field(t *testing.T, class, attrs string) *uitree.Tree {
	t.Helper()
	xml := fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8'?><hierarchy rotation="0">`+
		`<node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,2400]">`+
		`<node index="0" resource-id="app:id/f" class="%s" clickable="true" enabled="true" `+
		`bounds="[100,200][900,300]" %s /></node></hierarchy>`, class, attrs)
	tree, err := uitree.ParseAndroid([]byte(xml))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return tree
}

// focusFake answers focus from the tree, as both Android backends do.
type focusFake struct{ *fakeDriver }

func (focusFake) HasFocus(_ context.Context, n *uitree.Node) (bool, error) { return n.Focused, nil }

func TestWaitForCheckedAndUnchecked(t *testing.T) {
	off := field(t, "android.widget.CheckBox", `checkable="true" checked="false"`)
	on := field(t, "android.widget.CheckBox", `checkable="true" checked="true"`)

	h, sess, _ := withFake(t, off, off, on)
	if _, err := wait(h, sess, map[string]interface{}{"target": "testid=f", "condition": "checked",
		"timeout_ms": 5000}); err != nil {
		t.Fatalf("wait checked: %v", err)
	}

	// The control: a box that never changes is reported as it was.
	h, sess, _ = withFake(t, off)
	_, err := wait(h, sess, map[string]interface{}{"target": "testid=f", "condition": "checked", "timeout_ms": 200})
	if mobiumerr.CodeOf(err) != mobiumerr.Timeout || !strings.Contains(err.Error(), "it is unchecked") {
		t.Fatalf("a box that stayed unchecked: %v", err)
	}

	h, sess, _ = withFake(t, on, on, off)
	if _, err := wait(h, sess, map[string]interface{}{"target": "testid=f", "condition": "unchecked",
		"timeout_ms": 5000}); err != nil {
		t.Fatalf("wait unchecked: %v", err)
	}
}

// Nothing without a checked state will ever have one, so the wait is refused
// at once rather than timing out — as app_check refuses it.
func TestWaitForCheckedRefusesAButton(t *testing.T) {
	h, sess, f := withFake(t, field(t, "android.widget.Button", `text="Go"`))
	_, err := wait(h, sess, map[string]interface{}{"target": "testid=f", "condition": "checked", "timeout_ms": 5000})
	if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument || !strings.Contains(err.Error(), "no checked state") {
		t.Fatalf("a button was waited on for a checked state: %v", err)
	}
	if n := f.calls.Load(); n != 1 {
		t.Errorf("read the screen %d times before refusing; once is enough", n)
	}
}

func TestWaitForValueIsTheWholeValue(t *testing.T) {
	typing := field(t, "android.widget.EditText", `text="ab"`)
	typed := field(t, "android.widget.EditText", `text="abc"`)
	longer := field(t, "android.widget.EditText", `text="abcd"`)

	h, sess, _ := withFake(t, typing, typed)
	if _, err := wait(h, sess, map[string]interface{}{"target": "testid=f", "condition": "value", "text": "abc",
		"timeout_ms": 5000}); err != nil {
		t.Fatalf("wait value: %v", err)
	}

	// Where text would be satisfied by "abcd", value is not.
	h, sess, _ = withFake(t, longer)
	_, err := wait(h, sess, map[string]interface{}{"target": "testid=f", "condition": "value", "text": "abc",
		"timeout_ms": 200})
	if mobiumerr.CodeOf(err) != mobiumerr.Timeout || !strings.Contains(err.Error(), `its value is "abcd"`) {
		t.Fatalf("a longer value satisfied the wait: %v", err)
	}
}

// A placeholder is where an empty field's text goes, on both platforms, so a
// field showing its hint holds "".
func TestWaitForAnEmptyValueSeesPastThePlaceholder(t *testing.T) {
	hint := field(t, "android.widget.EditText", `text="Search" hint="Search" showing-hint="true"`)
	h, sess, _ := withFake(t, hint)
	if _, err := wait(h, sess, map[string]interface{}{"target": "testid=f", "condition": "value", "text": "",
		"timeout_ms": 200}); err != nil {
		t.Fatalf("a field showing its placeholder was not empty: %v", err)
	}

	_, err := wait(h, sess, map[string]interface{}{"target": "testid=f", "condition": "value", "timeout_ms": 200})
	if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Fatalf("value with no value to wait for: %v", err)
	}
}

// A password's value is never compared, so a wait can neither leak it nor
// print it.
func TestWaitForValueRefusesAPassword(t *testing.T) {
	h, sess, _ := withFake(t, field(t, "android.widget.EditText", `text="hunter2" password="true"`))
	_, err := wait(h, sess, map[string]interface{}{"target": "testid=f", "condition": "value", "text": "hunter2",
		"timeout_ms": 5000})
	if mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Fatalf("a password was compared: %v", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the refusal printed the password: %v", err)
	}
}

func TestWaitForFocus(t *testing.T) {
	blurred := field(t, "android.widget.EditText", `focused="false"`)
	focused := field(t, "android.widget.EditText", `focused="true"`)

	h, sess, f := withFake(t, blurred, focused)
	sess.driver = focusFake{f}
	if _, err := wait(h, sess, map[string]interface{}{"target": "testid=f", "condition": "focused",
		"timeout_ms": 5000}); err != nil {
		t.Fatalf("wait focused: %v", err)
	}

	h, sess, f = withFake(t, blurred)
	sess.driver = focusFake{f}
	_, err := wait(h, sess, map[string]interface{}{"target": "testid=f", "condition": "focused", "timeout_ms": 200})
	if mobiumerr.CodeOf(err) != mobiumerr.Timeout || !strings.Contains(err.Error(), "does not have keyboard focus") {
		t.Fatalf("a field that never had focus: %v", err)
	}
}

// A driver that cannot tell which element has focus would never satisfy the
// wait, so it is refused instead of timing out.
func TestWaitForFocusRefusesADriverThatCannotTell(t *testing.T) {
	h, sess, _ := withFake(t, field(t, "android.widget.EditText", `focused="true"`))
	_, err := wait(h, sess, map[string]interface{}{"target": "testid=f", "condition": "focused", "timeout_ms": 5000})
	if mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
		t.Fatalf("focus was waited for on a driver that cannot read it: %v", err)
	}
}
