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
		"its center: it is touched above the tab bar, on screen (CHALLENGES 238)",
	"ios26-pocketcasts-search.xml": "each search result is an unlabeled full-width button with a labeled one, " +
		"the podcast's name, laid over its middle: the unlabeled one is aimed at its clear edge; and the " +
		"results' filter chips sit over Discover's category chips (CHALLENGES 257)",
	"ios26-pocketcasts-search-over-discover.xml": "the same on the iPhone: Comedy's center is under the " +
		"Podcasts filter chip, and Fiction is under Episodes from edge to edge (CHALLENGES 257)",
	"ios26-pocketcasts-search-failed.xml": "Search Failed's Try Again button lies over the first row of " +
		"Discover behind it (CHALLENGES 257)",
	"ios26-kiwix-catalog.xml": "the last card runs under the Library's tab bar, and its center is under " +
		"Downloads by a point and a half: it is touched above the tab bar (CHALLENGES 257)",
	"ios26-pocketcasts-episode-sheet.xml": "a two-point divider runs across the row of buttons through Play's " +
		"center, and Play is touched just above it (CHALLENGES 238)",
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

// Something that is not a control over a target's center is avoided where
// the rest of the target is clear of it, rather than noted as possibly
// taking the touch: Pocket Casts' episode sheet draws a two-point divider
// through Play's center, and every tap on Play said the divider "may take
// the touch" while it played the episode. And a clear point is on the
// screen: below its bottom edge nothing is drawn, so every point there
// looked clear, and an Ice Cubes post running behind the tab bar was aimed
// 81 points below the screen. CHALLENGES 238.
func TestANonControlOverTheCenterIsAvoided(t *testing.T) {
	sheet := loadTree(t, "ios26-pocketcasts-episode-sheet.xml")
	var play *Node
	sheet.Walk(func(n *Node) bool {
		if n.Label == "Play" && Actionable(n) {
			play = n
		}
		return play == nil
	})
	if play == nil {
		t.Fatal("Play is not in the sheet")
	}
	a := sheet.AimAt(play)
	if a.Over != nil || !a.Moved || a.CenterCover == nil || IsControl(a.CenterCover) {
		t.Errorf("Play: %+v, want moved off the divider with nothing over the point", a)
	}
	if len(sheet.DrawnOver(play, a.X, a.Y)) != 0 || !contains(play.Bounds, a.X, a.Y) {
		t.Errorf("Play is touched at %d,%d, which is not a clear point of it", a.X, a.Y)
	}

	timeline := loadTree(t, "ios26-icecubes-timeline.xml")
	screen := timeline.Root.Bounds
	timeline.Walk(func(n *Node) bool {
		if Actionable(n) && !n.Bounds.Empty() {
			if a := timeline.AimAt(n); a.Moved && !contains(screen, a.X, a.Y) {
				t.Errorf("%q is touched at %d,%d, off the screen %v", describe(n), a.X, a.Y, screen)
			}
		}
		return true
	})
}

// A control inside a plain view laid over the target covers it. Pocket Casts'
// search results are a plain view over Discover, and their filter chips sit
// where Discover's category chips are: a tap on Comedy pressed Podcasts and
// reported Comedy tapped, because only the plain view, the outermost cover,
// was looked at; then, moved off its center into the gap beside Podcasts, a
// tap pressed Episodes, the strip being the chips'. On the iPhone, which the
// capture is from. CHALLENGES 257.
func TestAControlInsideAPlainCoverCovers(t *testing.T) {
	tree := loadIOS(t, "ios26-pocketcasts-search-over-discover.xml")
	chip := func(label string) *Node {
		var found *Node
		tree.Walk(func(n *Node) bool {
			if found == nil && n.Class == "XCUIElementTypeButton" && n.Label == label {
				found = n
			}
			return found == nil
		})
		if found == nil {
			t.Fatalf("no %q chip in the capture", label)
		}
		return found
	}
	// Not moved into the gap beside Podcasts either: the gap is the chip
	// strip's, and the touch there went to Episodes on the iPhone.
	for _, c := range []struct{ target, cover string }{{"Comedy", "Podcasts"}, {"All Categories", "Top Results"}} {
		if a := tree.AimAt(chip(c.target)); a.Blocker == nil || a.Blocker.Label != c.cover {
			t.Errorf("%s aimed %+v, want blocked by %s and its strip", c.target, a, c.cover)
		}
	}
	if a := tree.AimAt(chip("Fiction")); a.Blocker == nil || a.Blocker.Label != "Episodes" {
		t.Errorf("Fiction aimed %+v, want blocked by Episodes", a)
	}
}

// A control's container is its touch area, not its frame: in a strip of
// chips every point is the nearest chip's. Measured on Pocket Casts' Discover
// chips on the simulator — across a six-point gap, ten points below a chip,
// never two points above the strip. So a covered target is not moved into a
// strip laid over it: the rows under "Search Failed" lie under Try Again's,
// and a search result under the mini player's, whose old aim was inside the
// player. CHALLENGES 257.
func TestAStripOverATargetIsNotClear(t *testing.T) {
	failed := loadIOS(t, "ios26-pocketcasts-search-failed.xml")
	for _, e := range failed.Map() {
		if e.Label != "Machine Gods NPR" {
			continue
		}
		if a := failed.AimAt(e.Node); a.Blocker == nil || a.Blocker.Label != "Try Again" {
			t.Errorf("%s under Search Failed aimed %+v, want blocked by Try Again", e.Label, a)
		}
	}
	search := loadIOS(t, "ios26-pocketcasts-search.xml")
	for _, e := range search.Map() {
		if e.Node.Bounds != (Rect{X1: 0, Y1: 766, X2: 402, Y2: 846}) {
			continue
		}
		if a := search.AimAt(e.Node); a.Blocker == nil {
			t.Errorf("the last result, under the mini player and the tab bar, aimed at (%d, %d)", a.X, a.Y)
		}
	}
}
