# Pre-release checklist

**What CI cannot check.** Everything device-free is in `make ci`, which the
[CI workflow](../.github/workflows/ci.yml) runs on every push — formatting,
vet, both modules' tests, the four non-Go clients' own tests, and six
cross-compile targets. This file is the rest, and the rest is where most
serious defects in this project have come from: 153 of 189 were found only by
running against a real device.

Work through it before tagging a release, on three substrates: an Android
emulator, an iPhone simulator and a real iPhone — and a real Android phone for
the items that say so. Most of it is now the scripts in [checks/](checks/),
which assert what they can and print what a person should read.

**Last full run: 2026-09-25**, on a Pixel 7 AVD (API 35), an iPhone 17 Pro
simulator (iOS 26.5) and an iPhone 15 Plus (iOS 26.6.2): 29 minutes by the
clock, from booting the emulator to `clean-stop.sh` reporting clean, with the
three devices' scripts running side by side, each under its own
`MOBIUM_SESSION`. The scripts took 16 minutes on the phone, 8 on the
simulator and 6½ on the emulator; the phone's are the long pole. Everything below passed except what is listed here, and
it found four defects before they shipped (CHALLENGES 91 and 92, and two
checks that could only pass alone), and the phone below found three more:

- **A real Android phone, run the same afternoon** on a Pixel 8 Pro
  (Android 17): `calculator.sh`, `clock-timer.sh`, `external-driver.sh`,
  `crashes.sh` and `clients.sh` pass. It found two defects the emulator could
  not (CHALLENGES 93, 94) and one in the client flows.
- **Not run: Windows** — the laptop has not arrived; see that section.
- **Skipped by design on the phone:** setting a location and writing the
  clipboard, which a real iPhone refuses and the check asserts the refusal.

---

## Why there is no device job in CI

A hosted Android emulator job is possible and a hosted iOS simulator job is
easy. Neither is here, for one reason: **a device test that runs unattended
tells you it failed, not what the screen looked like.** Every defect in
[CHALLENGES.md](CHALLENGES.md) was diagnosed by reading a hierarchy or
comparing a computed point against a screenshot, which is a human activity.

That is a judgment, not a law. Revisit it when there is a suite worth running
unattended — a regression pack of screens with known-correct maps would be the
thing to build first.

**It also runs against the advice in [*Emulator vs Simulator vs Real
Device*](https://medium.com/@begunova/emulator-vs-simulator-vs-real-device-15ce1dd5babf),
which this project's author wrote.** That piece puts virtual devices squarely
in CI — roughly 90% of pre-commit validation, 70/30 virtual to real at
multi-merge, real devices nightly — on the grounds that a bug costs more the
later it is caught, so the cheap substrate belongs early.

The argument is right and the conclusion does not transfer, for a reason
specific to what is being tested here. That pipeline assumes the app under
test is the thing that might be wrong and the harness is trusted. **Here the
harness is the thing under test**, and its defects are overwhelmingly
*misreadings* of a device rather than crashes: a label attached to the wrong
node, a tap computed 186 pixels high, a rotation read in the wrong unit, a
password printed in plaintext. Each was caught by looking at a hierarchy or a
screenshot. A green unattended run would have reported all of them as passing,
because nothing threw.

So the split is the same and the line sits elsewhere: everything that can be
decided by an assertion is in `make ci` and runs on every push, and everything
that needs a person reading output is here. The day a regression pack can
assert "this screen maps to exactly these refs", most of this file moves left
and the advice applies unchanged.

[SETUP](SETUP.md) lists what each substrate
cannot do at all.

---

## Android — a booted Pixel 7 AVD

Setup is in [SETUP.md](SETUP.md), or [WINDOWS.md](WINDOWS.md) for Windows.
Start the emulator, then `mobium daemon stop` so nothing serves a stale build.

- [ ] `mobium devices` lists the emulator
- [ ] `mobium map` on the launcher returns app icons as `button`, not `link`
- [ ] `mobium launch com.android.settings && mobium tap "text=Network & internet"`
      with **no sleep between them** — the implicit wait is what makes this work
- [ ] `mobium wait "text=Network & internet"` returns a ref that `mobium tap`
      then accepts without an intervening `map`
- [ ] `mobium scroll-to "text=About emulated device"` reaches it, and reports a
      scroll count above zero
- [ ] `mobium scroll-to "text=Definitely Not Here"` stops at the end of the
      list in a few swipes, not fifteen
- [ ] `mobium grant com.android.chrome all` grants every declared permission,
      confirmed with `adb shell dumpsys package com.android.chrome | grep granted=false`
- [ ] `mobium --driver uiautomator map` works on an ordinary screen and
      refuses the About page by name (see defect 25)
- [ ] `mobium screenshot -o /tmp/x.png` writes a PNG whose coordinates agree
      with what `map` reported
- [ ] `mobium appearance dark` then `light`, each confirmed by reading it back
- [ ] `mobium apps` lists what is installed; `--system` includes the platform's
- [ ] After a `--driver uiautomator` run, `adb shell ls /data/local/tmp` has
      no mobium file in it
- [ ] `mobium uninstall com.android.settings` **fails**, naming
      DELETE_FAILED_INTERNAL_ERROR rather than reporting success

## iOS — a booted iPhone simulator

- [ ] `mobium --driver wda map` on the home screen returns icons
- [ ] Wait for something already on screen, and across a cold app launch
- [ ] `mobium scroll-to` reaches a Settings row below the fold, and tapping it
      opens that screen
- [ ] `mobium grant com.apple.Preferences photos` succeeds and says it could
      not be verified
- [ ] `mobium grant <anything> camera` is refused, naming the missing service
- [ ] Coordinates from `map` land where the screenshot says they should —
      iOS reports points, screenshots are pixels, and a 3x error is silent
- [ ] `mobium appearance dark` and a screenshot that is actually dark; `auto`
      is refused
- [ ] `mobium apps` lists WebDriverAgentRunner; uninstalling it works and the
      next command reinstalls it unaided
- [ ] `./docs/checks/ios-webview.sh` passes — WKWebViews over Remote Web
      Inspector, including that Safari's coordinates are *refused* rather than
      guessed. Stop the daemon first: a page's inspector target belongs to one
      debugger at a time, so a session left switched into a page blocks it
- [ ] Switch into a WebView, leave, and switch back in **on the same daemon
      session**. This is the one that breaks silently — a page's target is
      announced once per connection, so a missing `_rpc_forwardDidClose:`
      makes the second attach fail on a page that is plainly there

## A real iPhone

Paired, unlocked, and with Developer Mode on — see [SETUP.md](SETUP.md). The
first command builds and signs WebDriverAgent, which takes a minute or two.

- [ ] `./docs/checks/ios-device.sh <udid>` passes — app switches timed against
      the 61-second stall (defect 71), rows with no bounds, typing read back,
      and every refusal a phone owes
- [ ] `./docs/checks/third-party-app-ios.sh <udid>` passes — Wikipedia from the
      App Store, the only third-party app driven on iOS
- [ ] `MOBIUMAPP_BUNDLE=<path>/MobiumApp.app ./docs/checks/mobium-app.sh <udid>`
      passes, WebViews included — the phone's reach them over lockdown, not the
      simulator's socket
- [ ] `./docs/checks/crashes.sh <udid>` passes with MobiumApp installed: its
      logged line is in the captured log, and its Crash Demo's report is listed,
      read in full, and its cause found in the log rather than the report
- [ ] `mobium --device <udid> current`, **without** `--driver`, names the
      wda backend rather than saying there is no such device
      (defect 88)

## End-to-end

- [ ] `./docs/checks/calculator.sh <serial>` passes on an emulator
- [ ] `./docs/checks/clock-timer.sh <serial>` passes on an emulator
- [ ] Both pass on a real device, unchanged
- [ ] `./docs/checks/external-driver.sh <serial>` passes — the third-party
      driver protocol, with the reference driver's map diffed against the
      built-in backend's on the same screen
- [ ] `./docs/checks/device-state.sh <serial>` passes — all four orientations
      read back, the hierarchy follows the rotation, a tap lands in landscape,
      and an app pinned to `ja-JP` renders in Japanese with no split characters
- [ ] `./docs/checks/third-party-app.sh <serial> wikipedia.apk` passes — the
      only check that drives an app nobody at Google wrote. If a
      labeling change is in this release, this is the one that catches it
- [ ] `./docs/checks/gestures.sh` and `./docs/checks/zoom.sh` pass on the
      emulator, the simulator and the phone — each gesture asserted against
      the one it could be mistaken for, and the refusals where a platform
      cannot send one (defects 84–86)
- [ ] `./docs/checks/mobium-app.sh` passes on the emulator and the simulator
- [ ] `./docs/checks/crashes.sh` passes on the emulator and the simulator —
      device logs and crash reports, each against a crash caused on purpose

## Third-party drivers

The extension point is a published protocol, so a break here is a break in
somebody else's code that we cannot see.

- [ ] `examples/drivers/mobium-driver-adb` still imports nothing but the Python
      standard library. If a reference driver needs anything from this
      repository, the extension point is not open
- [ ] `docs/decisions/0003`'s method table matches what `internal/mobiumdriver`
      actually sends, field for field
- [ ] Every capability in `mobiumdriver.KnownCapabilities` has a matching `As*`
      helper, and nothing above `internal/mobiumdriver` asserts a capability
      interface directly (`grep 's\.driver\.(mobiumdriver\.'` finds nothing)
- [ ] `mobium doctor` lists a driver placed on PATH, and says none are there
      when none are

## Clients

Each client covers the whole tool surface, and `make ci` proves that by
name. `./docs/checks/clients.sh <serial>` proves it by driving: the same flow
through each client's own API against a booted Android device — reading,
acting, waiting, scrolling, lifecycle, permissions, the device log and crash
reports, and a failure that must arrive as that client's own exception.

- [ ] Python
- [ ] JavaScript
- [ ] Go
- [ ] Java
- [ ] .NET

## The environment itself

- [ ] `mobium doctor` on a healthy machine reports no problems and exits 0
- [ ] With `PATH` stripped of adb it names the fix and exits 1
- [ ] It still runs with `MOBIUM_HOME` set to something over the socket length
      limit — that is the case the first version could not report

## Cross-cutting

- [ ] `mobium daemon stop` followed immediately by any command works (defect 28)
- [ ] One SIGTERM stops an idle daemon and removes its PID file; a stuck call
      can no longer hold it up past half a minute (defect 89 — the case itself
      is `TestShutdownIsBoundedWhenClosingHangs`, since nothing on a device can
      be made to hang on purpose)
- [ ] Quit the emulator mid-session; the next command recovers rather than
      failing until the daemon is restarted (defect 9)
- [ ] `--json` output carries structured fields, not wrapped prose
- [ ] The MCP server answers `tools/list` and one `tools/call` over stdio

## Afterwards

- [ ] `./docs/checks/clean-stop.sh` reports clean

## Pinned agents

- [ ] The [Pinned agents](../.github/workflows/pinned-agents.yml) workflow has
      passed recently, or run `MOBIUM_NETWORK_TESTS=1 go test ./internal/device/ -run Network`
- [ ] If a pin was bumped, both checksums were regenerated from the download
      rather than edited by hand

## Windows

Unsupported until it has run on Windows. The named-pipe transport and the
other known gaps are written and cross-compile, and [WINDOWS.md](WINDOWS.md),
"Doing the work", is what to run on a Windows machine. Windows becomes
supported when those pass, and only then.

- [ ] `make crosscompile` passes (CI does this)
- [ ] On Windows: `go test ./internal/daemon/ ./internal/paths/` passes, and
      the CLI auto-starts a daemon that survives its terminal closing

## The MCP Registry

The release workflow attaches `mobium-<version>.mcpb` — the MCP bundle for
macOS and Linux — and `server.json`, the registry entry pointing at it,
after checking both ([packaging/mcp](../packaging/mcp/README.md)). Publishing
the entry is a person's step, under the name `dev.mobium/mobium`, which the
registry grants to whoever proves control of `mobium.dev`.

Once, before the first publish:

- [ ] An Ed25519 key, made with OpenSSL 3 (macOS's own `openssl` cannot):
      `/opt/homebrew/opt/openssl@3/bin/openssl genpkey -algorithm Ed25519 -out mcp-registry.pem`,
      kept out of every repository
- [ ] Its TXT record on the **apex** of `mobium.dev` — not under a selector —
      as `mcp-publisher` prints it:
      `mobium.dev. IN TXT "v=MCPv1; k=ed25519; p=<public key>"`, beside the
      Maven Central record already there

Each release, after the release workflow has attached the assets:

- [ ] `mcp-publisher` v1.8.1 or later, checked against the release's
      `registry_<version>_checksums.txt`
- [ ] `mcp-publisher login dns --domain mobium.dev --private-key <hex>`
- [ ] Download the release's `server.json` and, beside it,
      `mcp-publisher publish`
- [ ] The entry is there:
      `curl -s "https://registry.modelcontextprotocol.io/v0/servers?search=mobium" | grep -o '"dev.mobium/mobium"[^}]*"version":"[^"]*"'`
      shows the new version. Search matches the part of a name after the
      slash, so searching for `dev.mobium` finds nothing, measured with the
      registry's own `io.modelcontextprotocol/everything`
