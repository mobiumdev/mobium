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
# icecubes-ios.sh, and the Slider Demo on iOS — that plain view, an
# accessible one and a web range — is slider-ios.sh. See ROADMAP, "Sliders".
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
#   - a position that is not a number from 0 to 1, and app_type, are refused;
#   - a web page's range, which the WebView draws as a seek bar named by its
#     label, is filled through the page and lands exactly (CHALLENGES 319).
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
# refused <exit> <words> <command...>: refused with that exit status, saying so.
refused() {
  want=$1 words=$2; shift 2
  set +e; out=$("$@" 2>&1); got=$?; set -e
  [ "$got" = "$want" ] || fail "$*: exit $got, want $want: $out"
  echo "$out" | grep -q "$words" || fail "$*: refused without saying \"$words\": $out"
}
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
case "$DEV" in
  *-*-*-*-*|????????-????????????????)
    echo "slider.sh is Android only: on iOS the Slider Demo is slider-ios.sh — see the top of this file" >&2
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
# Waited for, or the first map can read the screen being left (CHALLENGES 233).
$M wait testid=sliderState --timeout 10s >/dev/null || fail "the Slider Demo did not open"
$M wait 'text="Range, 0 to 100 in steps of 10",role=slider' --timeout 10s >/dev/null || fail "the Slider Demo's page did not load — rebuild MobiumApp"
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

# A web page's range (CHALLENGES 319): Android's WebView draws it as a seek
# bar whose text is its label and which reports no progress, so natively it
# is named by that label, and a fill goes through the page, which lands it
# exactly from its own min, max and step. In the page it is a slider too.
RANGE='text="Range, 0 to 100 in steps of 10",role=slider'
$M map | grep -q " Range, 0 to 100 in steps of 10 (slider)" || fail "the page's range is not a slider named by its label natively: $($M map)"
out=$($M fill "$RANGE" 0.3 2>&1) || fail "fill the page's range natively: $out"
echo "$out" | grep -q 'reads "30"' || fail "the page's range filled to 0.3 natively: $out"
WEB=$($M contexts | awk '/^WEBVIEW_/ {print $1; exit}')
[ -n "$WEB" ] || fail "the Slider Demo's page is not inspectable — rebuild MobiumApp"
at_exit "$M context NATIVE_APP >/dev/null 2>&1"
$M context "$WEB" >/dev/null
$M map | grep -q " Range, 0 to 100 in steps of 10 (slider, 30)" || fail "the range does not map as a slider at 30 in its page: $($M map)"
$M fill testid=webRange 0.8 >/dev/null || fail "fill the range in its page"
$M text | grep -q "^range: 80\$" || fail "the range filled to 0.8 reads $($M text | tail -1)"
out=$($M type testid=webRange 20 2>&1) && fail "type on the page's range was accepted: $out"
# Two thumbs on one track, as pages build them (CHALLENGES 320): two ranges
# laid over each other, and two ARIA sliders, which map did not list. Each
# thumb is its own slider. A thumb pushed past the other is the page's to
# stop, and fill says the page stopped it rather than claiming the position.
$M map | grep -q " Minimum price (slider, 200)" || fail "Price's thumbs are not two sliders: $($M map)"
$M map | grep -q " Start hour (slider, 9:00)" || fail "the ARIA sliders are not in map: $($M map)"
$M fill testid=priceMin 0.5 >/dev/null || fail "fill Price's minimum"
$M fill testid=priceMax 0.9 >/dev/null || fail "fill Price's maximum"
$M text | grep -q "^price: 500 to 900\$" || fail "Price reads $($M text | grep '^price')"
refused 7 "rewrote" $M fill testid=priceMax 0.3
$M text | grep -q "^price: 500 to 500\$" || fail "the maximum pushed below the minimum left $($M text | grep '^price') — the other thumb moved"
$M fill testid=hoursStart 0.25 >/dev/null || fail "fill the start hour"
$M fill testid=hoursEnd 0.5 >/dev/null || fail "fill the end hour"
$M text | grep -q "^hours: 6 to 12\$" || fail "Hours reads $($M text | grep '^hours')"
refused 7 "stopped it at 12:00" $M fill testid=hoursStart 1
row "two thumbs" "Price and Hours each two sliders; a crossing is the page's to stop"
$M context NATIVE_APP >/dev/null
row "web range" "named by its label natively; 0.3 and 0.8 land on 30 and 80"

$M terminate "$APP" >/dev/null 2>&1 || true
echo PASS
