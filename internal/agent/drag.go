package agent

import (
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
)

const (
	// dragHold is how long the finger rests at each end. Above Android's
	// 500ms default long-press timeout on purpose: a drag-to-reorder list
	// arms on that long press, so a shorter hold picks nothing up and the
	// travel afterwards scrolls the list instead. iOS's own drag sessions
	// begin on a comparable delay.
	dragHold = 700 * time.Millisecond

	// dragMove is how long the travel takes. Slow enough to be a drag rather
	// than a fling, and slow enough that a target highlighting under the
	// finger has time to do it.
	dragMove = 800 * time.Millisecond
)

// drag is app_drag: pick something up, carry it somewhere, put it down.
//
// This is not app_swipe with better names. A swipe is one movement between
// two points; a drag is a press, a *hold* long enough for the thing under the
// finger to be picked up, a slow traversal that anything it passes over can
// see, another hold, and a release. Both ends of an app_swipe are instant,
// deliberately, because that is what makes it a swipe.
//
// What it can confirm is limited in the same way a zoom is, and for a better
// reason: the drop is the app's decision. Mobium can report that the gesture
// was delivered and where it went. Whether the list reordered, whether the
// icon landed in the folder, whether the target rejected the drop — all of
// that is in the app's own state, and the way to check it is to map the
// screen again and look.
func (h *Handlers) drag(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.dragOn(ctx, s, args)
}

// dragOn is app_drag once the device is resolved.
func (h *Handlers) dragOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	dragger, ok := mobiumdriver.AsDragger(s.driver)
	if !ok {
		return nil, mobiumerr.New(mobiumerr.Unsupported, "the %s backend cannot drag: it can swipe, but a swipe "+
			"has no hold at either end and a drag is mostly the holds — switch to "+
			"--backend uiautomator2", s.backend)
	}

	hold := time.Duration(intArgOr(args, "hold_ms", int(dragHold.Milliseconds()))) * time.Millisecond
	move := time.Duration(intArgOr(args, "duration_ms", int(dragMove.Milliseconds()))) * time.Millisecond
	if hold <= 0 || move <= 0 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "hold_ms and duration_ms are times and must be above zero, "+
			"got %d and %d", hold.Milliseconds(), move.Milliseconds())
	}

	from := stringArg(args, "from")
	to := stringArg(args, "to")
	x1, has1 := intArg(args, "x1")
	y1, has2 := intArg(args, "y1")
	x2, has3 := intArg(args, "x2")
	y2, has4 := intArg(args, "y2")
	coords := has1 && has2 && has3 && has4

	switch {
	case from == "" && to == "" && !coords:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "app_drag needs both from and to (refs or locators), "+
			"or all four of x1, y1, x2, y2")
	case (from != "") != (to != ""):
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "from and to must be given together — a drag has two ends")
	case from != "" && coords:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "give either from/to or x1,y1,x2,y2, not both")
	}

	if coords {
		if err := dragger.Drag(ctx, x1, y1, x2, y2, hold, move); err != nil {
			return nil, err
		}
		delete(h.refs, s.dev.Serial)
		return Result(fmt.Sprintf("dragged (%d,%d) to (%d,%d), holding %s at each end — "+
			"the gesture was delivered; whether anything was picked up or accepted is "+
			"the app's own state, so map the screen again to see it",
			x1, y1, x2, y2, hold),
			DragView{X1: x1, Y1: y1, X2: x2, Y2: y2, HoldMS: int(hold.Milliseconds()),
				DurationMS: int(move.Milliseconds())}), nil
	}

	// A ref taken in a WebView resolves through the page and is then dragged
	// natively, exactly as app_tap does it: the driver already knows how to
	// move a finger, so none of CDP's input domain is needed. Without this
	// branch a drag inside a page would resolve its ends against the native
	// hierarchy, where the page's own elements do not exist — and the failure
	// would name the ref rather than the context.
	if s.web != nil {
		// The source must be touchable, as for a tap. The destination is
		// where the finger lets go, and a drop zone often has something over
		// it mid-drag — the dragged item itself — so it keeps its center.
		ax, ay, _, err := h.aimWeb(ctx, s, from)
		if err != nil {
			return nil, err
		}
		b, err := h.resolveWeb(ctx, s, to)
		if err != nil {
			return nil, err
		}
		bx, by := b.Center()
		if ax == bx && ay == by {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "from and to are both at (%d, %d), so nothing "+
				"would move — %q and %q resolved to the same place", ax, ay, from, to)
		}
		if err := dragger.Drag(ctx, ax, ay, bx, by, hold, move); err != nil {
			return nil, err
		}
		delete(h.refs, s.dev.Serial)
		return Result(fmt.Sprintf("dragged %s at (%d,%d) onto %s at (%d,%d) in %s, "+
			"holding %s at each end — the gesture was delivered; whether the drop "+
			"was accepted is the page's own state, so map again to see it",
			from, ax, ay, to, bx, by, s.webCtx, hold),
			DragView{From: from, To: to, X1: ax, Y1: ay, X2: bx, Y2: by,
				HoldMS: int(hold.Milliseconds()), DurationMS: int(move.Milliseconds())}), nil
	}

	// Both ends are resolved from the same snapshot, before either is
	// touched. Resolving the destination after the drag began would read a
	// screen that the drag itself is moving — the list has already scrolled
	// under the finger, and the row that was the target is somewhere else.
	src, _, err := h.resolveNode(ctx, s, from)
	if err != nil {
		return nil, err
	}
	dst, _, err := h.resolveNode(ctx, s, to)
	if err != nil {
		return nil, err
	}
	sx, sy := src.Bounds.Center()
	dx, dy := dst.Bounds.Center()
	if sx == dx && sy == dy {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "from and to are both at (%d, %d), so nothing would move — "+
			"%q and %q resolved to the same place", sx, sy, from, to)
	}

	if err := dragger.Drag(ctx, sx, sy, dx, dy, hold, move); err != nil {
		return nil, err
	}
	// Whatever was dragged is somewhere else now, and so is everything it
	// displaced.
	delete(h.refs, s.dev.Serial)

	return Result(fmt.Sprintf("dragged %s at (%d,%d) onto %s at (%d,%d), holding %s at "+
		"each end — the gesture was delivered; whether the drop was accepted is the "+
		"app's own state, so map the screen again to see it",
		from, sx, sy, to, dx, dy, hold),
		DragView{From: from, To: to, X1: sx, Y1: sy, X2: dx, Y2: dy,
			HoldMS: int(hold.Milliseconds()), DurationMS: int(move.Milliseconds())}), nil
}

// DragView is the result of app_drag.
type DragView struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	X1   int    `json:"x1"`
	Y1   int    `json:"y1"`
	X2   int    `json:"x2"`
	Y2   int    `json:"y2"`
	// HoldMS is reported because it is the argument most likely to be wrong:
	// a drag that picks nothing up is usually one that did not hold long
	// enough, and a number in the answer is what makes that adjustable.
	HoldMS     int `json:"hold_ms"`
	DurationMS int `json:"duration_ms"`
}
