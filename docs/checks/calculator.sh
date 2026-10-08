#!/bin/sh
# 1 + 1 = 2 on Google Calculator, driven entirely through mobium's CLI.
#
# Both readings are by resource-id, and that matters. Reading "the first line
# of the screen" passed at every step of an earlier run — including *before*
# any digit was pressed — because the top line is the history strip still
# showing the previous run's answer. A test that passes before you do anything
# is worse than one that fails.
#
#   id/formula       the expression as it is typed
#   id/result_final  the answer, which only appears after equals
#
# The two are different elements and both contain "2" at the end, so the
# assertion names which one it means.
set -e
DEV="$1"; M="./bin/mobium --device $DEV"
case "$DEV" in
  ????????-????????????????|*-*-*-*-*) echo "$(basename "$0") is for an Android device, and $DEV is not one" >&2; exit 2 ;;
esac
if [ -z "$DEV" ]; then echo "usage: $0 <serial>" >&2; exit 2; fi
ID=com.google.android.calculator:id

# The formula id appears on three nodes — the container and its children all
# carry it — so the locator is narrowed by role. Errors are shown, not
# swallowed: an earlier version hid an ambiguity error behind an empty string
# and reported it as "the field is empty".
read_formula() { $M text "testid=$ID/formula,role=input" 2>&1; }
read_result()  { $M text "testid=$ID/result_final" 2>&1; }

step() { # step <label> <expected formula>
  $M tap "label=$1" >/dev/null
  got="$(read_formula)"
  printf "    tap %-8s formula=%-6s" "$1" "'$got'"
  if [ "$got" = "$2" ]; then echo "ok"; else echo "MISMATCH want '$2'"; exit 1; fi
}

echo "--- $DEV"
$M terminate com.google.android.calculator >/dev/null
$M launch com.google.android.calculator >/dev/null
$M wait "label=all clear" --timeout 15s >/dev/null
$M tap "label=all clear" >/dev/null

start="$(read_formula)"
printf "    after clear   formula='%s' " "$start"
# An empty field is not in the hierarchy at all, so "no element matches" is
# the expected answer here rather than an error.
# An empty formula field has no text, so mobium falls back to its
# accessibility label, which Calculator sets to "No formula".
case "$start" in
  ""|"No formula"|*"no element matches"*) echo "ok (empty)" ;;
  *) echo "NOT CLEAR"; exit 1 ;;
esac

step 1      "1"
step plus   "1+"
step 1      "1+1"

$M tap "label=equals" >/dev/null
answer="$(read_result)"
printf "    tap equals    result_final='%s' " "$answer"
if [ "$answer" = "2" ]; then echo "PASS"; else echo "FAIL want '2'"; exit 1; fi
