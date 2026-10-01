# 0002 — iOS WebViews are reachable, and the path is a Unix socket

**2026-09-13.** Supersedes the pessimism in
[0001](0001-cdp-not-webdriver-bidi.md) about WKWebView, which called it "a
third protocol again" and left it as a wall. Evidence:
[../checks/ios-webview-probe.sh](../checks/ios-webview-probe.sh), which walks
the whole path and passes on an iPhone 17 Pro simulator running iOS 26.5.

## What was believed

That WKWebView needed `com.apple.webinspectord_sim`, which was noted without
anyone checking what that actually is. The phrase "a third protocol after BiDi
and CDP" set the expectation of something like the BiDi problem — a protocol
mismatch with no bridge — and the item sat unstarted because of it.

## What is actually true

It is a **Unix domain socket**, dialable with `net.Dial("unix", …)`. No XPC,
no Mach ports, no cgo, no `ios_webkit_debug_proxy`, no third-party binary.

The host's `com.apple.webinspectord` does expose Mach endpoints, which is the
misleading part and probably where the pessimism came from. The *simulator's*
copy of the same service also publishes a socket, and that is the one to use:

```
$ xcrun simctl spawn <udid> launchctl print system/com.apple.webinspectord
    ...
    RWI_LISTEN_SOCKET => /private/var/tmp/com.apple.launchd.XXXX/com.apple.webinspectord_sim.socket
```

The path changes per boot, so it is discovered, not hardcoded.

## The whole path, each step verified

| Step | Message | Answer |
| --- | --- | --- |
| 1 | dial the socket | connects |
| 2 | `_rpc_reportIdentifier:` | `_rpc_reportCurrentState:`, then `_rpc_reportConnectedApplicationList:` |
| 3 | `_rpc_forwardGetListing:` | `_rpc_applicationSentListing:` — page id, title, URL |
| 4 | `_rpc_forwardSocketSetup:` | `Target.targetCreated` |
| 5 | `_rpc_forwardSocketData:` | `_rpc_applicationSentData:` |

Framing is four bytes of big-endian length followed by a **binary property
list**. Inside `WIRSocketDataKey` the payload is **JSON** — the same shape
Mobium already speaks for Android.

Step 3 gives exactly what `app_contexts` needs on Android today: an id, a
title and a URL per page. Two notes from running it. The `WebContent` process
appears in the application list alongside Safari and answers with an *empty*
listing, so the caller has to keep waiting rather than conclude there are no
pages. And Web Inspector must be on:

```sh
xcrun simctl spawn <udid> defaults write com.apple.mobilesafari \
    WebKitDeveloperExtrasEnabledPreferenceKey -bool true
```

## The one real gotcha

**WebKit is multi-target.** Sending `Runtime.evaluate` down the data channel
answers:

```json
{"error":{"code":-32601,"message":"'Runtime' domain was not found"}}
```

which reads like the domain is missing and is not. Since iOS 12.2, commands
must be wrapped in `Target.sendMessageToTarget` with a `targetId` from
`Target.targetCreated`, and replies arrive wrapped in
`Target.dispatchMessageFromTarget`. Wrapped, the same call works:

```
Runtime.evaluate -> {"result":{"type":"string","value":"Example Domain @ 402x714"}}
```

## Built, 2026-09-14

Done and verified against an iPhone 17 Pro simulator:
[../checks/ios-webview.sh](../checks/ios-webview.sh) drives discovery, context
switching, reading and the refusal below, and `internal/webview` now carries
`Inspector`, `IOSSession` and the plist framing. `app_contexts`, `app_context`,
`app_text` and `app_map` work on iOS exactly as they do on Android, because
everything above `webview.Page` is shared — only the transport differs.

Three things the protocol does that CDP does not, each found by running it and
each now recorded as a defect:

- **Only the first connection is prompt.** Later ones wait 10.2 seconds before
  the first byte, so the connection is held for the life of the session
  ([CHALLENGES 45](../CHALLENGES.md)).
- **The application list, and each page's target, are announced once per
  connection** — and a page's target belongs to one debugger at a time, so a
  daemon left switched into a page blocks Safari's own inspector
  ([46](../CHALLENGES.md)).
- **Mobile Safari's WebView element is the whole window, chrome included**,
  and the page cannot see its own inset, so coordinates inside Safari are
  refused rather than guessed ([47](../CHALLENGES.md)). An app's own WKWebView
  is unaffected, and that is the case this is for.

The remaining gap was honest and worth stating: **no genuine hybrid iOS app had
been driven**, only Safari. **Closed on 2026-09-17** by
[0004](0004-an-app-under-test-of-our-own.md), which also found why it could not
have been closed with an app from the store: on iOS 16.4+ the *app* must set
`isInspectable` or its WebView publishes no target at all.

## How to re-check this

```sh
# The socket is still where launchd says, and still a socket.
xcrun simctl spawn booted launchctl print system/com.apple.webinspectord | grep RWI_LISTEN

# The whole path, with no mobium code in it.
docs/checks/ios-webview-probe.sh

# And with mobium driving it.
docs/checks/ios-webview.sh
```

The probe is the important one: it verifies a **platform** assumption rather
than mobium's behavior, so if it fails the plan needs revisiting rather than
the code.

## What this meant for the work

The protocol question was settled; what was left was Go.

- ~~**A binary plist codec.**~~ **Done** — `internal/plist`, covering
  dictionaries, strings, integers, reals, booleans, arrays and data in both
  directions, and refusing anything else rather than dropping it silently.

  It is checked three ways, in ascending order of how much they prove.
  Round-tripping through itself shows only self-consistency, and a codec can
  be self-consistently wrong. Agreeing with **Python's `plistlib`** in both
  directions shows it matches somebody else's reading of the format — worth
  having, and already installed anywhere `simctl` is. But the program that
  decides whether these bytes are a property list is Apple's `webinspectord`,
  so a test drives it: on a booted simulator it completes the handshake, lists
  Safari's pages, opens the data channel and evaluates `1 + 1`, getting `2`
  back. It skips with an actionable message when no simulator is there.
- **The RWI handshake and target wrapping**, which the probe script already
  spells out step by step.
- **Reuse the rest.** JSON exists. The context model, `@ref` mapping and
  native-tap-after-CDP-read design all come straight from Android.

One pleasant surprise: Android needed `Page.getLayoutMetrics` and produced
[defect 6](../CHALLENGES.md), the visual-versus-layout viewport bug that put
every tap at a quarter of the right offset. Here `innerWidth`/`innerHeight`
through `Runtime.evaluate` gives the visual viewport directly — as the probe's
`402x714` shows — so the iOS side can avoid that trap rather than repeat it.

## Where it stands now

The codec is in. What is left is the RWI client itself — the handshake, the
listing, the target wrapping — and wiring it into `app_contexts` and
`app_context` so an iOS WebView becomes a context like an Android one. The
protocol has no unknowns left in it; the probe script and the codec's own
simulator test between them spell out every message.

## Why this is written down rather than just done

Because the previous belief was wrong in a way that cost the item months of
sitting still, and the correction is worth more than the implementation would
have been this session. The probe is checked in and runnable so the next
person starts from a working example rather than this document's prose — and
so that if Apple changes any of it, something fails loudly.

## A real iPhone, 2026-09-23

The same protocol, reached a different way. Verified on an iPhone 15 Plus on
iOS 26.6.2: MobiumApp's WebViews, two at once, and Safari's pages, through
[../checks/mobium-app.sh](../checks/mobium-app.sh).

**The route is usbmuxd and lockdown** — the one ios_webkit_debug_proxy
takes, and the "remote debugger proxy" people mean when
they say Safari on a device needs one:

| Step | What | Measured |
| --- | --- | --- |
| 1 | `/var/run/usbmuxd`: list devices, read the pairing record | world-writable, no root; the record the Mac made at "Trust" |
| 2 | usbmuxd `Connect` to device port 62078 | lockdown, `QueryType` answers `com.apple.mobile.lockdown` |
| 3 | `StartSession` with the record's HostID and SystemBUID | `EnableSessionSSL: true`; TLS with the record's host certificate |
| 4 | `StartService com.apple.webinspector` | a port, and `EnableServiceSSL: true` |
| 5 | usbmuxd `Connect` to that port, then TLS | `_rpc_reportIdentifier:` answers `_rpc_reportCurrentState:` |

From step 5 on it is this document's protocol, message for message, so
`internal/webview` is unchanged except that an `inspector` can start from any
stream. usbmuxd and lockdown accept a binary plist and always answer in XML,
which is the one thing `internal/plist` had to learn.

**The iOS 17+ route was tried first and ruled out.** The CoreDevice tunnel
Mobium already uses for WebDriverAgent carries the same service as
`com.apple.webinspector.shim.remote`, and after a one-message check-in
(`RSDCheckin`, answered by `RSDCheckin` then `StartService`) it speaks the same
protocol — measured. But:

- its port is assigned per tunnel connection and changed between two readings
  a few minutes apart;
- the service directory that would name it answered nothing on any port the
  tunnel had open, and `remotectl netcat <device>
  com.apple.webinspector.shim.remote` is refused ("Unable to connect") —
  macOS's own tool knows the service and will not open it for us;
- so finding the port means checking in with every service on the phone.
  That was done once, to measure it, and one service answered
  `ShowDialog: true`. Doing that on every connection, on somebody's phone, is
  not a transport.

The lockdown route needs the phone connected by cable, which the tunnel route
would not have. That is the price.
