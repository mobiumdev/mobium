# Device checks

Scripts that drive a real flow end to end through mobium's CLI and assert on
the result. Not part of `make ci` — they need a device — but they are the
thing [RELEASE-CHECKLIST.md](../RELEASE-CHECKLIST.md) asks you to run, written
down so nobody has to invent them again.

```sh
./docs/checks/calculator.sh emulator-5554
./docs/checks/calculator.sh <serial>
```

Getting a device attached in the first place is [../SETUP.md](../SETUP.md).

Each script takes a device serial and exits non-zero on failure.

| Script | What it proves |
| --- | --- |
| [calculator.sh](calculator.sh) | A full type-and-read loop: 1 + 1 = 2 in Google Calculator, every step asserted against a named element |
| [clock-timer.sh](clock-timer.sh) | Tab navigation, a keypad whose keys have no accessibility label, and one app in two versions whose screens differ |
| [clean-stop.sh](clean-stop.sh) | That nothing is left running afterwards, **on the machine and on the device** — instrumentation, `/data/local/tmp`, `/sdcard`, port forwards. `--quit` stops it all first; see [../SHUTDOWN.md](../SHUTDOWN.md) |
| [ios-webview-probe.sh](ios-webview-probe.sh) | That the iOS WebView path still works, without any Mobium code. Evidence for [../decisions/0002](../decisions/0002-ios-webviews-are-reachable.md); it verifies a *platform* assumption, so it failing means the plan needs revisiting |
| [external-driver.sh](external-driver.sh) | That a third-party driver process can drive a device as well as a built-in backend. Evidence for [../decisions/0003](../decisions/0003-drivers-are-processes-not-plugins.md) |
| [ios-webview.sh](ios-webview.sh) | That mobium drives a WKWebView end to end — discovery, switching, reading, and refusing coordinates it cannot compute. Evidence for [../decisions/0002](../decisions/0002-ios-webviews-are-reachable.md) |
| [device-state.sh](device-state.sh) | Rotation, per-app language, timezone, notifications and **geolocation**, each read back — and the only test of non-Latin text on a real screen |
| [third-party-app.sh](third-party-app.sh) | A genuine third-party **hybrid** app — install, feed, search, article, WebView. The only check driving an app nobody at Google or Apple wrote. **Given an iPhone's UDID it runs [third-party-app-ios.sh](third-party-app-ios.sh)**: Wikipedia from the App Store, which cannot run on a simulator — onboarding, the feed's cards, search, an article's links read natively, and a link followed by tapping it, since a phone's WebViews cannot be attached yet |
| [gestures.sh](gestures.sh) | That a long press is a long press, that a rotation is a rotation, and that a drag and a double tap are not a swipe and two taps. Both had shipped with no evidence behind them: a long press delivered as a tap is indistinguishable from outside, so the check asserts the *difference* and that `--duration` reaches the gesture; and a two-finger turn is measured by a WebView that computes its own angle from `touchmove`, in both directions, with the refusals. **The drag is asserted against a swipe over the same two points** — same landing, same travel, and holds of 715ms against 8ms, so an assertion that only said "it moved and landed" would pass on a swipe. The double tap is asserted against the browser's own `dblclick` on Android; **on iOS it asserts something weaker and says so where it runs**, because no witness there can see one ([CHALLENGES 70](../CHALLENGES.md)) |
| [ios-device.sh](ios-device.sh) | A **real iPhone**, through Apple's Settings: app switches in both directions and through Home, **each timed** — the first read after a switch once stalled for 61 seconds ([CHALLENGES 71](../CHALLENGES.md)) and a 40-second limit is what catches it coming back; scrolling onto rows the phone reports with no bounds; typing confirmed by read-back; a screenshot; and every refusal a phone owes, with its reason. Changes no setting |
| [record.sh](record.sh) | **Screen recording**: something moving recorded, saved where the command ran, and judged by the video's own header — frames and duration — then a still screen, fewer frames and still a valid file, the refusals, and nothing left recording or on the device. A real iPhone's refusal is asserted to name its reason |
| [autowait.sh](autowait.sh) | **Auto-wait**, judged by what MobiumApp received: a tap on a target sliding in waits for the slide to end (the Motion Demo's own stopwatch), and with Reduce Motion on the honoring target is tapped at once while the ignoring one still waits; a still button beside a confetti burst is tapped, and a piece that never holds still is refused; a second tap on Log In waits out "Signing in…" and lands; typing into a button, a checkbox and a read-only field is refused. Simulators and emulators: it switches Reduce Motion from outside, and puts it back |
| [dialogs.sh](dialogs.sh) | **Every kind of dialog** on MobiumApp's Dialog Demo, each judged by what the app says it received: alerts with one, two and three buttons, one that arrives late, the iOS action sheet, the share sheet, camera and location permission, App Tracking Transparency on iOS, and a paste — iOS's Allow Paste prompt refused and granted, Android asking nothing. `accept` and `dismiss` are held to what they were measured to press on each platform (CHALLENGES 106), and answering by caption to meaning the same thing on both. Then a declared rule answering a dialog on the way to a tap, a rule for a button the dialog lacks refused, and the refusals under a dialog and under the keyboard, with each remedy followed. Simulators and emulators only: a real iPhone cannot reset permissions from outside |
| [obstruction.sh](obstruction.sh) | **Receives events**, on MobiumApp's Obstruction Demo, judged by what the app says each tap touched: a control over all of a target is refused and named, one over its center is aimed around, a toast is waited out; a pass-through view, the negative control, lets the tap reach its target, and a plain view that swallows it is reported. On iOS an overlay hidden from accessibility is asserted as the known blind spot. Emulators, simulators and phones ([CHALLENGES 115](../CHALLENGES.md)) |
| [web-actionability.sh](web-actionability.sh) | **Actionability inside a WebView**, on MobiumApp's Actionability page, judged by what the page says each tap reached: a sliding target is tapped once it stops, disabled and `aria-disabled` ones are refused, one enabled late is waited for, a full cover and a plain div are refused by the page's own hit test, a covered center is aimed around, the `pointer-events: none` layer — the negative control — lets the tap through, and a target below the fold is scrolled into view. Emulators, simulators and phones ([CHALLENGES 118](../CHALLENGES.md)) |
| [web-type.sh](web-type.sh) | **Typing inside a WebView**, on MobiumApp's Web form page, judged by what the page says each field holds: text with an apostrophe, `&` and non-ASCII arrives exactly and the page hears the change, an empty type clears, read-only, `aria-readonly`, disabled and checkbox fields are refused, and a password is confirmed by the page without ever being printed. Emulators, simulators and phones ([CHALLENGES 119](../CHALLENGES.md)) |
| [accessibility.sh](accessibility.sh) | **Accessibility settings** through `app_accessibility`: every setting the platform has is changed, MobiumApp's Accessibility Demo is shown to be told of each it can see, what the platform lacks is refused with the alternative, and once the daemon stops every raw value is back as it was — an unset key unset again. On a real iPhone the refusal is asserted. Emulators, simulators and phones |
| [login.sh](login.sh) | **The login demo** — mobile automation's hello world, on MobiumApp's Login screen, and the script to show when showing Mobium. Negative paths first, each judged by the message the app shows: an empty form, a username too short, one with characters it does not allow, a password too short, a wrong password and an unknown user, told apart by nothing. Then positive: one field valid while the other is not, a username sanitized on submit, the welcome screen waited for rather than slept for, and log-out. A typed password is never printed. If iOS raises "Save Password?" over the app, log-out is skipped and the dialog quoted, rather than a swallowed tap reported as a timeout |
| [source.sh](source.sh) | **The raw source** on MobiumApp: the native hierarchy well-formed and in the platform's units — px on Android, pt with a scale on iOS — with a typed password nowhere in it; then a WebView's markup with a password planted in the page's own markup, hidden in the answer and still in the page. The native redaction's positive control is `internal/uitree`'s test on a captured Aegis screen |
| [clear-data.sh](clear-data.sh) | **Clearing an app's data**, with MobiumApp's Storage Demo as the witness: a count raised and shown to survive a relaunch, so that 0 afterwards means something; cleared; and 0 and no file read back from the app itself. On Android, a location grant read back as granted and then as revoked. An app that is not installed is refused, and so is a real iPhone, naming the reason |
| [keyboard.sh](keyboard.sh) | **The soft keyboard** on MobiumApp's Login screen, on any of the four devices: up once a field has focus, an empty field read as empty rather than as its placeholder, two appends kept (Android replaces a field on every keystroke, so this is the check that it reads and writes back), delete, a password typed and printed nowhere, hiding confirmed — and on an iPhone, the refusal to hide naming enter, and enter working |
| [clients.sh](clients.sh) | **All five clients driven**, not only compiled: Python, JavaScript, Go, Java and .NET each run one flow through their own API against a booted Android device, from a test that sits beside the client's unit tests and skips itself unless `MOBIUM_E2E_DEVICE` is set. A client whose toolchain is missing is reported as skipped, never as passed |
| [crashes.sh](crashes.sh) | **Device logs, crash reports and ANRs** — the ANR caused on purpose where adb can become root, by freezing MobiumApp and touching it, and checked through its dialog, its line in the app-filtered log and its report. **Device logs and crash reports**, on an Android device, an iOS simulator or a real iPhone, each against a positive control: a line logged between two reads is the next read's, a burst past the limit is counted rather than hidden, and a crash caused on purpose — `am crash` on Android, an injected `abort()` on a simulator, MobiumApp's Crash Demo on a phone, and on Android too when MobiumApp is installed — appears scoped to its app, reads in full with its cause, and is listed again, since a crash is a record and not a stream |
| [zoom.sh](zoom.sh) | Pinch-to-zoom, the first two-finger gesture — and the only thing that can confirm one. Neither platform reports a zoom level in the hierarchy, so it pinches a WebView and asks the page for its own `visualViewport.scale` |
| [mobium-app.sh](mobium-app.sh) | A genuine hybrid app on **both** platforms: an embedded WebView that opts into inspection, two live contexts in one app, a password field, **a GPX route watched from inside the app**, and **an
interruption**: a real system permission dialog, and whether the app still
holds what was typed before it. One script, backend chosen from the serial. Evidence for [../decisions/0004](../decisions/0004-an-app-under-test-of-our-own.md) |

## Results

Run 2026-09-13. Both emulators are the same Pixel 7 profile — 1080x2400 at
420dpi — so the only difference between them is the OS.

| Device | OS | calculator.sh | clock-timer.sh | external-driver.sh | third-party-app.sh | device-state.sh |
| --- | --- | --- | --- | --- | --- | --- |
| Pixel 7 AVD | Android 15 (API 35) | PASS 6.4s | PASS 5.5s | PASS | PASS | PASS |
| Pixel 7 AVD | Android 17 (API 37) | PASS 6.9s | PASS 5.6s | not run | not run | not run |
| Pixel 8 Pro | Android 17 (API 37) | PASS 11.3s | PASS 16.5s | not run | not run | not run |

`ios-webview.sh` runs against a simulator rather than a device and so has no
row here; it passed on an iPhone 17 Pro, iOS 26.5, on 2026-09-14.

`third-party-app.sh` passed on the **iPhone 15 Plus** on 2026-09-23, in 2m20s —
Wikipedia 2026.09.15 from the App Store, from its feed to Charles Babbage's article by
way of a native tap on a web link. Its first run found defects 77–80.

`ios-device.sh` passed on an **iPhone 15 Plus, iOS 26.6.2**, on 2026-09-22, in
2m07s. App switches read back in 6.1–16.2s. With the fix for defect 71
disabled it failed on its second launch, at 64.8s — the run that makes its
time limit a check rather than a guess.

**Everything added since has run on the iPhone 15 Plus too, on 2026-09-27**,
with MobiumApp rebuilt from `mobiumdev/mobium-app` and signed for the phone.
All passed: `login.sh` (2m45s), `keyboard.sh`, `autowait.sh`,
`obstruction.sh`, `web-actionability.sh`, `web-type.sh`, `source.sh`,
`crashes.sh`, `ios-device.sh` (1m53s), `gestures.sh` (4m55s), `zoom.sh`,
`mobium-app.sh` (4m06s, with `MOBIUM_NETWORK_TESTS=1`: the link now lands on
`github.com/mobiumdev`) and `third-party-app.sh`; and `record.sh`,
`accessibility.sh` and `clear-data.sh` assert the phone's refusals. Two
checks needed changing for a phone, and neither change was to Mobium:
`login.sh` met iOS's "Save Password?" sheet over the welcome screen, and now
answers it; and `autowait.sh` refused a phone outright, and now checks the
half of Reduce Motion the phone is set to — **three of its rows read NOT
CHECKED** on this phone, where Reduce Motion is on: the plain slide, and both
confetti rows, since the app draws no confetti then. `dialogs.sh` is
simulator-only and was not run.

`mobium-app.sh` needs MobiumApp built and installed, so it has no row either.
**On the iPhone 15 Plus it passed on 2026-09-23**, 30 steps in 3m31s, from a
fresh install it makes itself when given `MOBIUMAPP_BUNDLE` — **WebViews
included**, two at once, reached through lockdown. Location and the clipboard
are skipped there with their refusals asserted, the two things a phone cannot
do, and everything native ran as on the simulator: redaction, the pager in both axes, the real permission dialog and
what survives it, the app's own alerts and prompt, and the whole Form screen.
It found defect 76. The simulator passed the same script afterwards, all of
it.

It passed on both on **2026-09-18** — an iPhone 17 Pro simulator (iOS 26.5) and
the Pixel 7 AVD (Android 15) — covering WebViews, geolocation and routes, a
horizontal pager and the clipboard. It first passed on 2026-09-17, on Android
only after it found defect 55.
Do not run it while a build is installing to the same device: that produces a
failure message identical to the real one.

**Two kinds of dialog are driven, and they are not the same problem.** The
Interruption section raises a *system* permission prompt — another process's
window, which `app_current` reports while it is up. The Dialog Demo section
raises the app's **own** alerts (the App Alert Demo's, until they were merged
into it on 2026-09-26), and is what measured CHALLENGES 63: on an ordinary
two-button alert `accept` and `dismiss` read as confirm and cancel, and on
Apple's permission prompt they invert. Which button each presses is not
positional — CHALLENGES 106 has the three-button alert and the action sheet
that showed it. The prompt half is iOS-only and the Android branch asserts
the refusal rather than skipping, since a silent skip cannot be told from an
untested one.

**The interruption section asserts two things, and the second is why the first
means anything.** After the dialog clears, one draft still holds its text and
another is empty — the fields look identical, so a screenshot cannot tell them
apart and neither could a check that only asserted the screen came back. The
second assertion proves the check can see state loss at all; without it, the
first would pass on a tool that could not detect anything.

**The two platforms need different locators for the same card**, and the script
says so where it branches. React Native renders one labeled `Pressable` as a
single labeled node on Android and three on iOS, so a plain `label=` is
ambiguous there — mobium refuses it rather than tapping the first, which is the
rule working, not a limitation. The iOS branch narrows by `,role=button`.

**One assertion is opt-in.** Every "Learn more" in MobiumApp is a real link to
`https://github.com/mobiumdev`, and the check asserts the *target* by asking the
page (`document.querySelector('a#link').href`) rather than by following it. The
href is a property of the app and is true whether or not anything is served at
the other end; following it depends on DNS, a certificate and somebody keeping
a server running.

`MOBIUM_NETWORK_TESTS=1` follows it, and then asserts the host, the path and
the title. It was verified on both platforms when the link pointed at the
author's GitHub profile; the move to `github.com/mobiumdev` has not been
followed on a device yet. The title is matched on the **handle** rather than the
display name: one is part of the URL and stable, the other is a profile field
anybody can edit.

The link pointed at `mobium.ai` first, which is why the split exists at all:
that domain serves nothing — it resolves to a registrar parking page on plain
http and times out on https, which iOS blocks. The href assertion passed
throughout; only the following of it could not.

Its geolocation section is the only place a position is checked **from inside
an app**, which is the only way to check one on iOS at all: `simctl location`
has no `get`. It asserts the coordinates *move* during a route and *settle*
after a clear — not that they stop changing, since clearing reverts the device
to its own position and that is one more change. See CHALLENGES 59 for how
both halves of that were got wrong first.

Two things fall out of that table.

**The OS version costs almost nothing.** Android 15 to 17 on the same emulator
profile is within noise on both checks. Nothing in Mobium's hierarchy parsing,
coordinate handling or locator vocabulary needed to change across two major
releases.

**Real hardware costs about 2x**, and it is not the OS — the Android 17
emulator and the Android 17 phone differ only in being emulated or not.

## Why the external-driver check diffs two backends

The other checks assert against something the *app* computed, which is what
makes them end to end. A driver check cannot do that: a driver that returns a
plausible but wrong hierarchy would pass every "did it tap, did it scroll"
assertion, because everything downstream would agree with it.

So the load-bearing step is a **diff against a known-good implementation on the
same screen**. The reference driver deliberately covers the same ground as
`--driver uiautomator`, so the two maps must be identical, and the script
compares them line for line with the daemon stopped in between — leaving it up
would hand the second map the first backend's cached session, and the diff
would be comparing a backend against itself, which passes.

Both halves were confirmed by breaking the driver on purpose. Dropping
`clickable` from what it sends failed the element-count guard (11 elements down
to 2); dropping `resource-id` kept the count identical and failed the diff,
naming the two rows that changed. A check that has only ever been green is not
evidence that it can go red.

## Why Wikipedia, and what it cost

Everything else here drives an app that ships with the OS. That is convenient
and it is not representative: every rule Mobium has about labeling, collapsing
and filtering `map` output was tuned on Settings, Calculator and Clock, and
Google writes those to be accessible.

Wikipedia's official Android client was chosen because it is genuinely
third-party, large, widely used, **hybrid** — its article view is a WebView, so
one app covers native and WebView — and open source, so the APK comes from
F-Droid rather than somewhere that needs an account or trust:

```sh
curl -L -o wikipedia.apk https://f-droid.org/repo/org.wikipedia_50606.apk
docs/checks/third-party-app.sh emulator-5554 wikipedia.apk
```

It broke three things on the first screen it showed — an 876-character label
with raw HTML in it, an impossible error message inside the WebView, and
composite labels that cannot be matched by the text they display
([CHALLENGES 40-42](../CHALLENGES.md)). The check now asserts the first two
cannot come back: no label over 130 characters, no markup, no empty labels, and
the WebView refusal must explain itself rather than saying "unknown ref".

Confirmed it can fail, by raising `maxLabel` to 4000 and watching it report a
758-character label and exit non-zero.

## Why an arithmetic test

It is the smallest flow that is genuinely end to end. It launches an app,
waits for it, taps five times, reads a field after each tap, and checks a
value the *app* computed rather than one mobium put there. A screenshot
comparison or an element count would pass on a screen that is subtly wrong;
`1 + 1 = 2` would not.

## The Calculator is not on the emulator image

The Pixel 7 AVD's Google APIs image ships 241 packages and none of them is a
calculator. To make the two runs comparable the same APK was pulled off the
Pixel and installed on the emulator:

```sh
adb -s <phone> shell pm path com.google.android.calculator
adb -s <phone> pull <base.apk> ; adb -s <phone> pull <split_config.xxhdpi.apk>
adb -s <emulator> install-multiple -r base.apk split_config.xxhdpi.apk
```

Testing two different calculators would have proved less than testing one.

## The Clock is the same app in two different shapes

Clock is present everywhere, but at **7.5** on the emulators and **9.1** on the
Pixel, and the two do not agree:

| | Clock 7.5 | Clock 9.1 |
| --- | --- | --- |
| Tab name | "Timer" | "Timers" |
| Duration display | one field, `id/timer_setup_time` = `00h 01m 23s` | three, `id/hour_text` `id/minute_text` `id/second_text` |
| Reaching the keypad | it is the tab | behind "Add timer" once any timers are saved |

The keypad ids are identical in both, which is why the script addresses
everything by resource-id and normalizes the display to `HH:MM:SS`. This is
the most useful thing the second device bought: a version difference in an
app, which no amount of testing on one emulator would have surfaced.
