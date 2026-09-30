# Roadmap

What is planned, roughly in order. Nothing here is a promise of a date.
Everything in the [README](../README.md) is done and verified on a device;
this is what is not.

## Next

- **A test runner — `mobium test`, on the phones.** Iterations 1 and 2 are
  built and checked (`docs/checks/test-runner.sh`): JSON test files of
  `app_batch` steps, `app_wait_for` and `expect` assertions, projects as
  devices, workers, retries with flaky reported, `-g`, `--last-failed`,
  list, JSON, JUnit and HTML reports, `show-report` — and since 2026-09-29
  the step shorthand, soft assertions, a trace per test and `--debug`.
  On 2026-09-29 the suite passed seven of seven on the iPhone 15 Plus and
  on the Pixel 8 Pro, and on both the must-fail and soft controls failed as
  they must. The flaky control clears app data, which a phone cannot, so it
  stays on emulators and simulators. Getting there found CHALLENGES
  170–172. On an iPhone a password still needs a keyboard with its letters
  up (159). Later: an interactive
  mode, and parameters. Recording a test from what a person does is
  `mobium inspect`, since 2026-09-29 ([decisions/0007](decisions/0007-an-inspector.md)).
  [decisions/0006](decisions/0006-a-test-runner.md).
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
- **Video walkthroughs** of the quick start, one per client, once `start` and
  `quit` have settled. The pages' examples and captured output are the script.
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
    about two seconds ([decisions/0008](decisions/0008-a-hit-test-below-accessibility.md)).
    Still open: a real iPhone, which needs the app signed for debugging and
    a debug server on the phone; and doing it in milliseconds before every
    tap, which would mean loading the probe at launch.
- ~~**Session recording and `diff map`.**~~ Done 2026-09-29. `mobium trace
  start|stop` records a session as Vibium does: a zip in the Playwright
  trace format, with every call a step, the screen after it and the map
  drawn over it. trace.playwright.dev opens it (`docs/checks/trace.sh`,
  measured with `MOBIUM_TRACE_VIEWER=1`). `mobium map --diff` answers what
  changed since the last map (`docs/checks/map-diff.sh`). Next, if wanted:
  `mobium test --trace` writing the same zip, and a batch's steps as a group
  in it.
- **Screen recording on a real iPhone**, which needs a video stream Mobium does
  not build yet. WebDriverAgent already serves one, as MJPEG on its port
  9100. The plan is to read it over the same tunnel address as the HTTP port
  and write the frames into a container in Go, so nothing else needs
  installing. Record the frame rate achieved, not the one asked for. Done
  when a recording of MobiumApp's Motion Demo plays back with the confetti in
  it, and nothing is left on the phone.
- **Keep Mobium's runner on a phone that other tools also drive.** Tools that
  install their own WebDriverAgent can remove every installed runner whose
  `CFBundleName` is WebDriverAgent's, whatever its bundle id. Mobium's is
  built from WebDriverAgent's source and carries that name, so it gets
  removed (measured on the iPhone 15 Plus, 2026-09-30; see CHALLENGES 181
  for the simulator case). Mobium rebuilds it on its next session and says
  nothing. Two changes: give the device build a name of its own through an
  Info.plist override at `xcodebuild` time, and when a session finds its
  runner missing, say so rather than reinstall silently.
- **Touch targets on iOS.** `app_screen inspect` checks none on iOS, since
  the threshold was found wrong by the pixel-to-point scale (FORMFLUX.md).
  Divide a node's bounds by the screen's scale, compare with Apple's 44 pt,
  and keep the unit in the field's name. Done when the Layout Demo's small
  target is named on a simulator and on the iPhone, and a 44 pt button is
  not.
- **No `--driver` for a device Mobium has identified.** `mobium alert
  --device <udid>` on the iPhone answered that the device is an iPhone,
  driven by `wda`, and to pass `--driver wda`. When the lookup settles on one
  platform, use its driver, and refuse only a `--driver` that contradicts the
  device.
- **The cost of an action on iOS.** Every action reads the whole hierarchy
  through WebDriverAgent, about 500 ms, and on 2026-09-30 a tap's median was
  1.1 s on the simulator. Resolve `testid=` and `label=` with
  WebDriverAgent's own element queries, read the full source only where a
  check needs it (covered, keyboard, dialog), share one read across an
  action's checks, and try `snapshotMaxDepth` and excluded attributes. Done
  when the median tap is under 600 ms and every refusal in
  `docs/checks/obstruction.sh` and `autowait.sh` is unchanged.
- **Launching on a real iPhone.** `app_launch` had a median of 5.8 s on the
  iPhone 15 Plus against 2.3 s on the simulator (2026-09-30, 36 launches).
  First find where it goes: the launch, setting `defaultActiveApplication`
  before the switch (CHALLENGES 71), or waiting for the app to come to the
  front. Done when the median is under 2.5 s, the foreground is still
  confirmed, and `docs/checks/ios-device.sh` passes.
- **A reset a phone can do.** `app_clear_data` is refused on a real iPhone,
  which has no way to clear an app's container, and the refusal already
  names uninstalling and reinstalling as the reset. Take an app bundle path
  and do it, as `MOBIUMAPP_BUNDLE` does in the checks, and say that
  permissions are reset too. Measure that first: on 2026-09-28 the iPhone
  kept a notification denial through a reinstall.
- **The iOS settings still refused as not built:** orientation, locale,
  timezone and notifications. Per-app locale can work on a phone too, as
  launch arguments (`-AppleLanguages (xx) -AppleLocale xx_YY`) read back
  from the app. Orientation can go through WebDriverAgent's `/orientation`,
  refusing an app pinned to one orientation as Android already does.
  Timezone on a simulator can be `TZ` in the launch environment.
- **Auto-advancing code boxes on iOS.** In one simulator run the OTP Demo's
  six boxes lost a digit as focus moved. Mobium reported the loss (`"6"
  never arrived, lost as focus moved`) instead of passing, but the step
  failed. When a field
  moves focus by itself after each character, type one, wait for focus to
  land on the next field, then go on. Done when `otp-boxes` passes ten runs
  in ten on a simulator and on the iPhone.

## Under consideration

- **Devices served concurrently by one daemon.** Today a call holds the
  daemon's lock for its whole length, so two devices on one daemon go at the
  slower one's pace: an emulator's 15 `map` calls took 22.3s beside a
  simulator's, against 0.3s on a daemon of its own (2026-09-27). The answer
  chosen for now is one daemon per parallel run, which every client can name
  with its `session` option ([SETUP.md](SETUP.md#parallel-runs)); routing
  each call to its device's own lock would remove the wait in one daemon, and
  means reworking how every handler touches shared state.

- **Battery and the device's clock** (written down 2026-09-28): the level
  and whether it is charging, and the time as the device has it, which is
  what a test of a timezone or a clock-dependent screen checks against.
- ~~**Pulling a file off the device**~~ (written down 2026-09-28) — done
  2026-09-29 as `upload` and `download`: the shared Download folder on
  Android, an app's Documents on an iOS simulator, each confirmed at the far
  end (`docs/checks/files.sh`). Since 2026-09-29 also a real iPhone,
  through CoreDevice's file service, which has no delete (CHALLENGES 173).
  The check passes on the iPhone 15 Plus and the Pixel 8 Pro.
- **Booting and shutting down emulators and simulators** (written down
  2026-09-28). `devices` lists them; starting one is still the platform's
  own command.
- **Seeing an app's outgoing intents** on Android, so a test can assert that
  "share" asked for the chooser with the right text, without stubbing it.
- **Sliders**, and range sliders, as a first-class action.
- **Infinite scrolling** — a scroll loop that knows a list can grow as it is
  scrolled.
- **Switching between apps** and more than one window.
- **Seeding a device** with photos, files and other data before a flow.
- **Uploads and downloads** through the system file pickers.
- **Frames and iframes** inside a WebView.
- **Accessibility checks** as a side effect of the actions already being taken.
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
     is whether the host provides a signed WebDriverAgent — farms that run
     Appium must — and whether Mobium can be pointed at a runner it did not
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
  the case for a driver process ([decisions/0003](decisions/0003-drivers-are-processes-not-plugins.md)).
  First, without code: does `mobium devices` list a TV after `adb connect`;
  does `map` work with `--driver uiautomator`, and does Fire OS let the
  UiAutomator2 server install; does the hierarchy report `focused` reliably
  as `input keyevent DPAD_DOWN` moves focus; and does `input tap` do
  anything in Amazon's launcher and one third-party app. DRM video will
  likely screenshot black.

## Not planned

- **A test runner.** Mobium is a tool an agent or a test framework calls; it
  does not want to be the framework. Test code generation is the same decision.
- **Device-cloud allocation.** Mobium drives devices you can reach; renting
  them is a separate concern.
- **Anything that needs a runtime on the user's machine.** One static binary is
  the point.
