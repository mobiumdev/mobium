# Roadmap

What is planned, roughly in order. Nothing here is a promise of a date.
Everything in the [README](../README.md) is done and verified on a device;
this is what is not.

## Next

- ~~**Every check, swept.**~~ Done 2026-10-06: 28 on the iPhone 17 Pro
  simulator, 31 on the Android 15 emulator, and the nine that stay inside
  their apps on the iPhone 15 Plus, each with a fresh daemon — and then
  twelve more in MobiumApp on the phone, where `trace.sh` alone failed: a
  trace's first frame starved by two slow settle reads (CHALLENGES 270),
  fixed. Then, with the owner's leave, the phone checks that open
  Settings, Calendar or Safari or turn the device: `back`, `audit`,
  `locale-ios`, `timezone-ios` and `orientation` passed, and the phone was
  in portrait after; `clear-data` checked its refusal and said the reset
  needs MobiumApp's .app; `ios-webview.sh` skipped to a pass and now
  refuses a phone (269); `ios-device.sh` stopped at contexts with the phone
  off its cable — lockdown needs USB, and the error said so — and passed,
  all ten steps, once it was plugged in. `record.sh` was not run: on a phone it records
  Settings. Two
  regressions had sat red unnoticed: a back button "covered" by the list
  under its bar (CHALLENGES 263, from 257) and Flutter's password field
  going stale (265, from 240), both fixed. So were a menu whose list ran past
  the panel that clips it (262), a ref chased into a paging carousel and
  tapped off the screen (261, 267), a web tap that blamed Chrome's web apps
  for a dialog (268), and once, under load, bold text left on after a
  restore (266). Four checks were made sturdier: `files.sh` (an emulator
  taken for an iPhone; MediaStore's numbered saves), `icecubes-ios.sh` (a
  post taller than the screen), `pocketcasts-ios.sh` (a search left open).
  Two dark-screen sightings on the iPhone were explained (260). Since, with
  the owner's leave, every check written for a phone has passed on it:
  `autowait.sh` once with Reduce Motion on and once with it off (the
  switch moved by hand — a phone's cannot be from outside), `record.sh`,
  and `mobium-app.sh` from a reinstall (`MOBIUMAPP_BUNDLE`), so its
  permission prompt was a fresh one.
  The two left red on the simulator were setup, not code:
  `notifications-ios.sh` passes once MobiumApp is allowed to notify (Settings
  > Apps > MobiumApp > Notifications on iOS 26 — a reinstall or a data
  reset asks again, and `dialogs.sh` answers a permission prompt), and
  `third-party-app-ios.sh` is for a real iPhone and now says so on a
  simulator (CHALLENGES 269). On the iPhone it passed in 77 seconds once
  it found the featured card the way 237 maps it — Save for later its own
  entry after the card, no longer in the card's label.
- ~~**Gray box: waiting for the app to say it is idle.**~~ Done 2026-10-05, iOS and Android:
  `launch --gray-box` turns on Mobium's gray-box library in an app built
  with it, the app writes when it is busy to the device log, and every
  action waits for it to be idle before finding its target
  ([the gray box guide](guides/graybox.md), `docs/checks/graybox.sh`, on the
  iPhone 15 Plus, the iPhone 17 Pro simulator, the Pixel 8 Pro and the
  Pixel 7 emulator). On MobiumApp's Busy Demo, a tap right after a quiet
  refresh was stale 6 to 10 times in 10 launched normally, and 0 in 10 with
  the gray box, on every one. Hooks since 2026-10-05: `app_hook` calls a
  function the app registered by name and returns what it answered
  (`docs/checks/graybox-hooks.sh`). Next: the library for a native app and
  for Flutter.
- ~~**A test runner — `mobium test`, on the phones.**~~ Done: iterations 1
  and 2 are built and checked (`docs/checks/test-runner.sh`): JSON test files of
  `app_batch` steps, `app_wait_for` and `expect` assertions, projects as
  devices, workers, retries with flaky reported, `-g`, `--last-failed`,
  list, JSON, JUnit and HTML reports, `show-report` — and since 2026-09-29
  the step shorthand, soft assertions, a trace per test and `--debug`.
  On 2026-09-29 the suite passed seven of seven on the iPhone 15 Plus and
  on the Pixel 8 Pro, and on both the must-fail and soft controls failed as
  they must. The flaky control clears app data, which a phone cannot, so it
  stays on emulators and simulators. Getting there found CHALLENGES
  170–172. On an iPhone a password still needs a keyboard with its letters
  up (159). Parameters since 2026-10-01: `"each"` runs a test once per case,
  `${key}` filled in (the test runner guide, section 9). An interactive mode
  since 2026-10-01: `mobium test --ui` serves a page that lists the suite,
  runs a test, a file or all of it on its projects, shows each step with
  its screen as it happens, and re-runs what failed, the files read again
  for every run ([the test runner guide, --ui](guides/test-runner.md#10-a-page-to-run-tests-from---ui),
  `docs/checks/test-ui.sh`). Recording a test from what a person does is
  `mobium inspect`, since 2026-09-29 ([the inspector guide](guides/inspector.md)).
  The format is in [the test runner guide](guides/test-runner.md).
- ~~**A second third-party app on iOS.**~~ Done 2026-10-01: NetNewsWire, an
  RSS reader, built from its MIT source for a simulator and from the App
  Store (7.1.4) on the iPhone 15 Plus, so the same app runs on both, as
  Wikipedia cannot. Its first look found seven defects (CHALLENGES 191–197): a link
  matched `role=button`, XCUITest type names and a disclosure arrow's name
  in labels, section headers mapped as buttons, selection never shown, a
  miss blamed on the keyboard, and a row behind iOS 26's toolbar tapped
  through the toolbar. And one missing action: a list row's swipe actions
  were reachable only by coordinates, so `app_swipe` takes a `target` now
  (`mobium swipe @e5 left`), swiping part of the way across it to reveal
  them; eight of eight swipes revealed and none performed. Held by
  `docs/checks/netnewswire-ios.sh`, which passed on the simulator and,
  from a fresh install and again after, on the iPhone. A row behind the
  toolbar (197) is held on both by the lowest article row on screen: on
  the iPhone its center was at y 2553 pixels, under the toolbar from 2538,
  and `main` tapped it there and opened nothing, while this scrolls it out
  and the article opens.
- ~~**A third third-party app on iOS, the first in SwiftUI.**~~ Done
  2026-10-04: Ice Cubes, a Mastodon client (AGPL), browsed signed out on a
  server's public timeline, so it needs no account — built from its source
  at `9efcb16~1` for a simulator (its main branch needs the iOS 27 SDK) and
  from the App Store on the iPhone 15 Plus. Its first look
  found five defects (CHALLENGES 213–217): a timeline picker left out of
  `map` by the section-header rule, a post's Reply, Boost, Favorite and
  Share buttons left out because SwiftUI combines a row for VoiceOver,
  `label=` finding a control's wrapper, icon or title as well as the
  control, iOS 26's selected-tab pill reported as a cover, and a tap
  behind the app's own image viewer or sheet reported done. Held by
  `docs/checks/icecubes-ios.sh`, which presses nothing that posts: Share
  and the image viewer are opened and closed. It passed on the simulator
  and, from a fresh install and again after, on the iPhone.
- ~~**A tap under an app's own full-screen view is reported done.**~~ Done
  2026-10-04 (CHALLENGES 217): a target reported not visible, inside a
  screen-sized plain view reported not visible, is behind another screen of
  the app and is refused; measured against every captured hierarchy, with
  the obstruction screens as the control. Held by `icecubes-ios.sh`.
- ~~**A fourth third-party app on iOS: Pocket Casts.**~~ Done 2026-10-06,
  on the simulator and the iPhone. Begun 2026-10-05: a
  podcast player (MPL-2.0), built from its source at `72b785001~1` for a
  simulator — its main branch needs Xcode 27, and its scheme the watchOS
  simulator runtime, for the Watch app it embeds — and from the App Store
  on the iPhone 15 Plus, browsed signed out on both, Discover loading with
  empty credentials. Its first look found a popover
  that hid the whole player from `map` and `app_alert` (CHALLENGES 235) and
  a scrubber that printed as its own position and that `app_fill` would
  have typed into (234), a checkmark mapped as a target of its own under
  its image's name, `discover_tick` (236), rows named after the buttons
  and images inside them (237), and a divider through Play's center noted
  as possibly taking the touch (238), and a disabled button mapped as an
  ordinary one (239), all fixed; writing its check found two more, a
  search field typed into through the keyboard's Search key (240) and a
  filter chip scrolled for because it is taller than its row (241). A mini
  player docked over every list is tapped around, as CHALLENGES 115 does.
  Held by `docs/checks/pocketcasts-ios.sh`, which passes on the iPhone 17
  Pro simulator, an iPad Air simulator, where it found an ambiguous
  locator's remedy naming a role both matches shared (242), and the iPhone
  15 Plus, where onboarding and the first-run tip are skipped unless the
  app is freshly installed. This build's search works: on the evening of
  2026-10-06 six searches from fresh launches all answered in 5 to 7
  seconds, and Serial, Planet Money and Hardcore History each came back
  first among rows that named them. The "Search Failed" of that afternoon
  was passing, not the build's empty credentials, as CHALLENGES 257 first
  said; what looked like the same unrelated podcasts for every query was
  `map` listing Discover under the search screen.
- ~~**A fifth third-party app on iOS, the first whose WebView opens: Kiwix.**~~
  Done 2026-10-06, on the simulator and the iPhone. Begun 2026-10-05: the offline Wikipedia reader (GPL-3.0), SwiftUI around
  a WKWebView that a build from its source makes inspectable (a production
  build does not). Built for a simulator from its main branch with Xcode
  26.6 and its prebuilt libkiwix; a ZIM — Wikipedia's Ray Charles, 761 KB —
  uploaded to its Documents and opened through the system document picker,
  since its in-app download failed with "unknown error". Its first look
  found five defects (CHALLENGES 244–248): combined cards mapped as seven
  buttons each, an off-screen ambiguity sent to `map` for a ref, a web tile
  mapped twice and named twice, a tap on a disabled button reported done
  through its wrapper, and every tap into the page refused because the
  WebView runs under the bars — fixed by anchoring the page on text both
  sides report, which makes Safari's page tappable too, on a simulator and
  on the iPhone. Held by `docs/checks/kiwix-ios.sh` on the simulator and,
  since 2026-10-06, on the iPhone, where its first run found a page under
  the Library mapped link by link (CHALLENGES 256). The App Store build on the iPhone, meant as the control whose WebView is
  closed, was not one: its page is published, mapped and tapped like the
  simulator's — a tap on Hank Crawford opened Hank Crawford — so a
  production build of Kiwix is reachable as shipped. Its ZIM came through
  its own download: the phone refuses an upload into an App Store app's
  container, which now says so (CHALLENGES 249).
- **A tap in an emulator's Chrome web app after Chrome stops reporting its
  WebView.** Without Play services Chrome installs a web app as a launcher
  shortcut, and opened while Chrome is running its WebView leaves the
  accessibility tree within about five seconds; Mobium refuses taps in it
  with the reason (CHALLENGES 200). A WebAPK on the Pixel 8 Pro kept its
  WebView and took taps, so this is narrower than it first looked. What
  could place one is unmeasured: CDP can say where the page's viewport is in
  Chrome's own coordinates, and whether those can be tied to the screen
  without a native host is the question. Seen once on 2026-10-01 and not
  reproduced: the emulator and the booted simulators went away mid-session
  with nothing in Mobium stopping them and nothing in their logs saying
  why. The hang switching into a background tab is CHALLENGES 203, fixed.
- ~~**Back, on both platforms ([BACK.md](BACK.md)).**~~ Done 2026-10-05,
  the last two questions answered on the iPhone. Measured 2026-10-01 on
  the Pixel 8 Pro, both AVDs, the simulator and the iPhone: Mobium's back key
  and its edge swipes reach the platform's back on every one, and on iOS 26 a
  swipe from anywhere pops a navigation stack unless a row with swipe actions
  takes it. MobiumApp closed on every Android back; a `BackHandler` fixes it
  on all three Android devices (mobium-app #12). Done the same day for Mobium: `press back` says what back did,
  `--gesture` swipes back and is refused with button navigation, `devices`
  names the mode, the iOS refusal names the gesture, and
  `docs/checks/back.sh` holds it on both phones, both AVDs and the simulator.
  Predictive back measured 2026-10-02: with it turned on, React Native 0.86
  keeps `BackHandler` on Android 16 and later and loses it on 15 and
  earlier, where every back closed MobiumApp — and Mobium's `press back`
  said so. Measured on the iPhone on 2026-10-05, the two left open:
  MobiumApp has no navigation stack on iOS, so the edge swipe moves nothing
  and Mobium says the app is still in front — a feature the app lacks, not a
  gap in Mobium — and its WebView screens load their pages as HTML into a
  blank page, with no history to go back through: the swipe changed nothing
  and said so, and the app's own Back button returns.
- **App Clips: a clip of our own on a real iPhone.** Measured 2026-10-01:
  on the simulator a clip installed alone launches, reads, takes taps and
  goes back like any native app; on the iPhone a published clip opened from
  its default link put up its App Clip card, which `alert` reads and whose
  `OpenButton` opened the clip, then driven the same way ([APP-TYPES, "Try
  before you install"](APP-TYPES.md#try-before-you-install)). Open: our own
  clip on a phone and Local Experiences, which need a paid Apple Developer
  Program team. Measured on 2026-10-05: on the card `alert accept` presses
  its last button, "View on the App Store", and opens the clip's App Store
  page, and `alert dismiss` presses Close — neither opens the clip, which
  only a tap of `OpenButton` does.
- ~~**Android's autofill save dialog is not an alert.**~~ Done 2026-10-02:
  the system's offer to save a password — `android:id/autofill_save`, a sheet
  over the lower half of a window of package `android` that fills the screen,
  so not a floating one — is a dialog to `app_alert` and `app_dialogs` now,
  known by the platform's own id for it. On the Pixel 8 Pro `alert` reads it,
  refuses accept and dismiss saying the endpoint cannot press its buttons, and
  a rule `--when "Save password" --press "Close"` closed it in the way of a
  tap and the tap went on. Google Password Manager's "Not now" sheet is the
  same dialog: on 2026-10-02 MobiumApp under a package name the phone had
  never seen got it on its first login, `android:id/autofill_save` with Not
  now where the Never button is, and every later login of a package gets
  Never (six of six). `alert` read it, refused accept and dismiss, and a rule
  `--when "Save password" --press "Not now"` answered it in the way of a tap.
- ~~**Reading without silencing a screen reader, on UiAutomator2 too.**~~
  Done 2026-10-05: the dump backend reads through a reader of Mobium's own,
  and the UiAutomator2 server is now started with
  `DISABLE_SUPPRESS_ACCESSIBILITY_SERVICES` — an instrumentation argument,
  where the setting of almost the same name was accepted and ignored. TalkBack
  stays bound with touch exploration on through a session, and taps, scrolls
  and typing land, on the Android 17 emulator and a Pixel 8 Pro
  (`docs/checks/screen-reader.sh`, CHALLENGES 208). VoiceView on the Fire TV
  is measured for the dump backend only.
- **Windows.** Everything that needs no device passes on a GitHub-hosted
  Windows runner, every run: both modules' tests, the named-pipe daemon
  transport's acceptance tests five times over, and the built `mobium.exe` —
  `doctor`, `mcp`, and a daemon auto-started, read back and stopped. Getting
  there found four defects (CHALLENGES 139–142). No device has been driven
  from Windows, and until one has, Windows is unsupported.
  [WINDOWS.md](WINDOWS.md) is the state of it.
- **Published client packages.** Every client builds, as its registry would
  receive it, into a package that carries the LICENSE, a README and full
  metadata, and each has been installed from that package into a clean project
  and run on an Android emulator and an iOS simulator — the Java one from both
  Maven and Gradle. What remains is the release itself: accounts and tokens on
  PyPI, npm, NuGet and the Maven Central Portal (the `dev.mobium` namespace is
  verified by a TXT record on `mobium.dev`), a GPG key for Maven Central, and a
  tag — `v0.1.0`, and `clients/go/v0.1.0` for the Go module. All four names
  were unclaimed on 2026-09-27.
- **Prebuilt `mobium` binaries — built, waiting on the first tag.**
  `make dist` builds all six platforms, static and without build paths, into
  archives with the license, notices and README and a `SHA256SUMS`; the
  release workflow runs it on a `v*` tag, checks every archive and the Linux
  binary's `--version`, and attaches them to the release, and on a pull
  request does all of that but the release. What is left: the tag, which is
  a decision, not a step; a Homebrew tap; and notarizing the macOS binaries,
  which needs an Apple Developer Program membership — until then
  [SETUP.md](SETUP.md#installing-a-release) says to download with `curl`.
- **Video walkthroughs** of the quick start, one per client, once the clients'
  `start` and `quit` (the CLI's `session start` and `session end`) have
  settled. The pages' examples and captured output are the script.
- **Auto-wait, the rest of the actionability checks.** Actions already wait for
  a target to exist, be in view, stop moving and be enabled; refuse one under
  a dialog or the keyboard; and aim around, wait out or refuse a control the
  app drew over it ([CHALLENGES 115](CHALLENGES.md)). Every refusal from those
  checks has one shape, native and web — "X failed check C: reason", C in
  the error's details (CHALLENGES 158) — and `wait` also waits for checked,
  unchecked, focused and a value (`docs/checks/wait-states.sh`). Still to
  come:
  - `map` lists a screen an app laid over another as if nothing covered
    it: Discover's chips under Pocket Casts' search results. On the iPhone
    the tree gives no sign; taps into it are refused since CHALLENGES 257,
    where a control's container, not its frame, was measured to be what
    takes a touch.
  - A screen with a long list (CHALLENGES 258), read since 2026-10-06
    without `visible` and with what is shown worked out from geometry.
    Three things are left: the first read of such a screen does not know
    it is one, and pays the 60-second timeout before falling back (91
    seconds for Radiolab's page, four after); `map` there may list an
    element something covers; and an action there does not scroll for its
    target. Measured on 2026-10-06 across apps with nothing personal in
    them, on the iPhone 15 Plus — elements, cells, read without and with
    `visible`:

    | Screen | Elements | Cells | Light | Full |
    |---|---|---|---|---|
    | App Store, Top Free Apps | 195 | 9 | 0.46 s | 1.49 s |
    | App Store, Apps tab | 198 | 13 | 0.46 s | 1.64 s |
    | Pocket Casts, Hardcore History | 204 | 14 | 0.50 s | 2.88 s |
    | Pocket Casts, This American Life | 215 | 16 | 0.49 s | 3.24 s |
    | Settings, Keyboards | 263 | 22 | 0.52 s | 2.51 s |
    | Wikipedia, search results | 264 | 13 | 0.50 s | 2.70 s |
    | Pocket Casts, The Daily | 413 | 65 | 0.93 s | 9.57 s |
    | Settings, Add New Keyboard | 597 | 174 | 0.97 s | 7.18 s |
    | Pocket Casts, Radiolab | 2,847 | 674 | 3.3–6.3 s | 25.5–83.8 s |
    | Pocket Casts, 99% Invisible | 3,495 | 836 | 7.69 s | 104.48 s |

    Only a table that hands accessibility every row grows past the
    threshold — Pocket Casts' episode lists. The App Store and Wikipedia
    build rows as they come into view and stay small; Settings' longest
    list here is 597 elements and reads in seven seconds. A full read cost
    15 to 30 ms an element on the large screens, so 1,000 fits the 60
    seconds with room, and nothing measured fell between 597 and 2,847.
    A read without `visible` costs 15 to 30% of a full one even on small
    screens, so reading light first everywhere to spare the first read of
    a long list would tax every read for a case met in one app so far; left
    as it is. Not measured: a mailbox or a contacts list, which are someone's.
  - An overlay hidden from accessibility on iOS, which WebDriverAgent's tree
    does not contain, so a tap under one still lands on it. **On a
    simulator, `mobium hit-test` sees it** since 2026-09-29: UIKit's own hit
    test, asked through lldb, opt-in because the attach stops the app for
    about two seconds ([the hit test](guides/autowait.md#what-it-does-not-see)).
    On a real iPhone since 2026-09-30: the probe is evaluated as an lldb
    expression, through `xcrun lldb`'s own Python, and all seven cases of
    the Obstruction Demo agreed with where a raw touch went on the iPhone
    15 Plus, about nine seconds a case. It needs the app built for development. **Before every
    action, on a simulator**, since 2026-09-30: `launch --hit-test` loads
    the probe as the app starts and it answers on a Unix socket on the
    Mac's own disk in under a millisecond, so every action on an element
    asks it first; the seven cases agreed with a raw touch, and a tap cost
    the same with it as without. Not
    on a phone, where Mobium could reach a probe only over the phone's
    network; it refuses, and `hit-test` there stays the debugger's.
    XCTest's own `hittable` is no shortcut, measured on 2026-10-06 on the
    simulator's Obstruction Demo: it called the target under the hidden
    overlay hittable, where a touch lands on the overlay, and the
    pass-through target not, where a touch reaches it — accessibility's
    answer, as `visible` is. What sees the overlay is UIKit's hit test,
    inside the app. Decided on 2026-10-07 to leave it: asked through the
    gray-box channel, one question took 1.8 to 1.9 seconds on the iPhone 15
    Plus — a hook that does nothing, five times — against a whole tap at
    about one, so asking before every action would triple it; a listener of
    the app's own over the network tunnel would be fast and was declined, as
    for the simulator's probe. The auto-wait guide says plainly that a real
    iPhone's actions are not protected.
- ~~**Session recording and `diff map`.**~~ Done 2026-09-29. `mobium trace
  start|stop` records a session as Vibium does: a zip in Vibium's record
  format, with every call a step, the screen after it and the map drawn
  over it (`docs/checks/trace.sh`). `mobium map --diff` answers what
  changed since the last map (`docs/checks/map-diff.sh`). Since
  2026-10-01 `mobium test --trace` writes the same zip for each test it
  keeps, beside the filmstrip, and the HTML report links it; the runner's
  own screenshots and maps are sent with MCP's `_meta` marking them
  untraced, so the zip holds the test's calls alone (`test-runner.sh`).
  And since 2026-10-01 it plays in Vibium's player, player.vibium.dev,
  checked on a simulator and an emulator by `trace.sh` with
  `MOBIUM_TRACE_VIEWER=1`. Getting there: the player names a fill by the
  record format's `selector` and `value`, which Mobium did not send, so
  it read `Type "" into field` — they are recorded now, the value masked a
  dot a character; and the player shows at each step the frame kept after
  the one before, which after a launch was blank — an action's frame is now
  taken once the screen has settled, as `mobium test` reads its own.
  Next, if wanted: a batch's steps as a group in it.
- ~~**Screen recording on a real iPhone.**~~ Done 2026-09-30. `record`
  reads WebDriverAgent's MJPEG stream on the phone's port 9100 over the
  tunnel and writes the frames into an MP4 in Go — JPEG samples, each
  lasting until the next arrived, so the video carries the rate achieved:
  9.7 frames a second against the stream's default of 10, at the full
  1290x2796. AVFoundation opens and decodes it, and MobiumApp's Motion
  Demo recorded with its confetti falling. Nothing is written on the phone
  (`docs/checks/record.sh`). Later, if wanted: a higher rate through
  WebDriverAgent's `mjpegServerFramerate`, measured rather than assumed.
- ~~**Keep Mobium's runner on a phone that other tools also drive.**~~ Done
  2026-09-30. The phone build is named `MobiumWDA-Runner`, out of the sweep
  other tools make for `WebDriverAgentRunner-Runner`, and a session that finds
  its runner gone says so before installing it again (CHALLENGES 189). The
  simulator runner is still the prebuilt one under WebDriverAgent's name;
  there mobium already notices a runner that is not its own (181).
- ~~**Touch targets on iOS.**~~ Done 2026-09-30. Judged in points, through
  the scale WebDriverAgent reports: the Layout Demo's 24pt target is named on
  a simulator and on the iPhone, and its 50pt and 54pt bar is not
  (FORMFLUX.md, "iOS touch targets").
- ~~**No `--driver` for a device Mobium has identified.**~~ Done 2026-09-30.
  A device named with no driver named goes to `wda` when it is an iPhone or a
  simulator; a `--driver` that contradicts the device is still refused
  (CHALLENGES 88). And since 2026-10-01 a call that names no device and no
  driver, with no Android device connected, goes to the iOS device when
  there is exactly one, and with several is refused naming them; Android
  stays the default whenever one is connected. Measured with the iPhone 15
  Plus alone, and with it and a simulator together.
- ~~**The cost of an action on iOS.**~~ Done 2026-09-30, by its measure: a
  tap on the Layout Demo, timed from the CLI, went from a median of 600 ms
  to 524 ms on a simulator and from 961 ms to 807 ms on the iPhone, and
  every refusal in `obstruction.sh` and `autowait.sh` is unchanged on both
  — on the iPhone with Reduce Motion off for the run, so the moving cases
  ran. A tap was two full reads, a 100 ms window between them and the
  touch. The window now counts the read that came before it, which on iOS
  already spans it, and the second read is the one element's rectangle by
  its test id when that id is unique on screen, taken only on an exact
  match. The touch itself is about 400 ms on the phone whichever way it is
  sent, and WebDriverAgent's idle settings barely move it.
- **Reading an iOS screen without `visible`.** On the iPhone, Settings'
  hierarchy took 1.81 s to read and 0.29 s without the `visible`
  attribute: WebDriverAgent works it out for every element, and it is 85%
  of a read. Leaving out the attributes Mobium does not parse changed
  nothing. `visible` is what keeps hidden elements out of `map` and out of a
  locator's matches, and the dialog refusals use it (CHALLENGES 105), so it
  cannot simply go. Built on 2026-09-30 for actions: a read without it takes
  every element as shown, which can only add matches and covers, so an
  action uses it when it decides cleanly — no dialog or keyboard, one
  enabled match inside its container, nothing drawn over it — and the one
  element, asked alone, is visible; anything else reads in full. Measured
  first, by replaying Mobium's own decisions on 20 captured simulator
  screens with and without the attribute: 17 decided everything the same,
  and Safari and SpringBoard only added matches and covers. It is tried
  only where a session's full reads are slow, 250 ms or more
  (`MOBIUM_LIGHT_READ_MS`, 0 for every action): on a small screen a light
  read and the check cost more than the full read. On a simulator a tap of
  General in Settings went from 1.7 s to 1.5 s, most of what is left being
  WebDriverAgent waiting out the navigation, and every refusal in
  obstruction, autowait, dialogs, keyboard, login and wait-states held with
  it forced on. On the iPhone, tapping General in Settings went from 3.5 s
  to 2.2 s and tapping back from 4.6 s to 2.1 s, and obstruction and
  autowait (forced) and ios-device.sh pass. `map` and `text` still read in
  full, and on 2026-09-30 that was measured to be the cheapest correct way
  for them: WebDriverAgent has no cheaper way to ask about visibility.
  Safari's hidden elements are not one hidden tab that a single check could
  rule out but eleven small subtrees, one of them a hidden window with
  visible children. On the iPhone, with Safari in front, the full read took
  779 ms and the light one 160 ms, while a query for `visible == 0` took
  7.7 s and one element's rectangle 0.5 s; the same query on Settings ran
  past 60 s. A JSON source computes `isVisible` and costs what the XML does
  (1.77 s to 1.83 s on Settings). So a `map` that patched a light read
  would cost more than it saved on any screen with a hidden element, and
  one that skipped the patch would hand out refs to things not shown.
  What is left of a tap was measured on 2026-10-06 on the iPhone, in
  MobiumApp (CHALLENGES 271): a lookup of every device before each call,
  375ms, gone; then the light read, finding the element, its visibility
  and rectangle, about 560ms together, and the touch, 430 to 460ms, which
  no WebDriverAgent wait setting and no other way of tapping shortens.
  Visibility is asked in the find since CHALLENGES 272, one request fewer;
  the rectangle is the stillness check's second sample and stays apart.
  A tap that moves nothing is 0.96 seconds on the iPhone now. Android has
  no per-call lookup to remove (28 to 32ms for a call that asks nothing). Found on the
  way: a locator's resolution (`pickOne`) does not consult visibility on a
  full read either, so a uniquely labeled element iOS calls hidden is
  acted on unless it is under a dialog or the keyboard, or has no bounds.
  The light path asks the element and, when it is hidden, reads in full
  rather than act on it. Whether the full read should refuse it was
  answered on 2026-09-30 by a survey of seventeen screens on the iPhone: no.
  The hidden elements on screen were all covered ones, the cover rule's to
  answer, and one, the Obstruction Demo's pass-through target, is really
  reached. What the survey found instead was elements below the screen with
  nothing around them that scrolls, tapped there and reported done
  (CHALLENGES 190), and those are now refused.
- ~~**Launching on a real iPhone.**~~ Done 2026-09-30. `app_launch` had a
  median of 5.8 s on the iPhone 15 Plus against 2.3 s on the simulator. A
  launch read the screen before launching, usually the home screen at
  4.5 s, for nothing; without it the median was 2.51 s. The rest was the
  full read that confirmed the app in front and took the active-app hint
  off. A read without `visible` answers that in a fraction of the time, but
  took the hint off mid-switch, while iOS still reported the app being left
  in front, and the next read then hung for a minute, once in fifty. The
  hint now waits for the app being left to leave the front (CHALLENGES 71):
  a hundred launches of MobiumApp, Settings and Calendar, from the home
  screen and over another app, over fresh sessions, had a median of 0.89 s,
  the slowest 1.05 s, no hang and the right app in front every time, and
  `ios-device.sh` passes.
- ~~**A reset a phone can do.**~~ Done 2026-09-30. `app_clear_data` takes
  the app's own bundle on a real iPhone (`path`; `--bundle` on the CLI),
  checks it is that app, uninstalls it and installs it again, and reads the
  data container back. Measured first: the uninstall resets notifications,
  location and camera, each asked again afterwards; installing over the app
  keeps all three, which is what the 2026-09-28 note had seen. Permissions
  are reported as reset and not read back, since nothing outside the app can
  read them on a phone.
- ~~**The iOS settings still refused as not built.**~~ Done 2026-09-30:
  notifications, the time zone, per-app locale and orientation.
  Notifications are done on a simulator,
  through Notification Center read as SpringBoard: a post goes out with
  `simctl push` as the app in front and is confirmed by its banner, a read
  takes any banner and then Notification Center, opened for it and closed
  again, and the shade, once opened, is what `map` reads
  (`docs/checks/notifications-ios.sh`). A group expanded is read in full;
  collapsed, only its newest shows. A notification tapped in Notification
  Center did not open its app on the simulator, by tap or by element click,
  while Show less in the same view did respond. A real iPhone is refused:
  its Notification Center is its owner's.
  The time zone is the session's launch environment, as the language is:
  iOS has no device time zone that can be set from outside, so
  `app_timezone` keeps one for the session and every app Mobium launches
  gets it as `TZ`; the app in front is launched again in it, and setting
  the device's own zone ends it. Calendar, which marks the hour in
  progress, marked Tokyo's hour once set to Asia/Tokyo, again on a later
  launch, and the device's hour once set back, on a simulator and on the
  iPhone (`docs/checks/timezone-ios.sh`; Calendar has to be in its day
  view, and the check says it did not run when it is not). Driving
  Settings, the other way, would have moved a phone owner's clock.
  Per-app locale is done (2026-09-30): iOS stores no per-app
  language that can be set from outside, so `app_locale` keeps it for the
  session and every launch passes it as `-AppleLanguages (xx) -AppleLocale
  xx_YY`; a running app is launched again in it at once. Settings, pinned to
  ja-JP, read 一般 for General on a simulator and on the iPhone, on the
  pin's relaunch and on a later launch, and General again once cleared
  (`docs/checks/locale-ios.sh`).
  Orientation is done (2026-09-30), through WebDriverAgent's `/rotation`
  rather than `/orientation`, which reads both landscapes as one: an app
  that turns is turned and read back, and one pinned to portrait is refused
  naming where it stayed, on a simulator and on the iPhone
  (`docs/checks/orientation.sh`). iOS has no `auto`, and a Face ID iPhone
  is never upside down; both are refused with the reason.
- ~~**Auto-advancing code boxes on iOS.**~~ Done 2026-09-30. A code typed
  whole into the first box that loses a character as focus moves is typed
  again one character to a box and confirmed (CHALLENGES 156): 30 of 30 on
  a simulator, five of them recovered, and 10 of 10 on the iPhone.
- ~~**A phone restore once left a setting the opposite of what it found.**~~
  A cause found by reading the code on 2026-10-05 (CHALLENGES 243): the value
  a change is undone to was read from a ten-second cache of the last visit
  to Settings, which a switch flipped outside Mobium leaves stale. It is read
  from the switch itself now, and every read, record, change and restore is
  kept in `accessibility.log` in mobium's state directory. Not reproduced —
  a script cannot reach the switch inside the window — so if it recurs, the
  log says which read the restore followed.

## Under consideration

- **Devices served concurrently by one daemon.** Today a call holds the
  daemon's lock for its whole length, so two devices on one daemon go at the
  slower one's pace: an emulator's 15 `map` calls took 22.3s beside a
  simulator's, against 0.3s on a daemon of its own (2026-09-27). The answer
  chosen for now is one daemon per parallel run, which every client can name
  with its `session` option ([SETUP.md](SETUP.md#parallel-runs)); routing
  each call to its device's own lock would remove the wait in one daemon, and
  means reworking how every handler touches shared state.

- ~~**Battery and the device's clock**~~ (written down 2026-09-28) — done:
  `mobium battery` reads the level, the charging state and what it is
  plugged into, and `mobium time` the device's clock in its own zone.
- ~~**Pulling a file off the device**~~ (written down 2026-09-28) — done
  2026-09-29 as `upload` and `download`: the shared Download folder on
  Android, an app's Documents on an iOS simulator, each confirmed at the far
  end (`docs/checks/files.sh`). Since 2026-09-29 also a real iPhone,
  through CoreDevice's file service, which has no delete (CHALLENGES 173).
  The check passes on the iPhone 15 Plus and the Pixel 8 Pro.
- ~~**Files at any path, and folders**~~ (asked for 2026-10-02) — done:
  `upload` and `download --device-path` move a file or a whole folder to a
  path you name — on Android a shell path, or with `--app` a path in that
  app's private data through `run-as`, which Android allows for a debuggable
  build and refuses with the reason for a release one; on iOS a path in the
  app's data container, by simctl on a simulator and CoreDevice on a phone.
  Every file's size is read back at the far end (`docs/checks/device-paths.sh`).
  Measured on the way: `adb shell -T` carries bytes unchanged, and `adb
  exec-out`, given several arguments, escapes each itself — quoting them too
  made run-as look for a package named with its quotes, and its error came
  back as the file's content, caught by the size check. And on a phone
  CoreDevice will not copy an empty folder — "no such file" for one its own
  listing showed — so an empty folder is made, not copied. Passed on all
  four devices.
- ~~**Booting and shutting down emulators and simulators**~~ (written down
  2026-09-28) — done 2026-10-01: `mobium boot <avd | simulator>`
  (`app_boot`) starts one and answers once it has booted and a window has
  focus — Android says boot completed a second or two before the launcher
  is up, and the first call made then failed; `mobium shutdown` (`app_shutdown`)
  ends this daemon's session on it first and waits until it is gone. The
  device is named by `name`, never by `device`, which a client's pipe sets
  to its own: shut down from a call pinned to a simulator, the emulator went
  and the simulator stayed. A phone is refused by both. An agent over MCP,
  which has no shell, can start a device now (`docs/checks/boot.sh`).
- **A live view of any device** (written down 2026-10-07): a page the
  daemon serves, showing the device's screen as it changes, for watching a
  run, a demo, or a phone across the desk. Watching only, or clicks sent
  as `app_tap` — never a second way in, since input that does not go
  through the session is input nothing checks. The picture is half built:
  - **iOS, simulator and phone**: WebDriverAgent's MJPEG stream, which
    `record` already reads — 10 full-size JPEGs a second on the iPhone
    15 Plus. A live view shows the stream instead of writing it to a file.
    **It runs less than a frame behind the screen**, measured on the
    iPhone 15 Plus (iOS 26.6.2) on 2026-10-07: 9.6 frames a second, a
    frame every 105ms and never more than 170ms apart, and on three taps
    that opened MobiumApp screens the first changed frame arrived 40 to
    90ms *before* the tap call returned, about 0.75s after it was sent.
    Read by each frame's arrival and JPEG size, nothing decoded or kept.
  - **Android**: not a loop of `screencap`. Measured on the Pixel 8 Pro
    (Android 17, 1008x2244, over a 480 Mb/s USB link that moved 10 MB/s):
    `screencap -p` took 3.3s a frame, 1.4s of it encoding on the phone,
    and a raw frame 1.2s — under one frame a second. (`UIA2.Screenshot`
    cites 0.13s; that was not this phone, and a phone's screenshot is
    worth re-timing on its own.) `screenrecord --output-format=h264`
    to stdout through `adb exec-out` is a stream instead: first bytes in
    0.7s, about 118 pictures a second while MobiumApp's list scrolled (the
    screen's 120Hz), 1.7 MB/s, and nothing while the screen is still —
    one picture in 12s of an idle launcher. `--time-limit 0` was accepted.
    A browser decodes H.264 itself, so Go would pass the stream through
    without decoding it.
  - **Still to measure before building**: how far behind the screen the
    picture runs on Android and on a simulator; whether the H.264 stream outlasts the
    recorder's limit and survives a rotation; that a stream's process is
    gone from the device when the view closes — a cut-off read once left
    `screenrecord` running on the phone until it was killed by hand.
  - **Not**: a screenshot answered from the stream's latest frame. A
    frame is a JPEG where `screenshot` promises a PNG, and up to a tenth
    of a second old — a screenshot right after a tap could show the
    screen from before it.
- **Audio** (written down 2026-10-07): what an app played, heard and
  checked. **On an Android emulator, done** — `mobium audio start`, then
  `stop -o capture.wav` saves a WAV and answers with a timeline: sound and
  silence to a tenth of a second, each sound's pitch and level
  (`docs/checks/audio.sh`, on MobiumApp's Audio Demo, mobiumdev/mobium-app#21).
  The check passes on Android 15 and 17, each booted by `mobium boot`:
  seven runs in seven on 15, and on 17 three in three since a tone
  starting inside a window lost its pitch there (CHALLENGES 273). A 90-second capture placed a 45-second tone within
  0.15s of the taps that started and stopped it, at both ends: the stream
  keeps to the clock.
  It reads the emulator's own audio stream from its control port, which
  the emulator opens by default on 127.0.0.1 with a token in its discovery
  file, so nothing is started and nothing is put on the device; the boot's
  `-no-audio` silences the Mac and not the stream. What was measured:
  - **The platform's word is not the sound.** `dumpsys audio` reports the
    Audio Demo's silence — a player playing zeros — exactly as it reports
    a tone: `started`, its usage, not muted. The simulator's audio device
    on the Mac runs alike for both, and goes on running after. Either
    says a player runs, not that anything was heard.
  - **The system's sounds are in it.** With touch sounds on, a tap is a
    tenth of a second near 780 Hz at -47 dBFS; with them off the silence's
    loudest window was -97. The timeline reports it where it fell.
  - **Levels follow the device's volume** — a tone written at -15 dBFS
    arrived at -42 — so a check asserts sound, silence and pitch.
  - **The stream says nothing until the device has played**: not even its
    headers, so a capture counts that time as silence.
  - **Not yet**: a real phone, where nothing outside it hears what it
    plays — a capture needs a helper on the phone with the shell's
    permission, filtered to the app under test, since the phone also plays
    its owner's notifications and calls; the iOS simulator, whose audio
    goes to a Mac device of its own; and an iPhone — both measured since,
    below. **Measured since, on the Pixel 8 Pro** (2026-10-07): a capture
    of one app's audio alone works from adb as the shell user — an audio
    policy matching MobiumApp's uid, with no app installed and no consent
    asked. It heard 440 Hz, the sequence, and the silence as silence, and
    not the tap's click, which is the system's; and at -15 dBFS, what the
    app wrote, since it taps the app before the volume. Not built yet: a
    pinned helper on the phone for `--app`.
  - **iOS, measured, not built** (2026-10-07; `docs/probes/ios-audio-sim.swift`,
    `ios-audio-phone.swift`, `audio-runs.py`). Both heard the Audio Demo's
    440 Hz, a second of silence and 880 Hz, two seconds each, at -15.1
    dBFS — what the app writes — and its silence as zeros while audio kept
    arriving: the negative control. `mobium audio` still refuses both.
    - **The simulator, one app.** An app in the simulator is a process on
      the Mac, and CoreAudio lists it by its bundle id with the process's
      path holding the simulator's UDID. A process tap (macOS 14.2+) on it
      hears that app alone: a sound played on the Mac during the capture
      was not in it. The app is muted on the Mac's speakers while tapped,
      and nothing is put on the simulator. Costs: macOS asks once for
      "System Audio Recording" for whatever runs the tap, and until it is
      allowed the tap gets no audio at all — no IO cycles, which can be
      told from silence. An app is listed only once it has opened audio
      (Settings never was), so a capture started first must watch for it.
      CoreAudio's "running output" stayed on through the silence and after
      the tone ended — the platform's word again, not the sound.
    - **A real iPhone, the whole route.** The phone's USB screen-capture
      source — the one a USB view of the screen uses, after CoreMediaIO is
      told to allow such devices — carries its sound too, and WebDriverAgent's
      taps worked while it ran (iPhone Mirroring drops them). It is not a
      listener: while it runs the phone is silent and the Mac plays nothing,
      because the capture is an audio route on the phone, which asks once
      what it is. So the app under test sees its output change when a
      capture starts and stops, and the phone's owner hears nothing
      meanwhile. Sound arrives about 1.3s after the capture opens. Not
      measured: whether other apps' sound is in it (it is the phone's
      route, so likely), whether the level follows the phone's volume, and
      what tells a call or an alarm cutting across the app on iOS.
    - **Building it**: Mobium is built with `CGO_ENABLED=0`, and both APIs
      are Objective-C, so the likely route is a small Swift helper compiled
      on the Mac, as WebDriverAgent is — a simulator or a phone means
      Xcode is there.
    - Measuring the simulator found MobiumApp's tone module crashing on
      play — "player did not see an IO cycle" — in a process left running
      for hours, seconds after the audio device it played to was gone. A
      fresh launch played every time; what removed the device is not known.
  - **Asserting it in a test, done**: stop takes `expect`, the sounds to
    hear in order, and fails as not_confirmed saying what was heard, with
    the capture saved; `--expect 440:1.8-2.2,880` on the CLI, and
    `tests/audio/` in `mobium test` (guide, section 11). The result also
    says the media volume: the same tone arrived at -9 dBFS at 15 of 15,
    -42 at 5, -63 at 1 and as silence at 0.
  - **An incoming call** silenced the app's tone on the emulator for as
    long as it rang and brought it back on hang-up, while the app said it
    was playing throughout; the capture held the ring instead.
    `audio.sh` asserts it. **On the Pixel 8 Pro, with the ringer on and a
    real call** (2026-10-07), the audio service logged "call: muting" the
    app's player as the ringtone started and "call: unmuting" 12.6s later,
    and neither shell capture showed it: the mute is the app's own player
    volume, which the capture sits before, and the system's ringtone was
    kept out of a capture matching its usage. So on a phone an
    interruption is read from `dumpsys audio`'s player events — any phone
    adb reaches, a cloud farm's included — not heard; and making one ring
    takes a call from outside, which only a farm with a line can place.
  - **What interrupted the app, done** (2026-10-07): the stop reports it
    from that log, for start's `app` or the app in front — the app's own
    player muted, for a call or by a volume, and any other app's player
    sounding over it, named by what it is for: ringtone, alarm,
    notification, voice call, assistant, navigation, another app's media
    (a touch's click is not one). **On a phone `audio start` records
    these alone**, no sound, so any Android phone adb reaches — a cloud
    farm's — reports a call or an alarm cutting across the app. A Clock
    timer's alarm was reported on the emulator and the Pixel; on the Pixel
    it also muted the app's media for 40ms in pairs each time its sound
    began again, which the message counts rather than lists. `audio.sh`
    asserts the call's report and the alarm on both, the phone with
    `ALLOW_PHONE=1`, which stops its timer by the timer's own Stop — never
    by force-stopping Clock, which would cancel its owner's alarms.
  - **Hardened** (2026-10-07, CHALLENGES 279–284): a capture cut short — by
    its daemon stopping or killed, the emulator dying, a session ended —
    says it was lost and why; a capture outlasts the idle timeout and keeps
    at most an hour; each closes its connection; refusals name remedies
    that work. A lead from it: a frozen emulator's session start times out
    on `adb` and suggests the other Android driver, which needs the same
    `adb`.
  - Also open: comparing a capture with a baseline recording, and
    the emulator's microphone (`injectAudio`), which would let a test
    speak to an app.
- **Seeing an app's outgoing intents** on Android, so a test can assert that
  "share" asked for the chooser with the right text, without stubbing it.
- **Menus and long press on iOS.** Measured 2026-10-04 on Ice Cubes'
  post menus (CHALLENGES 220, 221): `long-press` opens a context menu,
  `map` lists the menu alone with its items by name, items work by label
  or ref, a submenu opens and maps, and a tap behind the menu is refused
  with a point outside it that closes it. On Android a popup menu is a
  window of its own, refused behind as a dialog is, and the refusal names
  back, which closes it (CHALLENGES 225; the Pixel 8 Pro's Clock). Declined: `map` saying an item
  opens a submenu. Share and Mute show a chevron, and the only sign of it in
  the tree is an image named `chevron.forward` — no trait or role — which
  the same apps also draw on rows that open a page; a state inferred from an
  icon's name would read as a promise the platform never made. Open: the
  long press's preview, which iOS reports with no label and nothing inside
  it, so it maps as `Other (button)`.
- **Sliders.** Done on iOS 2026-10-04 (CHALLENGES 219): a slider maps as
  `Slider (slider, 100%)`, its value as its state and as `value` on every
  client's element, `map --diff` reports a value that moved, and
  `app_fill` takes its position from 0 to 1 and reports what the app then
  reads. WebDriverAgent moves the thumb as a finger would: from either end
  it lands exactly, from partway it lands a step past, so landing exactly
  means filling 0 or 1 first, which is the caller's choice — the app sees
  the end it passes through. Measured on Ice Cubes' Font Scaling on the
  simulator and the iPhone, held by `icecubes-ios.sh`, which puts the
  slider back to what it read. On Android since the same day (CHALLENGES
  227): a seek bar is in `map` with its progress as its value, and
  `app_fill` touches its track at the position, which lands exactly —
  measured on MobiumApp's Slider Demo on the Pixel 8 Pro and held by
  `docs/checks/slider.sh`. Still to come: the Slider Demo on iOS, whose
  `@react-native-community/slider` reports a plain view with no Adjustable
  trait and no value, so nothing outside the app can call it a slider and
  WebDriverAgent will not move it; range sliders; and the phone's text
  size, which is a slider in Settings. A custom control that does carry the
  Adjustable trait — Pocket Casts' scrubber — maps as `adjustable` with its
  value since 2026-10-05 (CHALLENGES 234), and `app_fill` refuses it,
  naming a drag: WebDriverAgent cannot move it to a position.
- ~~**Infinite scrolling**~~ — done on iOS 2026-10-04 (CHALLENGES 223): a
  swipe that moves nothing while a busy indicator is inside the list waits
  for it to go, up to ten seconds, and goes on scrolling, so `scroll-to`
  reaches a row two pages down; a real end, with no indicator, is still the
  end at once. Measured on MobiumApp's Feed Demo, built for it, on the
  simulator and the iPhone, and held by `docs/checks/feed.sh`. On Android
  since the same day: the Feed Demo's spinner is a ProgressBar, and the
  check passed three runs of three on the Pixel 8 Pro, where the loop before
  the fix stopped at "the end of the list (3 scrolls)" as it had on iOS.
  Still to come: a list that loads with no indicator at all, which nothing
  here can tell from an end.
- **Switching between apps** and more than one window.
- **Seeding a device** with photos, files and other data before a flow.
- **Uploads and downloads** through the system file pickers. Copying a file
  to and from the device is done (`upload` and `download`, above, verified
  on a real iPhone on 2026-09-29); choosing it in the picker an app opens
  is not.
- ~~**Frames and iframes** inside a WebView.~~ Done 2026-10-04 (CHALLENGES
  228) for every frame the page can see into, nested ones included: mapped
  with the frame they are in, tapped, checked and filled, on the simulator
  and an emulator, held by `docs/checks/frames.sh`. Cross-origin frames too,
  the same day (CHALLENGES 229): each is mapped, tapped and filled through
  its own execution context, on both platforms; `NATIVE_APP` reaches them as
  well. On the iPhone 15 Plus too, on 2026-10-05, where a WebView is
  reached through usbmuxd and lockdown rather than the simulator's socket:
  `frames.sh` passed on its first run. Not reached: a cross-origin frame
  inside a cross-origin frame, which `map` would not know is there.
- ~~**A WebView with the iOS keyboard up refuses every action.**~~ Done
  2026-10-04 (CHALLENGES 230): with the keyboard over an app's WebView the
  frame is the WebView's, by its width, without the comparison written for
  mobile Safari, which still applies in Safari. Measured by touch first —
  the page still starts at the WebView's top with the keyboard up and the
  page scrolled — and held by `web-type.sh`, which taps two fields with the
  keyboard up and fills one, on the simulator and an emulator, and since
  2026-10-05 on the iPhone 15 Plus.
- **Accessibility checks** as a side effect of the actions already being taken.
  An explicit one exists since 2026-09-30: `mobium audit` (`app_audit`)
  runs Apple's own audit on the screen in front, on a simulator and on the
  iPhone (`docs/checks/audit.sh`). On the iPhone 15 Plus Settings gave
  eight findings in about six seconds, seven of them contrast findings that
  name no element, which a simulator's do; MobiumApp's Layout Demo gave
  none, because Apple's hit-region rule is far below the 44 pt guideline
  that `screen --inspect` holds. Android refuses: its audits run inside the
  app. Running it as a side effect would cost those seconds on every
  action, so it stays a call.
- ~~**The iPad's home screen, sometimes.**~~ Done 2026-10-04 (CHALLENGES
  224): once an app has been used, WebDriverAgent names the Dock's
  recent-apps service as the app in front of an iPad's home screen, with
  nothing in it; such a read is taken again with SpringBoard named.
  Measured on the iPad mini and iPad Pro 13-inch simulators; a real iPad
  not tried.
- **Fold posture as device state** (written down 2026-09-28), as rotation
  is: read with `cmd device_state state`, set by its override, and each
  change read back. Measured on a Pixel 9 Pro Fold emulator, where all four
  postures could be set and read ([FORMFLUX.md](FORMFLUX.md#foldables));
  whether a real foldable's shell may set it is the open question.
- **A mobium grid, and running mobium remotely** (written down 2026-09-28).
  Stage 0 is done, and needed no code: clients start `mobium pipe`, and
  `MOBIUM_BIN_PATH` may name any executable, so a short script that runs
  `mobium pipe` on another machine over SSH makes an unchanged client
  remote. Tried against the same Mac standing in for a node, with a key
  accepted only from localhost: the node started a daemon of its own, and
  a Python client's session, `map`, text and route by waypoints worked, and
  a screenshot's bytes came back. What broke is every argument that is a
  file path, because the node reads or writes it on its own disk:
  `screenshot` and `record` saved to the node's working directory and
  reported the node's path, and `install` looked for the app there; the
  CLI's GPX file is the same case. Relative paths showed it even on one
  machine; between two, absolute ones would too.
  1. **Stage 1: files as content — done, 2026-09-28.** With
     `MOBIUM_FILES=content`, the CLI and `pipe` send `install`'s app (a
     `.app` directory as a `.tar.gz`, links kept) and a GPX route as their
     content, and have a screenshot or a recording come back and saved where
     the caller asked, answering as the daemon would have; the daemon writes
     what it receives to a temporary directory and removes it. Verified on an
     Android emulator and an iPhone simulator, and through an unchanged
     Python client. It works where the conversion runs — the caller's own
     `mobium` — so it needs a transport that keeps that process local. SSH
     already is one: forwarding a node daemon's socket to a local path
     (`ssh -L <local.sock>:<node.sock>`) and pointing `MOBIUM_HOME` at it
     ran the CLI's install, GPX route, recording and screenshot, and an
     unchanged Python client, against the node's daemon, every file landing
     in the caller's folder and none on the node. Tried against this Mac
     standing in for a node.
  2. **Stage 2: a remote transport — done, 2026-09-28.** `--remote <node>`,
     or `MOBIUM_REMOTE`, on every command and on `mobium pipe`: mobium runs
     `mobium daemon up` on the node over SSH, forwards the socket it names
     to an owner-only one here, sets `MOBIUM_FILES=content`, and removes both
     on the way out. A call the node does not answer fails rather than start
     a daemon here. An unchanged Python client went remote with the
     environment variable alone ([SETUP.md](SETUP.md#driving-another-machines-devices)).
  3. **Stage 3: a router — done, 2026-09-28,** without a hub: `MOBIUM_GRID`
     names the nodes, the caller's own mobium routes each run by serial or
     platform, and the lease lives on the node — exclusive, renewed every
     20s, free 60s after a run dies, released at the end. A queue waits for a
     device up to `MOBIUM_GRID_WAIT`; a node that does not answer is left out.
     A `kill -9` left an SSH forward running for good until the forward
     read a pipe its parent holds ([SETUP.md](SETUP.md#a-grid)).
  4. **Model, OS and enforcement — done, 2026-09-28.** `MOBIUM_GRID_MODEL`
     and `MOBIUM_GRID_OS` route by model and OS version, the node reporting
     each device's. Each run gets a daemon of its own on its node, named by
     its lease, and every daemon there refuses a device leased to another —
     a run on the node that bypassed the grid was refused, naming the
     holder. Still to do: one device per run holds as anywhere, and nothing
     yet shares one session between two runs.
  5. **A grid UI — done, 2026-09-28.** `mobium grid status` and `mobium grid
     ui`, a page on `127.0.0.1` refreshed every few seconds: each node, its
     devices, who holds each lease and for how long, the nodes down, and the
     runs waiting — each queued run leaves a note on the nodes it asked,
     which lapses by itself. Both read the answer routing reads. Building it
     showed the grid listing a real iPhone as free: a node now lends a
     physical phone only with `MOBIUM_GRID_PHONES=1` in its own
     environment.
  6. **Docker for remote** (written down 2026-09-28): a node in a container —
     an Android emulator with mobium and an SSH server beside it — so a grid
     can be stood up without a spare machine, and torn down with it. Android
     only: an iOS node has to be a Mac, and a simulator does not run in a
     container. What to measure first is whether an emulator runs in the
     container at usable speed, which depends on hardware acceleration
     reaching it.
- **MobiumApp on AWS Device Farm, on its free trial** (written down
  2026-09-28, nothing measured yet) — the first devices Mobium would drive
  that nobody here owns. What the plan rests on, and must be checked against
  AWS's current terms before any run: a one-time free allowance of device
  minutes for a new account, and a *custom test environment*, where a test
  spec runs shell commands on a host with the device attached (Linux and adb
  for Android, macOS for iOS). Set a billing alarm at $1 first, so the end of
  the allowance is a notification and not a bill.
  1. **Android first.** Upload MobiumApp's release APK as the app, and as the
     test package a zip of a `mobium` binary built for the host's OS and
     architecture (find out which, first) with `docs/checks/`. The test spec
     runs `mobium devices`, then `login.sh`, `web-type.sh`, `obstruction.sh`
     and `dialogs.sh` against the attached serial, and copies the output into the
     run's logs. A real phone takes the checks' phone branches, which is
     what they are for; since `reset-permissions` names the app, `dialogs.sh`
     no longer needs the APK on an Android phone.
  2. **Measure before trusting.** Whether the host's adb reaches the device
     as a plain serial; whether the UiAutomator2 server may be installed;
     whether a device is wiped between runs. Each is a question with a
     device's answer, not a guess.
  3. **iOS second, and harder.** Mobium builds WebDriverAgent from source and
     signs it with a team from the local keychain; a farm's host has neither,
     and re-signs uploaded apps with its own identity. The route to find out
     is whether the host provides a signed WebDriverAgent — farms that drive
     iOS must — and whether Mobium can be pointed at a runner it did not
     build. Until that is answered, iOS on a farm is a question, not a step.
  4. **Budget.** A check takes two to five minutes of device time, so a trial
     of the size last seen covers a few hundred runs. Spend it on breadth — a
     handful of models neither of the phones here resembles — not on repeats.
- **Fire TV** (written down 2026-09-28; measured 2026-10-01 and 10-02 on a
  Hisense TV with Fire OS 7.7.1.7, which is Android 9). Fire OS is Android —
  7 is Android 9, 8 is Android 11 — reached by `adb connect <tv>:5555` once
  ADB debugging is on, and Amazon's newer Linux-based OS, Vega, is not
  Android and would be the case for a driver process
  ([the driver protocol](../examples/drivers/PROTOCOL.md)). The questions
  this entry asked, answered on the TV:
  - **It is listed.** After `adb connect`, `mobium devices` and `mobium
    doctor` both show it, and the UiAutomator2 server installs and runs.
  - **`map` reads the launcher**, and fails with `null root node` on some of
    Amazon's settings pages.
  - **`focused` follows the D-pad exactly; `selected` does not.** Every
    `DPAD_*` press moved `focused` to the node the screen highlighted, so
    focus is a state a tool can read back.
  - **What a touch tap does depends on the view.** On the launcher's top
    menu it only moved focus, and Mobium still reported "tapped" — the
    failure "report only what was checked" exists to prevent; those icons
    are tabs that open on focus alone, with no select at all. On a Settings
    tile, measured 2026-10-02, the same tap opened it: Network came forward.
    So a tap cannot simply be refused on a TV; it has to be read back.
    Select (`DPAD_CENTER`) is what activates everywhere.
  - **`mobium launch` refused TV apps** — they register only a
    `LEANBACK_LAUNCHER` activity — **and on Android 9 `state` read every app
    as in the background**, because nothing read Android 9's line for the
    activity in front. Both fixed and checked on the TV (CHALLENGES 209).
  - ~~**`screencap` prints a vendor line ahead of the PNG.**~~ Fixed: the
    dump backend refused it as "not a PNG", and UiAutomator2 silently fell
    back to its server's slower endpoint; both now read from the signature
    on (CHALLENGES 211).
  - **The two big apps draw to one surface.** Prime Video and YouTube (a
    Cobalt build) give `map` a single node. Prime publishes the focused
    item's name only while VoiceView runs — which is how CHALLENGES 208 was
    found, and why the reader of Mobium's own keeps VoiceView running; YouTube
    speaks through TTS directly and publishes nothing, so it is out of reach
    of any accessibility-based tool. Prime's picture is black on capture;
    its menus are not.
  - **The screen was watched through [scrcpy](https://github.com/Genymobile/scrcpy)**
    4.1, over the same Wi-Fi adb link, to see what the TV showed while
    Mobium read it. It mirrored the launcher and the apps' menus; Prime
    Video's picture was black there too, and Android 9 gives it no audio.
    It ignored SIGTERM three times out of three and had to be killed, and
    each run leaves `/data/local/tmp/oat/arm/scrcpy-server.{odex,vdex}` on
    the TV, which has to be removed afterward — the same runtime-compiled
    copy Mobium's own reader deletes after every read.
  - **The link is Wi-Fi and drops** every few minutes (`error: closed`,
    `device offline`), and on macOS an adb server started from a background
    process can be silently blocked by Local Network privacy — "No route to
    host" while ping works. `doctor` should name that, as it names the
    other traps that report the wrong cause. A drop under a session was
    reported as a failed reinstall of a server that was there, with advice
    to switch driver; now it is `device_not_ready` naming `adb connect`
    (CHALLENGES 210).
  - **Scenarios run on 2026-10-02**, with the default driver unless named.
    Working: `devices` (by model), `doctor`, `current`; `lock` reads the
    screensaver as locked and `lock unlock` wakes the TV; `find`, and `wait`
    both found (in about a second) and timed out (exit 6); `press back`
    reporting both leaving an app and staying in one, and `press home`;
    `screenshot` through UiAutomator2, a 1920x1080 PNG; `terminate` of
    Prime Video, read back as not running; and `mobium test`, a file of two
    tests on the TV as a project, the real one passing and a must-fail
    control failing. Not working, each a gap to close:
    - **Settings cannot be opened.** On Fire OS it is not an app: it is
      the launcher's `SettingsActivity`, reached by the
      `android.settings.SETTINGS` action, and `launch` takes a package
      while `open` takes a URL. Its screens belong to
      `com.amazon.tv.settings.v2`, which has no launcher activity either.
    - **`terminate` of a protected package** fails with Android's raw
      `SecurityException` and stack trace — Fire OS will not force-stop
      its own packages. A one-line refusal saying so is what it should be.
    - **`map` does not say what has focus**, which on a TV is the state
      that matters most; the hierarchy has it.
    - **The next row of tiles maps as `settings_card_view`**: five empty
      containers 44 pixels tall at the bottom edge, with nothing rendered
      in them yet, labeled by resource id.
    - ~~**A dropped link failed the test.**~~ Done 2026-10-02: a call
      that never reached the device is marked so (`details.reached`
      false), a network device is connected again before Mobium says it is
      missing or offline, and the runner waits out an unreached step. A
      20-step traced test passed through two deliberate disconnects and
      played in Vibium's player.
    - **`mobium test` says nothing when its trace cannot be saved.** It
      discards the error from the `app_trace stop` it makes after a test,
      so a test with `--trace on` can pass with no trace and no word
      (CHALLENGES 212 was found that way). It should say so in the result.
    - **`label=` against a TV tile's words answers "not on screen".** A
      Settings tile's name is the text of its child, so `label=Network`
      timed out with the tile on screen, where `text=Network` found it at
      once. The near-miss that names the locator that would have worked
      (CHALLENGES 196) did not fire here.
    - **`current` names the screensaver** (`com.amazon.ftv.screensaver`)
      while the launcher's activity is in front, since it reads the
      hierarchy — arguably right, as it is what is on screen.
    - ~~**`press` has no D-pad keys.**~~ Done the same day: `dpad-up`,
      `dpad-down`, `dpad-left`, `dpad-right` and `select`, and the media
      keys `play-pause`, `stop`, `next`, `previous`, `rewind` and
      `fast-forward`. A D-pad press reads focus back: on the TV it named
      each tile as focus reached it, across a row that scrolled in tiles
      not yet on screen, and said "focus did not move" at the top edge;
      select on the Network tile opened it. Media keys are reported as
      sent, as volume is. **The Settings row wraps** — right from its last
      tile, Help, went to its first, Inputs — so a focus strategy has to
      notice a cycle, not only a stop.

  What it takes, in order, after CHALLENGES 209 to 211 and the D-pad:
  `map` showing focus; a tap on a TV read back — focus moved, or the
  screen changed — rather than reported as "tapped"; a way to open an activity by
  action, for Settings; then the focus strategy — D-pad presses until the
  target reports `focused`, then select, each step read back and refused
  if focus stops moving or cycles — with swipes and `scroll-to` as D-pad
  presses too. That is the existing backend with a focus strategy, not a
  new driver. A check, `docs/checks/fire-tv.sh`, on a TV that is somebody's:
  it must leave VoiceView, ADB debugging's prompt and the home screen as it
  found them, and print nothing from a profile or a library.

## Not planned

- **Test code generation.** Mobium is a tool an agent or a test framework
  calls. Its own runner, `mobium test`, runs JSON test files of tool calls,
  and `mobium inspect` records one, but nothing generates test code in a
  client's language.
- **Device-cloud allocation.** Mobium drives devices you can reach; renting
  them is a separate concern.
- **Anything that needs a runtime on the user's machine.** One static binary is
  the point.
