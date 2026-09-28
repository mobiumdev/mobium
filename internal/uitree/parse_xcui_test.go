package uitree

import (
	"os"
	"strings"
	"testing"
)

// ios-springboard.xml is a verbatim WebDriverAgent /source capture from an
// iPhone 17 Pro simulator running iOS 26.5. It replaced a hand-written fixture
// that had agreed with two wrong heuristics: the real one maps zero elements
// under the original actionability rule, which is how the bug was found.

func loadIOS(t *testing.T, name string) *Tree {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tree, err := ParseIOS(data)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return tree
}

func TestParseIOSGeometry(t *testing.T) {
	tree := loadIOS(t, "ios-springboard.xml")

	var fitness *Node
	for _, n := range tree.All() {
		if n.TestID == "Fitness" {
			fitness = n
		}
	}
	if fitness == nil {
		t.Fatal("the Fitness icon is missing from the capture")
	}
	// WDA reports x/y/width/height separately; Android reports a bounds
	// string. Both must land in the same Rect.
	if fitness.Bounds.Width() <= 0 || fitness.Bounds.Height() <= 0 {
		t.Errorf("bounds = %v", fitness.Bounds)
	}
	if fitness.Bounds.X1 != 28 || fitness.Bounds.Y1 != 88 {
		t.Errorf("origin = (%d,%d), want (28,88)", fitness.Bounds.X1, fitness.Bounds.Y1)
	}
}

func TestParseIOSFieldMapping(t *testing.T) {
	tree := loadIOS(t, "ios-springboard.xml")
	var utilities *Node
	for _, n := range tree.All() {
		if n.TestID == "Utilities" {
			utilities = n
		}
	}
	if utilities == nil {
		t.Fatal("the Utilities folder is missing from the capture")
	}
	// name -> TestID, label -> Label.
	if utilities.Label != "Utilities folder" {
		t.Errorf("label = %q", utilities.Label)
	}
	if utilities.Class != "XCUIElementTypeIcon" {
		t.Errorf("class = %q", utilities.Class)
	}
}

func TestIOSActionabilityUsesAccessible(t *testing.T) {
	// The original rule whitelisted element types and mapped zero elements,
	// because the home screen is XCUIElementTypeIcon and that was not on the
	// list. `accessible` is the platform's own answer and needs no list.
	tree := loadIOS(t, "ios-springboard.xml")
	entries := tree.Map()
	if len(entries) == 0 {
		t.Fatal("the springboard mapped no elements")
	}

	var labels []string
	for _, e := range entries {
		labels = append(labels, e.Label)
	}
	joined := strings.Join(labels, ",")
	for _, want := range []string{"Fitness", "Watch", "Contacts", "Files"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q is not in the map: %v", want, labels)
		}
	}
}

func TestIOSIconIsAButton(t *testing.T) {
	// An app icon is the commonest tappable thing on iOS and is neither
	// XCUIElementTypeButton nor marked clickable by any attribute.
	if !HasRole(&Node{Class: "XCUIElementTypeIcon"}, "button") {
		t.Error("XCUIElementTypeIcon does not satisfy role=button")
	}
	tree := loadIOS(t, "ios-springboard.xml")
	for _, e := range tree.Map() {
		if e.Label == "Fitness" && e.Role != "button" {
			t.Errorf("the Fitness icon has role %q, want button", e.Role)
		}
	}
}

func TestIOSStaticTextIsNotActionable(t *testing.T) {
	// Content types are excluded even when the platform marks them
	// accessible, or the clock and every label become tappable rows.
	tree := loadIOS(t, "ios-springboard.xml")
	for _, e := range tree.Map() {
		if e.Node.Class == "XCUIElementTypeStaticText" {
			t.Errorf("%s (%s) is static text and should not be mapped", e.Ref, e.Label)
		}
	}
}

func TestIOSTextViewIsNotAnInput(t *testing.T) {
	// iOS renders read-only prose as TextView — Safari's privacy sheet has
	// three paragraphs of it — so role=input must not match, or an agent
	// tries to type into an explainer.
	if HasRole(&Node{Class: "XCUIElementTypeTextView"}, "input") {
		t.Error("XCUIElementTypeTextView matched role=input")
	}
	for _, class := range []string{
		"XCUIElementTypeTextField",
		"XCUIElementTypeSecureTextField",
		"XCUIElementTypeSearchField",
	} {
		if !HasRole(&Node{Class: class}, "input") {
			t.Errorf("%s does not satisfy role=input", class)
		}
	}
}

func TestIOSRolesDoNotCollideWithAndroid(t *testing.T) {
	// TextView is a read-only label on Android and a text view on iOS;
	// neither is an input, but they reach that answer through different
	// tables and must not be merged.
	if HasRole(&Node{Class: "android.widget.TextView"}, "input") {
		t.Error("an Android TextView was classified as an input")
	}
	// iOS types are exact, so a near miss must not match by suffix.
	if HasRole(&Node{Class: "XCUIElementTypeButtonBar"}, "button") {
		t.Error("XCUIElementTypeButtonBar matched role=button by suffix")
	}
}

func TestIsIOS(t *testing.T) {
	if !IsIOS(&Node{Class: "XCUIElementTypeButton"}) {
		t.Error("an XCUI class was not recognized as iOS")
	}
	if IsIOS(&Node{Class: "android.widget.Button"}) {
		t.Error("an Android class was recognized as iOS")
	}
}

func TestIOSLocatorsAreUniqueAndDurable(t *testing.T) {
	tree := loadIOS(t, "ios-springboard.xml")
	for _, e := range tree.Map() {
		matches := e.Locator.Resolve(tree)
		if len(matches) != 1 || matches[0] != e.Node {
			t.Errorf("%s (%s) derived %s resolving to %d nodes",
				e.Ref, e.Label, e.Locator, len(matches))
		}
	}
}

func TestParseIOSRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "not xml"} {
		if _, err := ParseIOS([]byte(bad)); err == nil {
			t.Errorf("ParseIOS(%q) did not error", bad)
		}
	}
}

func TestTreeScale(t *testing.T) {
	// WebDriverAgent reports points; mobium's coordinate space is device
	// pixels, which is also what screenshots are in. On an iPhone 17 Pro the
	// factor is exactly 3.
	tree := loadIOS(t, "ios-springboard.xml")

	var before Rect
	for _, n := range tree.All() {
		if n.TestID == "Fitness" {
			before = n.Bounds
		}
	}
	if before.Empty() {
		t.Fatal("the Fitness icon has no bounds before scaling")
	}

	tree.Scale(3)

	var after Rect
	for _, n := range tree.All() {
		if n.TestID == "Fitness" {
			after = n.Bounds
		}
	}
	want := Rect{before.X1 * 3, before.Y1 * 3, before.X2 * 3, before.Y2 * 3}
	if after != want {
		t.Errorf("scaled bounds = %v, want %v", after, want)
	}
}

func TestTreeScaleScalesTheRoot(t *testing.T) {
	// app_swipe derives its start and end from the root's size, so a root
	// left in points sends the gesture to a third of the intended place.
	tree := loadIOS(t, "ios-springboard.xml")
	before := tree.Root.Bounds
	if before.Empty() {
		t.Fatal("the root has no bounds to scale")
	}

	tree.Scale(3)

	if got, want := tree.Root.Bounds.Width(), before.Width()*3; got != want {
		t.Errorf("root width = %d, want %d", got, want)
	}
	if got, want := tree.Root.Bounds.Height(), before.Height()*3; got != want {
		t.Errorf("root height = %d, want %d", got, want)
	}
}

func TestTreeScaleIsANoOpAtOne(t *testing.T) {
	tree := loadIOS(t, "ios-springboard.xml")
	var before []Rect
	for _, n := range tree.All() {
		before = append(before, n.Bounds)
	}

	tree.Scale(1)

	for i, n := range tree.All() {
		if n.Bounds != before[i] {
			t.Fatalf("node %d changed at scale 1: %v -> %v", i, before[i], n.Bounds)
		}
	}
}

func TestTreeScaleIgnoresNonsenseFactors(t *testing.T) {
	// A zero or negative factor would collapse every element to a point,
	// which reads as "nothing is on screen" rather than as an error.
	for _, factor := range []float64{0, -1} {
		tree := loadIOS(t, "ios-springboard.xml")
		var before Rect
		for _, n := range tree.All() {
			if n.TestID == "Fitness" {
				before = n.Bounds
			}
		}
		tree.Scale(factor)
		for _, n := range tree.All() {
			if n.TestID == "Fitness" && n.Bounds != before {
				t.Errorf("factor %v changed bounds to %v", factor, n.Bounds)
			}
		}
	}
}

func TestScaledTreeStillMapsTheSameElements(t *testing.T) {
	// Scaling must not disturb actionability or locator derivation.
	plain := loadIOS(t, "ios-springboard.xml")
	scaled := loadIOS(t, "ios-springboard.xml")
	scaled.Scale(3)

	a, b := plain.Map(), scaled.Map()
	if len(a) != len(b) {
		t.Fatalf("scaling changed the map size: %d -> %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Line() != b[i].Line() {
			t.Errorf("entry %d: %q -> %q", i, a[i].Line(), b[i].Line())
		}
	}
}

// iOS puts a control's state in `value`, and `value` is not always text.
//
// A Switch reports "0" or "1"; React Native's custom controls report the
// phrase VoiceOver speaks, "checkbox, checked". Both were being used as the
// element's text, which meant `map` printed `@e7 0 (switch)` for a control
// labeled "Dark mode" — true of every iOS switch, not only ours. CHALLENGES 65.
func TestIOSValueIsNotAlwaysText(t *testing.T) {
	cases := []struct {
		class, value               string
		wantText                   string
		wantCheckable, wantChecked bool
		wantRole                   string
	}{
		{"XCUIElementTypeSwitch", "1", "", true, true, ""},
		{"XCUIElementTypeSwitch", "0", "", true, false, ""},
		{"XCUIElementTypeOther", "checkbox, checked", "", true, true, "checkbox"},
		{"XCUIElementTypeOther", "checkbox, unchecked", "", true, false, "checkbox"},
		{"XCUIElementTypeOther", "radio button, checked", "", true, true, "radio"},
		// A text field's value genuinely is its text, which is why the parser
		// reads it at all — that case must keep working.
		{"XCUIElementTypeTextField", "hello@example.com", "hello@example.com", false, false, ""},
		// And prose that merely mentions a checkbox is not one.
		{"XCUIElementTypeStaticText", "tick the checkbox, then continue", "tick the checkbox, then continue", false, false, ""},
	}
	for _, tc := range cases {
		if got := textValue(tc.class, tc.value, ""); got != tc.wantText {
			t.Errorf("textValue(%s, %q) = %q, want %q", tc.class, tc.value, got, tc.wantText)
		}
		checkable, checked := checkedState(tc.class, tc.value)
		if checkable != tc.wantCheckable || checked != tc.wantChecked {
			t.Errorf("checkedState(%s, %q) = %v,%v want %v,%v",
				tc.class, tc.value, checkable, checked, tc.wantCheckable, tc.wantChecked)
		}
		if got := declaredRole(tc.value); got != tc.wantRole {
			t.Errorf("declaredRole(%q) = %q, want %q", tc.value, got, tc.wantRole)
		}
	}
}

// "radio button" is what VoiceOver says and "radio" is what the locator
// vocabulary calls it. One vocabulary compiled per platform is the whole
// design, so the two have to meet somewhere.
func TestDeclaredRoleSatisfiesTheLocatorVocabulary(t *testing.T) {
	n := &Node{Class: "XCUIElementTypeOther", DeclaredRole: "radio"}
	if !HasRole(n, "radio") {
		t.Error("a declared radio does not answer role=radio")
	}
	if HasRole(n, "checkbox") {
		t.Error("a declared radio answers role=checkbox")
	}
	// A declared role must not override a class that genuinely disagrees for
	// some other role.
	plain := &Node{Class: "XCUIElementTypeButton"}
	if HasRole(plain, "checkbox") {
		t.Error("a plain button answers role=checkbox")
	}
}

// A selected button reports "1" as its value — Wikipedia's onboarding on an
// iPhone 15 Plus, iOS 26.6.2 — and that is a state, not the button's name.
// The same digit in a text field is what the user typed. CHALLENGES 77.
func TestIOSSelectedButtonIsNamedByItsLabel(t *testing.T) {
	const src = `<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Wikipedia" label="Wikipedia" enabled="true" visible="true" accessible="false" x="0" y="0" width="430" height="932" index="0">
  <XCUIElementTypeButton type="XCUIElementTypeButton" value="1" name="App Onboarding Community Option Button" label="Community-related content" enabled="true" visible="true" accessible="true" x="0" y="123" width="430" height="24" index="0" traits="Selected, StaticText, Button"/>
  <XCUIElementTypeButton type="XCUIElementTypeButton" name="App Onboarding Personalized Option Button" label="Personalized content" enabled="true" visible="true" accessible="true" x="0" y="415" width="430" height="24" index="1" traits="StaticText, Button"/>
  <XCUIElementTypeTextField type="XCUIElementTypeTextField" value="1" name="quantity" label="Quantity" enabled="true" visible="true" accessible="true" x="0" y="500" width="430" height="40" index="2"/>
</XCUIElementTypeApplication>`
	tree, err := ParseIOS([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, e := range tree.Map() {
		lines = append(lines, e.Line())
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"Community-related content (button)",
		"Personalized content (button)", // the positive control: no value, labeled
	} {
		if !strings.Contains(got, want) {
			t.Errorf("map lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, " 1 (button)") {
		t.Errorf("a selected button is named by its value:\n%s", got)
	}
	// A text field's "1" is its contents, and stays so.
	if textValue("XCUIElementTypeTextField", "1", "Quantity") != "1" {
		t.Error("a text field holding 1 lost its text")
	}
}

// Three shapes from Wikipedia on a real iPhone 15 Plus, iOS 26.6.2, trimmed.
// A feed card whose title text is accessible and whose cell is not; a
// section cell wrapping it; an article's links, marked not accessible with
// the accessible text inside; and a search result whose text has a newline
// in it. CHALLENGES 78, 79.
const wikipediaShapes = `<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Wikipedia" label="Wikipedia" bundleId="org.wikimedia.wikipedia" enabled="true" visible="true" accessible="false" x="0" y="0" width="430" height="932" index="0">
  <XCUIElementTypeCell type="XCUIElementTypeCell" enabled="true" visible="true" accessible="false" x="0" y="80" width="430" height="512" index="0">
    <XCUIElementTypeCollectionView type="XCUIElementTypeCollectionView" enabled="true" visible="true" accessible="false" x="20" y="155" width="390" height="422" index="0">
      <XCUIElementTypeCell type="XCUIElementTypeCell" name="Explore Article Cell" enabled="true" visible="true" accessible="false" x="20" y="155" width="390" height="422" index="0">
        <XCUIElementTypeStaticText type="XCUIElementTypeStaticText" value="Mary Mallon" name="Mary Mallon" label="Mary Mallon" enabled="true" visible="true" accessible="true" x="35" y="367" width="115" height="23" index="0"/>
        <XCUIElementTypeStaticText type="XCUIElementTypeStaticText" value="Irish-American cook" name="Irish-American cook" label="Irish-American cook" enabled="true" visible="true" accessible="true" x="35" y="395" width="200" height="20" index="1"/>
      </XCUIElementTypeCell>
    </XCUIElementTypeCollectionView>
  </XCUIElementTypeCell>
  <XCUIElementTypeWebView type="XCUIElementTypeWebView" enabled="true" visible="true" accessible="false" x="0" y="600" width="430" height="200" index="1">
    <XCUIElementTypeLink type="XCUIElementTypeLink" name="bacteria" label="bacteria" enabled="true" visible="true" accessible="false" x="179" y="680" width="61" height="21" index="0">
      <XCUIElementTypeStaticText type="XCUIElementTypeStaticText" value="bacteria" name="bacteria" label="bacteria" enabled="true" visible="true" accessible="true" x="179" y="680" width="61" height="21" index="0"/>
    </XCUIElementTypeLink>
    <XCUIElementTypeLink type="XCUIElementTypeLink" name="offscreen" label="offscreen" enabled="true" visible="false" accessible="false" x="0" y="0" width="0" height="0" index="1"/>
  </XCUIElementTypeWebView>
  <XCUIElementTypeCell type="XCUIElementTypeCell" name="Search Result Delilah" enabled="true" visible="true" accessible="false" x="0" y="820" width="430" height="61" index="2">
    <XCUIElementTypeStaticText type="XCUIElementTypeStaticText" value="Delilah S. Dawson Redirected from: Ava Lovelace
American author (born 1977)" enabled="true" visible="true" accessible="true" x="20" y="830" width="390" height="40" index="0"/>
  </XCUIElementTypeCell>
</XCUIElementTypeApplication>`

func TestIOSCardsAndLinksAreTargets(t *testing.T) {
	tree, err := ParseIOS([]byte(wikipediaShapes))
	if err != nil {
		t.Fatal(err)
	}
	entries := tree.Map()
	var lines []string
	for _, e := range entries {
		lines = append(lines, e.Line())
	}
	got := strings.Join(lines, "\n")

	// The card, labeled by its text — and only the innermost cell: the
	// section cell around it would put a whole feed section under one entry.
	if !strings.Contains(got, "Mary Mallon Irish-American cook (button)") {
		t.Errorf("the feed card is not a target:\n%s", got)
	}
	cells := 0
	for _, e := range entries {
		if e.Node.Class == "XCUIElementTypeCell" && strings.Contains(e.Label, "Mary Mallon") {
			cells++
		}
	}
	if cells != 1 {
		t.Errorf("%d cells carry the card; want only the innermost:\n%s", cells, got)
	}

	// The link, as a link; the one with no frame stays out.
	if !strings.Contains(got, "bacteria (link)") {
		t.Errorf("a visible link is not a target, or not called a link:\n%s", got)
	}
	if strings.Contains(got, "offscreen") {
		t.Errorf("a link with no frame was mapped:\n%s", got)
	}

	// One entry, one line.
	for _, e := range entries {
		if strings.ContainsAny(e.Line(), "\n\r") {
			t.Errorf("%s spans more than one line: %q", e.Ref, e.Line())
		}
	}
	if !strings.Contains(got, "Ava Lovelace American author (born 1977) (button)") {
		t.Errorf("the search result's two lines were not joined:\n%s", got)
	}
}

// iOS 26's Settings draws a switch row as a switch inside a switch: the outer
// one is the whole row and carries the name, the ID and the state, and the
// inner one is the toggle, with no name at all. Tapping the outer one's center
// lands on the words and does nothing; so does tapping the cell around them.
// `map` listed all three, and only the nameless one worked — measured on an
// iPhone 15 Plus (iOS 26.6.2) and the iPhone 17 Pro simulator (iOS 26.5),
// where `check` on the named entry reported "still unchecked".
func TestIOSSwitchRowIsOneEntryAimedAtItsToggle(t *testing.T) {
	tree := loadIOS(t, "ios26-settings-motion.xml")
	var rows []Entry
	for _, e := range tree.Map() {
		if strings.Contains(e.Line(), "Reduce Motion") {
			rows = append(rows, e)
		}
		if e.Label == "XCUIElementTypeSwitch" {
			t.Errorf("a switch is named for its class: %s", e.Line())
		}
	}
	if len(rows) != 1 {
		for _, e := range rows {
			t.Log(e.Line(), e.Bounds)
		}
		t.Fatalf("Reduce Motion maps as %d entries, want 1", len(rows))
	}
	e := rows[0]
	if e.Role != "switch" || e.Checked == nil || *e.Checked {
		t.Errorf("entry is %s, want an unchecked switch", e.Line())
	}
	// The toggle, x=305 w=63 y=146 h=29 in the captured hierarchy.
	if want := (Rect{X1: 305, Y1: 146, X2: 368, Y2: 175}); e.Bounds != want {
		t.Errorf("entry aims at %v, want the toggle's %v", e.Bounds, want)
	}
	if got := e.Locator.String(); got != "testid=REDUCE_MOTION,role=switch" {
		t.Errorf("locator %s, want testid=REDUCE_MOTION,role=switch", got)
	}
	// And every locator on the screen still resolves to its own node.
	for _, m := range tree.Map() {
		if got := m.Locator.Resolve(tree); len(got) != 1 || got[0] != m.Node {
			t.Errorf("%s derived %s resolving to %d nodes", m.Ref, m.Locator, len(got))
		}
	}
}

// iOS 26's share sheet is drawn by another process, and XCTest reports its
// content relative to the container that hosts it rather than to the screen:
// a tap on "Add to Home Screen" opened Find on Page, and one on "View More"
// closed the sheet, each reported as tapped. The expected frames are the
// captured ones plus the container's origin, and they agree with where a
// screenshot drew each row, to within reading it off the image. CHALLENGES 128.
func TestIOSShareSheetContentIsOnScreenWhereItIsDrawn(t *testing.T) {
	for _, c := range []struct {
		fixture, label string
		want           Rect
	}{
		// The cell at 283,249 under a container at 9,477.
		{"ios26-share-sheet-half.xml", "View More", Rect{X1: 292, Y1: 726, X2: 375, Y2: 865}},
		// The cell at 16,552 under a container at 0,63.
		{"ios26-share-sheet-expanded.xml", "Add to Home Screen", Rect{X1: 16, Y1: 615, X2: 386, Y2: 666}},
	} {
		tree := loadIOS(t, c.fixture)
		var found []Entry
		for _, e := range tree.Map() {
			if e.Label == c.label {
				found = append(found, e)
			}
		}
		if len(found) != 1 {
			t.Fatalf("%s: %q maps as %d entries, want 1", c.fixture, c.label, len(found))
		}
		if got := found[0].Bounds; got != c.want {
			t.Errorf("%s: %q aims at %v, want %v where it is drawn", c.fixture, c.label, got, c.want)
		}
		// Safari's own content, outside the sheet, is not moved.
		for _, e := range tree.Map() {
			if e.Label == "Paste" && e.Bounds.Y1 > 477 {
				t.Errorf("%s: Safari's page moved with the sheet: %v", c.fixture, e.Bounds)
			}
		}
	}
}

func TestRebaseRemoteContentOnlyAtACoordinateReset(t *testing.T) {
	node := func(r Rect, kids ...*Node) *Node {
		n := &Node{Bounds: r, Children: kids}
		for _, k := range kids {
			k.Parent = n
		}
		return n
	}
	// Screen coordinates: a same-size child shares its parent's origin, and
	// is left where it is.
	inPlace := node(Rect{X1: 10, Y1: 20, X2: 110, Y2: 220})
	root := node(Rect{X2: 402, Y2: 874}, node(Rect{X1: 10, Y1: 20, X2: 110, Y2: 220}, inPlace))
	rebaseRemoteContent(root)
	if want := (Rect{X1: 10, Y1: 20, X2: 110, Y2: 220}); inPlace.Bounds != want {
		t.Errorf("a child already on screen moved to %v", inPlace.Bounds)
	}

	// A reset: the child at 0,0 with its parent's size. It and everything
	// under it move by the parent's origin, except a node with no size.
	row := node(Rect{X1: 5, Y1: 50, X2: 95, Y2: 70})
	offscreen := node(Rect{})
	reset := node(Rect{X2: 100, Y2: 200}, row, offscreen)
	root = node(Rect{X2: 402, Y2: 874}, node(Rect{X1: 10, Y1: 20, X2: 110, Y2: 220}, reset))
	rebaseRemoteContent(root)
	if want := (Rect{X1: 15, Y1: 70, X2: 105, Y2: 90}); row.Bounds != want {
		t.Errorf("a row under the reset is at %v, want %v", row.Bounds, want)
	}
	if !offscreen.Bounds.Empty() || offscreen.Bounds.X1 != 0 {
		t.Errorf("an off-screen node was given a position: %v", offscreen.Bounds)
	}

	// Under a parent at the origin a 0,0 child is ordinary.
	top := node(Rect{X2: 402, Y2: 874})
	root = node(Rect{X2: 402, Y2: 874}, top)
	rebaseRemoteContent(root)
	if top.Bounds != (Rect{X2: 402, Y2: 874}) {
		t.Errorf("a full-screen child moved: %v", top.Bounds)
	}
}
