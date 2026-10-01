# Back, on each platform

What "back" is on Android and iOS, what Mobium sends for it, what MobiumApp
did with it, and what is still missing before back can be relied on in a
test. Every row below was measured on 2026-10-01; nothing here is from
documentation alone unless it says so.

It started with a report: on the Pixel 8 Pro, open MobiumApp, open any demo,
swipe from the right edge — and the whole app closes, where Android users
expect the previous screen.

## What was measured

Devices:

| | Android | Navigation | Notes |
| --- | --- | --- | --- |
| Pixel 8 Pro (Lana's phone) | 17, API 37 | gestures | Chrome 154, Play services |
| Pixel 7 AVD `mobium-test` | 15, API 35 | gestures, and three-button for one run | |
| Pixel 7 AVD `mobium-test-17` | 17, API 37 | gestures | |

| | iOS | Notes |
| --- | --- | --- |
| iPhone 17 Pro simulator | 26.5 | 1206×2622 px |
| iPhone 15 Plus (Lana's phone) | 26.6.2 | 1290×2796 px |

MobiumApp is React Native 0.86.3 on Expo 57, `targetSdk` 36, and its
manifest sets `android:enableOnBackInvokedCallback="false"`. Its screens
are one piece of React state, `screen`, with no navigation library; every
demo has its own Back button.

Each Android run started MobiumApp fresh, opened Login Demo, sent one back,
and read: the app in front, whether MobiumApp's process was alive, how many
of its activities were left, and — after `am start` of its activity — which
screen it came back to. The control is Settings, where back goes to the
parent page natively.

| Input | How Mobium sent it |
| --- | --- |
| key | `mobium press back` — `input keyevent KEYCODE_BACK` |
| right edge | `mobium swipe <w-1> <h/2> <35% w> <h/2> --duration 250ms` |
| left edge | `mobium swipe 0 <h/2> <65% w> <h/2> --duration 250ms` |

## Android

### MobiumApp as shipped

The same on all three devices, gesture navigation:

| Input | After | Process | Activities | Reopened at |
| --- | --- | --- | --- | --- |
| key | launcher | alive | 0 | home |
| right edge | launcher | alive | 0 | home |
| left edge | launcher | alive | 0 | home |

So the report is exact: every back from a demo left the app, its activity
was destroyed, and reopening it started again at home. Settings, given the
same three inputs, went to its parent page every time, so the inputs are
right and the app is what ignores them.

With three-button navigation on the API 35 AVD, the key did the same —
MobiumApp finished, and the app used before it came forward — and neither
edge swipe did anything, in MobiumApp or in Settings. **An edge swipe is
back only in gesture navigation**; `settings get secure navigation_mode` is
`2` for gestures and `0` for three buttons.

### Why

Android delivers back — key or gesture — to the activity. React Native
turns it into a `hardwareBackPress` event; if no JavaScript listener
returns `true`, `ReactActivity.invokeDefaultOnBackPressed` runs, and
MobiumApp's `MainActivity` calls the default, which on these builds
finished the activity (0 activities left, measured — not the "move the
task to the back" that Android 12 documents for root activities). MobiumApp
registered no listener.

### The fix, measured

A `BackHandler` listener that goes where the screen's Back button goes and
leaves home to the system — merged as
[mobium-app #12](https://github.com/mobiumdev/mobium-app/pull/12):

```tsx
useEffect(() => {
  const sub = BackHandler.addEventListener('hardwareBackPress', () => {
    if (screen === 'home') return false;
    setScreen(parent);
    return true;
  });
  return () => sub.remove();
}, [screen, parent]);
```

| Device | key | right edge | left edge |
| --- | --- | --- | --- |
| Pixel 8 Pro, API 37 | demo → home, 1 activity | demo → home | demo → home |
| AVD, API 35 | demo → home | demo → home | demo → home |
| AVD, API 37 | demo → home | demo → home | demo → home |

A nested path on the API 37 AVD: a gesture demo → Gestures → home → the
previous app, one edge swipe each. So the listener is reached on API 37
with `targetSdk` 36 and predictive back opted out — the one combination
that could have kept it from ever being called.

## iOS

iOS has no system back. Going back is the app's: a navigation bar's back
button, and, for a `UINavigationController`, a swipe. Mobium refuses
`press back` there by design (it is not an event iOS sends).

| | MobiumApp | Control |
| --- | --- | --- |
| `press back` | refused, with the reason | — |
| swipe from x=0, mid-height | nothing (simulator and iPhone) | Settings, simulator: back |
| the screen's own back button | back | back |

The control on the simulator is Settings > Search. On the iPhone it is
NetNewsWire's feed timeline rather than Settings, whose first page there
shows the owner's account. On the iPhone:

| Swipe right from | At y | NetNewsWire |
| --- | --- | --- |
| x=0 and x=20 | 1398 px, on an article row | stayed |
| x=0 and x=20 | 330 px, the navigation bar | back |
| x=0 and x=20 | 2600 px | back |
| x=400 and x=645 | 330 px | back |
| `mobium swipe right` | middle of the screen | stayed (on a row) |

And on the simulator, Settings went back from a swipe starting at x=0, 30,
60 and from `mobium swipe right`, the middle of the screen.

Three things follow.

- **Mobium's injected swipe is the platform's back gesture** on iOS 26,
  simulator and phone alike, and it need not start at the edge: iOS 26
  pops a navigation stack from a swipe that starts anywhere in the content.
- **A row with swipe actions takes the swipe first.** Starting on one,
  the row slid and the screen stayed. Start in the navigation bar.
- **MobiumApp has no swipe back on iOS** because nothing in it is a
  navigation controller. That is not fixable with a listener as on
  Android: it needs a native stack (`react-native-screens` behind a stack
  navigator) or a left-edge pan of its own, and only the first gets
  iOS 26's whole-screen swipe for free. Not built or measured.

## Gaps

### In MobiumApp

1. **Android back closed the app.** Fixed in mobium-app #12, measured
   above. Rebuild MobiumApp for any device that still has the old build,
   and keep `docs/checks/mobium-app.sh` passing.
2. **No swipe back on iOS.** Needs native-stack navigation; unmeasured.
3. **WebView screens.** Android back should go back in the page's history
   before leaving the screen, as a browser does. Unmeasured.
4. **Predictive back.** MobiumApp opts out
   (`enableOnBackInvokedCallback="false"`); with it on, the listener's
   behavior is unmeasured. Apps targeting Android 16 and later are moving
   to it, so it is the configuration a real app will have.

### In Mobium

Fixed on 2026-10-01, and held by `docs/checks/back.sh`, which passed on the
Pixel 8 Pro, both AVDs, the simulator and the iPhone 15 Plus:

- `press back` says what back did: "it left <app>; <app> is in the
  foreground", or "<app> is still in the foreground", and on iOS what the
  navigation bar says now. It waits for that outcome by its own clock — a
  back that left an app handed focus over in about 0.1s — so a back inside
  an app answers in one to two seconds, where the launch wait it first
  borrowed took 2.4.
- `press back --gesture` (`app_press` with `gesture`) swipes in from the left
  edge, mid-height on Android and in the navigation bar on iOS, and is
  refused with the reason on an Android device using button navigation.
- `devices` names an Android device's navigation mode.
- The iOS refusal names the gesture, the CLI flag and the MCP argument.
- Every client has a back that returns the outcome.

What was open before, kept for the record:

1. **`press back` does not say what back did.** On Android it answers
   "pressed back" whether the app went to a parent screen or closed —
   exactly the difference this report was about, and a test cannot see it
   without another read. `press home` already reports what came to the
   front. Back could report the app in front afterwards, and say so
   plainly when back left the app.
2. **No gesture back.** An agent testing the gesture path — which predictive
   back makes different from the key — has to know the screen's size and
   the navigation mode and compute an edge swipe. Measured to work on all
   five devices; missing is a command for it (`back --gesture`, or `swipe`
   from an edge) and refusing it in three-button mode, where an edge swipe
   is not back.
3. **The navigation mode is not reported.** `devices` and `doctor` could
   say gestures or three-button, since it decides what an edge swipe is.
4. **The iOS refusal's remedy is right but incomplete.** "Use an edge swipe
   with app_swipe if the app supports one" works — on iOS 26 even from the
   middle — but on a row with swipe actions the same swipe does nothing.
   The remedy should say to start in the navigation bar, and give the
   command.
5. **No check holds back.** Once MobiumApp handles it, a check should walk
   demo → hub → home → out with the key and both edges on Android, and the
   back button and a swipe on iOS, with Settings as the control, on
   emulators, simulators and both phones.

## Repeating it

`docs/probes/back-android.sh <serial>` and `docs/probes/back-ios.sh
<simulator-udid>`, small shell scripts around `mobium` and `adb`. The Android
one reads `navigation_mode`, the screen size from `wm size`, and the app's
activities from `dumpsys activity activities`, and prints nothing from the
device but the app in front and counts. On a phone, read nothing from
Settings' first page: it names the owner.
