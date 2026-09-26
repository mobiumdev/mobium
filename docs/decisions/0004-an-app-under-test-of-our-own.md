# 0004 — The app under test has to be ours, because of one platform rule

**2026-09-17.** Closes the gap [0002](0002-ios-webviews-are-reachable.md) left
open in its last section: *"no genuine hybrid iOS app has been driven, only
Safari."* Evidence: [../checks/mobium-app.sh](../checks/mobium-app.sh), which
passes on an iPhone 17 Pro simulator running iOS 26.5 **and on a Pixel 7 AVD
running Android 15**. One script drives both; only the backend differs.

## What was believed

That driving a real hybrid iOS app was blocked on hardware and signing — the
reasoning in ROADMAP, that `simctl install` takes a simulator-SDK bundle while
App Store apps ship device-signed arm64 IPAs, so the only third-party apps a
simulator can host are ones compiled from source, "which tests a build of mine
rather than something somebody shipped."

That reasoning is correct and it is not the binding constraint.

## What is actually true

**On iOS 16.4 and later a `WKWebView` is invisible to Remote Web Inspector
unless the app sets `isInspectable`.** An app that does not opt in publishes
no target, so mobium cannot attach to it whatever mobium does — and the opt-in
is a property of the app, not of the device or the debugger.

Measured, not read: Appium's TheApp v1.12.0 is a React Native hybrid app whose
prebuilt simulator bundle installs and runs fine on iOS 26.5. mobium drives its
native side — launch, map, tap, type, navigation. Its WebView is unreachable.
Writing the developer-extras default into the app's own domain, in both
spellings, changes nothing:

```sh
xcrun simctl spawn <udid> defaults write com.appiumpro.the_app \
    WebKitDeveloperExtrasEnabledPreferenceKey -bool true
```

That probe was run three times before it counted, because the first two were
invalid in the way this project keeps finding: the first read "no contexts" on
a screen with no WebView, the second on a screen whose WebView had not loaded.
A screenshot settled it — page rendered, content visible, still zero
attachable contexts. **A zero is not a result until the same measurement has
been shown able to come back non-zero**, and here the positive control had to
be built before the negative one meant anything.

So the App Store question is moot. Even with a signed IPA on real hardware,
**any app that has not opted in is unreachable.** The app under test must opt
in, which means the app under test must be one we control. This is not a
second-best substitute for a shipped app; for this feature it is the only
thing that can work.

## What was built

**MobiumApp** — React Native (Expo 57, RN 0.86.3, react-native-webview
13.16.1), bundle `dev.mobium.mobiumapp`, MIT, at
**[mobiumdev/mobium-app](https://github.com/mobiumdev/mobium-app)**.

Outside this repository on purpose: it needs Node, Gradle and CocoaPods, and
mobium's single-binary, no-runtime-dependencies property is load-bearing
enough that a toolchain like that does not belong in the tree even under
`examples/`. **It is private today, and so is mobium.** They have to move
together — both to `mobiumdev`, and both to public on the same day — or
[../checks/mobium-app.sh](../checks/mobium-app.sh) tells a stranger to build an
app they cannot obtain.
Every `WebView` sets `webviewDebuggingEnabled`, which compiles to
`_webView.inspectable = …` under `@available(iOS 16.4, *)` — checked in the
library's source at both the setup site and the setter, and confirmed to sit
outside any `RCT_NEW_ARCH_ENABLED` branch, since Expo 57 defaults to the New
Architecture.

Twelve screens, each a control for something specific. It started at four; the
other eight were each added because a claim here rested on a case nobody had
driven, and adding a screen was the cheapest way to test it.

| Screen | Controls for |
| --- | --- |
| WebView Demo | `NewFrame`'s **success** path — a WebView whose frame equals its content |
| Wide Viewport Demo | defect 6, the visual-versus-layout viewport, at `width=1200` |
| Dual WebView Demo | two live inspectable pages in one application |
| Login Demo | `role=password` and `uitree.Redact` on iOS; and a screen reachable only by logging in |
| Location Demo | a GPX route watched from inside the app, through the page's own `watchPosition` |
| Pager Demo | horizontal scrolling — the axis nothing in the hierarchy carries (defect 21) |
| Interruption Demo | a real system permission dialog, and what the app still holds after it clears. Called Popup Demo until 2026-09-26 |
| Dialog Demo | every kind of dialog the app can raise, on demand: its **own** alerts with one, two and three buttons and a prompt (the App Alert Demo, merged in on 2026-09-26), an action sheet, the share sheet, the system permission prompts, and iOS's Allow Paste — the controls behind CHALLENGES 105–107 |
| Form Demo | checkboxes, radios and switches that report a state — the screen defect 65 came out of |
| Gestures ▸ Tap and Press, Drag | which gesture actually arrived. Tap, long press and a short hold are the same touches differing only in duration, so only the app can tell them apart. Drag is the **native** drag witness — how long the finger rested, how many moves arrived, how far it traveled, and whether it came up over the target, all four of which a swipe gets wrong. One screen, "Gestures Demo", until 2026-09-23, when every gesture witness moved under one Gestures entry in the order of [GESTURES.md](../GESTURES.md) |
| Gestures ▸ Rotate | a two-finger turn, measured: the page computes its own angle from `touchmove`, since neither accessibility hierarchy reports a rotation |
| Gestures ▸ Double Tap | a **double tap the browser agreed was one**. React Native has no `onDoublePress`, so a native screen could only apply a threshold of this project's own choosing and then test mobium against it; a page's `dblclick` is the platform's verdict instead — on Android. An iOS WKWebView fires no `dblclick` for injected touches, though the same double tap does trigger WebKit's zoom. Called Touch Demo until 2026-09-23, when the drag measurement it also held — the one that **did not work**, see below — was removed |
| Gestures ▸ Flick and Pan, Pinch and Spread, Multi-Touch | how far a list coasts after the lift, which is what separates a flick from a pan; a page's own zoom level; and each finger's landing, lifting and travel with every raw touch event — the evidence that found UiAutomator2 dropping a pausing finger and WebDriverAgent touching a late finger's target early (CHALLENGES 84, 85) |
| Motion Demo | what **Reduce Motion** buys a tool that waits for a target to stop moving: one target that honors the setting, one that ignores it as the negative control, each timed by the app. 2.8s against 1.6s on an iPhone 15 Plus, the control unchanged |
| Crash Demo | a crash caused on purpose, for `app_logs` and `app_crashes` on a **real iPhone**, where nothing outside an app can crash it: a numbered line to the device log, and an unhandled JavaScript error a Release build treats as fatal. Its report was listed three seconds after the tap on an iPhone 15 Plus and one second on a Pixel 7 AVD — and the error's text is in the iPhone's log, but in Android's report. Added 2026-09-25 |

The drag witness is the one that had to move, and the reason belongs here
because it is about what an app under test is *for*. Measuring a drag in a
WebView, the way the rotation is measured, worked for holds up to 480ms and
reported nothing at all from 520ms up — Android's 500ms long-press timeout,
past which the WebView claims the press and the page stops receiving
`touchmove` ([CHALLENGES 68](../CHALLENGES.md)). A drag holds past that
timeout deliberately, so a page can only see the gesture when it is set up
wrongly. A screen that agrees with you whenever the question is easy is worth
less than no screen, because it reports a pass. The drag witness is native and
the double-tap witness is a page, and each is the one the platform will
actually answer. The page's drag half stayed behind after the native one
replaced it on 2026-09-22, driven by nothing, and was removed the next day: a
witness no check reads looks like coverage and is none.

Pages are shipped inline rather than fetched. The app this was modeled on
whitelists a single domain, and that domain has since lapsed to a parked site
serving somebody else's content — a check that depends on a domain is a check
that fails when somebody stops paying for it.

## What it closed

Three things had never executed:

- **`NewFrame` computing a scale on iOS.** Every previous iOS run went through
  Safari, whose WebView element covers the whole window, so only the refusal
  had ever run ([CHALLENGES 47](../CHALLENGES.md)). A tap inside an embedded
  WebView now lands.
- **Two live WebViews in one application.** What ROADMAP recorded as "done by
  accident" was two *tabs* in one browser. Two simultaneous hosts in one app
  is the harder case and now passes.
- **Anything past a login**, which had been blocked on not having an account.

And one rule is now checked on the platform it was not found on: CHALLENGES 43
was found on Android, where the typed value lands in the node's `text`. iOS
marks the field as `XCUIElementTypeSecureTextField` instead, and `Redact` holds
there too.

## What it does not close

**It is our build, so it is not the evidence a third-party app produced on Android.** A
fixture encodes its author's assumptions; that is what a fixture is, and this
project has five recorded cases of a careful one producing confident wrong
behavior. Wikipedia broke three labeling rules on its first screen precisely
because nobody designing mobium had imagined them. MobiumApp cannot do that.

Its value is the opposite one: it produces **positive controls on demand** —
the non-zero case that makes a zero mean something. Both are needed, and
neither substitutes for the other. Real third-party apps stay on the roadmap.

## Two things React Native does to a hierarchy

Found while driving TheApp, and true of MobiumApp too, so they are the
platform's and not one app's:

- **An accessibility identifier is repeated across the nested views RN renders
  for one component.** `label=Webview Demo` matched **14** nodes on one screen;
  `testid=navigateBtn` matched 5. The locators `map` derives are still unique —
  verified — so nothing is broken, but a hand-written `label=` or `testid=`
  locator is unreliable against any RN app. Use a ref from `map`.
- **An empty text input is labeled with its placeholder**, so `map` prints
  `https://appiumpro.com (input)` for a field containing nothing. It reads
  exactly like a value and cost two wrong turns here.

## A correction

ROADMAP recorded that Appium's TheApp "still cannot be installed on this
machine — `TheApp-v1.10.0.apk` ships `armeabi-v7a` and `x86` against an
`arm64-v8a` emulator." **v1.12.0 ships `arm64-v8a` and `x86_64`.** That
blocker was true of v1.10.0 and is stale.

TheApp was used here only as a reference for what an app under test should
contain, and nothing was taken from it: it carries **no license at all** — no
`LICENSE` file, no `license` field, `"license": null` from GitHub's API — so
there is nothing to take. The screens here are chosen against this project's
defect log rather than against Appium's feature list.

## What Android found

The Android half was built and driven the same day, and it did what a new real
app is supposed to do: it broke something on its first pass.

Text entry failed on the password field with `locate element (id=password): no
such element` — on a ref `map` had just emitted, for an element plainly on
screen, while `tap` on that same ref worked. React Native reports a **bare**
resource-id, and UiAutomator2's `id` strategy qualifies a bare name with the
package under test, so the device searched for `dev.mobium.mobiumapp:id/password`
and found nothing. Five Android apps had never triggered it because all five
report the qualified form, and because it needs a field whose best locator is
a testid rather than its own text — which an empty login box is and a Settings
row is not. [CHALLENGES 55](../CHALLENGES.md), fixed with a regression test.

That is the argument for this app working, and it is worth being precise about
why: the defect was not found because the app is ours. It was found because
the app is **React Native**, which is a hierarchy shape mobium had never met.
A real third-party RN app would have found it too, and would have been better
evidence. What ours added was that it took an afternoon rather than a search.

The app also had a real layout bug of its own on Android, which is worth
recording because it is the same trap in the other direction: React Native's
`SafeAreaView` is iOS-only, so on Android it renders as a plain `View` and the
header sat under the status bar. It looked correct on iOS and had been checked
there.

## Caveats

- **`mobium doctor` reporting "ANDROID_SDK_ROOT is not set" does not mean
  there is no SDK.** It says so — "only needed to start emulators, not to
  drive them" — and it was read here as absence, on top of checking only
  `~/Library/Android/sdk`. The SDK was installed the whole time, via Homebrew
  at `/opt/homebrew/share/android-commandlinetools`, with both AVDs already
  created. A caveat was published saying Android could not be built before
  anyone ran `sdkmanager --list_installed`.
- **Do not run the check while a build is installing to the same device.**
  Doing that produced `FAIL: no WebView context — the app did not opt into
  inspection`, which is exactly what the real failure prints. Builds to the
  *other* platform's device are fine and overlap safely.
- **`expo run:android --device emulator-5554` exits 0 while doing nothing.**
  It printed `CommandError: Could not find device with name: emulator-5554`,
  never ran Gradle, produced no APK, and returned success. The serial `adb`
  uses is not the name Expo wants. Drop the flag when one device is attached.
  This is the project's own first rule arriving from an unexpected direction:
  a zero exit is not evidence, and that is as true of the build tool as of
  `adb`.
- **`expo run:android` needs `ANDROID_HOME` exported into the shell that runs
  it, and says something else when it is missing.** The build fails with
  `Failed to apply plugin 'com.facebook.react.rootproject'` — a Gradle error
  that sends you looking at Gradle. The real cause is eight lines earlier and
  easy to scroll past: `Failed to resolve the Android SDK path. Default
  install location not found: ~/Library/Android/sdk`. A Homebrew install keeps the
  SDK at `/opt/homebrew/share/android-commandlinetools`, so the
  default lookup finds nothing. Export `ANDROID_HOME` **and**
  `ANDROID_SDK_ROOT` before the command; a shell that can run `adb` is not
  enough, because `adb` may be on `PATH` while the variable is unset.

## How to re-check this

```sh
# Build it, then drive it. Same script for both platforms.
cd ~/MobiumApp && npx expo run:ios --configuration Release --device <udid>

export ANDROID_HOME=/opt/homebrew/share/android-commandlinetools   # or it
export ANDROID_SDK_ROOT=$ANDROID_HOME                              # blames Gradle
cd ~/MobiumApp && npx expo run:android --variant release
docs/checks/mobium-app.sh <udid>            # iOS simulator
docs/checks/mobium-app.sh emulator-5554     # Android
```

See the caveats above before believing a failure.
