#!/bin/sh
# Set a Clock timer to 1m23s and read it back, through mobium's CLI.
#
# A companion to calculator.sh that exercises different things: tab
# navigation, a keypad whose keys carry *text* rather than an accessibility
# label, and a display that formats what you typed. Nothing is started, so no
# timer is left behind.
#
# It also has to cope with two versions of the same app, which is the point of
# running it on more than one device:
#
#   Clock 7.5 (emulator)  one display field, id/timer_setup_time = "00h 01m 23s"
#   Clock 9.1 (Pixel)     three fields, id/hour_text, id/minute_text, id/second_text
#                         and the keypad lives behind "Add timer" once any
#                         timers are saved
#
# The keypad ids are identical across both. Everything is addressed by
# resource-id: on this screen the digit keys have no content-desc, so
# `label=1` matches nothing, and `label=Timer` matches eleven things on a
# phone whose saved timers all have "Timer" in the name.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial>" >&2; exit 2; fi
# On the prelude since 2026-10-07: run outside it, this check left its
# daemon up, and the UiAutomator2 server under it held UiAutomation, so the
# next check that read the screen any other way failed (CHALLENGES 293).
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_platform android "$DEV"
check_lock "$DEV"
M="$ROOT/bin/mobium --device $DEV"
ID=com.google.android.deskclock:id

# show normalizes both layouts to HH:MM:SS.
#
# The one-field branch is chosen by matching a digit-h-digit-m shape, not by
# looking for "h" and "m" and "s" loosely. `*h*m*s*` also matches "No element
# matches testid=com.google...deskclock", so on a device without that field
# the error message was parsed as a duration and came out empty.
show() {
  # `|| true` because this read is a probe: on Clock 9.1 the field does not
  # exist and the command exits non-zero, which under `set -e` killed the
  # subshell and made show() return nothing at all. The message is kept rather
  # than discarded, so a real failure is still visible in the comparison.
  one="$($M text "testid=$ID/timer_setup_time" 2>&1 || true)"
  case "$one" in
    [0-9][0-9]h\ [0-9][0-9]m\ [0-9][0-9]s)
      echo "$one" | sed 's/[hms]//g' | awk '{printf "%s:%s:%s", $1, $2, $3}'; return ;;
  esac
  h="$($M text "testid=$ID/hour_text" 2>&1 || true)"
  m="$($M text "testid=$ID/minute_text" 2>&1 || true)"
  s="$($M text "testid=$ID/second_text" 2>&1 || true)"
  printf "%s:%s:%s" "$h" "$m" "$s"
}

echo "--- $DEV"
$M terminate com.google.android.deskclock >/dev/null
$M launch com.google.android.deskclock >/dev/null

# Addressed by id: 7.5 names the tab "Timer" and 9.1 names it "Timers".
$M wait "testid=$ID/tab_menu_timer" --timeout 15s >/dev/null
$M tap "testid=$ID/tab_menu_timer" >/dev/null

# With saved timers, the tab lists them and the keypad is behind "Add timer".
# `find` prints "No element matches ..." rather than nothing when it misses.
if $M find "testid=$ID/timer_setup_digit_1" 2>&1 | grep -q "No element matches"; then
  $M tap "label=Add timer" >/dev/null
fi
$M wait "testid=$ID/timer_setup_digit_1" --timeout 15s >/dev/null

i=0
while [ "$(show)" != "00:00:00" ] && [ $i -lt 8 ]; do
  $M tap "testid=$ID/timer_setup_delete" >/dev/null
  i=$((i + 1))
done
printf "    after clear   %s " "$(show)"
[ "$(show)" = "00:00:00" ] && echo "ok" || { echo "NOT CLEAR"; exit 1; }

# Digits fill from the right: 1, 2, 3 becomes one minute twenty-three seconds.
for d in 1 2 3; do
  $M tap "testid=$ID/timer_setup_digit_$d" >/dev/null
  echo "    tap $d         $(show)"
done

got="$(show)"
printf "    RESULT        %s " "$got"

# Leave without starting anything. Never touch a saved timer: it is somebody's.
$M terminate com.google.android.deskclock >/dev/null

if [ "$got" = "00:01:23" ]; then echo "PASS"; else echo "FAIL want '00:01:23'"; exit 1; fi
