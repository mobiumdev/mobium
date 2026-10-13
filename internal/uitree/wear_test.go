package uitree

import (
	"os"
	"testing"
)

func wearTree(t *testing.T, name string) *Tree {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := ParseAndroid(raw)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// A Wear OS chip is a Button whose own text repeats its title's TextView,
// so text= found the control and its title, two on every row of Settings.
// Captured from a Wear OS 5.1 emulator. CHALLENGES 315.
func TestAWearChipIsOneMatch(t *testing.T) {
	tree := wearTree(t, "wear-settings-uia2.xml")
	for _, s := range []string{`text="Display"`, `text=Display`, `text=Gestures,role=button`} {
		loc, err := ParseLocator(s)
		if err != nil {
			t.Fatal(err)
		}
		got := loc.Resolve(tree)
		if len(got) != 1 || got[0].Class != "android.widget.Button" {
			t.Errorf("%s resolved to %d nodes, want the chip's Button alone", s, len(got))
		}
	}
}

// A Wear OS toggle chip holds its switch and the switch's container, both
// checkable and neither clickable, with the chip's name: map listed each
// switch on Gestures three times. CHALLENGES 316.
func TestAWearToggleChipIsOneSwitch(t *testing.T) {
	tree := wearTree(t, "wear-gestures-uia2.xml")
	n := 0
	for _, e := range tree.Map() {
		if e.Label != "Tilt-to-wake" {
			continue
		}
		n++
		if e.Role != "switch" || e.Checked == nil || !*e.Checked || !e.Node.Clickable {
			t.Errorf("Tilt-to-wake: role %q, checked %v, clickable %v", e.Role, e.Checked, e.Node.Clickable)
		}
	}
	if n != 1 {
		t.Errorf("map lists Tilt-to-wake %d times, want once", n)
	}
	loc, _ := ParseLocator(`text="Tilt-to-wake",role=switch`)
	if got := loc.Resolve(tree); len(got) != 1 || !got[0].Clickable {
		t.Errorf("the switch's locator resolved to %d nodes", len(got))
	}
}

// Wear OS's own alert fills the watch's screen, so it is no floating
// window, and UiAutomator2's alert endpoint does not know it. CHALLENGES 317.
func TestAWearAlertIsADialog(t *testing.T) {
	d := wearTree(t, "wear-alert-uia2.xml").Dialog()
	if d == nil {
		t.Fatal("no dialog found")
	}
	ok, cancel := false, false
	var walk func(*Node)
	walk = func(n *Node) {
		ok = ok || n.Label == "OK" && n.Clickable
		cancel = cancel || n.Label == "Cancel" && n.Clickable
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(d)
	if !ok || !cancel {
		t.Errorf("the dialog holds OK %v and Cancel %v, want both", ok, cancel)
	}
	if wearTree(t, "wear-gestures-uia2.xml").Dialog() != nil {
		t.Error("a Wear screen with no dialog up has one")
	}
}
