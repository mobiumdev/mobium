package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// remoteDriver is an Apple TV: driven with its remote, and every tap is a
// failure of the test.
type remoteDriver struct {
	buttonDriver
	taps int
}

func (r *remoteDriver) RemoteOnly() bool { return true }

// AlertText reads the alert as WebDriverAgent does: its title, or none.
func (r *remoteDriver) AlertText(ctx context.Context) (string, error) {
	if d := r.tree.Dialog(); d != nil {
		return d.Label, nil
	}
	return "", mobiumdriver.ErrNoAlert
}
func (r *remoteDriver) AnswerAlert(ctx context.Context, accept bool) error {
	return mobiumdriver.AlertAnsweredByRemote(r)
}
func (r *remoteDriver) SendAlertText(ctx context.Context, text string) error { return nil }
func (r *remoteDriver) Tap(ctx context.Context, x, y int) error {
	r.taps++
	return mobiumerr.New(mobiumerr.Unsupported, "an Apple TV has no touch screen")
}

// tvPrompt is the dialog tvOS raises before a link opens an app, as captured
// on a tvOS 26.5 simulator: Open beside Cancel, focus on the one named, or no
// dialog at all when focused is "gone".
func tvPrompt(t *testing.T, focused string) *uitree.Tree {
	t.Helper()
	xml := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="PineBoard" bundleId="com.apple.PineBoard" visible="true" x="0" y="0" width="1920" height="1080">`
	if focused != "gone" {
		xml += `<XCUIElementTypeAlert type="XCUIElementTypeAlert" name="Open in “Morpho Market”?" label="Open in “Morpho Market”?" visible="true" x="668" y="408" width="584" height="264">`
		for i, b := range []string{"Open", "Cancel"} {
			xml += fmt.Sprintf(`<XCUIElementTypeButton type="XCUIElementTypeButton" name="%s" label="%s" visible="true" `+
				`accessible="true" enabled="true" focused="%v" x="%d" y="573" width="250" height="70"/>`,
				b, b, b == focused, 694+i*278)
		}
		xml += `</XCUIElementTypeAlert>`
	}
	tree, err := uitree.ParseIOS([]byte(xml + `</XCUIElementTypeApplication>`))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func withRemote(t *testing.T) (*Handlers, *session, *remoteDriver) {
	t.Helper()
	d := &remoteDriver{buttonDriver: buttonDriver{buttons: mobiumdriver.AllButtons()}}
	h := NewHandlers()
	h.implicitWait = 0
	h.settleWindow = 0
	s := &session{dev: fakeDevice(), driver: d, backend: BackendWDA}
	h.sessions["fake"] = s
	return h, s, d
}

// A dialog rule on an Apple TV answers with the remote: the D-pad to the
// button the rule names, read back by focus, and select — never a tap, which
// a TV refuses, so a link's "Open in …?" prompt could not be answered by a
// rule at all (CHALLENGES 334).
func TestDialogRuleOnAppleTVAnswersWithTheRemote(t *testing.T) {
	for _, tc := range []struct {
		press string
		want  []string
	}{
		{"Open", []string{mobiumdriver.ButtonSelect}},
		{"Cancel", []string{mobiumdriver.ButtonDpadRight, mobiumdriver.ButtonSelect}},
	} {
		h, s, d := withRemote(t)
		h.dialogRules[s.dev.Serial] = []dialogRule{{When: "Morpho Market", Press: tc.press}}
		d.tree = tvPrompt(t, "Open")
		d.after = map[string]*uitree.Tree{
			mobiumdriver.ButtonDpadRight: tvPrompt(t, "Cancel"),
			mobiumdriver.ButtonSelect:    tvPrompt(t, "gone"),
		}
		handled, err := h.answerByRule(context.Background(), s)
		if err != nil || !handled {
			t.Fatalf("%s: handled=%v, %v", tc.press, handled, err)
		}
		if d.taps != 0 || strings.Join(d.pressed, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s: pressed %v and tapped %d times, want %v and no tap", tc.press, d.pressed, d.taps, tc.want)
		}
	}
}

// Focus that does not move toward the button is refused, not pressed again
// and not selected on the wrong button.
func TestRemoteRefusesFocusThatDoesNotMove(t *testing.T) {
	h, s, d := withRemote(t)
	d.tree = tvPrompt(t, "Open")
	cancel := tvPrompt(t, "Cancel")
	var target *uitree.Node
	cancel.Walk(func(n *uitree.Node) bool {
		if n.Label == "Cancel" {
			target = n
		}
		return true
	})
	err := h.selectByRemote(context.Background(), s, target)
	if mobiumerr.CodeOf(err) != mobiumerr.NotConfirmed || !strings.Contains(err.Error(), "focus did not move") {
		t.Errorf("got %v", err)
	}
	for _, p := range d.pressed {
		if p == mobiumdriver.ButtonSelect {
			t.Errorf("selected %v with focus on the wrong button", d.pressed)
		}
	}
}

// A refusal's way out on an Apple TV is one it can take: a dialog in the way
// is answered with the remote or a rule, and a screen in front is closed with
// back — not a tap at a point outside it (CHALLENGES 334).
func TestRefusalsOnAppleTVNameTheRemote(t *testing.T) {
	_, s, _ := withRemote(t)
	loc := uitree.Locator{Kind: uitree.KindTestID, Value: "catalogList"}
	tree := tvPrompt(t, "Open")
	for _, err := range []error{
		dialogOver("Open in “Morpho Market”?", loc, false),
		screenOver(loc, tree),
	} {
		got := forRemote(s, err)
		text := mobiumerr.Text(got)
		if strings.Contains(text, "app_tap") || strings.Contains(text, "tap one") {
			t.Errorf("names a tap: %s", text)
		}
		if !strings.Contains(text, "select") && !strings.Contains(text, "app_press back") {
			t.Errorf("names no way a remote has: %s", text)
		}
		if mobiumerr.CodeOf(got) != mobiumerr.CodeOf(err) {
			t.Errorf("code changed: %v → %v", mobiumerr.CodeOf(err), mobiumerr.CodeOf(got))
		}
	}
}
