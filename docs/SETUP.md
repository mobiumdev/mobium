# Setting up devices

Everything Mobium needs on the machine and on the device, for all four kinds
of target. Written from doing it: every trap below cost real time here, and
most of them report something that names the wrong cause.

**Run `mobium doctor` first.** It checks the whole list without needing a
device, and exits non-zero if it found something:

```
ok    adb                    Android Debug Bridge version 1.0.41 at /opt/...
ok    android sdk root       /opt/homebrew/share/android-commandlinetools
ok    android devices        emulator-5554 (device)
note  ios simulators         11 available, none booted
ok    daemon socket          /Users/you/.mobium/daemon/mobium.sock (46 of ~104 bytes)
ok    device agents          cached: uiautomator2, webdriveragent
```

`note` means something is missing that is only needed for part of the job —
no Xcode on a machine doing Android work is normal, not a problem.

---

## Contents

- [Android emulator](#android-emulator)
- [Android real device](#android-real-device)
- [iOS simulator](#ios-simulator)
- [iOS real device](#ios-real-device) — native screens, WebViews and Safari
- [What Mobium installs, and removing it](#what-mobium-installs-and-removing-it)

---

## Android emulator

```sh
brew install --cask android-commandlinetools

export ANDROID_HOME=/opt/homebrew/share/android-commandlinetools
export ANDROID_SDK_ROOT=$ANDROID_HOME
export PATH="$ANDROID_HOME/emulator:$ANDROID_HOME/platform-tools:$PATH"

yes | sdkmanager --licenses
sdkmanager --install "emulator" "platform-tools" \
  "system-images;android-35;google_apis;arm64-v8a"

avdmanager create avd -n mobium-test -d pixel_7 \
  -k "system-images;android-35;google_apis;arm64-v8a"

emulator -avd mobium-test -no-snapshot-save -no-boot-anim &
adb wait-for-device
```

`sdkmanager` and `avdmanager` need a JDK. If they fail with a Java error:

```sh
export JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home
```

### Running headless

Add `-no-window`. Everything mobium does works unchanged, screenshots
included — `adb exec-out screencap` reads the framebuffer, not the window:

```sh
emulator -avd mobium-test -no-window -no-snapshot-save -no-boot-anim -no-audio &
```

**Unlike an iOS simulator, the window cannot be attached afterwards.** `simctl
boot` is always headless and `open -a Simulator` shows it later; the Android
emulator decides at launch, and the console (`adb emu help`) has no command to
raise a window. To see the screen on a `-no-window` emulator, take a
screenshot — or restart it without the flag.

Measured on a Pixel 7 AVD, both modes A/B in one sitting, so the comparison is
like for like:

| | `-no-window` | windowed |
| --- | --- | --- |
| `map` (uiautomator2), median of 5 | 0.03s | 0.04s |
| `screenshot` | 0.18s | 0.22s |

Slightly faster, not dramatically. The real reasons to use it are that nothing
steals focus and that it runs where there is no display at all. Resident memory
was ~5.3–5.6GB in both, and the two readings disagreed by less than they varied,
so treat them as the same.

### Headed and headless: the procedures, and what differs

Measured 2026-09-26, when a headless emulator was restarted with a window
mid-session and three things changed at once. The screen is the same; the
**keyboard** and the **snapshot** are not.

**Switching an emulator between the two.** It is decided at launch, so it
is a restart, in the order `docs/SHUTDOWN.md` gives:

```sh
mobium daemon stop                      # every session on this device, first
adb -s emulator-5554 emu kill
emulator -avd mobium-test -no-snapshot-load -no-boot-anim &            # headed
emulator -avd mobium-test -no-snapshot-load -no-boot-anim -no-window & # headless
adb -s emulator-5554 wait-for-device
```

**`-no-snapshot-load`, not `-no-snapshot-save`.** `-no-snapshot-save` still
*loads* the quick-boot snapshot on the next start, and everything installed
since that snapshot was taken is gone — MobiumApp came back as a build from
days earlier, with screens it no longer has, and the first check to look for
a new screen reported it missing. Cold-boot, or reinstall what you need after
a restart and confirm its version.

**The keyboard is the difference that matters.**

| | headless emulator | headed emulator | simulator |
| --- | --- | --- | --- |
| keyboard | the full on-screen keyboard | **none**: the Mac's keyboard is a hardware keyboard, and Gboard shows a floating toolbar — a pill at the left edge — in its place | on-screen, until the simulator switches itself to the hardware keyboard and parks it below the screen |
| `mobium keyboard` says | shown | shown (Android's `mInputShown` is true for the pill) | shown only while it is on screen |
| covers the bottom of the screen | yes | no — the pill covers the left edge, x 21–169 | yes, while it is up |

`show_ime_with_hard_keyboard` was already 1 on the headed emulator and did
not bring the full keyboard back. What follows:

- **Checks that need the keyboard over something skip, and say so.**
  `dialogs.sh`'s keyboard step asks whether the button is really covered —
  the touchable region on Android, the keyboard node on iOS — and prints
  SKIPPED with the reason when a hardware keyboard means nothing is.
- **Run keyboard checks headless**, or on a simulator whose software
  keyboard is showing (I/O ▸ Keyboard ▸ Toggle Software Keyboard, ⌘K). A
  simulator switches on its own; check `mobium keyboard` before blaming a
  check.
- **The pill is an obstruction worth knowing about**: it covers whatever is
  at the left edge, and Mobium refuses a target under it (CHALLENGES 107).

**Everything else is the same**: screenshots come from the framebuffer
either way, and `map` and screenshots cost about the same (the table above).

**One run per daemon session per device.** Two things run against one
device at once — a check in the background and commands by hand — on the
same `MOBIUM_SESSION`, or on two sessions both holding that device, made
WebDriverAgent restart mid-run and drop the app to the home screen: each
invalidates the other's device-side session. Give each concurrent run its
own `MOBIUM_SESSION`, on its own device.

### Two traps, neither of which names its own cause

- **`platform-tools` must be installed *into the SDK root* by `sdkmanager`**,
  not only as the `android-platform-tools` Homebrew cask. The emulator
  validates the SDK root by looking for that directory and aborts with
  `Broken AVD system path` / `Cannot find valid sdk root path` — while `adb`
  sits on your `PATH` working perfectly, which sends you looking in entirely
  the wrong place. `mobium doctor` checks this specifically.
- **`avdmanager` prints `Could not load devices from .../devices.xml`.**
  Harmless. The device profile *is* applied — confirm with `hw.lcd.width` in
  `~/.android/avd/<name>.avd/config.ini` rather than believing the error.

### Newer Android versions

Stable `sdkmanager` tops out at API 35 (Android 15). API 36 and 37 are
canary-channel only:

```sh
sdkmanager --channel=3 --list | grep "system-images;android-37"
sdkmanager --channel=3 "system-images;android-37.0;google_apis;arm64-v8a"
```

Use the plain `google_apis` flavor to match an existing AVD; the `_ps16k`
variants use a different memory page size, and `google_apis_playstore` images
cannot be rooted.

### Running two at once

```sh
emulator -avd mobium-test    -no-snapshot-save -no-boot-anim &            # 5554
emulator -avd mobium-test-17 -no-snapshot-save -no-boot-anim -port 5556 & # 5556
```

Then pass `--device emulator-5556` to every command, or set it once with
`mobium daemon start --device`.

### What the images do *not* have

A stock Google APIs image ships about 240 packages and **no calculator**.
Whatever app you plan to test, check it is there before building a test around
it. To compare against a real phone, pull the app off the phone:

```sh
adb -s <phone> shell pm path com.example.app
adb -s <phone> pull <base.apk> ; adb -s <phone> pull <split_config.xxhdpi.apk>
adb -s <emulator> install-multiple -r base.apk split_config.xxhdpi.apk
```

---

## Android real device

Verified on a Pixel 8 Pro running Android 17. Mobium needed no code changes
for real hardware; the setup is entirely on the phone.

### Over USB

1. **Settings → About phone → tap "Build number" seven times.** This is what
   creates Developer options; without it neither debugging switch exists.
2. **Settings → System → Developer options → USB debugging.**
3. Plug in. A **"Allow USB debugging?"** prompt appears — accept it, and tick
   *Always allow from this computer*, or you will re-accept it every time the
   adb server restarts.
4. `adb devices` should list the serial as `device`.

### Reading the failure modes

`adb devices` tells you which stage you are stuck at:

| What you see | What it means |
| --- | --- |
| the serial, `device` | working |
| the serial, `unauthorized` | debugging is on; the prompt is waiting on the phone screen — `doctor` flags this as a problem, not as a device |
| **nothing at all** | the phone is not exposing the debug interface — step 1 or 2 is missing |

The third is the confusing one, because the cable looks dead when it is not.
Check whether the Mac can see the hardware at all:

```sh
ioreg -p IOUSB -w0 -l | grep -i "Pixel\|Product Name"
```

Hardware visible but nothing in `adb devices` means USB debugging is off, not
a cable fault. `mobium doctor` makes this call for you:

```
FAIL  android devices   Pixel 8 Pro is plugged in but adb cannot see it
          → On the phone: Settings → About phone → tap Build number seven
            times, then Settings → System → Developer options → enable USB
            debugging...
```

One more, rarely: if debugging is on and still nothing appears, pull down the
notification shade, tap the USB notification and change the mode from charging
to **File transfer**. On Android 12+ this is usually unnecessary.

### Over Wi-Fi

Android 11+. The phone and the Mac must be on the same network.

1. **Developer options → Wireless debugging → on.**
2. **"Pair device with pairing code"** — it shows an address and a six-digit
   code. Both expire in about a minute.
3. Find the address without reading it off the screen:

   ```sh
   adb mdns services
   # adb-3C19...-FIOvFv  _adb-tls-pairing._tcp  10.0.0.96:33169
   # adb-3C19...-FIOvFv  _adb-tls-connect._tcp  10.0.0.96:43943
   ```

4. Pair against the **pairing** port, which is not the one on the main
   Wireless debugging screen:

   ```sh
   adb pair 10.0.0.96:33169 123456
   ```

`adb pair` may answer `protocol fault (couldn't read status message)` **and
have succeeded anyway** — check `adb devices` before retrying.

### Expect it to be slower

Measured on a Pixel 8 Pro against a Pixel 7 AVD, same Android 17:

| | real device | emulator |
| --- | --- | --- |
| `map` / `text`, uiautomator2 | 0.63s | 0.04s |
| `map` / `text`, dump backend | 3.2s | 1.96s |
| `screenshot` | 3.1s | 0.13s |

The screenshot cost is `adb exec-out screencap` itself, not Mobium — raw adb
measures the same. On a real device prefer `map` to `screenshot`, and the case
for UiAutomator2 over the dump backend is stronger than on an emulator, not
weaker.

---

## iOS simulator

Needs full Xcode, not just the Command Line Tools.

```sh
xcode-select -p                      # should be inside Xcode.app
sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
xcodebuild -downloadPlatform iOS     # the runtime, if it is missing

xcrun simctl list devices available
xcrun simctl boot <udid>             # boots headless — no window appears
open -a Simulator                    # only this puts it on screen
mobium --backend webdriveragent --device <udid> map
```

**`simctl boot` shows you nothing.** It starts the runtime with no window, so
a simulator can be fully booted and driveable while the screen stays empty.
`open -a Simulator` attaches the GUI to whatever is already booted. Mobium
neither needs nor opens it — `xcrun simctl io <udid> screenshot` works
headless — but it is the first thing anyone asks about.

**Install Xcode from the Mac App Store.** `xcodes install` has been broken
since 2026-09-11: Apple removed the `/olympus/v1/app/config` endpoint it uses
to fetch its service key, so it 404s immediately after the Apple ID prompt
([xcodes#490](https://github.com/XcodesOrg/xcodes/issues/490)). Downloading
the `.xip` from developer.apple.com and running
`xcodes install <ver> --path <file>.xip` also works, since that skips Apple
auth entirely.

Anything needing a password or 2FA has to run in a real terminal — there is no
TTY inside an agent session.

First run downloads and installs WebDriverAgent, which takes a moment and
reports progress. It is cached under `~/.mobium` afterwards.

### WebViews on a simulator

WKWebViews work, over WebKit's Remote Web Inspector rather than CDP — see
[decisions/0002](decisions/0002-ios-webviews-are-reachable.md). Two
preconditions, neither of which mobium can arrange:

```sh
# the app must be inspectable: webView.isInspectable = true on iOS 16.4+
# for Safari, enable Web Inspector:
xcrun simctl spawn booted defaults write com.apple.mobilesafari \
    WebKitDeveloperExtrasEnabledPreferenceKey -bool true
```

And one thing that looks like a bug and is not: **a page's inspector target
belongs to one debugger at a time.** If `app_context` says the page never
announced a target, something else already holds it — Safari's own Web
Inspector, or a mobium daemon still switched into that page. `mobium daemon
stop` clears the second.

### What a simulator does not have

Not a setup trap so much as the trap after setup: a test that passes here and
has never run on hardware. A simulator mimics the OS, not the device, so none
of this exists to be driven —

**Camera, motion (accelerometer and gyroscope), proximity sensor, ambient
light sensor, barometer, Bluetooth, and audio input.** Face ID and Touch ID
are *simulated outcomes* rather than recognition: the simulator can be told
the match succeeded or failed, which exercises an app's branches and proves
nothing about the sensor.

**Calls, SMS and anything behind them, including 2FA.** Worth contrasting with
Android, where the emulator is the *only* thing that can be made to ring —
`adb emu` drives a call lifecycle and a real handset cannot be driven that way
from outside. The capability runs in opposite directions on the two platforms.

**The binary you ship.** `simctl install` takes a `.app` built against the
simulator SDK. The `.ipa` that goes to the App Store is device-signed and for a
different architecture, so nothing about signing, entitlements or store
packaging is exercised on a simulator. This is the reason
[decisions/0004](decisions/0004-an-app-under-test-of-our-own.md) could not
close the hybrid-app gap by installing somebody's shipped app.

The Android emulator has its own list — no Bluetooth, NFC, SD card
insert/eject, attached headphones or USB — and it is a different list, which is
the point. Neither substrate is a subset of the other.

The background, and the argument for which stage of a pipeline each device
belongs to, is in [*Emulator vs Simulator vs Real
Device*](https://medium.com/@begunova/emulator-vs-simulator-vs-real-device-15ce1dd5babf).

---

## iOS real device

Verified on an iPhone 15 Plus running iOS 26.6.2, over USB, with Xcode 26.6,
on 2026-09-22. Native screens are driven exactly as on a simulator —
`map`, `tap`, `type`, `scroll-to`, `wait`, `screenshot`, `launch`,
`terminate`, `apps`, `uninstall`, `press home`.
[checks/ios-device.sh](checks/ios-device.sh) is the flow, end to end.

### On the phone, once

1. **Connect it with a cable, unlock it, tap Trust This Computer** and enter
   the passcode.
2. **Open Xcode → Window → Devices and Simulators** with the phone connected,
   and wait while Xcode prepares it. The next step's switch does not exist
   until this has happened.
3. **Settings → Privacy & Security → Developer Mode → on.** The phone
   restarts and asks you to confirm.
4. **Settings → Developer → Enable UI Automation → on.** Without it
   WebDriverAgent installs, launches, and fails with "Timed out while enabling
   automation mode", which does not say which switch.
5. **Settings → Display & Brightness → Auto-Lock → Never** while you work, or
   long enough that it will not lock mid-run. Low Power Mode forces 30 seconds
   and grays the setting out.

### On the Mac, once

**Xcode → Settings → Accounts → add an Apple ID.** A free one works. Then
Manage Certificates → + → Apple Development, if Xcode has not made one.

That is all. Mobium reads your development team from the certificate — its
OU field is the team id — and on the first command against the phone it
downloads WebDriverAgent's pinned, checksummed source, builds it signed for
your team and the phone, and starts it. About 20 seconds when that was
measured; a minute or two is not unusual. macOS may ask once whether
`codesign` may use the key: **Always Allow**, or it asks for every file it
signs, every build.

If the keychain holds certificates for more than one team, Mobium refuses to
guess: set `MOBIUM_IOS_TEAM=<team id>`.

```sh
mobium devices                                   # the phone is listed as "ios device"
mobium --backend webdriveragent --device <udid> map
```

With exactly one iOS device available — one booted simulator, or one
connected phone — `--device` can be left out.

### How long a signature lasts

Apple documents seven days for a profile made with a free Apple ID. The one
Xcode made for the account this was verified with expires on 2027-09-23, a
year out, so check yours: when it lapses the runner stops launching, and
removing `~/.mobium/webdriveragent-device/` makes Mobium build a fresh one.

### What a phone cannot do

These are done by `simctl` on a simulator and `devicectl` has no equivalent,
so on a phone they are refused, each with the reason, rather than
approximated: **permissions, appearance, the clipboard, simulated location
and routes.**

### WebViews and Safari

They work on a phone, reached through the same lockdown service Appium uses
and speaking the same protocol as a simulator's
([decisions/0002](decisions/0002-ios-webviews-are-reachable.md)). It needs
the phone connected by cable — usbmuxd, which carries it, is USB — and:

- **an app's WebView** is inspectable only if the app sets
  `isInspectable = true`, on a phone exactly as on a simulator. Wikipedia's
  App Store build does not, and its articles are read through the native tree
  instead, where their links are mapped;
- **Safari** needs Settings > Apps > Safari > Advanced > **Web Inspector** on
  — Apple's documented switch for remote inspection, and on a simulator the
  measured one (its equivalent `defaults` key). On the phone this was
  verified with it on; it has not yet been measured with it off. Remote
  Automation, on the same page, is Apple's switch for `safaridriver`, which
  Mobium does not use.

Tapping inside Safari's page is refused for the reason it is on a simulator:
the page cannot say where it sits under Safari's own chrome (CHALLENGES 47).
Reading, mapping and `eval` work.

### Slower, and one stall to know about

A phone reads a screen in about 2 seconds for Settings and 6 for the home
screen, against a fraction of a second on a simulator; a screenshot takes
0.5s. Prefer `map` to `screenshot` for the same reason as on Android.

**The first read after switching apps can stall for a minute and return the
app you just left** ([CHALLENGES 71](CHALLENGES.md)). Mobium prevents it for
the switches it makes itself — `launch` and `press home` — by telling
WebDriverAgent which app is coming. A tap that opens another app gives no
such warning, so after one, a slow first read is that and not a hang.

---

## What Mobium installs, and removing it

On the machine, under `~/.mobium`:

| | |
| --- | --- |
| `uiautomator2/`, `webdriveragent/` | pinned, checksummed device agents, ~40MB, downloaded on first use |
| `webdriveragent-device/` | WebDriverAgent's pinned source and one signed build per team and phone, ~150MB — only once a real iPhone has been driven |
| `daemon/` | socket and PID file, removed on shutdown |

On an **Android** device, the default backend installs two APKs —
`io.appium.uiautomator2.server` and `io.appium.uiautomator2.server.test`.
Nothing else, and nothing at all with `--backend uiautomator`.

On an **iOS simulator**, `com.facebook.WebDriverAgentRunner.xctrunner`.

On a **real iPhone**, `dev.mobium.wda.<team>.xctrunner`, signed for your team.
Mobium stops it when the session ends; `mobium --device <udid> uninstall
dev.mobium.wda.<team>.xctrunner` removes it. On the phone, also turn Enable UI
Automation back off and restore Auto-Lock.

To leave a device exactly as you found it:

```sh
mobium daemon stop                        # release the session first
mobium --device <serial> uninstall io.appium.uiautomator2.server.test
mobium --device <serial> uninstall io.appium.uiautomator2.server
mobium --device <serial> apps             # should list neither
```

Mobium reinstalls them automatically on the next run, so removing them costs
nothing but the reinstall.

Finishing a session cleanly — and the order that avoids orphaning a device
session — is [SHUTDOWN.md](SHUTDOWN.md).

Nothing is left in the device's filesystem. The dump backend writes a
hierarchy file to `/data/local/tmp` and deletes it after every read — that
file is a serialization of whatever is on screen, so on a real phone it is
your data, and it is not left behind. Verify with:

```sh
adb -s <serial> shell ls /data/local/tmp
```
