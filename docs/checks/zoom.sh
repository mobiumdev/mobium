#!/bin/sh
# Pinch-to-zoom, and the one thing that can confirm it.
#
# A pinch is the first two-finger gesture here. The W3C actions endpoint takes
# several pointer sources that run in lockstep, and both device-side servers
# implement it, so multi-touch needed no new protocol — only a second finger.
#
# The hard part is not the gesture, it is knowing whether it did anything.
# **Neither platform reports a zoom level in the accessibility hierarchy**, so
# there is nothing for mobium to read back the way `check` reads a checkbox,
# and `app_zoom` says so rather than implying it verified something. What can
# answer is the thing that was zoomed: a WebView knows its own
# `visualViewport.scale`, which is why this check pinches one.
#
# That makes this a narrow check on purpose. It proves the gesture reaches the
# platform and scales what is under it; it does not prove that any given native
# view will respond, because most of them expose nothing to ask.
#
#   docs/checks/zoom.sh <udid|serial>
#
# Needs MobiumApp installed — https://github.com/mobiumdev/mobium-app
set -e
DEV="${1:?usage: zoom.sh <udid|serial>}"
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

# A Remote Web Inspector connection belongs to one debugger at a time, so a
# previous run's session can leave this one with no attachable WebView.
"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true

echo "--- $DEV ($PLATFORM)"
$M apps 2>/dev/null | grep -q "$APP" || fail "$APP is not installed"

$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
sleep 2

ref() { $M map --json 2>/dev/null | python3 -c "
import json,sys
for e in json.load(sys.stdin).get('elements',[]):
    if (e.get('label') or '')=='$1': print(e['ref']); break
"; }

# The Pinch and Spread page under Gestures: a page that can be zoomed and
# shows its own scale, so a screenshot says what the number says.
$M tap "$(ref 'Gestures')" >/dev/null; sleep 1
$M tap "$(ref 'Pinch and Spread')" >/dev/null; sleep 3
CTX=$(appContexts "$APP" | head -1)
[ -n "$CTX" ] || fail "no WebView context to zoom: $($M contexts 2>&1 | grep -m1 "^error:" || echo "mobium listed none for this app")"

# scale reads the page's own idea of how far it is zoomed, which means
# switching context and back. The switching is the price of asking the only
# thing on the device that knows.
scale() {
  $M context "$CTX" >/dev/null
  v=$($M eval 'visualViewport.scale' 2>/dev/null | tail -1)
  $M context NATIVE_APP >/dev/null
  printf '%s' "$v"
}

BEFORE="$(scale)"
case "$BEFORE" in
  1|1.0*) ;;
  *) fail "the page did not start at scale 1 (got '$BEFORE'), so a zoom proves nothing" ;;
esac
echo "    start          the page is at scale $BEFORE                        ok"

$M zoom in >/dev/null
sleep 2
IN="$(scale)"
python3 -c "import sys; sys.exit(0 if float('$IN') > float('$BEFORE') * 1.5 else 1)" \
  || fail "zooming in left the scale at '$IN', up from '$BEFORE'"
echo "    zoom in        scale $BEFORE -> $IN"

# Out has to come back down, and it is the half that would pass by accident if
# the gesture were being delivered as a fling: a fling scrolls, and scrolling
# does not change the scale in either direction.
$M zoom out >/dev/null
sleep 2
OUT="$(scale)"
python3 -c "import sys; sys.exit(0 if float('$OUT') < float('$IN') * 0.7 else 1)" \
  || fail "zooming out left the scale at '$OUT', down from '$IN'"
echo "    zoom out       scale $IN -> $OUT"

# And the tool must not claim more than it checked. This is the assertion that
# keeps the honesty in the answer rather than only in the documentation.
$M zoom in 2>&1 | grep -qi 'all this can confirm' \
  || fail "app_zoom no longer says that it cannot confirm the zoom took effect"
echo "    honesty        the answer still says what it did not check      ok"

# --- a pinch of a particular size -------------------------------------------
# A direction pinches by a default amount. `from` and `to` say exactly how far
# the fingers travel, the way app_swipe takes either a direction or exact
# coordinates -- one tool, two levels of control, rather than a second tool for
# the same gesture.
#
# The assertion is comparative on purpose. Checking that a pinch with explicit
# gaps "zooms in" would pass just as well if the numbers were being ignored and
# the default used instead; only a small pinch producing visibly less zoom than
# a large one shows that they reach the gesture at all.
reset_scale() {
  $M zoom --from 400 --to 40 >/dev/null 2>&1 || true
  sleep 2
  $M zoom --from 400 --to 40 >/dev/null 2>&1 || true
  sleep 2
}

reset_scale
BASE="$(scale)"
$M zoom --from 40 --to 140 >/dev/null
sleep 2
SMALL="$(scale)"

reset_scale
$M zoom --from 40 --to 400 >/dev/null
sleep 2
LARGE="$(scale)"

python3 -c "import sys; sys.exit(0 if float('$SMALL') > float('$BASE') else 1)" \
  || fail "a small pinch did not zoom at all: $BASE -> $SMALL. Below about 100 device \
pixels of finger travel the platform ignores the gesture entirely -- measured on a \
Pixel 7 AVD, 50px left the scale at exactly 1.0"
python3 -c "import sys; sys.exit(0 if float('$LARGE') > float('$SMALL') * 1.4 else 1)" \
  || fail "a large pinch ($LARGE) was not meaningfully bigger than a small one ($SMALL), \
so from and to are probably being ignored"
echo "    pinch size     small $SMALL vs large $LARGE, from the same start"

# Refusing the ambiguous. A pinch whose fingers do not move is a two-finger
# tap, and giving both a direction and explicit gaps asks for two different
# gestures at once.
$M zoom --from 80 --to 80 2>&1 | grep -qi 'two-finger tap' \
  || fail "a pinch with no movement was accepted"
$M zoom in --from 40 --to 90 2>&1 | grep -qi 'either a direction or' \
  || fail "a direction and explicit gaps together were accepted"
$M zoom --from 40 2>&1 | grep -qi 'must be given together' \
  || fail "half a pinch was accepted"
echo "    pinch refuses  no movement, both forms, and half a pair          ok"

echo "PASS"
