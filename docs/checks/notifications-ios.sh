#!/bin/sh
# Notifications on an iOS simulator, read back from Notification Center.
#
#   docs/checks/notifications-ios.sh <simulator-udid | iphone-udid>
#
# iOS has no list of what is posted that can be asked from outside, so the
# list is read where a person would read it: a banner, or Notification
# Center, which is SpringBoard's and drawn over the app in front. The check
# posts as MobiumApp — simctl push, as the app in front, since iOS has no
# shell to post as — and holds Mobium to finding it, by app, title and body:
#
#   - post: confirmed by its banner, and MobiumApp still in front after;
#   - read: Notification Center opened for the read and closed again, so the
#     app in front is the one that was;
#   - shade: opened, map reads Notification Center and finds it there, and
#     closed, back to MobiumApp;
#   - the home screen in front: a post is refused, with what to do.
#
# A real iPhone's Notification Center is its owner's, so on a phone the
# check asserts only that it is refused, with the reason, and reads nothing.
# Needs MobiumApp installed and allowed to notify on the simulator.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-10s %-58s ok\n' "$1" "$2"; }
M="$ROOT/bin/mobium --device $DEV"
APP=dev.mobium.mobiumapp
echo "--- $DEV"

case "$DEV" in
  ????????-????????????????)
    out=$($M notifications 2>&1) && fail "a real iPhone's notifications were read: $out"
    echo "$out" | grep -q "owner's" || fail "the refusal did not say why: $out"
    row "refused" "a real iPhone, whose Notification Center is its owner's"
    echo "PASS"; exit 0 ;;
esac

BODY="Mobium check $(date +%s)"
$M notifications --shade close >/dev/null 2>&1 || true
$M launch $APP >/dev/null
out=$($M notifications --post "$BODY" 2>&1) || fail "the post was not confirmed: $out"
echo "$out" | grep -q "^$APP .*$BODY" || fail "the post's answer did not list it: $out"
[ "$($M current | head -1)" = "$APP" ] || fail "MobiumApp is not in front after the post"
row "post" "confirmed by its banner, MobiumApp still in front"

$M notifications | grep -q "^$APP .*$BODY" || fail "the read did not find it"
[ "$($M current | head -1)" = "$APP" ] || fail "the read left Notification Center open"
row "read" "found by app and body, Notification Center closed again"

$M notifications --shade open >/dev/null
[ "$($M current | head -1)" = com.apple.springboard ] || fail "with the shade open, reads are not of Notification Center"
$M map | grep -q "$BODY" || fail "map does not show the notification with the shade open"
$M notifications --shade close >/dev/null
[ "$($M current | head -1)" = "$APP" ] || fail "closing the shade did not bring MobiumApp back"
row "shade" "opened, map finds it there, closed back to MobiumApp"

$M press home >/dev/null 2>&1 || true
out=$($M notifications --post "from nowhere" 2>&1) && fail "a post with the home screen in front was accepted: $out"
echo "$out" | grep -q "launch the app" || fail "the refusal did not say what to do: $out"
row "refused" "a post with the home screen in front, and what to do"
echo "PASS"
