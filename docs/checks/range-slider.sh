#!/bin/sh
# A slider with two thumbs, natively: Morpho Market's price range — Material
# 3's RangeSlider on Android, and on iOS a control the app draws, as SwiftUI
# has none — judged by what the app says its range is. CHALLENGES 357, 358.
#
#   docs/checks/range-slider.sh <android-serial | simulator-udid>
#
# - The Price chip is off the chip row's edge, and scroll-to right reaches
#   it: the row, not the catalog grid below (358).
# - Each thumb is mapped as a slider or adjustable element with its value,
#   and filled with a position: 0.5, 0.25, 0.75, 0.1 and the ends land
#   exactly, as the app's own line says — "Price: $15 – $45" — while the
#   thumbs start at the screen's edges, where a drag is the back gesture.
# - A fill the other thumb blocks says what it reads — on Android the
#   thumb is then drawn under the other and only one is in the tree — and
#   presses nothing on its way; the app is still in front after every fill.
#
# Needs Morpho Market (mobiumdev/morpho-market) with the price range. Nothing
# here changes the device: the range is the app's own, and a reset puts it
# back.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <android-serial | simulator-udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
case "$DEV" in
  ????????-????-????-????-????????????) M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *) M="$ROOT/bin/mobium --device $DEV" ;;
esac
APP=dev.mobium.shop
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
echo "--- $DEV"

apps=$($M apps 2>&1) || fail "cannot list the apps: $(echo "$apps" | tail -1)"
echo "$apps" | grep -q "$APP" || fail "$APP is not installed — build and install Morpho Market first"
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M wait testid=catalogList --timeout 15s >/dev/null || fail "the catalog did not appear"

out=$($M scroll-to testid=priceFilter --direction right 2>&1) || fail "the Price chip was not reached: $out"
$M tap testid=priceFilter >/dev/null
$M wait 'label="Range start"' >/dev/null || fail "this Morpho Market has no price range — rebuild it"
row "scroll-to" "the Price chip, off the row's edge, one scroll right"

map=$($M map)
for want in 'Range start (' 'Range end ('; do
  echo "$map" | grep -qF "$want" || fail "map lacks $want: $map"
done
echo "$map" | grep -E 'Range (start|end) \((slider|adjustable), .*(0|60)' >/dev/null || fail "the thumbs carry no value: $map"
row "map" "each thumb a slider with its value"

# fill <thumb> <position> <the app's line after>
fill() {
  out=$($M fill "label=\"$1\"" "$2" 2>&1) || fail "fill $1 $2: $out"
  [ "$($M current)" = "$APP" ] || fail "fill $1 $2 left $APP: $($M current) is in front"
  got=$($M text testid=priceRangeLabel)
  [ "$got" = "$3" ] || fail "fill $1 $2: the app says \"$got\", want \"$3\" ($out)"
}
fill "Range end" 0.5 'Price: $0 – $30'
fill "Range start" 0.25 'Price: $15 – $30'
fill "Range end" 0.75 'Price: $15 – $45'
row "fill" "both thumbs from the screen's edges: 0.5, 0.25, 0.75"
fill "Range start" 0.1 'Price: $6 – $45'
fill "Range end" 1 'Price: $6 – $60'
fill "Range start" 0 'Price: $0 – $60'
row "ends" "0.1, then each thumb to its end"

fill "Range end" 0.5 'Price: $0 – $30'
out=$($M fill 'label="Range start"' 0.9 2>&1) || fail "a blocked fill failed: $out"
got=$($M text testid=priceRangeLabel)
[ "$got" = 'Price: $30 – $30' ] || fail "the start thumb crossed the end: \"$got\""
echo "$out" | grep -Eq 'reads? "\$?30' || fail "a fill the other thumb stopped does not say where: $out"
# Android then lists the end thumb alone, drawn over the start: the read-back
# looked for the start by scrolling, and swiped the chip row (357, 358).
$M map | grep -E 'Accessories \(button, (checked|selected)' >/dev/null &&
  fail "a category was pressed during the fill: $($M map | grep -E 'Accessories|Apparel|Drinkware')"
row "blocked" "start stops at the end thumb, says what both read, presses nothing"
echo PASS
