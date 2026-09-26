package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

// orientation is app_orientation: read which way the screen is turned, or
// turn it.
//
// Worth having for the same reason dark mode was: it is a genuinely different
// rendering of every screen, it is where layouts that only exist in one
// orientation go untested, and doing it by hand means finding a different
// toggle on each platform.
func (h *Handlers) orientation(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.orientationOn(ctx, s, args)
}

// orientationOn is app_orientation once the device is resolved.
func (h *Handlers) orientationOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	ctrl, ok := mobiumdriver.AsOrientation(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapOrientation, "read or change the screen orientation")
	}

	want := strings.ToLower(strings.TrimSpace(stringArg(args, "orientation")))
	// Checked here as well as in the driver so a typo never reaches a device
	// and the message is the same whichever backend is in use.
	if want != "" && want != mobiumdriver.OrientationAuto {
		if _, err := mobiumdriver.OrientationQuarter(want); err != nil {
			return nil, err
		}
	}

	before, lockedBefore, err := ctrl.Orientation(ctx)
	if err != nil {
		return nil, err
	}

	if want == "" {
		return Result(describeOrientation(before, lockedBefore),
			OrientationView{Orientation: before, Locked: lockedBefore, Device: s.dev.Serial}), nil
	}

	// "auto" is about the lock, not the angle, so it is never a no-op merely
	// because the screen already points the right way.
	if want != mobiumdriver.OrientationAuto && before == want && lockedBefore {
		return Result(fmt.Sprintf("already %s", want),
			OrientationView{Orientation: want, Locked: true, Device: s.dev.Serial}), nil
	}

	if err := ctrl.SetOrientation(ctx, want); err != nil {
		return nil, err
	}

	// Every ref from before belongs to a screen that has been laid out again.
	// Bounds do not survive a rotation, and a stale ref taps a coordinate that
	// now names something else entirely — worse than having no ref at all.
	delete(h.refs, s.dev.Serial)

	after, locked, err := ctrl.Orientation(ctx)
	if err != nil {
		return nil, err
	}
	msg := describeOrientation(after, locked)
	if after != before {
		msg = fmt.Sprintf("%s (was %s)", msg, before)
	}
	return Result(msg, OrientationView{
		Orientation: after, Locked: locked, Previous: before, Device: s.dev.Serial,
	}), nil
}

// describeOrientation says both facts, because they are different questions:
// which way the screen is pointing, and whether it will stay there.
func describeOrientation(mode string, locked bool) string {
	if locked {
		return mode + " (locked)"
	}
	return mode + " (following the sensor)"
}

// OrientationView is the result of app_orientation.
type OrientationView struct {
	Orientation string `json:"orientation"`
	// Locked distinguishes a screen that is pinned from one that happens to
	// be pointing this way and can turn under you.
	Locked bool `json:"locked"`
	// Previous is set only when the call changed something.
	Previous string `json:"previous,omitempty"`
	Device   string `json:"device"`
}
