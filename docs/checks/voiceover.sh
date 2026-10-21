#!/bin/sh
# MobiumApp driven with VoiceOver on, on a real iPhone.
#
#   docs/checks/voiceover.sh <iphone-udid>
#
# With VoiceOver on, a touch moves VoiceOver's cursor and activates nothing,
# so a tap answered "tapped" with nothing pressed (CHALLENGES 348). Mobium
# now touches, reads the cursor back against the target, and double-taps,
# as a VoiceOver user does. This turns VoiceOver on and checks, on
# MobiumApp:
#
#   - a tap opens the Form Demo, and selects the Pro radio and back to Free,
#     read back from map;
#   - check and uncheck reach the Email me checkbox;
#   - a swipe is refused, saying VoiceOver is on — it would move the cursor
#     and scroll nothing;
#   - typing reaches the Login Demo's username field;
#   - a two-button alert's Keep Editing is pressed and the alert goes;
#   - VoiceOver is off again at the end, read back.
#
# VoiceOver is switched through WebDriverAgent's own routes, which need a
# runner built by Xcode 27 and iOS 27 on the phone: Mobium has no setting for
# it yet. A simulator is refused: Mobium's simulator runner is WebDriverAgent's
# prebuilt release, which cannot reach VoiceOver. A phone that has VoiceOver
# on already is refused too, and left as it is: this check turns it off at
# the end. Each VoiceOver tap takes about seven seconds, against one with it
# off — XCTest's touch waits about five and a half with VoiceOver on — so
# the check takes about three minutes.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <iphone-udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_platform ios "$DEV"
case "$DEV" in
  ????????-????????????????) ;;
  *) echo "$(basename "$0") is for a real iPhone: a simulator's runner cannot reach VoiceOver" >&2; exit 2 ;;
esac
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-10s %-58s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
export MOBIUM_SESSION="${MOBIUM_SESSION:-vocheck}"
M="$ROOT/bin/mobium --device $DEV"
echo "--- $DEV"

out=$($M launch "$APP" 2>&1) || fail "MobiumApp did not launch: $out"
# The runner is reached where Mobium reaches it, over the CoreDevice tunnel.
tunnel=$(xcrun devicectl list devices --json-output /dev/stdout 2>/dev/null | python3 -c '
import json, sys
for d in json.load(sys.stdin)["result"]["devices"]:
    if d.get("hardwareProperties", {}).get("udid") == sys.argv[1]:
        print(d.get("connectionProperties", {}).get("tunnelIPAddress") or "")' "$DEV")
[ -n "$tunnel" ] || fail "no CoreDevice tunnel to $DEV"
WDA="http://[$tunnel]:8100"
vo() { curl -s -g -m 30 -H 'Content-Type: application/json' "$@"; }
enabled() { vo "$WDA/wda/voiceOver/enabled" | python3 -c '
import json, sys
v = json.load(sys.stdin).get("value") or {}
print("unsupported" if "enabled" not in v else str(v["enabled"]).lower())'; }

case "$(enabled)" in
  false) ;;
  true) echo "VoiceOver is on already on $DEV: left as it is" >&2; exit 2 ;;
  *) echo "the runner on $DEV cannot reach VoiceOver: it needs iOS 27 and a runner built by Xcode 27" >&2; exit 2 ;;
esac

off() {
  vo -X POST "$WDA/wda/voiceOver/disable" -d '{}' >/dev/null || true
  if [ "$(enabled)" != false ]; then echo "FAIL: VoiceOver is still on on $DEV" >&2; return 1; fi
  echo "    voiceover off again, read back"
}
at_exit off

# Home, at the top of its list, with VoiceOver off: the list cannot be
# scrolled with it on.
i=0; while $M map 2>&1 | grep -q '^@e[0-9]* Back (button)'; do
  i=$((i + 1)); [ $i -le 3 ] || fail "MobiumApp would not go back to its home screen"
  $M tap 'label=Back' >/dev/null
done
$M scroll-to 'label="Form Demo"' --direction up >/dev/null 2>&1 || true

vo -X POST "$WDA/wda/voiceOver/enable" -d '{}' >/dev/null
i=0; until [ "$(enabled)" = true ]; do
  i=$((i + 1)); [ $i -lt 10 ] || fail "VoiceOver did not come on"; sleep 1
done
row "voiceover" "on"

entry() { $M map 2>&1 | grep -F "$1" || true; }

out=$($M tap 'label="Form Demo"' 2>&1) || fail "tap Form Demo: $out"
entry "Email me (checkbox" | grep -q . || fail "the tap did not open the Form Demo: $($M map 2>&1 | head -3)"
row "tap" "Form Demo opened"

$M tap 'label=Pro' >/dev/null 2>&1 || fail "tap Pro"
entry "Pro (radio" | grep -q "checked)" || fail "Pro is not selected after a tap: $(entry 'Pro (radio')"
$M tap 'label=Free' >/dev/null 2>&1 || fail "tap Free"
entry "Free (radio" | grep -q "checked)" || fail "Free is not selected after a tap: $(entry 'Free (radio')"
row "tap" "Pro, then Free, selected"

out=$($M check 'label=Email me' 2>&1) || fail "check: $out"
echo "$out" | grep -q "is now checked" || fail "check answered: $out"
out=$($M uncheck 'label=Email me' 2>&1) || fail "uncheck: $out"
echo "$out" | grep -q "is now unchecked" || fail "uncheck answered: $out"
row "check" "Email me checked and unchecked"

out=$($M swipe up 2>&1) && fail "a swipe was sent with VoiceOver on: $out"
echo "$out" | grep -q "VoiceOver is on" || fail "the swipe was refused for another reason: $out"
row "swipe" "refused, naming VoiceOver"

$M tap 'label=Back' >/dev/null 2>&1 || fail "tap Back"
out=$($M tap 'label="Login Demo"' 2>&1) || fail "tap Login Demo: $out"
out=$($M type testid=username tomsmith 2>&1) || fail "type: $out"
entry "tomsmith (input)" | grep -q . || fail "the field does not hold what was typed: $(entry '(input)')"
$M keyboard --key enter >/dev/null 2>&1 || true
row "type" "username holds what was typed"

$M tap 'label=Back' >/dev/null 2>&1 || fail "tap Back"
# The Dialog Demo is below the fold, and nothing scrolls with VoiceOver on.
vo -X POST "$WDA/wda/voiceOver/disable" -d '{}' >/dev/null
$M scroll-to 'label="Dialog Demo"' --direction down >/dev/null 2>&1 || fail "the Dialog Demo is not reachable"
vo -X POST "$WDA/wda/voiceOver/enable" -d '{}' >/dev/null
i=0; until [ "$(enabled)" = true ]; do
  i=$((i + 1)); [ $i -lt 10 ] || fail "VoiceOver did not come back on"; sleep 1
done
$M tap 'label="Dialog Demo"' >/dev/null 2>&1 || fail "tap Dialog Demo"
$M tap 'label="Two-button alert"' >/dev/null 2>&1 || fail "tap Two-button alert"
$M alert 2>&1 | grep -q "Keep Editing" || fail "the tap raised no alert"
out=$($M alert --button "Keep Editing" 2>&1) || fail "alert button: $out"
$M alert 2>&1 | grep -q "no dialog" || fail "the alert is still up after Keep Editing"
row "alert" "Keep Editing pressed, the alert gone"

off || exit 1
at_exit_clear
$M tap 'label=Back' >/dev/null 2>&1 || true
echo "PASS"
