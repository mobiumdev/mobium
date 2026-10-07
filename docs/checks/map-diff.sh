#!/bin/sh
# `mobium map --diff`, held to what MobiumApp's Form Demo does:
#
# - the first diff of a session has nothing to compare with, and says so,
#   with every element as added;
# - a map with nothing done between says nothing changed — the control that
#   the comparison can come back empty;
# - checking a box is one change, its checked state, and nothing else;
# - leaving the screen is the Form Demo's elements gone and the home
#   screen's added, and a ref from the diff acts.
#
#   docs/checks/map-diff.sh <emulator-serial | simulator-udid>
#
# Leaves the Form Demo's box as it found it. Emulators and simulators.
set -e
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
DEV="${1:?usage: map-diff.sh <serial|udid>}"
check_lock "$DEV"
case "$DEV" in
  *-*-*-*-*) M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *) M="$ROOT/bin/mobium --device $DEV" ;;
esac
APP=dev.mobium.mobiumapp
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-14s %-58s ok\n' "$1" "$2"; }

# A fresh daemon has no earlier map. Stopping one ends its session, which on
# iOS leaves the app, so each fresh start opens the Form Demo again.
fresh() {
  "$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true
  $M terminate $APP >/dev/null 2>&1 || true
  $M launch $APP >/dev/null
  $M tap "label=Form Demo" >/dev/null
  $M wait testid=termsCheck >/dev/null
}
echo "--- $DEV"
fresh
out=$($M map --diff)
echo "$out" | head -1 | grep -q '^no earlier map' || fail "the first diff did not say it had nothing to compare with: $out"
fresh
first=$($M --json map --diff)
echo "$first" | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d["diff"]["first"] is True, "first not set"
assert len(d["diff"]["added"]) == len(d["elements"]) > 0, "a first diff does not add the whole screen"
' || fail "the first diff's structured answer"
row "first" "nothing to compare with, and the whole screen added"

out=$($M map --diff)
echo "$out" | grep -q '^nothing changed since the last map' || fail "a map with nothing done said: $out"
row "unchanged" "nothing done, nothing changed"

was=$($M map | grep 'Accept terms' | grep -o 'unchecked\|checked' | head -1)
[ "$was" = checked ] && want=unchecked || want=checked
[ "$want" = checked ] && $M check testid=termsCheck >/dev/null || $M uncheck testid=termsCheck >/dev/null
out=$($M map --diff)
# The box's state changed. On iOS its row also grows, and what is under it
# moves down together, which is one more line and nothing else.
echo "$out" | head -1 | grep -q "^~ @e[0-9]* Accept terms (checkbox, $want) — was $was" || fail "the check read as: $out"
extra=$(echo "$out" | sed 1d | grep -v '^~ [0-9]* elements moved together, ' || true)
[ -z "$extra" ] || fail "checking one box changed more than it: $out"
[ "$was" = checked ] && $M check testid=termsCheck >/dev/null || $M uncheck testid=termsCheck >/dev/null
$M map >/dev/null
row "checked" "one box checked is one change: its state, and what it was"

$M tap "label=Back" >/dev/null 2>&1 || $M press back >/dev/null
$M wait "label=Form Demo" >/dev/null
out=$($M map --diff)
echo "$out" | grep -q '^- Accept terms (checkbox' || fail "leaving did not remove the Form Demo's box: $out"
ref=$(echo "$out" | grep '^+ @e[0-9]* Form Demo' | awk '{print $2}')
[ -n "$ref" ] || fail "leaving did not add the home screen's Form Demo button: $out"
$M tap "$ref" >/dev/null
$M wait testid=termsCheck >/dev/null || fail "the ref $ref from the diff did not open the Form Demo"
row "screen" "a screen left is gone, the one reached added, its ref acts"

echo PASS
