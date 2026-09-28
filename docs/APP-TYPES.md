# What kind of app is it, and can Mobium drive it

The first question anyone asks, and the one the README answers only by
implication. The taxonomy is from [*Types of Mobile
Applications*](https://medium.com/@begunova/types-of-mobile-applications-9de9728b11c7),
written by this project's author — so agreement
between it and this repository is one person being consistent rather than
outside corroboration. What follows separates what a device here settled from
what is taken on trust.

Its opening claim is the one that matters most here: **the type dictates how
you find and interact with elements inside the app.** That is not a framing
device. It is the whole of why this document exists, and each
category reaches a different part of Mobium.

| Type | Driven? | How elements are found |
| --- | --- | --- |
| Native | **yes** | the platform accessibility tree, via UiAutomator2 or WebDriverAgent |
| Hybrid | **yes** | native shell through the tree; web content through CDP or Remote Web Inspector |
| Cross-platform | **depends, and not on the vendor's claim** | see below — the answer splits the category in half |
| Mobile web | **not supported, and partly reachable** | a browser is [Vibium's](https://github.com/VibiumDev/vibium) job; what Mobium reaches anyway is below |
| Progressive web app | **read on both, acted on in full only on Android** | the browser's web context; on iOS, taps go through the native tree |

## Native

The core case. Built with the vendor SDKs, shipping as `.apk` or `.ipa`, and
producing real `android.view` and `UIView` instances that appear in the
accessibility tree with labels, ids and bounds. Everything in
[checks/](checks/) drives one.

## Mobile web

**Not a supported app type**, and the taxonomy is why the line is clean
rather than arbitrary. A mobile web app is a web app that happens to be viewed
on a phone; nothing about it is mobile except the screen. Testing it means
testing in a browser, and that is Vibium — same architecture, different
domain, per the motto.

What that line does *not* mean is that Mobium cannot see a browser, and until
2026-09-27 this section said it did. Measured that day on a Pixel 7 emulator
(Android 15, Chrome 124) and an iPhone 17 Pro simulator (iOS 26.5):

- **Chrome on Android is reachable.** It publishes `chrome_devtools_remote`,
  which `app_contexts` has always read alongside the WebView sockets, and it
  reports its page area as an `android.webkit.WebView` node whose bounds are
  the content below the toolbar. `map`, `text`, `eval` and a tap on a link
  all worked. [CHALLENGES 12](CHALLENGES.md) said Chrome renders into a
  compositor view with no host rectangle; on this Chrome that is no longer
  true.
- **Safari on iOS is readable, and its taps are refused**, because its
  WebView spans the chrome and the page cannot see its own inset
  ([CHALLENGES 47](CHALLENGES.md)).

So "not supported" is a statement about scope — no browser management, no
tabs, no cookies, no network, and nothing checked in that drives a browser —
not a wall Mobium puts up. What is reachable is reachable because a browser
is, underneath, the hybrid case below.

## Progressive web apps

A PWA is a web app the user *installs*: it gets a home screen icon, opens
without the browser's toolbar, and a service worker can run it offline. For
automation it is one step closer to native than a web page is, since it
launches from the home screen, and one step short of hybrid, since the
browser owns the process. Squoosh (`squoosh.app`) was installed and driven
on both devices above.

| | Android (Chrome) | iOS (Add to Home Screen, "Open as Web App") |
| --- | --- | --- |
| What runs it | Chrome's `WebappActivity`, in `com.android.chrome` | `com.apple.webapp`, with its own bundle id `com.apple.WebKit.PushBundle.<id>` |
| Launched by id | no — use the home screen icon | no — FrontBoard does not know the bundle id; use the icon |
| The page says | `display-mode: standalone`, a service worker in control | the same, and `navigator.standalone` |
| Its context | `WEBVIEW_com.android.chrome`, named for Chrome | `WEBVIEW_com.apple.SafariViewService`, named for neither the app nor Safari |
| `map`, `text`, `eval` | work | work |
| A tap in the web context | lands | refused: the host is 874 points and the viewport 812, the difference being the status bar |
| A tap from `NATIVE_APP` | works | works — WebKit puts the page's controls in the accessibility tree |

Three things follow.

- **Name a PWA's context by its URL, not its name.** On Android the
  installed app and the Chrome tab it was installed from were both
  `WEBVIEW_com.android.chrome`, told apart only by the `_1` suffix, which is
  list order. Neither context is named for the app in front.
- **On iOS, act from `NATIVE_APP`.** The refusal is the right answer for
  what the web context can compute, and the native tree already has the
  page's buttons and links, labeled. A standalone web app has no browser
  chrome, so its inset is probably just the status bar; that is a hypothesis
  that would need a positive control before any code relied on it.
- **Cookies and web storage work in all of them** — Chrome, an app's own
  WebView, Safari and a home-screen web app's page — through `app_cookies`
  and `app_storage`, once the page is on an http or https origin; an app's
  inline HTML has none, and is refused as that.
- **Installing one is not something Mobium does.** On Android it needs
  Chrome's menu, on iOS the share sheet. On this emulator Chrome pinned a
  legacy web-app shortcut rather than minting a WebAPK, which needs Play
  services; a WebAPK — a real APK, with its own package — is unmeasured.

## Hybrid

A native app with a WebView inside it. The article's point that a hybrid app is
*technically a subcategory of native* — because the outermost component is
native — is exactly the shape of Mobium's context model: `NATIVE_APP` is the
shell, and each page is its own `WEBVIEW_<package>` context.

The more useful observation is the one about **ratio**: some hybrid apps are
almost entirely WebView, others expose a small one inside a native screen. That
spectrum is not a detail, it is the axis along which this tool either works or
refuses.

- **A WebView whose frame equals its content** — an app's own `WKWebView` or
  `android.webkit.WebView`. `NewFrame` computes a scale, and a tap inside the
  page lands. Verified in [checks/mobium-app.sh](checks/mobium-app.sh).
- **A WebView that spans the whole window** — mobile Safari. Its
  `XCUIElementTypeWebView` covers the chrome too, 874 points against a
  714-point viewport, and *the page cannot see its own inset*. Mobium refuses
  the coordinates rather than landing 186 pixels off. [CHALLENGES 47](CHALLENGES.md).

The article names Safari and Chrome as hybrid apps themselves, with native
controls around a WebView. Measuring that here refined it: the native controls
are indeed separate elements, but the WebView *host element* is not the content
area — it spans the window, and no element exposes the content rectangle. That
is the difference between a description that is true and one you can compute
against.

Web content is reached over CDP on Android
([decisions/0001](decisions/0001-cdp-not-webdriver-bidi.md)) and Remote Web
Inspector on iOS ([decisions/0002](decisions/0002-ios-webviews-are-reachable.md)),
and on iOS the app must have opted in with `isInspectable` — which cannot be
forced from outside, and is why the app under test had to be one we control
([decisions/0004](decisions/0004-an-app-under-test-of-our-own.md)).

Cordova and Ionic sit here. Neither has been driven.

## Cross-platform, or native-once-removed

This is where the taxonomy needs splitting for an automation tool, and the
article groups together two things that behave oppositely.

Its claim is that these frameworks embed web technologies inside a native app
and "trigger the creation and modification of purely native components", so the
end result is a completely native app. **That is true of React Native and
Xamarin. It is not true of Flutter**, and the difference decides whether a tool
like this one can see anything at all.

**React Native produces real native views**, and that is measured here rather
than read. MobiumApp is React Native; a `testID` of `password` arrives as
`resource-id="password"` on a genuine `android.widget.EditText`, which is only
possible because a real view exists to carry it. Mobium drives it with no
special support — and the hierarchy it produces was different enough to find
[defect 55](CHALLENGES.md) on the first run, because React Native reports a
*bare* resource-id where every previous app reported a qualified one.

**Flutter does not.** It renders its own widgets to a canvas through its own
engine, so there are no per-widget native views; what reaches the accessibility
tree is a *synthesized* semantics tree over a single surface. This is the same
shape as the Chrome case above, which is why Flutter appears in
[decisions/0003](decisions/0003-drivers-are-processes-not-plugins.md) as
something a third party would add **as a driver process**, rather than as an
app type that happens to work.

**This claim is read, not measured.** No Flutter app has been driven here. It
is stated because the distinction predicts something the vendor-facing
description does not, and because "the end result is a completely native app"
is the kind of true-in-marketing sentence that sends an automation engineer
down a week of wrong diagnosis.

The practical form of the rule: **for automation, ask what is in the
accessibility tree, not what the framework says it produces.** A framework that
creates native views per widget is drivable by anything that reads the tree. A
framework that paints is not, whatever it compiles to.

## What this changes

Nothing about the code. It names a distinction the roadmap already acted on
without writing down: React Native was driven as an ordinary native app and
immediately paid for itself in a defect, while Flutter has always been filed
under the driver protocol rather than under supported platforms. Those two
decisions look inconsistent if cross-platform is one category, and obvious once
it is two.
