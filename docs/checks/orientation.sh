#!/bin/sh
# Orientation, read back from the device: an app that turns is turned, and
# an app pinned to portrait is refused rather than reported turned.
#
#   docs/checks/orientation.sh <android-serial | simulator-udid | iphone-udid>
#
# The app that turns is Settings on Android and Safari on iOS. The pinned one
# is MobiumApp, which declares portrait alone. On iOS, portrait-reverse is
# refused too: a Face ID iPhone never turns upside down, in any app. And iOS
# has no "auto" — nothing outside the device hands rotation back to it — so
# that is refused there, while Android's is checked.
#
# Leaves the device as it found it, which is itself asserted: a session puts
# the orientation back when it ends, and on Android the rotation lock too.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-18s %-50s ok\n' "$1" "$2"; }
M="$ROOT/bin/mobium --device $DEV"
# A simulator is a UUID of five groups and a real iPhone's UDID two; anything
# else, emulator-5554 included, is an Android serial.
case "$DEV" in
  ????????-????????????????|*-*-*-*-*) PLATFORM=ios; TURNS=com.apple.mobilesafari ;;
  *) PLATFORM=android; TURNS=com.android.settings ;;
esac
PINNED=dev.mobium.mobiumapp
echo "--- $DEV ($PLATFORM)"

before=$($M orientation)

$M launch $TURNS >/dev/null
for o in landscape landscape-reverse portrait; do
  out=$($M orientation $o 2>&1) || fail "$TURNS did not turn to $o: $out"
  case "$out" in "$o "*) ;; *) fail "asked for $o and read back: $out" ;; esac
done
row "turns" "$TURNS: landscape, landscape-reverse, portrait"

if [ "$PLATFORM" = ios ]; then
  out=$($M orientation portrait-reverse 2>&1) && fail "portrait-reverse was reported done on iOS: $out"
  echo "$out" | grep -q "upside down" || fail "the portrait-reverse refusal did not say why: $out"
  row "portrait-reverse" "refused: never upside down on a Face ID iPhone"
  out=$($M orientation auto 2>&1) && fail "auto was reported done on iOS: $out"
  echo "$out" | grep -q "physically turned" || fail "the auto refusal did not say why: $out"
  row "auto" "refused: nothing hands rotation back from outside"
fi

$M launch $PINNED >/dev/null
out=$($M orientation landscape 2>&1) && fail "MobiumApp, which is portrait only, was reported turned: $out"
echo "$out" | grep -q "still" || fail "the refusal did not say where it stayed: $out"
echo "$out" | grep -q "taken back" || fail "the refusal did not say the request was taken back: $out"
case "$($M orientation)" in portrait*) ;; *) fail "MobiumApp is no longer portrait after the refusal" ;; esac
row "pinned" "MobiumApp refused landscape and stayed portrait"

# The refused request must not turn the next app: on Android it locked the
# device to landscape and Settings opened sideways (CHALLENGES 308), as the
# iPhone's next apps did before 218.
$M launch $TURNS >/dev/null
sleep 1
case "$($M orientation)" in portrait*) ;; *) fail "$TURNS opened turned after MobiumApp's refusal: $($M orientation)" ;; esac
row "taken back" "$TURNS opened upright after the refusal"

# A session puts the orientation back as it found it, lock and all
# (CHALLENGES 308): turned here, and back once the session ends.
$M orientation landscape >/dev/null || fail "$TURNS did not turn for the session check"
$M session end >/dev/null
sleep 1
after=$($M orientation)
[ "${after%% *}" = "${before%% *}" ] || fail "after the session: $after, before it: $before"
if [ "$PLATFORM" = android ]; then
  [ "$after" = "$before" ] || fail "after the session the lock is not as it was: $after, before it: $before"
fi
row "session end" "back as it was: ${before%% *}"
echo "PASS"
