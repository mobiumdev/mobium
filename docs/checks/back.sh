#!/bin/sh
# Back, end to end: the key and the gesture, each held to what it says it did
# and to the screen it left behind. docs/BACK.md has the measurements.
#
#   docs/checks/back.sh <android-serial | simulator-udid | iphone-udid>
#
# Needs MobiumApp with Android back handled (mobium-app #12 or later).
#
# Android:
# - `devices` names the navigation mode, since it decides what an edge swipe is.
# - From a demo, the key and the gesture each go back to MobiumApp's home and
#   say the app is still in front; from home the key says it left the app —
#   the difference MobiumApp's first report was about. Settings is the control.
# - On an emulator, with three-button navigation, the gesture is refused and
#   says why, and the key still goes back. The mode is put back afterwards and
#   read back. A phone's mode is never changed: it is device-wide.
#
# iOS:
# - `press back` is refused, and the refusal names the gesture.
# - The gesture in Settings > Search on a simulator, or NetNewsWire's feed on
#   a phone, goes back, and says what the navigation bar says now. Settings'
#   first page is never read on a phone: it names the owner.
# - The gesture in MobiumApp, which has no navigation stack, changes nothing,
#   and says the app is still in front — Mobium reports, the app decides.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <android-serial | simulator-udid | iphone-udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
APP=dev.mobium.mobiumapp
case "$DEV" in
  ????????-????????????????) PLATFORM=ios; PHONE=1 ;;
  *-*-*-*-*) PLATFORM=ios; PHONE="" ;;
  emulator-*) PLATFORM=android; PHONE="" ;;
  *) PLATFORM=android; PHONE=1 ;;
esac
if [ "$PLATFORM" = ios ]; then M="$ROOT/bin/mobium --driver wda --device $DEV"; else M="$ROOT/bin/mobium --device $DEV"; fi
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-60s ok\n' "$1" "$2"; }
echo "--- $DEV ($PLATFORM${PHONE:+, a phone})"

demo() { $M terminate $APP >/dev/null 2>&1 || true; $M launch $APP >/dev/null; sleep 1
  $M tap "label=Login Demo" >/dev/null; sleep 1
  $M map | grep -q "Back (button)" || fail "Login Demo did not open"; }
home() { $M map | grep -q "Login Demo (button)"; }

if [ "$PLATFORM" = android ]; then
  line=$("$ROOT/bin/mobium" devices | grep "^$DEV " || true)
  [ -n "$line" ] || fail "$DEV is not listed by mobium devices — is it connected?"
  nav=$(echo "$line" | sed -n 's/.*navigation: \([a-z-]*\).*/\1/p')
  [ -n "$nav" ] || fail "devices does not name the navigation mode"
  row "devices" "navigation: $nav"

  demo; t0=$(date +%s)
  out=$($M press back)
  echo "$out" | grep -q "$APP is still in the foreground" || fail "back from a demo: $out"
  home || fail "back from a demo did not reach MobiumApp's home"
  row "key" "demo to home, and says the app is still in front ($(( $(date +%s) - t0 ))s)"

  out=$($M press back)
  echo "$out" | grep -q "it left $APP" || fail "back from home did not say it left the app: $out"
  row "key" "from home, and says it left $APP"

  if [ "$nav" = gestures ]; then
    demo
    out=$($M press back --gesture)
    echo "$out" | grep -q "swiped back" && echo "$out" | grep -q "$APP is still in the foreground" ||
      fail "the gesture from a demo: $out"
    home || fail "the gesture from a demo did not reach MobiumApp's home"
    row "gesture" "demo to home, a swipe in from the edge"

    $M launch com.android.settings >/dev/null; sleep 1
    $M map | grep -q "Navigate up" && { $M press back >/dev/null; sleep 1; }
    $M tap "text=Network & internet" >/dev/null 2>&1 || $M tap "text=Display" >/dev/null; sleep 1
    $M map | grep -q "Navigate up" || fail "Settings did not open a sub-page"
    out=$($M press back --gesture)
    echo "$out" | grep -q "com.android.settings is still in the foreground" || fail "the gesture in Settings: $out"
    $M map | grep -q "Navigate up" && fail "the gesture left Settings on its sub-page"
    row "control" "Settings sub-page to its parent, by the gesture"
  fi

  if [ -z "$PHONE" ]; then
    adb -s "$DEV" shell cmd overlay enable com.android.internal.systemui.navbar.threebutton
    sleep 4
    restore() {
      adb -s "$DEV" shell cmd overlay enable com.android.internal.systemui.navbar.gestural; sleep 4
      [ "$(adb -s "$DEV" shell settings get secure navigation_mode | tr -d '\r')" = 2 ] ||
        { echo "FAIL: gesture navigation was not put back" >&2; exit 1; }
    }
    demo
    set +e; out=$($M press back --gesture 2>&1); status=$?; set -e
    [ "$status" = 5 ] && echo "$out" | grep -q "three-button" || { restore; fail "the gesture with three buttons ($status): $out"; }
    $M map | grep -q "Back (button)" || { restore; fail "the refused gesture moved the screen"; }
    out=$($M press back)
    echo "$out" | grep -q "$APP is still in the foreground" && home || { restore; fail "the key with three buttons: $out"; }
    restore
    row "buttons" "three-button: the gesture refused and why; the key goes back"
    row "restored" "gesture navigation, read back"
  fi
  echo PASS; exit 0
fi

# iOS.
set +e; out=$($M press back 2>&1); status=$?; set -e
[ "$status" = 5 ] && echo "$out" | grep -q -- "--gesture" || fail "press back on iOS ($status): $out"
row "refused" "no back button, and the refusal names the gesture"

if [ -z "$PHONE" ]; then
  $M launch com.apple.Preferences >/dev/null; sleep 2
  $M tap "label=Settings,role=button" >/dev/null 2>&1 || true; sleep 1
  $M tap "label=Search,role=button" >/dev/null; sleep 2
  out=$($M press back --gesture)
  echo "$out" | grep -q 'navigation bar now says "Settings"' || fail "the gesture in Settings: $out"
  row "control" "Settings > Search to Settings, and the bar's new title said"
else
  NNW=$($M apps | awk '/NetNewsWire/ { print $1; exit }')
  [ -n "$NNW" ] || fail "NetNewsWire is the control on a phone, and it is not installed"
  $M launch "$NNW" >/dev/null; sleep 2
  $M tap "label=Feeds,role=button" >/dev/null 2>&1 || true; sleep 1
  $M tap "text=NetNewsWire Blog" >/dev/null; sleep 2
  out=$($M press back --gesture)
  echo "$out" | grep -q 'navigation bar now says "Feeds"' || fail "the gesture in NetNewsWire: $out"
  row "control" "a feed to Feeds in NetNewsWire, and the bar's new title said"
fi

demo
out=$($M press back --gesture)
echo "$out" | grep -q "$APP is still in the foreground" || fail "the gesture in MobiumApp: $out"
$M map | grep -q "Back (button)" || fail "the gesture moved MobiumApp, which has no swipe back"
$M tap "label=Back" >/dev/null
row "gesture" "MobiumApp has no swipe back: nothing moved, and it said so"
echo PASS
