# Roadmap

What is planned, roughly in order. Nothing here is a promise of a date.
Everything in the [README](../README.md) is done and verified on a device;
this is what is not.

## Next

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
- **Prebuilt `mobium` binaries.** `go install
  github.com/mobiumdev/mobium/cmd/mobium@latest` works, but needs Go; someone
  installing a client from PyPI or npm should not have to have it. Releases
  with checksummed binaries for macOS and Linux, and possibly a Homebrew tap,
  are the missing piece — and the prerequisite for publishing the clients.
- **The quick start on Linux, against a device.** Mobium builds and passes its
  tests on Linux in CI; no emulator has been driven from Linux yet, so the
  quick start calls Linux expected rather than verified.
- **Video walkthroughs** of the quick start, one per client, once `start` and
  `quit` have settled. The pages' examples and captured output are the script.
- **Auto-wait, the rest of the actionability checks.** Actions already wait for
  a target to exist, be in view, stop moving and be enabled; refuse one under
  a dialog or the keyboard; and aim around, wait out or refuse a control the
  app drew over it ([CHALLENGES 115](CHALLENGES.md)). Still to come:
  - An overlay hidden from accessibility on iOS, which WebDriverAgent's tree
    does not contain, so a tap under one still lands on it.
  - One error shape for a failed check — "failed check X: reason" — on
    native screens too. A WebView's refusals use it since CHALLENGES 118,
    where Vibium's checks now run in the page.
  - More states to `wait` for: checked, focused, a value.
- **Accessibility settings on a real iPhone.** `app_accessibility` works on a
  simulator and on Android; on a phone nothing outside changes them, and the
  Settings screens are the route.
- **Network conditions.** Android only when it lands: the emulator console can
  throttle and read back, while `simctl` has no network control.
- **Session recording and `diff map`.** Screen recording is done; a filmstrip of
  what each call did, and a way to see what changed between two screens, are
  not.
- **Screen recording on a real iPhone**, which needs a video stream Mobium does
  not build yet.

## Under consideration

- **Devices served concurrently by one daemon.** Today a call holds the
  daemon's lock for its whole length, so two devices on one daemon go at the
  slower one's pace: an emulator's 15 `map` calls took 22.3s beside a
  simulator's, against 0.3s on a daemon of its own (2026-09-27). The answer
  chosen for now is one daemon per parallel run, which every client can name
  with its `session` option ([SETUP.md](SETUP.md#parallel-runs)); routing
  each call to its device's own lock would remove the wait in one daemon, and
  means reworking how every handler touches shared state.

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
  1. **Stage 1: files as content.** Those four arguments carry the file —
     an upload for `install` and a GPX route, the bytes back for a
     screenshot or a recording — instead of a path on the daemon's disk.
     Worth doing without a grid.
  2. **Stage 2: a remote transport,** `mobium pipe --remote <node>`,
     authenticated and encrypted — possibly SSH itself, since it already is
     both. The daemon's socket stays owner-only and local.
  3. **Stage 3: a router,** Selenium Grid's shape: a registry of nodes and
     their devices, a lease that keeps a device to one run, routing a
     session by platform and model, a queue when nothing matches is free,
     and dead nodes noticed. The node side exists — a daemon beside its
     devices, one per run, ports that do not collide (CHALLENGES 148). iOS
     nodes are Macs, and phones are on their USB, so a grid is a set of
     machines, as Appium's is.
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
     and `dialogs.sh` against the attached serial, with `MOBIUMAPP_BUNDLE`
     set to wherever the host puts the app, and copies the output into the
     run's logs. A real phone takes the checks' phone branches, which is
     what they are for.
  2. **Measure before trusting.** Whether the host's adb reaches the device
     as a plain serial; whether the UiAutomator2 server may be installed;
     whether a device is wiped between runs, which decides whether
     `reset-permissions` is safe there. Each is a question with a device's
     answer, not a guess.
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
