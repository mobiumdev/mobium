#!/bin/sh
# The gray box at its edges, on MobiumApp's Busy Demo: each a way a gray box
# could wait wrongly, or stop hearing the app and not say so.
#
#   docs/checks/graybox-edges.sh <serial|udid>
#
#   - alert:  the refresh is started by a button in an alert, which lifts in
#             a window the library does not watch; the tap after it is still
#             current;
#   - twice:  two refreshes at once are both waited out;
#   - poll:   work that never finishes is refused after 10 s as check idle,
#             naming it — and the app's still lines keep its lease for all of
#             that time;
#   - away:   with work in flight, Home: a tap elsewhere is not waited on,
#             and says the app is in the background;
#   - crash:  the app dies holding work: the next tap — Android's Close app,
#             or the home screen — is not held up by it, and says the app
#             stopped saying it is busy;
#   - deaf:   the log stream is killed: a tap says the app is not heard, and
#             the gray box hears it again — on Android by itself, from where
#             the stream stopped; on a simulator at the next gray-box launch;
#             on an iPhone at the next action, waiting for the app to
#             restate its work before trusting a count of zero.
#
# away and crash leave the app and tap the status bar — a point on the home
# screen can be an app's icon, and on the simulator one opened another app.
# They change no setting and record nothing outside MobiumApp, but the screen
# they leave the app for is the phone's owner's: on a phone they run only
# with ALLOW_PHONE=1. deaf kills the gray box's log stream, which is a
# process on this Mac on Android (adb logcat) and on a simulator (log
# stream); on a real iPhone it is a connection inside Mobium's daemon, which
# the daemon drops on SIGUSR1 — mid-work, so the reconnect has to recover
# work it did not hear.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-8s %-62s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
M="$ROOT/bin/mobium --device $DEV"
case "$DEV" in
  emulator-*) VIRTUAL=android ;;
  *-*-*-*-*) VIRTUAL=ios ;;
  *) VIRTUAL= ;;
esac
# Which platform the log stream belongs to, for deaf: a real iPhone's UDID
# is 8 and 16 hex digits; anything else that is not a simulator is Android.
case "$DEV" in
  *-*-*-*-*) STREAM=ios-sim ;;
  ????????-????????????????) STREAM= ;;
  *) STREAM=android ;;
esac
echo "--- $DEV"

demo() {
  $M terminate "$APP" >/dev/null 2>&1 || true
  out=$($M launch --gray-box "$APP") || fail "launch: $out"
  echo "$out" | grep -q "with the gray box" || fail "the app did not answer the gray box: $out"
  $M scroll-to "label=Busy Demo" --direction down >/dev/null 2>&1 || true
  $M tap "label=Busy Demo" >/dev/null || fail "MobiumApp has no Busy Demo — rebuild it"
  $M wait testid=busyCrash >/dev/null || fail "this MobiumApp's Busy Demo has no edge controls — rebuild it"
}
current() {
  case "$($M text testid=busyOutcome)" in
    *": current") ;;
    *) fail "$1: Row B reads $($M text testid=busyOutcome)" ;;
  esac
}

demo
i=0
while [ "$i" -lt 3 ]; do
  sleep 2
  $M tap testid=busyAlert >/dev/null
  $M alert accept >/dev/null
  $M tap testid=busyRowB >/dev/null
  current "after a refresh from an alert"
  i=$((i + 1))
done
row "alert" "3 of 3 current after a refresh started in an alert"

sleep 2
$M tap testid=busyTwice >/dev/null
said=$($M tap testid=busyRowB)
current "after two refreshes at once"
echo "$said" | grep -q "waited [0-9]* ms for the app to go idle (busy: quiet)" || fail "two refreshes were not waited: $said"
row "twice" "two at once waited out: $(echo "$said" | sed -n 's/.*waited \([0-9]*\) ms.*/\1 ms/p')"

sleep 2
$M tap testid=busyPoll >/dev/null
start=$(date +%s)
out=$($M tap testid=busyRowA 2>&1) && fail "a tap while the app polls forever was not refused: $out"
took=$(( $(date +%s) - start ))
echo "$out" | grep -q "failed check idle: the app says it is still busy after 10s, with poll" || fail "the refusal: $out"
[ "$took" -ge 9 ] || fail "refused after ${took}s: the lease ran out though the app kept saying it was busy"
row "poll" "refused after ${took}s as check idle, naming poll"

if [ -n "$VIRTUAL" ] || [ -n "${ALLOW_PHONE:-}" ]; then
  demo
  sleep 2
  $M tap testid=busyPoll >/dev/null
  $M press home >/dev/null
  sleep 1
  said=$($M tap 300 30)
  echo "$said" | grep -q "not waited — the app said it is in the background" || fail "away: $said"
  row "away" "a tap outside the app was not held up by it"

  demo
  sleep 1
  $M tap testid=busyCrash >/dev/null
  sleep 3
  # Android says the app stopped, in a dialog of its own; closing it is the
  # next thing anyone does. iOS goes to the home screen.
  if ! said=$($M tap "text=Close app" 2>/dev/null); then
    said=$($M tap 300 30)
  fi
  # The lease runs out — or, on Android, the app said it was in the
  # background on its way down. Either way it held nothing up.
  echo "$said" | grep -qE "not waited — (the app stopped saying it is busy .* \(it was busy with doomed\)|the app said it is in the background)" ||
    fail "crash: $said"
  row "crash" "the dead app's work held nothing up"
else
  echo "    (a phone: away and crash leave the app — ALLOW_PHONE=1 runs them)"
fi

if [ -n "$STREAM" ]; then
  demo
  case "$STREAM" in
    android) pkill -f "logcat -v epoch -T" || fail "no gray-box logcat to stop" ;;
    ios-sim) pkill -f "log stream --style ndjson --level default --predicate subsystem == \"dev.mobium.graybox\"" ||
           pkill -f "dev.mobium.graybox" || fail "no gray-box log stream to stop" ;;
  esac
  sleep 0.5
  said=$($M tap testid=busyRowC)
  echo "$said" | grep -q "not waited — not hearing the app" || fail "deaf: a tap with the stream stopped said $said"
  if [ "$STREAM" = ios-sim ]; then demo; else sleep 2; fi
  sleep 2
  $M tap testid=busyQuiet >/dev/null
  said=$($M tap testid=busyRowB)
  current "after the stream came back"
  echo "$said" | grep -q "waited [0-9]* ms for the app to go idle (busy: quiet)" || fail "not heard again: $said"
  row "deaf" "said it could not hear the app, then heard it again"
else
  # A real iPhone: its log stream is a connection inside the daemon, with no
  # process here to stop, so the daemon drops it on SIGUSR1 — the way an
  # unplugged cable or a restarted relay would — and the session stays. The
  # drop comes mid-work: the gray box must reconnect at the next action,
  # give the app time to restate the work it lost in the gap, wait it out,
  # and say so. The saying is the evidence the drop happened at all.
  demo
  pid=$("$ROOT/bin/mobium" daemon status --json | python3 -c 'import json, sys; print(json.load(sys.stdin)["pid"])')
  [ -n "$pid" ] || fail "no daemon PID to signal"
  sleep 2
  # The work starts inside the gap, so its busy line is lost: the alert is
  # opened while heard, the stream dropped, and the alert's Refresh accepted
  # — answering an alert waits on nothing, so nothing reconnects until the
  # tap on Row B. Only the app's restatement of its work can save that tap.
  $M tap testid=busyAlert >/dev/null
  kill -USR1 "$pid"
  sleep 0.3
  $M alert accept >/dev/null
  said=$($M tap testid=busyRowB)
  current "after work that started while the log stream was down"
  echo "$said" | grep -q "after its log stream dropped and was reconnected" || fail "deaf: the drop was not said: $said"
  echo "$said" | grep -q "waited [0-9]* ms for the app to go idle" || fail "deaf: not waited after the drop: $said"
  row "deaf" "work begun while the stream was down: reconnected, waited out"
fi
$M terminate "$APP" >/dev/null 2>&1 || true
echo "graybox-edges.sh: ok"
