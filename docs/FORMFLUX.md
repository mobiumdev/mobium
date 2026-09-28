# formflux — one device, many screens

**Started 2026-09-14.** `internal/formflux`.

A flow that works on the screen you happen to have is a flow tested once.
formflux makes one device impersonate several, so the same flow can be run
against a small phone, a flagship, a tablet and an accessibility display size
without owning any of them.

## What was measured before any of this was written

All of it on `emulator-5554`, a Pixel 7 AVD on Android 17, 2026-09-14.

| Question | Answer |
| --- | --- |
| Does `wm size` change the screen? | Yes. `wm size 720x1520` took effect in about 3 seconds. |
| Can it be read back? | **Yes, and this is the good news.** `wm size` reports `Physical size: 1080x2400` *and* `Override size: 720x1520` — so a check can tell what it changed and what it must put back. |
| Does the hierarchy follow? | Yes. `map` bounds went to a maximum of 720×1520. |
| Do screenshots follow? | Yes. 720×1520 PNG. |
| Does reset work? | Yes, and the override line disappears — which is how the reset is verified rather than assumed. |
| Does it actually change layouts? | **Yes.** Android Settings maps 11 actionable elements at 1080×2400, **8** at 720×1520 and **12** at 1600×2560. |

That last row is the one that matters. A measurement that comes back "no
difference" is not a result until it has been shown it can come back
different — this project has a defect on record for exactly that — so the
element counts above are the positive control, not a demo.

## The asymmetry, which is the whole design problem

**On Android a screen is a setting.** `wm size` and `wm density` override the
display for every app, both report their physical value alongside the
override, and `reset` puts them back. Cost: about half a second per profile,
measured across all six.

**On iOS a screen is a device.** A simulator's geometry is fixed when it is
created and nothing resizes a booted one. Covering four screens means creating
and booting four simulators — tens of seconds each — and the density cannot be
varied independently at all, because Apple ships the combinations Apple ships.

So this is one idea with two mechanisms, and `Profile.Settable()` is where the
difference is admitted rather than papered over. `Apply` refuses an iOS
profile and says to boot a simulator of that type instead, which is the same
rule the driver follows for `press back` on iOS: **name what the platform
cannot do, do not approximate it.**

## Three units, and only one of them decides a layout

This is not pedantry. formflux shipped a touch-target check that compared
Apple's 44**pt** guideline against **pixel** bounds and was wrong by a factor
of three, so every field in the code now carries its unit in its name.

| Unit | What it is | Where it appears |
| --- | --- | --- |
| **px** | physical device pixels | `map` bounds, screenshots, taps — on **both** platforms, because mobium normalizes iOS coordinates on the way out |
| **dpi** | Android's density, dots per inch against a 160 baseline | `wm density`; Android only |
| **dp** | Android's density-independent pixel, `px × 160 ÷ dpi` | what layouts and resource qualifiers are written in |
| **pt** | iOS's point, `px ÷ scale` (scale is 2 or 3) | Apple's guidelines, including the 44pt touch target |

**Android chooses layouts and resources by dp, never by pixels.** A screen's
`sw` value — its shorter edge in dp — is the single number that decides
whether a different layout is selected at all. Two screens with very different
pixel counts and the same `sw` will lay out identically.

## The profiles

Every Android geometry is a real device's, not a round number, because a
layout breaks at a real width.

| Profile | Pixels | dpi | dp | sw | What it actually tests |
| --- | --- | --- | --- | --- | --- |
| `pixel-7` | 1080×2400 | 420 | 411×914 | **411** | the AVD's own geometry; the baseline, and first on purpose |
| `pixel-8-pro` | 1008×2244 | 360 | 448×997 | **448** | the real phone: *fewer* pixels than the emulator and a **wider** layout, because it is less dense |
| `small-phone` | 720×1520 | 320 | 360×760 | **360** | sits exactly on Android's own `sw360dp` qualifier — a boundary worth testing *on*, not near |
| `flagship` | 1440×3120 | 560 | 411×891 | **411** | 78% more pixels than `pixel-7` and **the same layout width**. Tests high-density rendering and pixel-sized touch targets, *not* a wider screen |
| `tablet` | 1600×2560 | 320 | 800×1280 | **800** | past `sw600dp` and `sw720dp`, so the only profile here that can select a genuinely different layout |
| `fold-open` | 2076×2152 | 390 | 851×882 | **851** | a foldable's inner screen, nearly square: past both tablet qualifiers with no long edge, so a layout that assumes tablets are landscape or phones are tall is wrong here both ways |
| `fold-closed` | 1080×2424 | 390 | 443×994 | **443** | the same device folded — its outer screen, a phone. The pair crosses Android's tablet and phone layout classes on one device, mid-task |
| `display-size-large` | 1080×2400 | 560 | 308×685 | **308** | **the narrowest layout in the set**, on the phone with the most pixels. Android's display-size accessibility setting |

| iOS profile | Pixels | Scale | Points | Note |
| --- | --- | --- | --- | --- |
| `iphone-17-pro` | 1206×2622 | 3× | 402×874 | the verified baseline |
| `iphone-17-pro-max` | 1320×2868 | 3× | 440×956 | 38pt wider than the baseline |
| `ipad-mini` | 1488×2266 | 2× | 744×1133 | the only regular-width size class here, where every iPhone is compact — the nearest iOS has to a foldable's phone-to-tablet jump, as a second device rather than a transition |
| `ipad-pro-13` | 2064×2752 | 2× | 1032×1376 | the largest iOS screen — with `ipad-mini`, the two ends of the regular width class |
| `iphone-16e` | 1170×2532 | 3× | 390×844 | 12pt narrower than the baseline |

### Two things the arithmetic corrected

Both of these were written into the first version of this file as confident
rationales, and both were wrong. The dp column is why.

- **`flagship` does not test a wider layout.** It is sw411dp, exactly the same
  as `pixel-7`. The original note claimed it "catches layouts that assume a
  maximum width"; it cannot, because as far as Android is concerned it is the
  same width. What it does test is real and different: bitmap assets, and
  touch targets that are measured in pixels.
- **`display-size-large` is the narrowest profile**, at sw308dp — narrower
  than `small-phone` at sw360dp, on a screen with more pixels than either. That
  inversion is the reason it finds layout bugs nothing else does, and it is a
  much better explanation than the original "everything is larger".

**iPhone widths vary far less than Android's** — 390 to 440pt across the whole
current range, against 308 to 800dp here. One Android profile finds more than
one iOS profile does, which is worth knowing before spending tens of seconds
booting simulators.

### Why `display-size-large` is the one to keep

Changing density without changing size is what Android's **display size**
accessibility setting does. Text wraps, controls collide, and labels truncate
— and no amount of rotating or resizing finds it, because the pixels never
change.

## Foldables

The one device whose `sw` changes on its own. Measured on a Pixel 9 Pro Fold
emulator (Android 15) on 2026-09-28; the two `fold-` profiles are its two
screens, as the emulator's device profile gives them.

- **The posture reads back and can be set.** `cmd device_state state` names
  it — `CLOSED`, `HALF_OPENED`, `OPENED` and `REAR_DISPLAY_MODE` on this
  device — and `cmd device_state state <n>` overrides it, `state reset`
  clearing the override; each read names the committed, base and override
  states separately. The emulator console's `fold` and `unfold` move the
  simulated hinge itself, and exist only on an emulator.
- **`wm size` follows a fold**, 2076×2152 open and 1080×2424 closed, where
  it does not follow a rotation: folding switches to another physical panel.
  Coordinates still come from the hierarchy, and `map`'s bounds followed
  both ways.
- **A ref taken open and tapped closed** was re-resolved and landed on the
  folded layout's button — mobium's rule of re-resolving before acting,
  holding across a fold.
- **MobiumApp kept a typed field across both**; React Native handles the
  size change without restarting the screen. An app that restarts its
  activity on a size change would not, and that is the state-loss question a
  fold asks.
- **`fold-closed` applied by setting to the open device** gave the same
  1080-wide layout, with the typed field kept, as folding it did. For layout
  the two are equivalent; for anything tied to which panel is on, only a
  real fold is.

The first run at `fold-open` reported one tiny touch target, in Android's
Settings: a row cut off by the bottom of its list, measured by the sliver
still showing. That was formflux's mistake, not Settings' — CHALLENGES 146 —
and a dimension clipped by a scroll container is no longer judged.

That run also showed the device test could only log: against Settings,
nothing to find and nothing asserted. `TestDeviceCatchesThePlantedTargets`
drives MobiumApp's **Layout Demo**, a positive control built for it — a 24dp
target too small at every size, and a bar an eighth of the screen wide that
is 51dp on a 411dp phone and under 48dp below 384dp. At all eight profiles
it asserts the first is reported, and the second exactly on `small-phone`
and `display-size-large`; with the touch minimum lowered to 20dp it fails.

And it found something about MobiumApp: **a density change restarts its
screen**, dropping it back to its home list, while a size change — a fold,
or `fold-closed` applied by setting — keeps it. Density is what Android's
Display size setting changes, so a person who changes it mid-task loses
their place. That is React Native's default and the app's behavior, not
mobium's, and it is the kind of state loss a profile exists to show.

Not measured: a real foldable, One UI's own fold behavior (a Samsung
emulator skin is only the frame and the sizes, on stock Android), what
`HALF_OPENED`'s tabletop layout does to an app that supports it, and whether
a phone's shell may override `device_state` at all.

## Running it

The parsing is covered by fixtures captured from the device. The behavior is
not, and cannot be — so there is a device-backed test, opt-in like the
network one:

```sh
MOBIUM_DEVICE_TESTS=1 MOBIUM_DEVICE=emulator-5554 \
    go test ./internal/formflux/ -run Device -v
```

`TestDeviceCatchesThePlantedTargets` needs MobiumApp installed, and skips
without it.

It applies every Android profile, checks each against a readback, and
restores the physical screen **in a deferred call, so it runs even when an
assertion fails**. A half-applied profile is a state left on somebody's phone;
on an emulator it survives until the next wipe.

It also refuses to start if the device already has an override in force,
because the "physical" size it records for restoration would then be somebody
else's override.

## A real page to test against

Loaded on the iPhone 17 Pro simulator on 2026-09-14, through Safari and
mobium's WebView path:
**<https://www.apple.com/shop/buy-iphone/iphone-duo>**

It works as a target, and it is worth naming because every responsive check
written against a page somebody controls is a check that agrees with itself.

- The context appears as `WEBVIEW_com.apple.mobilesafari_2`, titled
  *Shop iPhone Duo - Apple*.
- `map` returns a real control surface: a gallery whose images are `tab`
  elements, finish swatches, a trade-in control, and a "See how to pay
  monthly" link.
- `eval` reads the images. `iphone-duo-finish-unselect-gallery-1` is **natural
  1280×492, displayed 640×246** — a 2× asset on a 3× device.

That last point is the one formflux should eventually assert. A page serving a
2× image to a 3× screen is a real, machine-checkable defect of exactly the
kind a resolution factory exists to find, and it is invisible to a human
looking at a screenshot. Nothing asserts it yet — see below.

It is a third-party page and it will change without telling us, so a check
built on it must fail loudly rather than silently pass when the markup moves,
and it needs a network. Treat it as a target to develop against, not a
regression test.

## What it does now

All three pieces that were open on the first commit are built, and each was
run against a device rather than reasoned about.

**Assertions.** `Inspect` reports four kinds — an element past the left or
right edge, a tappable target below the platform minimum, text the platform
truncated, and a tappable element with nothing to announce. On Android
Settings it is silent at the native screen and reports exactly one new finding
at 720x1520, one at 1440x3120 and one at 1080x2400 @560. Silent when fine,
specific when not.

Two things it learned by being run:

- The unlabeled check first reported seven identical findings on Settings'
  clickable containers, claiming a screen reader would announce nothing. That
  was **simply wrong** — Android composes a container's announcement from its
  children — so the rule was narrowed to a tappable with nothing announceable
  anywhere underneath it. A check that is wrong about a working screen is
  worse than no check.
- Findings carry the element's path, because without it two problems on two
  unlabeled containers print identically and neither can be acted on.

**iOS.** `Ensure` finds a simulator of a profile's device type, boots it, or
creates one if none exists; `Release` shuts down only what it booted and
deletes only what it created. Verified both ways: against an already-booted
simulator, which it correctly left alone, and against a shut-down type, which
exercised create, boot and delete in 21 seconds. The first run passed without
testing anything, because the simulator was already booted — the test now
defaults to a type that is not.

**The tool.** `app_screen`, reaching the CLI, the MCP schema and all five
clients, with `internal/apisurface` enforcing it. `mobium screen`,
`mobium screen small-phone`, `mobium screen --inspect`, `mobium screen reset`.

## What is still not here

- **iOS touch targets are not checked**, and that is a correction. The check
  first used 44pt against bounds that are **device pixels** — mobium
  normalizes iOS coordinates on the way out, so an iPhone 17 Pro reports
  1206x2622, not 402x874. It was wrong by the scale factor and called Apple's
  own status-bar items undersized. Fixing it needs the point-to-pixel scale,
  which the WDA driver reads but does not expose; that means a real capability
  on the driver seam rather than a bare type assertion. Until then the check
  does not run, because a threshold off by 3x produces confident findings
  about working screens.
- **Overlap** is unimplemented. Two controls on top of each other is a real
  defect, but a container legitimately contains its children, so it needs
  ancestry-aware comparison to avoid reporting every list.
- **Font scale.** Display size is driveable; `font_scale` is not.
