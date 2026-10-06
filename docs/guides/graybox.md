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

iOS, on a simulator and a real iPhone. Everything here was measured on an
iPhone 15 Plus on iOS 26.6.2 and an iPhone 17 Pro simulator on iOS 26.5, on
[MobiumApp](https://github.com/mobiumdev/mobium-app)'s Busy Demo, which
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

`--gray-box` launches the app with the argument `-MobiumGrayBox YES`. iOS
keeps a launch argument for that launch only, so the next ordinary launch is
ordinary again, and a person running the same build never turns it on. The
library writes nothing without it.

An app that does not link the library launches normally, and says so:

```
$ mobium launch --gray-box com.apple.Preferences
launched com.apple.Preferences, but the app has not answered the gray box in 5s, so actions are not waited for: it needs Mobium's gray-box library, in a build that reads the -MobiumGrayBox launch argument
```

From MCP and the clients it is `app_launch` with `gray_box: true` —
`launch(app, gray_box=True)` in Python, `launch(app, { grayBox: true })` in
JavaScript, `LaunchWithGrayBox` in Go and .NET, `launchWithGrayBox` in Java.

## What an action does

Before an action finds its target, it waits until the app has nothing in
flight. The result says what it waited for:

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
then trusts a count of zero. On an idle app it costs about 35 ms.

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

An app that is never idle — one polling in the background — is not waited for
forever. After 10 seconds the action is refused as a timeout that failed check
`idle`, naming what the app said kept it busy, with the remedy that works:
launch the app again without `--gray-box` to act on the screen as it is.

## What the app writes

The library writes one line to the device log for each change, under the
`os_log` subsystem `dev.mobium.graybox`, at the default (notice) level, with
its values public:

```
MOBIUM-GRAYBOX on                 the library is listening
MOBIUM-GRAYBOX busy=1 tag=fetch   work started; 1 thing in flight
MOBIUM-GRAYBOX busy=0 tag=fetch   that work finished, and is on screen
MOBIUM-GRAYBOX lift               a finger came up
```

`busy=` is the count of work in flight after the change; `tag=` names it, for
a refusal to name. Anything after the fields — MobiumApp's library adds the
phone's clock as `t=` — is ignored. On a phone the lines arrive through the
log the session already captures, 1 to 4 ms after the app writes them; on a
simulator through a log stream narrowed to that subsystem.

The app decides what counts as work, as it decides when it is finished. In
MobiumApp the Busy Demo calls the library's `busy("quiet")` when it starts
and `idle("quiet")` in an effect after the new rows are rendered, so idle
means "done and on screen", not "the response arrived". The library is a
local Expo module, `modules/graybox` in MobiumApp, about ninety lines of
Swift: copying it into another React Native app, or writing the same lines
from a native one, is all it takes.

## The Busy Demo, measured

The Busy Demo has two buttons that start the same 0.4 to 1.6 seconds of
work, then bring a new generation of rows. **Refresh** replaces the rows with
a spinner meanwhile; **Refresh quietly** leaves the old rows up. A row says
whether the one tapped was current. A trial is: tap the refresh, tap Row B,
read what the row said. Fifty of each, interleaved, on the iPhone:

| | Launched normally | Launched with `--gray-box` |
| --- | --- | --- |
| Refresh quietly | **15 of 50 current** | **50 of 50 current** |
| Refresh, with a spinner | 50 of 50 current | 50 of 50 current |

The spinner is the control: what the screen shows, auto-wait already waits
for. The quiet refresh is what only the app can say. On a simulator, where
calls are faster, a quiet refresh left Row B stale 9 times in 10.

[`docs/checks/graybox.sh`](../checks/graybox.sh) runs it on any iOS device,
and fails if the ordinary launch never tapped a stale row — a run in which
gray box had nothing to prevent shows nothing.

## What it does not do

- **Detect work.** The app says when it is busy. Work it does not declare is
  not waited for, and an app that says it is idle too early is believed.
- **Android, yet.** The same lines would come through logcat; not built.
- **An app without the library** — anything from the App Store — which stays
  driven the ordinary way.
- **`--hit-test` together with it**, for now: launch with one of them.
- **Switching apps.** The gray box belongs to the app last launched with it.
  An ordinary launch of any app ends it.
