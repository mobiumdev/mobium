# Meta Quest (Horizon OS)

**Status: 2D panel apps driven on the Meta Spatial Simulator since
2026-10-25; no real headset yet, and not declared supported.** Horizon OS
is Android 14 underneath, and the Android backends drive its 2D apps, the
panels a Quest shows in front of the user, after four fixes (CHALLENGES
364–367). Immersive apps, the ones that draw their own 3D scene, are not
reachable: they publish no accessibility tree.

Every measurement here is from the simulator: Meta Spatial Simulator
207.0.0.83.331 (Horizon OS build 207, Android 14, API 34) on an Apple
Silicon Mac, with Android Emulator 37.1.11. A real Quest may differ, and
nothing here says it does not.

## Setting up the simulator

The Spatial Simulator is an Android emulator system image that Meta ships
through its `metavr` command-line tool. It is not an AVD: `metavr ssim
start` runs the SDK's own `emulator` against it. Installing it needs a Meta
developer account and about 9 GB.

```sh
metavr ssim download
ANDROID_HOME=<your Android SDK> metavr ssim start --no-device-auth
mobium devices        # a plain "android emulator", emulator-5556 here
```

`metavr` does not find an Android SDK installed outside its usual places,
such as Homebrew's, unless `ANDROID_HOME` names it. The simulator starts
with a window; its launch script opens no gRPC port and no fixed console
port. It ships with **Location off**, no Google Play services and no input
method.

Mobium knows it is on Horizon OS by `ro.vros.build.version`. `devices`
does not yet say so.

## What works

Measured on the simulator, each with the command and its output kept:

| | Result |
| --- | --- |
| `doctor`, `devices`, `install`, `apps` | Work |
| `launch` by package | Works: the Browser, Gallery, Store, MobiumApp (0.78 s) |
| `map`, `text`, locators | Work, on the app in front (below) |
| `tap`, `type`, `fill`, `scroll-to` | Work, inside the app in front |
| A permission prompt | Mapped, read as an alert, answered with `alert --button` |
| WebViews over CDP | `contexts`, `context`, `eval` reach the Quest Browser and an app's own WebView; a web tap in an app's own WebView lands |
| `screenshot` | 0.54 s, 2064×2208: one flat view of the display, not one image per eye |
| `mobium-app.sh` | Every step through "popup setup" passes; the location steps need Location on and Google Play services (below) |
| A two-thumb slider's `fill` | Lands: 23 fills in 24 exact, the 24th refused one $1 step off (CHALLENGES 368) |
| Morpho Market's shared suite | 26 of 26 |

## How Horizon OS differs, and what Mobium does about it

**Every app is a panel, and every panel stays resumed.** A phone shows one
app in front, and its hierarchy holds that app. Horizon OS keeps every
open app as a panel smaller than the display, over the shell's own
windows, all resumed at once. UiAutomator2 reads the window in front only,
so the home screen mapped as nothing (364). On Horizon OS the session reads
every window, in drawing order, and `map`, `text` and every locator see the
windows of the app in front only, as on a phone (366, 367).

**A panel is not a dialog.** On a phone, a window that does not fill the
screen is a dialog. On Horizon OS none fills it, so every panel was "a
dialog", and a check passed on a dialog that was not there (365). The
window in front is a dialog only when a lower window that does not fill
the screen encloses it, as the location prompt does over its app.

**A touch on another app's panel only brings that panel forward.** Nothing
under it is pressed, and the injection reports failure. So a node in
another app's window is refused, naming the app in front, with two
remedies measured to work: `press home` brings the home screen in front of
every panel, and `launch` brings an app's panel in front (366).

**The resumed activity is not the app in front.** `dumpsys`'s first
`ResumedActivity` still named the Browser with MobiumApp in front. `launch`
confirms the foreground its own way.

## Not yet

- **A Compose app's drag moves about 2.4 times as far as the finger.**
  An app's window is 500×800 pixels, drawn 2.48 times that size; a React
  Native app's drag lands exactly, a Compose app's does not, so the gain is
  the app's and a plain `drag` or `swipe` is not corrected. A two-thumb
  slider's `fill` learns it from its own drags and lands (368). Morpho
  Match's sort level, which drops a card where its drag carried it, fails
  for it.
- **A web tap in the Quest Browser is refused**: its WebView element is
  taller than the page's viewport, the case refused in Mobile Safari for
  the same reason (CHALLENGES 47). An app's own WebView is not affected.
- **Enter did not submit the Browser's address bar**; a tap on its
  suggestion did.
- **The dump backend** (`--driver uiautomator`) still reads one window,
  and in the window's own pixels, where taps are in the screen's: it
  would tap the wrong place. Use the default driver.
- **Horizon OS Settings** cannot be tested on the simulator: it answers only
  the action `com.oculus.action.SETTINGS`, with no launcher, and the
  simulator ships a stub in its place. `launch` of it says "is it
  installed?", which names the wrong cause.
- **Location on the simulator**: with Location off, Horizon OS suppresses
  the permission prompt and says so only in the log. With it on, an app
  using Google's fused location sees nothing, since there are no Google
  Play services. `mobium-app.sh` has no Horizon OS branch for either.
- **A small panel wholly over a larger one** would read as a dialog. Not
  yet seen.
- **A real Quest**: its build properties, its keyboard window, and whether
  any of the above holds there.
- **Immersive apps**: they publish no accessibility tree, so Mobium sees
  nothing in one.
