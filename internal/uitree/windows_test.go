package uitree

import (
	"strings"
	"testing"
)

// The quest-*.xml fixtures are UiAutomator2's /source with
// enableMultiWindows on, from the Meta Spatial Simulator 207.0.0.83.331
// (Horizon OS, Android 14), 2026-10-25: every window, top first.

func windowedFixture(t *testing.T, file string) *Tree {
	t.Helper()
	tree := loadFixture(t, file)
	tree.MarkWindows()
	return tree
}

func mapLabels(tree *Tree) map[string]Entry {
	out := map[string]Entry{}
	for _, e := range tree.Map() {
		out[e.Label] = e
	}
	return out
}

// The shell's home screen is the bottom of three windows of its own, under
// the gear's and a 1x1 one. Read in front alone it mapped as nothing.
func TestWindowsHomeScreen(t *testing.T) {
	tree := windowedFixture(t, "quest-home.xml")
	got := mapLabels(tree)
	for _, want := range []string{"Settings", "Browser", "Gallery", "Store", "Files"} {
		e, ok := got[want]
		if !ok {
			t.Errorf("map has no %q: %v", want, got)
			continue
		}
		if e.Covered != "" {
			t.Errorf("%q marked covered by %q with nothing over it", want, e.Covered)
		}
	}
	if d := tree.Dialog(); d != nil {
		t.Errorf("the home screen read as a dialog: %s", d.Path)
	}
}

// A panel over the home screen is no dialog, and what it lies over is
// covered by its window — Gallery under the browser was tapped and reported
// done when the windows were read top first as one screen.
func TestWindowsPanelOverHome(t *testing.T) {
	plain := loadFixture(t, "quest-browser-over-home.xml")
	if plain.Dialog() == nil {
		t.Fatal("control: unmarked, the browser panel should read as a dialog, as it did on the device")
	}

	tree := windowedFixture(t, "quest-browser-over-home.xml")
	if d := tree.Dialog(); d != nil {
		t.Errorf("the browser panel read as a dialog: %s", d.Path)
	}
	if pkg := tree.Package(); pkg != "com.oculus.browser" {
		t.Errorf("Package() = %q, want the panel in front", pkg)
	}
	// map and locators see the app in front alone, as on a phone: the home
	// screen under the panel and MobiumApp's panel behind it, which sticks
	// out below and is under nothing, are neither listed nor found — a
	// touch there would only bring MobiumApp's panel forward.
	got := mapLabels(tree)
	for _, behind := range []string{"Gallery", "Ask for location"} {
		if e, ok := got[behind]; ok {
			t.Errorf("map lists %q, behind the browser: %+v", behind, e)
		}
		if n := (Locator{Kind: KindText, Value: behind, Exact: true}).Resolve(tree); len(n) != 0 {
			t.Errorf("text=%q resolves %d nodes behind the browser", behind, len(n))
		}
	}
	if e, ok := got["Reload"]; !ok || e.Covered != "" {
		t.Errorf("the browser's own Reload: %+v, want it mapped and clear", e)
	}
	// One Back, the browser's: MobiumApp's behind it made two.
	plainBacks := len((Locator{Kind: KindText, Value: "Back", Exact: true}).Resolve(plain)) +
		len((Locator{Kind: KindLabel, Value: "Back", Exact: true}).Resolve(plain))
	if plainBacks < 2 {
		t.Fatalf("control: unmarked, Back should be found in MobiumApp's window and the browser's, found %d", plainBacks)
	}
	backs := (Locator{Kind: KindLabel, Value: "Back", Exact: true}).Resolve(tree)
	backs = append(backs, (Locator{Kind: KindText, Value: "Back", Exact: true}).Resolve(tree)...)
	for _, n := range backs {
		if n.Package != "com.oculus.browser" {
			t.Errorf("Back resolved in %s's window", n.Package)
		}
	}
	// A node found some other way — a stale ref's path — is still refused,
	// naming the app in front.
	var gallery *Node
	tree.Walk(func(n *Node) bool {
		if n.Label == "Gallery" || n.Text == "Gallery" {
			gallery = n
		}
		return gallery == nil
	})
	if a := tree.AimAt(gallery); a.Blocker == nil || !strings.Contains(Describe(a.Blocker), "com.oculus.browser") {
		t.Errorf("Gallery under the browser: blocker %v, want the browser's window", a.Blocker)
	}
}

// One app's panel over another's is no dialog either: MobiumApp over the
// browser read as "a dialog — Popup —", its own heading.
func TestWindowsPanelOverPanel(t *testing.T) {
	tree := windowedFixture(t, "quest-mobiumapp-over-browser.xml")
	if d := tree.Dialog(); d != nil {
		t.Errorf("MobiumApp's panel read as a dialog: %s", d.Path)
	}
	if pkg := tree.Package(); pkg != "dev.mobium.mobiumapp" {
		t.Errorf("Package() = %q, want MobiumApp", pkg)
	}
}

// A window inside the panel under it is a dialog: the location prompt,
// whose window has MobiumApp's panel's very bounds.
func TestWindowsPermissionPromptIsADialog(t *testing.T) {
	tree := windowedFixture(t, "quest-permission-over-mobiumapp.xml")
	d := tree.Dialog()
	if d == nil {
		t.Fatal("the location prompt over MobiumApp is not a dialog")
	}
	if d.Package != "com.android.permissioncontroller" {
		t.Errorf("dialog is %s's window", d.Package)
	}
	if !strings.Contains(appDialogWords(d), "precise location") {
		t.Errorf("dialog says %q", appDialogWords(d))
	}
}

// The simulator's Settings stub opens over the browser panel, inside it.
func TestWindowsSettingsStubIsADialog(t *testing.T) {
	tree := windowedFixture(t, "quest-settings-stub-over-browser.xml")
	if d := tree.Dialog(); d == nil || d.Package != "com.oculus.panelapp.settings" {
		t.Errorf("the Settings stub: dialog %v, want its window", d)
	}
}

// Marking puts the windows bottom first and keeps every path as read.
func TestMarkWindowsKeepsPaths(t *testing.T) {
	tree := loadFixture(t, "quest-browser-over-home.xml")
	before := map[*Node]string{}
	tree.Walk(func(n *Node) bool { before[n] = n.Path; return true })
	first := tree.Root.Children[0]
	tree.MarkWindows()
	if last := tree.Root.Children[len(tree.Root.Children)-1]; last != first {
		t.Error("the window in front is not last after marking")
	}
	for _, w := range tree.Root.Children {
		if !w.Window {
			t.Errorf("window %s not marked", w.Path)
		}
	}
	tree.Walk(func(n *Node) bool {
		if before[n] != n.Path {
			t.Errorf("path changed: %q -> %q", before[n], n.Path)
		}
		return true
	})
}

func appDialogWords(d *Node) string {
	var b strings.Builder
	var walk func(*Node)
	walk = func(n *Node) {
		b.WriteString(n.Text + " " + n.Label + " ")
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(d)
	return b.String()
}

// What asks the screen for its largest list or its focused field walks the
// app in front. Morpho Market's catalog over the browser: the largest
// scrollable of every window was the browser's page, a swipe there only
// brought the browser forward, and every product below the fold was "no
// element matches".
func largestScrollable(walk func(func(*Node) bool)) *Node {
	var best *Node
	walk(func(n *Node) bool {
		if n.Scrollable && !n.Bounds.Empty() && (best == nil || area(n.Bounds) > area(best.Bounds)) {
			best = n
		}
		return true
	})
	return best
}

func TestWalkFrontKeepsToTheAppInFront(t *testing.T) {
	tree := windowedFixture(t, "quest-shop-over-browser.xml")
	if all := largestScrollable(tree.Walk); all == nil || all.Package == "dev.mobium.shop" {
		t.Fatalf("control: the largest scrollable of every window should be another app's, got %v", all)
	}
	front := largestScrollable(tree.WalkFront)
	if front == nil || front.TestID != "catalogList" {
		t.Errorf("largest scrollable in front: %v, want the catalog", front)
	}
	for _, n := range tree.AllFront() {
		if n.Package != "dev.mobium.shop" {
			t.Fatalf("AllFront holds %s's %s", n.Package, n.Path)
		}
	}
	// And where the app in front scrolls nothing, nothing: MobiumApp's
	// Interruption Demo over the browser, whose page scrolls.
	other := windowedFixture(t, "quest-mobiumapp-over-browser.xml")
	if n := largestScrollable(other.WalkFront); n != nil {
		t.Errorf("MobiumApp in front scrolls nothing, got %s's %s", n.Package, n.Path)
	}
}
