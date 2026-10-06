# Gray box: waiting for the app to say it is idle

Auto-wait judges a screen by what it shows: a target is there, has stopped
moving, is enabled and is not covered. That cannot see work the screen does
not show — a request in flight behind rows that look finished, which a tap
then lands on just before they are replaced.

Gray box asks the app instead. An app built with Mobium's gray-box library,
and launched with `--gray-box`, says in the device log when it starts work and
when that work is finished and on screen. Every action then waits for the app
to be idle before it finds its target. The app says when; Mobium does not
guess.

iOS and Android, on simulators, emulators and phones. Everything here was
measured on an iPhone 15 Plus on iOS 26.6.2, an iPhone 17 Pro simulator on
iOS 26.5, a Pixel 8 Pro on Android 17 and a Pixel 7 emulator on Android 15,
on [MobiumApp](https://github.com/mobiumdev/mobium-app)'s Busy Demo, which
links the library.

## Contents

- [Turning it on](#turning-it-on)
- [What an action does](#what-an-action-does)
- [What the app writes](#what-the-app-writes)
- [The Busy Demo, measured](#the-busy-demo-measured)
- [What it does not do](#what-it-does-not-do)

## Turning it on

```
$ mobium launch --gray-box dev.mobium.mobiumapp
launched dev.mobium.mobiumapp, with the gray box: every action waits for the app to say it is idle
```

On iOS, `--gray-box` launches the app with the argument
`-MobiumGrayBox YES`, which iOS keeps for that launch only. On Android it
starts the app afresh — stopped first, so its activity reads what it was
started with — with the intent extra `MobiumGrayBox=true`, which a launch
from the home screen never carries. Either way the next ordinary launch is
ordinary, a person running the same build never turns it on, and the library
writes nothing without it.

An app that does not link the library launches normally, and says so (an
emulator; a simulator says the same of `com.apple.Preferences`):

```
$ mobium launch --gray-box com.android.settings
launched com.android.settings, but the app has not answered the gray box in 5s, so actions are not waited for: it needs Mobium's gray-box library, in a build that reads the MobiumGrayBox launch argument or intent extra
```

In a test file it is `"grayBox": true` beside `"app"`, and each test's launch
turns it on — [the tutorial](graybox-tutorial.md) takes a test from five
failures in five to five passes that way. From MCP and the clients it is
`app_launch` with `gray_box: true` —
`launch(app, gray_box=True)` in Python, `launch(app, { grayBox: true })` in
JavaScript, `LaunchWithGrayBox` in Go and .NET, `launchWithGrayBox` in Java.

## What an action does

Before an action finds its target, it waits until the app has nothing in
flight. The result says what it waited for (the iPhone):

```
$ mobium tap testid=busyQuiet
tapped testid=busyQuiet at (645, 1011)
gray box: waited 35 ms for the app to go idle

$ mobium tap testid=busyRowB
tapped testid=busyRowB at (645, 1218)
gray box: waited 1018 ms for the app to go idle (busy: quiet)

$ mobium text testid=busyOutcome
row B, generation 2: current
```

The wait is the check `idle`, after the five auto-wait makes. Work a tap
starts is announced as the tap is handled, so the wait first lets the last
call's touch arrive, then 150 ms after the finger lifted — measured: the
app's busy line came 0 to 15 ms after the lift, in 200 trials — and only
then trusts a count of zero. On an idle app it costs about 35 ms, or nothing
when the last call ended long enough ago — `gray box: the app was idle`, as
the emulator printed for the tap before this one:

```
$ mobium tap testid=busyQuiet
tapped testid=busyQuiet at (540, 901)
gray box: the app was idle

$ mobium tap testid=busyRowB
tapped testid=busyRowB at (540, 1095)
gray box: waited 1184 ms for the app to go idle (busy: quiet)
```

With `--json` the waits are in `app_idle` (this one on the simulator):

```
$ mobium tap testid=busyRowB --json
{
  "action": "tap",
  "app_idle": {
    "waited_ms": 388,
    "waits": [
      {
        "busy": [
          "quiet"
        ],
        "waited_ms": 388
      }
    ]
  },
  "target": "testid=busyRowB",
  "x": 603,
  "y": 1275
}
```

Coordinate taps, long presses and swipes wait the same way as a tap on an
element, and so does a tap in a WebView. A tap on a button in an alert lifts
in the alert's own window, which the library does not watch; with no lift
heard during an action, the grace counts from the action's end instead.

An app that is never idle — one polling in the background — is not waited for
forever. After 10 seconds the action is refused as a timeout that failed check
`idle`, naming what the app said kept it busy, with the remedy that works:
launch the app again without `--gray-box` to act on the screen as it is.

```
$ mobium tap testid=busyRowA
error: testid=busyRowA failed check idle: the app says it is still busy after 10s, with poll — something in the app never finishes; to act on the screen as it is, launch the app again without gray_box
```

### When the app is not waited for

Three things end a wait early, and the result says which, so a check that
did not run never reads as one that passed:

- **The app went to the background.** The library says `away` and `back`;
  while the app is away, an action elsewhere is not held up by it.
- **The app stopped saying it is busy.** Busy is a lease: while work is in
  flight the library restates the count every half second, from a native
  timer that a busy JavaScript thread does not stop. A count nothing has
  restated for 1.5 seconds belongs to an app that crashed holding it, or
  was suspended, and is not waited on.
- **Mobium is not hearing the app.** The log stream stopped. On Android it
  starts again by itself within a second, from the device time of the last
  line it read, so nothing written in the gap is lost; on a simulator the
  next `launch --gray-box` starts it again; on an iPhone the session's log
  capture reconnects at the next action.

The Busy Demo's crash button dies holding work; the next tap, on the
emulator's home screen, was not held up by it:

```
$ mobium tap 540 1200
tapped (540, 1200)
gray box: not waited — the app stopped saying it is busy 2.1s ago (it was busy with doomed)
```

On Android the app often reports itself in the background on its way down,
and then that is the reason given — the same answer, reached sooner.

## What the app writes

The library writes one line to the device log for each change — on iOS under
the `os_log` subsystem `dev.mobium.graybox`, at the default (notice) level,
with its values public; on Android to logcat under the tag `MobiumGrayBox`,
at info:

```
MOBIUM-GRAYBOX on                 the library is listening
MOBIUM-GRAYBOX busy=1 tag=fetch   work started; 1 thing in flight
MOBIUM-GRAYBOX busy=0 tag=fetch   that work finished, and is on screen
MOBIUM-GRAYBOX lift               a finger came up
MOBIUM-GRAYBOX still busy=1       every half second while work is in flight
MOBIUM-GRAYBOX away / back        the app left the foreground / returned
```

`busy=` is the count of work in flight after the change; `tag=` names it, for
a refusal to name. Anything after the fields — MobiumApp's library adds the
phone's clock as `t=` — is ignored. On an iPhone the lines arrive through
the log the session already captures, 1 to 4 ms after the app writes them;
on a simulator through a log stream narrowed to that subsystem; on Android
through logcat narrowed to that tag, from the device's own clock at the
moment of the launch, so a line from an earlier launch is never read.

The app decides what counts as work, as it decides when it is finished. In
MobiumApp the Busy Demo calls the library's `busy("quiet")` when it starts
and `idle("quiet")` in an effect after the new rows are rendered, so idle
means "done and on screen", not "the response arrived". The library is a
local Expo module, `modules/graybox` in MobiumApp, about ninety lines of
Swift and seventy of Kotlin: copying it into another React Native app, or
writing the same lines from a native one, is all it takes.

## The Busy Demo, measured

The Busy Demo has two buttons that start the same 0.4 to 1.6 seconds of
work, then bring a new generation of rows. **Refresh** replaces the rows with
a spinner meanwhile; **Refresh quietly** leaves the old rows up. A row says
whether the one tapped was current. A trial is: tap the refresh, tap Row B,
read what the row said. [`docs/checks/graybox-edges.sh`](../checks/graybox-edges.sh)
holds the edges above to the same demo, with buttons that start the
refresh from an alert, run two at once, keep polling, and crash the app
mid-refresh. [`docs/checks/graybox.sh`](../checks/graybox.sh)
runs ten after a quiet refresh launched normally, three after a refresh with
a spinner, and ten after a quiet refresh launched with `--gray-box`:

| Device | Launched normally: stale | With `--gray-box`: stale |
| --- | --- | --- |
| iPhone 15 Plus, iOS 26.6.2 | 6 of 10, and 7 of 10 | 0 of 10, twice |
| iPhone 17 Pro simulator, iOS 26.5 | 9 of 10 | 0 of 10 |
| Pixel 8 Pro, Android 17 | 9 of 10 | 0 of 10 |
| Pixel 7 emulator, Android 15 | 10 of 10 | 0 of 10 |

After a refresh with a spinner, a normal launch tapped a current row every
time, on every device: that is the control — what the screen shows, auto-wait
already waits for. The quiet refresh is what only the app can say. Over fifty
more trials on the iPhone, launched normally, 35 of 50 rows tapped after a
quiet refresh were stale, and none of 50 after a refresh with a spinner.

The check fails if the normal launch never tapped a stale row: a run in which
gray box had nothing to prevent shows nothing.

## What it does not do

- **Detect work.** The app says when it is busy. Work it does not declare is
  not waited for, and an app that says it is idle too early is believed.
- **An app without the library** — anything from an app store — which stays
  driven the ordinary way.
- **`--hit-test` together with it**, for now: launch with one of them.
- **Switching apps.** The gray box belongs to the app last launched with it.
  An ordinary launch of any app ends it.
