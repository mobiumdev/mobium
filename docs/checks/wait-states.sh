#!/bin/sh
# Waiting for a state, end to end: `wait --for` checked, unchecked, focused and
# value, on MobiumApp's Form and Login Demos, and the one shape every refusal
# from a check an action makes has — "X failed check C: reason", with C in the
# error's details.
#
#   docs/checks/wait-states.sh <android-serial | simulator-udid | iphone-udid>
#
# Each condition is shown to time out, saying what it saw, before it is shown
# to succeed: a wait that could not fail proves nothing. Checked covers a
# custom checkbox, a radio and a real Switch; focused covers focus arriving and
# moving to the next field — read from the tree on Android and, on iOS, where
# every field says focused="false", by asking WebDriverAgent which element is
# active; value is the whole value, where `text` matches a part, and an empty
# field showing its placeholder holds "". What cannot be answered is refused
# at once: a checked state on a text field, and a password's value, which is
# never read.
#
# Needs MobiumApp installed (mobiumdev/mobium-app). Dark mode is switched on
# and back off; nothing else outside the app changes.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-16s %-52s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp

case "$DEV" in
  *-*-*-*-* | ????????-????????????????) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *) PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($PLATFORM)"

open() {
  $M terminate $APP >/dev/null 2>&1 || true
  $M launch $APP >/dev/null
  $M scroll-to "label=$1" >/dev/null 2>&1 || true
  $M tap "label=$1" >/dev/null
}

# holds <what> <wait args...>: the wait succeeds.
holds() {
  what="$1"; shift
  r=$($M wait "$@" 2>&1) || fail "$what: $r"
}

# times_out <what> <said> <wait args...>: the wait times out, saying <said>.
times_out() {
  what="$1"; said="$2"; shift 2
  set +e; r=$($M wait "$@" --timeout 1s 2>&1); st=$?; set -e
  [ $st -eq 6 ] || fail "$what: expected a timeout (6), got $st: $r"
  echo "$r" | grep -q "$said" || fail "$what: the timeout did not say \"$said\": $r"
}

# refused <what> <said> <cmd args...>: refused at once as invalid_argument.
refused() {
  what="$1"; said="$2"; shift 2
  set +e; r=$($M "$@" 2>&1); st=$?; set -e
  [ $st -eq 2 ] || fail "$what: expected a refusal (2), got $st: $r"
  echo "$r" | grep -q "$said" || fail "$what: the refusal did not say \"$said\": $r"
}

# --- checked and unchecked --------------------------------------------------
open "Form Demo"
times_out "an unchecked box" "it is unchecked" testid=termsCheck --for checked
$M tap testid=termsCheck >/dev/null
holds "a box after a tap" testid=termsCheck --for checked --timeout 3s
row "checkbox" "timed out unchecked; checked after a tap"

times_out "an unchosen radio" "it is unchecked" testid=planPro --for checked
$M tap testid=planPro >/dev/null
holds "the chosen radio" testid=planPro --for checked --timeout 3s
holds "the radio it replaced" testid=planFree --for unchecked --timeout 3s
row "radio" "chosen one checked, the other unchecked"

times_out "a switch that is off" "it is unchecked" testid=darkSwitch --for checked
$M check testid=darkSwitch >/dev/null
holds "a switch turned on" testid=darkSwitch --for checked --timeout 3s
$M uncheck testid=darkSwitch >/dev/null
holds "the switch put back" testid=darkSwitch --for unchecked --timeout 3s
row "switch" "on and back off, each seen"

refused "a checked state on a text field" "no checked state" wait testid=readOnlyField --for checked --timeout 5s
row "no state" "a text field refused at once"

# --- the failed-check shape --------------------------------------------------
refused "typing into a checkbox" "failed check editable:" type testid=termsCheck hello
check=$($M --json type testid=termsCheck hello 2>/dev/null | python3 -c 'import sys,json; print(json.load(sys.stdin)["details"]["check"])') || true
[ "$check" = editable ] || fail "the refusal's details name the check as \"$check\""
row "shape" "failed check editable, and details.check says so"

# --- focused ----------------------------------------------------------------
open "Login Demo"
times_out "a field nobody tapped" "does not have keyboard focus" testid=username --for focused
$M tap testid=username >/dev/null
holds "the tapped field" testid=username --for focused --timeout 3s
times_out "the other field" "does not have keyboard focus" testid=password --for focused
$M tap testid=password >/dev/null
holds "focus moved on" testid=password --for focused --timeout 3s
times_out "the field it left" "does not have keyboard focus" testid=username --for focused
row "focused" "arrived on a tap, and moved to the next field"

# --- value ------------------------------------------------------------------
holds "an empty field, placeholder showing" testid=username --for value --text "" --timeout 3s
$M type testid=username mob >/dev/null
holds "what was typed" testid=username --for value --text mob --timeout 3s
times_out "part of the value" 'its value is "mob"' testid=username --for value --text mo
holds "part of the text" testid=username --for text --text mo --timeout 3s
row "value" "whole, not part; empty past the placeholder"

refused "a password's value" "never read" wait testid=password --for value --text x --timeout 5s
row "password" "refused, and nothing compared"

$M terminate $APP >/dev/null 2>&1 || true
echo PASS
