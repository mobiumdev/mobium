#!/bin/sh
# Sliders, on MobiumApp's Slider Demo: Volume, 0 to 100 in steps of 10, and
# Balance, continuous from 0 to 1, both labeled and neither the device's, so
# moving them changes nothing a person owns.
#
#   docs/checks/slider.sh <android-serial>
#
# Android only, for now. On iOS the Slider Demo's sliders are not sliders to
# anything outside the app: @react-native-community/slider reports each as a
# plain XCUIElementTypeOther with no Adjustable trait and no value, so iOS —
# VoiceOver included — has nothing to call a slider, and WebDriverAgent moves
# only a Slider element. Ice Cubes' native sliders are the iOS control, in
# icecubes-ios.sh. See ROADMAP, "Sliders".
#
# A slider is filled with a position from 0 (the start of its track) to 1
# (the end), and the app says where it landed (CHALLENGES 219, 227):
#
#   - both map as sliders, by their names, with a value — the bar's own
#     progress, which a seek bar reports as its text — and neither as a
#     button named by its value;
#   - Volume lands on 30, 100 and 0 as the app counts them, and Balance on
#     0.25 within 0.03: a seek bar is touched on its track and lands
#     exactly. Each fill starts from an end anyway, as it must on iOS;
#   - map --diff says the value moved;
#   - a position that is not a number from 0 to 1, and app_type, are refused.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
case "$DEV" in
  *-*-*-*-*|????????-????????????????)
    echo "slider.sh is Android only: on iOS the Slider Demo's sliders report no slider at all — see the top of this file" >&2
    exit 2 ;;
  *) M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV"

APPS=$($M apps 2>&1) || fail "could not list the apps: $APPS"
echo "$APPS" | grep -q "$APP" || fail "$APP is not installed — build it first (see docs/checks/mobium-app.sh)"
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M scroll-to "label=Slider Demo" >/dev/null 2>&1 || true
$M tap "label=Slider Demo" >/dev/null || fail "MobiumApp has no Slider Demo — rebuild it"
state() { $M text testid=sliderState; }
volume() { state | sed -n 's/^volume: \([0-9]*\),.*/\1/p'; }
balance() { state | sed -n 's/.*balance: \([0-9.]*\),.*/\1/p'; }

map=$($M map)
VOL=$(echo "$map" | awk '/ Volume \(slider, / { print $1; exit }')
BAL=$(echo "$map" | awk '/ Balance \(slider, / { print $1; exit }')
[ -n "$VOL" ] && [ -n "$BAL" ] || fail "the sliders do not map as sliders by their names"
echo "$map" | grep -qE '^@e[0-9]+ [0-9.%]+ \(button\)' && fail "a slider maps as a button named by its value"
row "map" "Volume and Balance, each a slider with its value"

for p in 0.3 1 0; do
  $M fill "$VOL" 0 >/dev/null
  $M fill "$VOL" "$p" >/dev/null
  want=$(python3 -c "print(int(round($p*100)))")
  [ "$(volume)" = "$want" ] || fail "Volume filled to $p reads $(volume) in the app, want $want"
done
row "volume" "30, 100 and 0, as the app counts them"

$M fill "$BAL" 0 >/dev/null
$M fill "$BAL" 0.25 >/dev/null
python3 -c "import sys; sys.exit(0 if abs(float('$(balance)') - 0.25) <= 0.03 else 1)" \
  || fail "Balance filled to 0.25 reads $(balance) in the app"
row "balance" "0.25, read back as $(balance)"

$M map >/dev/null
$M fill "$VOL" 0.5 >/dev/null
$M map --diff | grep -q "Volume (slider, .*) — was " || fail "map --diff did not say Volume moved"
row "diff" "map --diff says the value moved"

out=$($M fill "$VOL" abc 2>&1) && fail "fill abc on a slider was accepted: $out"
[ "$(volume)" = 50 ] || fail "a refused fill moved Volume: it reads $(volume)"
out=$($M type "$VOL" 5 2>&1) && fail "type on a slider was accepted: $out"
echo "$out" | grep -q "app_fill" || fail "type on a slider was refused without naming app_fill: $out"
row "refusals" "abc refused before moving; type refused, naming app_fill"

$M terminate "$APP" >/dev/null 2>&1 || true
echo PASS
