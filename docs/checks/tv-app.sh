#!/bin/sh
# MobiumTV — mobium-app's tv/ app — driven as a TV is: with the remote's
# D-pad, select and media keys, and judged by what the app says it received.
#
#   docs/checks/tv-app.sh <android-serial | apple-tv-simulator-udid>
#
# Any Android device that runs it: a Fire TV over `adb connect`, an Android or
# Google TV, or Google's Android TV emulator
# (system-images;android-34;android-tv;arm64-v8a) — or an Apple TV simulator,
# with mobium-app's tvos/ app, the same screens on tvOS. Needs dev.mobium.tv
# installed — mobium-app's tv/README.md and tvos/README.md say how. Nothing on
# the TV but the app is read or touched, so nothing of the owner's is printed.
#
#   - Focus Grid: a D-pad press says where focus went, and the app agrees;
#     select selects what has focus, and the app says by select; a tap
#     selects by touch.
#   - Focus or Select: one tile opens on focus, and a tap on it opens it by
#     focus — Mobium reports "tapped" and the app received no click, which
#     is why a tap on a TV has to be read back. The other opens by select,
#     or by touch when tapped.
#   - Row: nine presses right reach Card 10, scrolled into view by focus
#     alone, and select selects it.
#   - Dialog: app_alert sees it, accept presses the positive button
#     (Discard), and back cancels it.
#   - Player Keys: play-pause, fast-forward and stop each arrive, as the app
#     counts them.
#
# An Apple TV differs where the platform does, and the check asserts the
# difference rather than skipping it: there is no touch screen, so every tap
# is refused naming the remote; an alert opens with focus on its cancel
# button, Keep, and is answered with the D-pad and select — app_alert accept
# is refused — and back answers it with Keep, not as canceled; and the Siri
# Remote's only media key is Play/Pause, so fast-forward is refused.
#
# The first D-pad press after a touch only leaves touch mode and moves
# nothing, so each screen starts by pressing up until focus stops moving.
# Over a Fire TV's Wi-Fi link a read can land before the screen it expects —
# measured 2026-10-08, the grid's status read before the grid was up — so
# each screen is waited for by its own element, and each expected line with
# `mobium wait --for text`, never read once.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <android-serial>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
fail() { echo "FAIL: $*" >&2; exit 1; }
case "$DEV" in
  ????????-????????????????|*-*-*-*-*)
    devs=$("$ROOT/bin/mobium" devices 2>&1) || fail "cannot list devices: $(echo "$devs" | tail -1)"
    echo "$devs" | grep -q "^$DEV .*apple tv simulator" || {
      echo "$(basename "$0") is for a TV, and $DEV is not an Apple TV simulator" >&2; exit 2; }
    TVOS=1 ;;
  *) check_platform android "$DEV"; TVOS= ;;
esac
check_lock "$DEV"
row() { printf '    %-12s %-60s ok\n' "$1" "$2"; }
APP=dev.mobium.tv
M="$ROOT/bin/mobium --device $DEV"
echo "--- $DEV"

apps=$($M apps 2>&1) || fail "cannot list the apps: $(echo "$apps" | tail -1)"
echo "$apps" | grep -q "$APP" || fail "$APP is not installed — build and install mobium-app's tv/ app first"

# open <n> <id>: the menu's nth entry (0 is Focus Grid), reached with the
# remote, then waited for by an element only that screen has.
open() {
  $M terminate "$APP" >/dev/null 2>&1 || true
  $M launch "$APP" >/dev/null
  $M press dpad-up >/dev/null
  $M press dpad-up >/dev/null
  i=0
  while [ "$i" -lt "$1" ]; do $M press dpad-down >/dev/null; i=$((i + 1)); done
  $M press select >/dev/null
  $M wait "testid=$2" --timeout 20s >/dev/null || fail "menu entry $1 did not open the screen with $2"
}
expect() {
  $M wait "testid=$1" --for text --text "$2" --exact --timeout 15s >/dev/null && return 0
  got=$($M text "testid=$1" 2>&1) || fail "cannot read $1: $got"
  fail "$1 reads \"$got\", want \"$2\""
}
moved() { out=$($M press "$1"); echo "$out" | grep -q "focus moved to $2" || fail "press $1: $out, want focus moved to $2"; }
# refused <why> <command...>: an Apple TV refuses it, saying <why>.
refused() {
  why="$1"; shift
  if out=$("$@" 2>&1); then fail "$*: an Apple TV did it ($out), want it refused"; fi
  echo "$out" | grep -q "$why" || fail "$*: refused with \"$out\", want it to say $why"
}

open 0 grid
expect focusState "Focused: Tile 1"
moved dpad-right "Tile 2"
moved dpad-down "Tile 6"
expect focusState "Focused: Tile 6"
row "focus" "a D-pad press says where focus went, and the app agrees"
$M press select >/dev/null
expect selectState "Selected: Tile 6, by select"
if [ -n "$TVOS" ]; then
  refused "no touch screen" $M tap "text=Tile 3"
  expect selectState "Selected: Tile 6, by select"
  row "select" "select arrives as select; a tap is refused, naming the remote"
else
  $M tap "text=Tile 3" >/dev/null
  expect selectState "Selected: Tile 3, by touch"
  row "select" "select and a tap arrive as select and as touch"
fi

if [ -z "$TVOS" ]; then
  open 1 panel
  expect panel "Panel: closed"
  $M tap "text=Opens on focus" >/dev/null
  expect panel "Panel: opened by focus on Opens on focus"
  row "tap" "a tap on a focus-opening tile reached it as focus, not a click"
fi
open 1 panel
expect panel "Panel: closed"
moved dpad-right "Opens on focus"
expect panel "Panel: opened by focus on Opens on focus"
moved dpad-right "Opens on select"
$M press select >/dev/null
expect panel "Panel: opened by select on Opens on select"
if [ -n "$TVOS" ]; then
  row "open" "one tile opens on focus, the other by select"
else
  $M tap "text=Opens on select" >/dev/null
  expect panel "Panel: opened by touch on Opens on select"
  row "open" "one tile opens on focus, the other by select or touch"
fi

open 2 rowScroll
expect focusState "Focused: Card 1"
i=0
while [ "$i" -lt 9 ]; do $M press dpad-right >/dev/null; i=$((i + 1)); done
expect focusState "Focused: Card 10"
$M map | grep -q " Card 10 (button)" || fail "Card 10 has focus but map does not list it on the screen"
$M press select >/dev/null
expect selectState "Selected: Card 10, by select"
row "row" "focus scrolls Card 10 into view, and select selects it"

open 3 dialogButton
expect dialogOutcome "Dialog: not opened"
$M press select >/dev/null
$M wait "text=Discard" --timeout 15s >/dev/null || fail "select did not open the dialog"
$M alert | grep -q "Discard the draft?" || fail "app_alert does not see the dialog"
if [ -n "$TVOS" ]; then
  refused "answered with its remote" $M alert accept
  moved dpad-right "Discard"
  $M press select >/dev/null
  expect dialogOutcome "Dialog: Discard"
  $M press select >/dev/null
  $M wait "text=Keep" --timeout 15s >/dev/null || fail "select did not open the dialog again"
  $M press back >/dev/null
  expect dialogOutcome "Dialog: Keep"
  row "dialog" "app_alert sees it, accept is refused; Discard by remote, back is Keep"
else
  $M alert accept >/dev/null
  expect dialogOutcome "Dialog: Discard"
  $M tap "text=Open dialog" >/dev/null
  $M press back >/dev/null
  expect dialogOutcome "Dialog: canceled"
  row "dialog" "app_alert sees it; accept is Discard, back cancels"
fi

open 4 playerState
if [ -n "$TVOS" ]; then
  $M press play-pause >/dev/null
  expect playerState "Player: playing"
  expect lastKey "Last key: playPause"
  refused "has no \"fast-forward\" button" $M press fast-forward
  $M press play-pause >/dev/null
  expect playerState "Player: paused"
  expect keyCount "Keys: 2"
  row "keys" "play-pause arrives twice; fast-forward is refused, two keys counted"
  $M terminate "$APP" >/dev/null 2>&1 || true
  echo "PASS"
  exit 0
fi
$M press play-pause >/dev/null
expect playerState "Player: playing"
expect lastKey "Last key: KEYCODE_MEDIA_PLAY_PAUSE"
$M press fast-forward >/dev/null
expect playerState "Player: skipped forward"
$M press stop >/dev/null
expect playerState "Player: stopped"
expect keyCount "Keys: 3"
row "keys" "play-pause, fast-forward and stop arrive, three keys counted"

$M terminate "$APP" >/dev/null 2>&1 || true
echo "PASS"
