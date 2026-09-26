package formflux

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// A Finding is something wrong with a layout, at one screen size.
//
// The bar for adding one is high and deliberately so. Changing the screen and
// reporting that the element count changed is a signal, not a verdict — it
// tells an operator to go and look, which is the work they wanted to avoid.
// A finding has to name a specific element and a specific thing wrong with it,
// and it has to be wrong rather than merely unusual, because a check that
// cries wolf on every screen gets turned off and then finds nothing at all.
type Finding struct {
	Kind    Kind
	Label   string      // what the element is, as a person would say it
	Bounds  uitree.Rect // where it is, in device pixels
	Detail  string      // the measurement that makes this a finding
	Locator string      // how to look at it, when the node gave one

	// Path is the sibling-index path, and it is here because without it two
	// findings on two different unlabeled containers print identically and
	// neither can be acted on. It identifies a node within one snapshot and
	// not across snapshots — which is exactly the limit Compare runs into.
	Path string
}

// Kind is what sort of thing is wrong.
type Kind string

const (
	// KindOverflow is an element extending past the left or right edge.
	//
	// Horizontal only, on purpose. Vertically, everything below the fold of a
	// scrollable list is outside the screen and entirely correct — Android
	// reports those nodes with real bounds and this project has a rule about
	// not confusing that with unreachability. Horizontal overflow has no such
	// innocent explanation on a phone: nothing scrolls sideways by accident,
	// so a control past the right edge is a control nobody can press.
	KindOverflow Kind = "overflow"

	// KindTinyTarget is a tappable element below the platform's minimum touch
	// size. Android asks for 48dp, iOS for 44pt. Both are guidelines their own
	// designers publish, and both convert to pixels through the density this
	// package is already changing — which is the point: raise the density and
	// targets that were fine become too small.
	//
	// **This is the one kind here that can be wrong about a working screen.**
	// Android's TouchDelegate expands a view's touch area without changing
	// its bounds, and the delegate is not in the hierarchy, so a control can
	// be comfortably tappable while measuring 30px. Google's own launcher
	// trips this on its date widget.
	//
	// It is kept because the opposite error is worse — a control that really
	// is too small is invisible to every other check — but it is reported as
	// something to look at rather than as a defect, and anything acting on
	// these findings should say so.
	KindTinyTarget Kind = "tiny-target"

	// KindTruncated is text the platform had to cut, which it signals by
	// ending the string with an ellipsis. Evidence rather than inference: the
	// platform did the truncating and said so.
	KindTruncated Kind = "truncated"

	// KindUnlabeled is a tappable *leaf* with nothing to announce: no text, no
	// accessibility label, no id, and no descendant carrying any of those.
	//
	// The leaf part is load-bearing and was learned by running this against
	// Settings, where it first reported seven identical findings on clickable
	// LinearLayouts whose children held all the text. That claim was simply
	// wrong — Android composes a container's announcement from its children,
	// so those rows read out fine. A check that is wrong about a working
	// screen is worse than no check, so the rule is now narrow enough to be
	// true: nothing anywhere underneath it either.
	KindUnlabeled Kind = "unlabeled"
)

// Minimum touch target, in Android's density-independent pixels.
//
// iOS's equivalent is 44pt and is deliberately not here: see minTouchPixels.
const androidMinTouchDP = 48

func (f Finding) String() string {
	label := f.Label
	if label == "" {
		label = "(unlabeled)"
	}
	// The locator is what makes a finding actionable; the path is the
	// fallback when the element gave nothing to match on, and saying "at
	// 0/0/3/1" beats printing the same line four times.
	where := f.Locator
	if where == "" && f.Path != "" {
		where = "at " + f.Path
	}
	if where != "" {
		return fmt.Sprintf("%-11s %s — %s  [%s]", f.Kind, label, f.Detail, where)
	}
	return fmt.Sprintf("%-11s %s — %s", f.Kind, label, f.Detail)
}

// Inspect walks a snapshot and reports what is wrong with it at this screen.
//
// screen is the size the tree was captured at, and dpi its density; dpi may be
// zero on iOS, where the touch-target check uses points and the tree is
// already in them.
func Inspect(t *uitree.Tree, screen uitree.Rect, dpi int, platform Platform) []Finding {
	if t == nil || t.Root == nil {
		return nil
	}
	var out []Finding
	seen := map[string]bool{}

	add := func(f Finding) {
		// One element can be reported once per kind and no more. Containers
		// repeat their child's text on both platforms, so without this a
		// single truncated string arrives three times and the report reads
		// like three problems.
		key := string(f.Kind) + "\x00" + f.Label + "\x00" + f.Bounds.String() + "\x00" + f.Path
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, f)
	}

	minTouch := minTouchPixels(dpi, platform)

	walk(t.Root, func(n *uitree.Node) {
		if !n.Displayed || n.Bounds.Empty() {
			return
		}
		name := describe(n)

		// Overflow. Only the horizontal axis, and only for something a person
		// is meant to reach: a decorative image bleeding off the edge is a
		// design, not a defect.
		if n.Clickable || n.Focusable {
			if n.Bounds.X2 > screen.X2 || n.Bounds.X1 < screen.X1 {
				add(Finding{
					Kind: KindOverflow, Label: name, Bounds: n.Bounds,
					Detail: fmt.Sprintf("extends to x=%d..%d on a screen %d wide",
						n.Bounds.X1, n.Bounds.X2, screen.X2-screen.X1),
					Locator: locator(n), Path: n.Path,
				})
			}
		}

		// Touch target.
		if n.Clickable && n.Enabled && minTouch > 0 {
			w, h := n.Bounds.Width(), n.Bounds.Height()
			if w < minTouch || h < minTouch {
				add(Finding{
					Kind: KindTinyTarget, Label: name, Bounds: n.Bounds,
					Detail: fmt.Sprintf("%dx%dpx, below the %dpx minimum (%s)",
						w, h, minTouch, touchRule(platform)),
					Locator: locator(n), Path: n.Path,
				})
			}
		}

		// Truncation, as reported by the platform rather than guessed at.
		if s := truncatedText(n); s != "" {
			add(Finding{
				Kind: KindTruncated, Label: name, Bounds: n.Bounds,
				Detail:  fmt.Sprintf("the platform cut it: %q", s),
				Locator: locator(n), Path: n.Path,
			})
		}

		// Unlabeled tappable, and only when nothing underneath it carries a
		// name either — see KindUnlabeled.
		if n.Clickable && n.Enabled && !announceable(n) {
			add(Finding{
				Kind: KindUnlabeled, Label: n.ShortClass(), Bounds: n.Bounds,
				Detail:  "tappable, and neither it nor anything inside it has text, a label or an id",
				Locator: locator(n), Path: n.Path,
			})
		}
	})

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// minTouchPixels converts the platform's guideline into device pixels.
//
// Returns 0 when it cannot be computed rather than guessing a number: on
// Android without a density there is no conversion, and a made-up threshold
// would produce confident findings about nothing.
func minTouchPixels(dpi int, platform Platform) int {
	switch platform {
	case Android:
		if dpi <= 0 {
			return 0
		}
		// Android's dp is defined against a 160dpi baseline.
		return androidMinTouchDP * dpi / 160
	case IOS:
		// Not checked, and this is a correction rather than an omission.
		//
		// It first returned 44 on the assumption that an iOS hierarchy is in
		// points. It is not: mobium normalizes iOS coordinates to device
		// pixels, so an iPhone 17 Pro reports 1206x2622 and not 402x874. The
		// check was therefore comparing pixels against points and was wrong
		// by the scale factor — it called Apple's own 39px status-bar items
		// undersized when they are 13pt, which is small, but not for the
		// reason given.
		//
		// The fix needs the device's point-to-pixel scale, which the WDA
		// driver reads but does not expose. Reaching it means a real
		// capability on the driver seam rather than a bare type assertion.
		// Until then this returns 0 and the check does not run,
		// because a threshold off by 3x produces confident findings about
		// working screens, which is worse than finding nothing.
		return 0
	}
	return 0
}

func touchRule(platform Platform) string {
	if platform == IOS {
		return "iOS asks for 44pt"
	}
	return "Android asks for 48dp"
}

// truncatedText returns the string the platform cut, or empty.
//
// An ellipsis anywhere but the end is punctuation — "Wait… what?" is not a
// truncation — so only a trailing one counts, and a bare "…" on its own is an
// overflow menu rather than a cut label.
func truncatedText(n *uitree.Node) string {
	if n.Password {
		return ""
	}
	for _, s := range []string{n.Text, n.Label} {
		s = strings.TrimRight(strings.TrimSpace(s), " ")
		if s == "" || s == "…" || s == "..." {
			continue
		}
		if strings.HasSuffix(s, "…") || strings.HasSuffix(s, "...") {
			return s
		}
	}
	return ""
}

// announceable reports whether this node or anything under it carries
// something a screen reader could say.
func announceable(n *uitree.Node) bool {
	if strings.TrimSpace(n.Text) != "" ||
		strings.TrimSpace(n.Label) != "" ||
		strings.TrimSpace(n.TestID) != "" {
		return true
	}
	for _, c := range n.Children {
		if announceable(c) {
			return true
		}
	}
	return false
}

// walk visits every node, depth first. uitree has no exported walker and this
// package has no business adding one to it for a single caller.
func walk(n *uitree.Node, fn func(*uitree.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		walk(c, fn)
	}
}

func describe(n *uitree.Node) string {
	// A password field's own text must never reach a report. Findings are
	// printed, logged and pasted into issues, which is exactly how CHALLENGES
	// 43 happened the first time.
	if masked, ok := uitree.Redact(n); ok {
		return masked
	}
	for _, s := range []string{n.Label, n.Text, n.ShortTestID()} {
		if t := strings.TrimSpace(s); t != "" {
			return trimTo(t, 60)
		}
	}
	return n.ShortClass()
}

func locator(n *uitree.Node) string {
	if n.Password {
		// The id is safe; the text is not, and a password field's value must
		// not become a locator.
		if id := n.ShortTestID(); id != "" {
			return "testid=" + id
		}
		return "role=password"
	}
	if id := n.ShortTestID(); id != "" {
		return "testid=" + id
	}
	if l := strings.TrimSpace(n.Label); l != "" {
		return "label=" + trimTo(l, 60)
	}
	if t := strings.TrimSpace(n.Text); t != "" {
		return "text=" + trimTo(t, 60)
	}
	return ""
}

func trimTo(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Compare reports what is wrong at one screen that was not wrong at another.
//
// The useful question is rarely "what is wrong at 720x1520" but "what is wrong
// there that was fine at 1080x2400", because the second is a layout that does
// not survive a smaller screen and the first includes everything that was
// already broken everywhere.
//
// # What it cannot do
//
// Findings are matched by kind and locator, and an element with no locator has
// no identity that survives a change of screen — its path shifts when the
// layout reflows, which is the whole reason the layout is being tested. So two
// unlabeled containers at two sizes cannot be told apart, and this will
// sometimes call an old problem new.
//
// Said plainly rather than papered over: the answer is trustworthy for
// anything with an id or a label, and indicative for anything without.
func Compare(baseline, other []Finding) (added []Finding) {
	key := func(f Finding) string {
		if f.Locator != "" {
			return string(f.Kind) + "\x00" + f.Locator
		}
		return string(f.Kind) + "\x00" + f.Label + "\x00" + f.Detail
	}
	was := map[string]bool{}
	for _, f := range baseline {
		was[key(f)] = true
	}
	for _, f := range other {
		if !was[key(f)] {
			added = append(added, f)
		}
	}
	return added
}
