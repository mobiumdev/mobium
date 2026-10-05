#!/bin/sh
# Frames inside a WebView, on MobiumApp's Frames page: a same-origin frame, a
# frame nested inside it, and a cross-origin one (a data: URL). Every button
# reports to the page, so frameOutcome says which frame a tap reached.
#
#   docs/checks/frames.sh <simulator-udid | iphone-udid>
#   docs/checks/frames.sh <android-serial>
#
# A WebView's map listed the page's own button and nothing inside its frames
# (CHALLENGES 228). Now, in the WebView's context:
#
#   - every frame's elements are mapped, each saying which frame it is in —
#     the same-origin and nested ones through the page, the cross-origin one
#     through its own execution context (CHALLENGES 229) — and a tap on each
#     button reaches its frame, as the page says;
#   - the cross-origin frame's field, a card field in a frame of its own, is
#     filled and typed into, and the frame tells the page what it holds;
#   - and NATIVE_APP reaches the cross-origin button too, through the
#     platform's accessibility.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
case "$DEV" in
  *-*-*-*-*|????????-????????????????) M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *) M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV"

APPS=$($M apps 2>&1) || fail "could not list the apps: $APPS"
echo "$APPS" | grep -q "$APP" || fail "$APP is not installed — build it first (see docs/checks/mobium-app.sh)"
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M tap "label=WebViews" >/dev/null
$M tap testid=framesBtn >/dev/null
$M wait "text=A button on the page itself" >/dev/null || fail "the Frames page did not come up"
WEB=$($M contexts | awk '/WEBVIEW/ { print $1; exit }')
[ -n "$WEB" ] || fail "the Frames page published no WebView context"
outcome() { $M context "$WEB" >/dev/null; $M eval "document.getElementById('frameOutcome').textContent"; }

$M context "$WEB" >/dev/null
map=$($M map)
same=$(echo "$map" | awk '/ in iframe#sameFrame$/ { print $1; exit }')
nested=$(echo "$map" | awk '/ in iframe#sameFrame >> iframe#nestedFrame$/ { print $1; exit }')
[ -n "$same" ] && [ -n "$nested" ] || fail "the frames' buttons are not in the WebView's map, each with its frame"
cross=$(echo "$map" | awk '/ Tap me \(button\) in iframe#crossFrame$/ { print $1; exit }')
field=$(echo "$map" | awk '/ \(input\) in iframe#crossFrame$/ { print $1; exit }')
[ -n "$cross" ] && [ -n "$field" ] || fail "the cross-origin frame's button and field are not in the WebView's map"
echo "$map" | grep -q "could not be reached" && fail "the map says a frame could not be reached"
row "map" "every frame's elements, cross-origin included, each with its frame"

# The field before any tap: on iOS a tap followed by a focus raises the
# keyboard, and with it up every web action is refused — a defect of its
# own, in ROADMAP, not one of frames.
typed() { $M context "$WEB" >/dev/null; $M eval "document.getElementById('frameTyped').textContent"; }
$M fill "$field" "4242 4242" >/dev/null
[ "$(typed)" = "typed: 4242 4242" ] || fail "fill in the cross-origin field: the frame says $(typed)"
$M type "$field" " 99" >/dev/null
[ "$(typed)" = "typed: 4242 4242 99" ] || fail "type in the cross-origin field: the frame says $(typed)"
row "field" "the cross-origin field filled, then added to, as the frame says"

$M tap "$same" >/dev/null
outcome | grep -q "the same-origin frame" || fail "a tap on $same reached $(outcome)"
$M tap "$nested" >/dev/null
outcome | grep -q "the nested frame" || fail "a tap on $nested reached $(outcome)"
$M tap "$cross" >/dev/null
outcome | grep -q "the cross-origin frame" || fail "a tap on $cross reached $(outcome)"
row "taps" "each frame's button reached, as the page says"


$M context NATIVE_APP >/dev/null
$M tap "text=A button on the page itself" >/dev/null
CROSS=$($M map | awk '/ Tap me \(button\)$/ { r = $1 } END { print r }')
[ -n "$CROSS" ] || fail "NATIVE_APP's map has no button inside the cross-origin frame"
$M swipe up >/dev/null
CROSS=$($M map | awk '/ Tap me \(button\)$/ { r = $1 } END { print r }')
$M tap "$CROSS" >/dev/null
outcome | grep -q "the cross-origin frame" || fail "the native tap on the cross-origin button reached $(outcome)"
$M context NATIVE_APP >/dev/null
row "native" "the cross-origin frame's button, reached through NATIVE_APP"

$M terminate "$APP" >/dev/null 2>&1 || true
echo PASS
