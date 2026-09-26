package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"math"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

const (
	// zoomDuration is how long the fingers travel. Slow enough that both
	// platforms read it as a scale rather than a fling, and slow enough that
	// a view animating to its new scale has started before the call returns.
	zoomDuration = 600 * time.Millisecond

	// nearGap and farGap are the default half-distances between the fingers,
	// in device pixels. A pinch that starts too close is ambiguous with a
	// double-tap and one that ends off the edge of the target does nothing,
	// so both ends are kept well inside a typical view.
	//
	// The gap between them matters more than either number. **A pinch whose
	// fingers travel less than about 100 device pixels each does not register
	// at all** — measured on a Pixel 7 AVD against a WebView: 50px of travel
	// left the scale at exactly 1.0, 100px reached 1.42, 160px reached 2.03.
	// Below the platform's own threshold the gesture is delivered and ignored,
	// which looks identical to a gesture that was never sent.
	//
	// And the same numbers do not mean the same pinch on two devices. These
	// are device pixels, like every coordinate here, so a gap that is a third
	// of a 1080px screen is a ninth of a 3x one — measured, `from 40 to 140`
	// reached 1.44 on the Pixel 7 AVD and 2.71 on an iPhone 17 Pro simulator.
	// That is the three-units rule doing what it always does rather than a
	// fault: size a pinch against the thing being pinched, not against a
	// number that worked somewhere else.
	nearGap = 80
	farGap  = 320
)

// zoom is app_zoom: pinch two fingers apart or together.
//
// The gesture is the easy half. The half worth stating is that **mobium cannot
// tell you whether it worked**: there is no scale in the accessibility
// hierarchy on either platform, so nothing here can read a zoom level back the
// way `check` reads a checkbox. What the tool can honestly report is that the
// gesture was delivered.
//
// That is a real limitation rather than an oversight, and the answer says so.
// Confirming a zoom means asking the thing that was zoomed — a WebView knows
// its own `visualViewport.scale`, and a native view usually exposes nothing at
// all.
func (h *Handlers) zoom(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.zoomOn(ctx, s, args)
}

// zoomOn is app_zoom once the device is resolved.
func (h *Handlers) zoomOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	pincher, ok := mobiumdriver.AsPincher(s.driver)
	if !ok {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "the %s backend cannot put two fingers on the screen, "+
			"so it cannot pinch", s.backend)
	}

	// Either an intent or the gesture itself, which is how app_swipe already
	// divides the same problem: a direction for "scroll the screen", exact
	// coordinates for a particular drag. Here a direction pinches by a
	// sensible default amount, and `from`/`to` say exactly how far the
	// fingers travel — for a pinch sized to an element, or a deliberately
	// small one, neither of which a fixed default can express.
	direction := strings.ToLower(stringArg(args, "direction"))
	from, hasFrom := intArg(args, "from")
	to, hasTo := intArg(args, "to")

	switch {
	case hasFrom != hasTo:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "from and to must be given together — they are the "+
			"half-distance between the fingers at the start and at the end")
	case hasFrom && direction != "":
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "give either a direction or from/to, not both: "+
			"from and to already say which way the fingers move")
	case hasFrom && (from <= 0 || to <= 0):
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "from and to are distances in device pixels and must be "+
			"above zero, got %d and %d", from, to)
	case hasFrom && from == to:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "from and to are both %d, so the fingers would not move — "+
			"that is a two-finger tap rather than a pinch", from)
	}

	if !hasFrom {
		if direction == "" {
			direction = "in"
		}
		if direction != "in" && direction != "out" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "direction must be \"in\" or \"out\", got %q", direction)
		}
	}

	// Center on an element when given one, on the screen otherwise. Pinching
	// about the wrong point scales the right amount around the wrong place,
	// which on a map is indistinguishable from a bug in the app.
	var cx, cy int
	target := stringArg(args, "target")
	switch {
	case target != "":
		node, _, err := h.resolveNode(ctx, s, target)
		if err != nil {
			return nil, err
		}
		cx, cy = node.Bounds.Center()
	default:
		tree, err := s.driver.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		b := tree.Root.Bounds
		cx, cy = b.Center()
	}

	if !hasFrom {
		from, to = nearGap, farGap
		if direction == "out" {
			from, to = farGap, nearGap
		}
	} else {
		// Report the direction the caller implied, so the answer reads the
		// same whichever way it was asked for.
		direction = "in"
		if to < from {
			direction = "out"
		}
	}

	if err := pincher.Pinch(ctx, cx, cy, from, to, zoomDuration); err != nil {
		return nil, err
	}

	// Anything on screen may have moved or resized, and every ref from the
	// last map named it at the old scale.
	delete(h.refs, s.dev.Serial)

	where := "the screen"
	if target != "" {
		where = target
	}
	return Result(fmt.Sprintf("pinched %s about %s at (%d, %d), fingers %d to %dpx "+
		"apart — the gesture was delivered, which is all this can confirm: neither "+
		"platform reports a zoom level in the hierarchy, so ask the thing you zoomed "+
		"(a WebView knows its own visualViewport.scale)",
		direction, where, cx, cy, from*2, to*2),
		ZoomView{Direction: direction, Target: target, X: cx, Y: cy, From: from, To: to}), nil
}

// ZoomView is the result of app_zoom.
type ZoomView struct {
	Direction string `json:"direction"`
	Target    string `json:"target,omitempty"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	// From and To are the half-distance between the fingers, in device
	// pixels, at the start and the end. Reported even when a direction was
	// given, because "it pinched" is not reproducible and a number is.
	From int `json:"from"`
	To   int `json:"to"`
}

// rotateRadius is how far each finger sits from the center, in device pixels.
// Wide enough that the arc between steps is a real movement rather than noise,
// and narrow enough to stay inside an ordinary view.
const rotateRadius = 200

// rotate is app_rotate: turn two fingers about a point.
//
// The same honesty problem as app_zoom, and worse. Neither platform reports a
// rotation anywhere in the accessibility hierarchy, and unlike a zoom there is
// not even a WebView property to ask — a page has to compute the angle from
// raw touch events itself. So this reports that the gesture was delivered and
// says what it cannot confirm.
func (h *Handlers) rotate(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.rotateOn(ctx, s, args)
}

// rotateOn is app_rotate once the device is resolved.
func (h *Handlers) rotateOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	pincher, ok := mobiumdriver.AsPincher(s.driver)
	if !ok {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "the %s backend cannot put two fingers on the screen, "+
			"so it cannot rotate", s.backend)
	}

	degrees := 90.0
	if _, given := args["degrees"]; given {
		d, err := floatArg(args, "degrees")
		if err != nil {
			return nil, err
		}
		degrees = d
	}
	if degrees == 0 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "degrees is 0, so the fingers would go round to where "+
			"they started — that is a two-finger press rather than a rotation")
	}
	if degrees > 360 || degrees < -360 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "degrees is %v, which is more than a full turn — a "+
			"rotation past 360 is indistinguishable from the same angle modulo it, "+
			"and almost certainly a units mistake", degrees)
	}

	radius := rotateRadius
	if r, given := intArg(args, "radius"); given {
		if r <= 0 {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "radius is a distance in device pixels and must be "+
				"above zero, got %d", r)
		}
		radius = r
	}

	var cx, cy int
	target := stringArg(args, "target")
	if target != "" {
		node, _, err := h.resolveNode(ctx, s, target)
		if err != nil {
			return nil, err
		}
		cx, cy = node.Bounds.Center()
	} else {
		tree, err := s.driver.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		cx, cy = tree.Root.Bounds.Center()
	}

	if err := pincher.Rotate(ctx, cx, cy, radius, degrees, zoomDuration); err != nil {
		return nil, err
	}
	delete(h.refs, s.dev.Serial)

	where := "the screen"
	if target != "" {
		where = target
	}
	way := "clockwise"
	if degrees < 0 {
		way = "anticlockwise"
	}
	return Result(fmt.Sprintf("rotated %s %.0f degrees %s about (%d, %d), fingers %dpx "+
		"from the center — the gesture was delivered, which is all this can confirm: "+
		"nothing in either hierarchy reports a rotation, and unlike a zoom there is no "+
		"WebView property to ask either",
		where, math.Abs(degrees), way, cx, cy, radius),
		RotateView{Degrees: degrees, Target: target, X: cx, Y: cy, Radius: radius}), nil
}

// RotateView is the result of app_rotate.
type RotateView struct {
	Degrees float64 `json:"degrees"`
	Target  string  `json:"target,omitempty"`
	X       int     `json:"x"`
	Y       int     `json:"y"`
	Radius  int     `json:"radius"`
}
