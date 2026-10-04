package uitree

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMapOutput(t *testing.T) {
	tree := loadFixture(t, "login.xml")
	got := tree.Map()

	want := []string{
		"@e1 Navigate up (button)",
		"@e2 Email address (input)",
		// Not "Password (input)": the platform marks this field
		// password="true", so it is named by its resource id and carries its
		// own role. Here the text was only the hint and printing it was
		// harmless; on a filled field it is the password itself. See
		// TestPasswordFieldNeverPrintsItsContents.
		"@e3 password (password)",
		// A checkbox reports its state: it is a toggle, so a caller that
		// cannot see whether it is already ticked cannot reach one.
		"@e4 Remember me (checkbox, unchecked)",
		"@e5 Sign In (button)",
		// A clickable TextView is a button, not a link: the Android launcher
		// renders every app icon that way (see launcher.xml). Native Android
		// has no link widget; role=link is kept for WebView content.
		"@e6 Forgot password? (button)",
		"@e7 providers (list)",
		"@e8 Continue with Google (button)",
		"@e9 Continue with Apple (button)",
	}
	if len(got) != len(want) {
		var lines []string
		for _, e := range got {
			lines = append(lines, e.Line())
		}
		t.Fatalf("map produced %d entries:\n%s\nwant %d", len(got), strings.Join(lines, "\n"), len(want))
	}
	for i := range want {
		if got[i].Line() != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i].Line(), want[i])
		}
	}
}

func TestMapSkipsRedundantWrapper(t *testing.T) {
	tree := loadFixture(t, "login.xml")
	// The clickable FrameLayout wrapping the submit button has identical
	// bounds and no label of its own. Showing both is how an agent ends up
	// choosing the wrapper and reporting a tap that did nothing.
	for _, e := range tree.Map() {
		if e.Locator.Value == "com.example.shop:id/submit_wrapper" {
			t.Error("redundant clickable wrapper was mapped alongside its button")
		}
	}
}

func TestMapSkipsOffscreenElements(t *testing.T) {
	tree := loadFixture(t, "login.xml")
	for _, e := range tree.Map() {
		if e.Bounds.Empty() {
			t.Errorf("%s has empty bounds and should not be mapped", e.Ref)
		}
		if e.Label == "Offscreen" {
			t.Error("zero-bounds element was mapped")
		}
	}
}

func TestMapRefsAreSequentialAndInDocumentOrder(t *testing.T) {
	tree := loadFixture(t, "login.xml")
	entries := tree.Map()
	lastY := -1
	for i, e := range entries {
		if want := "@e" + itoa(i+1); e.Ref != want {
			t.Errorf("entry %d has ref %s, want %s", i, e.Ref, want)
		}
		// Document order is top-to-bottom on this screen; a ref table that
		// jumps around is unreadable next to a screenshot.
		if e.Bounds.Y1 < lastY {
			t.Errorf("%s (%s) is above the previous entry", e.Ref, e.Label)
		}
		lastY = e.Bounds.Y1
	}
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func TestDescribePrefersMoreSpecificLabel(t *testing.T) {
	// text "Continue" + content-desc "Continue with Google": the label wins.
	n := &Node{Text: "Continue", Label: "Continue with Google", Class: "android.widget.Button"}
	if got := describe(n); got != "Continue with Google" {
		t.Errorf("describe = %q, want the fuller content-desc", got)
	}
	// An unrelated content-desc must not displace the visible text.
	n = &Node{Text: "Submit", Label: "icon", Class: "android.widget.Button"}
	if got := describe(n); got != "Submit" {
		t.Errorf("describe = %q, want the visible text", got)
	}
}

func TestDescribeFallsBackThroughTree(t *testing.T) {
	child := &Node{Text: "Buy now", Class: "android.widget.TextView"}
	parent := &Node{Class: "android.view.ViewGroup", Clickable: true, Children: []*Node{child}}
	if got := describe(parent); got != "Buy now" {
		t.Errorf("describe = %q, want the descendant text", got)
	}
}

func TestDescribeDoesNotBorrowScrollContents(t *testing.T) {
	child := &Node{Text: "Item one", Class: "android.widget.TextView"}
	list := &Node{
		Class:      "androidx.recyclerview.widget.RecyclerView",
		TestID:     "com.example.shop:id/feed",
		Scrollable: true,
		Children:   []*Node{child},
	}
	if got := describe(list); got != "feed" {
		t.Errorf("describe = %q, want the container's own id", got)
	}
}

func TestActionable(t *testing.T) {
	tests := []struct {
		name string
		node *Node
		want bool
	}{
		{"clickable button", &Node{Displayed: true, Clickable: true, Bounds: Rect{0, 0, 100, 50}}, true},
		{"scroll container", &Node{Displayed: true, Scrollable: true, Bounds: Rect{0, 0, 100, 50}}, true},
		{"checkable", &Node{Displayed: true, Checkable: true, Bounds: Rect{0, 0, 100, 50}}, true},
		{"long-clickable", &Node{Displayed: true, LongClickable: true, Bounds: Rect{0, 0, 100, 50}}, true},
		{"focusable input", &Node{Displayed: true, Focusable: true, Class: "android.widget.EditText", Bounds: Rect{0, 0, 100, 50}}, true},
		{"plain label", &Node{Displayed: true, Class: "android.widget.TextView", Bounds: Rect{0, 0, 100, 50}}, false},
		{"zero bounds", &Node{Displayed: true, Clickable: true, Bounds: Rect{0, 0, 0, 0}}, false},
		{"focusable non-input", &Node{Displayed: true, Focusable: true, Class: "android.view.View", Bounds: Rect{0, 0, 100, 50}}, false},
		// The case iOS actually produces: WebDriverAgent marks 120 of the 201
		// nodes in the SpringBoard capture visible="false", and a scroll view
		// is Scrollable by its class whatever its visibility. Without this
		// check such a node reaches `map` and an agent is offered something it
		// cannot see.
		{"hidden but scrollable", &Node{Scrollable: true, Bounds: Rect{0, 0, 100, 50}}, false},
		{"hidden but clickable", &Node{Clickable: true, Bounds: Rect{0, 0, 100, 50}}, false},
	}
	for _, tc := range tests {
		if got := Actionable(tc.node); got != tc.want {
			t.Errorf("%s: Actionable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTextDeduplicates(t *testing.T) {
	tree := loadFixture(t, "login.xml")
	text := tree.Text()
	lines := strings.Split(text, "\n")

	seen := map[string]int{}
	for _, l := range lines {
		seen[l]++
	}
	for l, n := range seen {
		if n > 1 {
			t.Errorf("line %q appears %d times", l, n)
		}
	}
	// The offscreen node contributes nothing readable.
	if strings.Contains(text, "Offscreen") {
		t.Error("text included a zero-bounds element")
	}
	for _, want := range []string{"Sign in", "Email address", "Remember me", "Forgot password?"} {
		if !strings.Contains(text, want) {
			t.Errorf("text is missing %q", want)
		}
	}
}

func TestEntryLineWithoutRole(t *testing.T) {
	e := Entry{Ref: "@e1", Label: "Something"}
	if got := e.Line(); got != "@e1 Something" {
		t.Errorf("Line = %q", got)
	}
}

// TestDisplayedIsHonouredEndToEnd pins what `displayed` is worth on each
// platform, because the three answers are different and none of them is
// guessable from the code.
//
// The field sat written-but-unread for months: both parsers set it, nothing
// consulted it, and `map` was none the worse — because on Android it is a
// constant and on iOS it was already folded into Clickable. Neither of those
// is a reason to drop it, and both are reasons to write down.
func TestDisplayedIsHonouredEndToEnd(t *testing.T) {
	t.Run("android says nothing", func(t *testing.T) {
		// Measured on a Pixel 7 AVD across ten screen states: 590 nodes,
		// every one displayed="true", not one false. UiAutomator2 filters
		// non-displayed nodes out of /source before serializing, so the
		// attribute carries no information. If a future server version starts
		// emitting false, this fixture assertion will not notice — the
		// platform note in Actionable is the record.
		tree := loadFixture(t, "launcher-uia2.xml")
		var hidden int
		walkTree(tree.Root, func(n *Node) {
			if !n.Displayed {
				hidden++
			}
		})
		if hidden != 0 {
			t.Errorf("%d hidden nodes in a uia2 capture; the measurement in Actionable needs redoing", hidden)
		}
	})

	t.Run("ios means it", func(t *testing.T) {
		tree := loadIOS(t, "ios-springboard.xml")
		var hidden, total int
		walkTree(tree.Root, func(n *Node) {
			total++
			if !n.Displayed {
				hidden++
			}
		})
		// Most of a real iOS hierarchy is off-screen. If this ever reads zero,
		// WebDriverAgent has changed what it reports and the whole visibility
		// story needs rechecking rather than the number nudging.
		if hidden < total/4 {
			t.Errorf("only %d of %d iOS nodes are hidden; that is not what WDA reports", hidden, total)
		}
		// And none of them may reach the map.
		for _, e := range tree.Map() {
			if e.Node != nil && !e.Node.Displayed {
				t.Errorf("map offered %s (%s), which is not visible", e.Ref, e.Label)
			}
		}
	})

	t.Run("an external driver is believed", func(t *testing.T) {
		// The wire protocol documents displayed:false as meaningful. This is
		// the assertion that makes that true rather than decorative.
		tree, err := UnmarshalWire([]byte(`{"class":"Screen","bounds":[0,0,400,800],"children":[
			{"class":"Button","text":"seen","bounds":[0,0,100,50],"clickable":true},
			{"class":"Button","text":"unseen","bounds":[0,50,100,100],"clickable":true,"displayed":false}
		]}`))
		if err != nil {
			t.Fatal(err)
		}
		var labels []string
		for _, e := range tree.Map() {
			labels = append(labels, e.Label)
		}
		if len(labels) != 1 || labels[0] != "seen" {
			t.Errorf("map = %v, want only the visible button", labels)
		}
	})
}

func walkTree(n *Node, fn func(*Node)) {
	fn(n)
	for _, c := range n.Children {
		walkTree(c, fn)
	}
}

// The three label defects below all came from one app — Wikipedia's Android
// client, the first genuinely third-party app Mobium was pointed at (rollout
// gate G4). Every screen before it shipped with the OS, and none of them
// produced a label longer than about ninety characters.

func TestLabelIsBounded(t *testing.T) {
	// The real card: a title, its Save and Share buttons, a subtitle, and the
	// whole article lede, under one clickable container. It came back as 876
	// runes — 91% of that map's entire text from one of thirteen entries.
	lede := "Rear-Admiral Sir Thomas Hardy was a British Royal Navy officer and politician. " +
		"Having joined the navy sometime before 1688, Hardy was supported in his career by " +
		"George Churchill, whom he served as first lieutenant during the Battle of Barfleur " +
		"in 1692. Promoted to captain in 1693, Hardy served in the Channel Islands and off " +
		"the coast of England until 1702."
	card := &Node{
		Displayed: true, Clickable: true, Bounds: Rect{0, 0, 1080, 600},
		Children: []*Node{
			{Displayed: true, Text: "Thomas Hardy (Royal Navy officer, died 1732)"},
			{Displayed: true, Label: "Save", Clickable: true},
			{Displayed: true, Label: "Share", Clickable: true},
			{Displayed: true, Text: "British Royal Navy officer and politician (1666–1732)"},
			{Displayed: true, Text: lede},
		},
	}
	got := describe(card)
	if n := len([]rune(got)); n > maxLabel+1 { // +1 for the ellipsis
		t.Errorf("label is %d runes, want at most %d:\n%s", n, maxLabel, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated label should say so:\n%s", got)
	}
	// It must still be the useful end of the card, not the boilerplate.
	if !strings.Contains(got, "Thomas Hardy") {
		t.Errorf("truncation dropped the title:\n%s", got)
	}
}

func TestLabelStripsMarkupAnAppPutInIt(t *testing.T) {
	// Verbatim from Wikipedia's feed, in an accessibility string. Passing it
	// through means an agent reads tag soup, and a text= locator has to spell
	// out the markup to match.
	raw := `<span lang="en" dir="ltr"><span class="mw-page-title-main">` +
		`Thomas Hardy (Royal Navy officer, died 1732)</span></span>`
	n := &Node{Displayed: true, Clickable: true, Bounds: Rect{0, 0, 100, 50}, Text: raw}
	got := describe(n)
	if strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Errorf("markup survived into the label: %q", got)
	}
	if !strings.Contains(got, "Thomas Hardy (Royal Navy officer, died 1732)") {
		t.Errorf("stripping the markup lost the words: %q", got)
	}
}

func TestOrdinaryTextWithAngleBracketsIsLeftAlone(t *testing.T) {
	// The stripper must not eat arithmetic, code samples or comparisons. It
	// requires something that actually looks like a tag.
	for _, in := range []string{"a < b", "5<6>7", "x -> y", "<<", "if a<b then", "3 > 2"} {
		n := &Node{Displayed: true, Clickable: true, Bounds: Rect{0, 0, 100, 50}, Text: in}
		if got := describe(n); got != in {
			t.Errorf("describe(%q) = %q, want it untouched", in, got)
		}
	}
}

func TestLabelPartsAreNotRepeated(t *testing.T) {
	// The search field: hint text and content description say the same thing,
	// and the mic button's label was appended too, giving
	// "Search Wikipedia Search Wikipedia Voice input search".
	field := &Node{
		Displayed: true, Clickable: true, Bounds: Rect{0, 0, 1080, 120},
		Children: []*Node{
			{Displayed: true, Text: "Search Wikipedia"},
			{Displayed: true, Label: "Search Wikipedia"},
			{Displayed: true, Label: "Voice input search", Clickable: true},
		},
	}
	got := describe(field)
	if strings.Count(got, "Search Wikipedia") != 1 {
		t.Errorf("label repeats itself: %q", got)
	}
}

// TestShortLabelsAreUnchanged is the guard that matters most: the bounding and
// cleaning must be invisible on every screen that was already fine, which is
// every screen tested before this one.
func TestShortLabelsAreUnchanged(t *testing.T) {
	for _, name := range []string{"launcher.xml", "launcher-uia2.xml", "login.xml"} {
		tree := loadFixture(t, name)
		for _, e := range tree.Map() {
			if strings.HasSuffix(e.Label, "…") {
				t.Errorf("%s: %s was truncated and should not have been: %q", name, e.Ref, e.Label)
			}
		}
	}
}

// TestPasswordFieldNeverPrintsItsContents guards a leak found on a real app.
//
// Android puts the typed value straight into the `text` attribute of a node it
// has already marked `password="true"`. Mobium parsed that flag into
// Node.Password and consulted it nowhere, so typing into Aegis's master
// password field made the very next `map` read:
//
//	@e2 Sup3rSecret! (input)
//
// `map` is broadcast output — agent transcripts, CI logs, MCP responses — and
// nobody consented to that.
func TestPasswordFieldNeverPrintsItsContents(t *testing.T) {
	secret := "Sup3rSecret!"
	field := &Node{
		Displayed: true, Focusable: true, Password: true,
		Class:  "android.widget.EditText",
		TestID: "com.example:id/text_password",
		Text:   secret,
		Bounds: Rect{0, 0, 1080, 120},
	}
	label := describe(field)
	if strings.Contains(label, secret) {
		t.Fatalf("the password reached the label: %q", label)
	}
	if label != "text_password" {
		t.Errorf("label = %q, want the resource id", label)
	}
	if got := roleOf(field); got != "password" {
		t.Errorf("role = %q, want password — an agent cannot avoid echoing what it cannot identify", got)
	}

	// Two password fields on one screen must stay distinguishable, or the
	// map hands out two identical lines and a confirm field is unusable.
	confirm := &Node{
		Displayed: true, Focusable: true, Password: true,
		Class:  "android.widget.EditText",
		TestID: "com.example:id/text_password_confirm",
		Text:   "Please confirm the password",
		Bounds: Rect{0, 120, 1080, 240},
	}
	if describe(field) == describe(confirm) {
		t.Error("the two password fields describe identically")
	}

	// A field with no resource id still must not leak.
	bare := &Node{
		Displayed: true, Focusable: true, Password: true,
		Class: "android.widget.EditText", Text: secret,
		Bounds: Rect{0, 0, 100, 50},
	}
	if l := describe(bare); strings.Contains(l, secret) {
		t.Errorf("a password field without an id leaked: %q", l)
	}
}

// TestPasswordRoleMatchesOnBothPlatforms: iOS names the type outright, Android
// sets a flag, and role=password has to mean the same thing either way.
func TestPasswordRoleMatchesOnBothPlatforms(t *testing.T) {
	android := &Node{Class: "android.widget.EditText", Password: true}
	ios := &Node{Class: "XCUIElementTypeSecureTextField", Password: true}
	plain := &Node{Class: "android.widget.EditText"}

	for _, n := range []*Node{android, ios} {
		if !HasRole(n, "password") {
			t.Errorf("%s is not matched by role=password", n.Class)
		}
	}
	if HasRole(plain, "password") {
		t.Error("an ordinary text field matched role=password")
	}
}

// Fixtures captured from two real third-party apps, because the four screens
// this project shipped with could not exercise the code that guards them:
// their longest label is 27 characters against a 120 cap and none contains
// markup or a password field. These two do.
//
//   aegis-password-uia2.xml  Aegis's master-password setup — two password
//                            fields, one of them filled. The captured value
//                            was replaced with PASSWORD-MUST-NOT-APPEAR so a
//                            leak names itself in the failure output.
//   fdroid-list-uia2.xml     F-Droid's app list — sixteen dense rows of
//                            icon + name + summary, the shape that produced
//                            the 876-character label on Wikipedia.

func TestRealAppFixturesProduceUsableMaps(t *testing.T) {
	for _, tc := range []struct {
		file        string
		minEntries  int
		maxLabelLen int
	}{
		{"fdroid-list-uia2.xml", 10, maxLabel + 1},
		{"aegis-password-uia2.xml", 5, maxLabel + 1},
	} {
		t.Run(tc.file, func(t *testing.T) {
			tree := loadFixture(t, tc.file)
			entries := tree.Map()
			if len(entries) < tc.minEntries {
				t.Fatalf("%d entries, want at least %d", len(entries), tc.minEntries)
			}
			seen := map[string]bool{}
			for _, e := range entries {
				if strings.TrimSpace(e.Label) == "" {
					t.Errorf("%s has an empty label", e.Ref)
				}
				if n := len([]rune(e.Label)); n > tc.maxLabelLen {
					t.Errorf("%s label is %d runes: %q", e.Ref, n, e.Label)
				}
				if strings.Contains(e.Label, "<") && strings.Contains(e.Label, ">") {
					t.Errorf("%s label carries markup: %q", e.Ref, e.Label)
				}
				// Two rows an agent cannot tell apart are two rows it will
				// pick between at random.
				if seen[e.Label] {
					t.Errorf("two entries share the label %q", e.Label)
				}
				seen[e.Label] = true
			}
		})
	}
}

// TestPasswordNeverEscapesARealScreen is the regression test for the leak, on
// the actual hierarchy it was found in rather than a hand-built node.
func TestPasswordNeverEscapesARealScreen(t *testing.T) {
	const marker = "PASSWORD-MUST-NOT-APPEAR"
	tree := loadFixture(t, "aegis-password-uia2.xml")

	// The fixture must still contain the value, or this test proves nothing.
	var found bool
	walkTree(tree.Root, func(n *Node) {
		if strings.Contains(n.Text, marker) {
			found = true
		}
	})
	if !found {
		t.Fatal("the fixture no longer holds a filled password field; this test is vacuous")
	}

	for _, e := range tree.Map() {
		if strings.Contains(e.Label, marker) {
			t.Errorf("map leaked the password on %s: %q", e.Ref, e.Label)
		}
	}
	if strings.Contains(tree.Text(), marker) {
		t.Error("the whole-screen text read leaked the password")
	}

	// And the masked form still answers "did my typing land".
	walkTree(tree.Root, func(n *Node) {
		if !strings.Contains(n.Text, marker) {
			return
		}
		masked, ok := Redact(n)
		if !ok {
			t.Fatal("Redact declined a filled password field")
		}
		if strings.Contains(masked, marker) {
			t.Errorf("the mask contains the value: %q", masked)
		}
		if !strings.Contains(masked, "24 characters") {
			t.Errorf("the mask lost the length, so typing cannot be verified: %q", masked)
		}
	})
}

// TestTruncationCountsCharactersNotBytes: every label rule so far has only
// ever seen Latin script. Cutting a multi-byte string at a byte offset splits
// a character and emits invalid UTF-8.
//
// The first version of this test proved nothing. Its sample was
// strings.Repeat("日本語のテキスト", 40) — 24 bytes per repeat, so a 120-byte
// cut lands exactly on a character boundary every time, and byte truncation
// passed every assertion. The inputs below are deliberately misaligned, and
// the check is utf8.ValidString rather than a search for U+FFFD, which a
// malformed tail does not actually contain.
func TestTruncationCountsCharactersNotBytes(t *testing.T) {
	samples := []string{
		strings.Repeat("日本語のテキスト", 40),
		"x" + strings.Repeat("日本語のテキスト", 40),  // misaligned by one byte
		"xy" + strings.Repeat("日本語のテキスト", 40), // and by two
		strings.Repeat("Текст на русском ", 20),
		"a" + strings.Repeat("نص عربي ", 40),
		strings.Repeat("🙂🚀", 200), // four bytes per rune
		"abc" + strings.Repeat("🙂🚀", 200),
	}
	for _, s := range samples {
		got := truncate(s)
		if !utf8.ValidString(got) {
			t.Errorf("truncation produced invalid UTF-8 from %.12q…", s)
		}
		if n := len([]rune(got)); n > maxLabel+1 {
			t.Errorf("%d runes, want at most %d", n, maxLabel)
		}
		if !strings.HasSuffix(got, "…") {
			t.Errorf("no ellipsis on a truncated label: %.24q", got)
		}
		body := strings.TrimSuffix(got, "…")
		if !strings.HasPrefix(s, body) {
			t.Errorf("truncation changed the text: %.24q", got)
		}
		// The point of counting runes: a label of mostly 3- and 4-byte
		// characters must still carry close to maxLabel *characters*, not
		// maxLabel bytes' worth of them.
		if n := len([]rune(body)); n < maxLabel/2 {
			t.Errorf("kept only %d characters of %.12q…, which is a byte-shaped cut", n, s)
		}
	}
}

// TestPasswordIsAddressableAsARole: masking a password field's label removes
// the obvious way to name it, so `role=password` has to be a working locator
// and not just a thing `map` prints. Without it, the two fields on a signup
// screen can only be told apart by resource id.
func TestPasswordIsAddressableAsARole(t *testing.T) {
	tree := loadFixture(t, "aegis-password-uia2.xml")
	loc, err := ParseLocator("role=password")
	if err != nil {
		t.Fatalf("role=password is not a valid locator: %v", err)
	}
	got := loc.Resolve(tree)
	if len(got) != 2 {
		t.Fatalf("role=password matched %d nodes, want the 2 password fields", len(got))
	}
	for _, n := range got {
		if !n.Password {
			t.Errorf("role=password matched %s, which is not one", n.Class)
		}
	}
	// `password` is a *subtype* of `input`, not a sibling of it: a password
	// field is a text field, and `role=input` matching it is correct. What
	// makes the pair useful is that the narrower role exists at all, since
	// `map` no longer prints a label anyone could match on. Asserted rather
	// than assumed, because the first version of this test expected the
	// opposite and the code was right.
	plain, _ := ParseLocator("role=input")
	if n := len(plain.Resolve(tree)); n != 2 {
		t.Errorf("role=input matched %d nodes; a password field is still a text input", n)
	}
	// The distinction only pays off if the narrower one is narrower.
	if len(got) >= len(plain.Resolve(tree))+1 {
		t.Error("role=password is not narrower than role=input")
	}
}

// iOS keeps what a dialog covers in the tree, reported not visible. Captured
// from MobiumApp on an iPhone 17 Pro simulator: its own alert, and the "Save
// Password?" sheet iOS raised after a login.
func TestDialogFindsWhatCoversTheScreen(t *testing.T) {
	for _, tc := range []struct{ file, dialog, under, on string }{
		{"ios-app-alert.xml", "Delete draft?", "backBtn", "Delete"},
		{"ios-save-password.xml", "Save Password?", "logoutBtn", "Not Now"},
	} {
		tree := loadIOS(t, tc.file)
		d := tree.Dialog()
		if d == nil || d.Label != tc.dialog {
			t.Fatalf("%s: dialog = %+v, want %q", tc.file, d, tc.dialog)
		}
		byID := map[string]*Node{}
		tree.Walk(func(n *Node) bool { byID[n.TestID] = n; return true })
		if byID[tc.under] == nil || byID[tc.under].Within(d) {
			t.Errorf("%s: %s should be under the dialog, not in it", tc.file, tc.under)
		}
		if byID[tc.on] == nil || !byID[tc.on].Within(d) {
			t.Errorf("%s: %s should be the dialog's own", tc.file, tc.on)
		}
	}
	// And a screen with no dialog has none.
	if d := loadIOS(t, "ios26-settings-motion.xml").Dialog(); d != nil {
		t.Errorf("found a dialog on a plain screen: %s", d.Label)
	}
}

// On iOS the keyboard is in the app's tree, and what it covers stays there,
// marked not visible — where a locator could still resolve it. Captured from
// MobiumApp's Dialog Demo with the keyboard up over a button pinned to the
// bottom of the screen; it covered the field being typed into as well.
func TestKeyboardCoversWhatIsUnderIt(t *testing.T) {
	tree := loadIOS(t, "ios-keyboard-over-button.xml")
	k := tree.Keyboard()
	if k == nil {
		t.Fatal("no keyboard found")
	}
	byID := map[string]*Node{}
	tree.Walk(func(n *Node) bool { byID[n.TestID] = n; return true })
	for _, id := range []string{"coveredBtn", "coverField"} {
		if n := byID[id]; n == nil || !k.Bounds.Covers(n) {
			t.Errorf("%s should be under the keyboard", id)
		}
	}
	// Nothing above the keyboard's top edge is under it.
	if b := byID["backBtn"]; b == nil || k.Bounds.Covers(b) {
		t.Error("the Back button, at the top, reads as covered")
	}
}

// NetNewsWire, the second third-party app driven on iOS, on its Settings and
// search screens. Four things map got wrong on its first look, all from
// reading WebDriverAgent's attributes but not its traits:
//   - a table printed as "XCUIElementTypeTable", XCUITest's own type name;
//   - section headers, traits="Header", printed as buttons;
//   - rows named "On My iPhone chevron", from a disclosure arrow VoiceOver
//     never reads (accessible="false");
//   - and no sign of which search scope was chosen (traits="Selected").
func TestNetNewsWireReadsTraits(t *testing.T) {
	lines := map[string]bool{}
	for _, e := range loadIOS(t, "ios26-netnewswire-settings.xml").Map() {
		line := strings.TrimPrefix(e.Line(), e.Ref+" ")
		lines[line] = true
		if strings.Contains(e.Label, "XCUIElementType") {
			t.Errorf("%q prints XCUITest's type name", line)
		}
		if strings.Contains(e.Label, "chevron") {
			t.Errorf("%q borrows the disclosure arrow's name", line)
		}
	}
	for _, want := range []string{"Table (list)", "On My iPhone (button)", "Add Account (button)",
		"Sort Oldest to Newest (switch, unchecked)", "Confirm Mark All as Read (switch, checked)"} {
		if !lines[want] {
			t.Errorf("Settings does not map %q", want)
		}
	}
	for _, header := range []string{"Accounts (button)", "Feeds (button)", "Timeline (button)"} {
		if lines[header] {
			t.Errorf("the section header %q maps as a button", header)
		}
	}

	scope := map[string]string{}
	for _, e := range loadIOS(t, "ios26-netnewswire-search.xml").Map() {
		scope[e.Label] = strings.TrimPrefix(e.Line(), e.Ref+" ")
	}
	if scope["Here"] != "Here (button, selected)" || scope["All Articles"] != "All Articles (button)" {
		t.Errorf("the search scope maps as %q and %q", scope["Here"], scope["All Articles"])
	}
}

// Ice Cubes, the third third-party app driven on iOS and the first written
// in SwiftUI. Its timeline's posts are each one accessible button holding
// the controls a person uses — Reply, Boost, Favorite, Share — marked not
// accessible, and none was in map; its timeline picker is a PopUpButton with
// the Header trait, which the section-header rule took out of map. Post
// content in the fixtures is replaced: authors are "Author One" and "Author
// Two".
func TestIceCubesMapsWhatSwiftUICombines(t *testing.T) {
	lines := map[string]int{}
	for _, e := range loadIOS(t, "ios26-icecubes-timeline.xml").Map() {
		lines[strings.TrimPrefix(e.Line(), e.Ref+" ")]++
	}
	for _, want := range []string{"Trending (button)", "Author One 🧿 (button)", "Reply (button)", "Boost (button)",
		"Favorite (button)", "Share post link (button)", "Image alt text: A four-panel comic. (button)", "#books (link)",
		"Timeline (button, selected)"} {
		if lines[want] != 1 {
			t.Errorf("the timeline maps %q %d times, want once", want, lines[want])
		}
	}
	// The author's name is a button inside one that adds the time; the
	// inner one is the target, not both.
	for line := range lines {
		if strings.HasPrefix(line, "Author One 🧿, 2h (") {
			t.Errorf("the wrapper %q maps as well as the name inside it", line)
		}
	}
}

// And a label= locator finds the control, not the parts SwiftUI labels
// around and inside it: the image viewer's Close button sits in a wider
// container of the same name, a Settings row holds its title as a text of
// the same name, and the Settings tab holds a gear image labeled
// "settings". (label=Settings itself still finds the screen's title and
// "Display Settings" as well, which are other things.)
func TestIceCubesLabelFindsTheControl(t *testing.T) {
	settings, err := ParseLocator("label=Settings")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range settings.Resolve(loadIOS(t, "ios26-icecubes-settings.xml")) {
		if n.Class == "XCUIElementTypeImage" {
			t.Errorf("label=Settings found the tab's icon as well as the tab")
		}
	}
	for _, c := range []struct{ file, loc string }{
		{"ios26-icecubes-image-viewer.xml", "label=Close"},
		{"ios26-icecubes-image-viewer.xml", "label=Info"},
		{"ios26-icecubes-settings.xml", "label=Display Settings"},
	} {
		tree := loadIOS(t, c.file)
		loc, err := ParseLocator(c.loc)
		if err != nil {
			t.Fatal(err)
		}
		// One, and where the button is: Info's container has its very frame,
		// and the first version of the rule found neither.
		got := loc.Resolve(tree)
		if len(got) != 1 || got[0].Bounds != byLabelClass(tree, got[0].Label, "XCUIElementTypeButton") {
			t.Errorf("%s: %s found %d, want the one button", c.file, c.loc, len(got))
		}
	}
}

// byLabelClass is the bounds of the node with this label and class.
func byLabelClass(tree *Tree, label, class string) Rect {
	var r Rect
	tree.Walk(func(n *Node) bool {
		if n.Label == label && n.Class == class {
			r = n.Bounds
			return false
		}
		return true
	})
	return r
}

// A screen the app puts in front of another is no Alert or Sheet, and iOS
// keeps the screen behind in the tree: with Ice Cubes' image viewer and its
// Add Account sheet up, a tap on the Timeline tab behind was reported done
// and did nothing. The negative control is the Obstruction Demo, whose
// targets under a pass-through view are reported not visible too and do
// take a tap; and no hidden target on an ordinary screen is covered.
func TestCoveredByScreen(t *testing.T) {
	timeline := func(tree *Tree) *Node {
		var tab *Node
		tree.Walk(func(n *Node) bool {
			if n.Class == "XCUIElementTypeButton" && n.Label == "Timeline" {
				tab = n
				return false
			}
			return true
		})
		return tab
	}
	for _, f := range []string{"ios26-icecubes-image-viewer.xml", "ios26-icecubes-add-account.xml"} {
		tree := loadIOS(t, f)
		if tab := timeline(tree); tab == nil || !tree.CoveredByScreen(tab) {
			t.Errorf("%s: the Timeline tab behind is not covered", f)
		}
		for _, e := range tree.Map() {
			if tree.CoveredByScreen(e.Node) {
				t.Errorf("%s: %s, on the screen in front, reads as covered", f, e.Line())
			}
		}
	}
	if tab := timeline(loadIOS(t, "ios26-icecubes-timeline.xml")); tab == nil || loadIOS(t, "ios26-icecubes-timeline.xml").CoveredByScreen(tab) {
		t.Error("the Timeline tab with nothing in front of it reads as covered")
	}

	ordinary := []string{"obstruction-ios.xml", "ios26-mobiumapp-home.xml", "ios26-settings-root.xml",
		"ios26-netnewswire-feeds-toolbar.xml", "ios26-icecubes-timeline.xml", "ios26-icecubes-settings.xml",
		"ios-keyboard-over-button.xml", "ios-keyboard-over-scrolled-form.xml"}
	hidden := 0
	for _, f := range ordinary {
		tree := loadIOS(t, f)
		tree.Walk(func(n *Node) bool {
			if !n.Displayed && n.Clickable == false && !n.Bounds.Empty() && iosControlTypes[n.Class] {
				hidden++
				if tree.CoveredByScreen(n) {
					t.Errorf("%s: %s %q, with nothing in front of it, reads as covered", f, n.Class, n.Label)
				}
			}
			return true
		})
	}
	if hidden == 0 {
		t.Fatal("no hidden targets on the ordinary screens: the negative control cannot fail")
	}
}
