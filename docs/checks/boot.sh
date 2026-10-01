#!/bin/sh
# `mobium boot` and `mobium shutdown`: a virtual device started and stopped
# through Mobium, held to what the device then is, not to what the command
# said.
#
#   docs/checks/boot.sh <avd> [simulator-udid]
#
# - The AVD, not running, boots, and the first call after it is answered: the
#   launcher is in front. Android says boot completed a second or two before
#   anything has focus, and a call made then failed reading a screen that was
#   still changing, so boot waits for a focused window too.
# - Booted again, it is said to be running already, with the same serial.
# - Shut down by its AVD's name from a call pinned to another device, the
#   emulator goes and the other device does not: the name is the argument, not
#   the device the call is pinned to, which a client's pipe sets.
# - Given a simulator, the same: shut down, booted by UDID, answered at once,
#   and booted again is running already.
# - A real phone, when one is connected, is refused: it is somebody's.
set -e
AVD="$1"; SIM="$2"
if [ -z "$AVD" ]; then echo "usage: $0 <avd> [simulator-udid]" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-60s ok\n' "$1" "$2"; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }
echo "--- $AVD${SIM:+, $SIM}"

$M shutdown "$AVD" >/dev/null 2>&1 || true
out=$($M --json boot "$AVD")
serial=$(echo "$out" | json 'd["device"]')
[ "$(echo "$out" | json 'd["already"]')" = False ] || fail "$AVD was already running before it was booted"
app=$($M --device "$serial" current 2>&1) || fail "the first call after boot failed: $app"
row "boot" "$AVD as $serial, $(echo "$out" | json 'd.get("took","")'); the first call answered"
again=$($M --json boot "$AVD")
[ "$(echo "$again" | json 'd["already"]')" = True ] && [ "$(echo "$again" | json 'd["device"]')" = "$serial" ] ||
  fail "booting it again: $again"
row "again" "said to be running already, as $serial"

if [ -n "$SIM" ]; then
  $M boot "$SIM" >/dev/null
  $M --device "$SIM" shutdown "$AVD" >/dev/null
  xcrun simctl list devices | grep "$SIM" | grep -q Booted || fail "shutting the emulator down from a call pinned to $SIM took $SIM down"
else
  $M shutdown "$AVD" >/dev/null
fi
adb devices | grep -q "^$serial" && fail "$serial is still listed by adb after shutdown"
row "shutdown" "$serial gone${SIM:+; $SIM, the device the call was pinned to, still up}"

if [ -n "$SIM" ]; then
  $M shutdown "$SIM" >/dev/null
  xcrun simctl list devices | grep "$SIM" | grep -q Booted && fail "$SIM is still booted after shutdown"
  out=$($M --json boot "$SIM")
  [ "$(echo "$out" | json 'd["already"]')" = False ] || fail "$SIM was still running"
  $M --device "$SIM" current >/dev/null 2>&1 || fail "the first call after booting $SIM failed"
  [ "$($M --json boot "$SIM" | json 'd["already"]')" = True ] || fail "booting $SIM again started it again"
  row "simulator" "shut down, booted by UDID, answered at once, running already"
fi

PHONE=$($M --json devices 2>/dev/null | python3 -c '
import json, sys
for d in json.load(sys.stdin).get("devices", []):
    if not d.get("emulator"):
        print(d["id"]); break' 2>/dev/null || true)
if [ -n "$PHONE" ]; then
  out=$($M shutdown "$PHONE" 2>&1) && fail "a phone was shut down: $out"
  echo "$out" | grep -q "somebody's" || fail "the refusal for a phone did not say why: $out"
  row "phone" "refused, as somebody's"
fi
echo PASS
