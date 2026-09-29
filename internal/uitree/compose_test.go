package uitree

import "testing"

// Jetpack Compose wraps an icon button in a long-clickable tooltip box one
// pixel off the button's own bounds. Each of Seal's three icon buttons
// mapped twice, once bare and once as a button; they are one target each, a
// button, tapped at the clickable node (CHALLENGES 176).
func TestComposeIconButtonsMapOnce(t *testing.T) {
	tree := loadFixture(t, "seal-home-uia2.xml")
	count := map[string]int{}
	for _, e := range tree.Map() {
		count[e.Label]++
		switch e.Label {
		case "Settings", "Running tasks", "Downloads":
			if e.Role != "button" || !e.Node.Clickable {
				t.Errorf("%s: role %q, clickable %v — want the button itself", e.Label, e.Role, e.Node.Clickable)
			}
		}
	}
	for _, l := range []string{"Settings", "Running tasks", "Downloads", "Paste", "Download"} {
		if count[l] != 1 {
			t.Errorf("%q mapped %d times, want once", l, count[l])
		}
	}
}

// Two neighbors that line up within a pixel stay two: only nested nodes
// merge.
func TestNearlyTheSameMergesOnlyNestedNodes(t *testing.T) {
	a := &Node{Bounds: Rect{0, 0, 100, 100}, Clickable: true, Displayed: true, Enabled: true, Text: "a"}
	b := &Node{Bounds: Rect{1, 1, 101, 101}, Clickable: true, Displayed: true, Enabled: true, Text: "b"}
	if got := collapseByBounds([]*Node{a, b}); len(got) != 2 {
		t.Errorf("siblings merged: %d entries", len(got))
	}
	b.Parent, b.Depth = a, 1
	if got := collapseByBounds([]*Node{a, b}); len(got) != 1 || got[0].node != b {
		t.Errorf("nested nodes: %+v", got)
	}
}

// A window that does not cover the screen is a dialog: Seal's Compose "User
// guide" and Android's permission prompt. An app's own window is not, even
// where the device reports the display without its navigation bar, smaller
// than the window (CHALLENGES 177).
func TestAFloatingWindowIsADialog(t *testing.T) {
	for file, want := range map[string]bool{
		"seal-dialog-uia2.xml":        true,
		"android-permission-uia2.xml": true,
		"seal-home-uia2.xml":          false,
		"settings-color-uia2.xml":     false,
		"fdroid-list-uia2.xml":        false,
		"launcher.xml":                false,
	} {
		if got := loadFixture(t, file).Dialog() != nil; got != want {
			t.Errorf("%s: a dialog = %v, want %v", file, got, want)
		}
	}
}
