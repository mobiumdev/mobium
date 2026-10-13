#!/bin/sh
# Sliders on iOS, on MobiumApp's Slider Demo: three kinds, each handled the
# way the one fill rule says — a slider is filled with a position from 0 to
# 1, and a control Mobium cannot set to a position is refused, naming a drag.
#
#   docs/checks/slider-ios.sh <simulator-udid|iphone-udid>
#
#   - Volume, @react-native-community/slider as shipped: on iOS a plain view
#     with no trait and no value, so map calls it a button. app_fill is
#     refused as unsupported, naming app_swipe, with nothing moved
#     (CHALLENGES 318); a drag on its thumb lands it, in steps of 10.
#   - Level, the same slider made accessible: the Adjustable trait and a
#     value. Mapped as adjustable with its value; app_fill refused naming a
#     drag (234); the drag lands it and map reads the new value.
#   - Range, a web page's <input type="range">, 0 to 100 in steps of 10: a
#     slider by its label, filled by position, landing exactly from the
#     page's own min, max and step; a value and app_type refused (319).
#
# Android's sliders are slider.sh. Real UIKit and SwiftUI sliders, which
# app_fill moves (219), are icecubes-ios.sh.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <simulator-udid|iphone-udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_platform ios "$DEV"
check_lock "$DEV"
M="$ROOT/bin/mobium --device $DEV"
APP=dev.mobium.mobiumapp
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-10s %-60s ok\n' "$1" "$2"; }

# refused <exit> <words> <command...>: refused with that exit status, saying so.
refused() {
  want=$1 words=$2; shift 2
  set +e; out=$("$@" 2>&1); got=$?; set -e
  [ "$got" = "$want" ] || fail "$*: exit $got, want $want: $out"
  echo "$out" | grep -q "$words" || fail "$*: refused without saying \"$words\": $out"
}
state() { $M text testid=sliderState; }
# The track's bounds in device pixels, from map: x1 x2 y.
track() { $M map --json | python3 -c '
import json, sys
for e in json.load(sys.stdin)["elements"]:
    if e["label"] == sys.argv[1]:
        b = e["bounds"]; print(b["x1"], b["x2"], (b["y1"] + b["y2"]) // 2); break' "$1"; }
# drag <label> <from> <to>: a finger on the thumb at one fraction of the
# track, moved to another.
drag() {
  set -- "$1" "$2" "$3" $(track "$1")
  [ -n "$6" ] || fail "no bounds for $1 in map"
  x() { python3 -c "print(int($2 + $1 * ($3 - $2)))"; }
  $M swipe "$(x "$2" "$4" "$5")" "$6" "$(x "$3" "$4" "$5")" "$6" --duration 800ms >/dev/null
}

echo "--- $DEV"
$M terminate "$APP" >/dev/null 2>&1 || true
out=$($M launch "$APP" 2>&1) || fail "launch: $out"
$M scroll-to 'text="Slider Demo"' --direction down >/dev/null || fail "MobiumApp has no Slider Demo — rebuild it"
$M tap 'text="Slider Demo"' >/dev/null
$M wait testid=sliderState --timeout 10s >/dev/null || fail "the Slider Demo did not open"
case "$(state)" in
  "volume: 50, balance: 0.50, level: 5, changes: 0") ;;
  *) fail "the Slider Demo has no Level, or did not start fresh: $(state) — rebuild MobiumApp" ;;
esac

map=$($M map)
echo "$map" | grep -q " Volume (button)" || fail "Volume is no longer a plain view to map — has the library changed? $map"
echo "$map" | grep -q " Level (adjustable, 50%)" || fail "Level is not adjustable at 50%: $map"
row "map" "Volume a plain view (button); Level adjustable, 50%"

refused 5 "not a text field or a slider" $M fill testid=volumeSlider 0.2
echo "$(state)" | grep -q "^volume: 50," || fail "a refused fill moved Volume: $(state)"
drag Volume 0.5 0.2
echo "$(state)" | grep -q "^volume: 20," || fail "a drag from 50% to 20% left $(state)"
row "volume" "fill refused, naming app_swipe; a drag lands it on 20"

refused 2 "app_swipe" $M fill testid=levelSlider 0.8
echo "$(state)" | grep -q "level: 5," || fail "a refused fill moved Level: $(state)"
drag Level 0.5 0.8
echo "$(state)" | grep -q "level: 8," || fail "a drag from 50% to 80% left $(state)"
$M map | grep -q " Level (adjustable, 80%)" || fail "map does not read Level at 80%"
row "level" "fill refused, naming a drag; the drag lands it on 8"

WEB=$($M contexts | awk '/^WEBVIEW_/ {print $1; exit}')
[ -n "$WEB" ] || fail "the Slider Demo's page is not inspectable — rebuild MobiumApp"
at_exit "$M context NATIVE_APP >/dev/null 2>&1"
$M context "$WEB" >/dev/null
$M map | grep -q " Range, 0 to 100 in steps of 10 (slider, 50)" || fail "the range does not map as a slider by its label: $($M map)"
for p in "0.2 20" "1 100" "0 0" "0.35 40"; do
  set -- $p
  $M fill testid=webRange "$1" >/dev/null || fail "fill the range to $1"
  $M text | grep -q "^range: $2\$" || fail "the range filled to $1 reads $($M text | tail -1), want $2"
done
refused 2 "from 0" $M fill testid=webRange 20
refused 2 "app_fill" $M type testid=webRange 20
$M text | grep -q "^range: 40\$" || fail "a refused fill or type moved the range"
row "range" "a slider by its label; 0.2, 1, 0, 0.35 land on 20, 100, 0, 40"
row "refusals" "a value and app_type refused before moving"

$M context NATIVE_APP >/dev/null
$M terminate "$APP" >/dev/null 2>&1 || true
echo PASS
