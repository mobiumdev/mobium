#!/bin/sh
# A list that grows as it is scrolled, on MobiumApp's Feed Demo: twenty rows
# a page, the next page a moment after the end is reached, with a spinner
# below the last row while it loads, and a true end after three pages.
#
#   docs/checks/feed.sh <simulator-udid | iphone-udid>
#   docs/checks/feed.sh <android-serial>
#
# The swipe that reaches the end of a page moves nothing, because the next
# rows are not there yet, and scroll-to took that for the end of the list:
# "the end of the list (3 scrolls)" for Row 55 while the second page was a
# second away (CHALLENGES 223). It now waits out a busy indicator inside the
# list. Ice Cubes' timeline never showed this, because its pages arrived
# before the next swipe, and iOS cannot slow a network from outside, so the
# delay is the app's: 2.5 seconds, its slow setting.
#
#   - Row 55, on the third page, is reached by scroll-to, and the app says
#     it loaded all three;
#   - Row 70, past the true end, is still "the end of the list", at once —
#     the negative control: a real end must still read as one;
#   - a role nothing has is refused, rather than matching nothing: the help's
#     own `wait role=progressbar --for hidden` was over at once before
#     progressbar was a role (CHALLENGES 222).
#
# Android is listed for when it is run; it has not been yet.
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

# A listing that fails is not an empty one.
APPS=$($M apps 2>&1) || fail "could not list the apps: $APPS"
echo "$APPS" | grep -q "$APP" || fail "$APP is not installed — build it first (see docs/checks/mobium-app.sh)"
# Relaunched, so the feed starts from its first page.
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M scroll-to "label=Feed Demo" >/dev/null 2>&1 || true
$M tap "label=Feed Demo" >/dev/null || fail "MobiumApp has no Feed Demo — rebuild it"
$M tap testid=feedSlow >/dev/null
$M text testid=feedState | grep -q "rows: 20, idle" || fail "the feed did not start on its first page: $($M text testid=feedState)"
row "feed" "one page of twenty, loading the next in 2.5s"

out=$($M scroll-to "label=Row 55" 2>&1) || fail "Row 55, two pages down, was not reached: $out"
$M text testid=feedState | grep -q "rows: 60" || fail "Row 55 was reached and the app did not load it: $($M text testid=feedState)"
row "load more" "Row 55 reached across two loads"

start=$(date +%s)
out=$($M scroll-to "label=Row 70" 2>&1) && fail "Row 70, past the end, was reported reached: $out"
echo "$out" | grep -q "end of the list" || fail "past the end did not say so: $out"
[ $(( $(date +%s) - start )) -lt 10 ] || fail "the real end took longer to report than a load is waited for"
row "true end" "Row 70 is past the end of the list, said at once"

out=$($M wait role=nosuchrole --for hidden --timeout 1s 2>&1) && fail "a role nothing has was waited on: $out"
echo "$out" | grep -q "unknown role" || fail "an unknown role was refused without saying why: $out"
row "role" "an unknown role refused, naming the roles there are"

$M terminate "$APP" >/dev/null 2>&1 || true
echo PASS
