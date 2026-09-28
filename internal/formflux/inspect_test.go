package formflux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// The fixtures are real hierarchies captured from real apps and shared with
// the uitree tests. They are used here for the one thing a fixture can
// honestly settle — whether a check fires on a screen somebody actually
// shipped, and how much noise it makes doing it.
//
// What a fixture cannot settle is whether the finding is *true*: that a
// control really is unreachable, not merely reported past the edge. Those go
// through the device test.
func load(t *testing.T, name string) *uitree.Tree {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "uitree", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	parse := uitree.ParseAndroid
	if strings.Contains(name, "ios") || strings.Contains(name, "springboard") {
		parse = uitree.ParseIOS
	}
	tree, err := parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// screenOf returns the bounding box of everything in the tree, which is the
// screen for a full-screen capture.
func screenOf(tr *uitree.Tree) uitree.Rect {
	var r uitree.Rect
	walk(tr.Root, func(n *uitree.Node) {
		if n.Bounds.X2 > r.X2 {
			r.X2 = n.Bounds.X2
		}
		if n.Bounds.Y2 > r.Y2 {
			r.Y2 = n.Bounds.Y2
		}
	})
	return r
}

func TestInspectIsQuietOnAScreenThatIsFine(t *testing.T) {
	// The whole design rests on this. A check that reports something on every
	// screen is a check nobody reads, and after a week it is a check nobody
	// runs. The launcher is a first-party screen laid out by people who had
	// the device in front of them; if this is noisy here, the thresholds are
	// wrong and not the screen.
	tr := load(t, "launcher-uia2.xml")
	got := Inspect(tr, screenOf(tr), 420, Android)
	for _, f := range got {
		t.Logf("  %s", f)
	}
	if len(got) > 3 {
		t.Errorf("%d findings on a first-party launcher — too noisy to be believed", len(got))
	}
}

func TestInspectFindsTheThingsItClaimsTo(t *testing.T) {
	// A positive control for each kind, built by hand, because the point is
	// to prove each check can fire at all. A check that has never returned
	// non-zero is not a passing check, it is an unrun one.
	screen := uitree.Rect{X2: 1080, Y2: 2400}
	root := &uitree.Node{Displayed: true, Bounds: screen, Class: "android.widget.FrameLayout"}
	root.Children = []*uitree.Node{
		{
			Displayed: true, Clickable: true, Enabled: true, Label: "Off the right edge",
			Bounds: uitree.Rect{X1: 900, Y1: 100, X2: 1300, Y2: 200}, Class: "android.widget.Button",
		},
		{
			Displayed: true, Clickable: true, Enabled: true, Label: "Tiny",
			Bounds: uitree.Rect{X1: 10, Y1: 300, X2: 40, Y2: 330}, Class: "android.widget.ImageButton",
		},
		{
			Displayed: true, Text: "A headline that did not quite…",
			Bounds: uitree.Rect{X1: 0, Y1: 400, X2: 1080, Y2: 460}, Class: "android.widget.TextView",
		},
		{
			Displayed: true, Clickable: true, Enabled: true,
			Bounds: uitree.Rect{X1: 0, Y1: 500, X2: 200, Y2: 700}, Class: "android.widget.ImageView",
		},
	}
	for _, c := range root.Children {
		c.Parent = root
	}

	got := Inspect(&uitree.Tree{Root: root}, screen, 420, Android)
	kinds := map[Kind]bool{}
	for _, f := range got {
		kinds[f.Kind] = true
		t.Logf("  %s", f)
	}
	for _, want := range []Kind{KindOverflow, KindTinyTarget, KindTruncated, KindUnlabeled} {
		if !kinds[want] {
			t.Errorf("%s never fired", want)
		}
	}
}

func TestATruncationMustBeAtTheEnd(t *testing.T) {
	// "Wait… what?" is punctuation, not a cut label, and a lone "…" is an
	// overflow menu. Getting this wrong makes the check fire on prose.
	for _, s := range []string{"Wait… what?", "…", "...", "", "no ellipsis here"} {
		n := &uitree.Node{Text: s}
		if got := truncatedText(n); got != "" {
			t.Errorf("%q was read as truncated", s)
		}
	}
	for _, s := range []string{"Cut off here…", "Cut off here..."} {
		n := &uitree.Node{Text: s}
		if truncatedText(n) == "" {
			t.Errorf("%q was not read as truncated", s)
		}
	}
}

func TestAPasswordNeverReachesAFinding(t *testing.T) {
	// Findings are printed, logged and pasted into issues. CHALLENGES 43 was
	// a password reaching `map` output; this is the same leak through a new
	// door, so it is closed before the door is opened rather than after.
	tr := load(t, "aegis-password-uia2.xml")
	for _, f := range Inspect(tr, screenOf(tr), 420, Android) {
		if strings.Contains(f.Label, "hunter2") || strings.Contains(f.Detail, "hunter2") {
			t.Errorf("a password field's text reached a finding: %s", f)
		}
	}
	// And directly, so the test cannot pass merely because the fixture has no
	// findings at all.
	n := &uitree.Node{Displayed: true, Password: true, Text: "hunter2",
		Bounds: uitree.Rect{X2: 100, Y2: 100}, Class: "android.widget.EditText"}
	if got := describe(n); strings.Contains(got, "hunter2") {
		t.Errorf("describe leaked a password: %q", got)
	}
	if got := locator(n); strings.Contains(got, "hunter2") {
		t.Errorf("locator leaked a password: %q", got)
	}
	if got := truncatedText(n); got != "" {
		t.Errorf("truncatedText read a password field: %q", got)
	}
}

func TestTouchThresholdFollowsDensity(t *testing.T) {
	// This is why the check belongs in this package: the density formflux
	// changes is the density the threshold is computed from. A 30px control
	// is fine at 160dpi and too small at 560.
	if got := minTouchPixels(160, Android); got != 48 {
		t.Errorf("at 160dpi, 48dp should be 48px, got %d", got)
	}
	if got := minTouchPixels(560, Android); got != 168 {
		t.Errorf("at 560dpi, 48dp should be 168px, got %d", got)
	}
	// Refuse rather than invent one.
	if got := minTouchPixels(0, Android); got != 0 {
		t.Errorf("with no density there is no conversion, got %d", got)
	}
	// iOS is deliberately unchecked: mobium reports iOS bounds in device
	// pixels, so a 44pt threshold would be wrong by the scale factor. A
	// wrong threshold is worse than no threshold.
	if got := minTouchPixels(0, IOS); got != 0 {
		t.Errorf("iOS touch targets should not be checked yet, got a threshold of %d", got)
	}
}

func TestCompareReportsWhatIsNewRatherThanEverythingWrong(t *testing.T) {
	// "What breaks when the screen gets smaller" is the question. "What is
	// wrong everywhere" is a different and much longer answer.
	base := []Finding{{Kind: KindTinyTarget, Label: "Always small"}}
	narrow := []Finding{
		{Kind: KindTinyTarget, Label: "Always small"},
		{Kind: KindOverflow, Label: "Only when narrow"},
	}
	added := Compare(base, narrow)
	if len(added) != 1 || added[0].Label != "Only when narrow" {
		t.Errorf("got %v, want just the new one", added)
	}
	if len(Compare(narrow, base)) != 0 {
		t.Error("a screen with fewer findings reported additions")
	}
}

func TestInspectSurvivesAnEmptyTree(t *testing.T) {
	if got := Inspect(nil, uitree.Rect{}, 420, Android); got != nil {
		t.Errorf("a nil tree produced findings: %v", got)
	}
	if got := Inspect(&uitree.Tree{}, uitree.Rect{}, 420, Android); got != nil {
		t.Errorf("an empty tree produced findings: %v", got)
	}
}

// A row cut off by the bottom of its list is not a tiny target: Android
// clips a child's bounds to its scroll container, so the sliver still
// showing is all it reports. Captured on a Pixel 9 Pro Fold emulator's open
// screen, where Settings' "Sound & vibration" row — 215px like its
// neighbors — read 52px at the list's edge.
func TestARowCutByItsListIsNotATinyTarget(t *testing.T) {
	tr := load(t, "settings-fold-open-uia2.xml")
	var cut *uitree.Node
	for _, n := range tr.All() {
		if n.Path == "0/0/0/0/1/0/0/0/0/0/6" {
			cut = n
		}
	}
	// The fixture has to hold the case, or the assertion below proves nothing.
	if cut == nil || !cut.Clickable || cut.Bounds.Height() >= minTouchPixels(390, Android) {
		t.Fatalf("the fixture no longer has the cut row: %+v", cut)
	}
	for _, f := range Inspect(tr, screenOf(tr), 390, Android) {
		if f.Kind == KindTinyTarget && f.Path == cut.Path {
			t.Errorf("a row cut by its list was reported: %s", f.Detail)
		}
	}

	// And a short row that is not at an edge is still one: the same row
	// moved up into the middle of the list is reported.
	cut.Bounds.Y1, cut.Bounds.Y2 = 1500, 1552
	found := false
	for _, f := range Inspect(tr, screenOf(tr), 390, Android) {
		found = found || (f.Kind == KindTinyTarget && f.Path == cut.Path)
	}
	if !found {
		t.Error("a 52px row in the middle of the list was not reported: the exemption hides real ones")
	}
}
