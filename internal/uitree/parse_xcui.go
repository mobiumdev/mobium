package uitree

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// This file is deliberately not named parse_ios.go: Go reads a trailing _ios
// as a GOOS build constraint and would exclude it from every build that is
// not targeting iOS, silently. The same trap waits for _android, _windows and
// friends.

// ParseIOS parses a WebDriverAgent /source hierarchy.
//
// The shape differs from Android in three ways that matter, all taken from
// WDA's own source-generation tests rather than guessed:
//
//   - the element name is the XCUIElementType ("XCUIElementTypeButton")
//   - geometry is separate x/y/width/height attributes, not a bounds string
//   - visibility is reported directly, as `visible` and `hittable`
//
// Field mapping: `name` is the accessibility identifier and behaves like
// Android's resource-id; `label` is the accessibility label; `value` is the
// element's current value, which is where a text field's contents live.
func ParseIOS(data []byte) (*Tree, error) {
	if i := bytes.IndexByte(data, '<'); i > 0 {
		data = data[i:]
	} else if i < 0 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "parse iOS hierarchy: no XML found")
	}

	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }

	root := &Node{Class: "hierarchy", Enabled: true, Displayed: true}
	tree := &Tree{Root: root}
	stack := []*Node{root}
	count := 0

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse iOS hierarchy: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			parent := stack[len(stack)-1]
			n := iosNodeFrom(t, parent, len(parent.Children))
			parent.Children = append(parent.Children, n)
			stack = append(stack, n)
			count++
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if count == 0 {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "parse iOS hierarchy: no elements found")
	}
	if len(root.Children) == 1 {
		root.Bounds = root.Children[0].Bounds
	}
	rebaseRemoteContent(root)
	foldSwitchRows(root)
	markIntrinsicTargets(root)
	return tree, nil
}

// rebaseRemoteContent moves content drawn by another process back to where
// it is on screen.
//
// iOS 26's share sheet is a remote view: Safari hosts a
// `ShareSheet.RemoteContainerView` and another process draws what is in it.
// XCTest reports that content relative to the container, not the screen —
// the container at 9,477, the node under it at 0,0 with the same size, and
// every row below offset by the container's origin. A tap on a row's center
// landed on whatever was that far above it, and reported success: "Add to
// Home Screen" opened Find on Page. Measured on an iPhone 17 Pro simulator
// (iOS 26.5) and a real iPhone 15 Plus (iOS 26.6.2). CHALLENGES 128.
//
// The boundary is recognizable without knowing what hosts it. In screen
// coordinates a node the exact size of its parent, and inside it, has the
// parent's origin; one at 0,0 under a parent that is not is in a coordinate
// space of its own, and the parent's origin is the offset. Nothing is named,
// so another remote view with the same habit is corrected too, and the check
// runs at every depth, so one nested in another is corrected twice.
func rebaseRemoteContent(n *Node) {
	for _, c := range n.Children {
		b, p := c.Bounds, n.Bounds
		if b.X1 == 0 && b.Y1 == 0 && (p.X1 != 0 || p.Y1 != 0) &&
			!b.Empty() && b.Width() == p.Width() && b.Height() == p.Height() {
			shiftSubtree(c, p.X1, p.Y1)
		}
		rebaseRemoteContent(c)
	}
}

// shiftSubtree moves n and everything under it by dx, dy. A node with no
// size is left alone: it is how an off-screen element is reported
// (CHALLENGES 72), and moving it would give it a position it does not have.
func shiftSubtree(n *Node, dx, dy int) {
	if !n.Bounds.Empty() {
		n.Bounds = Rect{X1: n.Bounds.X1 + dx, Y1: n.Bounds.Y1 + dy, X2: n.Bounds.X2 + dx, Y2: n.Bounds.Y2 + dy}
	}
	for _, c := range n.Children {
		shiftSubtree(c, dx, dy)
	}
}

// markIntrinsicTargets makes a card or a link a tap target when the platform
// put `accessible` on the text inside it instead.
//
// `accessible` is the right signal almost everywhere, and cells are where it
// is not. Wikipedia's Explore feed marks the *title text* of each article
// card accessible and the cell around it not, so the thing a user taps — the
// card — had no entry in `map` at all: the featured article was on screen,
// under a finger, and absent. Text is content, not a target, so nothing else
// picked it up. Measured on a real iPhone 15 Plus, iOS 26.6.2. CHALLENGES 78.
//
// UIKit makes every collection and table cell selectable whatever its
// accessibility says, so a visible cell with text in it is a target. Only an
// innermost one: a feed wraps each section in a cell of its own, and treating
// that as a target would put a whole section, and every card in it, under one
// entry. A cell that already is a target — Settings' rows are marked
// accessible — is left as it was.
//
// A link is the same case in a WebView. WebKit publishes a page's links in the
// native hierarchy as XCUIElementTypeLink with real frames, and marks the text
// inside each one accessible rather than the link: a Wikipedia article on the
// phone had 8 visible links, every one `accessible="false"`, and so none in
// `map`. On a phone, where a WebView cannot be attached (yet), those native
// links are the only way to follow one, and a link has no purpose but to be
// tapped.
func markIntrinsicTargets(n *Node) {
	for _, c := range n.Children {
		markIntrinsicTargets(c)
	}
	if n.Clickable || !n.Displayed || n.Bounds.Empty() {
		return
	}
	switch n.Class {
	case "XCUIElementTypeLink":
		if clean(n.Label) != "" || descendantText(n, 3) != "" {
			n.Clickable = true
		}
	case "XCUIElementTypeCell":
		// A row that carries a switch is operated by the switch: tapping the
		// cell's center, beside it, changed nothing (Settings > Motion).
		if !hasDescendantClass(n, "XCUIElementTypeCell") && !hasDescendantClass(n, "XCUIElementTypeSwitch") &&
			descendantText(n, 3) != "" {
			n.Clickable = true
		}
	}
}

// foldSwitchRows makes a switch row one control, aimed at its toggle.
//
// iOS 26's Settings draws a row as a switch inside a switch. The outer one is
// the whole row, and carries the name, the ID and the state; the inner one is
// the toggle, with no name at all. A tap lands on a node's center, and the
// outer one's center is on the words, which do nothing when touched — so the
// entry an agent would choose could not flip the switch, and the one that
// could was named "XCUIElementTypeSwitch" and reachable only by path.
// Measured on an iPhone 15 Plus (iOS 26.6.2) and an iPhone 17 Pro simulator
// (iOS 26.5).
//
// The outer switch takes the toggle's frame, because that is where it can be
// operated, and the toggle stops being a target of its own. Everything that
// aims at a node aims at its bounds, so this is the one place to say it.
func foldSwitchRows(n *Node) {
	for _, c := range n.Children {
		foldSwitchRows(c)
	}
	if n.Class != "XCUIElementTypeSwitch" {
		return
	}
	toggle := innerSwitch(n)
	if toggle == nil || toggle.Bounds.Empty() {
		return
	}
	n.Bounds = toggle.Bounds
	toggle.Clickable = false
	toggle.Checkable = false
}

// innerSwitch finds an unnamed switch inside n, the toggle of a switch row.
func innerSwitch(n *Node) *Node {
	for _, c := range n.Children {
		if c.Class == "XCUIElementTypeSwitch" && c.Label == "" && c.TestID == "" {
			return c
		}
		if s := innerSwitch(c); s != nil {
			return s
		}
	}
	return nil
}

func hasDescendantClass(n *Node, class string) bool {
	for _, c := range n.Children {
		if c.Class == class || hasDescendantClass(c, class) {
			return true
		}
	}
	return false
}

func iosNodeFrom(e xml.StartElement, parent *Node, sibling int) *Node {
	path := strconv.Itoa(sibling)
	if parent.Parent != nil || parent.Path != "" {
		path = parent.Path + "/" + path
	}

	// The element name carries the type; the `type` attribute repeats it.
	class := attr(e, "type")
	if class == "" {
		class = e.Name.Local
	}

	n := &Node{
		Text:    textValue(class, attr(e, "value"), attr(e, "label")),
		Hint:    attr(e, "placeholderValue"),
		Package: attr(e, "bundleId"),
		Label:   attr(e, "label"),
		TestID:  attr(e, "name"),
		Class:   class,
		Bounds:  iosBounds(e),
		Depth:   parent.Depth + 1,
		Index:   atoiOr(attr(e, "index"), sibling),
		Parent:  parent,
		Path:    path,
		Enabled: attr(e, "enabled") != "false",
		// WDA omits `visible` on some element kinds; absent means "assume
		// visible" so a whole screen is not filtered away, matching how the
		// Android dump format is treated.
		Displayed: attr(e, "visible") != "false",
		// WebDriverAgent reports selection in the accessibility traits —
		// traits="Selected, Button" on a search scope that is chosen — and
		// leaves `selected` out, so read both.
		Selected:      attr(e, "selected") == "true" || hasTrait(attr(e, "traits"), "Selected"),
		NotAccessible: attr(e, "accessible") == "false",
	}
	if parent.Parent == nil {
		n.Depth = 0
	}

	// iOS has no `clickable` attribute, and `hittable` — which WebDriverAgent
	// documents — is simply absent from a real simulator's output. What it
	// does report is `accessible`, the platform's own answer to "is this one
	// distinct thing a user deals with", and that turns out to be the right
	// signal: on the home screen it selects the app icons and the search
	// pill and nothing else.
	//
	// The first version of this file whitelisted element types instead and
	// mapped zero elements, because the home screen is 23 XCUIElementTypeIcon
	// and that type was not on the list. Enumerating what counts is the same
	// mistake `role=link` was on Android; asking the platform is not.
	accessible := attr(e, "accessible") == "true"
	n.Clickable = accessible && n.Displayed && !iosContentTypes[class]
	// A section header is accessible, so VoiceOver can land on it, and is
	// typed Other, which says nothing — but its traits say Header, and it
	// is a heading, not a control: NetNewsWire's Settings mapped "Accounts"
	// and "Feeds" as buttons. Unless the app also gave it the Button trait.
	if traits := attr(e, "traits"); hasTrait(traits, "Header") && !hasTrait(traits, "Button") {
		n.Clickable = false
	}
	n.Checkable, n.Checked = checkedState(class, attr(e, "value"))
	n.DeclaredRole = declaredRole(attr(e, "value"))
	n.Scrollable = iosScrollableTypes[class]
	n.Focusable = iosInputTypes[class]
	n.Password = class == "XCUIElementTypeSecureTextField"
	// An empty field's value is its placeholder, in plain text even on a
	// secure field, whose typed value is bullets — so equality is the flag
	// UiAutomator2 sends as showing-hint.
	n.ShowingHint = n.Hint != "" && n.Text == n.Hint
	return n
}

// iosBounds reads the separate geometry attributes WDA emits.
func iosBounds(e xml.StartElement) Rect {
	x := atoiOr(attr(e, "x"), 0)
	y := atoiOr(attr(e, "y"), 0)
	w := atoiOr(attr(e, "width"), 0)
	h := atoiOr(attr(e, "height"), 0)
	return Rect{X1: x, Y1: y, X2: x + w, Y2: y + h}
}

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}

// iosContentTypes are the types that carry information rather than accept
// interaction. Everything else the platform marks accessible is treated as
// actionable.
//
// Containers are here because a Window or Application marked accessible is a
// structural artifact, not something to tap.
var iosContentTypes = map[string]bool{
	"XCUIElementTypeStaticText":    true,
	"XCUIElementTypeImage":         true,
	"XCUIElementTypeApplication":   true,
	"XCUIElementTypeWindow":        true,
	"XCUIElementTypeStatusBar":     true,
	"XCUIElementTypeNavigationBar": true,
	"XCUIElementTypeToolbar":       true,
	"XCUIElementTypeTabBar":        true,
	"XCUIElementTypeKeyboard":      true,
}

var iosScrollableTypes = map[string]bool{
	"XCUIElementTypeScrollView":     true,
	"XCUIElementTypeTable":          true,
	"XCUIElementTypeCollectionView": true,
}

var iosInputTypes = map[string]bool{
	"XCUIElementTypeTextField":       true,
	"XCUIElementTypeSecureTextField": true,
	"XCUIElementTypeSearchField":     true,
	"XCUIElementTypeTextView":        true,
}

// stateValue matches the accessibility value a custom control composes for
// itself: the role it claims, then its state. React Native writes exactly
// this for `accessibilityRole="checkbox"` with `accessibilityState={{checked}}`
// — measured as `value="checkbox, checked"` and `value="radio button,
// unchecked"` — and it is the string VoiceOver would speak.
var stateValue = regexp.MustCompile(`^(?i)(checkbox|radio button|radio|switch|toggle), (checked|unchecked)$`)

// textValue decides whether an element's `value` is its text.
//
// It usually is: a text field's contents live there, which is why the parser
// reads it at all. But a value is not always text, and treating it as one put
// nonsense in front of every caller.
//
//   - A Switch's value is "0" or "1". Using it as text made `map` print
//     `@e7 0 (switch)` for a control whose label was "Dark mode" — and that
//     has been true of every iOS switch since the backend was written, not
//     only of ours.
//
//   - A custom checkbox's value is its role and state, "checkbox, checked".
//     Using it as text made `map` print `@e2 checkbox, unchecked (button)`
//     and hid the label entirely.
//
//   - A selected button's value is "1". Wikipedia's onboarding offers two
//     choices as plain buttons, and the chosen one carries `value="1"` and
//     the Selected trait while the other has no value at all — so `map`
//     printed `@e2 1 (button)` for "Community-related content" beside a
//     correctly labeled "Personalized content". Measured on a real iPhone.
//     A bare 0 or 1 on anything that is not a text field, next to a real
//     label, is a state rather than a name. A text field is exempt: "1" is
//     exactly what a quantity field holds.
//
// In each case the element carries a perfectly good `label`, which is what
// `describe` falls back to once this returns empty. CHALLENGES 65, 77.
func textValue(class, value, label string) string {
	if class == "XCUIElementTypeSwitch" || stateValue.MatchString(value) {
		return ""
	}
	if (value == "0" || value == "1") && label != "" && !iosInputTypes[class] {
		return ""
	}
	return value
}

// checkedState reads whether an element has a checked state, and what it is.
//
// Two spellings, because the platform has two. A real Switch reports "0" or
// "1"; a custom control reports the phrase VoiceOver speaks. Android needs
// neither — it has `checkable` and `checked` attributes of its own — which is
// the asymmetry this function exists to absorb.
func checkedState(class, value string) (checkable, checked bool) {
	if class == "XCUIElementTypeSwitch" {
		return true, value == "1"
	}
	if m := stateValue.FindStringSubmatch(value); m != nil {
		return true, strings.EqualFold(m[2], "checked")
	}
	return false, false
}

// declaredRole reads the role a custom control claims in its accessibility
// value, normalized to mobium's vocabulary.
//
// "radio button" is what VoiceOver says and "radio" is what the locator
// vocabulary calls it; mapping the two here keeps `role=radio` meaning the
// same thing on both platforms, which is the whole point of having one
// vocabulary compiled per platform rather than a dialect each.
func declaredRole(value string) string {
	m := stateValue.FindStringSubmatch(value)
	if m == nil {
		return ""
	}
	switch strings.ToLower(m[1]) {
	case "checkbox":
		return "checkbox"
	case "radio", "radio button":
		return "radio"
	case "switch", "toggle":
		return "switch"
	}
	return ""
}

// hasTrait reports whether WebDriverAgent's traits attribute — a comma-
// separated list such as "Selected, Button" — names trait.
func hasTrait(traits, trait string) bool {
	for _, t := range strings.Split(traits, ",") {
		if strings.TrimSpace(t) == trait {
			return true
		}
	}
	return false
}
