# Gestures

Every touch gesture Mobium performs, the tool that performs it, the MobiumApp
screen that witnesses it, and what was measured on each platform. Organized by
the gesture charts designers use rather than by Mobium's own tool list, so a
gap shows up as an empty row instead of as an absence nobody notices.

**Measured 2026-09-23** on a Pixel 7 AVD on Android 15 and on 17, a Pixel 8
Pro on Android 17 (UiAutomator2 10.6.6), an iPhone 17 Pro simulator (iOS 26.5)
and an iPhone 15 Plus (iOS 26.6.2), both through WebDriverAgent 16.12.8.
`docs/checks/gestures.sh <device>` walks the whole chart in this order and
passes on the Android 15 AVD, the Pixel 8 Pro, the simulator and the iPhone.
The Android 17 AVD is too slow to deliver a drag's closing hold (CHALLENGES
86), which the check rightly fails. On the phone: a 120ms swipe
coasted 1418pt and a 2500ms one 0, a zoom went 1 → 2.79 → 1, a rotation turned
89.1° and back to −0.6°, and press-tap and press-drag were refused as below.

## The chart

Three sources name the gestures, and they agree more than they differ: the
familiar touch-gesture reference chart (tap, double tap, drag, flick, pinch,
spread, press, press and tap, press and drag, rotate), Apple's Human Interface
Guidelines (tap, swipe, drag, touch and hold, double tap, zoom, rotate), and
Android's Compose gesture guide (tap, double tap, long press and press through
`detectTapGestures`; drag in three forms; pan, zoom and rotate through
`detectTransformGestures`). Two remote-desktop cheat sheets add pan and the
multi-finger taps.

| Gesture | Mobium | Witness in MobiumApp (Gestures ▸) | Android | iOS |
| --- | --- | --- | --- | --- |
| **Tap** | `tap` | Tap and Press — the app says which gesture arrived | yes | yes |
| **Press** (touch and hold, long press) | `long-press` | Tap and Press; a 50ms hold must read as a tap | yes | yes |
| **Double tap** | `double-tap` | Double Tap — the page's own `dblclick` | yes, `dblclick` fired | two presses ~200ms apart on a React Native control, as a person's double tap (CHALLENGES 149); a WKWebView gets WebDriverAgent's own double tap, two clicks and no `dblclick` for injected touches, though WebKit's double-tap zoom does fire |
| **Drag** (tap and drag, touch and drag) | `drag` | Drag — a native drop zone that records the holds and the travel | yes | yes |
| **Flick** | `swipe`, fast | Flick and Pan — how far the list coasts after the lift | yes, 120ms coasts ~1560dp | yes, 120ms coasts ~1370pt |
| **Pan** | `swipe`, slow | Flick and Pan | yes, 2500ms coasts ~5dp | yes, 2500ms coasts 0 |
| **Pinch / spread** (zoom) | `zoom out` / `zoom in` | Pinch and Spread — the page's own `visualViewport.scale` | yes | yes |
| **Rotate** | `rotate` | Rotate — the page computes the angle from raw touches | yes | yes |
| **Two-, three-finger tap** | `tap --fingers N` | Multi-Touch — each finger's landing, lifting and travel | yes | yes |
| **Press and tap** | `press-tap` | Multi-Touch | Android 15: yes. 16 and later: **refused** | **refused** — see below |
| **Press and drag** | `press-drag` | Multi-Touch | Android 15: yes. 16 and later: **refused** | **refused** |

Not here, deliberately: **system gestures.** iOS reserves three-finger swipes
and pinches for undo, redo, copy and paste, four-finger swipes on iPad, and
edge swipes for navigation and Control Center; Android reserves its edges for
back and Home. An app is told not to redefine them, and a tool that sent them
would be driving the system rather than the app. Mobium's `press back` and
`press home` are the buttons, not the swipes. **Shake** is in Apple's list and
is device motion, not touch.

## What each witness measures, and its control

Every assertion in `gestures.sh` has a control that would fail if the tool
were sending something else, because a gesture delivered as its neighbor
still makes the app "do something":

- **Press**: a tap must read as a tap first, and a 50ms hold must read as a
  tap too — otherwise the hold duration is not reaching the gesture.
- **Double tap**: two separate taps must count as two, not as a double.
- **Drag**: a swipe along the same path must be told apart by its holds.
- **Flick and pan**: the same swipe at 120ms and at 2500ms. What separates
  them is **how far the list coasts after the finger lifts**, not whether the
  platform's momentum event fires: Android fires it for any release and
  coasts a continuously smaller distance (1560, 246, 52, 30 and 6dp for 120,
  300, 600, 1000 and 2500ms), while iOS stopped coasting entirely somewhere
  between 300 and 1000ms. `swipe`'s default 300ms is a flick on both.
- **Pinch and spread**: in, then out, back near 1 — a fling would scroll and
  leave the scale where it was.
- **Rotate**: clockwise, then back; a pinch-shaped gesture would not reverse
  cleanly.
- **Press and tap**: two fingers landing *together* must read as a two-finger
  tap, not as press and tap, and `--lead-ms 800` must deliver 800. The second
  finger must lift on its own — it once did not.
- **Multi-finger taps**: a plain tap must count one.

The Multi-Touch pad shows its evidence beside its verdict: each finger's
landing and lifting in ms from the first, how far it moved, and every raw
touch event in order. Every number in this document came off that line.

## Three ways a second finger went wrong

All three were found by a witness, and none was visible to the tool, which
reported every gesture as delivered until the last one, which reported a
failure and left the device unable to accept any touch at all.

**UiAutomator2 forgets a finger that pauses.** It builds each step's Android
`MotionEvent` from the fingers that have an event *in that step*, and counts
the fingers already down from the same events. A holding finger that merely
pauses is therefore missing from the step: while the second finger lands it
is not counted, so the landing goes out as a fresh one-finger `ACTION_DOWN`
and Android starts a new gesture (the pointer-location overlay read `P: 0 / 1`
for a press-tap, `P: 0 / 2` for a two-finger tap); while the second finger
rests it is dropped from the `MOVE`, and its later lift names a pointer Android
no longer tracks and never arrives. Pinch never met it because both its
fingers move in every step. The fix is in Mobium's chains: **a finger that is
down never pauses; it holds still by moving to where it already is.**

**WebDriverAgent touches a late finger's target at the start.** XCTest gives
any finger that lands after the gesture has begun an extra, zero-length touch
at its target at time zero — `down 1 at 0ms, up 1 at 0ms, … down 1 at 300ms` —
for every chain shape tried, including one sent straight to WebDriverAgent
with nothing else in it — on the simulator and on the iPhone 15 Plus alike
(`down 4 at 0ms, up 4 at 0ms, down 4 at 299ms`). An app can read that instant
touch as a tap on the target before the gesture it belongs to, so Mobium
refuses press-tap and press-drag on iOS rather than send a gesture that is
also something else.
Multi-finger taps land every finger together and are unaffected.

**UiAutomator2 gives a late finger its own down time**, where Android's
contract is one per gesture. Android 15 does not check; Android 17 does, and
rejects everything after the second finger lands — the lifts too — so both
fingers stay down for every injected gesture on the device, `adb shell input`
included, until a two-finger tap's closing events clear it (CHALLENGES 86).
No chain avoids it, so press-tap and press-drag refuse on Android 16 and later.

## Adding a gesture

The same order every time, because it is the order the faults above were
found in: a witness screen that reports what arrived with its evidence, then
the tool, then a check with a positive assertion and a control, run on both
platforms. A gesture that only has a tool is a claim.
