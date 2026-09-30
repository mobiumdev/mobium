package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// pollInterval is the pause between snapshots while waiting.
//
// A snapshot costs 0.04s on UiAutomator2 and a couple of seconds on the dump
// backend, so this is a floor rather than a period: the fast backend polls
// roughly four times a second, and the slow one polls as fast as it can.
const pollInterval = 250 * time.Millisecond

// implicitWait is how long an action retries before giving up on its target.
//
// Every action re-resolves its target against a fresh snapshot, which means
// every action can lose a race with an animation or a screen that has not
// finished settling. Retrying briefly absorbs that; it is the reason a caller
// does not have to sleep before each tap.
//
// It is deliberately short. A wait long enough to cover a network round trip
// belongs in an explicit app_wait_for, where the timeout is visible in the
// call and the failure says what was being waited for.
const implicitWait = 2 * time.Second

// settleWindow is the gap between the two readings that decide whether an
// element is holding still, and settleTimeout bounds how long it may keep
// moving before an action gives up on it.
//
// 100ms is six frames at 60Hz — long enough that anything actually animating
// has visibly moved, short enough not to be felt. Five seconds covers a
// deliberate animation, not only a screen transition: two seconds did, only
// while UiAutomator2 blocked every read until the screen was idle, and once
// that wait was capped (CHALLENGES 109) a tap on MobiumApp's two-second
// slide-in gave up as its last pixels crept into place (CHALLENGES 112).
// Five seconds covers a deliberate animation. A thing still moving after
// that is animating on purpose — confetti, a ticking clock — and is
// refused rather than chased.
const (
	settleWindow  = 100 * time.Millisecond
	settleTimeout = 5 * time.Second
)

// Bounds on an explicit wait. The maximum exists because callTimeout bounds
// the whole tool call: a wait allowed to exceed it would be killed by the
// deadline and report a context error instead of naming what never appeared.
const (
	defaultWaitTimeout = 10 * time.Second
	maxWaitTimeout     = 120 * time.Second
)

// errPollTimeout means the condition never held. Callers replace it with a
// message naming what they were waiting for; it is never returned to a user.
var errPollTimeout = mobiumerr.New(mobiumerr.Timeout, "poll timed out")

// pollUntil calls attempt until it reports success, the timeout expires, or
// ctx is canceled.
//
// attempt returns (true, nil) when the condition holds and (false, nil) when
// it does not yet — the caller records why in its own closure, so the eventual
// error can name the real reason rather than "timed out". A non-nil error is
// fatal and stops the poll immediately: it means the device could not be
// asked, which waiting will not fix.
//
// attempt always runs at least once, so a zero timeout is a single try rather
// than an instant failure.
func pollUntil(ctx context.Context, timeout time.Duration, attempt func(context.Context) (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		ok, err := attempt(ctx)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errPollTimeout
		}
		pause := pollInterval
		if remaining < pause {
			pause = remaining
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pause):
		}
	}
}

// Wait conditions.
const (
	condVisible  = "visible"
	condHidden   = "hidden"
	condText     = "text"
	condEnabled  = "enabled"
	condDisabled = "disabled"
	// Checked and unchecked are a checkbox, radio or switch's state, read as
	// app_check reads it; focused is keyboard focus; value is a field's whole
	// content, exactly, where text is a part of what an element says.
	condChecked   = "checked"
	condUnchecked = "unchecked"
	condFocused   = "focused"
	condValue     = "value"
	// Count is how many elements the locator matches on screen.
	condCount = "count"
)

// waitConditions is every condition, in the order an error lists them.
var waitConditions = []string{condVisible, condHidden, condText, condValue, condEnabled, condDisabled,
	condChecked, condUnchecked, condFocused, condCount}

// waitFor is app_wait_for: block until the screen says what the caller is
// waiting for, or fail naming what it was.
func (h *Handlers) waitFor(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.waitOn(ctx, s, args)
}

// waitOn is app_wait_for once the device is resolved. Split out so the wait
// itself can be exercised against a scripted screen without a device.
func (h *Handlers) waitOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	if s.web != nil {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "app_wait_for works on the native context — "+
			"switch back with app_context native, or poll the page with app_map")
	}

	target := stringArg(args, "target")
	if target == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_wait_for needs a target (\"@e5\" or \"text=Sign In\")")
	}
	cond := strings.ToLower(stringArg(args, "condition"))
	if cond == "" {
		cond = condVisible
	}
	want := stringArg(args, "text")
	_, hasText := args["text"]
	negate, _, err := boolParam(args, "not")
	if err != nil {
		return nil, err
	}
	exact, _, err := boolParam(args, "exact")
	if err != nil {
		return nil, err
	}
	if exact && cond != condText {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "exact goes with condition \"text\"; value is always exact")
	}
	count := -1
	var focus mobiumdriver.FocusReader
	switch cond {
	case condVisible, condHidden, condEnabled, condDisabled, condChecked, condUnchecked:
	case condText:
		if want == "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "condition \"text\" needs the text to wait for")
		}
	case condValue:
		// An empty value is a question worth asking — has the field been
		// cleared — so only a missing one is refused.
		if !hasText {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "condition \"value\" needs the value to wait for, "+
				"in text; \"\" waits for an empty field")
		}
	case condCount:
		f, err := floatArg(args, "count")
		if err != nil || f < 0 || f != float64(int(f)) {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "condition \"count\" needs count, a whole number "+
				"of matches, 0 or more")
		}
		count = int(f)
	case condFocused:
		f, ok := mobiumdriver.AsFocusReader(s.driver)
		if !ok {
			return nil, mobiumerr.New(mobiumerr.Unsupported, "the %s driver cannot say which element has keyboard "+
				"focus, so a wait for it could never end", s.driver.Name()).
				WithRemedy("wait for something the app shows once the field has focus")
		}
		focus = f
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown condition %q (want one of %s)",
			cond, strings.Join(waitConditions, ", "))
	}

	timeout := time.Duration(intArgOr(args, "timeout_ms", int(defaultWaitTimeout/time.Millisecond))) * time.Millisecond
	if timeout < 0 {
		timeout = 0
	}
	if timeout > maxWaitTimeout {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "timeout_ms of %d exceeds the %s ceiling on a single tool call",
			timeout/time.Millisecond, maxWaitTimeout)
	}

	loc, err := h.locatorFor(s.dev.Serial, target)
	if err != nil {
		return nil, err
	}

	// saw is the last thing the screen said, kept so a timeout can report
	// what was actually there instead of only what was wanted.
	var saw string
	var matched *uitree.Node
	var tree *uitree.Tree
	started := time.Now()
	answered := 0

	err = pollUntil(ctx, timeout, func(ctx context.Context) (bool, error) {
		t, err := s.driver.Snapshot(ctx)
		if err != nil {
			return false, err
		}
		tree = t
		nodes := visible(loc.Resolve(t))
		// What a dialog covers is not on screen, whatever its own flag says:
		// iOS keeps the covered screen in the tree, and under iOS's "Save
		// Password?" sheet a target read visible here while every action and
		// read refused it; Android's tree holds the dialog's window alone, so
		// there the same wait timed out. The predicate is pickOne's, so wait
		// and act agree on both platforms. CHALLENGES 145.
		notOnScreen := "it is not on screen"
		if d := t.Dialog(); d != nil {
			var shown []*uitree.Node
			for _, n := range nodes {
				if n.Within(d) {
					shown = append(shown, n)
				}
			}
			if len(nodes) > 0 && len(shown) == 0 {
				notOnScreen = fmt.Sprintf("it is under a dialog, %q — answer it first", strings.TrimSpace(strings.SplitN(d.Label, "\n", 2)[0]))
			}
			nodes = shown
		}
		// A declared rule answers a dialog in a wait's way as it does in an
		// action's, as Playwright's locator handlers run for an assertion
		// too: login.test.json's rule for "Save Password?" answered it in
		// front of a tap and not in front of the wait for the next screen,
		// which timed out on a real iPhone. Not for a wait until the target
		// is gone, which a dialog over it already satisfies. On Android the
		// tree holds the dialog's window alone, so an absent target asks too.
		if !negate && len(nodes) == 0 && answered < maxDialogsPerCall && len(h.dialogRules[s.dev.Serial]) > 0 {
			handled, herr := h.answerByRule(ctx, s)
			if herr != nil {
				return false, herr
			}
			if handled {
				answered++
				return false, nil
			}
		}
		holds, match, desc, err := judge(ctx, cond, nodes, notOnScreen, loc, want, exact, count, focus)
		if err != nil {
			return false, err
		}
		if holds != negate {
			if !negate {
				matched = match
			}
			return true, nil
		}
		saw = desc
		return false, nil
	})

	waited := time.Since(started)
	if err != nil {
		if errors.Is(err, errPollTimeout) {
			return nil, mobiumerr.New(mobiumerr.Timeout, "timed out after %s waiting for %s to %s — %s",
				waited.Round(time.Millisecond), loc, phrase(condPhrase(cond, want, exact, count), negate), saw)
		}
		return nil, err
	}

	view := WaitView{
		Target:    target,
		Condition: cond,
		WaitedMs:  int(waited.Milliseconds()),
	}
	state := condState(cond, want, exact, count)
	if negate {
		state = "is no longer " + strings.TrimPrefix(strings.TrimPrefix(state, "is "), "")
		if cond == condText || cond == condValue || cond == condCount {
			state = "no longer " + condState(cond, want, exact, count)
		}
	}
	msg := fmt.Sprintf("%s %s after %s", loc, state, waited.Round(time.Millisecond))

	// A wait that found something leaves a fresh ref table behind, so the
	// element it waited for can be tapped without a separate app_map. This is
	// what makes `wait_for` a replacement for `sleep; map` rather than an
	// extra call in front of it.
	if matched != nil && tree != nil {
		if e, ok := h.rememberRefs(s.dev.Serial, tree, matched); ok {
			ev := elementView(e)
			view.Element = &ev
			msg = fmt.Sprintf("%s — %s", msg, e.Line())
		}
	}
	return Result(msg, view), nil
}

// visible drops matches that occupy no space. An element present in the
// hierarchy but sized zero has not appeared as far as a user is concerned,
// and it cannot be tapped.
func visible(nodes []*uitree.Node) []*uitree.Node {
	out := nodes[:0:0]
	for _, n := range nodes {
		if !n.Bounds.Empty() {
			out = append(out, n)
		}
	}
	return out
}

// condPhrase says what a condition waits for, after "waiting for X to":
// "hold \"\"", "become checked". condState says it held: "is checked".
func condPhrase(cond, want string, exact bool, count int) string {
	switch cond {
	case condText:
		if exact {
			return fmt.Sprintf("say exactly %q", want)
		}
		return fmt.Sprintf("contain %q", want)
	case condValue:
		return fmt.Sprintf("hold %q", want)
	case condFocused:
		return "have keyboard focus"
	case condCount:
		return fmt.Sprintf("match %d on screen", count)
	}
	return "become " + cond
}

// phrase negates a condPhrase: "not contain \"x\"", "stop being visible".
func phrase(p string, negate bool) string {
	if !negate {
		return p
	}
	if rest, ok := strings.CutPrefix(p, "become "); ok {
		return "stop being " + rest
	}
	return "no longer " + p
}

func condState(cond, want string, exact bool, count int) string {
	switch cond {
	case condText:
		if exact {
			return fmt.Sprintf("says exactly %q", want)
		}
		return fmt.Sprintf("contains %q", want)
	case condValue:
		return fmt.Sprintf("holds %q", want)
	case condCount:
		return fmt.Sprintf("matches %d on screen", count)
	}
	return "is " + cond
}

// judge says whether a condition holds for what the locator matched on
// screen, which node satisfied it, and what the screen showed in words — the
// half a timeout reports, whichever way the wait was asked. A non-nil error
// is a refusal: nothing on this screen could ever satisfy it.
func judge(ctx context.Context, cond string, nodes []*uitree.Node, notOnScreen string, loc uitree.Locator,
	want string, exact bool, count int, focus mobiumdriver.FocusReader) (bool, *uitree.Node, string, error) {
	if cond == condCount {
		return len(nodes) == count, nil, fmt.Sprintf("%d match on screen", len(nodes)), nil
	}
	if cond == condHidden {
		if len(nodes) > 0 {
			return false, nil, fmt.Sprintf("%d still on screen", len(nodes)), nil
		}
		return true, nil, notOnScreen, nil
	}
	if len(nodes) == 0 {
		return false, nil, notOnScreen, nil
	}
	n := nodes[0]
	switch cond {
	case condVisible:
		return true, n, "it is on screen", nil
	case condEnabled, condDisabled:
		word := map[bool]string{true: condEnabled, false: condDisabled}[n.Enabled]
		return n.Enabled == (cond == condEnabled), n, "it is " + word, nil
	case condChecked, condUnchecked:
		// The row, when the target is its words, as app_check reads it.
		n = stateOf(n)
		// Refused at once, as app_check refuses it: nothing without a
		// checked state will ever be checked, and waiting says nothing.
		if !n.Checkable {
			return false, nil, "", mobiumerr.New(mobiumerr.InvalidArgument, "%s is not a checkbox, radio or "+
				"switch, so it has no checked state to wait for", loc)
		}
		return n.Checked == (cond == condChecked), n, "it is " + stateWord(n.Checked), nil
	case condFocused:
		has, err := focus.HasFocus(ctx, n)
		if err != nil {
			return false, nil, "", err
		}
		if has {
			return true, n, "it has keyboard focus", nil
		}
		return false, n, "it does not have keyboard focus", nil
	case condValue:
		// Never compared, so that nothing can be learned from it and no
		// failure has a value to print (CHALLENGES 43).
		if n.Password {
			return false, nil, "", mobiumerr.New(mobiumerr.InvalidArgument, "%s is a password field, and its "+
				"value is never read — wait for what the app shows once it is accepted", loc)
		}
		v := fieldValue(n)
		return v == want, n, fmt.Sprintf("its value is %q", v), nil
	}
	// condText
	for _, m := range nodes {
		t := nodeText(m)
		if (exact && t == want) || (!exact && strings.Contains(t, want)) {
			return true, m, fmt.Sprintf("its text is %q", t), nil
		}
	}
	return false, n, fmt.Sprintf("its text is %q", nodeText(n)), nil
}

// fieldValue is what a field holds: its text, and nothing when the text is
// its placeholder, which both platforms put where an empty field's text goes
// (CHALLENGES 102).
func fieldValue(n *uitree.Node) string {
	if n.ShowingHint || (n.Hint != "" && n.Text == n.Hint) {
		return ""
	}
	return n.Text
}

// nodeText is what a caller means by "the text of this element": its own text
// if it has any, otherwise its accessibility label.
func nodeText(n *uitree.Node) string {
	if n.Text != "" {
		return n.Text
	}
	return n.Label
}

// rememberRefs maps a snapshot, stores the ref table for the device, and
// returns the entry a caller can act on for one particular node.
//
// The node that satisfied a wait is often not the node the map kept. A wait
// for text="Network & internet" matches the TextView carrying that text,
// while the map collapses the row and hands out a ref for the clickable
// parent — so an exact match on the node found nothing, and the wait reported
// no element even though the ref table it had just written contained the row.
// Walking up to the nearest mapped ancestor is what makes the returned ref
// the one a caller would have used anyway.
func (h *Handlers) rememberRefs(serial string, tree *uitree.Tree, want *uitree.Node) (uitree.Entry, bool) {
	entries := tree.Map()
	table := &refTable{entries: map[string]uitree.Locator{}, taken: time.Now()}
	byNode := make(map[*uitree.Node]uitree.Entry, len(entries))
	for _, e := range entries {
		table.add(e)
		byNode[e.Node] = e
	}
	h.refs[serial] = table

	for n := want; n != nil; n = n.Parent {
		if e, ok := byNode[n]; ok {
			return e, true
		}
	}
	return uitree.Entry{}, false
}

// WaitView is the result of app_wait_for.
type WaitView struct {
	Target    string `json:"target"`
	Condition string `json:"condition"`
	WaitedMs  int    `json:"waited_ms"`
	// Element is the element that satisfied the wait, absent when waiting for
	// something to go away or when the map filters the match out.
	Element *ElementView `json:"element,omitempty"`
}
