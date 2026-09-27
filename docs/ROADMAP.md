# Roadmap

What is planned, roughly in order. Nothing here is a promise of a date.
Everything in the [README](../README.md) is done and verified on a device;
this is what is not.

## Next

- **Windows.** Everything cross-compiles for Windows and the named-pipe daemon
  transport is written; it has not yet been verified on a Windows machine, and
  until it has, Windows is unsupported. [WINDOWS.md](WINDOWS.md) is the state
  of it.
- **Auto-wait, the rest of the actionability checks.** Actions already wait for
  a target to exist, be in view, stop moving and be enabled; refuse one under
  a dialog or the keyboard; and aim around, wait out or refuse a control the
  app drew over it ([CHALLENGES 115](CHALLENGES.md)). Still to come:
  - An overlay hidden from accessibility on iOS, which WebDriverAgent's tree
    does not contain, so a tap under one still lands on it.
  - The same checks *inside WebViews*, run in the page, with the tap staying a
    real touch.
  - One error shape for a failed check — "failed check X: reason".
  - More states to `wait` for: checked, focused, a value.
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
