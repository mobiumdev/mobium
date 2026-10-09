# Windows support

**Status: not supported — everything but a device is verified.** Android
development on Windows is common, so this is a real gap rather than a
theoretical one. Since 2026-09-27 CI runs on a GitHub-hosted Windows runner:
both modules' tests pass there, the named-pipe transport's acceptance tests
below pass five times a run, and the built `mobium.exe` answers `doctor` and
`mcp` and auto-starts, reports and stops its daemon. Getting there found four
defects, CHALLENGES 139–142 — a daemon stop that never returned among them.
No emulator or phone has been driven from Windows, and until one has, Windows
stays unsupported.

Everything, the daemon transport now included, compiles, vets and
cross-compiles for `windows/amd64`. This document is written for anyone who
wants to try it on a Windows machine, and for picking the work up there.

## Android only. iOS is not possible on Windows.

Said here rather than in a footnote, because it is the first question anyone
asks and the answer is structural rather than a gap in the work.

Mobium drives iOS through `xcrun simctl` — 41 call sites in `internal/device`
alone — and through WebDriverAgent, which it installs as the prebuilt
`WebDriverAgentRunner-Build-Sim-arm64.zip`. `simctl` ships inside Xcode and
Xcode is macOS-only; the artifact is a *simulator* build for Apple Silicon,
and a simulator is a macOS process. There is no configuration, no port and no
extra download that changes any of that. Nothing else in this document would
help, and no amount of Windows work would move it.

So on Windows the target is **Android: emulators and real devices**, which is
the common case anyway — Android development on Windows is ordinary, and iOS
development on Windows is not a thing that exists. What `mobium devices`
should do on a machine with no Xcode is list the Android devices and add a
*note* about iOS rather than fail, and confirming that is item 4 in "What to
try".

A real iPhone attached to a Windows machine does not change the answer either.
Reaching it needs WebDriverAgent built and signed, which is `xcodebuild`
on a Mac — how Mobium does it since 2026-09-22 — and the tunnel it is reached
over is CoreDevice's, which is Xcode's. A Mac-built `.ipa` installed from Windows by a third-party tool is a
route other projects take and is not this project's architecture today; it
would be a driver, not a flag.

Everything that needs no device is verified in CI on Windows, `mobium mcp`
answering `tools/list` included (the `go-windows` job). What has never run is
a tool call against a device — and everything this project has learned says
that unverified means wrong. The list below is ordered so the first hour on a
Windows machine is spent finding out what is broken rather than setting up.

**The first thing to do on Windows is not to write code.** It is to run
`mobium mcp` and a client against a connected Android device and find out
whether the claim above — that the MCP path already works on Windows — holds
for a tool call that reaches a device. That has never been run. If it holds,
Windows users have a working tool today and the daemon is an improvement
rather than a rescue; if it does not, the gap is much bigger than a transport
and the plan changes.

---

## What is expected to work already

Never run against a device on Windows — that is the point of this exercise —
but these have no platform-specific code:

- the tool layer, the driver interface, the tree parser, locators and refs
- `adb` discovery and every Android command, including port forwarding
- the UiAutomator2 backend, its APK download and checksum verification
- WebView contexts over CDP
- **`mobium mcp`**, which runs the tool layer in-process on stdio and never
  touches the daemon socket

The last one matters: **`mobium mcp` should be fully usable on Windows today.**
CI shows it starts and answers `tools/list` there; whether a tool call reaches
a device is the unverified part. If it does, Windows users have a working tool
via MCP.

## The five gaps — verified in CI, without a device

Found by reading rather than running, **written on 2026-09-25**, and since
2026-09-27 run on a GitHub-hosted Windows runner: everything below compiles
and vets for `windows/amd64` and `windows/arm64`, and its tests pass on
Windows. None of it has yet carried a session to a device, which is the part
this project's record says is wrong until shown otherwise. The list is still a
starting point, not a complete inventory.

### 1. The daemon transport — `internal/daemon/listener_windows.go`, `dial_windows.go`
A named pipe through `github.com/Microsoft/go-winio`, as Vibium does it. It
adds the project's second and third dependencies after `gorilla/websocket`:
go-winio and `golang.org/x/sys`, both pure Go, so the single-binary property
survives. `x/sys` is held at v0.41.0, the last release that asks for Go 1.24;
anything newer raises the module's `go` line.

Three things differ from the sketch this document used to carry:

- **The pipe is owner-only by SID, not by `OW`.** The DACL is
  `D:P(A;;GA;;;<current user's SID>)`. An OWNER RIGHTS ACE would lock the
  user's own unelevated CLI out of a daemon started elevated, because an
  elevated process's objects are owned by Administrators.
- **A dial failure is wrapped in `*net.OpError`.** go-winio reports a missing
  pipe as `*os.PathError`, and `IsConnectionError` — which decides whether the
  CLI auto-starts a daemon — only knows the first. Unwrapped, a missing daemon
  would read as some other failure and never be started.
- **A second daemon on a live pipe fails to bind** rather than becoming a
  second instance of it: go-winio creates the first instance with
  `FILE_FLAG_FIRST_PIPE_INSTANCE`. `TestSecondListenerOnOnePipeIsRefused`
  asserts that, on Windows only.

### 2. `paths.SocketPath()` — a pipe name, keyed by `MOBIUM_HOME`
It returns `\\.\pipe\mobium-<12 hex>[-<session>]`, where the hex is a hash of
the absolute, lowercased state directory. **Vibium uses a bare
`\\.\pipe\vibium`, and copying it would have been a bug here**: a pipe is
machine-wide, where a Unix socket lives inside `MOBIUM_HOME`, so every home —
two test runs, two users — would share one daemon. The `sun_path` length check
does not apply and is skipped. `TestPipeNameIsPerHomeAndSession` runs on every
platform and was shown to fail when the hash ignores the directory.

### 3. `daemon.Running(pid)` — split into `running_unix.go` / `running_windows.go`
Signal 0 is unsupported on Windows, so the old check judged every daemon dead
and `CleanStale` would delete a live one's PID file. The Windows version opens
the process and reads its exit code, which must be `STILL_ACTIVE` —
**not** the `os.FindProcess` check this document first proposed, because an
exited process stays openable while anything holds a handle to it. Access
denied answers false, as EPERM does on Unix: a PID taken by another user's
process was reused.

### 4. `device.Detach` — `internal/device/detach_windows.go` (and `detach_unix.go`)
`CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS`: a Ctrl-C does not reach the
daemon, and with no console it does not die when the terminal window closes.

### 5. Test helpers — `t.TempDir()` on Windows
`internal/daemon/daemon_test.go` and `internal/device/wdainstall_network_test.go`
fall back to `t.TempDir()`. The daemon tests no longer look for a socket file:
`socket_unix_test.go` stats it and checks mode 0600, and
`socket_windows_test.go` dials the pipe and reads its DACL back, which must be
protected and hold exactly one entry, for the current user.

Also moved: removing a stale socket and setting its mode now happen inside
`listen` on Unix, and the socket is deleted through `removeSocket`, a no-op on
Windows — `daemon.go` used to `os.Chmod` the pipe name, which would have
failed every Windows start after the bind succeeded.

### The `exec.Command` audit — 2026-09-25

Every `exec.Command`, `exec.CommandContext` and `exec.LookPath` outside the
clients was read for `.exe` resolution and for paths with spaces.
Unverified, like everything else here.

- **Spaces and backslashes in arguments are fine.** Go builds the Windows
  command line with `syscall.EscapeArg`, which quotes for the parsing
  `adb.exe` does, and nothing is run through `cmd.exe`, so `%` and `^` mean
  nothing. `adb shell` arguments are quoted a second time for the *device's*
  shell by `shellQuote`, which is unaffected.
- **`.exe` resolution was already right for adb**: `exec.LookPath` applies
  `PATHEXT`, and the SDK fallback adds `.exe` itself.
- **The daemon re-executes itself** through `os.Executable()`, which returns
  the full path, `.exe` included, so a `C:\Program Files` install is fine.
- **iOS-only tools** — `xcrun`, `security`, `xcodebuild`, `plutil`, `ioreg` —
  are reached only on the iOS paths, which report the missing toolchain as a
  note.

Fixed, each with a test that runs on every platform:

- **`doctor` could not see a Windows driver.** It judged
  `mobium-driver-*` files by execute bits, which Windows does not have — Go
  reports every file as 0666 — so it said "none on PATH" for a driver that
  `--driver` would have run, and would have named one `x.exe` if it had seen
  it. It now uses `PATHEXT`, the rule `exec.LookPath` uses to find it.
- **Quoted environment paths.** `set ANDROID_HOME="C:\Program Files\Android"`
  in `cmd.exe` keeps the quotes as part of the value, and every lookup under
  it failed as if the SDK were missing. `device.EnvPath` drops surrounding
  quotes on Windows only, since a quote is a legal path character on Unix;
  adb discovery and `doctor`'s SDK root check both read through it.
- **`MOBIUM_ADB_PATH` without `.exe`** — the Unix spelling of the path — was
  refused, though `CreateProcess` would have run it. It is now retried with the
  suffix.
- **`doctor` measured the pipe name against the socket limit**, printing
  "`N bytes of ~104`" for a constraint that does not apply. It now says
  "named pipe", and the test for the limit skips on Windows.

A driver written as a script needs an `.exe` or a `.cmd` wrapper on Windows;
the driver guide in [examples/drivers](../examples/drivers/README.md) says
how.

### Also expected, unverified

- **iOS is macOS-only.** `simctl` does not exist on Windows. `mobium devices`
  should report that as a note rather than an error — the code path already
  does this for a missing toolchain, but it has never been exercised with
  Xcode genuinely absent on a non-Mac.
- Paths in `app_install` and `screenshot -o` take Windows separators; nothing
  obviously assumes `/`, but nothing has tested it either.

---

## Choosing a Windows host

What is missing is one thing: **an Android device driven from Windows**,
through the CLI, MCP and a client. Everything that needs no device already
passes on GitHub's Windows runner. So the question for a host is whether it can
reach a device, and how close it is to the Windows most users run. Weighed on
2026-10-08, from an Apple Silicon Mac:

| Host | Cost | Reaches a device | Close to most users | Verdict |
| --- | --- | --- | --- | --- |
| An x64 Windows laptop | the hardware | yes: an emulator with WHPX, and phones over USB | the closest | the target; waiting on the hardware |
| Windows 11 on ARM in a VM on the Mac (VMware Fusion or UTM) | the VM software is free; Windows needs a license to be used properly, though it installs and runs unactivated | a **real** device only: the Fire TV over `adb connect`, the Pixel over wireless debugging, or the Pixel's USB passed through to the VM. **No emulator**: that needs virtualization inside the VM, which a Windows guest does not get on Apple Silicon | partly: ARM64 Windows, running Mobium's `windows/amd64` build and x64 `adb` under its built-in emulation, or the native `windows/arm64` build | the free route to the first device run, today |
| GitHub-hosted Windows runner | free for this public repository | no: no emulator acceleration, and no way to reach a device | x64 | already used for everything device-free; keep it |
| A cloud Windows VM | free tiers are too small; an emulator needs nested virtualization on a paid size | only a device in the same cloud, or a device cloud | x64 | not worth it for one run |
| Microsoft's Windows development VMs | were free, 90-day | — | — | unavailable since October 2024 |

**Recommended order.** Do the first device run in a Windows 11 ARM VM against a
device over the network — the Fire TV needs nothing but `adb connect`, and a
Pixel needs wireless debugging — and record what it found. Then repeat it on an
x64 laptop with an emulator before calling Windows supported: an ARM VM
proves the transport and the tools layer on Windows, but not the x64 binary
running natively, the emulator's WHPX path, or USB drivers on a typical
machine.

**What each run must record:** the Windows build and architecture, which
Mobium build (`windows/amd64` under emulation or `windows/arm64`), how the
device was reached (`adb connect`, wireless debugging, USB passthrough, USB),
and the steps under [What to try](#what-to-try-in-order) with their output.

**Linux is a different question.** The quick start already passed on a
GitHub-hosted x86_64 Ubuntu runner with KVM, against an emulator booted there
(`.github/workflows/linux-quickstart.yml`, run by hand). A Linux VM on Apple
Silicon is ARM, and the Android SDK has no ARM Linux build of `adb` or the
emulator, so it is not a substitute.

---

## Setting up the machine

### 1. Go and the toolchain

```powershell
winget install GoLang.Go Git.Git
go version                      # needs 1.24+
```

### 2. Android

```powershell
winget install Google.AndroidStudio
```

Then in Android Studio: **More Actions → SDK Manager → SDK Tools**, tick
*Android SDK Platform-Tools* and *Android Emulator*. Create a device under
**Device Manager** — a Pixel 7 on API 35 matches what the Android work was
verified against.

Add to `PATH` (adjust for your user):

```powershell
$env:ANDROID_HOME = "$env:LOCALAPPDATA\Android\Sdk"
$env:PATH += ";$env:ANDROID_HOME\platform-tools;$env:ANDROID_HOME\emulator"
```

Mobium finds `adb` through `PATH`, `ANDROID_HOME`, or `MOBIUM_ADB_PATH`; the
lookup in `internal/device/adb.go` already includes the Windows SDK location.

> **Hardware acceleration.** The emulator needs WHPX (Windows Hypervisor
> Platform) enabled in *Turn Windows features on or off*, and it conflicts with
> some VPN and antivirus drivers. If the emulator will not boot, this is almost
> always why.

### 3. The repository

```powershell
git clone https://github.com/mobiumdev/mobium.git
cd mobium
go build -o bin\mobium.exe .\cmd\mobium
go test ./...
cd clients/go; go test ./...; cd ../..
```

There is a `Makefile` but no `make` on Windows by default. These are the
build and test it runs, near enough: `make build` also stamps the version
through `-ldflags`, and `make test` runs `clients/go` because it is a module
of its own.

---

## What to try, in order

If you try this, please open an issue with what happened at each step —
including the exact error text where it fails.
The failures are the useful part.

**1. Does the suite pass?**
```powershell
go test ./...
```
No device needed. Any failure here is a portability bug in code
nobody suspected, which is the most valuable thing this exercise can find.

**2. Does the MCP server work?** This is the path that should already be fine.
```powershell
'{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | .\bin\mobium.exe mcp
```
Expect 73 tools — [API.md](API.md) is generated from the source and is the
live number if this one has drifted again. It has before.

Then, with an emulator running:
```powershell
'{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"app_map","arguments":{}}}' | .\bin\mobium.exe mcp
```

**3. What exactly does the CLI do?** It goes through the daemon, whose
named-pipe transport passes in CI but has never carried a device session. Expect it to work; a hang, a
panic, or an error that names the wrong cause is a bug, and the exact text is
what to report.
```powershell
.\bin\mobium.exe devices
.\bin\mobium.exe map
```

**4. Does `mobium devices` handle a missing Xcode gracefully?** It should list
Android devices and add an iOS note, not fail.

**5. Does the emulator work at all?** `adb devices` should list it before any
of the above is meaningful.

### Worth reporting even if it works

- Anything where a Windows path with spaces or backslashes misbehaves
- Whether the UiAutomator2 APK download and install work (`app_map` over MCP
  triggers them on first use)
- Whether WebView contexts work (`app_contexts` over MCP)

---

## Doing the work

Written and verified in CI without a device; see "The five gaps" above. What
is left is to run it against one. `internal/daemon`'s tests are the
acceptance criteria: they start a real daemon, call it over
the real transport, check the pipe exists and is owner-only while it runs and
is gone after, and check that a second home reaches no daemon. If those pass
on Windows, the transport works:

```powershell
go test ./internal/daemon/ ./internal/paths/ -v
```

Then the CLI end to end, which the tests do not cover — auto-start in
particular, which goes through `device.Detach` and a real child process. The
`go-windows` CI job runs all of this except closing the terminal window:

```powershell
.\bin\mobium.exe daemon status   # "not running"
.\bin\mobium.exe devices         # auto-starts the daemon
.\bin\mobium.exe daemon status   # names the pipe and the PID
# close the terminal window, open another: is the daemon still up?
.\bin\mobium.exe daemon stop
```

### Project rules that apply

From [CONTRIBUTING.md](../CONTRIBUTING.md):

- **Verify by outcome, never exit code.** Especially here: a named pipe that
  accepts a connection and never answers looks identical to success.
- Run `make ci` before committing, and `mobium daemon stop` after
  rebuilding, or you will debug a binary you are not running.
- Never name a Go file `*_windows.go` unless you intend the build constraint —
  though here, you do.

### When it works

Update in the same commit:
- `docs/ROADMAP.md` — move Windows out of Outstanding
- `docs/CHALLENGES.md` — the environment traps hit, and any defect found
- `README.md` — drop the Windows caveat from the daemon description
