#!/bin/sh
# The time zone on iOS, read back from an app: Calendar's day view marks the
# hour in progress, in the zone it runs in.
#
#   docs/checks/timezone-ios.sh <simulator-udid | iphone-udid>
#
# iOS has no device time zone that can be set from outside, so Mobium keeps
# one for the session and gives it to every app it launches as TZ. The check
# holds that to what the app shows: Calendar marks the device's hour, then,
# set to Asia/Tokyo, Tokyo's hour — at once, since the app in front is
# launched again, and on a later launch — and, set back to the device's own
# zone, the device's hour again. An unknown zone is refused.
#
# On a phone Calendar shows its owner's events, so only the hour rows are
# matched, and only hours are printed; and Calendar opens in whatever view it
# was left in, so a phone not left in the day view is reported as not
# checked rather than switched. iOS writes 8:00 PM with a narrow
# no-break space before PM, which the comparison allows for.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-10s %-58s ok\n' "$1" "$2"; }
M="$ROOT/bin/mobium --device $DEV"
C=com.apple.mobilecal
# marked: the hour row Calendar marks in progress, as "8:00 PM", or nothing.
marked() {
  $M source 2>/dev/null | python3 -c '
import re, sys
m = re.search(r"label=\"(\d{1,2}:00)\W+([AP]M), In Progress\"", sys.stdin.read())
print(f"{m.group(1)} {m.group(2)}" if m else "")'
}
hour_in() { TZ=$1 date '+%-I:00 %p'; }
# shows <hour>: waits up to five seconds for Calendar to mark it, since a
# launch returns before the day view is drawn, and leaves what it marks in
# $seen either way.
shows() {
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    seen=$(marked)
    [ "$seen" = "$1" ] && return 0
    sleep 0.5
  done
  return 1
}
echo "--- $DEV"

device=$($M timezone)
at_exit '$M timezone "$device" >/dev/null 2>&1 || true'
$M terminate $C >/dev/null 2>&1 || true
$M launch $C >/dev/null
# Calendar opens in the view it was last left in. Only the day view has
# hour rows, and changing the view is the owner's, so without them the
# check says so rather than fails.
hour_rows() {
  $M source 2>/dev/null | python3 -c '
import re, sys
print(len(re.findall(r"label=\"\d{1,2}:00\W+[AP]M[\",]", sys.stdin.read())))'
}
if ! shows "$(hour_in "$device")" && [ "$(hour_rows)" = 0 ]; then
  echo "    NOT CHECKED  Calendar is not in its day view, which is what this reads; switch it there to run"
  $M terminate $C >/dev/null 2>&1 || true
  exit 0
fi
shows "$(hour_in "$device")" || fail "Calendar marks '$seen', not $device's hour $(hour_in "$device")"
row "device" "$device: Calendar marks $(hour_in "$device")"

$M timezone Asia/Tokyo >/dev/null
[ "$($M timezone)" = Asia/Tokyo ] || fail "the zone did not read back"
shows "$(hour_in Asia/Tokyo)" || fail "set to Tokyo, Calendar marks '$seen', not $(hour_in Asia/Tokyo)"
row "set" "Asia/Tokyo: relaunched, Calendar marks $(hour_in Asia/Tokyo)"

$M terminate $C >/dev/null
$M launch $C >/dev/null
shows "$(hour_in Asia/Tokyo)" || fail "a later launch marks '$seen', not Tokyo's hour"
row "launch" "a later launch is in Tokyo too"

out=$($M timezone Mars/Olympus 2>&1) && fail "an unknown zone was accepted: $out"
echo "$out" | grep -q "IANA" || fail "the refusal did not say why: $out"
row "refused" "Mars/Olympus, which the IANA database does not know"

$M timezone "$device" >/dev/null
shows "$(hour_in "$device")" || fail "set back, Calendar marks '$seen'"
row "reset" "$device again: Calendar marks $(hour_in "$device")"
$M terminate $C >/dev/null 2>&1 || true
echo "PASS"
