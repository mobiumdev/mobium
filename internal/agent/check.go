package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"

	"github.com/mobiumdev/mobium/internal/uitree"
)

// check is app_check: put a checkbox or switch into a state, rather than
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

// checkOn is app_check once the device is resolved.
func (h *Handlers) checkOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	if s.web != nil {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "app_check works on the native context — "+
			"switch back with app_context NATIVE_APP")
	}
	target := stringArg(args, "target")
	if target == "" {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_check needs a target (\"@e2\" or \"testid=terms\")")
	}
	want := true
	if raw, ok := args["checked"]; ok {
		b, isBool := raw.(bool)
		if !isBool {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "checked must be true or false")
		}
		want = b
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
	_, aim, err := h.resolveAim(ctx, s, target)
	if err != nil {
		return nil, err
	}
	if err := s.driver.Tap(ctx, aim.X, aim.Y); err != nil {
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

// CheckView is the result of app_check.
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
