# 0008 — A hit test below accessibility: `mobium hit-test`

**2026-09-29. Built and checked the same day.** On an iOS simulator, mobium
can now ask UIKit itself which view a touch would reach, and so see the one
case of [CHALLENGES 115](../CHALLENGES.md) no tree could: a tap under an
overlay hidden from accessibility. It is opt-in, because the only way in
from outside stops the app for about two seconds.

## The problem

MobiumApp's Obstruction Demo puts something over each of seven buttons, every
cover itself pressable, and a line at the top names whatever a tap really
reached. Since CHALLENGES 115, every action refuses a control over all of its
target, aims around one over its center, waits out a toast, and reports a
view over the point that is not a control. That handled every case on
Android and every case but one on iOS: an overlay hidden from accessibility
(`accessibilityElementsHidden`). WebDriverAgent's tree does not contain it,
so the tap was sent and landed on the overlay while every check passed.

XCTest's own `hittable` was measured as a way out and is not one: it is an
accessibility hit test, so it says true under the hidden overlay, where a tap
lands on the overlay, and false over the pass-through view, where a tap
reaches the target. What decides where a touch goes is UIKit's
`hitTest:withEvent:`, which knows nothing of accessibility — and until this,
nothing outside the app could ask it.

## What was decided

**A tool, `app_hit_test` / `mobium hit-test <target>`, not a check inside
every action.** It resolves the target as `tap` does, takes the point `tap`
would touch — the center, or the clear point when a control covers the
center — and asks UIKit, inside the app, which view a touch there goes to.
When that view is not the target or inside it, the tool fails as a refused
tap does, "failed check receivesEvents", naming the view, its class, its
bounds and whether accessibility can see it. A receiver hidden from
accessibility gets no "tap its ref" remedy, since it has none.

**How it gets in: lldb, attached for one call.** mobium compiles a small
probe ([internal/device/hitprobe/probe.m](../../internal/device/hitprobe/probe.m))
against the simulator SDK once per source, caches it, attaches lldb to the
app in front, `dlopen`s the probe, calls one C function that takes numbers
and returns a string — lldb's own expression evaluator could not be trusted
with UIKit's struct types — and detaches. The function's name is derived
from the probe's source, so a library an older mobium loaded into the same
app is never the one called. It finds the target's view by accessibility
identifier, falling back to its accessibility frame, since a covered
element's frame is not always the one accessibility reports.

**Why opt-in.** The attach stops the app for about two seconds (2.7 to 3
measured per call), which is too slow and too intrusive to put in front of
every tap. The other way in — loading the probe at launch through
`DYLD_INSERT_LIBRARIES` so it answers in milliseconds — was considered and
not chosen: it puts a listener inside the app under test, and covers only
apps mobium launched. So the check is there to ask where it matters: once
per screen in a test, or when a tap reported success and the app disagrees.

**Where it runs.** An iOS simulator. Android refuses, saying why it needs
none: its hierarchy lists a view accessibility hides — the hidden overlay
is a clickable, unnamed view there — and every action already refuses a
control over its target. A real iPhone refuses too: attaching to an app
there needs it signed for debugging and a debug server on the phone, which
is not built.

## What it showed

[checks/hit-test.sh](../checks/hit-test.sh) holds the hit test to the
truth: for each case it takes the verdict, then touches that same point by
coordinates — no check, no aim — and reads what the app received. On the
iPhone 17 Pro simulator all seven agreed: refused where the touch went to
the full cover, to nothing under the plain view, to the hidden overlay
(named as hidden from accessibility) and to the scrim; "reaches" where the
target got it, at a clear point beside the center cover, past the edge
cover, and through the pass-through view. The app's outcome line was
unchanged by the hit tests themselves.

## On a real iPhone

Added 2026-09-30. A phone runs only code signed for it, so the probe cannot
be loaded as a library: the same question is asked as one Objective-C
expression, evaluated in the app by lldb, with the recursion over views made
an explicit stack and UIKit touched on the main thread only. The app must
let a debugger in — `get-task-allow`, which a development build from Xcode
has and an App Store app does not.

lldb reaches an app on a phone through CoreDevice with `device process
attach`. In batch mode that attach never surfaces the stop: the process
read as running and stopped at once, `process interrupt` answered that it
must be launched, and an lldb that quit on that error took the app down
with it. So the probe runs as a command inside `xcrun lldb`'s own Python
(`internal/device/hitprobe/phone.py`), on a debugger of its own in
asynchronous mode, waits for the stop as an event, and always detaches; no
Python of the user's is involved. Measured on an iPhone 15 Plus, iOS
26.6.2: about nine seconds a case, five of them the attach, and all seven
cases of the Obstruction Demo agreed with where a raw touch went
(`docs/checks/hit-test.sh`), as they do on a simulator.

## Loaded at launch, on a simulator

Added 2026-09-30, revisiting "Why opt-in". `launch --hit-test` (`hit_test`
on `app_launch`, `LaunchWithHitTest` in the clients) loads the probe as the
app starts, through `DYLD_INSERT_LIBRARIES` in the environment
WebDriverAgent launches it with, and the probe listens on a Unix socket
named in `MOBIUM_HIT_SOCKET`: a file under `MOBIUM_HOME`, created readable
by its user only. Both objections to this were weighed again:

- *A listener inside the app under test.* The socket is a file on the
  Mac's own disk, which a simulator's apps share; nothing listens on a
  network, and the library unsets `DYLD_INSERT_LIBRARIES` as it loads.
- *Only apps Mobium launched.* Still true, and stated: an app without the
  probe is not asked, and the per-action check is opt-in per launch. A
  relaunch Mobium makes itself — for a language or a time zone — loads it
  again; a plain `launch` does not.

With it loaded, every action on an element — tap, long press, check —
asks before touching, in the same place the tree's cover checks run
(`resolveAim`), and is refused as `hit-test` refuses, after waiting out
the implicit wait as a cover is. An answer of unknown, which comes for an
element named only by its label, leaves the action to the tree's checks.
`hit-test` itself answers from the loaded probe when there is one.

Measured on the iPhone 17 Pro simulator: an answer in 0.2 to 0.5 ms over
the socket, against about two seconds through lldb; `hit-test` from the
CLI in 0.66 s, all of it the CLI and the read of the screen; a tap on
the pass-through target at a median of 621 ms with the probe and 621 ms
without. `checks/hit-test.sh` runs the seven cases as plain taps: the
three that reach were tapped and the target got each, and the four that
do not were refused with the app untouched — while without the probe,
the hidden overlay's and the plain view's taps went through, the positive
control.

**Not on a real iPhone.** A probe loaded there would have to listen on
the phone's network for Mobium to reach it, and opening a service on
somebody's phone is not something Mobium does; `launch --hit-test`
refuses, saying so, and `hit-test` there stays the nine-second debugger
call.

## What it does not do

- It is not consulted by `tap` unless the app was launched with
  `--hit-test`, on a simulator. Otherwise a test that needs the guarantee
  calls `hit-test` before the tap.
- It answers for the key window. A system alert is another process, and has
  checks of its own (CHALLENGES 105).
- It does not say whether a receiver will *handle* the touch. React Native
  decides that in JavaScript: the plain view is the receiver and handles
  nothing, so the tap goes nowhere — which the check reports as the touch
  reaching the plain view, not the target, and that is the answer that
  matters.
