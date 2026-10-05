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
#   - the same-origin and nested frames' buttons are mapped, each saying
#     which frame it is in, and a tap on each reaches it;
#   - the cross-origin frame, closed to the page's scripts, is said to be
#     there rather than left out in silence;
#   - and what that says is so: in NATIVE_APP the platform's accessibility
#     reaches its button, and a tap there reaches the cross-origin frame.
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
echo "$map" | grep -q "1 frame on this page is cross-origin" || fail "the cross-origin frame was left out without a word"
row "map" "the same-origin and nested frames, and the closed one said"

$M tap "$same" >/dev/null
outcome | grep -q "the same-origin frame" || fail "a tap on $same reached $(outcome)"
$M tap "$nested" >/dev/null
outcome | grep -q "the nested frame" || fail "a tap on $nested reached $(outcome)"
row "taps" "each frame's button reached, as the page says"

$M context NATIVE_APP >/dev/null
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
