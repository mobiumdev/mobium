#!/bin/sh
# Device logs and crash reports, end to end, on an Android device or an iOS
# simulator.
#
# Each half needs a positive control — something known to have happened — or
# an empty answer proves nothing. On Android that is `am crash`, which crashes
# a running app from inside its own process, and `log`, which writes a line.
# On an iOS simulator it is a library injected at launch that calls abort():
# a SIGABRT sent from outside also produces a report, but took 34 seconds to,
# and the simulator has no `logger`. A simulator's apps are Mac processes, so
# each run's crash also raises macOS's own "quit unexpectedly" dialog on the
# Mac: Ignore is safe, and `defaults write com.apple.CrashReporter DialogType
# notification` makes it a banner instead, if you would rather.
#
#   docs/checks/crashes.sh <android-serial>
#   docs/checks/crashes.sh <simulator-udid>
#   docs/checks/crashes.sh <iphone-udid>
#
# On Android, MobiumApp's Crash Demo is used instead of `am crash` when it is
# installed — the report then carries the JavaScript error's own text, which
# the iPhone's does not.
#
# Nothing outside an app can crash it on a real iPhone — a signal from
# outside produces no report there — so on a phone the positive control is
# MobiumApp's Crash Demo, which logs a line and throws an unhandled JavaScript
# error when asked. With MobiumApp installed, the phone gets the same checks
# as a simulator; without it, the check reads the crashes already on the
# phone and says it skipped causing one. The phone's log is captured from the
# session's start, since the phone keeps no history.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

case "$DEV" in
  *-*-*-*-*) PLATFORM=ios; APP=com.apple.Preferences
             M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  ????????-????????????????) PLATFORM=iphone; APP=com.apple.Preferences
             M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *)         PLATFORM=android; APP=com.google.android.apps.messaging
             M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($PLATFORM)"

# Logs. The first read has no mark to start from; every later read returns
# only what arrived after the newest line it last handed out. A read that
# returns nothing sets no mark, and a phone's capture is empty the moment its
# session starts, so read until something comes back.
for _ in 1 2 3 4 5; do
  n=$($M logs --source device --lines 5 --json | json 'len(d["entries"])')
  [ "$n" -gt 0 ] && break
  sleep 1
done
[ "$n" -gt 0 ] || fail "five reads of the device log returned nothing at all"
if [ "$PLATFORM" = android ]; then
  adb -s "$DEV" shell log -t MobiumCheck "the positive control"
  # A read with room for everything since the probe: a just-booted device
  # logged more than the default hundred lines after it, and the probe was
  # counted as skipped — correct of the tool, and a false failure here.
  got=$($M logs --source device --lines 100000 --json | json '[e["message"] for e in d["entries"] if e["tag"]=="MobiumCheck"]')
  case "$got" in *"the positive control"*) ;; *) fail "a line logged between reads was not returned: $got" ;; esac
  echo "    logs           a line written between reads is the next read's      ok"

  # The limit keeps the newest and says how many it dropped. Measured: a
  # burst between two reads made a line vanish without a word until it did.
  adb -s "$DEV" shell 'for i in $(seq 1 60); do log -t MobiumNoise n$i; done'
  skipped=$($M logs --source device --lines 10 --json | json 'd.get("skipped",0)')
  [ "$skipped" -ge 50 ] || fail "60 lines against a limit of 10 reported $skipped skipped"
  echo "    logs           a burst past the limit is counted, not hidden        ok"
fi
if [ "$PLATFORM" = ios ] || [ "$PLATFORM" = iphone ]; then
  # No logger here, but iOS logs hundreds of lines a second on its own: a
  # small limit a few seconds after a read has to count what it skipped.
  sleep 4
  skipped=$($M logs --source device --lines 5 --json | json 'd.get("skipped",0)')
  [ "$skipped" -gt 0 ] || fail "a read of 5 lines after 4 seconds of iOS logging skipped nothing"
  echo "    logs           a burst past the limit is counted, not hidden        ok"
fi
again=$($M logs --source device --lines 1000 --json | json 'd["first"]')
[ "$again" = False ] || fail "a later read claimed to be the first"

# A level the platform does not have is refused, not answered with nothing.
if [ "$PLATFORM" = ios ] || [ "$PLATFORM" = iphone ]; then
  $M logs --source device --level warn >/dev/null 2>&1 && fail "iOS accepted a warn level it does not have"
  echo "    logs           a level iOS lacks is refused                         ok"
fi

if [ "$PLATFORM" = iphone ]; then
  if ! $M apps | grep -q dev.mobium.mobiumapp; then
    $M launch "$APP" >/dev/null
    sleep 2
    n=$($M logs --source device --app "$APP" --lines 1000 --json | json 'len(d["entries"])')
    [ "$n" -gt 0 ] || fail "Settings launched and logged nothing the session captured"
    echo "    logs           Settings' launch is in the captured log, filtered to it ok"
    id=$($M crashes --limit 1 --json | json '(d["crashes"] or [{"id":""}])[0]["id"]')
    [ -n "$id" ] || fail "no crash reports on the phone at all — nothing to read, so nothing verified"
    $M crashes "$id" | grep -q "crashed:" || fail "report $id has no crashed thread"
    echo "    crashes        the newest report reads in full                      ok"
    echo "    crashes        a fresh crash                                        SKIPPED -- install MobiumApp for its Crash Demo"
    exit 0
  fi
  APP=dev.mobium.mobiumapp
  $M terminate "$APP" >/dev/null 2>&1 || true
  $M launch "$APP" >/dev/null
  $M tap testid=crashBtn >/dev/null
  $M tap testid=logLineBtn >/dev/null
  n=$($M text testid=loggedCount | sed 's/[^0-9]//g')
  sleep 1
  $M logs --source device --app "$APP" --lines 5000 | grep -q "mobium-log-control #$n" \
    || fail "the app logged 'mobium-log-control #$n' and the captured log does not have it"
  echo "    logs           the app's own line is in the log, filtered to it     ok"
fi

# Crashes. A crash is a record, so it is found by id rather than by being new.
before=$($M crashes --app "$APP" --limit 1 --json | json '(d["crashes"] or [{"id":""}])[0]["id"]')
if [ "$PLATFORM" = android ] && $M apps | grep -q dev.mobium.mobiumapp; then
  # MobiumApp's own crash, as on a phone, when it is installed: an
  # unhandled JavaScript error, whose text Android's report carries.
  APP=dev.mobium.mobiumapp
  before=$($M crashes --app "$APP" --limit 1 --json | json '(d["crashes"] or [{"id":""}])[0]["id"]')
  $M terminate "$APP" >/dev/null 2>&1 || true
  $M launch "$APP" >/dev/null
  $M tap testid=crashBtn >/dev/null
  $M tap testid=crashJsBtn >/dev/null
elif [ "$PLATFORM" = android ]; then
  $M launch "$APP" >/dev/null
  sleep 2
  adb -s "$DEV" shell am crash "$APP"
elif [ "$PLATFORM" = iphone ]; then
  # Already on the Crash Demo, from the log check above.
  $M tap testid=crashJsBtn >/dev/null
else
  lib="$(mktemp -d)/abort.dylib"
  printf '%s\n' '#include <stdlib.h>' '#include <dispatch/dispatch.h>' \
    '__attribute__((constructor)) static void boom(void) {' \
    '  dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 2 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{ abort(); });' \
    '}' > "${lib%.dylib}.c"
  xcrun -sdk iphonesimulator clang -dynamiclib -arch arm64 -mios-simulator-version-min=17.0 \
    -o "$lib" "${lib%.dylib}.c"
  SIMCTL_CHILD_DYLD_INSERT_LIBRARIES="$lib" xcrun simctl launch --terminate-running-process "$DEV" "$APP" >/dev/null
fi

# A report is not there the moment the app dies — a second on Android and on
# a simulator's own abort, and 34 seconds once on iOS — so wait for it.
id=""
for _ in $(seq 1 30); do
  id=$($M crashes --app "$APP" --limit 1 --json | json '(d["crashes"] or [{"id":""}])[0]["id"]')
  [ -n "$id" ] && [ "$id" != "$before" ] && break
  id=""; sleep 2
done
if [ -z "$id" ]; then
  # A crash that happened and was not recorded is Android's rate limit, not
  # Mobium missing it: after many crashes of one app in a few minutes the
  # platform writes no record and shows "keeps stopping" instead. Say so,
  # since running this check repeatedly is exactly how to get there.
  if $M alert 2>/dev/null | grep -q "keeps stopping"; then
    fail "$APP crashed (Android shows \"keeps stopping\") but wrote no crash record: Android rate-limits crash records per app. Run this again in a few minutes"
  fi
  fail "no new crash for $APP within 60s"
fi
echo "    crashes        the crash appears, scoped to $APP                   ok"

text=$($M crashes "$id")
case "$PLATFORM:$text" in
  android:*CrashedByAdbException*) ;;
  android:*"JavascriptException: Error: mobium-crash-control"*) ;;
  ios:*"abort + "*) ;;
  iphone:*"abort() called"*) ;;
  *) fail "report $id does not show the crash: $(echo "$text" | head -5)" ;;
esac
echo "    crashes        read in full by id, the cause in it                  ok"

# On a phone the JavaScript error's text is in the log, not the report —
# the report carries only abort() — so reading both is how the cause is found.
# One read for everything asked of the app's log after the crash: each read
# returns only what is new since the last, so a second read would not see
# lines the first one already handed out — which failed this check once,
# with the line plainly in the log.
applog=$($M logs --source device --app "$APP" --lines 20000)
if [ "$PLATFORM" = iphone ]; then
  echo "$applog" | grep -q "Unhandled JS Exception: Error: mobium-crash-control" \
    || fail "the crash is reported but the log does not say which error caused it"
  echo "    crashes        the log names the error the report does not          ok"
fi

# The death itself is logged by the system, not the app: runningboardd on
# iOS ("termination reported by launchd"), system_server on Android. A log
# narrowed to the app has to keep that line, or it holds everything the app
# said and not the fact that it died. On Android that is system_server's
# "Force finishing activity" or "Process … has died", measured on the Pixel
# 8 Pro; an ANR's "ANR in" is checked with the ANR below. `am crash` is a
# system-induced crash, so only the app's own crash is held to this.
case "$PLATFORM" in
  ios|iphone) died="termination reported by launchd" ;;
  android)    died="Force finishing activity $APP|Process $APP \(pid [0-9]+\) has died" ;;
esac
if [ "$PLATFORM" != android ] || [ "$APP" = dev.mobium.mobiumapp ]; then
  echo "$applog" | grep -qE "$died" \
    || fail "the app-filtered log lacks the system's line that $APP died"
  echo "    logs           the system's line that the app died, kept by --app   ok"
fi

# An ANR is not a crash: the app is frozen, not gone. The control is
# MobiumApp stopped with SIGSTOP and then touched, which is Android's
# documented ANR — input dispatching timed out after 5 seconds. Stopping
# another app's process needs root, so this runs only where adb can become
# root (an emulator or a debug build), and puts adb back as it found it.
if [ "$PLATFORM" = android ] && [ "$APP" = dev.mobium.mobiumapp ]; then
  # adbd restarts to change user, and `wait-for-device` returns before it
  # answers a shell as the new one: the first shell command after it failed
  # silently and ended the check with no message. So wait for the answer.
  as_user() { for _ in $(seq 1 30); do
                [ "$(adb -s "$DEV" shell id -u 2>/dev/null | tr -d '\r')" "$1" 0 ] && return 0; sleep 1
              done; return 1; }
  # Judged by the outcome, not by what `adb root` prints: it sometimes prints
  # nothing at all and still restarts as root (measured, one run in three),
  # which read as "cannot" and skipped this step silently. Only a production
  # build's refusal is taken at its word, so a phone does not wait 30s.
  rooted=$(adb -s "$DEV" root 2>&1 || true)
  if ! echo "$rooted" | grep -q "production builds" && as_user = ; then
    # Whatever happens next, leave nothing frozen and adb as it was: a run
    # that failed here once left MobiumApp stopped and adb root, and every
    # later read timed out against the frozen window.
    # Each step may fail harmlessly — "Close app" has usually killed the
    # process already — and none may end the cleanup early: under `set -e`
    # a failed kill here once did, and adb was left root.
    thaw() { set +e
             [ -n "$pid" ] && adb -s "$DEV" shell kill -CONT "$pid" >/dev/null 2>&1
             adb -s "$DEV" shell am force-stop "$APP" >/dev/null 2>&1
             adb -s "$DEV" unroot >/dev/null 2>&1
             as_user != || echo "WARNING: adb is still root on $DEV" >&2
             set -e; }
    trap thaw EXIT
    adb -s "$DEV" shell am force-stop "$APP"
    $M launch "$APP" >/dev/null
    # Frozen before its first screen is drawn, an app has no window to be
    # unresponsive in, and the touches raise nothing — which is how this
    # step first failed, straight after the Crash Demo had killed the app.
    $M wait testid=crashBtn >/dev/null
    pid=$(adb -s "$DEV" shell pidof "$APP" | tr -d '\r')
    adb -s "$DEV" shell kill -STOP "$pid"
    adb -s "$DEV" shell input tap 540 1200; sleep 1; adb -s "$DEV" shell input tap 540 1400
    sleep 12
    $M alert | grep -q "isn't responding" || fail "a frozen app raised no ANR dialog that alert could see"
    # The dialog can be read but not answered through the alert endpoint;
    # the refusal must name the way through.
    $M alert accept 2>&1 | grep -q "tap one by the ref" || fail "answering the ANR dialog did not name a remedy"
    $M logs --source device --app "$APP" --level error | grep -q "ANR in $APP" \
      || fail "the app-filtered log lacks system_server's 'ANR in $APP'"
    anr=$($M crashes --app "$APP" --limit 5 --json | json '[c["id"] for c in d["crashes"] if c["kind"]=="anr"][0]')
    $M crashes "$anr" | head -1 | grep -q "^ANR: Input dispatching timed out" || fail "ANR $anr does not lead with its reason"
    ref=$($M map | grep "Close app" | grep -oE '@e[0-9]+')
    $M tap "$ref" >/dev/null
    thaw; trap - EXIT
    echo "    anr            a frozen app: its dialog, its log line, its report   ok"
  else
    adb -s "$DEV" unroot >/dev/null 2>&1 || true
    echo "    anr            SKIPPED -- adb cannot become root here, and freezing an app needs it (adb root: $rooted)"
  fi
fi

# Not drained: asking twice shows the same crash twice. Looked up by id,
# not as the newest: the ANR step above adds a newer record.
$M crashes --app "$APP" --limit 20 --json | json 'd["crashes"]' | grep -q "$id" \
  || fail "the second listing lost $id"
echo "    crashes        a record, not a stream: listed again                 ok"
