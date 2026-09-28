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
- **On iOS, `double-tap` reaches a React Native `Pressable` as one press**
  (Android: two, 165-184ms apart). **A person's double tap is two**: on the
  iPhone 15 Plus, 2026-09-27, a human double tap on MobiumApp's Press target
  counted two presses 200ms apart, and `double-tap` on the same target one.
  So the defect is real and below Mobium, which is the control that was
  missing. Measured on the iPhone 17 Pro simulator every way WebDriverAgent
  offers: its double tap, the element's, and one W3C chain with a pause
  between the taps (WebDriverAgent drops a pause while the pointer is up, so
  they arrive together) each counted one press; two separate taps counted
  two, but 350-380ms apart, past the platform's window. What is left to try
  is a chain whose gap WebDriverAgent cannot drop — the pointer kept busy
  between the taps rather than paused. The other gesture found with this
  one, a tap above the Android keyboard, was the app moving its button
  (CHALLENGES, "Findings that were not defects").
- **Android contexts include other apps' pages.** iOS lists only the app in
  front since CHALLENGES 138, from WebKit's own active flag. Android lists
  every debuggable page on the device, Chrome's tabs included, and has no
  such flag in `/json/list`; what tells a page on screen from one behind has
  to be measured there, custom tabs included, before the same rule applies.
- **A page attached while its app goes behind.** CHALLENGES 138 refuses
  switching to a page whose app is not in front; a page already attached when
  its app leaves the screen is not yet noticed, and a tap into it is aimed
  through whatever WebView is in front.
- **`wait` sees what a dialog covers; `text` refuses it.** On the iPhone,
  with "Save Password?" over MobiumApp, `wait testid=welcomeText` reported it
  visible in 816ms while `text` refused it as under the dialog. On Android the
  dialog's window is all there is, so the same `wait` would time out. One
  answer for both, and a check that asserts it.
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

## Not planned

- **A test runner.** Mobium is a tool an agent or a test framework calls; it
  does not want to be the framework. Test code generation is the same decision.
- **Device-cloud allocation.** Mobium drives devices you can reach; renting
  them is a separate concern.
- **Anything that needs a runtime on the user's machine.** One static binary is
  the point.
