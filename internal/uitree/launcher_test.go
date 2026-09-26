package uitree

import (
	"strings"
	"testing"
)

// launcher.xml is a verbatim `uiautomator dump` from a Pixel 7 AVD running
// Android 15 (1080x2400, 420dpi) on the Nexus launcher home screen. It is here
// because the hand-written fixture could not have produced any of the three
// behaviors below — each was found by running against the emulator.

func TestLauncherMap(t *testing.T) {
	tree := loadFixture(t, "launcher.xml")
	var lines []string
	for _, e := range tree.Map() {
		lines = append(lines, e.Line())
	}

	want := []string{
		"@e1 workspace (list)",
		"@e2 At a glance (button)",
		"@e3 Fri, Sep 11 (button)",
		"@e4 Gmail (button)",
		"@e5 Photos (button)",
		"@e6 YouTube (button)",
		"@e7 Phone (button)",
		"@e8 Messages (button)",
		"@e9 Chrome (button)",
		"@e10 Predicted app: Gmail (button)",
		"@e11 Google search (button)",
		"@e12 Google app (button)",
		"@e13 Voice search (button)",
		"@e14 Google Lens (button)",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("launcher map:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestLauncherAppIconsAreButtonsNotLinks(t *testing.T) {
	// Every app icon on the launcher is a clickable TextView. Treating that
	// as a link — the web instinct — mislabels the entire home screen.
	tree := loadFixture(t, "launcher.xml")
	for _, e := range tree.Map() {
		if e.Role == "link" {
			t.Errorf("%s (%s) was labeled a link", e.Ref, e.Label)
		}
	}

	var gmail *Node
	for _, n := range tree.All() {
		if n.Label == "Gmail" {
			gmail = n
		}
	}
	if gmail == nil {
		t.Fatal("Gmail icon not found in the launcher dump")
	}
	if gmail.ShortClass() != "TextView" || !gmail.Clickable {
		t.Fatalf("precondition: Gmail icon is %s clickable=%v", gmail.ShortClass(), gmail.Clickable)
	}
	if !HasRole(gmail, "button") {
		t.Error("a clickable TextView app icon should satisfy role=button")
	}
}

func TestLauncherCollapsesStackedWidgetNodes(t *testing.T) {
	// The "At a glance" widget is a long-clickable ViewPager wrapping a
	// clickable ViewGroup on identical bounds. Before collapsing, this mapped
	// as two entries covering one rectangle, and the two disagreed about the
	// label: the outer one knew it was "At a glance", the inner one had
	// borrowed "Fri, Sep 11" from the date inside it.
	tree := loadFixture(t, "launcher.xml")

	widgetRect := Rect{83, 167, 997, 438}
	var atGlance []Entry
	for _, e := range tree.Map() {
		if e.Bounds == widgetRect {
			atGlance = append(atGlance, e)
		}
	}
	if len(atGlance) != 1 {
		t.Fatalf("widget rect produced %d entries, want 1: %+v", len(atGlance), atGlance)
	}
	if atGlance[0].Label != "At a glance" {
		t.Errorf("collapsed entry labeled %q, want the widget's own name", atGlance[0].Label)
	}
	// The kept node must be the one a tap dispatches to.
	if !atGlance[0].Node.Clickable {
		t.Error("collapsed entry kept a node that is not clickable")
	}
}

func TestLauncherEveryLocatorIsUniqueAndDurable(t *testing.T) {
	tree := loadFixture(t, "launcher.xml")
	for _, e := range tree.Map() {
		matches := e.Locator.Resolve(tree)
		if len(matches) != 1 || matches[0] != e.Node {
			t.Errorf("%s (%s) derived %s resolving to %d nodes",
				e.Ref, e.Label, e.Locator, len(matches))
		}
		// A path locator means nothing durable was available. On a real home
		// screen every element has an id or an accessibility label, so a path
		// here is a regression in Derive.
		if e.Locator.Kind == KindPath {
			t.Errorf("%s (%s) fell back to a sibling path", e.Ref, e.Label)
		}
	}
}

func TestLauncherNoDuplicateRectangles(t *testing.T) {
	tree := loadFixture(t, "launcher.xml")
	seen := map[Rect]string{}
	for _, e := range tree.Map() {
		if prev, dup := seen[e.Bounds]; dup {
			t.Errorf("%s (%s) shares a rectangle with %s", e.Ref, e.Label, prev)
		}
		seen[e.Bounds] = e.Ref
	}
}
