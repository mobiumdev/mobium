package uitree

import (
	"os"
	"strings"
	"testing"
)

func loadTree(t *testing.T, name string) *Tree {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var tree *Tree
	if strings.Contains(name, "ios") {
		tree, err = ParseIOS(data)
	} else {
		tree, err = ParseAndroid(data)
	}
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return tree
}

// coverEffects counts what AimAt would do to every target map offers.
func coverEffects(tree *Tree) (blocked, moved, over int) {
	tree.Walk(func(n *Node) bool {
		if !Actionable(n) || n.Bounds.Empty() {
			return true
		}
		switch a := tree.AimAt(n); {
		case a.Blocker != nil:
			blocked++
		case a.Moved:
			moved++
		case a.Over != nil:
			over++
		}
		return true
	})
	return
}

// On every captured screen that is not built to be covered — three real
// apps, the launcher, SpringBoard, Settings, alerts and the keyboard — the
// rule changes nothing: no refusal, no moved aim, no note. Those screens are
// full of later full-screen layers, a transparent XCUIElementTypeWindow or
// Other on iOS and the launcher's drag layer on Android, which the first
// version reported over nearly every target (CHALLENGES 115). The obstruction
// screens are the positive control: the same count there is not zero.
// coveredCaptures are real screens captured with something drawn over their
// targets, so they must show a cover as the obstruction screens do, and why.
var coveredCaptures = map[string]string{
	"ios26-notification-center-group.xml": "an expanded group in Notification Center stacks its oldest " +
		"notifications under each other, and Clear sits over Show less: a tap of Show less was aimed at a " +
		"clear point of it, and collapsed the group",
	"ios26-icecubes-timeline.xml": "the second post runs down behind iOS 26's tab bar, which is drawn over " +
		"its center",
	"ios26-icecubes-display-settings.xml": "Ice Cubes pins a sample post at the top of Display Settings and " +
		"scrolls the settings under it: Tint Color is wholly behind the post's button, and Theme's center is",
}

func TestTheCoverRuleLeavesOrdinaryScreensAlone(t *testing.T) {
	ents, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range ents {
		name := e.Name()
		if !strings.HasSuffix(name, ".xml") {
			continue
		}
		b, m, o := coverEffects(loadTree(t, name))
		if strings.HasPrefix(name, "obstruction-") || coveredCaptures[name] != "" {
			if b+m+o == 0 {
				t.Errorf("%s: the screen built to be covered showed no cover — the test cannot fail", name)
			}
			continue
		}
		checked++
		if b+m+o != 0 {
			t.Errorf("%s: blocked=%d moved=%d over=%d on a screen with nothing drawn over its targets", name, b, m, o)
		}
	}
	if checked == 0 {
		t.Fatal("no ordinary screens were checked")
	}
}

func byID(t *testing.T, tree *Tree, id string) *Node {
	t.Helper()
	var found *Node
	tree.Walk(func(n *Node) bool {
		if n.TestID == id || strings.HasSuffix(n.TestID, ":id/"+id) {
			found = n
			return false
		}
		return true
	})
	if found == nil {
		t.Fatalf("%s is not in the tree", id)
	}
	return found
}

type coverCase struct {
	target  string
	blocker string // Describe of the control over all of it, or ""
	moved   bool   // aimed at a clear point
	over    string // Describe of a non-control over the point, or ""
}

// checkCovers asserts each case of MobiumApp's Obstruction Demo as measured:
// where a tap really landed on the device decided every expectation here.
func checkCovers(t *testing.T, fixture string, cases []coverCase) {
	tree := loadTree(t, fixture)
	for _, c := range cases {
		n := byID(t, tree, c.target)
		a := tree.AimAt(n)
		desc := func(x *Node) string {
			if x == nil {
				return ""
			}
			return Describe(x)
		}
		if got := desc(a.Blocker); got != c.blocker {
			t.Errorf("%s %s: blocker %q, want %q", fixture, c.target, got, c.blocker)
		}
		if a.Moved != c.moved {
			t.Errorf("%s %s: moved=%v, want %v", fixture, c.target, a.Moved, c.moved)
		}
		if got := desc(a.Over); got != c.over {
			t.Errorf("%s %s: over %q, want %q", fixture, c.target, got, c.over)
		}
		if c.moved {
			if !contains(n.Bounds, a.X, a.Y) {
				t.Errorf("%s %s: moved aim (%d,%d) is off the target %s", fixture, c.target, a.X, a.Y, n.Bounds)
			}
			for _, o := range tree.DrawnOver(n, a.X, a.Y) {
				if IsControl(o) {
					t.Errorf("%s %s: moved aim (%d,%d) is still under %q", fixture, c.target, a.X, a.Y, Describe(o))
				}
			}
		}
	}
}

func TestObstructionDemoAndroid(t *testing.T) {
	checkCovers(t, "obstruction-uia2.xml", []coverCase{
		{target: "fullTarget", blocker: "full cover"},
		{target: "halfTarget", moved: true},
		{target: "edgeTarget"},
		{target: "passTarget", over: "pass-through cover"},
		{target: "plainTarget", over: "plain cover"},
		// Hidden from accessibility and still in UiAutomator2's tree, as a
		// clickable ViewGroup with no name.
		{target: "hiddenTarget", blocker: "hidden overlay"},
		{target: "scrimTarget", blocker: "scrim"},
	})
}

func TestObstructionDemoIOS(t *testing.T) {
	checkCovers(t, "obstruction-ios.xml", []coverCase{
		{target: "fullTarget", blocker: "full cover"},
		{target: "halfTarget", moved: true},
		{target: "edgeTarget"},
		{target: "passTarget", over: "pass-through cover"},
		{target: "plainTarget", over: "plain cover"},
		// The blind spot, asserted so it stays known: an overlay hidden from
		// accessibility is not in WebDriverAgent's tree at all, and the
		// target under it still reads visible. The tap lands on the overlay.
		{target: "hiddenTarget"},
		{target: "scrimTarget", blocker: "scrim"},
	})
}
