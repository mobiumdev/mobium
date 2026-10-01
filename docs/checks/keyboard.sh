#!/bin/sh
# The soft keyboard, end to end, on MobiumApp's Login screen: reading it,
# typing at the focused field, a named key, hiding it, and a password field
# that is typed into and never printed.
#
#   docs/checks/keyboard.sh <android-serial>
#   docs/checks/keyboard.sh <simulator-udid | iphone-udid>
#
# Two things the tool does are only right because they were measured, and
# this checks both. Android's keyboard cannot type at a cursor — its server
# replaces the field whatever it is asked — so typing is the field read and
# written back, and appending twice has to leave both. And an empty field
# reports its placeholder as its value on both platforms, which once made
# every append look like a dropped keystroke and, on iOS, the retry for one
# write the placeholder into the field.
#
# A simulator can be left believing a hardware keyboard is attached:
# XCTest's typing does it now and then, iOS then keeps the software keyboard
# below the screen, and only a reboot brings it back — not relaunching the
# app, not restarting the keyboard daemon (CHALLENGES 198). Mobium answers
# "not shown" then, truthfully, and this check's first step failed after
# one run in two. So on a simulator, a focused field with the keyboard held
# below the screen gets one reboot, said aloud, before the step is judged.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }
APP=dev.mobium.mobiumapp

KIND=device
case "$DEV" in *-*-*-*-*) KIND=simulator ;; esac
case "$DEV" in
  *-*-*-*-*|????????-????????????????) PLATFORM=ios
             M="$ROOT/bin/mobium --driver wda --device $DEV"
             PASSWORD="role=password" ;;
  *)         PLATFORM=android; M="$ROOT/bin/mobium --device $DEV"
             PASSWORD="testid=password" ;;
esac
# React Native repeats a test id on iOS (CHALLENGES 55), so the password
# field is reached by its role there.
echo "--- $DEV ($PLATFORM)"

# A listing that fails is not an empty one: a phone gone from its cable
# answered "not installed" here while the real error was the device.
APPS=$($M apps 2>&1) || fail "could not list the apps: $APPS"
echo "$APPS" | grep -q "$APP" || fail "$APP is not installed — build it first (see docs/checks/mobium-app.sh)"
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M tap "label=Login Demo" >/dev/null

[ "$($M keyboard --json | json 'd["shown"]')" = False ] || fail "the keyboard was up before anything had focus"
$M tap testid=username >/dev/null
sleep 1
# held_below: the keyboard is in the tree with its top at or below the
# screen's bottom edge — the hardware-keyboard state, not a missing one.
held_below() {
  $M source 2>/dev/null | python3 -c '
import re, sys
src = sys.stdin.read()
app = re.search(r"<XCUIElementTypeApplication [^>]*height=\"(\d+)\"", src)
kb = re.search(r"<XCUIElementTypeKeyboard [^>]*y=\"(-?\d+)\"", src)
print("yes" if app and kb and int(kb.group(1)) >= int(app.group(1)) else "no")'
}
if [ "$($M keyboard --json | json 'd["shown"]')" != True ] && [ "$KIND" = simulator ] && [ "$(held_below)" = yes ]; then
  echo "    reboot         the simulator held its keyboard below the screen, as with a hardware keyboard — rebooting it"
  $M daemon stop >/dev/null 2>&1 || true
  xcrun simctl shutdown "$DEV" && xcrun simctl boot "$DEV" && xcrun simctl bootstatus "$DEV" -b >/dev/null
  $M launch "$APP" >/dev/null
  $M tap "label=Login Demo" >/dev/null
  $M tap testid=username >/dev/null
  sleep 1
fi
[ "$($M keyboard --json | json 'd["shown"]')" = True ] || fail "focusing a field did not bring the keyboard up"
[ "$($M keyboard --json | json 'd["focused"].get("empty", False)')" = True ] \
  || fail "an empty field was not read as empty — its placeholder was taken for its value"
echo "    read           up once a field has focus, and an empty field is empty ok"

$M keyboard --text 'mob ü"q' >/dev/null
$M keyboard --text "'s" >/dev/null
v=$($M keyboard --json | json 'd["focused"]["value"]')
[ "$v" = "mob ü\"q's" ] || fail "two appends left the field holding [$v], want [mob ü\"q's]"
echo "    type           two appends, quotes and ü intact, read back       ok"

$M keyboard --key delete >/dev/null
v=$($M keyboard --json | json 'd["focused"]["value"]')
[ "$v" = "mob ü\"q'" ] || fail "delete left [$v]"
echo "    key            delete took one character                           ok"

$M tap "$PASSWORD" >/dev/null
sleep 1
out=$($M keyboard --text hunter2 2>&1)
case "$out" in *hunter2*) fail "the password was printed: $out" ;; esac
$M keyboard --json | grep -q hunter2 && fail "the password is in the structured result"
echo "    password       typed and never printed, in text or JSON           ok"

if [ "$PLATFORM" = android ]; then
  # Android cannot read a password back, and typing into one replaces it,
  # so adding to a non-empty one must be refused rather than overwrite it.
  $M keyboard --text x >/dev/null 2>&1 && fail "typing into a non-empty password field was not refused"
  echo "    password       adding to a non-empty one refused, not overwritten  ok"
  $M keyboard --hide >/dev/null
  [ "$($M keyboard --json | json 'd["shown"]')" = False ] || fail "hide left the keyboard up"
  [ "$($M current)" = "$APP" ] || fail "hiding the keyboard navigated away from $APP"
  echo "    hide           hidden, confirmed, and the app still in front      ok"
else
  # An iPhone keyboard has no hide key; the refusal must name the way out,
  # and following it must work.
  $M keyboard --hide 2>&1 | grep -q 'key "enter"' || fail "the hide refusal did not name enter"
  $M keyboard --key enter >/dev/null
  [ "$($M keyboard --json | json 'd["shown"]')" = False ] || fail "enter, the named way out, left the keyboard up"
  echo "    hide           refused with a remedy, and the remedy worked        ok"
fi
$M keyboard --hide | grep -q "already hidden" || fail "hiding a hidden keyboard did something"

# app_type adds to a field and app_fill replaces it, as Vibium's type and fill
# do — the same answer as the keyboard's own appends above, by another road.
$M fill testid=username mob >/dev/null
$M type testid=username ium >/dev/null
v=$($M text testid=username)
[ "$v" = mobium ] || fail "type after fill left [$v], want [mobium]: type must add to the field"
$M fill testid=username xyz >/dev/null
v=$($M text testid=username)
[ "$v" = xyz ] || fail "fill left [$v], want [xyz]: fill must replace"
out=$($M type "$PASSWORD" more 2>&1) && fail "adding to a non-empty password field was accepted: $out"
echo "$out" | grep -q "app_fill" || fail "the refusal to add to a password did not name app_fill: $out"
echo "    type, fill     type added, fill replaced, a password refused       ok"
# From the home screen, where nothing has focus: on Android focus outlives a
# hidden keyboard, so after the password step it would still be there, and
# this once passed on the password refusal instead of the one it names.
$M terminate "$APP" >/dev/null; $M launch "$APP" >/dev/null
$M keyboard --text x 2>&1 | grep -q "no field has keyboard focus.*app_type" \
  || fail "typing with nothing focused was not refused with app_type as the way through"
echo "    idle           hide does nothing, typing is refused with a remedy  ok"
