# Roadmap

What is planned, roughly in order. Nothing here is a promise of a date.
Everything in the [README](../README.md) is done and verified on a device;
this is what is not.

## Next

- **Windows.** Everything cross-compiles for Windows and the named-pipe daemon
  transport is written; it has not yet been verified on a Windows machine, and
  until it has, Windows is unsupported. [WINDOWS.md](WINDOWS.md) is the state
  of it.
- **Two gestures that land wrong, found and not yet diagnosed** (driving
  every Go client method against MobiumApp, 2026-09-27):
  - On a headless Android emulator, a tap on a button still visible above the
    full on-screen keyboard is reported as tapped and never reaches the app;
    the keyboard stays up and the button leaves the hierarchy. Windowed
    emulators show only a floating toolbar, and the iPhone simulator passes.
  - On iOS, `double-tap` reaches a React Native `Pressable` as one press
    (Android: two, 165-184ms apart). WebDriverAgent's own double tap is the
    only form WebKit accepts, so the fix is not simply a different chain.
- **iOS contexts include other apps' pages.** After Safari has opened a link,
  `contexts` on iOS lists its page beside the app's own WebViews; the names
  say which is which, but the list is not scoped to the app in front.
- **Context names are positions.** `WEBVIEW_<package>_1`, `_2` number the
  pages in listing order, so a tab opening between two calls renumbers the
  rest, and a name taken from one listing can attach another page (seen with
  Safari on 2026-09-27). An installed PWA and the Chrome tab it came from are
  both `WEBVIEW_com.android.chrome`. Naming by CDP target id or page id would
  hold still; the URL is what a caller can recognize.
- **On iOS, `role=button` misses a row `map` calls a button.** In Safari's
  share sheet, `map` listed "Add to Home Screen (button)" while
  `label=Add to Home Screen,role=button` found nothing, and `scroll-to` with
  that locator reported the end of the list; `label=View More` matched three
  nodes. The row is a Cell, and the role `map` prints and the role a locator
  matches evidently come from different rules. Measured on the iPhone 17 Pro
  simulator, 2026-09-27; not yet diagnosed.
- **Cookies and web storage.** Nothing reads or sets them today except
  `app_eval` of `document.cookie`, which cannot see HttpOnly cookies, and
  nothing checks whether `app_clear_data` empties a WebView's. The plan
  (2026-09-27): Vibium's command set as it is — get, set and clear cookies,
  export, restore and clear storage state — over CDP's cookie commands on
  Android and WebKit's `Page.*Cookie` and `DOMStorage` on iOS, both reachable
  through the transports' existing generic call path, none of it yet measured
  on a device. One JSON shape everywhere, the clients' Playwright-like one;
  Vibium has two, and its restore drops secure, httpOnly, sameSite and
  expiry. The scope has to be said: an Android app's WebViews share one
  cookie store, and a PWA on Android shares Chrome's. MobiumApp's WebView
  screen is the positive control.
- **Parallel runs on one daemon.** Separate `MOBIUM_SESSION` daemons run in
  parallel with no contention, measured on an emulator and a simulator at
  once. One daemon serves one call at a time, across every device, because a
  call holds the daemon's lock for its whole length. Whether it should serve
  different devices concurrently is open.
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
