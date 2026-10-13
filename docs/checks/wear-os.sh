#!/bin/sh
# Wear OS: the Android backend on a watch, through Wear's own Settings.
#
# Wear OS is Android, and Mobium drives it as one — but a watch draws its
# controls its own way, and three of them were wrong on the first run
# (Wear OS 5.1 emulator, 2026-10-13):
#   - a chip is a Button whose own text repeats its title's, so
#     text="Gestures" matched two and was refused (CHALLENGES 315);
#   - a toggle chip holds its switch and the switch's container, each with
#     the chip's name and state, so map listed every switch three times
#     (316);
#   - Wear's own dialog, a ✓ and a ✕, was no dialog to app_alert (317).
#
# Each step here is by locator, not by ref, because the locator is what
# broke. Tilt-to-wake asks before it turns on — "Tilt-to-wake reduces
# battery life." — so turning it on is the dialog's positive control: read,
# dismissed (still off), then accepted (on). It is put back as found.
#
#   docs/checks/wear-os.sh <serial>     a Wear OS emulator or watch
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_platform android "$DEV"
case "$(adb -s "$DEV" shell getprop ro.build.characteristics 2>&1)" in
  *watch*) ;;
  *) echo "$(basename "$0") is for a Wear OS device, and $DEV is not one" >&2; exit 2 ;;
esac
check_lock "$DEV"
M="$ROOT/bin/mobium --device $DEV"
SETTINGS=com.google.android.apps.wearable.settings
SWITCH='text="Tilt-to-wake",role=switch'

fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf "  %-10s ok — %s\n" "$1" "$2"; }

# The switch's state from map, failing on map's own error rather than
# reading it as a state.
state() {
  out=$($M map 2>&1) || fail "map: $out"
  n=$(echo "$out" | grep -c "Tilt-to-wake (switch") || true
  [ "$n" = 1 ] || fail "map lists Tilt-to-wake $n times, want once:
$out"
  case "$out" in
    *"Tilt-to-wake (switch, checked)"*) echo on ;;
    *"Tilt-to-wake (switch, unchecked)"*) echo off ;;
    *) fail "no state for Tilt-to-wake in map: $out" ;;
  esac
}

echo "--- $DEV"
$M terminate $SETTINGS >/dev/null
$M launch $SETTINGS >/dev/null
$M wait 'text="Gestures"' --timeout 15s >/dev/null || fail "Settings did not show Gestures"
$M tap 'text="Gestures"' >/dev/null || fail 'tap text="Gestures" was refused'
$M wait 'text="Tilt-to-wake"' --timeout 10s >/dev/null || fail "Gestures did not open"
row "chip" 'text="Gestures" is one match, and tapping it opens Gestures'

was=$(state)
row "toggle" "map lists Tilt-to-wake once, as a switch, $was"
# Put back as found: off is an uncheck; on asks first, and is accepted.
restore() {
  $M alert dismiss >/dev/null 2>&1 || true
  if [ "$was" = off ]; then $M uncheck "$SWITCH" >/dev/null 2>&1 || true; return; fi
  $M check "$SWITCH" >/dev/null 2>&1 || $M alert accept >/dev/null 2>&1 || true
}
at_exit restore
if [ "$was" = on ]; then
  $M uncheck "$SWITCH" >/dev/null || fail "uncheck by locator was refused"
  [ "$(state)" = off ] || fail "uncheck left Tilt-to-wake on"
fi

$M tap "$SWITCH" >/dev/null
out=$($M alert 2>&1) || fail "alert: $out"
case "$out" in
  *"battery life"*"Cancel"*"OK"*) ;;
  *) fail "app_alert does not see Wear's dialog: $out" ;;
esac
$M alert dismiss >/dev/null || fail "dismiss was refused"
[ "$(state)" = off ] || fail "dismiss turned Tilt-to-wake on"
row "dialog" "app_alert reads Wear's dialog, Cancel and OK; dismiss leaves it off"

$M tap "$SWITCH" >/dev/null
out=$($M alert accept 2>&1) || fail "accept: $out"
case "$out" in
  "accepted the dialog"*'pressed "OK"') ;;
  *) fail "accept did not say it pressed OK: $out" ;;
esac
[ "$(state)" = on ] || fail "accept left Tilt-to-wake off"
row "accept" "accept presses OK, and it is on"

# Swiping right is a watch's back.
$M swipe right >/dev/null
$M wait 'text="Display"' --timeout 10s >/dev/null || fail "swiping right did not go back to Settings"
row "swipe" "swiping right goes back"

out=$($M press home 2>&1) || fail "home: $out"
row "home" "$out"
echo "PASS"
