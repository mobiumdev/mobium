#!/bin/sh
# Locators for a list, on MobiumApp's Table Demo: thirty rows, Item 1 to
# Item 30, each with a More and a Delete whose test IDs repeat in every row,
# as a real list's cells' do — so a hand-written locator can only tell them
# apart by the row. Item 8 has no More; Items 1 to 3 have a Starred switch.
#
#   docs/checks/locators.sh <simulator-udid | iphone-udid>
#   docs/checks/locators.sh <android-serial>
#
# What it holds, judged by what the app says it received:
#
#   - `within` scopes a target to the row of its anchor: More within Item 2
#     is Item 2's (CHALLENGES 307). React Native's rows arrive flat on both
#     platforms — an empty node the size of the row, its text and buttons
#     beside it under the list — so the row is found by its rectangle;
#   - Item 8 has no More, and More within Item 8 finds nothing: never the
#     More of the row beside it, the negative control;
#   - a scoped target off screen is scrolled to and pressed, Item 25's;
#   - a quoted value is the whole text (CHALLENGES 306): text=Item is a
#     part of every title, and "Item" is none of them;
#   - nth picks one of what is shown, in reading order: the third Delete is
#     Item 3's;
#   - a switch is scoped like a button: Starred within Item 3.
#
# Passed on the iPhone 17 Pro simulator (iOS 26.5) and the Android 15
# emulator. Nothing here changes the device: Starred is the app's own.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-14s %-56s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
case "$DEV" in
  *-*-*-*-*|????????-????????????????) M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *) M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV"

# A listing that fails is not an empty one.
APPS=$($M apps 2>&1) || fail "could not list the apps: $APPS"
echo "$APPS" | grep -q "$APP" || fail "$APP is not installed — build it first (see docs/checks/mobium-app.sh)"
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M scroll-to 'text="Table Demo"' >/dev/null 2>&1 || true
$M tap 'text="Table Demo"' >/dev/null || fail "MobiumApp has no Table Demo — rebuild it (mobium-app's Table Demo)"
$M wait testid=tableOutcome >/dev/null || fail "the Table Demo did not open"

# outcome <text>: the app says it received exactly that.
outcome() {
  got=$($M text testid=tableOutcome)
  [ "$got" = "$1" ] || fail "the app received \"$got\", want \"$1\""
}
# press <locator> <outcome>
press() {
  out=$($M tap "$1" 2>&1) || fail "tap $1: $out"
  sleep 0.5
  outcome "$2"
}

# Repeated, so a bare locator is refused rather than guessed: the reason
# within exists.
st=0; out=$($M tap label=More 2>&1) || st=$?
[ $st -eq 4 ] || fail "label=More, in every row, exited $st, want 4 (ambiguous): $out"
row "ambiguous" "label=More alone is refused: it is in every row"

press 'label=More within "Item 2"' "More: Item 2"
press 'label=Delete within text=Item 5' "Delete: Item 5"
row "within" "More and Delete, each in its own row"

out=$($M find 'label=More within "Item 8"' 2>&1)
echo "$out" | grep -q "No element matches" || fail "Item 8 has no More, and within found one: $out"
row "no borrowing" "Item 8 has no More, and none is found"

press 'role=switch within "Item 3"' "Starred: Item 3 on"
press 'role=switch within "Item 3"' "Starred: Item 3 off"
row "a switch" "Starred within Item 3, on and back off"

press 'label=Delete,nth=3' "Delete: Item 3"
row "nth" "the third Delete shown is Item 3's"

# Where only parts match, quotes decide: CHALLENGES 233's shape, a wait met
# by "Network & internet". Every title contains Item; none is it.
n=$($M find text=Item 2>&1 | grep -c "Item ")
[ "$n" -ge 2 ] || fail "text=Item, a part of every title, found $n"
out=$($M find 'text="Item"' 2>&1)
echo "$out" | grep -q "No element matches" || fail "text=\"Item\", the whole of no title, found: $out"
$M find 'text="Item 1"' 2>&1 | grep -q "Item 1[0-9]" && fail "text=\"Item 1\" matched an Item 1x"
row "quoted" "text=Item is every row; \"Item\" is none of them"

out=$($M scroll-to 'label=More within "Item 25"' 2>&1) || fail "Item 25's More, off screen, was not reached: $out"
press 'label=More within "Item 25"' "More: Item 25"
row "off screen" "Item 25's More, scrolled to and pressed"

echo PASS
