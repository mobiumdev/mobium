#!/bin/sh
# A tap made while Siri's panel is up, on a real iPhone.
#
#   docs/checks/siri.sh <iphone-udid>
#
# While Siri answers, WebDriverAgent reads SpringBoard, whose tree holds Siri's
# window and not the app under it, so a tap meant for the app found nothing:
# it answered "no element matches" after eight seconds and told the caller to
# map again, never naming Siri (CHALLENGES 349). Mobium now waits for the
# panel to go — about eight seconds after Siri answers — and then taps. This
# asks Siri the time, typed, over MobiumApp's Form Demo and checks:
#
#   - Siri's panel is up: MobiumApp is not in front;
#   - a tap on the Pro radio, sent a second and a half into the panel, lands,
#     read back from map once the app is back — and Free put back after.
#
# Without the fix the tap fails, "no element matches label=Pro"; with it, it
# waits about six seconds and lands.
#
# Only "What time is it" is asked: nothing with side effects, nothing read out
# of the phone's own data. Siri is asked through WebDriverAgent's
# /wda/siri/activate, which takes typed text, not speech. A simulator is
# refused: Siri there was never measured.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <iphone-udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_platform ios "$DEV"
case "$DEV" in
  ????????-????????????????) ;;
  *) echo "$(basename "$0") is for a real iPhone: Siri on a simulator has not been measured" >&2; exit 2 ;;
esac
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-10s %-58s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
export MOBIUM_SESSION="${MOBIUM_SESSION:-siricheck}"
M="$ROOT/bin/mobium --device $DEV"
echo "--- $DEV"

out=$($M launch "$APP" 2>&1) || fail "MobiumApp did not launch: $out"
tunnel=$(xcrun devicectl list devices --json-output /dev/stdout 2>/dev/null | python3 -c '
import json, sys
for d in json.load(sys.stdin)["result"]["devices"]:
    if d.get("hardwareProperties", {}).get("udid") == sys.argv[1]:
        print(d.get("connectionProperties", {}).get("tunnelIPAddress") or "")' "$DEV")
[ -n "$tunnel" ] || fail "no CoreDevice tunnel to $DEV"
WDA="http://[$tunnel]:8100"
session=$(curl -s -g -m 10 "$WDA/status" | python3 -c 'import json, sys; print(json.load(sys.stdin).get("sessionId") or "")')
[ -n "$session" ] || fail "WebDriverAgent on $DEV has no session"
siri() {
  curl -s -g -m 30 -H 'Content-Type: application/json' -X POST "$WDA/session/$session/wda/siri/activate" \
    -d "{\"text\":\"$1\"}" | python3 -c '
import json, sys
v = json.load(sys.stdin).get("value")
sys.exit(1 if isinstance(v, dict) and v.get("error") else 0)'
}

# The Form Demo, from wherever the app opened.
i=0; while ! $M map 2>&1 | grep -q 'Pro (radio'; do
  i=$((i + 1)); [ $i -le 4 ] || fail "MobiumApp's Form Demo could not be reached"
  if $M map 2>&1 | grep -q '^@e[0-9]* Back (button)'; then $M tap 'label=Back' >/dev/null
  else $M scroll-to 'label="Form Demo"' >/dev/null 2>&1 || true; $M tap 'label="Form Demo"' >/dev/null 2>&1 || true; fi
done
$M map 2>&1 | grep -q 'Free (radio, checked)' || $M tap 'label=Free' >/dev/null
at_exit "$M tap 'label=Free' >/dev/null 2>&1 || true"

# The tap is sent at once, a second and a half into the panel: one read in
# between, the light read of what is in front. With more, the tap started
# about five seconds in, and without the fix its own retries outlasted the
# panel and it landed by timing — the control passed.
siri "What time is it" || fail "Siri did not take the request"
sleep 1.5
[ "$($M current 2>&1 | tail -1)" != "$APP" ] || fail "MobiumApp is still in front: Siri's panel did not come up"
started=$(date +%s)
out=$($M tap 'label=Pro' 2>&1) || fail "the tap behind Siri's panel was refused: $out"
took=$(( $(date +%s) - started ))
row "siri" "answered, MobiumApp behind its panel"
$M map 2>&1 | grep -q 'Pro (radio, checked)' || fail "the tap did not select Pro: $($M map 2>&1 | grep 'Pro (radio')"
[ "$($M current 2>&1 | tail -1)" = "$APP" ] || fail "MobiumApp is not back in front"
row "tap" "waited ${took}s for the panel, then selected Pro"

echo "PASS"
