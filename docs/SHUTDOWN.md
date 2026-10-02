# Stopping cleanly

How to finish a session without leaving anything running, and why the order
matters. Every rule here exists because it was got wrong first.

```sh
./docs/checks/clean-stop.sh          # report what is still running
./docs/checks/clean-stop.sh --quit   # stop it all, then report
```

---

## The order

**Daemon → simulators → emulators → adb.** Not arbitrary.

### 1. `mobium daemon stop`, always first

The daemon owns the device-side sessions: a UiAutomator2 server running as
instrumentation on Android, a WebDriverAgent process on an iOS simulator.
Stopping it tells both to shut down.

Kill the *device* first and neither gets that message. On Android you leave
instrumentation running against hardware that is about to disappear; when an
emulator with the same serial comes back — and emulator serials are
deterministic, so it will — a cached session points at a server that died with
the old one, and every command fails until the daemon is restarted. That was
defect 9, and the `Health` interface exists because of it.

`daemon stop` now waits for the process to actually exit before returning.
It used to return as soon as the request was acknowledged, while teardown was
still going, which made `daemon stop && mobium <anything>` fail five times out
of five — defect 28. The wait covers the daemon's own worst case, 90 seconds since putting a real
iPhone's accessibility settings back through its Settings app needed more than
the 35 there were (defect 160),
and a daemon still running after it is reported as an error with the
`timeout` code rather than as stopped (defect 133). Ending a session also
stops the app `session start --app` launched. A browser restores its tabs on
its next launch, Chrome and Safari both, so on Android the tabs `open`
opened in it are closed first, over CDP, and only those; Safari's tabs
cannot be closed from outside and come back.

**It no longer waits forever for a stuck call.** A tool call that never
returns holds the sessions, and shutdown used to wait for it indefinitely —
SIGTERM and `daemon stop` both hung, with the socket already gone, so the
daemon looked dead while its PID file refused a replacement (CHALLENGES 89).
Now it waits 10 seconds for calls to finish and up to 75 more for the
sessions to close, then exits and says on stderr that a session was left open. A second
Ctrl-C or SIGTERM exits at once. Either way, what a session left behind is
still there: run `docs/checks/clean-stop.sh --quit`, or the commands under
"On the device" below.

### 2. Simulators, then emulators

```sh
xcrun simctl shutdown <udid>          # or `all`
adb -s emulator-5554 emu kill
```

`mobium shutdown <serial | avd | udid | simulator>` does this for one
device in the same order — its session ended, then the device — and
returns once the device is gone. It ends only its own daemon's session and
does not check for other daemons on the device, so stop those first
(`MOBIUM_SESSION=<name> mobium daemon stop` for each).

A **headless** emulator (`-no-window`) is stopped exactly the same way, and
this is where the check below earns its keep: there is no window whose absence
tells you it worked, so "did it stop" has to be answered by asking rather than
by looking.

`adb emu kill` is a clean shutdown request. Killing the qemu process directly
works but skips the emulator's own cleanup, and leaves the AVD's lock files
behind.

### 3. `adb kill-server` last

Only after the emulators are gone, or adb keeps stale `offline` entries for
them. If you see `emulator-5554 offline` in `adb devices` after shutting one
down, an adb restart clears it.

Leaving the adb server running is harmless — it starts on demand anyway, and a
physical device needs it. It is only worth killing for a genuinely quiet
machine.

---

### A client's sessions end with it

A session a client opened with `start()` ends when the client goes, whether
it called `quit()`, returned without it, crashed or was killed: `mobium pipe`
ends the sessions its client started, and only those. `close()` is the way
to leave one open on purpose. So a test run that dies half-way no longer
leaves a device session behind for the next one to trip over (defect 130).
A pipe still running after its client is reported by `clean-stop.sh`.

### After a crash

A daemon killed without tearing down — `kill -9`, a crash, a second Ctrl-C —
leaves the device-side server running. The next session clears what it can
find: on a simulator it stops the leftover WebDriverAgent before starting
its own (defect 131), and on Android it force-stops a leftover UiAutomator2
server and removes its adb forward (defect 132), and on a real iPhone it
takes over the `xcodebuild` runner a dead daemon left, so its own end stops
it (defect 135). A runner started from Xcode is not taken over, and
`clean-stop.sh` does not report it: it checks for Mobium's runner only.

## Verify, do not assume

Every item below has been left running by accident at least once here.

| Check | Why |
| --- | --- |
| `mobium pipe` and `mobium mcp` | a client's transport outliving the client. Matched the same way as the daemon |
| `xcodebuild test-without-building` for `webdriveragent-device` | a real iPhone's WebDriverAgent runner; a daemon killed without tearing down leaves it, and the next one reuses it rather than stopping it |
| `simctl io … recordVideo` | a simulator recording that was never stopped |
| the daemon | matching the text `mobium daemon` is not enough — it also matches the script doing the checking, an editor with the source open, and **any shell whose command line contains it**. Require the executable to be `mobium` and its first argument to be `daemon` |
| `pgrep -f "qemu-system.*-avd"` | a bare `qemu` match catches unrelated VMs |
| `netsimd`, `emulator/crashpad_handler` | the emulator starts these as separate processes and they outlive a hard kill. Match them **by name, not by `$ANDROID_HOME`**: that variable is usually unset in the shell doing the checking, and a check that silently passes when its input is missing is worse than no check |
| `pgrep -x adb` | **any** port, not just 5037 |
| `xcrun simctl list devices booted` | |
| `pgrep -f CoreSimulator/Profiles/Runtimes` | simulator runtime processes outlive a badly shut down simulator |
| `pgrep -x scrcpy` | a screen mirror, used to watch a Fire TV while it was measured. It ignored SIGTERM three times in three, so `--quit` sends it and then `-9` |
| `ls -A ${MOBIUM_HOME:-~/.mobium}/daemon` | a socket or PID file left behind means the daemon did not exit cleanly. Under `MOBIUM_HOME` if it is set: checking the default while the daemon used another reported "cleared" for files nobody looked at |

### A leak this actually caught

A backgrounded wait loop of mine — `until adb shell getprop ...` against two
attached devices, so it could never succeed — was still spinning `sleep 3`
forty-two minutes later. It was reported as a *second daemon*, because its
command line contained `bin/mobium daemon stop`.

Two lessons in one process: a loop waiting on a condition that cannot be met
runs forever, and a check matching command-line text will find your own
commands. Both are fixed above.

### Two false positives worth knowing

- **`pgrep -f crashpad_handler` matches Chrome.** Chrome runs its own crash
  handler, re-parented to launchd, often for weeks. It looks exactly like an
  orphan and is not. Match on the emulator's path instead.
- **An adb server on a non-default port is invisible to the usual check.**
  A `doctor` test that set `ANDROID_ADB_SERVER_PORT=5999` left a server behind
  that `adb kill-server` never touched, because that reaches the default port
  only. `pgrep -x adb` finds it; `ANDROID_ADB_SERVER_PORT=5999 adb kill-server`
  stops it.

---

## On the device

Stopping processes on your machine is not the whole job.

**`clean-stop.sh --quit` now runs these**, so they are no longer a checklist
nobody gets to. They were manual until a hierarchy dump turned up in
`/data/local/tmp` six days after the session that wrote it, on an emulator
stopped and rebooted twice in between — it was a hand-run `uiautomator dump`
rather than mobium's own file, which is deleted after every read, but nothing
was ever going to notice either way.

```sh
adb -s <serial> shell ps -A | grep uiautomator     # should be empty
adb -s <serial> shell ls /data/local/tmp           # no mobium file
adb -s <serial> shell ls -p /sdcard/ | grep -v /$  # no loose files
adb -s <serial> forward --list                     # no forwards
```

**`/sdcard` was missing from this list until 2026-09-17, and that is where the
leaks actually were.** `uiautomator dump` writes there when nobody gives it a
path, and eight hierarchy dumps — `c.xml`, `k.xml`, `l.xml`, `s.xml`, `t.xml`,
`w.xml`, `w2.xml`, `w3.xml` — were found at its root, dated across two earlier
sessions and untouched for five days. They held Chrome, Calculator, Settings,
Clock and the launcher; nothing sensitive, on an emulator. The same commands
against a phone would have left five days of somebody's screens in a world-
readable directory.

The rule for that directory is structural rather than a list of names, because
a list of names is what failed: **a stock Android `/sdcard` root holds only
directories** — Alarms, Android, DCIM, Download and the rest — so any plain
file there was put there and left. A loose `.xml` is reported as a failure,
since at that path it is a hierarchy dump in all but name; anything else is
named without being judged.

**When they run is the whole difficulty**, and it is why they sit inside
`--quit` rather than beside the other checks. Instrumentation is *supposed* to
be up while the daemon is, so asking before `daemon stop` reports a false
positive; asking after the emulator is killed has nothing left to ask. The one
useful moment is between the two. With no device attached the script says
"none attached — nothing checked" rather than reporting clean, for the reason
in the table above: a check that passes when its input is missing is worse
than no check.

A file in `/data/local/tmp` that is not mobium's is **named rather than
failed**. It may well be somebody else's, and a check that cries wolf is one
that gets ignored — but it is printed, which is all that was missing.

The hierarchy file the dump backend writes is deleted after every read, so
`/data/local/tmp` should be clear — and it was. Every file the checks have
actually caught came from a command typed by hand, not from mobium. It is a serialization of whatever was on
screen, which on a real phone is somebody's data — defect 34.

### A TV, or anything reached by `adb connect`

A Fire TV is reached over Wi-Fi with `adb connect <tv>:5555`, after ADB
debugging is turned on in its Developer Options. It is somebody's living-room
TV, so the device checks above run on it as on a phone, and four more run on
every device:

- **A screen mirror's server.** scrcpy was how the TV was watched while it
  was measured, and its server streams the screen off the device for as long
  as it runs. On the device it is `app_process`, never named scrcpy, so the
  check reads the command line (`ps -A -o PID,ARGS`); the first version
  matched the name and reported none with one running. `--quit` stops the
  mirror on this machine first, and the server goes with it.
- **What a mirror leaves.** On Android 9 each scrcpy run leaves the
  runtime's compiled copy of its server in `/data/local/tmp/oat`. Reported,
  with the command that removes it; nothing is deleted for you.
- **Accessibility services that are on**, by package. A measurement turned
  VoiceView on (CHALLENGES 208), and one left on changes how the TV talks
  to whoever uses it next — but one may equally be the owner's, so they are
  named, not judged. The positive control was the emulator's Accessibility
  Menu: Android drops a service from the setting when it is not installed,
  so a made-up one reads back as none.
- **The link itself.** A serial with a port in it is a network device.
  `--quit` disconnects it, and says that **ADB debugging is still on** and
  where to turn it off — on a Fire TV, Settings > Device & Software >
  Developer Options. That setting is the owner's, and turning it off from here would
  also cut the only way to check the result.

Fire OS is the one device whose `/sdcard` root is not all folders: Amazon's
Photos app keeps two state files there, named with a hash, and the check
leaves them out rather than print them. And `dalvik-cache` in
`/data/local/tmp`, which the TV grew with its first `uiautomator dump`, is
the system's, as on a phone.

Reconnecting after `--quit` has a trap of its own on a Mac: an adb server
started by a background process is silently blocked from the local network
by macOS, and `adb connect` says "No route to host" while ping works. Start
it from a Terminal window (`adb kill-server && adb connect <tv>:5555`) and
allow the prompt; [SETUP.md](SETUP.md#over-wi-fi) has the rest.

To take the device-side agents off entirely — worth doing on a phone you
actually use, unnecessary on an emulator you are about to delete:

```sh
mobium daemon stop
mobium --device <serial> uninstall io.appium.uiautomator2.server.test
mobium --device <serial> uninstall io.appium.uiautomator2.server
mobium --device <serial> apps                      # should list neither
```

They are reinstalled automatically next run.

---

## What to leave alone

- **`~/.mobium/uiautomator2` and `~/.mobium/webdriveragent`** — about 40MB of
  pinned, checksummed agents. Deleting them only costs the next run a
  download.
- **A physical device.** Releasing the session is enough; unplugging is
  yours. USB and wireless debugging stay on until you turn them off — a
  network device's link is ended by `--quit`, its debugging setting is not.
- **Anything you did not start.** Check what a process actually is before
  killing it — see the Chrome case above.
