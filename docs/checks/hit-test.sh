#!/bin/sh
# The hit test below accessibility, held to what a touch really reached.
#
# For every case on MobiumApp's Obstruction Demo, `mobium hit-test` says
# whether a touch at the point `tap` would use reaches the target; then that
# same point is touched by its coordinates — no check, no aim — and the
# app's outcome line says what it reached. The two must agree: "reaches"
# only where the target got the touch, and a named receiver where something
# else did. The overlay hidden from accessibility is the case this exists
# for (CHALLENGES 115); the pass-through view is the
# negative control, reached although something is drawn over it.
#
#   docs/checks/hit-test.sh <simulator-udid | iphone-udid>
#   docs/checks/hit-test.sh <emulator-serial>   # the refusal
#
# Then, on a simulator, the same cases with the probe loaded at launch
# (`launch --hit-test`): a plain `tap` asks it first, so it must tap where
# the touch reaches the target and refuse, touching nothing, where it does
# not — and `hit-test` answers from the loaded probe, without lldb. A phone
# refuses `--hit-test`, saying why.
#
# Needs MobiumApp installed — on an iPhone, built for development, as it is
# from Xcode. The app is stopped while lldb is attached, about two seconds
# a case on a simulator and nine on a phone; nothing on either is changed.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-13s %-60s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp

case "$DEV" in
  ????????-????????????????) KIND=phone; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *-*-*-*-*) KIND=simulator; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *) KIND=android; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($KIND)"

$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
at_exit '$M terminate "$APP" >/dev/null 2>&1 || true'
$M scroll-to "testid=obstructionBtn" --direction down >/dev/null 2>&1 || true
$M tap "testid=obstructionBtn" >/dev/null
$M wait "testid=obstructionOutcome" >/dev/null

if [ "$KIND" = android ]; then
  set +e; out=$($M hit-test testid=hiddenTarget 2>&1); status=$?; set -e
  [ "$status" = 5 ] || fail "not refused as unsupported (exit $status): $out"
  echo "$out" | grep -q "needs no hit test" || fail "the refusal does not say why: $out"
  row "refused" "unsupported, saying why"
  echo PASS; exit 0
fi

outcome() {
  $M scroll-to "testid=obstructionOutcome" --direction up >/dev/null 2>&1 || true
  $M text "testid=obstructionOutcome" | tr -d '\n'
}

# case <target> <what the touch reaches>: the hit test's verdict, then the
# touch at its point, then the app's word for what that touch reached.
case_() {
  id="$1"; want="$2"
  $M scroll-to "testid=$id" --direction down >/dev/null 2>&1 || true
  set +e; said=$($M hit-test "testid=$id" 2>&1); status=$?; set -e
  point=$(echo "$said" | sed -n 's/.*at (\([0-9]*\), \([0-9]*\)).*/\1 \2/p' | head -1)
  [ -n "$point" ] || fail "$id: the hit test named no point: $said"
  before=$(outcome)
  $M scroll-to "testid=$id" --direction down >/dev/null 2>&1 || true
  # The point is re-derived after scrolling back, since the list moved.
  set +e; again=$($M hit-test "testid=$id" 2>&1); set -e
  point=$(echo "$again" | sed -n 's/.*at (\([0-9]*\), \([0-9]*\)).*/\1 \2/p' | head -1)
  # shellcheck disable=SC2086
  $M tap $point >/dev/null
  sleep 1
  after=$(outcome)
  reached="nothing"
  [ "$after" = "$before" ] || reached=$(echo "$after" | sed 's/^[0-9]*: //')
  [ "$reached" = "$want" ] || fail "$id: the touch reached \"$reached\", where the case says \"$want\""
  if [ "$reached" = "target $id" ]; then
    [ "$status" = 0 ] || fail "$id: the touch reached the target, and the hit test said it would not: $said"
    row "$id" "reaches, and the touch did"
  else
    [ "$status" = 4 ] || fail "$id: the touch reached \"$reached\", and the hit test said it would reach the target"
    echo "$said" | grep -q "failed check receivesEvents" || fail "$id: not refused in a failed check's shape: $said"
    row "$id" "refused, and the touch reached $reached"
  fi
}

case_ fullTarget "cover fullCover"
case_ halfTarget "target halfTarget"
case_ edgeTarget "target edgeTarget"
case_ passTarget "target passTarget"
case_ plainTarget "nothing"
case_ hiddenTarget "cover hidden overlay"
set +e; said=$($M hit-test testid=hiddenTarget 2>&1); set -e
echo "$said" | grep -q "hidden from accessibility" || fail "the hidden overlay was not named as hidden: $said"
row "hidden" "named as hidden from accessibility"
case_ scrimTarget "cover scrimCover"

if [ "$KIND" = phone ]; then
  set +e; out=$($M launch --hit-test "$APP" 2>&1); status=$?; set -e
  [ "$status" = 5 ] || fail "launch --hit-test was not refused as unsupported on a phone (exit $status): $out"
  echo "$out" | grep -q "network" || fail "the refusal does not say why: $out"
  row "at launch" "refused on a phone, saying why"
  echo PASS; exit 0
fi

ms() { python3 -c 'import time; print(int(time.time()*1000))'; }
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch --hit-test "$APP" | grep -q "hit probe" || fail "launch --hit-test did not say the probe is loaded"
$M scroll-to "testid=obstructionBtn" --direction down >/dev/null 2>&1 || true
$M tap "testid=obstructionBtn" >/dev/null
$M wait "testid=obstructionOutcome" >/dev/null
t0=$(ms); $M hit-test testid=passTarget >/dev/null; t1=$(ms)
[ $((t1 - t0)) -lt 1500 ] || fail "hit-test took $((t1 - t0)) ms with the probe loaded — it went through lldb"
row "loaded" "hit-test answers in $((t1 - t0)) ms, without lldb"

# tapcase <target> <what the touch reaches>: a plain tap, which the probe
# lets through only to the target.
tapcase() {
  id="$1"; want="$2"
  $M scroll-to "testid=$id" --direction down >/dev/null 2>&1 || true
  before=$(outcome)
  $M scroll-to "testid=$id" --direction down >/dev/null 2>&1 || true
  set +e; said=$($M tap "testid=$id" 2>&1); status=$?; set -e
  sleep 1
  after=$(outcome)
  if [ "$want" = "target $id" ]; then
    [ "$status" = 0 ] || fail "$id: tap refused where the touch reaches the target: $said"
    [ "$after" != "$before" ] && [ "$(echo "$after" | sed 's/^[0-9]*: //')" = "$want" ] ||
      fail "$id: tapped, and the app says \"$after\""
    row "$id" "tapped, and the target got it"
  else
    [ "$status" = 4 ] || fail "$id: tapped where the touch reaches \"$want\": $said"
    [ "$after" = "$before" ] || fail "$id: refused, and still touched — the app says \"$after\""
    row "$id" "refused, nothing touched ($want)"
  fi
}
tapcase fullTarget "cover fullCover"
tapcase halfTarget "target halfTarget"
tapcase edgeTarget "target edgeTarget"
tapcase passTarget "target passTarget"
tapcase plainTarget "nothing"
tapcase hiddenTarget "cover hidden overlay"
set +e; said=$($M tap testid=hiddenTarget 2>&1); set -e
echo "$said" | grep -q "hidden from accessibility" || fail "the tap's refusal did not name the overlay as hidden: $said"
tapcase scrimTarget "cover scrimCover"
echo PASS
