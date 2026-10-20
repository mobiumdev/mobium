# A proxy between the app and its servers

What it would take for Mobium to route a device's traffic through a
debugging proxy, so a test can see, shape and change what an app sends and
receives — and to drive Charles, as an opt-in, the way a tester drives it by
hand: record a session, throttle, switch Map Local and Rewrite rules on and
off. What was measured, and the plan, in the order it should be built.

**Status on 2026-10-20.** Nothing is built; step 1 is measured (below). `mobium network` sets airplane
mode on any Android device and shapes latency and bandwidth on an emulator,
and refuses iOS; a proxy is the next setting of that tool, not a tool of its
own.

## The shape

Two layers, the second opt-in.

- **A proxy, any proxy.** `mobium network --proxy <host:port>` points the
  device at a proxy and reads it back; `--no-proxy` takes it off; the end of
  the session puts back what was there, as every network setting is put
  back. A proxy's root certificate is installed where the device allows it,
  so HTTPS can be decrypted. This works with Charles, Fiddler, mitmproxy or
  anything else that listens on a port, and knows nothing of any of them.
- **Charles.** `--proxy charles` does the above with Charles's port and
  certificate (`charles ssl export -`), and drives Charles through what it
  documents for automation: `-headless` with a `-config` file, and its Web
  Interface at `http://control.charles/`, reached through the proxy itself,
  which starts and stops recording, clears and exports the session, switches
  throttling presets, and switches tools on and off. Charles is never
  installed or required by Mobium: no runtime dependencies is a load-bearing
  property, and a missing Charles is refused, naming it.

What a test gets from the second layer:

| Tester's workflow | In a test |
| --- | --- |
| Record, then File > Save Session | recording starts with the step and the session is saved beside the test's report, as a trace and a WAV are |
| Throttling presets | a preset on, by name, read back — on iOS and phones too, where `network` refuses shaping today, but only for traffic the proxy carries, which the result says |
| Map Local, Rewrite | rules written once in a Charles configuration, whole sets switched on and off by a step: the "thirty places", "the server answers 500" and feature-flag cases, repeatable |
| Breakpoints | left manual: they wait for a person to edit a response |

## Pointing each device at it

| Device | Proxy | Its certificate |
| --- | --- | --- |
| Android emulator | `settings put global http_proxy`, read back, and `adb reverse tcp:<port> tcp:<port>` so `127.0.0.1` on the device is the Mac | the user store, trusted only by apps that opt in; the system store needs a writable system image |
| Android phone, TV | the same, over USB or Wi-Fi adb, no root | the user store only |
| iOS simulator | the Mac's own network: its system proxy | `xcrun simctl keychain <udid> add-root-cert` |
| Real iPhone | the Wi-Fi network's HTTP proxy, in Settings, which Mobium drives on a phone as it does the accessibility switches | a profile installed and trusted by hand, once |

`adb reverse` is what removes most of the manual setup's traps: no address
on the LAN to find, no Access Control entry to keep, no firewall or network
profile in the way. Android's proxy is a device-wide setting, so the restore
is read back, as `accessibility` reads its restore back.

## What a proxy cannot see

Every result names which of these applies rather than reporting an empty
session as a quiet app.

- **Flutter ignores the system proxy.** Dart's HTTP client reads no proxy
  setting unless the app gives it one, so a Flutter app's traffic goes
  around the proxy.
- **Android apps trust only system certificates since Android 7**, unless
  their network security configuration opts in to user ones — debug builds
  usually, release builds rarely. Their HTTPS stays encrypted. Android 14
  moved the system store into a module, which a writable system image alone
  no longer reaches.
- **Certificate pinning** refuses the proxy's certificate whatever store it
  is in. That is the app's answer, to be reported, not retried.
- React Native, native networking and WebViews follow the system proxy.

## What was measured — 2026-10-20

Charles 5.2.1 headless on the Mac, with a configuration of its own, and the
Android 15 emulator.

- **The configuration file can be written, not clicked.** Charles writes only
  what differs from its defaults, so its element names were read from its
  classes: `proxyConfiguration` holds `port`, `decryptSSL`, `sslLocations`
  (`locationPatterns` of `locationMatch`, each a `location` and `enabled`)
  and `macOSXConfiguration` with `enableAtStartup`;
  `remoteControlConfiguration` — the Web Interface — holds `enabled` and
  `allowAnonymous`; the Auto Save tool holds `enableOnStartup`, `savePath`
  and `savePeriod`, in minutes. Charles kept every one through its own save
  at quit.
- **Headless, it changes the Mac unless told not to.** Its first start, with
  defaults and nothing to answer its prompts, installed its privileged macOS
  proxy helper (`/Library/PrivilegedHelperTools/com.xk72.charles.ProxyHelper`
  and its launch daemon) and logged restoring the Mac's proxy settings at
  quit. With `enableAtStartup` false the Mac's proxy stayed off throughout,
  read back. It listens on every interface, not only `127.0.0.1`.
- **The Web Interface's pages, from its HTML:** `/recording/start` and
  `stop`; `/session/clear`, `export-xml`, `export-json`, `export-trace`,
  `export-csv`, `export-har` and `download`; `/tools/<tool>/enable` and
  `disable` for breakpoints, no-caching, block-cookies, map-remote,
  map-local, rewrite, dns-spoofing, block-list, allow-list, auto-save and
  client-process, each page saying "Status: Enabled" or "Disabled"; and
  `/throttling/activate?preset=<name>` and `deactivate`, with eleven presets
  from "56 kbps Modem" to "4G" and its status read the same way.
- **Headless, the Web Interface exports nothing.** Every export, and the
  session download, answered with the session page's own HTML, with a
  request just recorded from the Mac. What works is Auto Save, which writes a
  `.chlz` every period and starts a new one, and `charles convert x.chlz
  x.har`, which ran beside the headless instance in 0.4 seconds. So a
  session is saved by period, and a test that stops recording waits for the
  next save — a minute at the shortest.
- **The emulator through `adb reverse`.** `settings put global http_proxy
  127.0.0.1:8888` and `adb reverse tcp:8888 tcp:8888`: Chrome's
  `http://example.com/?mobium=http2` was in the session, 200. Taken off —
  the setting deleted, read back `null`, the reversal removed — the page
  loaded directly.
- **A request refused for the certificate is not in the session.** Chrome's
  `https://example.com/` reached Charles and showed `NET::ERR_CERT_AUTHORITY_INVALID`,
  since the emulator does not trust Charles's root, and no entry for it was
  in the HAR; an HTTPS request from the Mac in the same minute, with Charles
  decrypting it, was. An empty session is not a quiet app: the result has to
  say so from the device's side.

Not measured: the certificate in the emulator's user store and which apps
then trust it; MobiumApp's own requests and a Flutter screen's; throttling,
Map Local and Rewrite switched by the Web Interface with rules in place.

## The plan

### 1. Measure the shape on the emulator — measured, above

Left of it: the certificate in the user store, MobiumApp's own requests,
and a Flutter screen's request missing from the session.

### 2. `--proxy` on Android

The setting, its read-back and its restore, on the emulator and the Pixel,
with `adb reverse`. A check that drives MobiumApp through any proxy —
mitmproxy's, since it needs no license, or a tiny one of the check's own —
and asserts the requests arrived.

### 3. The simulator

The Mac's proxy for the simulator, set and restored, and the certificate
through `simctl keychain`. The Mac's proxy is the whole Mac's: the result
says so, and nothing is left set.

### 4. Charles as an opt-in

`--proxy charles`: headless start with a configuration Mobium writes —
the macOS proxy off, the Web Interface on, Auto Save into the test's report
folder — or an existing Charles found; recording, throttling and tool
switches through the Web Interface, each read back from its status line; the
session converted to HAR beside the test's report. Listening on every
interface is said, or bound where Charles allows.

### 5. A real iPhone

The Wi-Fi proxy through Settings, put back and read back; the certificate's
one manual step written down, and refused clearly when it has not been done.

### 6. Fiddler and mitmproxy

Both through the first layer only. Fiddler Classic is free, Windows-only and
scriptable — the Windows host's natural proxy; Fiddler Everywhere is
cross-platform and by subscription, with no documented way to run without
its window. mitmproxy runs without one by design and writes HAR, which makes
it the one to reach for in CI; a layer of its own only if a test needs what
the first layer cannot give.

## Rules for this work

- **A phone is somebody's.** Its proxy is put back and read back, and a
  certificate installed for a test is named so it can be found and removed.
- **Say which traffic was seen.** A session is what passed through the
  proxy, not what the app did; a Flutter app's empty session is not a quiet
  app.
- **Never save a password through the proxy.** A recorded session carries
  whatever the app sent, so a test that types a secret records only from
  after it, as the manual notes do.
