# 0001 — WebViews use CDP, not WebDriver BiDi

**Status:** accepted, 2026-09-12
**Supersedes:** the original step-5 design note, which was wrong

## The claim that turned out to be false

The plan for hybrid-app support, written at step 1, said:

> WebView context switching that hands off to Vibium itself — a WebView is
> BiDi/CDP-attachable, so `vibium:element.*` works unchanged inside it.

This was the most attractive part of the whole design. Mobium would drive the
native shell, hand the WebView to Vibium, and every one of Vibium's ~120
element commands would work inside the page for free. No new protocol, no new
element model, and a genuine justification for "built on top of Vibium" rather
than merely "inspired by".

It does not work. The two halves of "BiDi/CDP-attachable" are not
interchangeable, and that sentence quietly assumed they were.

## What was actually measured

Two checks, both cheap, neither done until step 5.

**1. Does Vibium speak CDP?** No. Searching its source for any CDP surface
returns nothing:

```
$ grep -rln "CDP\|chrome-devtools\|/json/list\|webSocketDebuggerUrl" internal/ cmd/
$
```

Vibium connects over WebDriver BiDi and only BiDi — `internal/bidi/connect.go`
opens a WebSocket and issues `session.new`. There is no second transport.

**2. Does an Android WebView speak BiDi?** No. Forwarding a WebView's devtools
socket and asking it directly:

```
$ curl http://localhost:$P/json/version
{
   "Android-Package": "com.google.android.googlequicksearchbox",
   "Browser": "Chrome/124.0.6367.219",
   "Protocol-Version": "1.3",
   ...
}

$ curl -o /dev/null -w "%{http_code}" http://localhost:$P/session
404
```

`Protocol-Version: 1.3` is CDP. `/session` — the BiDi entry point — returns
404. An Android WebView publishes the Chrome DevTools Protocol and nothing
else.

So: Vibium speaks only BiDi, the WebView speaks only CDP, and there is nothing
to hand off.

## Why the assumption was so easy to make

Chrome *the browser* supports both. Chrome exposes CDP on its debugging port
and, in recent versions, BiDi as well — and chromedriver has implemented a
BiDi-to-CDP mapper for years. "Chromium supports BiDi" is true and was the
basis for the claim.

Android **WebView** is a different product with a different surface. It embeds
the same rendering engine but ships no BiDi endpoint and no mapper. The
generalisation from "Chromium" to "anything running Chromium" is where the
error entered.

## Options considered

**A. Run chromedriver as a BiDi↔CDP mapper between Vibium and the WebView.**
This would have preserved the original design exactly. Rejected: it puts a
chromedriver binary on the user's machine, matched to the WebView's Chromium
version. That breaks the single-binary, zero-runtime-dependency property, which
is the first of Mobium's three design commitments.

**B. Implement a BiDi mapper inside Mobium.** Rejected on cost. Chromedriver's
mapper is a substantial piece of software, and Mobium needs roughly two calls'
worth of functionality from it.

**C. Implement the slice of CDP that Mobium actually needs.** Accepted.

## The decision

Mobium implements two CDP methods for WebView contexts:

- `Runtime.evaluate` — run the map script, read the page text
- `Page.getLayoutMetrics` — read the viewport, to convert coordinates

That is the entire protocol surface. `internal/webview/cdp.go` is 231 lines
including the JSON-RPC framing.

**Reads go over CDP; taps stay native.** This is what keeps the surface small.
The page is mapped by evaluating a script in it, the resulting CSS coordinates
are converted to device pixels, and the tap is delivered by the ordinary
UiAutomator2 driver, which already knows how to touch a point. None of CDP's
`Input` domain is used. A web element is acted on exactly as a native one is,
which also means gestures, waits and screenshots need no WebView-specific code
at all.

The coordinate conversion takes the **native WebView element** as ground truth:
its on-screen rectangle gives the origin, and its width against the page's
visual viewport gives the scale. A `devicePixelRatio` read from the page would
be wrong wherever the WebView does not fill the screen, which is most hybrid
apps. (The viewport choice has its own trap; see
[CHALLENGES.md](../CHALLENGES.md#6-the-visual-viewport).)

## The cost avoided

Chromedriver's version must match the device's Chrome: a tool that bundles
one version fails on a device whose Chrome has fallen behind, with `Chrome
version must be >= 76`, and the remedies are to download a matching
chromedriver at run time or keep several. That is option A's cost.

## Consequences

**Good.**
- The zero-dependency property survives. Still one binary.
- The CDP surface is small enough to reason about and to test against a fake
  server.
- Because taps stay native, WebView support inherited gestures and screenshots
  for free.

**Bad.**
- Mobium is not "built on top of Vibium" in the way the plan promised. The
  reuse is architectural — the daemon shape, the tool layer, the `@ref` model,
  the design instincts — not code. That should be said plainly rather than
  implied.
- Mobium now owns a CDP client it has to maintain, small as it is.
- iOS WebViews are not covered *yet*. **This bullet was pessimistic, and is
  now simply wrong**: superseded by
  [0002](0002-ios-webviews-are-reachable.md), and the work was built on
  2026-09-14. The path is an ordinary Unix socket, not a wall. It is still a
  third protocol — CDP for Android, Remote Web Inspector for iOS — but both
  sit under one `webview.Page`, so `Map` and `Text` are shared and only the
  transport differs. `app_context` works on both platforms.

## How to re-check this

Every claim above is a command, which is the point: a decision record whose
premises were never re-examined is indistinguishable from one whose premises
have quietly stopped being true.

**Re-run 2026-09-14**: the first three, which need no device, all still hold —
two CDP methods, no `Input` domain anywhere, and no CDP surface in Vibium. The
fourth needs a WebView forwarded to a port and was not re-run; it is the one
that would actually change the decision, so it is worth doing the next time a
hybrid app is on a device.

```sh
# The protocol surface really is two methods.
grep -n 'call(ctx, "' internal/webview/cdp.go

# Taps stay native: nothing touches CDP's Input domain.
grep -rn "Input\.\|dispatchMouseEvent\|dispatchTouchEvent" internal/webview/

# Vibium still speaks only BiDi.
grep -rln "CDP\|chrome-devtools\|/json/list" ~/Projects/vibium/clicker/internal ~/Projects/vibium/clicker/cmd

# An Android WebView still publishes CDP and not BiDi. With a WebView
# forwarded to $P:
curl -s http://localhost:$P/json/version | grep Protocol-Version   # 1.3 = CDP
curl -o /dev/null -w "%{http_code}" http://localhost:$P/session    # 404 = no BiDi
```

If the last one ever returns 200, this decision is worth revisiting: a WebView
that speaks BiDi would make the original handoff possible after all.

## What this should change about how the plan is read

The rest of the original design held up well under measurement — the driver
seam, the neutral locator vocabulary, the ref model, the daemon. This one item
did not, and it failed in a specific way worth remembering: **it was the only
step whose feasibility rested on a protocol claim nobody had checked.** The
two commands that disproved it took under a minute and could have been run on
day one.

Any remaining plan item that asserts "X is attachable/compatible/supported"
should be treated the same way: verify before building on it.
