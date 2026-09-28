package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"strings"
	"time"

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
)

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
	switch cond {
	case condVisible, condHidden, condEnabled, condDisabled:
	case condText:
		if want == "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "condition \"text\" needs the text to wait for")
		}
	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown condition %q (want %q, %q, %q, %q or %q)",
			cond, condVisible, condHidden, condText, condEnabled, condDisabled)
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
		switch cond {
		case condVisible:
			if len(nodes) == 0 {
				saw = notOnScreen
				return false, nil
			}
			matched = nodes[0]
			return true, nil
		case condHidden:
			if len(nodes) > 0 {
				saw = fmt.Sprintf("%d still on screen", len(nodes))
				return false, nil
			}
			return true, nil
		case condEnabled, condDisabled:
			if len(nodes) == 0 {
				saw = notOnScreen
				return false, nil
			}
			if nodes[0].Enabled == (cond == condEnabled) {
				matched = nodes[0]
				return true, nil
			}
			saw = "it is " + map[bool]string{true: condEnabled, false: condDisabled}[nodes[0].Enabled]
			return false, nil
		default: // condText
			if len(nodes) == 0 {
				saw = notOnScreen
				return false, nil
			}
			for _, n := range nodes {
				if strings.Contains(nodeText(n), want) {
					matched = n
					return true, nil
				}
			}
			saw = fmt.Sprintf("its text is %q", nodeText(nodes[0]))
			return false, nil
		}
	})

	waited := time.Since(started)
	if err != nil {
		if errors.Is(err, errPollTimeout) {
			return nil, mobiumerr.New(mobiumerr.Timeout, "timed out after %s waiting for %s to be %s — %s",
				waited.Round(time.Millisecond), loc, cond, saw)
		}
		return nil, err
	}

	view := WaitView{
		Target:    target,
		Condition: cond,
		WaitedMs:  int(waited.Milliseconds()),
	}
	msg := fmt.Sprintf("%s is %s after %s", loc, cond, waited.Round(time.Millisecond))

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
		table.entries[e.Ref] = e.Locator
		table.lines = append(table.lines, e.Line())
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
