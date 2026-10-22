# A proxy between the app and its servers

What it would take for Mobium to route a device's traffic through a
debugging proxy, so a test can see, shape and change what an app sends and
receives — and to drive Charles, as an opt-in, the way a tester drives it by
hand: record a session, throttle, switch Map Local and Rewrite rules on and
off. What was measured, and the plan, in the order it should be built.

**Status on 2026-10-22.** Step 1 is measured (below), and step 2's
setting is built: `mobium network --proxy host:port` and `--no-proxy`, read
back, put back at the end of the session, a loopback proxy reached through
`adb reverse`, held by `docs/checks/proxy.sh` on the emulator. Left of
step 2: the certificate, each app's trust read from its APK, and a run on
the Pixel. `mobium network` sets airplane mode on any Android device and
shapes latency and bandwidth on an emulator, and refuses iOS; the proxy is
a setting of that tool, not a tool of its own.

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

- **The certificate goes into the user store through Settings, and only
  there.** `charles ssl export` wrote Charles's root; pushed to Downloads and
  opened with `android.credentials.INSTALL`, Android answered "Install CA
  certificates in Settings". Settings' search for "CA certificate", Install
  anyway, the file — all driven by Mobium — put it in the user store, named
  by its subject hash (`477e5098.0`, which `openssl x509 -subject_hash_old`
  gives for Charles's root). Android 15 asked for no screen lock. It comes
  out the same way: Trusted credentials, User, the certificate, Uninstall.
- **Each app's own configuration decides, not whether it is a release
  build.** With the certificate in: Chrome's HTTPS page loaded and was
  decrypted; F-Droid's refresh was decrypted, five requests to `f-droid.org`;
  Wikipedia showed "An error occurred" and left no entry. Their
  `networkSecurityConfig`, read from the APKs with `aapt2`: F-Droid's trust
  anchors are `system` and `user`; Wikipedia's are two certificates it ships
  and `system`. So whether a test will see an app's HTTPS is answerable from
  the APK before it runs, and the result can say so. **Control:** with the
  certificate uninstalled, Chrome was refused again
  (`NET::ERR_CERT_AUTHORITY_INVALID`) and F-Droid's HTTPS left no entry.
- **MobiumApp's Network Demo** (mobium-app, branch `network-demo`): a native
  `fetch` (OkHttp) and a WebView page, both to an address that defaults to
  a server the check runs on the Mac — bound to `127.0.0.1`, reached
  through `adb reverse` — each saying what came back, and a count of
  presses. Its network security configuration permits plain HTTP to the
  Mac's addresses only and trusts user certificates. With no proxy both
  reached the server, `200` and "loaded", and the server logged OkHttp's and
  the WebView's user agents.
- **A loopback address goes round the proxy for a WebView, not for OkHttp.**
  Through Charles, the native fetch to `127.0.0.1:8765` was in the session
  and the WebView's page was not — it reached the server directly. By the
  name `localhost.charlesproxy.com`, which resolves to `127.0.0.1`, both
  went through Charles, the page's favicon too. A test that wants a WebView
  behind a proxy needs a name, and the app's configuration has to permit
  it: a host it did not list was refused as cleartext by both, "not
  permitted by network security policy" and `ERR_CLEARTEXT_NOT_PERMITTED`.
- **Charles decrypts the ports it is told to, and does not check the server
  it forwards to.** HTTPS to the demo's port 8766, with Charles's root in the
  user store, was a `CONNECT` tunnel, and the app refused the server's own
  self-signed certificate — the configuration named port 443 only. With
  `localhost.charlesproxy.com:8766` added to `sslLocations` both requests
  were decrypted, bodies and all, and Charles accepted the self-signed
  server. Without the root installed both refused Charles's certificate
  (`Trust anchor for certification path not found`, "The certificate
  authority is not trusted") and nothing reached the server.
- **Deleting `http_proxy` does not take the proxy off.** Setting it writes
  `global_http_proxy_host` and `global_http_proxy_port` as well, and those
  stay in force: after `settings delete global http_proxy` read back `null`,
  OkHttp still went to `127.0.0.1:8888` and failed to connect. Putting
  `http_proxy` to `:0` and deleting all of `http_proxy`, its host, port,
  exclusion list and PAC URL cleared it. The restore reads all of them
  back, not the one written.

- **On the simulator** (iPhone 17 Pro, iOS 26.5, the Network Demo built
  for iOS): with no proxy both requests reached the Mac's server — App
  Transport Security's `NSAllowsLocalNetworking` lets plain HTTP to
  `127.0.0.1` through — from CFNetwork and from WebKit.
- **A reverse proxy needs no proxy setting at all.** Charles's Reverse
  Proxies tool (`reverseProxies` with `active`, and a `reverseProxy` of
  `enabled`, `sourcePort`, `destHost`, `destPort`, `preserveHostHeader`,
  `rewriteRedirects`) listened on 8865 and forwarded to the server on 8765:
  the demo pointed at 8865 had both requests recorded, the WebView's too,
  with the Mac's own proxy off throughout. It serves an app whose address a
  test sets — the demo — not somebody's app, which still needs the device's
  proxy; for the simulator that is the whole Mac's, so this is the way to put
  the simulator behind Charles without touching the Mac.
- **`simctl keychain add-root-cert` trusts a root at once, and `simctl
  keychain reset` does not take it away.** The demo's HTTPS to a self-signed
  server failed ("A TLS error caused the secure connection to fail") until
  the server's certificate was added, then succeeded without a relaunch. A
  keychain reset, a relaunch and a reboot all left it trusted: the root is a
  row of trustd's `TrustStore.sqlite3` (`tsettings`, keyed by the
  certificate's SHA-256), which the reset leaves alone. Deleting that row
  with the simulator shut down restored the refusal. That is the undo.

- **On the real iPhone, over its USB tunnel.** The CoreDevice tunnel Mobium
  reaches WebDriverAgent through gives the phone and the Mac an address each
  (`fda9:7046:6460::1` and `::2` on `utun6`); a server bound to the Mac's
  end is reachable from the phone and from nothing on the Wi-Fi. The Network
  Demo on the iPhone 15 Plus, iOS 27.0.1, fetched and loaded from
  `http://[fda9:7046:6460::2]:8765/` — App Transport Security's local
  networking took the IPv6 literal — and the server logged the phone's end
  as the client. Nothing on the phone was changed.
- **Charles refuses the phone by its Access Control list, and says so only
  in its log.** Through the reverse proxy on the tunnel, both requests
  failed "The network connection was lost", and Charles logged "Denying
  access from address not on ACL" for the phone's address: headless, there
  is no prompt to allow it. An `accessControlConfiguration` entry for the
  address did not take — its format is not known. A forwarder on the Mac,
  from the tunnel's address to `127.0.0.1`, which Charles allows, put both
  requests in Charles's session, CFNetwork's and WebKit's.
- **MobiumApp did not launch on iOS 27 until it adopted the scene life
  cycle** (mobium-app, branch `ios27-scene-lifecycle`): built by Xcode 27.1
  it trapped in `_UIApplicationEvaluateRuntimeIssueForNoSceneLifecycleAdoption`
  at every launch, Network Demo or not.

Not measured: the system store (the image is `userdebug`, so `adb root`
works; Android 14 and later keep it in a module); a Flutter app's requests,
for which MobiumApp's `flutter/` demo needs the same screen;
throttling, Map Local and Rewrite switched by the Web Interface with rules
in place.

## The plan

### 1. Measure the shape on the emulator — measured, above

The certificate and MobiumApp's Network Demo measured too. Left of it: a
Flutter request missing from the session.

### 2. `--proxy` on Android

**The setting, done 2026-10-22** (`docs/guides/network.md`, section 5): set,
read back from the keys in force, taken off with `:0` and every key
deleted, put back at the session's end; a loopback proxy's port reversed
before the device points at it, and only Mobium's reverses removed. A
reverse that was there before is left — a test caught the first version
taking it as its own. `proxy.sh` fetches a port nothing on the device
listens on, so the body can only be the proxy's: failed with none, the
proxy's own body with it, failed again after. Not yet on the Pixel.

The setting, its read-back and its restore, on the emulator and the Pixel,
with `adb reverse`; the certificate into the user store through Settings,
and out again; and each app's trust read from its APK's network security
configuration, so a result says in advance whether the app's HTTPS can be
seen. A check that drives MobiumApp through any proxy —
mitmproxy's, since it needs no license, or a tiny one of the check's own —
and asserts the requests arrived.

### 3. The simulator

The certificate through `simctl keychain add-root-cert`, and out again by
its row in trustd's trust store, measured above. For an app whose address
the test sets, a reverse proxy, measured above; for any other app, the
Mac's proxy, which is the whole Mac's — set only when the caller asks for
it, the result saying so, and nothing left set.

### 4. Charles as an opt-in

`--proxy charles`: headless start with a configuration Mobium writes —
the macOS proxy off, the Web Interface on, Auto Save into the test's report
folder — or an existing Charles found; recording, throttling and tool
switches through the Web Interface, each read back from its status line; the
session converted to HAR beside the test's report. Listening on every
interface is said, or bound where Charles allows.

### 5. A real iPhone

For an app whose address the test sets, the USB tunnel, measured above:
a server or a forwarder on the Mac's end, and nothing on the phone or the
Wi-Fi. For any other app, the Wi-Fi proxy through Settings, put back and
read back — and since the phone then connects from the network, Charles's
Access Control list has to admit it, or every request is "lost" with the
reason only in Charles's log; the certificate's one manual step written
down, and refused clearly when it has not been done.

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
