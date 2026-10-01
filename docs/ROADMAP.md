# Roadmap

What is planned, roughly in order. Nothing here is a promise of a date.
Everything in the [README](../README.md) is done and verified on a device;
this is what is not.

## Next

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
  Still to do: what is left of a Settings tap, mostly WebDriverAgent
  waiting out the page animation after the touch. Found on the
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
- **Seeing an app's outgoing intents** on Android, so a test can assert that
  "share" asked for the chooser with the right text, without stubbing it.
- **Sliders**, and range sliders, as a first-class action.
- **Infinite scrolling** — a scroll loop that knows a list can grow as it is
  scrolled.
- **Switching between apps** and more than one window.
- **Seeding a device** with photos, files and other data before a flow.
- **Uploads and downloads** through the system file pickers. Copying a file
  to and from the device is done (`upload` and `download`, above, verified
  on a real iPhone on 2026-09-29); choosing it in the picker an app opens
  is not.
- **Frames and iframes** inside a WebView.
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
- **The iPad's home screen, sometimes.** On the iPad mini simulator,
  WebDriverAgent reported the Dock's folder service as the app in front on
  the home screen, with nothing to map; on the iPad Pro 13-inch, freshly
  booted, it reported SpringBoard as an iPhone does. So it depends on the
  home screen's state, not on iPads, and anything that confirms Home by
  SpringBoard coming forward could misread it. What state causes it is not
  yet known (CHALLENGES 147); a real iPad not tried.
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
- **Fire TV** (written down 2026-09-28, nothing measured yet). Fire OS is
  Android — 7 is Android 9, 8 is Android 11 — reached by `adb connect
  <tv>:5555` once ADB debugging is on, so discovery, the hierarchy, locators,
  text entry and app lifecycle should be the Android backend unchanged. What
  changes is the action model: a TV is driven by focus, not touch, so a tap
  becomes D-pad presses until the target reports `focused`, then select —
  each step read back, refused if focus stops moving or cycles — and swipes
  become D-pad presses too; `press` already has back, home and the media
  keys. That makes it the existing backend with a focus strategy, not a new
  driver. Amazon's newer Linux-based OS, Vega, is not Android, and would be
  the case for a driver process ([the driver protocol](../examples/drivers/PROTOCOL.md)).
  First, without code: does `mobium devices` list a TV after `adb connect`;
  does `map` work with `--driver uiautomator`, and does Fire OS let the
  UiAutomator2 server install; does the hierarchy report `focused` reliably
  as `input keyevent DPAD_DOWN` moves focus; and does `input tap` do
  anything in Amazon's launcher and one third-party app. DRM video will
  likely screenshot black.

## Not planned

- **Test code generation.** Mobium is a tool an agent or a test framework
  calls. Its own runner, `mobium test`, runs JSON test files of tool calls,
  and `mobium inspect` records one, but nothing generates test code in a
  client's language.
- **Device-cloud allocation.** Mobium drives devices you can reach; renting
  them is a separate concern.
- **Anything that needs a runtime on the user's machine.** One static binary is
  the point.
