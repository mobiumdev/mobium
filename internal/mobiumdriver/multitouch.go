package mobiumdriver

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Fingers that do different things at once, built as W3C pointer chains —
// one per finger — the way pinch and rotate already are. Two rules, each
// learned by a gesture arriving as something else:
//
// **Every chain has the same number of steps, and step i lasts as long in
// every chain.** The spec runs actions tick by tick, each tick as long as its
// longest action, and UiAutomator2 does exactly that. WebDriverAgent instead
// turns each chain into its own event path and places each action at the sum
// of the durations before it. The two only agree when the chains are aligned
// step for step.
//
// **A finger that is down never pauses: it holds still by moving to where it
// already is**, for as long as the pause would have lasted. UiAutomator2
// builds each step's MotionEvent from the fingers that have an event in that
// step, and counts the fingers down from the same events. A held finger that
// pauses is therefore missing: while another lands it was not counted, so the
// landing went out as a fresh one-finger ACTION_DOWN; while another dwelt it
// was dropped from the MOVE, and Android took it as gone, so that finger's
// later lift named a pointer nobody was tracking and never arrived. Measured
// on a Pixel 7 AVD against MobiumApp's Multi-Touch pad and Android's
// pointer-location overlay, which read "P: 0 / 1" for a press-tap the tool
// had reported as sent (appium-uiautomator2-server 10.6.6,
// ActionsExecutor.executeMotionEvents; CHALLENGES 84). Pinch never met either, because both
// its fingers move in every step. On WebDriverAgent a move to the current
// point is simply a finger that did not move.
//
// And a finger not yet down does nothing but pause until its moment:
// WebDriverAgent reads a chain that *starts* with a move as a finger already
// on the glass, which made press-tap arrive as three fingers (CHALLENGES 85).

const (
	// fingerDwell is how long a tapping finger stays down: long enough to be
	// a touch on both platforms, far short of any long-press timeout.
	fingerDwell = 80 * time.Millisecond

	// settleBeforeMove is the rest between a finger landing and moving,
	// for the reason pinchActions gives: movement in the frame of the press
	// reads as a fling.
	settleBeforeMove = 80 * time.Millisecond

	// trailAfter is how long the holding finger stays down once the other has
	// lifted, so the gesture reads as "held while the other acted" rather than
	// two fingers leaving together.
	trailAfter = 120 * time.Millisecond
)

type action = map[string]interface{}

func moveTo(p Point, d time.Duration) action {
	return action{"type": "pointerMove", "duration": d.Milliseconds(), "x": p.X, "y": p.Y}
}

// stay is a down finger holding still at p for d: a move to where it is.
func stay(p Point, d time.Duration) action { return moveTo(p, d) }

func down() action                 { return action{"type": "pointerDown", "button": 0} }
func up() action                   { return action{"type": "pointerUp", "button": 0} }
func pause(d time.Duration) action { return action{"type": "pause", "duration": d.Milliseconds()} }
func idle() action                 { return pause(0) }

// multiTapActions lands every finger at once and lifts them together.
func multiTapActions(pts []Point) [][]action {
	chains := make([][]action, len(pts))
	for i, p := range pts {
		chains[i] = []action{moveTo(p, 0), down(), stay(p, fingerDwell), up()}
	}
	return chains
}

// pressTapActions holds one finger at hold and taps a second at tap.
func pressTapActions(hold, tap Point, lead time.Duration) [][]action {
	first := []action{
		moveTo(hold, 0), down(), stay(hold, lead),
		stay(hold, 0), stay(hold, 0), stay(hold, fingerDwell), stay(hold, 0),
		stay(hold, trailAfter), up(),
	}
	second := []action{
		idle(), idle(), pause(lead),
		moveTo(tap, 0), down(), stay(tap, fingerDwell), up(),
		pause(trailAfter), idle(),
	}
	return [][]action{first, second}
}

// pressDragActions holds one finger at hold and drags a second from one
// point to another.
func pressDragActions(hold, from, to Point, lead, move time.Duration) [][]action {
	first := []action{
		moveTo(hold, 0), down(), stay(hold, lead),
		stay(hold, 0), stay(hold, 0), stay(hold, settleBeforeMove), stay(hold, move),
		stay(hold, settleBeforeMove), stay(hold, 0),
		stay(hold, trailAfter), up(),
	}
	second := []action{
		idle(), idle(), pause(lead),
		moveTo(from, 0), down(), stay(from, settleBeforeMove), moveTo(to, move), stay(to, settleBeforeMove), up(),
		pause(trailAfter), idle(),
	}
	return [][]action{first, second}
}

func (u *UIA2) MultiTap(ctx context.Context, fingers []Point) error {
	return u.w3c.pointerSequences(ctx, multiTapActions(fingers)...)
}

func (u *UIA2) PressTap(ctx context.Context, hold, tap Point, lead time.Duration) error {
	if err := u.lateFingerAllowed(ctx); err != nil {
		return err
	}
	return u.w3c.pointerSequences(ctx, pressTapActions(hold, tap, lead)...)
}

func (u *UIA2) PressDrag(ctx context.Context, hold, from, to Point, lead, move time.Duration) error {
	if err := u.lateFingerAllowed(ctx); err != nil {
		return err
	}
	return u.w3c.pointerSequences(ctx, pressDragActions(hold, from, to, lead, move)...)
}

// lastLenientAPI is the newest Android that accepted a finger landing after
// the first through UiAutomator2: API 35, Android 15, measured on a Pixel 7
// AVD. Android 17 (API 37) rejected it on a Pixel 8 Pro and on an AVD alike.
// Android 16 was not measured and is refused with the rest — see
// lateFingerAllowed for why guessing wrong is not cheap.
const lastLenientAPI = 35

// lateFingerAllowed refuses press-tap and press-drag where Android verifies
// injected input. UiAutomator2 (10.6.6, the newest release) stamps each event
// with the down time of the finger that produced it, where Android's contract
// is the first finger's down time for the whole gesture. Android 17 accepts
// the second finger's POINTER_DOWN and then rejects every later event as
// "Down time went backwards" — including the lifts, so both fingers stay down
// in the injector's state and **every gesture after it fails** until the
// UiAutomator2 server restarts. A refusal costs one gesture; a wrong guess
// costs the session. CHALLENGES 86.
func (u *UIA2) lateFingerAllowed(ctx context.Context) error {
	out, err := u.adb.Shell(ctx, "getprop", "ro.build.version.sdk")
	if err != nil {
		return err
	}
	api, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return mobiumerr.New(mobiumerr.DeviceServer, "could not read the device's API level: got %q", out)
	}
	if api > lastLenientAPI {
		return mobiumerr.New(mobiumerr.Unsupported, "UiAutomator2 cannot land a second finger after the first "+
			"on Android API %d: it gives the second finger its own down time, Android 16 and later reject the "+
			"rest of the gesture as \"down time went backwards\", and the rejected lifts leave both fingers "+
			"down, so every later gesture would fail until the session restarts. Measured working on Android "+
			"15 and failing on 17; 16 is refused unmeasured. Multi-finger taps, where the fingers land "+
			"together, work (app_tap with fingers)", api).
			WithRemedy("run press and tap or press and drag on Android 15 or earlier; use app_tap with fingers for taps of several fingers").
			WithDetail("api_level", api)
	}
	return nil
}

// WebDriverAgent takes points, and every coordinate here is device pixels.
func (w *WDA) pt(p Point) Point { return Point{X: w.toPoints(p.X), Y: w.toPoints(p.Y)} }

func (w *WDA) MultiTap(ctx context.Context, fingers []Point) error {
	pts := make([]Point, len(fingers))
	for i, p := range fingers {
		pts[i] = w.pt(p)
	}
	return w.w3c.pointerSequences(ctx, multiTapActions(pts)...)
}

// errLateFinger is why WebDriverAgent refuses press-tap and press-drag.
//
// XCTest gives any finger that lands after the gesture has begun an extra,
// zero-length touch at its target at the very start: a press-tap arrived as
// "second finger down and up at 0ms, first finger down at 0ms, second finger
// down at 300ms" — the same target touched twice, the first time before the
// gesture it belongs to. Measured on an iPhone 17 Pro simulator through
// WebDriverAgent 16.12.8 with MobiumApp's Multi-Touch pad, for every chain
// shape tried: aligned, unaligned, a leading pause, a leading slow move. An
// app can read that instant touch as a tap, so sending the gesture would do
// something other than what was asked; the rule is to refuse instead.
// CHALLENGES 85.
// Multi-finger taps, where every finger lands together, are unaffected.
var errLateFinger = mobiumerr.New(mobiumerr.Unsupported, "WebDriverAgent cannot land a second finger after "+
	"the first: XCTest adds a zero-length touch at the second finger's target at the start of the gesture, "+
	"so the target would be touched twice — once before the press it belongs to. Multi-finger taps work "+
	"(app_tap with fingers); press and tap and press and drag work on Android").
	WithRemedy("run press and tap or press and drag on an Android device; on iOS use app_tap with fingers for taps of several fingers")

func (w *WDA) PressTap(ctx context.Context, hold, tap Point, lead time.Duration) error {
	return errLateFinger
}

func (w *WDA) PressDrag(ctx context.Context, hold, from, to Point, lead, move time.Duration) error {
	return errLateFinger
}
