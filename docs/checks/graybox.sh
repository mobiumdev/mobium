#!/bin/sh
# Gray box, on MobiumApp's Busy Demo: work that finishes after the screen
# looks finished. Refresh quietly starts 0.4 to 1.6 seconds of work and leaves
# the old rows up meanwhile; a row says whether the one tapped was current.
# MobiumApp links Mobium's gray-box library, which is silent unless the app
# was launched with --gray-box.
#
#   docs/checks/graybox.sh <serial|udid> [rounds]
#
# A simulator, a real iPhone, an emulator or an Android phone.
#
#   - launched the ordinary way, tap Refresh quietly then Row B: some taps
#     land on a stale row. If none does, the run proves nothing — a check
#     that cannot fail is not a check — and it says so;
#   - launched with --gray-box, the same taps: every one is current, and
#     each result says what the gray box waited for;
#   - Refresh, which shows a spinner, is current either way: what can be
#     seen is waited for already;
#   - --gray-box on an app without the library says it did not answer, and
#     actions are not waited for — Settings, on a simulator or an emulator
#     only: on Android --gray-box starts the app afresh, and a person's
#     Settings is not stopped for a check.
set -e
DEV="$1"
ROUNDS="${2:-10}"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid> [rounds]" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
M="$ROOT/bin/mobium --device $DEV"
echo "--- $DEV"

APPS=$($M apps 2>&1) || fail "could not list the apps: $APPS"
echo "$APPS" | grep -q "$APP" || fail "$APP is not installed — build it first (see docs/checks/mobium-app.sh)"

open_demo() {
  $M terminate "$APP" >/dev/null 2>&1 || true
  out=$($M launch $1 "$APP") || fail "launch $1: $out"
  $M scroll-to "label=Busy Demo" --direction down >/dev/null 2>&1 || true
  $M tap "label=Busy Demo" >/dev/null || fail "MobiumApp has no Busy Demo — rebuild it"
  echo "$out"
}

# trials <button> <rounds>: tap the button then Row B, and count the rows the
# app says were stale.
trials() {
  stale=0
  i=0
  while [ "$i" -lt "$2" ]; do
    # The work takes up to 1.6s, and a trial started while the last one's
    # is still running taps a stale row whatever the button: on a simulator,
    # back to back, it did.
    sleep 2
    $M tap "testid=$1" >/dev/null
    $M tap testid=busyRowB >/dev/null
    case "$($M text testid=busyOutcome)" in
      *": current") ;;
      *": stale") stale=$((stale + 1)) ;;
      *) fail "the Busy Demo's outcome reads $($M text testid=busyOutcome)" ;;
    esac
    i=$((i + 1))
  done
  echo "$stale"
}

out=$(open_demo "")
echo "$out" | grep -q "gray box" && fail "an ordinary launch mentioned the gray box: $out"
black=$(trials busyQuiet "$ROUNDS")
[ "$black" -gt 0 ] || fail "black box was current $ROUNDS times of $ROUNDS on the quiet refresh, so this run cannot show the gray box does anything — run more rounds"
row "black box" "$black of $ROUNDS taps after a quiet refresh were stale"
seen=$(trials busyRefresh 3)
[ "$seen" -eq 0 ] || fail "black box tapped a stale row after a visible refresh ($seen of 3)"
row "control" "a refresh with a spinner is waited for already"

# The app is still running from the ordinary launch: --gray-box must start
# it afresh, or a launch argument never reaches it (iOS kept the running
# app and its old arguments until this launch stopped it first).
out=$($M launch --gray-box "$APP") || fail "launch --gray-box: $out"
echo "$out" | grep -q "with the gray box: every action waits" || fail "an app already running did not answer the gray box: $out"
row "relaunch" "an app already running is started afresh, and answers"
out=$(open_demo --gray-box)
echo "$out" | grep -q "with the gray box: every action waits" || fail "the app did not answer the gray box: $out"
row "launch" "the app answered the gray box"
gray=$(trials busyQuiet "$ROUNDS")
[ "$gray" -eq 0 ] || fail "with the gray box, $gray of $ROUNDS taps after a quiet refresh were stale"
row "gray box" "0 of $ROUNDS taps after a quiet refresh were stale"
sleep 2
$M tap testid=busyQuiet >/dev/null
said=$($M tap testid=busyRowB)
echo "$said" | grep -q "gray box: waited [0-9]* ms for the app to go idle (busy: quiet)" \
  || fail "a tap that waited out work did not say so: $said"
row "report" "the tap says it waited, and for what"
$M terminate "$APP" >/dev/null 2>&1 || true

case "$DEV" in
  *-*-*-*-*) SETTINGS=com.apple.Preferences ;;
  emulator-*) SETTINGS=com.android.settings ;;
  *) SETTINGS= ;;
esac
if [ -n "$SETTINGS" ]; then
  out=$($M launch --gray-box "$SETTINGS")
  echo "$out" | grep -q "has not answered the gray box" || fail "an app without the library was not reported as unheard: $out"
  $M terminate "$SETTINGS" >/dev/null 2>&1 || true
  row "unheard" "an app without the library launches, and says it did not answer"
else
  echo "    (a phone: the app-without-the-library case runs on a simulator or an emulator)"
fi
echo "graybox.sh: ok"
