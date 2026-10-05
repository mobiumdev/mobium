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
| Progressive web app | **read on both; tapped from the native tree on iOS, and in the page on Android — on an emulator only while Chrome reports its WebView** | the browser's web context; on iOS, taps go through the native tree |

## What has been driven

Every type has been driven here; not every framework within a type has.
"Checked in" means a script in [checks/](checks/) holds it; "measured" means
it was driven once, with the result written down below, and nothing re-runs it.

| Type | Driven | Where |
| --- | --- | --- |
| Native | Android: Settings, Calculator, Clock, Wikipedia, F-Droid, Aegis, and Seal (Jetpack Compose). iOS: Settings, Wikipedia and NetNewsWire from the App Store | checked in — most of [checks/](checks/); `third-party-app.sh`, `compose-app.sh`, `netnewswire-ios.sh` for the apps nobody at Google or Apple wrote |
| Hybrid | Wikipedia's articles, and MobiumApp's WebView screens, on both platforms and on real phones | checked in — `third-party-app.sh`, `mobium-app.sh`, `web-type.sh`, `web-actionability.sh`, `web-storage.sh` |
| Mobile web | Safari on iOS; Chrome on Android, emulator and the Pixel 8 Pro | checked in — `chrome.sh` (read, a tap counted by the page, a link followed), `ios-webview.sh`, `orientation.sh`, `shake.sh` |
| Progressive web app | Squoosh, installed to the home screen on both platforms, and as a WebAPK on the Pixel 8 Pro; OYO Lite, a Trusted Web Activity from the Play Store, on the Pixel 8 Pro | checked in — `pwa.sh`: installed if absent, launched, standalone, and a tap counted by the page — or, on an emulator's shortcut, refused with the reason; `twa.sh`: a Play Store PWA launched, tapped, and backed through |
| Cross-platform | React Native: MobiumApp, on both platforms and on real phones | checked in — `mobium-app.sh`, `login.sh`, `otp.sh`, `dialogs.sh` and every other MobiumApp check |
| | Flutter: MobiumApp's `flutter/` demo, on the Pixel 7 AVD, the iPhone 17 Pro simulator, the Pixel 8 Pro and the iPhone 15 Plus | checked in — `flutter.sh`: driven through the semantics tree Flutter publishes, with no driver ([below](#cross-platform-or-native-once-removed)) |
| | Xamarin/.NET MAUI | **never driven** |
| Hybrid frameworks | Cordova, Ionic | **never driven**; a WebView inside them is the hybrid case above |
| App Clip (iOS) | MobiumApp's clip demo on the iPhone 17 Pro simulator; AdvantageScope XR's published clip on the iPhone 15 Plus | measured 2026-10-01: opened from its link, its card read and its Open tapped, then driven like any native app — [below](#try-before-you-install) |

## Native

The core case. Built with the vendor SDKs, shipping as `.apk` or `.ipa`, and
producing real `android.view` and `UIView` instances that appear in the
accessibility tree with labels, ids and bounds. Everything in
[checks/](checks/) drives one.

Jetpack Compose is native too, but its tree has another shape. Compose
draws its own views and hands Android a semantics tree, so most nodes are
plain `android.view.View`. A button is a clickable node with its words on a
child, a switch row carries its state on the row, and a dialog is a window
the platform's alert endpoint does not recognize. The first Compose app
driven here, Seal, broke three things on its first screens (CHALLENGES
176–178); `checks/compose-app.sh` holds them.

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
- **Safari on iOS is readable, and its taps are placed from its text.** Its
  WebView spans the chrome and the page cannot see its own inset
  ([CHALLENGES 47](CHALLENGES.md)), so where the page starts is read from
  text the page and the WebView both report, two runs of it agreeing, and a
  tap is refused only when they do not (CHALLENGES 248). Measured on a
  simulator; not yet on a phone.

So "not supported" is a statement about scope — no browser management and
no tabs — not a wall Mobium puts up. A browser's page is driven by checks all
the same: Safari's in `ios-webview.sh` and `orientation.sh`, Chrome's in
`chrome.sh` and `shake.sh`. A web context's cookies (`app_cookies`) and the device's network
conditions (`app_network`) are reachable, but they belong to the page and the
device, not to managing a browser. What is reachable is reachable because a browser
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
| Launched by id | a WebAPK, yes: `app_launch org.chromium.webapk.<hash>`, which says it is a web app ([CHALLENGES 202](CHALLENGES.md)); a launcher shortcut, no — use its icon | no — FrontBoard does not know the bundle id; use the icon |
| The page says | `display-mode: standalone`, a service worker registered and active | the same, and `navigator.standalone` |
| Its context | `WEBVIEW_com.android.chrome`, named for Chrome | `WEBVIEW_com.apple.SafariViewService`, named for neither the app nor Safari |
| `map`, `text`, `eval` | work | work |
| A tap in the web context | lands — on a WebAPK on the Pixel 8 Pro, Chrome running or not. On an emulator's shortcut opened while Chrome is running, Chrome stops reporting the WebView within about five seconds and the tap is refused ([CHALLENGES 200](CHALLENGES.md)) | refused: the host is 874 points and the viewport 812, the difference being the status bar |
| A tap from `NATIVE_APP` | works while Chrome reports the page; once it stops, its tree is empty there too | works — WebKit puts the page's controls in the accessibility tree |

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
  Chrome's menu, on iOS the share sheet; `checks/pwa.sh` drives both. On this
  emulator Chrome pinned a legacy web-app shortcut rather than minting a
  WebAPK, which needs Play services; a WebAPK — a real APK, with its own
  package — is unmeasured. Those shortcuts are fragile: after a cold boot,
  and once after force-stopping Chrome, the launcher showed none of them
  while Chrome still listed six, and Chrome then offered only a plain
  shortcut, which opens a tab.
- **A PWA from the Play Store is a Trusted Web Activity** — a package of
  its own that opens the site in Chrome, full screen, in a custom tab
  (Bubblewrap and PWABuilder make them). OYO Lite, `com.oyo.consumerlite`,
  measured on the Pixel 8 Pro on 2026-10-01: its task is rooted in Google's
  `androidbrowserhelper.trusted.LauncherActivity` with Chrome's
  `CustomTabActivity` on top, its page is a `WEBVIEW_com.android.chrome`
  context with the site's URL, it runs standalone, and taps land. Like a
  WebAPK, its windows are Chrome's, so `app_launch` and back name it by its
  task ([CHALLENGES 205](CHALLENGES.md)). Back goes through the page's
  history first, then leaves — but only through entries a user's gesture
  made: a navigation run with `app_eval` is skipped by back, as Chrome skips
  any made without one. Tap the link instead.
- **On a phone with Play services, a PWA is a WebAPK — a real package.**
  On the Pixel 8 Pro, Chrome 154 offered "Install and create shortcut", then
  a choice of web app or shortcut, and minted
  `org.chromium.webapk.<hash>` in about twelve seconds. Its task is its own,
  Chrome's `SameTaskWebApkActivity` shows its pages, and it uninstalls like
  any app. Its WebView stayed in the tree and taps landed.
- **On an emulator's shortcut, when the tap matters, launch the web app with
  Chrome not running.** Opened from its icon while Chrome was running,
  Chrome stopped reporting the app's WebView within about five seconds,
  three launches out of three; opened after Chrome's process had ended, it
  kept reporting it, two out of two. Mobium refuses the tap it can no longer
  place and says why.

## Try before you install

Both stores once let a user run part of an app without installing it. Only
Apple's still does.

- **Android: Google Play Instant**, "Try now" on a Play listing, ran a
  slice of an app of up to 15 MB from a link. Google shut it down in
  December 2025 for low use. Play's remaining "Try now" streams a premium
  game for ten minutes from Google's servers; nothing runs on the phone, so
  there is nothing on it to drive.
- **iOS: App Clips.** A small native part of an app — UIKit or SwiftUI,
  not web content — opened from an App Clip Code, an NFC tag, a QR code, a
  link in Safari, Messages or Maps, or Apple's default link
  `https://appclip.apple.com/id?p=<bundle-id>`. The system shows a card
  first, then runs the clip full screen, and installing the full app
  replaces it. Up to 15 MB from iOS 16, and 50 MB from iOS 17 for a clip
  opened only from links.

Measured on 2026-10-01, with a clip of our own: `appclip/` in MobiumApp's
repository, a SwiftUI clip with a tap counter, a pushed screen and the URL
that opened it, embedded in a minimal parent app.

| On the iPhone 17 Pro simulator, iOS 26.5 | |
| --- | --- |
| The clip installed alone, as iOS delivers one | `simctl install` of the clip's `.app` |
| `apps` | lists it, `dev.mobium.clipdemo.Clip` |
| `launch`, `current` | starts it by bundle id; names the clip, not its parent |
| `map`, `text`, a tap | read it; a tap took the counter from 0 to 1 |
| Back through its navigation stack | the bar's button, and `press back --gesture`, which said the bar now reads "Clip Home" |
| Opened from a link | **not reached**: the simulator's Settings > Developer has no Local Experiences, and `simctl launch` ignored Xcode's `_XCAppClipURL` — the clip said "Invoked by: none" |

So **a clip, once running, is an ordinary native app to Mobium**, on the
simulator.

On the iPhone 15 Plus, iOS 26.6.2, the same day, with clips already
published in the App Store, opened by their default links — found in their
own projects' public source:

| | |
| --- | --- |
| `mobium open https://appclip.apple.com/id?p=org.littletonrobotics.advantagescopexr.Clip` | SpringBoard put up the App Clip card |
| The card in WebDriverAgent's tree | an `Alert` holding `AppClipCard`: the clip's name and description, a hero image, `OpenButton` (labeled Open), `Close`, and "Powered by AdvantageScope XR, Age Rating 4+, View on the App Store" |
| `mobium alert` | reads it: "AdvantageScope XR — Experience AdvantageScope in augmented reality" |
| `tap testid=OpenButton` | the clip downloaded and opened; its camera prompt, SpringBoard's, came first and was declined |
| `current`, `map`, `apps` | name the clip, `org.littletonrobotics.advantagescopexr.Clip`; read its controls; list it |
| `press back --gesture` | no navigation bar, so it swiped high on the screen and said the clip stayed in front |
| `alert accept` on the card (2026-10-05) | pressed its last button, "View on the App Store": the clip's App Store page opened, not the clip |
| `alert dismiss` on the card | pressed Close, and the card went |
| `uninstall` | removed it; `apps` no longer listed it |
| Pillar Valley's clip, `com.evanbacon.pillarvalley.clip` | a card with only Close: "This app clip is not currently available in your country or region", read by `alert` |
| `com.apple.store.Jolly.Clip` | no card at all: that app may have no clip |

Two things to know when driving one:

- **Open the clip from its card by `testid=OpenButton`.** The card is an
  alert to `alert`, and `alert accept` presses its last button, "View on
  the App Store", which opens the clip's store page rather than the clip;
  `alert dismiss` presses Close. Measured on 2026-10-05 — on iOS the choice
  is positional, as CHALLENGES 106 found on other alerts.
- **Until the clip is up, SpringBoard is in front** — the card, the
  download, and any permission prompt the clip raises are all its.

Still open: **a clip of our own on a real iPhone**, which a free Apple ID's
team cannot sign — Xcode refuses with "Personal development teams … do not
support the App Clip capability" — and with it Settings > Developer > App
Clips Testing > Local Experiences, which registers a link or a code for a
clip still in development. The simulator has no such page, and `simctl
launch` ignored Xcode's `_XCAppClipURL`.

`docs/probes/appclip-sim.sh` repeats the simulator measurement.

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

Web content is reached over CDP on Android, which needs no chromedriver to
match the device's Chrome, and Remote Web Inspector on iOS, and on iOS the
app must have opted in with `isInspectable` — which cannot be forced from
outside, and is why the app under test had to be one we control:
[MobiumApp](https://github.com/mobiumdev/mobium-app).

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

**Flutter does not produce native views** — it renders its own widgets
through its own engine — **but it publishes a semantics tree**, built for
screen readers, and UiAutomator2 and XCUITest read it as they read any
other. This section once said that made Flutter a job for a driver process;
that was reasoned, not measured, and on 2026-10-01 it was measured and was
wrong. MobiumApp's `flutter/` demo, Flutter 3.47, on the Pixel 7 AVD and the
iPhone 17 Pro simulator, with no Flutter driver and nothing added to the
app:

| | Android | iOS |
| --- | --- | --- |
| `map` | every control: buttons by text or tooltip, the checkbox and switch with state, a field by its label | the same; Flutter's checkbox reads as a switch |
| A `Semantics(identifier:)` | the resource-id of the control itself | an element of its own, followed by the control in the same frame |
| Text on a widget | the label (content-desc), not the text | the label |
| Typing | reaches the app — once the field has focus | reaches the app |
| The password field | `password="true"` from the start | a plain TextField until it holds something, then a SecureTextField |
| `check`, `uncheck`, a counted tap, back | work | work |
| `scroll-to` the fortieth row | four swipes | four swipes; the scroll view is an empty element and the rows are its siblings |
| An icon button with no tooltip | `Button` — nothing names it | `Button` |

Five things were wrong on the first run, each found by checking what the
app said it got rather than what Mobium reported (CHALLENGES 206): typing
on Android reported success while the field stayed empty; an empty field
mapped as "EditText" and `label=` could not find it by the name it
printed; on iOS `testid=signIn` was refused as covered by its own button;
on iOS typing a password reported a dropped keystroke and printed the
password in the error; and on iOS `scroll-to` stopped at "the end of the
list" after one swipe. All five are fixed, and `docs/checks/flutter.sh`
holds them; it passed on both virtual devices and on both phones.

What Flutter does not publish, Mobium cannot see: a widget drawn with
`CustomPaint` and no `Semantics`, a game's canvas, an icon with no label —
the last is in the demo and maps as a bare `Button`. Those are what a driver
process would be for, and the app's own accessibility is the better fix.

The practical form of the rule: **for automation, ask what is in the
accessibility tree, not what the framework says it produces.** A framework
that creates native views per widget is drivable by anything that reads the
tree, and so is one that paints but publishes semantics. One that paints and
publishes nothing is not, whatever it compiles to.

## What this changes

It named a distinction the roadmap had acted on without writing down: React
Native was driven as an ordinary native app and paid for itself in a
defect, while Flutter was filed under the driver protocol. Measuring Flutter
moved it: it is driven through its semantics tree like any app, and its
first run paid for itself in five defects, as React Native's did in one.
