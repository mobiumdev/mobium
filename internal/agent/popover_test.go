package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
)

func iosCapture(t *testing.T, name string) *uitree.Tree {
	t.Helper()
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

// Pocket Casts' first-run tip is a popover over the player, and iOS reports
// everything behind it hidden: map listed nothing and app_alert said no
// dialog was on screen, while the tip was the one thing on it. Both now say
// it is there, what it says, and a point outside it that closes it — the
// point the refusal behind it already offered. A share sheet is a popover
// too, with no outside to touch and its own controls in map, and is left
// alone, as is the player once the tip has gone. CHALLENGES 235.
func TestAPopoverIsSaid(t *testing.T) {
	note := popoverNote(iosCapture(t, "ios26-pocketcasts-player-popover.xml"))
	for _, want := range []string{"a popover is in front", `"Add bookmark — Keep the part you wanted"`, "app_tap at x "} {
		if !strings.Contains(note, want) {
			t.Errorf("the note %q does not say %q", note, want)
		}
	}
	for _, name := range []string{"ios26-pocketcasts-player.xml", "ios26-share-sheet-half.xml", "ios26-share-sheet-expanded.xml"} {
		if note := popoverNote(iosCapture(t, name)); note != "" {
			t.Errorf("%s: %q", name, note)
		}
	}
}

func TestAlertSaysAPopoverIsUp(t *testing.T) {
	d := &alertDriver{fakeDriver: fakeDriver{screens: []*uitree.Tree{iosCapture(t, "ios26-pocketcasts-player-popover.xml")}}}
	res, err := alertOn(t, d, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if got := textOf(res); !strings.Contains(got, "no dialog is on screen, but a popover is in front") {
		t.Errorf("read = %q", got)
	}
	if res.StructuredContent.(AlertView).Present {
		t.Error("a popover was reported as a dialog the alert endpoint can answer")
	}
	_, err = alertOn(t, d, map[string]interface{}{"action": "accept"})
	if mobiumerr.CodeOf(err) != mobiumerr.NoSuchAlert || !strings.Contains(err.Error(), "outside it") {
		t.Errorf("accept = %v", err)
	}
}
