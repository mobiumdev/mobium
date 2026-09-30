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

## What it does not do

- It is not consulted by `tap`. A test that needs the guarantee calls it
  before the tap.
- It answers for the key window. A system alert is another process, and has
  checks of its own (CHALLENGES 105).
- It does not say whether a receiver will *handle* the touch. React Native
  decides that in JavaScript: the plain view is the receiver and handles
  nothing, so the tap goes nowhere — which the check reports as the touch
  reaching the plain view, not the target, and that is the answer that
  matters.
