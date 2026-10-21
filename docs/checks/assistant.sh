#!/bin/sh
# The platform's assistant, asked in typed text: `mobium assistant`.
#
#   docs/checks/assistant.sh <android-serial | iphone-udid | simulator-udid>
#
# Gemini or Google Assistant on Android, opened with the assist key; Siri on
# a real iPhone. It checks:
#
#   - a request that opens an app reaches it: "Open Calculator" on Android,
#     "Open MobiumApp" on an iPhone, with --app naming it, answers reached;
#   - the same request naming another app answers not reached — the app in
#     front once the panel closes is never taken for the assistant's doing;
#   - a request that acts for the owner ("Text …") is refused without --app,
#     before anything is asked;
#   - a simulator is refused: its Siri answers nothing (CHALLENGES 351).
#
# Each request goes to the account the assistant is signed in to, and Gemini
# keeps it in that account's activity: a phone only with ALLOW_PHONE=1. It
# never asks anything that reads the owner's data.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-10s %-60s ok\n' "$1" "$2"; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }
export MOBIUM_SESSION="${MOBIUM_SESSION:-asstcheck}"
M="$ROOT/bin/mobium --device $DEV"
case "$DEV" in
  ????????-????????????????) KIND=phone ;;
  *-*-*-*-*) KIND=simulator ;;
  emulator-*) KIND=emulator ;;
  *) KIND=android-phone ;;
esac
echo "--- $DEV ($KIND)"

if [ "$KIND" = simulator ]; then
  out=$($M assistant "What time is it" 2>&1) && fail "a simulator's Siri was asked: $out"
  echo "$out" | grep -q "answers nothing" || fail "the refusal did not say why: $out"
  row "refused" "a simulator, whose Siri answers nothing"
  echo PASS; exit 0
fi
case "$KIND" in
  phone | android-phone) [ -n "$ALLOW_PHONE" ] || { echo "$DEV is a phone: each request goes to its owner's assistant account. ALLOW_PHONE=1 to go ahead" >&2; exit 2; } ;;
esac

out=$($M assistant "Text Mom that I am late" 2>&1) && fail "a request to text was sent without --app: $out"
echo "$out" | grep -q "on the device owner's behalf" || fail "the refusal did not say why: $out"
row "refused" "a request to text, with no app named"

if [ "$KIND" = phone ]; then
  REQ="Open MobiumApp"; APP=dev.mobium.mobiumapp; START=com.apple.Preferences
else
  REQ="Open Calculator"; APP=com.google.android.calculator; START=dev.mobium.mobiumapp
fi
$M terminate $APP >/dev/null 2>&1 || true
$M launch $START >/dev/null
at_exit "$M terminate $APP >/dev/null 2>&1 || true"
got=$($M assistant "$REQ" --app $APP --json)
[ "$(echo "$got" | json 'd["typed"]')" = True ] || fail "the answer does not say the request was typed"
[ "$(echo "$got" | json 'd["reached"]')" = True ] || fail "\"$REQ\" did not reach $APP: $(echo "$got" | json 'd["front"]') in front"
[ "$($M current)" = "$APP" ] || fail "$APP is not in front after the assistant said so"
row "reached" "\"$REQ\" brought $APP forward ($(echo "$got" | json 'd["assistant"]'))"

$M terminate $APP >/dev/null 2>&1 || true
$M launch $START >/dev/null
got=$($M assistant "$REQ" --app $START --json)
[ "$(echo "$got" | json 'd["reached"]')" = False ] || fail "\"$REQ\" was reported as reaching $START"
row "control" "the same request, naming the app it left, is not reached"
echo PASS
