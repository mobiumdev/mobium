package mobiumdriver

import (
	"context"
	"time"
)

// A double tap and a drag are both single-finger gestures that the backends
// already able to swipe cannot necessarily do, because both are defined by
// their timing rather than by where the finger goes. They live together here
// for that reason: the same argument, made twice.
//
// Both are implemented on the two W3C backends and on neither of the others.
// The W3C /actions endpoint takes the whole chain in one request and the
// device-side server plays it back with the pauses intact, which is the only
// way to be sure of an interval this side of the wire.

// DoubleTap taps twice at a point. Coordinates are device pixels.
func (u *UIA2) DoubleTap(ctx context.Context, x, y int) error {
	return u.w3c.pointerSequence(ctx, doubleTapActions(x, y))
}

// DoubleTap taps twice at a point, in device pixels, converted to the points
// WebDriverAgent expects.
//
// Through WebDriverAgent's own endpoint rather than the W3C chain the Android
// backend uses, because the chain cannot express this gesture here: WDA drops
// a pause that occurs while the pointer is up, so the two taps arrive with
// nothing between them and WebKit discards the second as a bounce (measured —
// see doubleTapActions). Spending the interval on a timed move instead made
// it worse, arriving as two overlapping contacts.
//
// This is the driver layer doing its job rather than a parallel
// implementation: a backend exists to say how a gesture is expressed on its
// platform, and iOS ships a primitive for this one. Asking for it is
// categorically better than assembling something that imitates it, and the
// timing is then Apple's rather than a constant chosen here.
func (w *WDA) DoubleTap(ctx context.Context, x, y int) error {
	return w.w3c.wdaDoubleTap(ctx, w.toPoints(x), w.toPoints(y))
}

// Drag presses, holds, travels and releases. Coordinates are device pixels.
func (u *UIA2) Drag(ctx context.Context, x1, y1, x2, y2 int, hold, move time.Duration) error {
	return u.w3c.pointerSequence(ctx, dragHoldActions(x1, y1, x2, y2, hold, move))
}

// Drag presses, holds, travels and releases, in device pixels converted to
// points. The durations are not converted, which is the point of saying so:
// only the geometry scales.
func (w *WDA) Drag(ctx context.Context, x1, y1, x2, y2 int, hold, move time.Duration) error {
	return w.w3c.pointerSequence(ctx,
		dragHoldChain(w.toPoints(x1), w.toPoints(y1), w.toPoints(x2), w.toPoints(y2), hold, move, true))
}

// The dump backend implements neither, and the reasons are different.
//
// **A double tap** is two `adb shell input tap` calls, and each one is a
// separate round trip that forks a process on the device. Nothing here
// controls the interval between them, and the platform's window is 40-300ms
// (DoubleTapper says where those numbers come from). The gesture would
// sometimes arrive as one double tap and sometimes as two taps, with no way
// to tell which from the result — a coin flip reported as a success, which is
// the failure this project refuses most consistently.
//
// **A drag** is `adb shell input swipe`, which has no hold at either end and
// no argument that could add one, so what it delivers is a fling. `input
// draganddrop` exists on recent Android and looks like the right primitive,
// but nothing here has measured which API levels carry it, how long it holds,
// or whether it holds at both ends — and "looks like the right primitive" is
// not a thing to ship. Measuring it is the way to close this, not assuming it.
//
// Both refusals are made by the tool layer through AsDoubleTapper and
// AsDragger rather than here, since a missing method is what a refusal *is*.
