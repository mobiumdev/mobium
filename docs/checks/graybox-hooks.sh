#!/bin/sh
# Gray-box hooks, on MobiumApp: functions the app registered by name, called
# from a test with `mobium hook`. MobiumApp registers three — raiseToast,
# screen and signIn — and each is held to what the app then shows, never to
# what the tool reports.
#
#   docs/checks/graybox-hooks.sh <serial|udid>
#
#   - screen:     answers the screen that is showing, read the way a user
#                 would see it;
#   - raiseToast: the toast the app draws says what was sent — an ASCII
#                 message, then one that is not, which a phone's keyboard
#                 would drop if it were typed as it is;
#   - signIn:     the app is signed in without its form: the welcome screen
#                 greets the name sent, and screen says so;
#   - unknown:    a name the app never registered is refused, naming the
#                 ones it did;
#   - normal:     a hook on an app launched without the gray box is refused,
#                 naming the launch that fixes it.
#
# Every hook leaves the device as it was: nothing outside MobiumApp.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-11s %-58s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
M="$ROOT/bin/mobium --device $DEV"
echo "--- $DEV"

$M terminate "$APP" >/dev/null 2>&1 || true
out=$($M launch --gray-box "$APP") || fail "launch: $out"
echo "$out" | grep -q "with the gray box" || fail "the app did not answer the gray box: $out"

said=$($M hook screen) || fail "hook screen: $said"
echo "$said" | grep -q '^hook screen answered: "home"' || fail "screen on the home screen answered $said"
row "screen" "answered \"home\" on the home screen"

for msg in "Toast raised by test script" "Привет, café — 5 ✓"; do
  said=$($M hook raiseToast "$msg") || fail "hook raiseToast: $said"
  echo "$said" | grep -q 'answered: "shown"' || fail "raiseToast answered $said"
  shown=$($M text testid=hookToast) || fail "no toast on screen after raiseToast: $shown"
  [ "$shown" = "$msg" ] || fail "the toast says \"$shown\", not \"$msg\""
done
row "raiseToast" "the app's toast says what was sent, ASCII and not"

said=$($M hook signIn mobium) || fail "hook signIn: $said"
echo "$said" | grep -q 'answered: "signed in as mobium"' || fail "signIn answered $said"
welcome=$($M text testid=welcomeText) || fail "no welcome screen after signIn: $welcome"
[ "$welcome" = "Welcome, mobium!" ] || fail "the welcome screen says \"$welcome\""
said=$($M hook screen)
echo "$said" | grep -q 'answered: "secret"' || fail "screen after signIn answered $said"
row "signIn" "signed in without the form; screen says \"secret\""

if out=$($M hook nosuch 2>&1); then fail "an unknown hook was answered: $out"; fi
echo "$out" | grep -q "no hook named nosuch; registered: raiseToast, screen, signIn" || fail "unknown hook: $out"
row "unknown" "refused, naming raiseToast, screen and signIn"

$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
if out=$($M hook screen 2>&1); then fail "a hook on an app launched normally was answered: $out"; fi
echo "$out" | grep -q "launch it with gray_box first" || fail "normal launch: $out"
row "normal" "refused without the gray box, naming the launch"
$M terminate "$APP" >/dev/null 2>&1 || true
echo "graybox-hooks.sh: ok"
