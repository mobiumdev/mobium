package mobiumdriver

import (
	"context"
	"time"
)

// Two fingers, one code path. Both device-side servers speak the same W3C
// actions endpoint, and multi-touch there is simply a second pointer source
// rather than a different API — so this is the same shape as every other
// gesture, with the iOS scale conversion applied where it always is.

// Pinch moves two fingers about a point. Coordinates are device pixels.
func (u *UIA2) Pinch(ctx context.Context, cx, cy, from, to int, d time.Duration) error {
	left, right := pinchActions(cx, cy, from, to, d)
	return u.w3c.pointerSequences(ctx, left, right)
}

// Pinch moves two fingers about a point. Coordinates are device pixels, and
// are converted to the points WebDriverAgent expects — including the gaps,
// which are distances and scale the same way a position does.
func (w *WDA) Pinch(ctx context.Context, cx, cy, from, to int, d time.Duration) error {
	left, right := pinchActions(w.toPoints(cx), w.toPoints(cy), w.toPoints(from), w.toPoints(to), d)
	return w.w3c.pointerSequences(ctx, left, right)
}

// Rotate turns two fingers about a point. Coordinates are device pixels.
func (u *UIA2) Rotate(ctx context.Context, cx, cy, radius int, degrees float64, d time.Duration) error {
	left, right := rotateActions(cx, cy, radius, degrees, d)
	return u.w3c.pointerSequences(ctx, left, right)
}

// Rotate turns two fingers about a point, in device pixels — converted to the
// points WebDriverAgent expects, radius included, since it is a distance.
func (w *WDA) Rotate(ctx context.Context, cx, cy, radius int, degrees float64, d time.Duration) error {
	left, right := rotateActions(w.toPoints(cx), w.toPoints(cy), w.toPoints(radius), degrees, d)
	return w.w3c.pointerSequences(ctx, left, right)
}
