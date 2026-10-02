#!/bin/sh
# A Flutter app, end to end: MobiumApp's flutter/ demo, through the semantics
# tree Flutter builds for accessibility — no Flutter driver, no app changes.
# docs/APP-TYPES.md, "Cross-platform, or native-once-removed".
#
#   docs/checks/flutter.sh <android-serial | simulator-udid | iphone-udid>
#
# Needs the demo installed: on Android `flutter build apk` and adb install,
# on a simulator `flutter build ios --simulator` and simctl install.
#
# - map names every control: the fields by their labels, the password field
#   as a password once it holds one, the checkbox and switch with state, an
#   icon button by its tooltip.
# - Typing reaches the app, judged by what the app says it got, not by the
#   tree: on Android Flutter takes text only into the field with focus, and
#   the tree read back what nothing had received (CHALLENGES 206). The
#   password is never echoed — on iOS the field is a plain text field until
#   it holds something.
# - A Semantics identifier is a locator: testid=signIn taps the button,
#   which on iOS is a sibling of the identifier's element in the same frame.
# - check and uncheck reach a state; scroll-to reaches the fortieth row, and
#   on iOS that means reading a scroll view whose rows are its siblings.
# - Back from a pushed screen: the key on Android, the gesture on iOS.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <android-serial | simulator-udid | iphone-udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
case "$DEV" in
  ????????-????????????????|*-*-*-*-*) PLATFORM=ios; APP=dev.mobium.mobiumFlutter
    M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *) PLATFORM=android; APP=dev.mobium.mobium_flutter; M="$ROOT/bin/mobium --device $DEV" ;;
esac
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-60s ok\n' "$1" "$2"; }
echo "--- $DEV ($PLATFORM)"

$M apps | grep -q "$APP" || fail "$APP is not installed — build and install MobiumApp's flutter/ demo first"
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null; sleep 2
MAP=$($M map)
for want in "Username (input)" "Settings (button)" "Sign In (button)" "Remember me (" "Notifications (switch, checked)"; do
  echo "$MAP" | grep -qF "$want" || fail "map lacks \"$want\": $MAP"
done
row "map" "fields by label, a tooltip, a checkbox and a switch with state"

out=$($M type "label=Username" lana)
echo "$out" | grep -q 'typed "lana"' || fail "typing the username: $out"
out=$($M type testid=password secret12 2>&1) || fail "typing the password: $out"
echo "$out" | grep -q "not echoed" || fail "the password was not reported as one: $out"
echo "$out" | grep -q secret12 && fail "the password was echoed: $out"
$M map | grep -q "(password)" || fail "the password field does not map as a password once it holds one"
if [ "$PLATFORM" = ios ]; then $M keyboard --key enter >/dev/null 2>&1 || true; fi
$M tap testid=signIn >/dev/null
sleep 1
$M text | grep -q "Signed in as lana with 8 characters" || fail "the app did not get what was typed: $($M text | grep -m1 'igned')"
row "type" "the app got both fields; the password not echoed"

$M tap "label=Tap me" >/dev/null
$M text | grep -q "Taps: 1" || fail "the tap did not reach the app"
$M uncheck "label=Notifications" >/dev/null
$M check "label=Remember me" >/dev/null
$M map | grep -q "Notifications (switch, unchecked)" || fail "the switch did not reach unchecked"
row "act" "a counted tap; a switch unchecked, a checkbox checked"

out=$($M scroll-to "label=Row 40" 2>&1) || fail "scrolling to the last row: $out"
$M scroll-to "label=Details" --direction up >/dev/null
row "scroll" "$(echo "$out" | sed 's/ —.*//')"

$M tap "label=Details" >/dev/null; sleep 1
$M text | grep -q "The details screen" || fail "Details did not open"
if [ "$PLATFORM" = android ]; then out=$($M press back); else out=$($M press back --gesture); fi
sleep 1
$M text | grep -q "The details screen" && fail "back left the app on its details screen: $out"
echo "$out" | grep -q "$APP is still in the foreground" || fail "back did not stay in the app: $out"
row "back" "from the pushed screen, by the $([ "$PLATFORM" = android ] && echo key || echo gesture)"
echo PASS
