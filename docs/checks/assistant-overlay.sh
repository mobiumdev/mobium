#!/bin/sh
# A tap behind Gemini's or Google Assistant's overlay, on Android.
#
#   docs/checks/assistant-overlay.sh <android-serial>
#
# While the assistant's overlay is up, UiAutomator2 reads its window alone,
# and a tap meant for the app found nothing: it answered "run app_map again"
# after five and a half seconds, never naming the assistant, which stays until
# it is closed (CHALLENGES 350). The assist key opens it — Gemini on a Pixel
# signed in, Google Assistant on an emulator signed out — and this checks, on
# MobiumApp's Form Demo:
#
#   - a tap on the Pro radio behind the overlay is refused at once, naming
#     the assistant and back, and taps nothing;
#   - back closes the overlay, MobiumApp is in front again, and the same tap
#     lands — Free put back after.
#
# Nothing is asked of the assistant, so nothing reaches the account's
# activity; the overlay is opened and closed. Without the fix the refusal is
# "no element matches label=Pro", which this fails on.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <android-serial>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_platform android "$DEV"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-10s %-58s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
ASSISTANT=com.google.android.googlequicksearchbox
export MOBIUM_SESSION="${MOBIUM_SESSION:-assistcheck}"
M="$ROOT/bin/mobium --device $DEV"
a() { adb -s "$DEV" shell "$@" | tr -d '\r'; }
echo "--- $DEV"

a pm list packages "$ASSISTANT" | grep -q "$ASSISTANT" || { echo "$DEV has no Google app to be its assistant" >&2; exit 2; }
out=$($M launch "$APP" 2>&1) || fail "MobiumApp did not launch: $out"

# The Form Demo, from wherever the app opened.
i=0; while ! $M map 2>&1 | grep -q 'Pro (radio'; do
  i=$((i + 1)); [ $i -le 4 ] || fail "MobiumApp's Form Demo could not be reached"
  if $M map 2>&1 | grep -q '^@e[0-9]* Back (button)'; then $M tap 'label=Back' >/dev/null
  else $M scroll-to 'label="Form Demo"' --direction down >/dev/null 2>&1 || true; $M tap 'label="Form Demo"' >/dev/null 2>&1 || true; fi
done
$M map 2>&1 | grep -q 'Free (radio, checked)' || $M tap 'label=Free' >/dev/null
closed() { [ "$($M current 2>&1 | tail -1)" = "$APP" ]; }
at_exit "closed || $M press back >/dev/null 2>&1; $M tap 'label=Free' >/dev/null 2>&1 || true"

a input keyevent KEYCODE_ASSIST
i=0; until [ "$($M current 2>&1 | tail -1)" = "$ASSISTANT" ]; do
  i=$((i + 1)); [ $i -lt 10 ] || fail "the assist key brought no assistant forward"; sleep 1
done
row "assistant" "open over MobiumApp"

started=$(date +%s)
out=$($M tap 'label=Pro' 2>&1) && fail "a tap was sent with the assistant's overlay in front: $out"
took=$(( $(date +%s) - started ))
echo "$out" | grep -q "is open over the app" || fail "the refusal did not name the assistant: $out"
echo "$out" | grep -q "app_press back" || fail "the refusal did not name back: $out"
$M map 2>&1 | grep -q 'Pro (radio' && fail "the overlay is gone, so the refusal is not the case being checked"
row "refused" "in ${took}s, naming the assistant and back"

$M press back >/dev/null
i=0; until closed; do
  i=$((i + 1)); [ $i -lt 6 ] || fail "back did not close the assistant's overlay"; sleep 1
done
$M tap 'label=Pro' >/dev/null || fail "the tap after back was refused"
$M map 2>&1 | grep -q 'Pro (radio, checked)' || fail "the tap after back did not select Pro"
row "back" "closed the overlay, and the tap landed"

echo "PASS"
