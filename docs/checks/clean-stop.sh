#!/bin/sh
# Verify that nothing Mobium started is still running, and optionally stop it.
#
#   ./docs/checks/clean-stop.sh            report only
#   ./docs/checks/clean-stop.sh --quit     stop the daemon and every virtual device first
#
# Reporting is the default because emulators are expensive to boot and you may
# well want to keep yours. Nothing here touches a physical device beyond
# releasing the session: unplugging is yours to do.
#
# Order matters and is not arbitrary — see ../SHUTDOWN.md.

QUIT=""
[ "$1" = "--quit" ] && QUIT=1
BIN="./bin/mobium"
[ -x "$BIN" ] || BIN="mobium"
fail=0

note() { printf "  %-34s %s\n" "$1" "$2"; }
bad()  { printf "  %-34s %s   <-- \n" "$1" "$2"; fail=1; }

# Stopping processes on your machine is not the whole job — ../SHUTDOWN.md,
# "On the device". These were a manual checklist until a hierarchy dump was
# found in /data/local/tmp six days after the session that wrote it, on an
# emulator that had been stopped and rebooted twice in between. Nothing was
# going to run them by hand.
#
# Timing is the whole difficulty: instrumentation is *supposed* to be running
# while the daemon is up, so checking before it stops reports a false
# positive, and checking after the emulator dies has nothing to ask. The only
# useful moment is between the two, which is where --quit calls this.
device_checks() {
  devices=$(adb devices 2>/dev/null | awk '$2 == "device" { print $1 }')
  if [ -z "$devices" ]; then
    # Deliberately not phrased as a pass. A check that reports "clean" when
    # its input is missing is worse than no check — the same rule that made
    # the emulator-helper check match by name instead of by $ANDROID_HOME.
    note "android devices" "none attached — nothing checked"
    return
  fi
  for d in $devices; do
    n=$(adb -s "$d" shell ps -A 2>/dev/null | grep -c uiautomator)
    [ "$n" = "0" ] && note "$d instrumentation" "none" \
      || bad "$d instrumentation" "$n uiautomator process(es) still running"

    # dalvik-cache belongs to the system. Anything else is something a
    # session put there and did not take away. A hierarchy dump is a
    # serialization of whatever was on screen, which on a phone is somebody's
    # data — defect 34.
    left=$(adb -s "$d" shell ls /data/local/tmp 2>/dev/null \
             | tr -d '\r' | grep -v '^dalvik-cache$' | grep -v '^$' | tr '\n' ' ')
    if [ -z "$left" ]; then
      note "$d /data/local/tmp" "empty"
    elif echo "$left" | grep -q "mobium-dump.xml"; then
      bad "$d /data/local/tmp" "mobium left: $left"
    else
      # Named rather than failed: this is not necessarily ours, and crying
      # wolf on somebody else's file would get the whole check ignored.
      note "$d /data/local/tmp" "not empty: $left"
    fi

    # /sdcard is where `uiautomator dump` writes when nobody says otherwise,
    # and it was not on the checklist -- which is how eight hierarchy dumps
    # came to sit at its root for five days. The rule is structural rather
    # than a list of names: a stock Android /sdcard root holds *only*
    # directories (Alarms, Android, DCIM, Download, ...), so any plain file
    # there was put there by somebody and left.
    stray=$(adb -s "$d" shell 'ls -p /sdcard/ 2>/dev/null | grep -v "/$"' 2>/dev/null \
              | tr -d '\r' | grep -v '^$' | tr '\n' ' ')
    if [ -z "$stray" ]; then
      note "$d /sdcard" "no loose files"
    elif echo "$stray" | grep -q "\.xml"; then
      # A loose .xml here is a hierarchy dump in all but name.
      bad "$d /sdcard" "hierarchy dumps left: $stray"
    else
      note "$d /sdcard" "loose files: $stray"
    fi

    n=$(adb -s "$d" forward --list 2>/dev/null | grep -c "^$d")
    [ "$n" = "0" ] && note "$d port forwards" "none" \
      || bad "$d port forwards" "$n left open"
  done
}

if [ -n "$QUIT" ]; then
  echo "stopping:"
  # The daemon goes first. It owns the UiAutomator2 and WebDriverAgent
  # sessions, and killing a device out from under one leaves instrumentation
  # running against hardware that is about to vanish.
  $BIN daemon stop 2>/dev/null | sed 's/^/  /'
  echo
  echo "on the device, after the daemon stopped and before the device goes:"
  device_checks
  echo
  echo "stopping devices:"

  for s in $(xcrun simctl list devices booted 2>/dev/null | grep -oE '[0-9A-F-]{36}'); do
    echo "  shutting down simulator $s"
    xcrun simctl shutdown "$s" >/dev/null 2>&1
  done

  for d in $(adb devices 2>/dev/null | awk '/^emulator-/{print $1}'); do
    echo "  killing $d"
    adb -s "$d" emu kill >/dev/null 2>&1
  done

  # Give the emulators a moment to go, then clear adb's stale entries.
  i=0
  while pgrep -f "qemu-system.*-avd" >/dev/null 2>&1 && [ $i -lt 30 ]; do
    sleep 1; i=$((i + 1))
  done
  adb kill-server >/dev/null 2>&1
  echo
fi

echo "checking:"

# Report-only: the device is still up and so, quite possibly, is the daemon.
# Instrumentation showing here is expected in that case and is reported rather
# than judged; --quit is the mode that can tell the difference.
[ -z "$QUIT" ] && device_checks

# Matching "mobium daemon" as text is not enough: it also matches this
# script, an editor with the source open, and any shell whose command line
# happens to contain it — a leaked wait loop of mine was reported as a second
# daemon for exactly that reason. Require the executable itself to be mobium
# and the first argument to be daemon.
n=$(pgrep -fl "mobium daemon" 2>/dev/null \
      | awk '$2 ~ /(^|\/)mobium$/ && $3 == "daemon"' | wc -l | tr -d ' ')
[ "$n" = "0" ] && note "mobium daemon" "none" || bad "mobium daemon" "$n running"

# A client's pipe, or an MCP server, outliving its client. Since 2026-09-27 a
# pipe ends the sessions its client started when the client goes away, so one
# still running means a client is — or a pipe that could not exit.
n=$(pgrep -fl "mobium (pipe|mcp)" 2>/dev/null \
      | awk '$2 ~ /(^|\/)mobium$/ && ($3 == "pipe" || $3 == "mcp")' | wc -l | tr -d ' ')
[ "$n" = "0" ] && note "mobium pipe/mcp" "none" || bad "mobium pipe/mcp" "$n running (a client is still attached)"

# The real iPhone's WebDriverAgent runner: an xcodebuild the session owns,
# which a daemon killed without tearing down leaves running, and the next
# daemon then reuses as not its own and never stops.
n=$(pgrep -f "xcodebuild test-without-building -xctestrun .*webdriveragent-device" 2>/dev/null | wc -l | tr -d ' ')
[ "$n" = "0" ] && note "iphone wda runner" "none" || bad "iphone wda runner" "$n xcodebuild running"

# A simulator recording the daemon started and did not stop.
n=$(pgrep -f "simctl io .* recordVideo" 2>/dev/null | wc -l | tr -d ' ')
[ "$n" = "0" ] && note "simulator recordings" "none" || bad "simulator recordings" "$n recording"

# -avd narrows this to emulators. A bare qemu match would catch any VM.
n=$(pgrep -f "qemu-system.*-avd" 2>/dev/null | wc -l | tr -d ' ')
[ "$n" = "0" ] && note "android emulators" "none" || bad "android emulators" "$n running"

# Matched by name, not by $ANDROID_HOME: the variable is often unset in the
# shell doing the checking, and the old version silently reported "none"
# whenever it was, which is the worst possible answer from a check.
n=$(pgrep -f "netsimd|emulator/crashpad_handler|qemu-img" 2>/dev/null | wc -l | tr -d ' ')
[ "$n" = "0" ] && note "emulator helpers" "none" || bad "emulator helpers" "$n running (netsimd, crashpad)"

# Any adb server, on any port. A doctor test that set ANDROID_ADB_SERVER_PORT
# left one behind on 5999 that the default-port check never saw.
n=$(pgrep -x adb 2>/dev/null | wc -l | tr -d ' ')
[ "$n" = "0" ] && note "adb servers" "none" || note "adb servers" "$n (fine if a device is attached)"

n=$(xcrun simctl list devices booted 2>/dev/null | grep -c Booted)
[ "$n" = "0" ] && note "ios simulators" "none booted" || bad "ios simulators" "$n booted"

# Simulator runtime processes outlive a badly shut down simulator.
n=$(pgrep -f "CoreSimulator/Profiles/Runtimes" 2>/dev/null | wc -l | tr -d ' ')
[ "$n" = "0" ] && note "simulator runtimes" "none" || bad "simulator runtimes" "$n running"

# MOBIUM_HOME moves the state directory, and a check of the default one
# while the daemon used another reported "cleared" for files it never looked at.
state="${MOBIUM_HOME:-$HOME/.mobium}"
if [ -d "$state/daemon" ]; then
  n=$(ls -A "$state/daemon" 2>/dev/null | wc -l | tr -d ' ')
  [ "$n" = "0" ] && note "daemon socket/pid" "cleared" || bad "daemon socket/pid" "$n file(s) left"
fi

echo
if [ "$fail" = "0" ]; then
  echo "clean."
else
  echo "something is still running. ./docs/checks/clean-stop.sh --quit"
  exit 1
fi
