# Tutorial: a flaky test made steady with the gray box

A test that taps right after the app starts work it does not show fails
whenever the work is slower than the tap. This tutorial writes one such test
against [MobiumApp](https://github.com/mobiumdev/mobium-app)'s Busy Demo,
watches it fail, has the app say when it is busy, and turns the gray box on.
Every command and line of output is what a Pixel 7 emulator on Android 15
printed on 2026-10-05; an iOS simulator, an iPhone and an Android phone pass
the same way.

What the gray box is and does is in [the gray box guide](graybox.md). Before
this tutorial: [the test runner guide](test-runner.md)'s first section, and
MobiumApp installed.

## Contents

- [1. The test](#1-the-test)
- [2. It fails](#2-it-fails)
- [3. The app says when it is busy](#3-the-app-says-when-it-is-busy)
- [4. Turning the gray box on](#4-turning-the-gray-box-on)
- [5. Your own app](#5-your-own-app)

## 1. The test

The Busy Demo's **Refresh quietly** starts 0.4 to 1.6 seconds of work and
leaves the old rows up while it runs; then a new generation of rows replaces
them. Tapping a row says whether it was `current` or `stale`. The test taps
the refresh, then Row B, and expects the row to be current — five times,
each from a fresh app:

```
busy-tests/
  mobium.config.json
  tests/
    busy.test.json
```

```json
{
  "testDir": "tests",
  "projects": [
    {"name": "android", "device": "emulator-5554"}
  ]
}
```

```json
{
  "app": "dev.mobium.mobiumapp",
  "beforeEach": [
    {"scroll_to": {"target": "label=Busy Demo", "direction": "down"}},
    {"tap": "label=Busy Demo"}
  ],
  "tests": [
    {
      "name": "a row tapped after a quiet refresh is current (${run})",
      "each": [{"run": 1}, {"run": 2}, {"run": 3}, {"run": 4}, {"run": 5}],
      "steps": [
        {"tap": "testid=busyQuiet"},
        {"tap": "testid=busyRowB"},
        {"wait_for": {"target": "testid=busyOutcome", "condition": "text", "text": ": current", "timeout_ms": 3000}}
      ]
    }
  ]
}
```

Nothing in it is wrong. Every tap waits, as every Mobium action does, for its
target to be on screen, still, enabled and uncovered — and Row B is all of
those, the whole time the work runs.

## 2. It fails

```
$ mobium test
  FAIL  [android · emulator-5554] busy.test.json › a row tapped after a quiet refresh is current (1) (6.9s)
        step 3 (app_wait_for): [timeout] step 3 of 3 (app_wait_for) failed: timed out after 3.003s waiting for testid=busyOutcome to contain ": current" — its text is "row B, generation 1: stale"; steps 1-2 ran before it, and nothing after
  FAIL  [android · emulator-5554] busy.test.json › a row tapped after a quiet refresh is current (2) (6.9s)
        step 3 (app_wait_for): [timeout] step 3 of 3 (app_wait_for) failed: timed out after 3.027s waiting for testid=busyOutcome to contain ": current" — its text is "row B, generation 1: stale"; steps 1-2 ran before it, and nothing after
  ...
0 passed, 5 failed (34.9s)
error: 5 of 5 tests failed
```

Five of five: the row was tapped while generation 1 was still up. Within a
test, steps follow each other in milliseconds, so the race is lost every
time; driven by hand, one command at a time, it is lost six to ten times in
ten. A sleep would make it pass on a slow day and fail on a slower one, and
a `wait_for` has nothing on screen to wait for — the work shows nothing.

## 3. The app says when it is busy

Only the app knows when its work starts and when it is done. MobiumApp tells
Mobium's gray-box library — a local Expo module, `modules/graybox` — at both
ends:

```ts
import GrayBox from './modules/graybox/src/GrayBoxModule';

const refresh = (visible: boolean) => {
  const tag = visible ? 'refresh' : 'quiet';
  pending.current += 1;
  GrayBox.busy(tag);
  // ... start the work; when it is done, set the new rows ...
};

// Idle once the new rows are rendered, not when the work returns:
// "done and on screen".
useEffect(() => {
  while (finished.current.length) {
    pending.current -= 1;
    GrayBox.idle(finished.current.shift()!);
  }
}, [gen]);
```

`busy` and `idle` do nothing unless the app was launched with the gray box
on, so this is safe to ship in every build of the app under test.

## 4. Turning the gray box on

One key in the test file, beside `app`:

```json
{
  "app": "dev.mobium.mobiumapp",
  "grayBox": true,
  "beforeEach": [
```

Each test's launch now turns the library on, and every step waits for the app
to say it is idle before finding its target:

```
$ mobium test
  ok    [android · emulator-5554] busy.test.json › a row tapped after a quiet refresh is current (1) (7s)
  ok    [android · emulator-5554] busy.test.json › a row tapped after a quiet refresh is current (2) (6.8s)
  ok    [android · emulator-5554] busy.test.json › a row tapped after a quiet refresh is current (3) (6.6s)
  ok    [android · emulator-5554] busy.test.json › a row tapped after a quiet refresh is current (4) (5.6s)
  ok    [android · emulator-5554] busy.test.json › a row tapped after a quiet refresh is current (5) (6.1s)
5 passed (32.1s)
```

Nothing else changed: the steps are the same, and no sleep was added. The
tap on Row B waited as long as the work took — a second or so — and no
longer. Outside a test it is `mobium launch --gray-box`, and every result
says what was waited for:

```
$ mobium tap testid=busyRowB
tapped testid=busyRowB at (540, 1095)
gray box: waited 1184 ms for the app to go idle (busy: quiet)
```

## 5. Your own app

The library is small, and what it writes is the whole contract. In a React
Native app, copy MobiumApp's `modules/graybox` (Swift and Kotlin, about 160
lines between them) and call `busy` and `idle` around the work a test should
wait for. In a native app, write the same lines yourself, only when launched
with the gray box on — on iOS the argument `-MobiumGrayBox YES`, on Android
the intent extra `MobiumGrayBox=true`:

```swift
// iOS: os_log, subsystem dev.mobium.graybox, notice level, values public
let log = Logger(subsystem: "dev.mobium.graybox", category: "busy")
if UserDefaults.standard.bool(forKey: "MobiumGrayBox") {
  log.notice("MOBIUM-GRAYBOX busy=\(count, privacy: .public) tag=\(tag, privacy: .public)")
}
```

```kotlin
// Android: logcat, tag MobiumGrayBox, info level
if (activity.intent.getBooleanExtra("MobiumGrayBox", false)) {
  Log.i("MobiumGrayBox", "MOBIUM-GRAYBOX busy=$count tag=$tag")
}
```

Write `MOBIUM-GRAYBOX on` once at start, `busy=<n> tag=<what>` at every
change of the count of work in flight, and `lift` whenever a finger comes up
— Mobium counts its grace from the lift, so work a tap starts is never
missed. [The gray box guide](graybox.md#what-the-app-writes) has the lines in
full, and MobiumApp's module is a complete example of all four.
