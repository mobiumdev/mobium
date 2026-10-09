# Setting up devices

Everything Mobium needs on the machine and on the device, for all four kinds
of target. Written from doing it: every trap below cost real time here, and
most of them report something that names the wrong cause.

**Run `mobium doctor` first.** It checks the whole list without needing a
device, and exits non-zero if it found something:

```
ok    adb                    Android Debug Bridge version 1.0.41 at /opt/homebrew/bin/adb
note  android sdk root       ANDROID_SDK_ROOT is not set — only needed to start emulators, not to drive them
note  android devices        none running — start an emulator or plug in a phone
ok    xcode                  /usr/bin/xcrun
note  ios simulators         11 available, none booted — `xcrun simctl boot <udid>`
ok    daemon socket          ~/.mobium/daemon/mobium.sock (46 bytes of ~104)
ok    device agents          cached: uiautomator2, webdriveragent
note  third-party drivers    none on PATH — a driver is an executable named mobium-driver-<name>, used with --driver <name>

everything mobium needs is here
```

`note` means something is missing that is only needed for part of the job —
no Xcode on a machine doing Android work is normal, not a problem.

---

## Contents

- [Installing a release](#installing-a-release) — no Go needed, from the first release
- [Android emulator](#android-emulator)
- [Android real device](#android-real-device)
- [iOS simulator](#ios-simulator) — and [an Apple TV simulator](#an-apple-tv-simulator)
- [iOS real device](#ios-real-device) — native screens, WebViews and Safari
- [Parallel runs](#parallel-runs) — one daemon for each
- [Driving another machine's devices](#driving-another-machines-devices) — `--remote`, and a grid
- [What Mobium installs, and removing it](#what-mobium-installs-and-removing-it)

---

## Installing a release

From the first tagged release on, each one carries mobium prebuilt for
macOS, Linux and Windows, on amd64 and arm64, so installing it needs no Go:
`mobium_<version>_<os>_<arch>.tar.gz` (`.zip` for Windows), each holding the
binary, the license and its notices, and the README, and `SHA256SUMS` over
all of them. The binary is static — nothing else to install with it.

```sh
v=0.1.0; os=darwin; arch=arm64          # or linux / windows, amd64
base=https://github.com/mobiumdev/mobium/releases/download/v$v
curl -LO "$base/mobium_${v}_${os}_${arch}.tar.gz" -LO "$base/SHA256SUMS"
shasum -a 256 -c --ignore-missing SHA256SUMS   # must say OK
tar -xzf "mobium_${v}_${os}_${arch}.tar.gz"
mv "mobium_${v}_${os}_${arch}/mobium" /usr/local/bin/   # or anywhere on PATH
mobium --version
```

**On macOS, download with `curl`, not a browser.** The binaries are not
notarized — that needs an Apple Developer Program membership — and macOS
blocks a quarantined, un-notarized download from a browser at first launch.
`curl` does not quarantine what it saves. For a copy that came through a
browser, `xattr -d com.apple.quarantine mobium` lifts it, after checking its
checksum.

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

Or let Mobium start it: `mobium boot mobium-test` cold-boots the AVD headless
(`--window` to see it) and answers with its serial once it has booted, and
`mobium shutdown emulator-5554` ends the session on it and shuts it down.

`sdkmanager` and `avdmanager` need a JDK. If they fail with a Java error:

```sh
export JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home
```

### On Linux

Google ships the command-line tools as a zip, and the emulator and
`platform-tools` for Linux **only for x86_64** — the SDK repository has no
Linux arm64 build of either (checked 2026-09-28) — so an arm64 Linux machine
cannot run the emulator. The emulator needs KVM: your user must be able to
open `/dev/kvm` (usually by being in the `kvm` group).

```sh
mkdir -p ~/android-sdk/cmdline-tools && cd ~/android-sdk/cmdline-tools
curl -LO https://dl.google.com/android/repository/commandlinetools-linux-16111833_latest.zip
unzip commandlinetools-linux-16111833_latest.zip && mv cmdline-tools latest

export ANDROID_HOME=~/android-sdk
export ANDROID_SDK_ROOT=$ANDROID_HOME
export PATH="$ANDROID_HOME/cmdline-tools/latest/bin:$ANDROID_HOME/emulator:$ANDROID_HOME/platform-tools:$PATH"

sdkmanager --install "emulator" "platform-tools" \
  "system-images;android-35;google_apis;x86_64"

avdmanager create avd -n mobium-test -d pixel_7 \
  -k "system-images;android-35;google_apis;x86_64"

emulator -avd mobium-test -no-snapshot-save -no-boot-anim &
adb wait-for-device
```

Run that way on Ubuntu 24.04 on 2026-09-28, with a JDK 17, and every
quick-start page passed. Three things it showed:

- **`sdkmanager` says it is deprecated** and hands over to the new Android
  CLI (`android sdk`), downloading it on first use. It still installs what it
  is asked for; `--licenses` is no longer needed.
- **The Pixel 7 profile needs 12GB free** for its data partition. With less,
  `emulator` exits at once with `Not enough space to create userdata
  partition` — in its log, not on the terminal — and `adb wait-for-device`
  waits forever for a device that is not coming.
- **On two cores it is slow the first time.** The emulator warns that it
  wants four. On a freshly booted one, the first tap into Network & internet
  took 10.5 seconds to open the screen — past the quick start's 10-second
  wait once in two runs — and 4.3 seconds later on; on a Mac it takes under
  one.

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
mobium shutdown emulator-5554 && mobium boot mobium-test --window  # headed
mobium shutdown emulator-5554 && mobium boot mobium-test           # headless
```

`mobium boot` cold-boots, passing `-no-snapshot-load`. `mobium shutdown` ends
only its own daemon's session, so stop any other session on the device first
(`docs/SHUTDOWN.md`).

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

Then pass `--device emulator-5556` to every command, or run each one under
its own `MOBIUM_SESSION`, or give a client the device option.

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

### With a screen reader on

Android's test-automation connection silences every other accessibility
service while it is open, unless it is opened not to. So on a device with
TalkBack — or VoiceView, on a Fire TV — on, a read of the screen takes the
screen reader away for as long as it runs, and an app that publishes its
contents only to a screen reader stops publishing them (CHALLENGES 208).

Both drivers leave the screen reader running. The default, UiAutomator2,
opens its connection that way; `--driver uiautomator` reads through a reader
of Mobium's own whenever an accessibility service is enabled. With TalkBack
on, taps, scrolls and typing still land: Mobium's touches are injected, not
explored. Nothing in the device's settings changes either way.
`docs/checks/screen-reader.sh` checks it on an emulator.

`MOBIUM_DUMP_READER=mobium` uses Mobium's reader for every read, and
`=uiautomator` uses `uiautomator dump` for every read. **It is read by the
daemon**, so it takes effect only if it is set when the daemon starts — set
on a later command, it does nothing. Run `mobium daemon stop` first.

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

**A Fire TV, or anything older than Android 11**, has no pairing: Developer
Options → ADB Debugging on (on a Fire TV, Developer Options appears under
Settings > Device & Software after selecting the device name seven times
in About; older Fire OS calls that menu My Fire TV),
then `adb connect <tv>:5555` with the TV's IPv4 address, and accept the prompt
on the screen. Two things go wrong there, and neither says so:

- **"No route to host" while ping works is macOS, not the network.** An adb
  server started by a background process — Mobium's daemon, or an agent's
  shell — is blocked from the local network by macOS's Local Network
  privacy without a prompt. `adb kill-server && adb connect <tv>:5555` from
  a Terminal window makes macOS ask; after that it works from anywhere until
  the server is killed again.
- **The link drops** every few minutes on the Fire TV measured,
  `error: closed` then `device offline`, in latency spikes of several
  seconds with no packet lost. `adb connect` alone leaves an offline device
  offline; `adb disconnect` first. Mobium does both by itself before it
  answers that a network device is missing or offline, and `mobium test`
  waits out a step that never reached the device
  ([the test runner guide](guides/test-runner.md#a-device-that-drops-off-and-comes-back)).

Stopping one cleanly, ADB debugging included, is
[SHUTDOWN.md](SHUTDOWN.md#a-tv-or-anything-reached-by-adb-connect).

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
mobium --device <udid> map
```

`mobium boot "iPhone 17 Pro"` boots one by name or UDID and waits until it
has, and `mobium shutdown "iPhone 17 Pro"` ends the session and shuts it down.

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

WKWebViews work, over WebKit's Remote Web Inspector rather than CDP — a
Unix socket on a simulator, carrying binary property lists. Two
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
nothing about the sensor. `mobium biometric` does the telling — and on an
Android emulator, a fingerprint's — and a real phone refuses it.

**Calls, SMS and anything behind them, including 2FA.** Worth contrasting with
Android, where the emulator is the *only* thing that can be made to ring —
`adb emu` drives a call lifecycle and a real handset cannot be driven that way
from outside. The capability runs in opposite directions on the two platforms.

**The binary you ship.** `simctl install` takes a `.app` built against the
simulator SDK. The `.ipa` that goes to the App Store is device-signed and for a
different architecture, so nothing about signing, entitlements or store
packaging is exercised on a simulator. This is also why somebody's shipped
app could not close the hybrid-app gap here, and
[MobiumApp](https://github.com/mobiumdev/mobium-app) was built instead.

The Android emulator has its own list — no Bluetooth, NFC, SD card
insert/eject, attached headphones or USB — and it is a different list, which is
the point. Neither substrate is a subset of the other.

The background, and the argument for which stage of a pipeline each device
belongs to, is in [*Emulator vs Simulator vs Real
Device*](https://medium.com/@begunova/emulator-vs-simulator-vs-real-device-15ce1dd5babf).

### An Apple TV simulator

Verified on a tvOS 26.5 simulator (Apple TV 4K, 3rd generation, at 1080p)
with Xcode 26.6, on 2026-10-09. The tvOS platform is a separate download:

```sh
xcodebuild -downloadPlatform tvOS      # or Xcode > Settings > Components
xcrun simctl create "Apple TV" \
  com.apple.CoreSimulator.SimDeviceType.Apple-TV-4K-3rd-generation-1080p \
  com.apple.CoreSimulator.SimRuntime.tvOS-26-5
xcrun simctl boot <udid> && open -a Simulator
```

`mobium devices` lists it as an `apple tv simulator`, and the first command
installs WebDriverAgent's tvOS runner, from the same pinned release as the
iOS one and checked the same way. It is driven with the remote, as a Fire TV
is: `press dpad-up`, `dpad-down`, `dpad-left`, `dpad-right` and `select`,
each D-pad press reported by where focus went; `back` is the remote's Menu
button, and `play-pause` is the Siri Remote's only media key. There is no
touch screen, so `tap` and every other gesture are refused, and so are
`alert accept` and `dismiss`: an alert opens with focus on its cancel button
and is answered by moving focus and pressing select.
[checks/tv-app.sh](checks/tv-app.sh) drives
[MobiumTV](https://github.com/mobiumdev/mobium-app/tree/main/tvos) on it,
the same check as on an Android TV.

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
mobium --device <udid> map
```

A device named by `--device` needs no `--driver`: an iPhone or a simulator
is driven by `wda`, the only driver either has.

With exactly one iOS device available — one booted simulator, or one
connected phone — and no Android device connected, `--device` and
`--driver` can both be left out: a call that names neither goes to the only
device there is. Android stays the default when any Android device is
connected, so with both platforms connected, name the device. With several
iOS devices and no Android one, a call that names none is refused, listing
them.

### How long a signature lasts

Apple documents seven days for a profile made with a free Apple ID. The one
Xcode made for the account this was verified with expires on 2027-09-23, a
year out, so check yours: when it lapses the runner stops launching, and
removing `~/.mobium/webdriveragent-device/` makes Mobium build a fresh one.

### What a phone cannot do

These are done by `simctl` on a simulator and `devicectl` has no equivalent,
so on a phone they are refused, each with the reason, rather than
approximated: **permissions, appearance, the clipboard, simulated location
and routes, and notifications** — reading or posting them.

**Clearing an app's data takes its bundle on a phone.** Nothing can delete
from an app's container there, so `mobium clear-data <app> --bundle
<App.app or .ipa>` uninstalls the app and installs it again, which empties
its container, read back, and resets its permissions. Installing over the
app without uninstalling would keep both.

**Screen recording works on a phone**, differently: the frames come from
WebDriverAgent's screen stream, about ten a second whether or not anything
moves, and are written into the MP4 on the Mac as JPEG images rather than
as H.264. Apple's AVFoundation, which QuickTime Player is built on, opens
and decodes it (measured); browsers are not known to play JPEG video in an
MP4, and none has been tried.

### WebViews and Safari

They work on a phone, reached through the phone's lockdown service and
speaking the same protocol as a simulator's. It needs
the phone connected by cable — usbmuxd, which carries it, is USB — and:

- **an app's WebView** is inspectable only if the app sets
  `isInspectable = true`, on a phone exactly as on a simulator. Wikipedia's
  App Store build does not, and its articles are read through the native tree
  instead, where their links are mapped;
- **Safari** needs Settings > Apps > Safari > Advanced > **Web Inspector** on
  — Apple's documented switch for remote inspection, and on a simulator the
  measured one (its equivalent `defaults` key). On the phone this was
  verified with it on; it has not yet been measured with it off. Remote
  Automation, on the same page, is Apple's switch for WebDriver automation
  of Safari, which Mobium does not use.

Tapping inside Safari's page was refused on a phone as on a simulator: the
page cannot say where it sits under Safari's own chrome (CHALLENGES 47).
Since CHALLENGES 248 that is read from text the page and the WebView both
report, which places a tap exactly on a phone as on a simulator: on the
iPhone 15 Plus a tap on example.com's "Learn more" opened the page it links
to. Reading, mapping and `eval` work either way.

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

## Parallel runs

**Give each run that drives a device at the same time as another its own
daemon.** One daemon serves one call at a time, across every device it
holds, so two runs sharing one go at the pace of the slower device.
Measured on 2026-09-27, 15 `map` calls on each of an Android 15 emulator and
an iPhone 17 Pro simulator at once:

| | the emulator's 15 calls |
| --- | --- |
| alone | 0.3s |
| sharing a daemon with the simulator's | 22.3s |
| on a daemon of its own, while the simulator's ran on another | 0.3s |

A daemon is named by `MOBIUM_SESSION`, and every client can set it for the
connection it opens:

| | |
| --- | --- |
| CLI | `MOBIUM_SESSION=android-run mobium map` |
| Python | `mobium.start(platform="android", session="android-run")` |
| JavaScript | `start({ platform: 'android', session: 'android-run' })` |
| Go | `mobium.Start(ctx, mobium.WithPlatform("android"), mobium.WithSession("android-run"))` |
| Java | `Mobium.builder().platform("android").session("android-run").start()` |
| .NET | `Device.Builder().Platform("android").Session("android-run").Start()` |

The option wins over a `MOBIUM_SESSION` already in the environment. Keep the
name short: it is part of a socket path, and the OS caps those at about 104
bytes. And one device belongs to one run at a time — two daemons driving the
same device invalidate each other's device-side session (above, under
"Headed and headless").

**Two simulators at once work.** Every simulator's WebDriverAgent listens on
the Mac itself, and until 2026-09-28 all of them on port 8100, so a second
simulator's daemon reached the first simulator's runner and drove it while
reporting it as its own. Each runner is now given free ports of its own when
it starts; two iPhone simulators on two daemons, ten `map` calls each at
once, each saw only its own screen. Nothing needs setting for it
(CHALLENGES 148).

**Android never shared a port.** Its UiAutomator2 server listens on each
device's own loopback, and mobium reaches it through `adb forward tcp:0`,
for which adb picks a free port on the Mac per device — as it does for each
WebView. Measured on 2026-09-28 with two emulators on two daemons: forwards
on host ports 52360 and 52527, both to their own device's 6790, and ten
`map` calls on each at once, each seeing only its own screen.

## Driving another machine's devices

**`--remote <node>`, or `MOBIUM_REMOTE=<node>`, drives the devices plugged
into another machine**, over SSH. Every command takes it, and so does
`mobium pipe`, which is how all five clients connect — so a client goes
remote with the environment variable alone, and no change to its code.

What happens: mobium asks the node, over SSH, to have a daemon running and
say where it listens (`mobium daemon up`); forwards that socket to one in a
private directory here, owner-only; and sends every call through it. Nothing
new listens on a network — SSH authenticates and encrypts, and the node's
daemon socket stays owner-only on the node. Files travel as content
(`MOBIUM_FILES=content`, set for you): `install`'s app and a GPX route go to
the node, and a screenshot or a recording comes back and is saved where you
asked. If the node's daemon stops answering, the call fails; a daemon is
never started here in its place, which would drive this machine's devices
under the node's name.

What it needs:

| | |
| --- | --- |
| **SSH without a prompt** | key-based login to the node; mobium runs SSH with `BatchMode=yes`, because a prompt would land in the stream a client speaks. `MOBIUM_SSH` replaces the `ssh` command, options included — `ssh -i ~/.ssh/node_key` |
| **mobium on the node** | reachable by the node's non-interactive shell. `MOBIUM_REMOTE_BIN` is what that shell runs as mobium, and may set its environment — `PATH=/opt/homebrew/bin:$PATH ~/bin/mobium` — when adb or Xcode's tools are not on the default `PATH` there |
| **macOS or Linux here** | the forward ends in a Unix socket; on Windows `--remote` refuses, and says so |

Commands about this machine stay here: `daemon`, `doctor` and `mcp` ignore
`--remote`. One device still belongs to one run at a time, on the node as
anywhere. Verified with this Mac standing in for a node: the CLI and an
unchanged Python client ran sessions, maps, installs, screenshots and
recordings on the node's daemon, every file landing on the caller's side.

### A grid

**`MOBIUM_GRID=node1,node2` spreads runs over several machines' devices**,
with no hub. At a run's first call — which is where it
says what it wants: a serial, or `platform` when it starts a session — mobium
asks every node over SSH for its devices and which of them are held, takes
the first free one that matches, and connects to its node as `--remote`
would. The lease that keeps a device to one run lives on the node, so runs
started on different machines cannot take the same device. With nothing free
that matches, a run waits, asking again every two seconds, up to
`MOBIUM_GRID_WAIT` (default `60s`), and then says what was busy and which
nodes did not answer. A node that does not answer within five seconds is
left out, and routing goes on without it.

`MOBIUM_GRID_MODEL` narrows it to a model — part of its name, in any case —
and `MOBIUM_GRID_OS` to an OS version, matched at the start of a word, so
`17` means Android 17 and its point releases and `iOS 26` an iOS 26 runtime.

**The node enforces a lease; mobium does not merely respect it.** Each run
gets a daemon of its own on its node, named by its lease, and every daemon
on the node refuses a device leased to another: a `mobium --device` run on
the node that goes around the grid is told the device belongs to a grid run
until that run ends. The run's daemon stops with it.

**A node lends its emulators and simulators, and a phone only when it says
so.** A phone plugged into a node is usually somebody's, and a grid run on it
would install, tap and change settings; so a physical device is routed to
only when the node's own environment sets `MOBIUM_GRID_PHONES=1` — the
node's choice, never the caller's. Until then it is listed as not offered,
and a run that asks for it by serial is told so.

**`mobium grid status`** prints each node's devices — platform, OS, model,
state — who holds each and for how long, the nodes not answering, and the
runs waiting. **`mobium grid ui`** serves the same as a page, refreshed every
few seconds, on `127.0.0.1` only. Both read the nodes' own answers, the ones
routing reads, so neither can disagree with where runs go.

```
$ mobium grid status
NODE      DEVICE         PLATFORM  OS          MODEL               STATE      HELD BY
lab-mac   emulator-5554  android   Android 15  sdk_gphone64_arm64  device     g5c1e9a07 for 42s
lab-mac   emulator-5556  android   Android 17  sdk_gphone64_arm64  device     free

waiting:
  g0d4f2b11 wants an android device, OS 15, waiting 9s (seen by lab-mac)
```

A lease is renewed every 20 seconds while its run lives, released when it
ends, and free again 60 seconds after a run that died without releasing it.
A run killed outright takes its SSH forward with it, and the next run clears
what it left on this machine. Each CLI command is a run of its own, so a
series of them may land on different devices: a client, whose run lasts as
long as it does, is the way to keep one.

Verified with this Mac standing in for a node with two emulators, and a
second node that does not exist: three Python clients asking for Android at
once got the two emulators, the third waited until one was released and got
it, and no lease, forward or directory was left.

## What Mobium installs, and removing it

On the machine, under `~/.mobium`:

| | |
| --- | --- |
| `uiautomator2/`, `webdriveragent/` | pinned, checksummed device agents, ~40MB, downloaded on first use |
| `webdriveragent-device/` | WebDriverAgent's pinned source and one signed build per team and phone, ~150MB — only once a real iPhone has been driven |
| `daemon/` | socket and PID file, removed on shutdown |

On an **Android** device, the default backend installs two APKs —
`io.appium.uiautomator2.server` and `io.appium.uiautomator2.server.test`.
Nothing else, and nothing at all with `--driver uiautomator`.

On an **iOS simulator**, `com.facebook.WebDriverAgentRunner.xctrunner`.

On a **real iPhone**, `dev.mobium.wda.<team>.xctrunner`, signed for your team
and named MobiumWDA-Runner. Another tool that installs its own WebDriverAgent
removed every runner named WebDriverAgentRunner-Runner before installing its
own, and left this one alone (measured, CHALLENGES 189). If it
is removed anyway, the next session says so as it installs it again.
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
your data, and it is not left behind. With a screen reader on, it pushes its
own reader to `/data/local/tmp/mobium-reader` instead, and deletes the folder,
with the copy Android compiles into it, after every read. Verify with:

```sh
adb -s <serial> shell ls /data/local/tmp
```
