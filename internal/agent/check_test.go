package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// boxDriver is a screen with one checkable control on it.
type boxDriver struct {
	fakeDriver
	class   string
	checked bool
	taps    int
	// stuck means the control does not respond — a tap lands and nothing
	// changes, which is what a disabled or covered control does.
	stuck bool
}

func (d *boxDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	n := &uitree.Node{
		TestID:    "box",
		Label:     "Accept terms",
		Class:     d.class,
		Bounds:    uitree.Rect{X1: 0, Y1: 0, X2: 100, Y2: 40},
		Clickable: true,
		Checkable: d.class != "android.widget.Button",
		Checked:   d.checked,
		Displayed: true,
		Enabled:   true, // the zero value is disabled, which actions now wait on
	}
	root := &uitree.Node{Class: "hierarchy", Displayed: true, Children: []*uitree.Node{n}}
	n.Parent = root
	return &uitree.Tree{Root: root}, nil
}

func (d *boxDriver) Tap(ctx context.Context, x, y int) error {
	d.taps++
	if !d.stuck {
		d.checked = !d.checked
	}
	return nil
}

func (d *boxDriver) Name() string { return "box-fake" }

func checkOn(t *testing.T, d *boxDriver, args map[string]interface{}) (*ToolsCallResult, error) {
	t.Helper()
	h := NewHandlers()
	h.implicitWait, h.settleWindow = 0, 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendUIA2}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return h.checkOn(ctx, s, args)
}

// The point of the tool. Asking for a state it is already in must do nothing —
// not tap and toggle it away, which is what app_tap would do and why this
// exists.
func TestCheckIsIdempotent(t *testing.T) {
	d := &boxDriver{class: "android.widget.CheckBox", checked: true}
	res, err := checkOn(t, d, map[string]interface{}{"target": "testid=box", "value": true})
	if err != nil {
		t.Fatal(err)
	}
	if d.taps != 0 {
		t.Errorf("tapped %d times on a control already in the wanted state", d.taps)
	}
	if !d.checked {
		t.Error("the control was toggled away from the state that was asked for")
	}
	v := res.StructuredContent.(CheckView)
	if v.Changed || !v.Checked {
		t.Errorf("view = %+v", v)
	}
	if !strings.Contains(textOf(res), "already checked") {
		t.Errorf("answer = %q", textOf(res))
	}
}

func TestCheckTapsOnlyWhenItMustAndConfirms(t *testing.T) {
	d := &boxDriver{class: "android.widget.CheckBox", checked: false}
	res, err := checkOn(t, d, map[string]interface{}{"target": "testid=box", "value": true})
	if err != nil {
		t.Fatal(err)
	}
	if d.taps != 1 {
		t.Errorf("tapped %d times, want exactly 1", d.taps)
	}
	if v := res.StructuredContent.(CheckView); !v.Changed || !v.Checked {
		t.Errorf("view = %+v", v)
	}
}

func TestUncheckIsTheSameToolTheOtherWay(t *testing.T) {
	d := &boxDriver{class: "android.widget.CheckBox", checked: true}
	if _, err := checkOn(t, d, map[string]interface{}{"target": "testid=box", "value": false}); err != nil {
		t.Fatal(err)
	}
	if d.checked || d.taps != 1 {
		t.Errorf("checked=%v taps=%d", d.checked, d.taps)
	}
}

// A tap that lands and changes nothing must be reported. That is the whole
// difference between this and app_tap, which would return perfectly happily.
func TestCheckReportsAControlThatDidNotRespond(t *testing.T) {
	d := &boxDriver{class: "android.widget.CheckBox", checked: false, stuck: true}
	_, err := checkOn(t, d, map[string]interface{}{"target": "testid=box", "value": true})
	if err == nil {
		t.Fatal("a control that ignored the tap was reported as checked")
	}
	for _, want := range []string{"still unchecked", "did not respond"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not say what happened (%q): %v", want, err)
		}
	}
}

// Tapping a button and calling it checked is precisely the class of thing this
// project refuses to do.
func TestCheckRefusesSomethingWithNoState(t *testing.T) {
	d := &boxDriver{class: "android.widget.Button"}
	_, err := checkOn(t, d, map[string]interface{}{"target": "testid=box", "value": true})
	if err == nil {
		t.Fatal("a button was accepted as a checkbox")
	}
	if d.taps != 0 {
		t.Error("a button with no checked state was tapped anyway")
	}
	if !strings.Contains(err.Error(), "app_tap") {
		t.Errorf("the refusal does not name what to use instead: %v", err)
	}
}

// A radio is not unchecked; its group is cleared by choosing another member.
// Obeying literally would tap a selected radio and then wait for a change
// that is never coming.
func TestCheckRefusesToUncheckARadio(t *testing.T) {
	d := &boxDriver{class: "android.widget.RadioButton", checked: true}
	_, err := checkOn(t, d, map[string]interface{}{"target": "testid=box", "value": false})
	if err == nil {
		t.Fatal("unchecking a radio was accepted")
	}
	if d.taps != 0 {
		t.Error("the radio was tapped before the refusal")
	}
	if !strings.Contains(err.Error(), "choose a different one") {
		t.Errorf("err = %v", err)
	}
	// Checking one is fine, and must still work.
	d2 := &boxDriver{class: "android.widget.RadioButton", checked: false}
	if _, err := checkOn(t, d2, map[string]interface{}{"target": "testid=box", "value": true}); err != nil {
		t.Errorf("checking a radio was refused: %v", err)
	}
}

func TestCheckDefaultsToChecked(t *testing.T) {
	d := &boxDriver{class: "android.widget.CheckBox", checked: false}
	if _, err := checkOn(t, d, map[string]interface{}{"target": "testid=box"}); err != nil {
		t.Fatal(err)
	}
	if !d.checked {
		t.Error("app_set with no state did not check it")
	}
}

// A Jetpack Compose switch row is one clickable, checkable node with its
// words on a child. Seal's "Dynamic color" by its text resolves to the words;
// the state is the row's, and a row with no state is still refused
// (CHALLENGES 178).
func TestAComposeRowsStateIsFoundFromItsWords(t *testing.T) {
	raw, err := os.ReadFile("../uitree/testdata/seal-look-uia2.xml")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := uitree.ParseAndroid(raw)
	if err != nil {
		t.Fatal(err)
	}
	words := func(text string) *uitree.Node {
		var found *uitree.Node
		tree.Walk(func(n *uitree.Node) bool {
			if found == nil && n.Text == text {
				found = n
			}
			return found == nil
		})
		if found == nil {
			t.Fatalf("no %q in the capture", text)
		}
		return found
	}
	n := words("Dynamic color")
	if n.Checkable {
		t.Fatal("the capture's words carry the state themselves; the test would prove nothing")
	}
	if row := stateOf(n); !row.Checkable || !row.Clickable {
		t.Errorf("Dynamic color's state was not found on its row: %+v", row)
	}
	if got := stateOf(words("Display language")); got.Checkable {
		t.Error("a row with no state was given one")
	}
}

// pageBoxDriver is a WebView whose page has a checkbox the native tree
// carries no checked state for, as MobiumApp's Web form read on Android 15.
type pageBoxDriver struct{ fakeDriver }

func (d *pageBoxDriver) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	box := &uitree.Node{Label: "A checkbox", Class: "android.view.View", Clickable: true, Displayed: true, Enabled: true,
		Bounds: uitree.Rect{X1: 10, Y1: 10, X2: 60, Y2: 40}}
	web := &uitree.Node{Class: "android.webkit.WebView", Displayed: true, Enabled: true,
		Bounds: uitree.Rect{X1: 0, Y1: 0, X2: 1080, Y2: 2000}, Children: []*uitree.Node{box}}
	root := &uitree.Node{Class: "hierarchy", Displayed: true, Children: []*uitree.Node{web}}
	box.Parent, web.Parent = web, root
	return &uitree.Tree{Root: root}, nil
}

// From the native context, a checkbox of a WebView's page is sent to the
// WebView's context, where app_set reads the page — never told it is not a
// checkbox and left to tap (CHALLENGES 299).
func TestCheckSendsAPageCheckboxToItsWebView(t *testing.T) {
	h := NewHandlers()
	h.implicitWait, h.settleWindow = 0, 0
	s := &session{dev: fakeDevice(), driver: &pageBoxDriver{}, backend: BackendUIA2}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	_, err := h.checkOn(ctx, s, map[string]interface{}{"target": "label=A checkbox"})
	if err == nil {
		t.Fatal("checked a node with no checked state")
	}
	msg := err.Error()
	if !strings.Contains(msg, "WebView") || strings.Contains(msg, "not a checkbox") {
		t.Errorf("the refusal does not send the caller to the WebView: %s", msg)
	}
	if e, ok := mobiumerr.As(err); !ok || !strings.Contains(e.Remedy, "app_context") {
		t.Errorf("the remedy does not name the context switch: %+v", err)
	}
}

func TestInWebView(t *testing.T) {
	d := &pageBoxDriver{}
	tree, _ := d.Snapshot(context.Background())
	web := tree.Root.Children[0]
	if !inWebView(web.Children[0]) {
		t.Error("a node under a WebView is not seen as part of its page")
	}
	if inWebView(web) || inWebView(nil) {
		t.Error("the WebView itself, or nothing, is not part of a page")
	}
}
