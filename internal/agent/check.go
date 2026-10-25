package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/uitree"
	"github.com/mobiumdev/mobium/internal/webview"
)

// check is app_set: put a checkbox or switch into a state, rather than
// toggling whatever it is in.
//
// The distinction is the whole tool. Tapping a checkbox flips it, so a caller
// that wants it *on* has to read the state, compare, tap only if it differs,
// and read again to be sure — four round trips, on every checkbox, written out
// by hand every time. This is that, done once and verified.
//
// Idempotent on purpose: asking for a state it is already in does nothing and
// says so. That is what makes it safe to call without checking first, which is
// the point of having it at all.
func (h *Handlers) check(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.checkOn(ctx, s, args)
}

// checkOn is app_set once the device is resolved.
func (h *Handlers) checkOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	target := stringArg(args, "target")
	if target == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_set needs a target (\"@e2\" or \"testid=terms\")")
	}
	want := true
	if raw, ok := args["value"]; ok {
		b, isBool := raw.(bool)
		if !isBool {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "value must be true or false")
		}
		want = b
	}
	if s.web != nil {
		return h.checkWeb(ctx, s, target, want)
	}

	resolved, _, err := h.resolveNode(ctx, s, target)
	if err != nil {
		return nil, err
	}
	node := stateOf(resolved)

	// Refuse anything with no state to set. Tapping a button and reporting it
	// as checked would be the shape this project exists to not have — and the
	// platform already says which elements have one.
	if !node.Checkable {
		// Part of a WebView's page, the native tree may carry no checked
		// state for a checkbox the page has; the page does (CHALLENGES 299).
		if inWebView(node) {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is part of a WebView's page, and the native "+
				"tree gives it no checked state — in the WebView's context app_set reads the page's own", target).
				WithRemedy("app_contexts, then app_context with the WebView's name, app_map, and app_set its ref")
		}
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not a checkbox, radio or switch, so it has no "+
			"checked state to set — app_tap is what acts on it", target)
	}

	// A radio cannot be unchecked. A group is cleared by choosing a different
	// member, and tapping a selected radio does nothing, so obeying literally
	// would leave the caller waiting for a change that is not coming.
	role := uitree.RoleOf(node)
	if !want && role == "radio" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "a radio button cannot be unchecked — choose a different "+
			"one in the group instead, which clears %s", target)
	}

	view := CheckView{Target: target, Role: role, Checked: node.Checked, Wanted: want}
	if node.Checked == want {
		view.Changed = false
		return Result(fmt.Sprintf("%s is already %s", target, stateWord(want)), view), nil
	}

	// Aimed like any tap, so a control the app drew over this one is not
	// pressed in its place (CHALLENGES 115).
	aimed, aim, err := h.resolveAim(ctx, s, target)
	if err != nil {
		return nil, err
	}
	if err := h.touch(ctx, s, aim.X, aim.Y, accessibleName(aimed)); err != nil {
		return nil, err
	}

	// Verify by outcome. A tap that lands on a disabled or intercepted
	// control returns perfectly well and changes nothing, and reporting that
	// as success is exactly the failure this tool is meant to remove.
	afterNode, _, err := h.resolveNode(ctx, s, target)
	if err != nil {
		return nil, mobiumerr.New(mobiumerr.NotConfirmed, "tapped %s and could not read it back to confirm: %w", target, err)
	}
	after := stateOf(afterNode)
	if after.Checked != want {
		return nil, mobiumerr.New(mobiumerr.NotConfirmed, "tapped %s to make it %s and it is still %s — the control "+
			"did not respond, so nothing here can make it", target, stateWord(want),
			stateWord(after.Checked))
	}

	view.Checked, view.Changed = after.Checked, true
	return Result(fmt.Sprintf("%s is now %s", target, stateWord(want)), view), nil
}

// checkWeb is app_set inside a WebView: the same reach-a-state, with the
// state read from the page — an input's checked, or aria-checked — rather
// than the native tree, and the tap aimed as app_tap aims one there. It used
// to refuse and send the caller to the native context, where the same
// checkbox was "not a checkbox" (CHALLENGES 299).
func (h *Handlers) checkWeb(ctx context.Context, s *session, target string, want bool) (*ToolsCallResult, error) {
	read := func() (*webview.CheckState, error) {
		_, el, _, err := h.findWeb(ctx, s, target)
		if err != nil {
			return nil, err
		}
		st, err := webview.Checked(ctx, s.web, el)
		if err != nil {
			return nil, err
		}
		if !st.Found {
			return nil, mobiumerr.New(mobiumerr.NoSuchElement, "%s is no longer on the page — the content changed, run app_map again", target)
		}
		return st, nil
	}
	st, err := read()
	if err != nil {
		return nil, err
	}
	if st.Checked == nil {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not a checkbox, radio or switch, so it has no "+
			"checked state to set — app_tap is what acts on it", target)
	}
	if !want && st.Kind == "radio" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "a radio button cannot be unchecked — choose a different "+
			"one in the group instead, which clears %s", target)
	}
	view := CheckView{Target: target, Role: st.Kind, Checked: *st.Checked, Wanted: want}
	if *st.Checked == want {
		return Result(fmt.Sprintf("%s is already %s", target, stateWord(want)), view), nil
	}

	if err := h.awaitAppIdle(ctx, s, target); err != nil {
		return nil, err
	}
	x, y, _, err := h.aimWeb(ctx, s, target)
	if err != nil {
		return nil, err
	}
	if err := h.touch(ctx, s, x, y, ""); err != nil {
		return nil, err
	}

	// Read back, giving the page a moment: a framework re-renders after the
	// event, not during it.
	var after *webview.CheckState
	for i := 0; i < 5; i++ {
		if after, err = read(); err != nil {
			return nil, mobiumerr.New(mobiumerr.NotConfirmed, "tapped %s and could not read it back to confirm: %w", target, err)
		}
		if after.Checked != nil && *after.Checked == want {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if after.Checked == nil || *after.Checked != want {
		return nil, mobiumerr.New(mobiumerr.NotConfirmed, "tapped %s to make it %s and the page still holds it %s — "+
			"the control did not respond, so nothing here can make it", target, stateWord(want), stateWord(!want))
	}
	view.Checked, view.Changed = want, true
	return Result(fmt.Sprintf("%s is now %s in %s", target, stateWord(want), s.webCtx), view), nil
}

// stateOf is the node whose checked state a target means: the target, or,
// when it has none, the row it is the words of. A Jetpack Compose switch row
// is one clickable, checkable node with its label on a child, so
// `check "text=Dynamic color"` found the words and refused them while map
// showed the row checked (CHALLENGES 178). Only the nearest clickable
// ancestor, which is where a tap on the words lands; if that has no state
// either, the target is returned and refused as before.
func stateOf(n *uitree.Node) *uitree.Node {
	if n == nil || n.Checkable {
		return n
	}
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Clickable {
			if p.Checkable {
				return p
			}
			return n
		}
	}
	return n
}

func stateWord(checked bool) string {
	if checked {
		return "checked"
	}
	return "unchecked"
}

// CheckView is the result of app_set.
type CheckView struct {
	Target string `json:"target"`
	Role   string `json:"role,omitempty"`
	// Checked is the state afterwards.
	Checked bool `json:"checked"`
	// Wanted is what was asked for, which equals Checked on success.
	Wanted bool `json:"wanted"`
	// Changed is false when it was already in that state and nothing was
	// tapped, which is the idempotent case rather than a failure.
	Changed bool `json:"changed"`
}
