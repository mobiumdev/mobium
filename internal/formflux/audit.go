package formflux

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// An accessibility audit of an Android screen, from the hierarchy Mobium
// already reads.
//
// Android has no audit a tool outside the app can run: Google's Accessibility
// Test Framework is a library inside an instrumented test, and Accessibility
// Scanner is an app a person drives. Most of the framework's checks need
// nothing but the hierarchy, though, and those are run here under the
// framework's own names, so a finding reads the same as Scanner's would. What
// needs the pixels — text and image contrast — is not checked, and the
// result says so rather than reading as a pass.

// AuditRule names a check, as the Accessibility Test Framework names it
// without its "Check" suffix.
type AuditRule string

const (
	// RuleTouchTargetSize is a control smaller than 48dp either way.
	RuleTouchTargetSize AuditRule = "TouchTargetSize"
	// RuleSpeakableTextPresent is a control TalkBack can say nothing for:
	// no text and no content description, in it or anything inside it. A
	// resource id is not spoken, so it does not count — the one place this
	// differs from app_screen inspect's "unlabeled", which asks whether a
	// test can find the control rather than whether a person can hear it.
	RuleSpeakableTextPresent AuditRule = "SpeakableTextPresent"
	// RuleDuplicateSpeakableText is two or more controls that say the same
	// thing, so a screen reader user cannot tell them apart.
	RuleDuplicateSpeakableText AuditRule = "DuplicateSpeakableText"
	// RuleDuplicateClickableBounds is two controls on one rectangle: a touch
	// there reaches one of them, and a screen reader stops on both.
	RuleDuplicateClickableBounds AuditRule = "DuplicateClickableBounds"
	// RuleRedundantDescription is a description that says what kind of
	// control it is, which TalkBack says again after it: "Save button,
	// button".
	RuleRedundantDescription AuditRule = "RedundantDescription"
	// RuleEditableContentDesc is a text field with a content description,
	// which TalkBack reads in place of what was typed.
	RuleEditableContentDesc AuditRule = "EditableContentDesc"
	// RuleLabelInName is a control whose description leaves out the words it
	// shows (WCAG 2.5.3): a person using speech says what they see, and
	// "tap Send" finds nothing on a button named "Submit form". Not one of
	// the framework's checks, so it takes WCAG's name. Android only: there
	// the shown text and the description are separate in the tree, where on
	// iOS one label usually stands for both.
	RuleLabelInName AuditRule = "LabelInName"
)

// AuditFinding is one thing an Android audit found.
type AuditFinding struct {
	Rule    AuditRule
	Summary string // what is wrong, in a sentence
	Detail  string // the measurement that makes it so
	Node    *uitree.Node
}

// Element names the finding's control as a person would: its kind and what
// it says, a password field's text never among it.
func (f AuditFinding) Element() string {
	return fmt.Sprintf("%s %q", f.Node.ShortClass(), describe(f.Node))
}

// Locator finds the finding's control, as map's locators do, when it has a
// name to find it by.
func (f AuditFinding) Locator() string { return locator(f.Node) }

// roleWords are what TalkBack says after a control's name for its kind,
// by the end of the class name that makes it that kind.
var roleWords = []struct {
	class string
	words *regexp.Regexp
}{
	{"RadioButton", regexp.MustCompile(`(?i)(^|[^-\w])radio( button)?$`)},
	{"CheckBox", regexp.MustCompile(`(?i)(^|[^-\w])check ?box$`)},
	{"Switch", regexp.MustCompile(`(?i)(^|[^-\w])(switch|toggle)$`)},
	{"ToggleButton", regexp.MustCompile(`(?i)(^|[^-\w])(toggle|button)$`)},
	{"Button", regexp.MustCompile(`(?i)(^|[^-\w])button$`)},
	{"SeekBar", regexp.MustCompile(`(?i)(^|[^-\w])slider$`)},
	{"EditText", regexp.MustCompile(`(?i)(^|[^-\w])(edit box|text field)$`)},
}

// redundantRole is the word for its own kind that a description ends with —
// "Save button" on a button — or empty. Only the end, and only the
// control's own kind: "One-button alert" and "Prompt with a text field" are
// names of buttons, and a check that flagged them was crying wolf on
// MobiumApp's Dialog Demo.
func redundantRole(n *uitree.Node, desc string) string {
	desc = strings.TrimRight(strings.TrimSpace(desc), ".!:")
	for _, r := range roleWords {
		if !strings.HasSuffix(n.Class, r.class) {
			continue
		}
		if m := r.words.FindString(desc); m != "" {
			return strings.ToLower(strings.TrimSpace(m))
		}
		return ""
	}
	return ""
}

// AuditAndroid runs the checks on t, a screen of screen device pixels at dpi.
// With no density there is no dp, and the touch target check is left out
// rather than judged against a guessed threshold.
func AuditAndroid(t *uitree.Tree, screen uitree.Rect, dpi int) []AuditFinding {
	if t == nil || t.Root == nil {
		return nil
	}
	minTouch := minTouchPixels(dpi, Android)
	var out []AuditFinding
	var controls []*uitree.Node

	walk(t.Root, func(n *uitree.Node) {
		if !n.Displayed || n.Bounds.Empty() || !onScreen(n.Bounds, screen) {
			return
		}
		control := (n.Clickable || n.LongClickable) && n.Enabled
		if control {
			controls = append(controls, n)
		}

		if control && minTouch > 0 {
			w, h := n.Bounds.Width(), n.Bounds.Height()
			cutW, cutH := clippedByScroll(n)
			if (w < minTouch && !cutW) || (h < minTouch && !cutH) {
				out = append(out, AuditFinding{
					Rule: RuleTouchTargetSize, Node: n,
					Summary: "a control smaller than 48dp",
					Detail: fmt.Sprintf("%dx%ddp (%dx%dpx at %ddpi), below Android's 48dp minimum",
						dp(w, dpi), dp(h, dpi), w, h, dpi),
				})
			}
		}

		if control && spoken(n) == "" && !namedAbove(n) {
			out = append(out, AuditFinding{
				Rule: RuleSpeakableTextPresent, Node: n,
				Summary: "a control with nothing for a screen reader to say",
				Detail:  "no text and no content description, in it or anything inside it",
			})
		}

		if desc := strings.TrimSpace(n.Label); desc != "" && (control || n.Checkable) {
			if w := redundantRole(n, desc); w != "" {
				out = append(out, AuditFinding{
					Rule: RuleRedundantDescription, Node: n,
					Summary: "a description that names the kind of control",
					Detail:  fmt.Sprintf("%q ends with %q, which TalkBack says again after it", trimTo(desc, 60), w),
				})
			}
		}

		if control && !editable(n) && !n.Password {
			if shown := shownText(n); shown != "" && !containsWords(n.Label, shown) {
				out = append(out, AuditFinding{
					Rule: RuleLabelInName, Node: n,
					Summary: "a name without the words shown",
					Detail: fmt.Sprintf("shows %q and is named %q: a person who says what they see is not understood",
						trimTo(shown, 60), trimTo(strings.TrimSpace(n.Label), 60)),
				})
			}
		}

		if strings.TrimSpace(n.Label) != "" && editable(n) {
			out = append(out, AuditFinding{
				Rule: RuleEditableContentDesc, Node: n,
				Summary: "a text field with a content description",
				Detail:  "TalkBack reads the description in place of the field's text; use a hint or a label instead",
			})
		}
	})

	out = append(collapse(out), duplicates(controls)...)

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		a, b := out[i].Node.Bounds, out[j].Node.Bounds
		if a.Y1 != b.Y1 {
			return a.Y1 < b.Y1
		}
		return a.X1 < b.X1
	})
	return out
}

// duplicates finds controls that say the same thing, and controls on the
// same rectangle. A control inside another on the same rectangle is the
// common case for the second — a row and its own button — and each pair is
// reported once, on the inner one.
func duplicates(controls []*uitree.Node) []AuditFinding {
	var out []AuditFinding
	byName := map[string][]*uitree.Node{}
	var names []string
	byRect := map[uitree.Rect]*uitree.Node{}
	for _, n := range controls {
		if other, ok := byRect[n.Bounds]; ok {
			out = append(out, AuditFinding{
				Rule: RuleDuplicateClickableBounds, Node: n,
				Summary: "two controls on one rectangle",
				Detail: fmt.Sprintf("%s and %s are both clickable at %s",
					describe(other), describe(n), n.Bounds),
			})
		} else {
			byRect[n.Bounds] = n
		}
		name := strings.ToLower(strings.Join(strings.Fields(spoken(n)), " "))
		if name == "" {
			continue
		}
		if _, ok := byName[name]; !ok {
			names = append(names, name)
		}
		byName[name] = append(byName[name], n)
	}
	// One finding for each name, on the first control that says it: a list
	// of rows each with its own "Delete" is one thing to fix, and as one
	// finding a row it was 46 on MobiumApp's Table Demo.
	for _, name := range names {
		ns := distinctRects(byName[name])
		if len(ns) < 2 {
			continue
		}
		out = append(out, AuditFinding{
			Rule: RuleDuplicateSpeakableText, Node: ns[0],
			Summary: "controls that say the same thing",
			Detail:  fmt.Sprintf("%d controls on this screen say %q", len(ns), trimTo(spoken(ns[0]), 60)),
		})
	}
	return out
}

// collapse makes one finding of the same rule on look-alike controls — every
// row's "More" too small the same way — with the count in its detail. Rows
// are one thing to fix, and listed row by row they buried the rest: 22 of
// 25 findings on MobiumApp's Table Demo.
func collapse(fs []AuditFinding) []AuditFinding {
	type key struct {
		rule            AuditRule
		locator, detail string
	}
	count := map[key]int{}
	first := map[key]int{}
	var out []AuditFinding
	for _, f := range fs {
		loc := locator(f.Node)
		if loc == "" {
			out = append(out, f)
			continue
		}
		k := key{f.Rule, loc, f.Detail}
		if _, ok := first[k]; !ok {
			first[k] = len(out)
			out = append(out, f)
		}
		count[k]++
	}
	for k, n := range count {
		if n > 1 {
			out[first[k]].Detail += fmt.Sprintf(" (%d controls like it on this screen)", n)
		}
	}
	return out
}

// distinctRects drops a control whose rectangle another in ns already has:
// one control drawn as two nested views says its name once.
func distinctRects(ns []*uitree.Node) []*uitree.Node {
	seen := map[uitree.Rect]bool{}
	var out []*uitree.Node
	for _, n := range ns {
		if seen[n.Bounds] {
			continue
		}
		seen[n.Bounds] = true
		out = append(out, n)
	}
	return out
}

// spoken is what a screen reader has to say for n: its own description or
// text, else what the views inside it say that are not controls of their own,
// which TalkBack reads as part of it. A password field's text is never read
// out here, and it is not spoken either: its description or nothing.
func spoken(n *uitree.Node) string {
	if l := strings.TrimSpace(n.Label); l != "" {
		return l
	}
	if n.Password {
		return strings.TrimSpace(n.Hint)
	}
	if t := strings.TrimSpace(n.Text); t != "" {
		return t
	}
	if h := strings.TrimSpace(n.Hint); h != "" {
		return h
	}
	var parts []string
	for _, c := range n.Children {
		if !c.Displayed || ((c.Clickable || c.LongClickable) && c.Enabled) {
			continue
		}
		if s := spoken(c); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// namedAbove says whether a control around n, on the same rectangle, has a
// name: the launcher's At a Glance card is a nameless control inside a named
// one, and a screen reader says the named one's name for that rectangle. The
// pair is reported once, as DuplicateClickableBounds.
func namedAbove(n *uitree.Node) bool {
	for p := n.Parent; p != nil && p.Bounds == n.Bounds; p = p.Parent {
		if (p.Clickable || p.LongClickable) && p.Enabled && spoken(p) != "" {
			return true
		}
	}
	return false
}

// shownText is the one piece of text a control shows as its caption — its
// own text, or the one text inside it that is not a control of its own —
// when it also has a description that might leave it out. Empty when there
// is no description, no caption, or more than one text: a card's several
// lines have no single caption to be named by. A caption with no letter or
// digit is an icon drawn with a glyph, and says no word.
func shownText(n *uitree.Node) string {
	if strings.TrimSpace(n.Label) == "" {
		return ""
	}
	var texts []string
	if t := strings.TrimSpace(n.Text); t != "" && !(n.Checkable && stateWord(t)) {
		texts = append(texts, t)
	}
	var under func(*uitree.Node)
	under = func(p *uitree.Node) {
		for _, c := range p.Children {
			if !c.Displayed || ((c.Clickable || c.LongClickable) && c.Enabled) {
				continue
			}
			if t := strings.TrimSpace(c.Text); t != "" {
				texts = append(texts, t)
			}
			under(c)
		}
	}
	under(n)
	if len(texts) != 1 || len(words(texts[0])) == 0 {
		return ""
	}
	return texts[0]
}

// stateWord is a switch's own text when it shows its state, not a caption.
func stateWord(t string) bool {
	switch strings.ToLower(t) {
	case "on", "off":
		return true
	}
	return false
}

// containsWords says whether name holds the words of shown, in order and
// next to each other, whatever their case and punctuation: "Send message"
// holds "Send", "Submit form" does not, and "Resend" does not hold "Send".
func containsWords(name, shown string) bool {
	n, w := words(name), words(shown)
	for i := 0; i+len(w) <= len(n); i++ {
		match := true
		for j := range w {
			if n[i+j] != w[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// words splits s into lower-case words of letters and digits.
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// editable is a text field, by class.
func editable(n *uitree.Node) bool {
	return strings.HasSuffix(n.Class, "EditText") || strings.HasSuffix(n.Class, "AutoCompleteTextView")
}

// onScreen says whether r overlaps the screen at all: a row the list holds
// below the fold is no finding about this screen.
func onScreen(r, screen uitree.Rect) bool {
	if screen.Empty() {
		return true
	}
	return r.X1 < screen.X2 && r.X2 > screen.X1 && r.Y1 < screen.Y2 && r.Y2 > screen.Y1
}
