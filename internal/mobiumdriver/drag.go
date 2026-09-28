package mobiumdriver

import (
	"context"
	"strings"
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
// WebDriverAgent expects — in one of two ways, because neither reaches both
// kinds of target. Measured on an iPhone 17 Pro simulator against MobiumApp,
// 2026-09-28 (CHALLENGES 149):
//
//   - WebDriverAgent's own doubleTap endpoint reached a WebView page as two
//     clicks, and a React Native Pressable as **one** press — where a
//     person's double tap on the iPhone 15 Plus was two, 200ms apart.
//   - A W3C chain whose gap is a timed move to the same point, not a pause
//     (WDA drops a pause while the pointer is up), reached the Pressable as
//     two presses 167ms apart, three times in three, the second landing with
//     no other finger down; and reached the page as one click, at every gap
//     tried up to 250ms, WebKit reading the move as a second contact.
//
// So a point on a WebView gets the endpoint, and any other point the chain.
// Neither made a WebView fire `dblclick`.
func (w *WDA) DoubleTap(ctx context.Context, x, y int) error {
	if w.onWebView(ctx, x, y) {
		return w.w3c.wdaDoubleTap(ctx, w.toPoints(x), w.toPoints(y))
	}
	return w.w3c.pointerSequence(ctx, iosDoubleTapActions(w.toPoints(x), w.toPoints(y)))
}

// onWebView reports whether a point, in device pixels, falls on a WebView in
// the current hierarchy. A snapshot that fails answers no: the chain is the
// right gesture everywhere but on a page.
func (w *WDA) onWebView(ctx context.Context, x, y int) bool {
	tree, err := w.Snapshot(ctx)
	if err != nil {
		return false
	}
	for _, n := range tree.All() {
		b := n.Bounds
		if strings.HasSuffix(n.ShortClass(), "WebView") && x >= b.X1 && x < b.X2 && y >= b.Y1 && y < b.Y2 {
			return true
		}
	}
	return false
}

// iosDoubleTapActions is doubleTapActions with the gap spent on a move to
// the same point, which WebDriverAgent honors, instead of a pause, which it
// drops while the pointer is up.
func iosDoubleTapActions(x, y int) []map[string]interface{} {
	return []map[string]interface{}{
		{"type": "pointerMove", "duration": 0, "x": x, "y": y},
		{"type": "pointerDown", "button": 0},
		{"type": "pause", "duration": 60},
		{"type": "pointerUp", "button": 0},
		{"type": "pointerMove", "duration": doubleTapGap.Milliseconds(), "x": x, "y": y},
		{"type": "pointerDown", "button": 0},
		{"type": "pause", "duration": 60},
		{"type": "pointerUp", "button": 0},
	}
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
