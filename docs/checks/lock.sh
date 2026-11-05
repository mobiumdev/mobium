#!/bin/sh
# A locked device, end to end: straight after `lock lock`, an action is refused
# as device_not_ready naming the lock, and nothing is reported as done. Then
# `lock unlock`, and the same tap lands, which is what shows the refusal was the
# lock's.
#
#   docs/checks/lock.sh <emulator-serial | simulator-udid>
#
# CHALLENGES 389. On Android a touch at a point is refused too, since the screen
# is off. On iOS the lock screen stays on and takes touches, so `scroll-to` is
# what is asked: it must not swipe it, which can unlock it. Not on a phone: one
# with a PIN cannot be unlocked from outside.
#
# Needs MobiumApp installed (mobiumdev/mobium-app).
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-16s %-52s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp

case "$DEV" in
  *-*-*-*-*) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  emulator-*) PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
  *) echo "lock.sh is for an emulator or a simulator: a phone with a PIN cannot be unlocked from outside" >&2; exit 2 ;;
esac
echo "--- $DEV ($PLATFORM)"
at_exit "$M lock unlock >/dev/null 2>&1 || true"

# refused <what> <cmd args...>: exit 3, device_not_ready, naming the lock.
refused() {
  what="$1"; shift
  set +e; r=$($M "$@" 2>&1); st=$?; set -e
  [ $st -eq 3 ] || fail "$what: expected device_not_ready (3), got $st: $r"
  echo "$r" | grep -q 'lock unlock' || fail "$what: the refusal does not name the unlock: $r"
}

fresh() {
  $M lock unlock >/dev/null
  $M terminate $APP >/dev/null 2>&1 || true
  $M launch $APP >/dev/null
  $M wait "label=Login Demo" >/dev/null
  $M lock lock >/dev/null
}

fresh
refused "a tap" tap "label=Login Demo"
$M lock unlock >/dev/null
sleep 1
if $M find testid=username 2>/dev/null | grep -q 'username (input)'; then
  fail "a refused tap opened the Login Demo"
fi
row "tap" "refused, and it reached nothing"

# A lock screen holds nothing of the app: a target missing from it has not gone.
fresh
refused "a wait to be hidden" wait "label=Login Demo" --for hidden --timeout 2s
refused "a wait for it not to show" wait "label=Login Demo" --not --timeout 2s
refused "a wait for none" wait "label=Login Demo" --for count --count 0 --timeout 2s
refused "a wait to show" wait "label=Login Demo" --timeout 2s
refused "a find" find "label=Login Demo"
refused "a text" text "label=Login Demo"
row "read" "names the lock, and absence is not claimed"

fresh
if [ "$PLATFORM" = android ]; then
  refused "a swipe at a point" swipe 540 1500 540 1000
  refused "a long press at a point" long-press 540 1500
  row "at a point" "refused: the screen is off"
else
  refused "scroll-to" scroll-to testid=nothing
  $M lock | grep -q '^locked' || fail "scroll-to unlocked the device"
  row "scroll-to" "refused, and the device is still locked"
fi

$M lock unlock >/dev/null
$M wait "label=Login Demo" >/dev/null
$M tap "label=Login Demo" >/dev/null
$M wait testid=username >/dev/null || fail "the tap did not land once unlocked"
row "unlocked" "the same tap lands"

$M terminate $APP >/dev/null 2>&1 || true
echo PASS
