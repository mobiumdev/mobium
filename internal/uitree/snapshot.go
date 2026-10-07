package uitree

import (
	"fmt"
	"regexp"
	"strings"
)

// Entry is one line of `mobium map`: a stable @ref, the locator it resolves
// through, and the label an agent reads to decide what to touch.
type Entry struct {
	Ref     string  `json:"ref"`
	Label   string  `json:"label"`
	Role    string  `json:"role"`
	Locator Locator `json:"locator"`
	Bounds  Rect    `json:"bounds"`
	// Checked is the state of a checkbox, radio or switch, and nil for
	// everything else.
	//
	// A pointer because "not a checkable thing" and "unchecked" are different
	// answers, and reporting a button as unchecked would be a lie of the kind
	// this project keeps finding. It was missing entirely until 2026-09-21:
	// the state was parsed, carried on the wire and then dropped here, so
	// `map` printed the same line for a ticked box and an empty one while the
	// device was plainly reporting `checked="true"`. Tapping a checkbox is a
	// toggle, so a caller that cannot see the state cannot reach one — it can
	// only flip whatever is there. CHALLENGES 65.
	Checked *bool `json:"checked,omitempty"`
	// Selected is set when the platform reports the element chosen: the
	// current tab, the chosen segment of a segmented control. Omitted
	// otherwise — nothing reports "not selected" in a way that tells a
	// selectable thing from any other. NetNewsWire's search scope gave no
	// sign of which of Here and All Articles was chosen until it was read
	// from iOS's traits.
	Selected bool `json:"selected,omitempty"`
	// Disabled is set when the platform reports the element not enabled. An
	// action waits for such a target to be enabled and refuses it if it
	// stays disabled, so map says so first: Pocket Casts' onboarding button
	// "Select at least 3" printed as a plain `(button)` while the device
	// reported it NotEnabled. CHALLENGES 239.
	Disabled bool `json:"disabled,omitempty"`
	// Value is a slider's value as the platform states it, and empty for
	// everything else. A string because it is the app's: iOS reports
	// "80%" or "1.2", whatever the app made of the position.
	Value string `json:"value,omitempty"`
	// Covered names a control drawn over every point of the element, so a
	// tap on it is refused (AimAt's Blocker), and is empty otherwise. map
	// listed Discover's category chips under Pocket Casts' search results,
	// and the rows under its "Search Failed", as if they could be pressed.
	// It cannot say what a plain view covers, which may let a tap through
	// or take it: what it marks, a tap refuses. CHALLENGES 264.
	Covered string `json:"covered,omitempty"`
	Node    *Node  `json:"-"`
}

// Line renders the entry the way the CLI prints it.
func (e Entry) Line() string {
	if e.Role == "" {
		return fmt.Sprintf("%s %s", e.Ref, e.Label)
	}
	states := []string{e.Role}
	if e.Checked != nil {
		state := "unchecked"
		if *e.Checked {
			state = "checked"
		}
		states = append(states, state)
	}
	if e.Selected {
		states = append(states, "selected")
	}
	if e.Disabled {
		states = append(states, "disabled")
	}
	if e.Value != "" {
		states = append(states, e.Value)
	}
	if e.Covered != "" {
		states = append(states, fmt.Sprintf("covered by %q", e.Covered))
	}
	return fmt.Sprintf("%s %s (%s)", e.Ref, e.Label, strings.Join(states, ", "))
}

// Actionable reports whether a node is worth putting in front of an agent.
//
// This is the native counterpart of vibium's interactive-element filter. A
// node qualifies if a user could do something to it and it occupies real
// space on screen; scrollable containers are included because scrolling is an
// action even when the container itself is not clickable.
//
// Displayed is checked here, and what that is worth differs by platform:
//
//   - Android tells us nothing. UiAutomator2 filters non-displayed nodes out
//     of /source before serializing, so the attribute is a constant: measured
//     across ten screen states — launcher, Settings, Settings scrolled to the
//     bottom, a search overlay, the notification shade, Clock, Chrome,
//     Contacts, Photos, Calculator — all 590 nodes carried displayed="true"
//     and not one carried false. The `uiautomator dump` format has no such
//     attribute at all and is parsed as displayed.
//   - iOS means it. 120 of the 201 nodes in the SpringBoard capture are
//     visible="false". It is mostly already accounted for, because the iOS
//     parser folds it into Clickable — but only into Clickable, so an
//     invisible scroll view or switch would still reach here.
//   - An external driver may send it, and this is the only place its answer
//     is acted on. Without this line the protocol would document a field that
//     changes nothing, which is worse than not having one.
//
// It costs nothing measurable today: honouring it changes no entry in any of
// the four captured hierarchies. That is the point — it is a guard against a
// screen we have not seen, not a fix for one we have.
func Actionable(n *Node) bool {
	if n.Bounds.Empty() {
		return false
	}
	if !n.Displayed {
		return false
	}
	if !n.Clickable && !n.LongClickable && !n.Checkable && !n.Scrollable {
		// Text inputs are frequently focusable-only until touched. And a
		// slider: Android marks a seek bar neither clickable nor scrollable,
		// so MobiumApp's two sliders were missing from map on the Pixel 8
		// Pro (CHALLENGES 227).
		if !(n.Focusable && HasRole(n, "input")) && !HasClassRole(n, "slider") {
			return false
		}
	}
	return true
}

// maxLabel is how many runes of label `map` will print for one element.
//
// A label exists so an agent can choose between elements; past a sentence or
// so it stops helping and starts costing context. The number is not arbitrary:
// on Wikipedia's feed, the useful labels ran 4 to 90 runes ("Ada Lovelace Day
// Annual event celebrating the contributions of women to STEM fields" is 82)
// while a single feed card produced **876** — 91% of that entire map's text
// from one of its thirteen entries. 120 keeps every real label intact and
// stops the runaway.
//
// Truncating is safe for resolution: Map derives each entry's locator from the
// node, not from the printed label, so a shortened label never changes what a
// @ref resolves to.
const maxLabel = 120

// describe produces the human label for a node, preferring what a person
// would actually read on screen. Falls back through the tree because Android
// containers routinely carry the click and their children carry the words.
func describe(n *Node) string {
	// A password field's text is the password. Android puts the typed value
	// straight into the `text` attribute of a node it has already marked
	// `password="true"`, so `map` printed it — found on Aegis, where typing
	// into the master-password field made the next map read
	// `@e2 Sup3rSecret! (input)`.
	//
	// `map` is broadcast output: it goes to agent transcripts, CI logs, MCP
	// responses and anywhere a user pipes it, and nobody consented to that.
	// The platform marked the field; honor the marking. The resource id is
	// used instead because it is stable, descriptive and distinguishes two
	// password fields on one screen — a confirm field is otherwise
	// indistinguishable from the first.
	if n.Password {
		if id := n.ShortTestID(); id != "" {
			return id
		}
		if label := clean(n.Label); label != "" {
			return label
		}
		return "password field"
	}

	text := onceIfTwice(clean(n.Text))
	label := onceIfTwice(clean(n.Label))

	// Android's idiom is a short visible text plus a fuller content-desc
	// ("Continue" / "Continue with Google"). Two buttons reading "Continue"
	// are useless to an agent, so the more specific string wins.
	if text != "" && label != "" && len(label) > len(text) && strings.Contains(label, text) {
		return truncate(label)
	}
	if text != "" {
		return truncate(text)
	}
	if label != "" {
		return truncate(label)
	}
	// An empty field is named by its hint. Native Android puts the hint in
	// the text and says showing-hint, so it arrives above; Flutter reports
	// the hint in its own attribute with no text, and the Username field of
	// a Flutter form mapped as "EditText" (CHALLENGES 206).
	if h := clean(n.Hint); h != "" {
		return truncate(h)
	}
	// A scroll container's descendants are its contents, not its name —
	// borrowing their text produces labels like "Continue Continue".
	if !n.Scrollable {
		if s := descendantText(n, 3); s != "" {
			return truncate(s)
		}
	}
	if s := n.ShortTestID(); s != "" {
		return s
	}
	return n.ShortClass()
}

// onceIfTwice is a label that is one phrase said twice, said once. WebKit
// names a link from everything in it, and Kiwix's article tiles hold a
// picture whose alt text is the article's title, then the title: "America
// the Beautiful America the Beautiful". Only an exact repeat, split at the
// middle space; the printed label only — a locator still matches the whole.
// CHALLENGES 246.
func onceIfTwice(s string) string {
	if half := len(s) / 2; len(s)%2 == 1 && s[half] == ' ' && s[:half] == s[half+1:] {
		return s[:half]
	}
	return s
}

// tagPattern matches an HTML/XML tag. Deliberately narrow: the name must start
// with a letter, a slash or a bang, so arithmetic like "a < b" and "5<6>7" is
// left alone.
var tagPattern = regexp.MustCompile(`<[a-zA-Z/!][^>]*>`)

// whitespaceRun collapses the gaps left behind once tags are removed.
var whitespaceRun = regexp.MustCompile(`\s+`)

// clean strips markup an app put in its own text or content description.
//
// Apps do this. Wikipedia's feed card carries
// `<span lang="en" dir="ltr"><span class="mw-page-title-main">Thomas Hardy…`
// in an accessibility string, and passing it through means an agent reads tag
// soup and a `text=` locator has to spell out the markup to match. Only strings
// that actually contain a tag have tags removed.
//
// Whitespace is collapsed in every string, tag or none. `map` prints one
// element per line, and a label with a newline in it prints as two: a
// Wikipedia search result on a real iPhone read "Delilah S. Dawson Redirected
// from: Ava Lovelace" on one line and "American author (born 1977) (button)"
// on the next, which splits one entry into two for anything reading the
// output line by line. CHALLENGES 79.
func clean(s string) string {
	s = strings.TrimSpace(whitespaceRun.ReplaceAllString(s, " "))
	if s == "" || !strings.Contains(s, "<") || !tagPattern.MatchString(s) {
		return s
	}
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(tagPattern.ReplaceAllString(s, " "), " "))
}

// truncate bounds a label at maxLabel runes, breaking on a word where it can
// so the result reads as a phrase rather than a severed word.
func truncate(s string) string {
	r := []rune(s)
	if len(r) <= maxLabel {
		return s
	}
	cut := string(r[:maxLabel])
	// Only back up to a space if one is reasonably near the end; otherwise a
	// label with no spaces would lose most of its budget.
	if i := strings.LastIndex(cut, " "); i > maxLabel*3/4 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:.-") + "…"
}

// descendantText collects the text of a node's descendants up to maxDepth,
// joined the way it reads on screen.
//
// Bounded in three ways, each of which a real app broke. Depth was always
// capped; breadth and total length were not, so one Wikipedia feed card —
// a title, Save and Share buttons, a subtitle and a full article lede — came
// back as a single 876-rune label. And the parts repeat: a search field
// rendered as "Search Wikipedia Search Wikipedia Voice input search", because
// the hint text and the content description say the same thing.
func descendantText(n *Node, maxDepth int) string {
	var parts []string
	seen := map[string]bool{}
	total := 0

	var walk func(*Node, int)
	walk = func(p *Node, depth int) {
		if depth <= 0 || total >= maxLabel {
			return
		}
		for _, c := range p.Children {
			if total >= maxLabel {
				return
			}
			// A leaf VoiceOver does not read is not part of the name: a
			// disclosure arrow, a separator. A container that is not read
			// can still hold text that is, so only leaves are skipped.
			if c.NotAccessible && len(c.Children) == 0 {
				continue
			}
			// Nor is a control inside it, which map lists under its own
			// name: Pocket Casts' Discover rows read "Machine Gods NPR
			// Follow" beside the Follow button's own entry, and Ice Cubes'
			// posts ended "Reply Boost Favorite status.action.context-menu".
			// Shown or not — a post running behind the tab bar has its
			// buttons reported hidden, and they are no more its words.
			// Nor an iOS image: the name WebDriverAgent reports for one is
			// its asset's when the app gave it none, and a podcast's header
			// read "chevron-small-down star-full star-half 4.9". CHALLENGES 237.
			if Actionable(c) || c.shaped || c.Class == "XCUIElementTypeImage" {
				continue
			}
			s := clean(c.Text)
			if s == "" {
				s = clean(c.Label)
			}
			if s == "" {
				walk(c, depth-1)
				continue
			}
			// A card repeats its title in the text and the content
			// description; printing it twice helps nobody.
			if seen[s] {
				continue
			}
			seen[s] = true
			parts = append(parts, s)
			total += len([]rune(s)) + 1
		}
	}
	walk(n, maxDepth)
	return strings.Join(parts, " ")
}

// RoleOf names the most specific role a node satisfies, for callers outside
// this package. The tool layer needs it to tell a radio from a checkbox: one
// of the two cannot be unchecked.
func RoleOf(n *Node) string { return roleOf(n) }

// roleOf names the most specific role a node satisfies, for map output.
func roleOf(n *Node) string {
	// Before "input", because a password field is one and the more specific
	// answer is the useful one.
	if n.Password {
		return "password"
	}
	// iOS names a link as an element type, so it is reported as one. Android
	// is left out on purpose: there a "link" is any tappable text view, which
	// is every icon on the launcher (defect 1).
	if IsIOS(n) && HasRole(n, "link") {
		return "link"
	}
	for _, r := range []string{"input", "checkbox", "switch", "radio", "slider", "adjustable", "button", "image", "list", "tab"} {
		if HasRole(n, r) {
			return r
		}
	}
	if n.Scrollable {
		return "list"
	}
	if n.Clickable {
		return "button"
	}
	return ""
}

// MailboxLabel names the field Mobium's gray-box library adds in a
// gray-box launch, which app_hook writes a call into.
const MailboxLabel = "mobium-mailbox"

// IsMailbox reports whether n is that field. It is Mobium's own plumbing,
// not part of the app: map leaves it out, so no agent or test is offered it
// as something to tap or type into.
func IsMailbox(n *Node) bool {
	return n.Label == MailboxLabel || n.TestID == MailboxLabel
}

// Map builds the @ref table for a snapshot. Refs are assigned in document
// order, which is top-to-bottom, left-to-right on screen.
func (t *Tree) Map() []Entry {
	var actionable []*Node
	t.Walk(func(n *Node) bool {
		if Actionable(n) && !IsMailbox(n) {
			actionable = append(actionable, n)
		}
		return true
	})

	var out []Entry
	for i, n := range collapseByBounds(actionable) {
		e := Entry{
			Ref:      fmt.Sprintf("@e%d", i+1),
			Label:    n.label,
			Role:     roleOf(n.node),
			Locator:  Derive(n.node, t),
			Bounds:   n.node.Bounds,
			Selected: n.node.Selected,
			Disabled: !n.node.Enabled,
			Node:     n.node,
		}
		if e.Role == "slider" || e.Role == "adjustable" {
			e.Value = n.node.Value
		}
		// Only for things that have a state to report. The platform says
		// which: Android marks them `checkable`, and the iOS parser sets the
		// same flag for the element types that carry a value.
		if n.node.Checkable {
			checked := n.node.Checked
			e.Checked = &checked
		}
		if a := t.AimAt(n.node); a.Blocker != nil {
			e.Covered = Describe(a.Blocker)
		}
		out = append(out, e)
	}
	return out
}

// chosen is one map entry's node paired with the label picked for it, which
// may have come from a different node in the same collapsed group.
type chosen struct {
	node  *Node
	label string
}

// collapseByBounds merges actionable nodes that occupy exactly the same
// rectangle into one entry.
//
// Real screens stack them: the launcher's "At a glance" widget is a
// long-clickable ViewPager wrapping a clickable ViewGroup on identical bounds,
// and an app's submit button is routinely a clickable wrapper around a
// clickable button. Listing both is noise — a tap lands in the same place
// either way — and the two rows disagree about the label.
//
// The tap target kept is the innermost clickable node, since that is what the
// platform dispatches to; the label kept is the most specific one any node in
// the group carries in its own text or content-desc, rather than one borrowed
// from a descendant. (A group's outer node may accept a long-press the inner
// one does not; that distinction returns when a longpress command needs it.)
//
// "Exactly" allows nearlyTheSame pixels for a node nested in another:
// Jetpack Compose wraps an icon button in a long-clickable tooltip box one
// pixel off the button's own bounds — [32,95][158,221] around
// [33,96][159,222] on Seal — and every icon button then mapped twice, once
// bare and once as a button (CHALLENGES 176). Only ancestor and descendant
// merge, so two neighbors that happen to line up stay two.
func collapseByBounds(nodes []*Node) []chosen {
	groups := map[Rect][]*Node{}
	var order []Rect
	for _, n := range nodes {
		key := n.Bounds
		if _, seen := groups[key]; !seen {
			for _, r := range order {
				if nearlyTheSameRect(r, key) && nested(groups[r], n) {
					key = r
					break
				}
			}
		}
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], n)
	}

	out := make([]chosen, 0, len(order))
	for _, r := range order {
		group := groups[r]
		out = append(out, chosen{node: tapTarget(group), label: bestLabel(group)})
	}
	return out
}

// nearlyTheSame is how far apart, in pixels, two nested nodes' edges may be
// and still be one target.
const nearlyTheSame = 2

func nearlyTheSameRect(a, b Rect) bool {
	d := func(x, y int) bool { return x-y <= nearlyTheSame && y-x <= nearlyTheSame }
	return d(a.X1, b.X1) && d(a.Y1, b.Y1) && d(a.X2, b.X2) && d(a.Y2, b.Y2)
}

// nested reports whether n is an ancestor or a descendant of a node in group.
func nested(group []*Node, n *Node) bool {
	for _, g := range group {
		if isAncestor(g, n) || isAncestor(n, g) {
			return true
		}
	}
	return false
}

func isAncestor(a, n *Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p == a {
			return true
		}
	}
	return false
}

// tapTarget picks the node a tap should resolve to: the innermost clickable
// node, falling back to the innermost node of any kind.
func tapTarget(group []*Node) *Node {
	best := group[0]
	for _, n := range group {
		if n.Clickable && (!best.Clickable || n.Depth > best.Depth) {
			best = n
		} else if !best.Clickable && n.Depth > best.Depth {
			best = n
		}
	}
	return best
}

// bestLabel prefers a name a node carries itself over one inferred from its
// descendants — "At a glance" over the "Fri, Sep 11" its card happens to show.
func bestLabel(group []*Node) string {
	for _, n := range group {
		if strings.TrimSpace(n.Text) != "" || strings.TrimSpace(n.Label) != "" {
			return describe(n)
		}
	}
	return describe(tapTarget(group))
}

// Redact returns what may safely be shown for a node's text.
//
// A password field's text *is* the password: Android puts the typed value into
// the `text` attribute of a node it has already marked `password="true"`, and
// iOS uses XCUIElementTypeSecureTextField. The platform says it is a secret;
// everything here that prints text honors that, because all of it ends up in
// agent transcripts, CI logs and MCP responses.
//
// The length is kept, because the real question an agent asks after typing is
// "did that land", and the answer to that is not secret. What cannot be
// distinguished is a filled field from an empty one showing its placeholder,
// since both platforms put the placeholder where the text goes. Both also say
// which: UiAutomator2 with `showing-hint`, and WebDriverAgent by reporting
// the placeholder in plain text where typed text would be bullets. An empty
// field is said to be empty; MobiumApp's was reported as an 8-character
// password, its placeholder being "password" (CHALLENGES 102). The dump
// backend reports neither, so there it is masked like anything else; losing
// a hint is cheap, and leaking a password is not.
func Redact(n *Node) (string, bool) {
	if !n.Password {
		return "", false
	}
	raw := strings.TrimSpace(n.Text)
	if raw == "" {
		return "", false
	}
	if n.ShowingHint {
		return "(empty, showing its placeholder)", true
	}
	k := len([]rune(raw))
	return fmt.Sprintf("%s (%d characters, hidden: this is a password field)", strings.Repeat("•", k), k), true
}

func (t *Tree) Text() string {
	var lines []string
	seen := map[string]bool{}
	t.Walk(func(n *Node) bool {
		if n.Bounds.Empty() {
			return true
		}
		s := strings.TrimSpace(n.Text)
		if masked, yes := Redact(n); yes {
			s = masked
		}
		if s == "" {
			s = strings.TrimSpace(n.Label)
		}
		if s == "" || seen[s] {
			return true
		}
		seen[s] = true
		lines = append(lines, s)
		return true
	})
	return strings.Join(lines, "\n")
}

// Dialog returns the dialog over the screen when the hierarchy holds one
// alongside what it covers, or nil.
//
// iOS puts an app's own alert, and a sheet such as "Save Password?", into the
// app's tree as XCUIElementTypeAlert or XCUIElementTypeSheet, and keeps every
// element underneath — reporting each one not visible. A locator could
// resolve one of those, and a tap on it landed on the dialog while reporting
// success. Android, and SpringBoard's prompts on iOS, give the dialog's window
// alone, with nothing underneath to resolve, so this finds nothing there.
func (t *Tree) Dialog() *Node {
	if w := t.floatingWindow(); w != nil {
		return w
	}
	if s := t.autofillSave(); s != nil {
		return s
	}
	var found *Node
	t.Walk(func(n *Node) bool {
		if found != nil {
			return false
		}
		if n.Displayed && (n.Class == "XCUIElementTypeAlert" || n.Class == "XCUIElementTypeSheet") {
			found = n
			return false
		}
		return true
	})
	return found
}

// Popover returns a popover in front of an iOS app, or nil. It is not a
// Dialog: nothing answers it, and a tip such as Pocket Casts' "Add bookmark"
// has no button at all — it closes when touched outside it. But iOS reports
// everything behind one hidden, so a screen with a popover up maps as only
// what is on it. CHALLENGES 235.
func (t *Tree) Popover() *Node {
	var found *Node
	t.Walk(func(n *Node) bool {
		if found != nil {
			return false
		}
		if n.Displayed && n.Class == "XCUIElementTypePopover" {
			found = n
			return false
		}
		return true
	})
	return found
}

// floatingWindow is, on Android, the window the hierarchy holds when that
// window does not fill the screen: a dialog. UiAutomator2 gives the dialog's
// window alone, and its W3C alert endpoint recognizes only the framework's
// AlertDialog by its resource ids, so a Jetpack Compose dialog — Seal's
// "User guide", [120,474][960,1937] on a 1080x2400 screen — was not a dialog
// to anything in Mobium (CHALLENGES 177). An app's own window fills the
// screen, edge to edge or not; a dialog, a bottom sheet or a popup does not.

// autofillSave is Android's own offer to save what was typed into a form —
// "Save password to Google Password Manager", Never (Not now the first time
// the phone sees an app), Save and a close button —
// which the autofill framework draws in a window of package `android` that
// fills the screen, the sheet itself over its lower half. So it is not a
// floating window, and the W3C alert endpoint does not know it either: on the
// Pixel 8 Pro it came up over a login and `app_alert` said no dialog was on
// screen (CHALLENGES 206, ROADMAP). Known by the platform's own resource id for
// it, which no app chooses.
func (t *Tree) autofillSave() *Node {
	var found *Node
	t.Walk(func(n *Node) bool {
		if found != nil {
			return false
		}
		if n.TestID == "android:id/autofill_save" && !n.Bounds.Empty() {
			found = n
			return false
		}
		return true
	})
	return found
}

func encloses(outer, inner Rect) bool {
	return outer.X1 <= inner.X1 && outer.Y1 <= inner.Y1 && outer.X2 >= inner.X2 && outer.Y2 >= inner.Y2
}
func (t *Tree) floatingWindow() *Node {
	if t.Screen.Empty() || t.Root == nil {
		return nil
	}
	for _, w := range t.Root.Children {
		if w.Bounds.Empty() {
			continue
		}
		// Covers, not equals: a device can report the display without its
		// navigation bar, 1080x2201, under an app window of 1080x2400.
		if !encloses(w.Bounds, t.Screen) {
			return w
		}
		return nil
	}
	return nil
}

// Within reports whether n is anc or lies inside it.
func (n *Node) Within(anc *Node) bool {
	for p := n; p != nil; p = p.Parent {
		if p == anc {
			return true
		}
	}
	return false
}

// Keyboard returns the on-screen keyboard when the hierarchy holds one, or
// nil. Only iOS reports it: WebDriverAgent puts XCUIElementTypeKeyboard in
// the app's tree, while UiAutomator2 shows only the app's window and leaves
// out whatever the keyboard covers.
//
// A keyboard that is present but off the bottom of the screen is not one:
// with a hardware keyboard attached, a simulator keeps the software keyboard
// in the tree below the screen's edge (y=952 on an 874-point screen).
func (t *Tree) Keyboard() *Node {
	var screen Rect
	var found *Node
	t.Walk(func(n *Node) bool {
		if found != nil {
			return false
		}
		if screen.Empty() && !n.Bounds.Empty() {
			screen = n.Bounds
		}
		if n.Class == "XCUIElementTypeKeyboard" && n.Displayed && !n.Bounds.Empty() && n.Bounds.Y1 < screen.Y2 {
			found = n
			return false
		}
		return true
	})
	return found
}

// CoveredByScreen reports whether n is on a screen of the app that another
// screen has been put in front of: a full-screen view, a sheet the app
// presents itself. iOS keeps the screen behind in the tree, and a tap on a
// target there lands on what is in front while reporting success — Ice
// Cubes' Timeline tab, with its image viewer and again its Add Account
// sheet up. Neither is an XCUIElementTypeAlert or Sheet, so Dialog does
// not find them.
//
// Two things together, because neither alone is enough. The target is
// reported not visible — but so is a target under a pass-through view,
// which a tap does reach (MobiumApp's Obstruction Demo). And it is inside
// a plain view the size of the screen, itself reported not visible: the
// screen behind. Not a list, which reports itself not visible on Settings'
// root with nothing in front of it, and not by the view alone: a share
// sheet's own buttons sit, visible, inside one. Across the captured
// hierarchies the two together hold for every target behind a presented
// screen or a system dialog, and for none on the obstruction screens or an
// ordinary one. Only iOS reports hidden nodes; Android leaves them out.
func (t *Tree) CoveredByScreen(n *Node) bool {
	if n == nil || n.Displayed || !isIOSClass(n.Class) {
		return false
	}
	var screen Rect
	t.Walk(func(m *Node) bool {
		if !m.Bounds.Empty() {
			screen = m.Bounds
			return false
		}
		return true
	})
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Class == "XCUIElementTypeOther" && !p.Displayed && p.Bounds.Width() >= screen.Width() &&
			p.Bounds.Height() >= screen.Height() {
			return true
		}
	}
	return false
}

// OutsidePoint finds a point on the screen in front where nothing is drawn
// but the full-screen layer behind what it shows: the backdrop of a menu.
// A menu has no close control — Ice Cubes' post menus, both the "…" one
// and a long press's — and closes when touched outside it, which is how a
// person closes one; but nothing in the tree is that outside, so a
// refusal that said "tap its close control" named something that is not
// there. ok is false when no such point exists: a full-screen viewer, whose
// content is the whole screen.
//
// The screen behind is reported not visible, so the visible nodes are the
// front screen's. A point is outside when every visible node over it is the
// app, a window or a plain view the size of the screen — not the innermost
// one alone: iOS lays a transparent full-screen window over everything,
// and asked that way a point on a sheet's row read as outside the sheet.
// Inside a sheet a list or a row is always over the point, so a sheet,
// which closes by its own control, is offered none. Searched from the
// bottom up,
// clear of the top and bottom edges, where the status bar and the home
// indicator are.
func (t *Tree) OutsidePoint() (x, y int, ok bool) {
	var screen Rect
	t.Walk(func(m *Node) bool {
		if !m.Bounds.Empty() {
			screen = m.Bounds
			return false
		}
		return true
	})
	if screen.Empty() {
		return 0, 0, false
	}
	backdrop := func(n *Node) bool {
		switch n.Class {
		case "XCUIElementTypeApplication", "XCUIElementTypeWindow":
			return true
		}
		return n.Class == "XCUIElementTypeOther" && !n.Scrollable &&
			n.Bounds.Width() >= screen.Width() && n.Bounds.Height() >= screen.Height()
	}
	const cols, rows = 7, 20
	for r := rows - 2; r >= 2; r-- {
		for c := 0; c < cols; c++ {
			px := screen.X1 + (2*c+1)*screen.Width()/(2*cols)
			py := screen.Y1 + (2*r+1)*screen.Height()/(2*rows)
			clear := true
			t.Walk(func(n *Node) bool {
				if clear && n.Displayed && !n.Bounds.Empty() && contains(n.Bounds, px, py) && !backdrop(n) {
					clear = false
				}
				return clear
			})
			if clear {
				return px, py, true
			}
		}
	}
	return 0, 0, false
}

// Covers reports whether r sits over the center of n.
func (r Rect) Covers(n *Node) bool {
	x, y := n.Bounds.Center()
	return x >= r.X1 && x < r.X2 && y >= r.Y1 && y < r.Y2
}
