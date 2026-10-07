#!/bin/sh
# Auto-wait, end to end: an action waits until its target is ready — on
# screen, holding still, enabled, and the kind of thing the action is for —
# and refuses, saying why, what it cannot wait out. Judged by what MobiumApp
# says it received, never by the tool's own report.
#
#   docs/checks/autowait.sh <android-serial | simulator-udid | iphone-udid>
#
# Stable, on the Motion Demo: a tap on a target sliding in waits for the slide
# to end — the app's own stopwatch says how long after Replay the tap came —
# and with Reduce Motion on, the honoring target is tapped at once while the
# ignoring one still waits. Confetti: a still button is tappable during a
# burst, and a piece that never holds still is refused. Enabled, on the Login
# Demo: a second tap on Log In waits out "Signing in…" and lands. Editable, on
# the Form Demo: typing into a button, a checkbox and a read-only field is
# refused. Covered targets — a dialog, the keyboard — are dialogs.sh's.
#
# Needs MobiumApp installed (mobiumdev/mobium-app). Emulators and simulators
# switch Reduce Motion from outside; a phone does not allow that, so there the
# half its own setting selects is checked and the other said to be unchecked.
# Every setting changed is put back, on failure too.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-16s %-52s ok\n' "$1" "$2"; }
ms() { python3 -c 'import time; print(int(time.time()*1000))'; }
APP=dev.mobium.mobiumapp

case "$DEV" in
  ????????-????????????????) PLATFORM=ios; PHONE=1; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *-*-*-*-*) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *)         PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($PLATFORM)"

# Reduce Motion, on and off, from outside: Android's is the transition
# animation scale (what React Native reads — not the animator scale), and a
# simulator's is mobium's own accessibility setting. The original is put back
# on any exit.
ORIG_SCALE=""
reduce() { # reduce on|off
  # A phone's cannot be set from outside: WebDriverAgent answers the setting
  # and changes nothing there. The phone is checked as it stands instead.
  [ -n "$PHONE" ] && return 0
  if [ "$PLATFORM" = android ]; then
    [ -z "$ORIG_SCALE" ] && ORIG_SCALE=$(adb -s "$DEV" shell settings get global transition_animation_scale | tr -d '\r')
    if [ "$1" = on ]; then
      adb -s "$DEV" shell settings put global transition_animation_scale 0
    elif [ "$ORIG_SCALE" = null ]; then
      # Never set on this device: deleted again, not written as "null" —
      # on somebody's phone that would be a setting the check invented.
      adb -s "$DEV" shell settings delete global transition_animation_scale >/dev/null
    else
      adb -s "$DEV" shell settings put global transition_animation_scale "${ORIG_SCALE:-1.0}"
    fi
  else
    # Through mobium's own setting, not WebDriverAgent's port: each
    # simulator's runner has a port of its own now (CHALLENGES 148), and the
    # setting is put back when the session ends.
    $M accessibility reduce_motion "$1" >/dev/null
  fi
}
cleanup() { reduce off >/dev/null 2>&1 || true; $M terminate "$APP" >/dev/null 2>&1 || true; }
at_exit cleanup
# A session first, so the setting's undo belongs to it.
$M current >/dev/null

open() { # open <home entry>: from a cold start, so the screen starts fresh
  $M terminate "$APP" >/dev/null 2>&1 || true
  $M launch "$APP" >/dev/null
  $M scroll-to "label=$1" >/dev/null 2>&1 || true
  $M tap "label=$1" >/dev/null
}
after_replay() { # after_replay <panel>: ms the app says passed between Replay and the tap
  $M text "testid=${1}Result" | sed -n 's/.*tapped \([0-9]*\)ms after replay.*/\1/p'
}
slide() { # slide <panel>: replay, then tap the target at once
  $M tap "testid=${1}Replay" >/dev/null
  $M tap "testid=${1}Target" >/dev/null
  sleep 1
  after_replay "$1"
}

if [ -n "$PHONE" ]; then
# --- a phone: Reduce Motion as the phone has it -----------------------------
# Only one half can be checked, the one the phone's own setting selects, and
# the other is said to be unchecked rather than left out.
open "Motion Demo"
$M wait testid=honoringReplay >/dev/null
state=$($M text testid=reduceMotion)
h=$(slide honoring); i=$(slide ignoring)
[ -n "$h" ] && [ -n "$i" ] || fail "a tap on a sliding target did not reach the app (honoring '$h', ignoring '$i')"
[ "$i" -ge 1800 ] || fail "the ignoring target — the control — was tapped at ${i}ms, before its 2s slide ended"
if [ "$state" = "reduceMotion=true" ]; then
  # Not under 1800ms, as on a simulator: a phone takes about two seconds
  # between one tap and the next, so a target tapped at once still reads
  # ~2000ms (measured 1913-2031ms, the control 3246-3279ms). Judged against
  # the control instead: had it waited out a slide too, it would be as late.
  [ "$h" -lt $((i - 1000)) ] || fail "with Reduce Motion on, the honoring target was tapped at ${h}ms, not clearly before the ignoring one at ${i}ms"
  row "reduce motion" "on, as the phone has it: honoring ${h}ms, ignoring ${i}ms"
  printf '    %-16s %s\n' "stable" "NOT CHECKED — Reduce Motion is on, and a phone's cannot be switched from outside"
else
  [ "$h" -ge 1800 ] || fail "the honoring target was tapped ${h}ms after Replay, before its 2s slide ended"
  row "stable" "tapped after the slide: ${h}ms and ${i}ms after Replay"
  printf '    %-16s %s\n' "reduce motion" "NOT CHECKED — it is off, and a phone's cannot be switched from outside"
fi
else
# --- stable: a target sliding in -------------------------------------------
reduce off
open "Motion Demo"
$M wait testid=honoringReplay >/dev/null
[ "$($M text testid=reduceMotion)" = "reduceMotion=false" ] || fail "Reduce Motion is on before the check turned it on"
h=$(slide honoring); i=$(slide ignoring)
[ -n "$h" ] && [ -n "$i" ] || fail "a tap on a sliding target did not reach the app (honoring '$h', ignoring '$i')"
[ "$h" -ge 1800 ] || fail "the honoring target was tapped ${h}ms after Replay, before its 2s slide ended"
[ "$i" -ge 1800 ] || fail "the ignoring target was tapped ${i}ms after Replay, before its 2s slide ended"
row "stable" "tapped after the slide: ${h}ms and ${i}ms after Replay"

# --- stable, with Reduce Motion on -----------------------------------------
# The honoring target appears at once, so there is nothing to wait out; the
# ignoring one is the control, and must still wait for its slide.
reduce on
open "Motion Demo"
$M wait testid=honoringReplay >/dev/null
i=0; until [ "$($M text testid=reduceMotion)" = "reduceMotion=true" ]; do
  i=$((i + 1)); [ $i -lt 5 ] || fail "the app never saw Reduce Motion on"; sleep 1
done
h=$(slide honoring); i=$(slide ignoring)
[ "$h" -lt 1800 ] || fail "with Reduce Motion on, the honoring target still waited ${h}ms"
[ "$i" -ge 1800 ] || fail "with Reduce Motion on, the ignoring target — the control — was tapped at ${i}ms"
row "reduce motion" "honoring ${h}ms, at once; ignoring still ${i}ms"
reduce off

fi

if [ -n "$PHONE" ] && [ "$state" = "reduceMotion=true" ]; then
# MobiumApp honors Reduce Motion by drawing no confetti at all, so with it on
# there is no burst to tap beside and no piece to refuse — and a tap beside
# nothing passes whatever mobium does. Said, not passed.
printf '    %-16s %s\n' "confetti" "NOT CHECKED — with Reduce Motion on the app draws none"
printf '    %-16s %s\n' "never still" "NOT CHECKED — with Reduce Motion on the app draws none"
else
# --- a still button beside a burst of confetti ------------------------------
open "Motion Demo"
$M wait testid=celebrateBtn >/dev/null
$M tap testid=celebrateBtn >/dev/null
# The button is still, so it is tapped — not refused, and not waited on
# until the burst is over. How long it takes is reported, not judged: every
# read of a screen in motion is slower (about a second on an emulator,
# CHALLENGES 109), and a tap takes three.
t0=$(ms); $M tap testid=ignoringReplay >/dev/null || fail "a still button beside the burst was not tapped"; t1=$(ms)
row "confetti" "a still button beside it tapped, in $((t1 - t0))ms"
sleep 4

# --- a target that never holds still ----------------------------------------
# An exposed piece falls in 8.4 to 12 seconds, and finding one, resolving the
# tap and five seconds of settling take about nine on an emulator, where
# every read of a moving screen is slow (CHALLENGES 109) — so a piece can
# leave the screen before the verdict. That is a miss, not a pass: it is
# tried again with a new burst, up to three, and anything but "still moving"
# or a piece gone fails.
$M check testid=confettiExposed >/dev/null
tries=0
while :; do
  tries=$((tries + 1))
  $M tap testid=celebrateBtn >/dev/null
  # Pieces start above the top edge and ease in, so the first look can come
  # before any has arrived.
  piece=""; looks=0
  while [ -z "$piece" ] && [ $looks -lt 4 ]; do
    looks=$((looks + 1))
    piece=$($M find testid=confetti --json | python3 -c '
import json,sys
e=[x for x in json.load(sys.stdin)["elements"] if "confetti " in (x.get("label") or "") and x["bounds"]["y1"]>0]
e.sort(key=lambda x: x["bounds"]["y1"]); print(e[0]["label"].replace(" ","") if e else "")')
  done
  [ -n "$piece" ] || fail "no piece of exposed confetti was on screen to aim at, in $looks looks"
  out=$($M tap "testid=$piece" 2>&1) && fail "a piece that never holds still was tapped: $out"
  echo "$out" | grep -q "still moving" && break
  echo "$out" | grep -q "no element matches testid=$piece" || fail "the refusal did not say it was moving: $out"
  [ $tries -lt 3 ] || fail "three pieces in a row left the screen before the verdict: $out"
  sleep 10
done
row "never still" "a falling piece refused as still moving ($tries burst$( [ $tries = 1 ] || echo s))"
sleep 10
$M uncheck testid=confettiExposed >/dev/null

fi

# --- enabled: Log In while it signs in --------------------------------------
open "Login Demo"
$M type testid=username mobium >/dev/null
$M type testid=password wrongpass1 >/dev/null
$M tap testid=loginBtn >/dev/null
$M wait testid=loginBtn --for disabled --timeout 2s >/dev/null || fail "Log In was never disabled while signing in"
t0=$(ms); $M tap testid=loginBtn >/dev/null || fail "a tap on Log In did not wait for it to be enabled"; t1=$(ms)
# It landed: the button signs in again.
$M wait testid=loginBtn --for disabled --timeout 2s >/dev/null || fail "the second tap reported success and did not land"
row "enabled" "waited $((t1 - t0))ms for Log In, then signed in again"

# --- editable: typing into what is not a field ------------------------------
open "Form Demo"
$M wait testid=notifyCheck >/dev/null
out=$($M type testid=backBtn hello 2>&1) && fail "typing into a button was accepted: $out"
echo "$out" | grep -q "not a text field" || fail "the refusal for a button did not say why: $out"
out=$($M type testid=notifyCheck hello 2>&1) && fail "typing into a checkbox was accepted: $out"
echo "$out" | grep -q "not a text field" || fail "the refusal for a checkbox did not say why: $out"
out=$($M type testid=readOnlyField hello 2>&1) && fail "typing into a read-only field was accepted: $out"
ro=$(echo "$out" | sed -n 's/^error: //p' | head -1 | cut -c1-70)
row "editable" "a button and a checkbox refused as not text fields"
printf '    %-16s %s\n' "read-only" "refused: $ro"

echo PASS
