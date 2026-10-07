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

Most scripts take a device serial or UDID as their first argument, and every
one exits non-zero on failure. The exceptions say so in their headers:
`clean-stop.sh` takes no device, `boot.sh` takes an AVD name and an optional
simulator UDID, `ios-webview.sh` defaults to the booted simulator,
`test-runner.sh` and `test-ui.sh` run on `emulator-5554` and read
`MOBIUM_IOS_DEVICE` for iOS, and `test-grid.sh` reads `MOBIUM_GRID`.

## Running several at once

Every check sources [lib.sh](lib.sh), which keeps it from reaching into
another:

- **A daemon of its own** (`MOBIUM_SESSION`), stopped when the check ends.
  Checks once shared one: a check that ended on the `uiautomator` backend
  handed it to every check after, and one run's `daemon stop` ended another
  device's trace. A session the caller set, as a grid run's or
  `mobium test`'s, is kept.
- **One check per device.** `check_lock` takes the device, and a second
  check on it is refused, naming the first; a lock whose holder has gone is
  taken over. A check that runs another shares its lock.
- **A folder of its own**, `CHECK_TMP`, removed at the end, and `at_exit` for
  cleanup in place of `trap ... EXIT`, which would replace the prelude's.

So checks on different devices can run side by side:

```sh
./docs/checks/obstruction.sh emulator-5554 &
./docs/checks/obstruction.sh <simulator-udid> &
wait
```

What they share is the machine: an emulator under load stopped answering
`adb` once, mid-sweep, so start with as many devices as the Mac drives
comfortably. `make ci` runs [lib-selftest.sh](lib-selftest.sh), which tests
the prelude without a device and fails when a check stops using it. Four
checks do not: `clean-stop.sh`, which stops the caller's daemon, and
`test-runner.sh`, `test-grid.sh` and `test-ui.sh`, which test the sessions
`mobium test` and the grid give themselves.

| Script | What it proves |
| --- | --- |
| [calculator.sh](calculator.sh) | A full type-and-read loop: 1 + 1 = 2 in Google Calculator, every step asserted against a named element |
| [clock-timer.sh](clock-timer.sh) | Tab navigation, a keypad whose keys have no accessibility label, and one app in two versions whose screens differ |
| [clean-stop.sh](clean-stop.sh) | That nothing is left running afterwards, **on the machine and on the device** — instrumentation, `/data/local/tmp`, `/sdcard`, port forwards. `--quit` stops it all first; see [../SHUTDOWN.md](../SHUTDOWN.md) |
| [ios-webview-probe.sh](ios-webview-probe.sh) | That the iOS WebView path still works, without any Mobium code. Evidence that iOS WebViews are reachable ([SETUP](../SETUP.md#webviews-and-safari)); it verifies a *platform* assumption, so it failing means the plan needs revisiting |
| [external-driver.sh](external-driver.sh) | That a third-party driver process can drive a device as well as a built-in backend. Evidence for [the driver protocol](../../examples/drivers/PROTOCOL.md) |
| [back.sh](back.sh) | **Back**, on any device: on Android the key and the gesture from a MobiumApp demo to its home, each saying the app stayed in front; from home, saying it left; Settings as the control; on an emulator, three-button navigation refusing the gesture and the key still going back, then gestures put back and read back. On iOS `press back` refused naming the gesture; the gesture in Settings > Search (simulator) or NetNewsWire (phone) going back and giving the bar's new title; and in MobiumApp, which has no swipe back, nothing moving and it said ([BACK.md](../BACK.md)). Needs MobiumApp from mobium-app #12 on. Passed 2026-10-01 on the Pixel 8 Pro, both AVDs, the simulator and the iPhone 15 Plus |
| [device-paths.sh](device-paths.sh) | **Files and folders at a named path**: `upload` and `download --device-path`, byte for byte. Android: a shell path in `/data/local/tmp`, a release build's private data refused as not debuggable, and with `MOBIUM_DEBUGGABLE_APP` that app's private data through run-as. iOS: MobiumApp's container, and its own `Library/Preferences`. A `..` path refused, a folder never merged into one that exists, and everything written removed and read back as gone. On a phone, where CoreDevice has no delete, the check's folder is emptied down to one empty marker in the app's tmp/, which iOS clears. Passed 2026-10-02 on the Pixel 7 AVD, the Pixel 8 Pro (private data with the debug build, the release build put back), the iPhone 17 Pro simulator and the iPhone 15 Plus |
| [flutter.sh](flutter.sh) | **A Flutter app** — MobiumApp's `flutter/` demo, installed by hand — on any device, through the semantics tree and no driver: `map` names every control; typing reaches the app, judged by what the app says it got, the password not echoed; a Semantics identifier taps its button; check, uncheck, a counted tap; `scroll-to` the fortieth row; back from a pushed screen. Passed 2026-10-01/02 on the Pixel 7 AVD, the iPhone 17 Pro simulator, the Pixel 8 Pro and the iPhone 15 Plus ([CHALLENGES 206](../CHALLENGES.md)) |
| [twa.sh](twa.sh) | **A PWA from the Play Store** — a Trusted Web Activity, OYO Lite by default, installed by hand — on an Android phone: checked to be a TWA by its task, `launch` saying so, its page attached by the site's host and standalone, a tap counted by the page, and back through a tapped link's history and then out, each naming the app. Passed on the Pixel 8 Pro on 2026-10-01 ([CHALLENGES 205](../CHALLENGES.md)) |
| [chrome.sh](chrome.sh) | **A page in Chrome on Android**, on an emulator or a phone, with the network: listed by its title and URL, `text`, `eval` and `map` answered by the page, a tap on a ref counted by a button put on the page, and the page's own link followed to iana.org. On an emulator it starts Chrome fresh first ([CHALLENGES 200](../CHALLENGES.md)); on a phone it leaves Chrome running, prints no listing, and closes its tab |
| [pwa.sh](pwa.sh) | **A progressive web app**, Squoosh, on an emulator, an Android phone or a simulator, with the network: installed through Chrome's menu or Safari's share sheet when its icon is absent, launched from the icon, running standalone with an active service worker, its context found by its start URL. iOS: a tap in the web context refused with the reason, and from `NATIVE_APP` counted by the page. Android phone: installed as a WebAPK, launched by its package with `app_launch` saying so ([CHALLENGES 202](../CHALLENGES.md)), the tap counted by the page, and the WebAPK and the tabs removed afterwards. Emulator: the tap counted, or refused because Chrome stopped reporting its WebView and the page counted nothing ([CHALLENGES 200](../CHALLENGES.md)). An iPhone refused |
| [ios-webview.sh](ios-webview.sh) | That mobium drives a WKWebView end to end — discovery, switching, reading, and a tap in Safari's page placed from text both sides report, held to the page the link opens (CHALLENGES 248). Evidence that iOS WebViews are reachable ([SETUP](../SETUP.md#webviews-and-safari)) |
| [device-state.sh](device-state.sh) | Rotation, per-app language, lock, a call and an SMS (on an emulator; a phone's refusal otherwise), timezone, notifications and **geolocation**, each read back — and the only test of non-Latin text on a real screen |
| [third-party-app.sh](third-party-app.sh) | A genuine third-party **hybrid** app — install, feed, search, article, WebView. The only check driving an app nobody at Google or Apple wrote. **Given an iPhone's UDID it runs [third-party-app-ios.sh](third-party-app-ios.sh)**: Wikipedia from the App Store, which cannot run on a simulator — onboarding, the feed's cards, search, an article's links read natively, and a link followed by tapping it. A phone's WebViews can be attached, but the App Store build does not opt into inspection, so the check asserts it publishes nothing |
| [gestures.sh](gestures.sh) | That a long press is a long press, that a rotation is a rotation, and that a drag and a double tap are not a swipe and two taps. Both had shipped with no evidence behind them: a long press delivered as a tap is indistinguishable from outside, so the check asserts the *difference* and that `--duration` reaches the gesture; and a two-finger turn is measured by a WebView that computes its own angle from `touchmove`, in both directions, with the refusals. **The drag is asserted against a swipe over the same two points** — same landing, same travel, and holds of 715ms against 8ms, so an assertion that only said "it moved and landed" would pass on a swipe. The double tap is asserted against the browser's own `dblclick` on Android, and on iOS against a React Native control that counts two presses inside the double-tap window where two separate taps land outside it ([CHALLENGES 149](../CHALLENGES.md)) |
| [ios-device.sh](ios-device.sh) | A **real iPhone**, through Apple's Settings: app switches in both directions and through Home, **each timed** — the first read after a switch once stalled for 61 seconds ([CHALLENGES 71](../CHALLENGES.md)) and a 40-second limit is what catches it coming back; scrolling onto rows the phone reports with no bounds; typing confirmed by read-back; a screenshot; and every refusal a phone owes, with its reason. Changes no setting |
| [audio.sh](audio.sh) | **Audio capture on an Android emulator**, on MobiumApp's Audio Demo: 440 Hz for 2 s heard as such, 440 then a second of silence then 880 in order, 660 Hz played as an alarm, a tone ended at Stop, and the silence control heard as silence while `dumpsys audio` reports a player started for it — the contrast the capture exists for. Each assertion was shown to fail on a wrong expectation. Refuses a phone and an iOS device by name |
| [record.sh](record.sh) | **Screen recording**: something moving recorded, saved where the command ran, and judged by the video's own header — frames and duration — then a still screen, fewer frames and still a valid file (on a real iPhone, whose stream sends frames at a steady rate, a steady rate of them), the refusals, and nothing left recording or on the device |
| [locale-ios.sh](locale-ios.sh) | **Per-app language on iOS**, read back from the app: Settings pinned to ja-JP reads 一般 for General — relaunched by the pin, and again on a later launch — and General once cleared; a malformed tag is refused. iOS has no stored per-app language, so this holds Mobium's launch arguments to what the app shows. Android's half is `device-state.sh` |
| [notifications-ios.sh](notifications-ios.sh) | **Notifications on an iOS simulator**, read back from Notification Center: a post as MobiumApp confirmed by its banner with MobiumApp still in front; the read finding it by app and body and closing Notification Center again; the shade opened so `map` reads Notification Center and finds it, then closed back to MobiumApp; and a post with the home screen in front refused. On a real iPhone, only the refusal is checked |
| [timezone-ios.sh](timezone-ios.sh) | **The time zone on iOS**, read back from Calendar, which marks the hour in progress: the device's hour, then Asia/Tokyo's once set — the app in front relaunched, and a later launch too — and the device's again once set back; an unknown zone refused. Only hour rows are read, so a phone's events are never printed |
| [audit.sh](audit.sh) | **Apple's accessibility audit**, held to two known screens: Settings, at least one finding, each with a type and summary, bounds on screen where it names an element and a locator that resolves (a phone's contrast findings name none, and a phone prints only counts); and MobiumApp's Layout Demo, no findings, while `screen --inspect` names its 24pt target — Apple's hit-region rule is not the 44pt guideline. Android refuses, naming what it has instead |
| [orientation.sh](orientation.sh) | **Orientation**, read back: an app that turns — Settings on Android, Safari on iOS — turned to landscape, landscape-reverse and portrait; MobiumApp, which is portrait only, refused and left upright; on iOS, portrait-reverse and `auto` refused with the reason; and the device left in portrait, Android's rotation lock handed back if it was found free |
| [autowait.sh](autowait.sh) | **Auto-wait**, judged by what MobiumApp received: a tap on a target sliding in waits for the slide to end (the Motion Demo's own stopwatch), and with Reduce Motion on the honoring target is tapped at once while the ignoring one still waits; a still button beside a confetti burst is tapped, and a piece that never holds still is refused; a second tap on Log In waits out "Signing in…" and lands; typing into a button, a checkbox and a read-only field is refused. Simulators and emulators: it switches Reduce Motion from outside, and puts it back. On a real iPhone, which does not allow that, the half its own setting selects is checked and the other reported NOT CHECKED |
| [dialogs.sh](dialogs.sh) | **Every kind of dialog** on MobiumApp's Dialog Demo, each judged by what the app says it received: alerts with one, two and three buttons, one that arrives late, the iOS action sheet, the share sheet, camera and location permission, App Tracking Transparency on iOS, and a paste — iOS's Allow Paste prompt refused and granted, Android asking nothing. `accept` and `dismiss` are held to what they were measured to press on each platform (CHALLENGES 106), and answering by caption to meaning the same thing on both. Then a declared rule answering a dialog on the way to a tap, a rule for a button the dialog lacks refused, and the refusals under a dialog and under the keyboard, with each remedy followed. On a real iPhone it needs `MOBIUMAPP_BUNDLE` and reinstalls the app, the only permission reset a phone has, and paste is not checked |
| [obstruction.sh](obstruction.sh) | **Receives events**, on MobiumApp's Obstruction Demo, judged by what the app says each tap touched: a control over all of a target is refused and named, one over its center is aimed around, a toast is waited out; a pass-through view, the negative control, lets the tap reach its target, and a plain view that swallows it is reported. On iOS an overlay hidden from accessibility is asserted as the known blind spot. Emulators, simulators and phones ([CHALLENGES 115](../CHALLENGES.md)) |
| [web-actionability.sh](web-actionability.sh) | **Actionability inside a WebView**, on MobiumApp's Actionability page, judged by what the page says each tap reached: a sliding target is tapped once it stops, disabled and `aria-disabled` ones are refused, one enabled late is waited for, a full cover and a plain div are refused by the page's own hit test, a covered center is aimed around, the `pointer-events: none` layer — the negative control — lets the tap through, and a target below the fold is scrolled into view. Emulators, simulators and phones ([CHALLENGES 118](../CHALLENGES.md)) |
| [web-type.sh](web-type.sh) | **Typing inside a WebView**, on MobiumApp's Web form page, judged by what the page says each field holds: text with an apostrophe, `&` and non-ASCII arrives exactly and the page hears the change, an empty type clears, read-only, `aria-readonly`, disabled and checkbox fields are refused, and a password is confirmed by the page without ever being printed. Emulators, simulators and phones ([CHALLENGES 119](../CHALLENGES.md)) |
| [web-storage.sh](web-storage.sh) | **Cookies and web storage inside a WebView**, on MobiumApp's Web storage page — a page with an origin of its own, loaded under `https://mobiumapp.test/`, which never resolves. Every step asks the page what it holds: starting empty, what the page writes read by `cookies` and `storage -o`, cleared to nothing and restored, and a cookie set and cleared by name. Emulators and simulators |
| [battery.sh](battery.sh) | **The battery**, held to MobiumApp's Battery Demo, which reads it through the platform's API: on an emulator the console sets 42% and then 73%, unplugged and then charging, and `mobium battery` and the app must both follow; on a simulator both must say there is no battery. The emulator's battery is put back |
| [files.sh](files.sh) | **Files to and from the device**, `upload` and `download`, held to MobiumApp's Files Demo. The app saves a numbered report where the device keeps downloads, twice, and each download must bring back that save's number and size. A file with a name nobody could guess is uploaded, listed, and picked in the system file picker, and the app must say its name, size and first line. That is the Android picker reading MediaStore, which is why an upload waits for the index, and on iOS the Files picker in the app's own folder. A path given as a name and a file that is not there are refused. What it made is removed: on a real iPhone, which cannot delete one file, by reinstalling MobiumApp from `MOBIUMAPP_BUNDLE`; on an Android phone only the two files it made, and it refuses to start over a `mobium-report.txt` already there. On a phone, nothing read off the device is ever printed (CHALLENGES 174). Emulators, simulators and phones |
| [netnewswire-ios.sh](netnewswire-ios.sh) | **A second third-party app on iOS**, NetNewsWire, built from its source for a simulator or from the App Store on a phone, and CHALLENGES 191–197 held against it: no XCUITest type names or disclosure-arrow names in labels, headers not buttons, the chosen search scope read as selected and its move seen by `map --diff`, the back button found apart from a link of the same name, `label=URL` refused naming `text=URL`, a row behind the toolbar scrolled out before it is tapped, and an article row's swipe actions revealed by `swipe <row> left`, Star tapped by name, and the row unstarred again |
| [icecubes-ios.sh](icecubes-ios.sh) | **A third third-party app on iOS, and the first in SwiftUI**: Ice Cubes, a Mastodon client, signed out on a public timeline, built from its source for a simulator or from the App Store on a phone, and CHALLENGES 213–217 held against it: the timeline picker in `map` and opened by ref, a post's Reply, Boost, Favorite and Share in `map` with Share and the image opened by ref, `label=Close` and `label=Display Settings` finding one control each, a tap on the selected tab not reported as covered and going through, and a tab behind the image viewer or the Add Account sheet refused. Nothing that posts is pressed, and nothing a post says is printed |
| [pocketcasts-ios.sh](pocketcasts-ios.sh) | **A fourth third-party app on iOS**: Pocket Casts, a podcast player, signed out, built from its source for a simulator or from the App Store on a phone, and CHALLENGES 234–239 held against it: onboarding's button mapped disabled and `map --diff` saying when it is enabled, a row's checkmark not a target of its own, Discover's rows and a podcast's header named by their words rather than the buttons and images inside them, Play touched clear of the divider through its center, the player's first-run tip said by `map` and `app_alert` and closed where they say, and the scrubber mapped as adjustable with `app_fill` refused. On a simulator, iPhone or iPad, the app's data is cleared first; a phone's onboarding and tip run only on a fresh install, and the app is first brought back to Discover from wherever it was left. An episode is played for a moment and paused |
| [kiwix-ios.sh](kiwix-ios.sh) | **A fifth third-party app on iOS, and the first whose WebView opens**: Kiwix, the offline Wikipedia reader, built from its source for a simulator or from the App Store on an iPhone, and CHALLENGES 244–248 and 256 held against it with a pinned ZIM: each catalog card one entry, off-screen ambiguity told to name one, an article tile one link named once, the disabled List button refused by its own ref, and a tap in the WebView — taller than its page, under the bars — opening the article it names; a page under the Library neither mapped nor tapped. A simulator starts from cleared data, with the ZIM uploaded and opened through the system document picker; a phone keeps its data, so the ZIM must already be in Kiwix's library from its own download, and 245 is reached only with no page under the Library |
| [feed.sh](feed.sh) | **A list that grows as it is scrolled**, on MobiumApp's Feed Demo: twenty rows a page, the next 2.5 seconds after the end is reached, three pages in all. `scroll-to` reaches Row 55 across two loads instead of stopping at the first page's end, says Row 70 is past the real end at once, and an unknown role is refused rather than waited on (CHALLENGES 222, 223). Passed on the simulator, the iPhone and the Pixel 8 Pro |
| [slider.sh](slider.sh) | **Sliders on Android**, on MobiumApp's Slider Demo: Volume (0–100 in tens) and Balance (0–1, continuous) map as sliders by name with a value, are filled by position and land exactly — Volume on 30, 100 and 0, Balance on 0.25 — `map --diff` says the value moved, and a non-number and `type` are refused (CHALLENGES 227). Passed on the Pixel 8 Pro. Android only: on iOS the demo's sliders report no slider at all |
| [frames.sh](frames.sh) | **Frames inside a WebView**, on MobiumApp's Frames page: in the WebView's context every frame's elements are mapped with their frame — same-origin and nested through the page, cross-origin through its own execution context — each button tapped reaches its frame, the cross-origin card field is filled and added to as the frame reports, and `NATIVE_APP` reaches the cross-origin button too (CHALLENGES 228, 229). Passed on the iPhone simulator and an emulator |
| [compose-app.sh](compose-app.sh) | **A Jetpack Compose app**, Seal from F-Droid, the first Compose app driven here, and CHALLENGES 176–178 held against it. Its icon buttons map once each as buttons. Its first-launch dialog, which the platform's alert endpoint does not know, is read by `alert` and not accepted, a tap on what it covers is refused naming it, and a rule answers it by its Close button. A switch row is set by its words, read back and put back, and a row with no state is refused. An app the check installed is uninstalled after, and on a phone nothing read off it is printed. Emulators and phones |
| [map-diff.sh](map-diff.sh) | **`map --diff`**, on MobiumApp's Form Demo. The first diff of a session says it had nothing to compare with and adds the whole screen. A map with nothing done between says nothing changed, the control that it can come back empty. A box checked is its state and what it was, and on iOS, where its row grows, what is under it moves as one line, not five. A screen left is its elements gone and the next screen's added, and a ref from the diff acts. Emulators and simulators |
| [boot.sh](boot.sh) | **`mobium boot` and `mobium shutdown`**, held to what the device then is: an AVD booted from nothing and answering its first call (boot waits for a focused window, not only for boot completed), booted again and said to be running already, shut down by name from a call pinned to another device without touching that one, the same for a simulator by UDID, and a real phone refused. Emulators and simulators |
| [trace.sh](trace.sh) | **`mobium trace`**, a sign-in on MobiumApp's Login Demo traced and the zip held to Vibium's record format and what its player reads. context-options comes first with its name. Every call has a before and an after, and a failed tap carries its error. After every call there is a screencast frame with its image and a snapshot with the map drawn over it, and a tap's point is recorded. A typed password appears nowhere in the zip, only its length. A second start and a stop with none are refused. With `MOBIUM_TRACE_VIEWER=1` and Vibium installed, the zip is opened in Vibium's player at player.vibium.dev, which must count every call and name each step, a fill included. Emulators and simulators |
| [accessibility.sh](accessibility.sh) | **Accessibility settings** through `app_accessibility`: every setting the platform has is changed, MobiumApp's Accessibility Demo is shown to be told of each it can see, what the platform lacks is refused with the alternative, and once the daemon stops every raw value is back as it was — an unset key unset again. On a real iPhone, where Mobium goes through the Settings app, each of the six switches is flipped from how it was found, and after the daemon stops every switch and what the app is told are back as they were (CHALLENGES 160); invert, grayscale and text size are refused naming Settings. Emulators, simulators and phones |
| [screen-reader.sh](screen-reader.sh) | **A screen reader stays on**: TalkBack turned on, then a UiAutomator2 session opened on MobiumApp, and every accessibility service still bound and touch exploration still on — before and after a tap the Motion Demo reports, a scroll and a fill read back. Against a build that starts the server without `DISABLE_SUPPRESS_ACCESSIBILITY_SERVICES` nothing is bound once the session opens (CHALLENGES 208). The accessibility settings are put back exactly, an empty value included. Android; a phone only with `ALLOW_PHONE=1`, since TalkBack takes over its touch |
| [login.sh](login.sh) | **The login demo** — mobile automation's hello world, on MobiumApp's Login screen, and the script to show when showing Mobium. Negative paths first, each judged by the message the app shows: an empty form, a username too short, one with characters it does not allow, a password too short, a wrong password and an unknown user, told apart by nothing. Then positive: one field valid while the other is not, a username sanitized on submit, the welcome screen waited for rather than slept for, and log-out. A typed password is never printed. If iOS raises "Save Password?" over the app, log-out is skipped and the dialog quoted, rather than a swallowed tap reported as a timeout |
| [otp.sh](otp.sh) | **A one-time code**, on MobiumApp's OTP Demo: the code read where it arrives — the notification shade on Android, the banner on iOS, read through rather than landing on SpringBoard (CHALLENGES 155) — six one-digit boxes that move focus on, filled digit by digit and then as one string into the first box, which either arrives whole or names the digit that never did (156); no code, verified, already used, wrong twice, locked, and expired after `background 62`; and Resend refused while the screen disables it |
| [wait-states.sh](wait-states.sh) | **Waiting for a state**, on MobiumApp's Form and Login Demos: `wait --for` checked and unchecked on a custom checkbox, a radio and a real Switch; focused, arriving on a tap and moving to the next field — on iOS by asking WebDriverAgent which element is active, since the tree says every field is unfocused; and value, the whole of it where `text` is a part, with a field showing its placeholder holding "". Each timed out, saying what it saw, before it succeeded. A checked state on a text field and a password's value are refused at once, and a refusal's `details.check` is read from a real one (CHALLENGES 158) |
| [network.sh](network.sh) | **Network conditions**, judged by the traffic: on an emulator, latency by ping and download and upload by timing 2MB and 1MB against servers the script runs on the Mac, each before, shaped and after; offline by a connection that must fail; the download limit airplane mode removes, put back on coming online; and a session ended offline and shaped leaving the device online and unshaped. On a phone, offline and back, and shaping refused. The emulator console's own `network speed`/`delay` read back as set and change nothing — see CHALLENGES, findings |
| [test-runner.sh](test-runner.sh) | **`mobium test`**, held to its controls: a project whose device is unset refused before any test runs; the suite in `tests/mobiumapp` passing on every project, the projects at once and faster than one after another; every test in `tests/controls/must-fail.test.json` failing with its step, code and a screenshot, exit 1, and the JUnit file read by Python's XML parser agreeing; `--last-failed` running exactly those; and the flaky control failing with no retry and reported flaky with one. Iteration 2: the soft control reporting both soft failures and still reaching its last step, a retain-on-failure trace kept for a failed test and not a passing one, with its recording in Vibium's record format, and `--debug` stopping before each step and quitting. The suite's login test uses `each`; `--ui` is `test-ui.sh`'s. Emulators and simulators (the flaky control clears app data). [the test runner guide](../guides/test-runner.md) |
| [test-ui.sh](test-ui.sh) | **`mobium test --ui`, in a browser**, driven through Vibium as a person drives it: the suite listed on every project, ▶ on one test showing its steps while it runs and every step's screen loaded, a test file changed on disk under the open page running as changed and failing on every project, and "Re-run failed" running exactly those once the file is put back. Emulators and simulators; needs Vibium ([the test runner guide, --ui](../guides/test-runner.md#10-a-page-to-run-tests-from---ui)) |
| [test-grid.sh](test-grid.sh) | **`mobium test` through a grid** (`MOBIUM_GRID=<node>`): two projects asking only for a platform lease two different devices and run at once, every result naming its device, both leases held mid-run and none after; and a third project the grid cannot serve refuses the run with exit 3 before any test, releasing what the others took. Needs a node with two Android emulators. [the grid guide](../guides/grid.md#4-tests-on-a-grid) |
| [source.sh](source.sh) | **The raw source** on MobiumApp: the native hierarchy well-formed and in the platform's units — px on Android, pt with a scale on iOS — with a typed password nowhere in it; then a WebView's markup with a password planted in the page's own markup, hidden in the answer and still in the page. The native redaction's positive control is `internal/uitree`'s test on a captured Aegis screen |
| [clear-data.sh](clear-data.sh) | **Clearing an app's data**, with MobiumApp's Storage Demo as the witness: a count raised and shown to survive a relaunch, so that 0 afterwards means something; cleared; and 0 and no file read back from the app itself. On Android, a location grant read back as granted and then as revoked. An app that is not installed is refused. A real iPhone, which cannot clear in place, refuses without a bundle and names `--bundle`; with `MOBIUMAPP_BUNDLE` set, the reset itself — uninstalled and installed again from the bundle — is held to the same witness, its unreadable permissions must be listed apart, and a bundle for another app is refused with MobiumApp left installed |
| [keyboard.sh](keyboard.sh) | **The soft keyboard** on MobiumApp's Login screen, on any of the four devices: up once a field has focus, an empty field read as empty rather than as its placeholder, two appends kept (Android replaces a field on every keystroke, so this is the check that it reads and writes back), delete, a password typed and printed nowhere, hiding confirmed — and on an iPhone, the refusal to hide naming enter, and enter working. A simulator that earlier typing left believing a hardware keyboard is attached — its keyboard held below the screen — is rebooted once first, saying so ([CHALLENGES 198](../CHALLENGES.md)) |
| [clients.sh](clients.sh) | **All five clients driven**, not only compiled: Python, JavaScript, Go, Java and .NET each run one flow through their own API against a booted Android device, from a test that sits beside the client's unit tests and skips itself unless `MOBIUM_E2E_DEVICE` is set. A client whose toolchain is missing is reported as skipped, never as passed |
| [crashes.sh](crashes.sh) | **Device logs, crash reports and ANRs** — the ANR caused on purpose where adb can become root, by freezing MobiumApp and touching it, and checked through its dialog, its line in the app-filtered log and its report. **Device logs and crash reports**, on an Android device, an iOS simulator or a real iPhone, each against a positive control: a line logged between two reads is the next read's, a burst past the limit is counted rather than hidden, and a crash caused on purpose — `am crash` on Android, an injected `abort()` on a simulator, MobiumApp's Crash Demo on a phone, and on Android too when MobiumApp is installed — appears scoped to its app, reads in full with its cause, and is listed again, since a crash is a record and not a stream |
| [shake.sh](shake.sh) | **Shake**, each platform against a detector, before and after: on a simulator iOS's own Undo Typing after typing, and none with nothing typed; on an emulator a page in Chrome listening to `devicemotion` (so it needs the network), still before and quiet after; a real phone refused, saying why |
| [graybox.sh](graybox.sh) | **Gray box**, held to what MobiumApp's Busy Demo says about the row tapped: launched normally, a tap right after a quiet refresh must land on a stale row at least once — or the run shows nothing — and after a refresh with a spinner on a current one; launched with `--gray-box`, every tap must be current and say what it waited for. On a simulator or an emulator, an app without the library must launch and say it did not answer. iOS and Android, a phone included ([gray box](../guides/graybox.md)) |
| [graybox-hooks.sh](graybox-hooks.sh) | **Gray-box hooks**, held to what MobiumApp then shows: `screen` answers the screen showing; `raiseToast`'s toast says what was sent, ASCII and not; `signIn` reaches the welcome screen without the form; an unknown name is refused naming the registered ones; a hook after an ordinary launch is refused naming the launch ([hooks](../guides/graybox.md#hooks-calling-into-the-app)) |
| [graybox-edges.sh](graybox-edges.sh) | **The gray box at its edges**, on the Busy Demo's edge controls: a refresh started from an alert still waited out; two at once; work that never ends refused after 10 s as check `idle`, its lease kept by the app's still lines; Home mid-work and a crash mid-work, each not waited on and said so — on a phone with `ALLOW_PHONE=1`, since they leave the app; and a killed log stream, said and then heard again — on Android and a simulator by stopping its process, on a real iPhone by the daemon's `SIGUSR1`, mid-work, with the work begun inside the gap and still waited out ([gray box](../guides/graybox.md#when-the-app-is-not-waited-for)) |
| [hit-test.sh](hit-test.sh) | **The hit test below accessibility**, held to what a touch really reached: for each case on MobiumApp's Obstruction Demo, `hit-test`'s verdict, then a touch at that same point by coordinates, and the app's word for what it reached — all seven must agree, the overlay hidden from accessibility named as hidden. Then, on a simulator, the same seven with the probe loaded at launch (`launch --hit-test`): a plain `tap` must tap where the touch reaches the target and refuse, touching nothing, everywhere else. On a simulator and a phone (about nine seconds a case there, where `--hit-test` is refused, saying why); an emulator is refused too ([the hit test](../guides/autowait.md#what-it-does-not-see)) |
| [biometric.sh](biometric.sh) | **Biometrics**, held to MobiumApp's Biometrics Demo, which says what the platform told it: enrollment reaching the running app, a match refused with nothing enrolled and with no prompt up, the enrolled face or finger signing in, and a stranger not recognized. On a Face ID simulator a second stranger to Not Recognized is refused and Cancel reaches the app; on a Touch ID one the third closes the prompt as `authentication_failed`; on an emulator — enrolled through Settings, with PIN 1111 — strangers lock the sensor out, the app hears `lockout`, and the finger signs in once it ends. The enrollment is put back. A real phone is refused, status included |
| [zoom.sh](zoom.sh) | Pinch-to-zoom, the first two-finger gesture — and the only thing that can confirm one. Neither platform reports a zoom level in the hierarchy, so it pinches a WebView and asks the page for its own `visualViewport.scale` |
| [mobium-app.sh](mobium-app.sh) | A genuine hybrid app on **both** platforms: an embedded WebView that opts into inspection, two live contexts in one app, a password field, **a GPX route watched from inside the app**, and **an interruption**: a real system permission dialog, and whether the app still holds what was typed before it. One script, backend chosen from the serial. Evidence for why the app under test is our own, [MobiumApp](https://github.com/mobiumdev/mobium-app) |

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

**The checks that existed by 2026-09-27 and can run on a phone ran on the
iPhone 15 Plus that day**,
with MobiumApp rebuilt from `mobiumdev/mobium-app` and signed for the phone.
All passed: `login.sh` (2m45s), `keyboard.sh`, `autowait.sh`,
`obstruction.sh`, `web-actionability.sh`, `web-type.sh`, `source.sh`,
`crashes.sh`, `ios-device.sh` (1m53s), `gestures.sh` (4m55s), `zoom.sh`,
`mobium-app.sh` (4m06s, with `MOBIUM_NETWORK_TESTS=1`: the link now lands on
`github.com/mobiumdev`) and `third-party-app.sh`. `accessibility.sh`,
`record.sh` and `clear-data.sh` asserted the phone's refusals until the phone
got a route of its own: through Settings (2026-09-28), WebDriverAgent's
screen stream and a reinstall from the bundle (both 2026-09-30). Two
checks needed changing for a phone, and neither change was to Mobium:
`login.sh` met iOS's "Save Password?" sheet over the welcome screen, and now
answers it; and `autowait.sh` refused a phone outright, and now checks the
half of Reduce Motion the phone is set to — **three of its rows read NOT
CHECKED** on this phone, where Reduce Motion is on: the plain slide, and both
confetti rows, since the app draws no confetti then. And `dialogs.sh`, which
refused a phone, now runs there with `MOBIUMAPP_BUNDLE`, reinstalling the app
as the only permission reset a phone has: every section passed — the app's
alerts, the action and share sheets, camera, location and App Tracking
Transparency prompts, rules and refusals — except paste, **NOT CHECKED**
because a phone's clipboard cannot be seeded from outside.

**Every check that ran on Android by then passed on the Pixel 8 Pro, Android
17, on 2026-09-27**, with MobiumApp rebuilt from `mobiumdev/mobium-app`: the five in
the table, and `login.sh`, `keyboard.sh`, `autowait.sh`, `obstruction.sh`,
`web-actionability.sh`, `web-type.sh`, `source.sh`, `crashes.sh`,
`accessibility.sh`, `clear-data.sh`, `dialogs.sh`, `gestures.sh`, `zoom.sh`,
`mobium-app.sh` (with `MOBIUM_NETWORK_TESTS=1`) and `clients.sh`, all five
clients — the Java one timed out once waiting for Settings' first screen and
passed on the rerun, cause not found. A phone is somebody's, and four checks
had to learn that: `dialogs.sh` and `mobium-app.sh` reset permissions with
Android's reset, which is **device-wide**, and now reset MobiumApp's alone,
which `reset-permissions <app>` does on Android since 2026-09-28; `device-state.sh` locked a phone
that has a PIN, which cannot be unlocked from outside, and now asks first
(`cmd lock_settings verify`, answered without locking) and says the lock
round-trip is **NOT CHECKED**; and `autowait.sh` restores an animation scale
that was never set by deleting it. `third-party-app.sh` reads Wikipedia's
article natively on a phone, since a user build publishes a WebView only if
the app opts in and the F-Droid build does not — no devtools socket at all,
measured — and follows a link through the app's preview sheet. `crashes.sh`
skips the ANR, which needs a root adb a retail phone does not have, and says
so.

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
author's GitHub profile, and followed to `github.com/mobiumdev` on the iPhone
15 Plus and the Pixel 8 Pro on 2026-09-27, after the move. The title is matched on the **handle** rather than the
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
