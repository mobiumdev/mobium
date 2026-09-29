# Quick start

For people who want to use Mobium: from nothing to a script that starts a
session on a device, launches an app, taps something, takes a screenshot and
quits — in the language you use. Contributors want
[DEVELOPMENT.md](../DEVELOPMENT.md).

| After start | After the tap |
| --- | --- |
| ![Android Settings, as start left it](images/android-1-start.jpg) | ![Network & internet, opened by the tap](images/android-2-tapped.jpg) |
| ![iOS Settings, as start left it](images/ios-1-start.jpg) | ![General, opened by the tap](images/ios-2-tapped.jpg) |

Pick your client once the steps below are done:

| [Command line](cli.md) | [Python](python.md) | [JavaScript](javascript.md) | [Go](go.md) | [Java](java.md) | [.NET](dotnet.md) |
| --- | --- | --- | --- | --- | --- |

Every page runs the same program, and every one was run, unchanged, on an
Android 15 emulator and an iOS 26.5 simulator; the output shown on each is
what it printed.

## Contents

- [Where it runs](#where-it-runs)
- [1. Install mobium](#1-install-mobium)
- [2. Start a device](#2-start-a-device)
- [3. Check the setup](#3-check-the-setup)
- [4. Start, work, quit](#4-start-work-quit)
- [Next](#next)
- [When something goes wrong](#when-something-goes-wrong)

## Where it runs

| | Android emulator or phone | iOS simulator or iPhone |
| --- | --- | --- |
| **macOS** | Yes | Yes — needs Xcode |
| **Linux** | Yes — every page below run on Ubuntu 24.04, x86_64, against an Android 15 emulator. [SETUP.md](../SETUP.md#on-linux) has the emulator's Linux steps | No — iOS needs Xcode, which runs only on macOS |
| **Windows** | Not yet: the code is written and compiles, and has not run on Windows. See [WINDOWS.md](../WINDOWS.md) | No — no Xcode |

## 1. Install mobium

Until the first release there are no prebuilt binaries: mobium installs with
**Go 1.24 or later** ([go.dev/dl](https://go.dev/dl/)), and needs nothing else.

```sh
go install github.com/mobiumdev/mobium/cmd/mobium@latest
mobium --version
```

It lands in `$(go env GOPATH)/bin`; make sure that is on your `PATH`.

Each client page says how to install that client. Python (with `git`) and Go
install straight from GitHub today; JavaScript, Java and .NET need a clone of the
repository until their first release on npm, Maven Central and NuGet.

**Working on Mobium itself** — building from source, running the tests,
sending a change? That is [DEVELOPMENT.md](../DEVELOPMENT.md), not this page.

## 2. Start a device

One device is enough. Start any of these.

### Android emulator (macOS, Linux)

Install [Android Studio](https://developer.android.com/studio) or the
command-line tools, then create a virtual device (Device Manager in Android
Studio, or `avdmanager`) and boot it:

```sh
emulator -avd <name> &
adb wait-for-device
```

`adb` comes with the SDK's platform-tools; put it on your `PATH`. On Linux the
emulator needs KVM. [SETUP.md](../SETUP.md#android-emulator) has the exact
commands and two traps whose errors name the wrong cause.

### Android phone

Turn on **Developer options** (tap Build number seven times, in About phone),
then **USB debugging**, plug the phone in and accept the prompt on it.
`adb devices` should list it as `device`. [SETUP.md](../SETUP.md#android-real-device)
covers Wi-Fi and what each failure means.

### iOS simulator (macOS)

Install Xcode from the App Store, open it once, and add an iOS simulator
runtime (Settings → Components). Then boot a simulator:

```sh
xcrun simctl list devices available | grep iPhone    # pick one
xcrun simctl boot "iPhone 17 Pro"
open -a Simulator                                    # to watch it; optional
```

### iPhone (macOS)

It works with a free Apple ID, but takes a few one-time steps on the phone and
in Xcode: [SETUP.md](../SETUP.md#ios-real-device). The first session builds
WebDriverAgent for your phone, which takes a few minutes.

## 3. Check the setup

```sh
mobium doctor     # checks everything mobium needs; names the fix for anything missing
mobium devices    # lists what is attached
```

```
$ mobium devices
emulator-5554                          device     (android emulator, model: sdk_gphone64_arm64)
457C7DC2-C706-45D9-8D68-1D26953E28B1   booted     (ios simulator, iPhone 17 Pro, iOS 26.5)
…                                      shutdown   (every other simulator Xcode has)
```

## 4. Start, work, quit

Every client does the same three things, as an Appium script does:

| | Start a session | Quit it |
| --- | --- | --- |
| Command line | `mobium session start --platform android --app com.android.settings` | `mobium session end` |
| Python | `start(platform="android", app="com.android.settings")` | `device.quit()` |
| JavaScript | `await start({ platform: 'android', app: 'com.android.settings' })` | `await device.quit()` |
| Go | `mobium.Start(ctx, mobium.WithPlatform("android"), mobium.WithApp("com.android.settings"))` | `device.Quit(ctx)` |
| Java | `Mobium.builder().platform("android").app("com.android.settings").start()` | `device.quit()` |
| .NET | `Device.Builder().Platform("android").App("com.android.settings").Start()` | `device.Quit()` |

- **Start** starts the driver on the device — UiAutomator2 on Android,
  WebDriverAgent on iOS — and launches the app fresh: if it was running it is
  stopped first, so the session begins on the app's first screen. Its data is
  kept. `platform: "ios"` picks the iOS driver, so you never name one.
- **Everything between** uses that session: `map` lists what is on screen,
  each element with a ref such as `@e5`, and `tap`, `type`, `wait` and the rest
  act on refs or on locators such as `text=Internet`.
- **Quit** ends the session on the device and puts back anything it changed
  for the session, such as accessibility settings. Python's `with`, Java's
  try-with-resources and .NET's `using` quit for you when the block ends, even
  after an error; a second quit does nothing.

Start is never required: any call opens a session on first use. It exists so
the slow first start happens where you asked for it, and so a script says
plainly where its session begins and ends.

## Next

[The guides](../guides/README.md): what actions wait for and refuse, writing
and running tests with `mobium test`, and driving another machine's devices.

## When something goes wrong

- **"2 devices are available … pick one"** — `start` refuses to guess. Name
  one: `MOBIUM_DEVICE=<serial or UDID>` for the examples, `--device` on the
  command line, or the client's `device` option. `mobium devices` lists them.
- **The first start seems stuck** — it installs the UiAutomator2 server on
  Android (about 40MB, once) and prints `waiting for the UiAutomator2 server to
  start...`. On a real iPhone the first one builds WebDriverAgent, which takes
  minutes.
- **"the iOS driver is called "wda""** — you passed the old name,
  `webdriveragent`. Use `wda`, or just `platform: "ios"`.
- **Anything else** — `mobium doctor` checks the list of known setup problems
  and names the fix. [SETUP.md](../SETUP.md) has the rest.
