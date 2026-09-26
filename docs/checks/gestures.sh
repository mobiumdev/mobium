#!/bin/sh
# Gestures that look alike from outside, and telling them apart.
#
# `long_press` has shipped since the beginning, is reachable from the CLI, the
# MCP surface and all five clients, and **nothing had ever proven it does
# anything.** Its only appearances in the test suite were fake drivers
# implementing the interface, which asserts that the method exists and not that
# a device receives a long press. That is a shipped claim with no evidence
# behind it, which this project treats as worse than a missing feature.
#
# It cannot be checked by watching the screen. A long press delivered as a tap
# highlights the same element and makes the app do something either way, so the
# only thing that can tell them apart is the app saying which one it got. The
# target here records that, and the check asserts the *difference* rather than
# that either one did something.
#
#   docs/checks/gestures.sh <udid|serial>
#
# Needs MobiumApp installed — https://github.com/mobiumdev/mobium-app
set -e
DEV="${1:?usage: gestures.sh <udid|serial>}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
case "$DEV" in
  [0-9A-Fa-f]*-[0-9A-Fa-f]*-[0-9A-Fa-f]*-[0-9A-Fa-f]*-[0-9A-Fa-f]*)
    BACKEND="--backend webdriveragent"; PLATFORM=ios ;;
  # A real iPhone's UDID is two groups, 00008120-0001234567890ABC. Without
  # this, a phone was taken for an Android serial and the check failed on
  # "not installed" (2026-09-23) -- mobium-app.sh had the fix, these did not.
  [0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f]-[0-9A-Fa-f]*)
    BACKEND="--backend webdriveragent"; PLATFORM=ios ;;
  *)
    BACKEND=""; PLATFORM=android ;;
esac
M="$ROOT/bin/mobium $BACKEND --device $DEV"
APP=dev.mobium.mobiumapp
fail() { echo "FAIL: $*" >&2; exit 1; }

# appContexts lists the WebView contexts that belong to one app — WEBVIEW_<id>,
# or WEBVIEW_<id>_<n> when it has several — and nothing else. `contexts`
# reports every inspectable page on the device, not only the app in front: on
# a real iPhone with Wikipedia in the foreground it listed Safari's page. So
# "the first WEBVIEW_ line" can be another app's, and a count of them can pass
# with one of ours and one of Safari's.
appContexts() { $M contexts | awk -v id="WEBVIEW_$1" \
  '$1 == id || (index($1, id "_") == 1 && substr($1, length(id) + 2) ~ /^[0-9]+$/) { print $1 }'; }

"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true

echo "--- $DEV ($PLATFORM)"
$M apps 2>/dev/null | grep -q "$APP" || fail "$APP is not installed"

ref() { $M map --json 2>/dev/null | python3 -c "
import json,sys
for e in json.load(sys.stdin).get('elements',[]):
    if (e.get('label') or '')=='$1': print(e['ref']); break
"; }
last() { $M text 2>/dev/null | grep -oE 'last: .*' | head -1; }

# open_gesture starts the app fresh and opens one witness from the Gestures
# screen. Fresh every time, so a section that fails cannot leave the next one
# on the wrong screen and blame it for what it finds there.
open_gesture() {
  $M terminate $APP >/dev/null 2>&1 || true
  $M launch $APP >/dev/null
  sleep 2
  r=$(ref 'Gestures'); [ -n "$r" ] || fail "no Gestures entry on the home screen"
  $M tap "$r" >/dev/null; sleep 1
  r=$(ref "$1"); [ -n "$r" ] || fail "no '$1' entry on the Gestures screen"
  $M tap "$r" >/dev/null; sleep 2
}

# The sections follow docs/GESTURES.md, which follows the touch-gesture
# charts: tap and press, double tap, drag, flick and pan, pinch and spread,
# rotate, and the gestures of more than one finger. Each asserts the gesture
# *and* a control that would fail if the tool were sending something else.

# --- tap and press ----------------------------------------------------------
open_gesture 'Tap and Press'
$M text | grep -q 'last: nothing yet' || fail "the gesture target did not start clean"

# A tap first, so the long press below has something to differ from. Asserting
# a long press in isolation proves nothing: if it were being delivered as a
# tap, the app would still report a gesture and the check would still pass.
$M tap 'testid=pressTarget' >/dev/null; sleep 1
TAP="$(last)"
[ "$TAP" = "last: tap" ] || fail "a tap was reported as '$TAP'"
echo "    tap            the app received a tap                          ok"

$M long-press 'testid=pressTarget' >/dev/null; sleep 1
HOLD="$(last)"
[ "$HOLD" = "last: long press" ] \
  || fail "a long press was reported as '$HOLD' — if it says 'tap' then the hold is \
not reaching the platform, which is what this check exists to catch"
echo "    long press     the app received a long press, not a tap        ok"

# And the hold has to be doing the work rather than the default happening to be
# long enough. A deliberately short hold must come back as a tap: if it does
# not, the duration is being ignored and the two gestures are the same call
# with different names.
$M long-press 'testid=pressTarget' --duration 50ms >/dev/null; sleep 1
SHORT="$(last)"
[ "$SHORT" = "last: tap" ] \
  || fail "a 50ms hold was reported as '$SHORT', so --duration is not reaching the gesture"
echo "    hold duration  50ms reads as a tap, 800ms does not              ok"

# --- double tap -------------------------------------------------------------
# What can be asserted differs by platform, and the difference is measured
# rather than assumed -- so the weaker branch says which assertion it is
# making instead of quietly making a smaller one.
count() { $M text 2>/dev/null | grep -oE 'gestures: [0-9]+' | head -1 | awk '{print $2}'; }

if [ "$PLATFORM" = android ]; then
  # The platform's own verdict. A page fires `dblclick` only when the browser
  # agreed two taps were one gesture, so this is not mobium marking its own
  # homework.
  open_gesture 'Double Tap'; sleep 1
  CTX=$(appContexts "$APP" | head -1)
  [ -n "$CTX" ] || fail "no WebView context for the double tap"
  $M context "$CTX" >/dev/null
  $M map >/dev/null
  D0=$($M eval 'tapCounts().doubles' | tail -1)
  $M double-tap @e1 >/dev/null; sleep 1
  D1=$($M eval 'tapCounts().doubles' | tail -1)
  [ "$D1" -gt "$D0" ] \
    || fail "the page counted $D0 -> $D1 doubles, so the browser did not read \
the two taps as one gesture"
  echo "    double tap     the page fired dblclick ($D0 -> $D1)                ok"

  # The control. Two ordinary taps must *not* produce one, or the assertion
  # above would pass on a tool that had no double tap at all.
  C0=$($M eval 'tapCounts().clicks' | tail -1)
  $M map >/dev/null; $M tap @e1 >/dev/null; sleep 1; $M tap @e1 >/dev/null; sleep 1
  C1=$($M eval 'tapCounts().clicks' | tail -1)
  D2=$($M eval 'tapCounts().doubles' | tail -1)
  [ "$C1" -gt "$C0" ] || fail "two separate taps reached the page as $C0 -> $C1 clicks"
  [ "$D2" -eq "$D1" ] \
    || fail "two taps a second apart also counted as a double tap, so the page \
cannot tell them apart and the assertion above is empty"
  echo "    vs two taps    2 more clicks, no new double                        ok"
  $M context NATIVE_APP >/dev/null
else
  # **iOS has no witness for this, measured rather than assumed.** A WKWebView
  # does not synthesize `dblclick` from XCUITest-injected touches -- not from
  # the W3C chain and not from WebDriverAgent's own doubleTap endpoint, which
  # is the platform's own primitive -- and a React Native Pressable coalesces
  # the two taps into a single onPress. So the outcome cannot be observed.
  #
  # What can be: the two gestures are *distinguishable*. A double tap arrives
  # as one press and two taps arrive as two, every time. That is indirect, and
  # it is asserted rather than skipped, because a silent skip cannot be told
  # from an untested one.
  open_gesture 'Tap and Press'
  B=$(count)
  $M double-tap 'label=Press target' >/dev/null; sleep 1
  A=$(count)
  [ "$((A - B))" -eq 1 ] \
    || fail "a double tap reached the app as $((A - B)) presses, not the 1 that \
XCUITest's own double tap produces"
  B2=$(count)
  $M tap 'label=Press target' >/dev/null; sleep 1
  $M tap 'label=Press target' >/dev/null; sleep 1
  A2=$(count)
  [ "$((A2 - B2))" -eq 2 ] \
    || fail "two separate taps reached the app as $((A2 - B2)) presses, so this \
check cannot tell them apart and the assertion above is empty"
  echo "    double tap     1 press where two taps give 2 (iOS has no dblclick) ok"
fi


# --- drag and drop ----------------------------------------------------------
# Measured natively, and the first attempt shows why. A WebView witness worked
# for holds up to 480ms and reported nothing at all from 520ms up -- a hard
# edge at Android's 500ms long-press timeout, past which the WebView claims
# the press and the page stops receiving touchmove. A drag holds *past* that
# timeout on purpose, so the only witness that can see a real one is native.
open_gesture 'Drag'
$M map >/dev/null

dragged() { $M text 2>/dev/null | grep -oE 'drag: .*' | head -1; }
field() { dragged | grep -oE "$1 -?[0-9]+" | awk '{print $2}'; }

$M drag 'label=Drag source' 'label=Drop zone' >/dev/null; sleep 1
[ -n "$(dragged)" ] || fail "the drop target saw no drag at all"
[ "$(field dropped= 2>/dev/null)" ] 2>/dev/null || true
echo "$(dragged)" | grep -q 'dropped yes' \
  || fail "the drag did not end over the drop zone: $(dragged)"
MOVES="$(field moves)"
[ "${MOVES:-0}" -ge 5 ] \
  || fail "only $MOVES moves arrived, so the travel is not stepped -- a drop \
target that highlights on hover would have nothing to highlight on"
HOLD_AFTER="$(field holdAfter)"
python3 -c "import sys; sys.exit(0 if 600 <= $HOLD_AFTER <= 900 else 1)" \
  || fail "the closing hold was ${HOLD_AFTER}ms, not the 700 asked for"
echo "    drag           held ${HOLD_AFTER}ms, $MOVES moves, dropped on target   ok"

# The control, and it is the assertion that means something. A swipe over the
# same two points lands in the same place with the same travel: only the holds
# differ, so a check that asserted "it moved and landed" would pass on a swipe
# and prove nothing about drag at all.
SX=$($M map --json 2>/dev/null | python3 -c "
import json,sys
els=json.load(sys.stdin).get('elements',[])
def center(name):
    for e in els:
        if (e.get('label') or '') == name:
            b = e['bounds']
            return (b['x1'] + b['x2']) // 2, (b['y1'] + b['y2']) // 2
    return None
a, b = center('Drag source'), center('Drop zone')
if a and b:
    print(a[0], a[1], b[0], b[1])
")
[ -n "$SX" ] || fail "could not read the two targets' centers for the swipe control"
$M swipe $SX --duration 800ms >/dev/null; sleep 1
SWIPE_AFTER="$(field holdAfter)"
python3 -c "import sys; sys.exit(0 if $SWIPE_AFTER < 200 else 1)" \
  || fail "a swipe held ${SWIPE_AFTER}ms at the end, so this check cannot tell \
a drag from a swipe and its drag assertion above means nothing"
echo "    vs swipe       same path, same drop, held ${SWIPE_AFTER}ms not ${HOLD_AFTER}ms  ok"

# And the hold has to be the caller's rather than a constant that happens to
# work, which is the same argument --duration settles for a long press.
$M map >/dev/null
$M drag 'label=Drag source' 'label=Drop zone' --hold-ms 1200 >/dev/null; sleep 1
LONG="$(field holdAfter)"
python3 -c "import sys; sys.exit(0 if 1100 <= $LONG <= 1400 else 1)" \
  || fail "--hold-ms 1200 produced a ${LONG}ms hold, so the flag is not reaching \
the gesture"
echo "    drag --hold-ms 1200 asked, ${LONG}ms delivered                 ok"

# --- flick and pan ----------------------------------------------------------
# One finger across a list, twice: fast, and slow. The only difference that
# matters is what the list does after the finger lifts -- a flick leaves it
# coasting, a pan does not -- and the list reports that through the platform's
# own momentum events, not a threshold of ours. Both go through app_swipe;
# until this section nothing had checked that its duration changes the answer.
open_gesture 'Flick and Pan'
flicked() { $M text 2>/dev/null | grep -oE 'scroll: .*' | head -1; }
# Found by its label on Android and as the screen's one list on iOS, where
# React Native puts the label and test ID on a view inside the scroll view and
# map sees only the scroll view itself.
LIST=$($M map --json 2>/dev/null | python3 -c "
import json,sys
els = json.load(sys.stdin).get('elements',[])
hit = [e for e in els if (e.get('label') or '') == 'Flick list'] or \\
      [e for e in els if e.get('role') == 'list']
if len(hit) == 1:
    b = hit[0]['bounds']; cx = (b['x1'] + b['x2']) // 2; h = b['y2'] - b['y1']
    print(cx, b['y1'] + h * 3 // 4, cx, b['y1'] + h // 4)
")
[ -n "$LIST" ] || fail "could not find the flick list"

coast() { flicked | grep -oE 'coasted [0-9]+' | awk '{print $2}'; }

# $LIST is split by the shell here on purpose: four coordinates.
# shellcheck disable=SC2086
$M swipe $LIST --duration 120ms >/dev/null; sleep 3
FLICK="$(flicked)"
[ "$(coast)" -gt 500 ] 2>/dev/null \
  || fail "a 120ms swipe was reported as '$FLICK' -- a fast swipe that does not \
coast is not a flick"
echo "    flick          ${FLICK#scroll: }                            ok"

# shellcheck disable=SC2086
$M swipe $LIST --duration 2500ms >/dev/null; sleep 3
PAN="$(flicked)"
[ "$(coast)" -lt 20 ] 2>/dev/null \
  || fail "a 2500ms swipe was reported as '$PAN' -- if it coasted, a slow swipe \
is still a flick and there is no way to pan"
echo "    pan            ${PAN#scroll: }                               ok"

# --- pinch and spread -------------------------------------------------------
# Checked in depth by zoom.sh, against the Pinch and Spread page; run it here
# so this file walks the whole chart, rather than repeating its assertions.
sh "$ROOT/docs/checks/zoom.sh" "$DEV" | grep -v -e '^---' -e '^PASS' \
  || fail "zoom.sh failed"

# --- rotate -----------------------------------------------------------------
# The hardest gesture here to confirm. Nothing in either accessibility
# hierarchy reports a rotation, and unlike a zoom there is no WebView property
# to ask -- `visualViewport.scale` has no rotational counterpart -- so the page
# computes the angle between two touches itself and accumulates the change.
#
# That makes this the only place a rotation can be observed at all, and it is
# why the demo is a WebView rather than a native view.
open_gesture 'Rotate'; sleep 1
CTX=$(appContexts "$APP" | head -1)
[ -n "$CTX" ] || fail "no WebView context to rotate in"

turned() {
  $M context "$CTX" >/dev/null
  v=$($M eval 'rotation()' 2>/dev/null | tail -1)
  $M context NATIVE_APP >/dev/null
  printf '%s' "$v"
}

[ "$(turned)" = "0" ] || fail "the page did not start at zero rotation"

$M rotate 90 >/dev/null
sleep 2
CW="$(turned)"
python3 -c "import sys; sys.exit(0 if float('$CW') > 45 else 1)" \
  || fail "a 90 degree clockwise turn registered as $CW"
echo "    rotate cw      the page turned $CW degrees"

# The other way, and this is the assertion that catches the likely failure
# rather than the obvious one. If the two chains were being delivered as
# anything other than a rotation -- a pinch, say, which a straight chord move
# would produce -- the accumulated angle would not reverse cleanly.
$M rotate --degrees -90 >/dev/null
sleep 2
BACK="$(turned)"
python3 -c "import sys; sys.exit(0 if abs(float('$BACK')) < abs(float('$CW')) * 0.4 else 1)" \
  || fail "turning back left the total at $BACK, down from $CW -- the reverse turn \
did not undo the first, so this is probably not a rotation"
echo "    rotate acw     turning back brought it to $BACK"

# Refusing the meaningless.
$M rotate 0 2>&1 | grep -qi 'two-finger press' \
  || fail "a zero-degree rotation was accepted"
$M rotate 720 2>&1 | grep -qi 'more than a full turn' \
  || fail "a rotation past a full turn was accepted"
echo "    rotate refuses zero and past a full turn, with reasons          ok"

# --- more than one finger ---------------------------------------------------
# Press and tap, press and drag, and several fingers at once: the gestures in
# which fingers do different things. The witness is native, because a WebView
# takes a held finger as its own long press and stops reporting (CHALLENGES
# 68), and it names the gesture from each finger's record once all are up.
open_gesture 'Multi-Touch'
multi() { $M text 2>/dev/null | grep -oE 'multi: .*' | head -1; }
num() { multi | grep -oE "$1 [0-9]+" | awk '{print $NF}'; }

API=""
[ "$PLATFORM" = android ] && API=$(adb -s "$DEV" shell getprop ro.build.version.sdk | tr -d '\r')
if [ "$PLATFORM" = android ] && [ "$API" -gt 35 ]; then
  # **Refused on Android 16 and later, and the refusal is what is asserted.**
  # UiAutomator2 gives a late finger its own down time; Android 17 rejects
  # the rest of the gesture, and the rejected lifts leave both fingers down
  # for every gesture after it (CHALLENGES 86). The refusal must name the
  # reason, and a plain tap afterwards must still work -- the damage this
  # refusal exists to prevent.
  set +e
  OUT=$($M press-tap "$(ref 'Hold zone')" "$(ref 'Act zone')" 2>&1); CODE=$?
  set -e
  [ "$CODE" -eq 5 ] && echo "$OUT" | grep -q 'down time went backwards' \
    || fail "press-tap on API $API was not refused as unsupported with its reason (exit $CODE): $OUT"
  set +e
  OUT=$($M press-drag "$(ref 'Hold zone')" "$(ref 'Act zone')" "$(ref 'Drag end')" 2>&1); CODE=$?
  set -e
  [ "$CODE" -eq 5 ] || fail "press-drag on API $API was not refused as unsupported (exit $CODE): $OUT"
  echo "    press-tap/drag refused on API $API, with UiAutomator2's reason   ok"

  $M tap "$(ref 'Finger tap zone')" --fingers 2 >/dev/null; sleep 1
  TWO="$(multi)"
  [ "$TWO" = "multi: 2-finger tap" ] || fail "a two-finger tap was read as '$TWO'"
  echo "    2-finger tap   both fingers together                            ok"
elif [ "$PLATFORM" = android ]; then
  $M press-tap "$(ref 'Hold zone')" "$(ref 'Act zone')" >/dev/null; sleep 1
  PT="$(multi)"
  echo "$PT" | grep -q 'press and tap' || fail "press-tap was read as '$PT'"
  LEAD="$(num lead)"
  python3 -c "import sys; sys.exit(0 if 200 <= $LEAD <= 450 else 1)" \
    || fail "the second finger landed ${LEAD}ms after the first, not the 300 asked for"
  # The lift, which is the difference between a tap and a finger left down.
  # It went missing once, while the landing looked right (see multitouch.go).
  $M text | grep -oE 'events: .*' | grep -q 'up 1' \
    || fail "the second finger never lifted on its own: $($M text | grep -oE 'events: .*')"
  echo "    press and tap  second finger ${LEAD}ms after the first, and lifted ok"

  # The control: the same two fingers landing together must *not* read as a
  # press and tap, or the screen cannot tell a held finger from a tap.
  $M tap "$(ref 'Finger tap zone')" --fingers 2 >/dev/null; sleep 1
  TWO="$(multi)"
  [ "$TWO" = "multi: 2-finger tap" ] \
    || fail "a two-finger tap was read as '$TWO', so press and tap above proves nothing"
  echo "    2-finger tap   read as a tap, not as press and tap              ok"

  $M press-tap "$(ref 'Hold zone')" "$(ref 'Act zone')" --lead-ms 800 >/dev/null; sleep 1
  LONG_LEAD="$(num lead)"
  python3 -c "import sys; sys.exit(0 if 700 <= $LONG_LEAD <= 950 else 1)" \
    || fail "--lead-ms 800 produced a ${LONG_LEAD}ms lead, so the flag is not reaching the gesture"
  echo "    --lead-ms 800  asked, ${LONG_LEAD}ms delivered                          ok"

  $M press-drag "$(ref 'Hold zone')" "$(ref 'Act zone')" "$(ref 'Drag end')" >/dev/null; sleep 1
  PD="$(multi)"
  echo "$PD" | grep -q 'press and drag' || fail "press-drag was read as '$PD'"
  SECOND="$(num 'second moved')"; FIRST="$(num 'first moved')"
  [ "${SECOND:-0}" -gt 50 ] || fail "the second finger moved only ${SECOND}pt"
  [ "${FIRST:-99}" -le 20 ] || fail "the held finger moved ${FIRST}pt -- it is meant to anchor"
  echo "    press and drag second finger moved ${SECOND}, held one ${FIRST}          ok"
else
  # **Refused on iOS, and the refusal is what is asserted.** XCTest adds a
  # zero-length touch at a late finger's target when the gesture starts, so a
  # press-tap would touch its target twice (multitouch.go). Asserting the
  # refusal means a WebDriverAgent that fixes this shows up here as a changed
  # answer rather than as a tool that quietly never tried.
  set +e
  OUT=$($M press-tap "$(ref 'Hold zone')" "$(ref 'Act zone')" 2>&1); CODE=$?
  set -e
  [ "$CODE" -eq 5 ] && echo "$OUT" | grep -q 'zero-length touch' \
    || fail "press-tap on iOS was not refused as unsupported with its reason (exit $CODE): $OUT"
  set +e
  OUT=$($M press-drag "$(ref 'Hold zone')" "$(ref 'Act zone')" "$(ref 'Drag end')" 2>&1); CODE=$?
  set -e
  [ "$CODE" -eq 5 ] || fail "press-drag on iOS was not refused as unsupported (exit $CODE): $OUT"
  echo "    press-tap/drag refused on iOS, with XCTest's reason (exit 5)     ok"

  $M tap "$(ref 'Finger tap zone')" --fingers 2 >/dev/null; sleep 1
  TWO="$(multi)"
  [ "$TWO" = "multi: 2-finger tap" ] || fail "a two-finger tap was read as '$TWO'"
  echo "    2-finger tap   both fingers together                            ok"
fi

$M tap "$(ref 'Finger tap zone')" --fingers 3 >/dev/null; sleep 1
THREE="$(multi)"
[ "$THREE" = "multi: 3-finger tap" ] || fail "a three-finger tap was read as '$THREE'"
$M tap "$(ref 'Finger tap zone')" >/dev/null; sleep 1
ONE="$(multi)"
echo "$ONE" | grep -q 'one finger' || fail "a plain tap was read as '$ONE'"
echo "    3-finger tap   3 fingers counted; a plain tap counts one        ok"

# --- what the dump backend refuses ------------------------------------------
# Every refusal names its own reason: they are different reasons, and a
# backend reported as merely unsupported would send the caller looking in the
# wrong place.
if [ "$PLATFORM" = android ]; then
  open_gesture 'Drag'
  "$ROOT/bin/mobium" --backend uiautomator --device "$DEV" \
    drag 'label=Drag source' 'label=Drop zone' 2>&1 | grep -qi 'hold' \
    || fail "the dump backend did not refuse a drag, or refused without saying \
that a swipe has no hold"
  "$ROOT/bin/mobium" --backend uiautomator --device "$DEV" \
    double-tap 'label=Drag source' 2>&1 | grep -qi 'interval' \
    || fail "the dump backend did not refuse a double tap, or refused without \
naming the interval"
  "$ROOT/bin/mobium" --backend uiautomator --device "$DEV" \
    press-tap 'label=Drag source' 'label=Drop zone' 2>&1 | grep -qi 'second finger' \
    || fail "the dump backend did not refuse press-tap, or refused without saying why"
  "$ROOT/bin/mobium" --backend uiautomator --device "$DEV" \
    tap 'label=Drag source' --fingers 2 2>&1 | grep -qi 'several fingers' \
    || fail "the dump backend did not refuse a two-finger tap, or refused without saying why"
  echo "    dump backend   refuses all four, each for its own reason        ok"
fi

echo "PASS"
