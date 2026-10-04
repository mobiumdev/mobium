#!/bin/sh
# A second third-party app on iOS: NetNewsWire, an RSS reader in UIKit, on a
# simulator built from its source and on an iPhone from the App Store.
#
#   docs/checks/netnewswire-ios.sh <simulator-udid | iphone-udid>
#
# Wikipedia was the only third-party app driven on iOS, and only on a phone,
# since an App Store build will not run on a simulator. NetNewsWire is open
# source (MIT), needs no account and ships with a few feeds, so the same app
# runs on both. Its first look found seven things wrong (CHALLENGES
# 191–197), and this check holds each of them to the app:
#
#   - no label is an XCUITest type name ("XCUIElementTypeTable"), and none
#     borrows a disclosure arrow's name ("On My iPhone chevron");
#   - a section header ("Accounts") is not a button;
#   - the chosen search scope reads "(button, selected)", and map --diff
#     says when it moves;
#   - label=NetNewsWire Blog,role=button finds the back button alone, not
#     the article's link of the same name;
#   - label=URL on the Add Feed sheet is refused naming text=URL, the
#     locator that works, and not the keyboard;
#   - a swipe on an article row reveals its actions without performing the
#     first, the revealed Star is tapped by name, and the row says Starred;
#   - and a row behind iOS 26's toolbar — the feed's on a simulator, the
#     lowest article's on both — is scrolled out from under it and opens
#     (197).
#
# The row is unstarred again before the end. Nothing is added: the Add Feed
# sheet is canceled. On a fresh install iOS asks to send notifications,
# which is declined, as a person who had not decided would leave it.
#
# To build it for a simulator (no signing; the first build can fail on a
# generated secrets file and pass the second time):
#   git clone --depth 1 https://github.com/Ranchero-Software/NetNewsWire.git
#   xcodebuild -project NetNewsWire/NetNewsWire.xcodeproj -scheme NetNewsWire-iOS \
#     -sdk iphonesimulator -destination 'generic/platform=iOS Simulator' \
#     -derivedDataPath nnw-dd CODE_SIGNING_ALLOWED=NO build
#   mobium install nnw-dd/Build/Products/Debug-iphonesimulator/NetNewsWire.app
set -e
DEV="${1:?usage: netnewswire-ios.sh <udid>}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium --driver wda --device $DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }

echo "--- $DEV"
APP=$($M apps | awk '/NetNewsWire/ { print $1; exit }')
[ -n "$APP" ] || fail "NetNewsWire is not installed — see the top of this file"
row "install" "$APP present, confirmed by listing"

$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
if $M alert 2>/dev/null | grep -q "Notifications"; then
  $M tap "label=Don’t Allow" >/dev/null
  row "prompt" "notifications declined"
fi
# It reopens where it was left: back out to the feed list, by its back
# button, which is named by the screen it returns to.
for _ in 1 2 3; do
  $M map | grep -q "^@e[0-9]* Settings (button)" && break
  $M tap label=close >/dev/null 2>&1 || true
  $M tap testid=BackButton >/dev/null 2>&1 || $M tap "label=Feeds,role=button" >/dev/null 2>&1 || true
done
$M wait "label=Settings" >/dev/null || fail "the feed list did not come up"

# Settings: types, chevrons and headers.
$M tap label=Settings >/dev/null
$M wait "label=Done" >/dev/null
settings=$($M map)
echo "$settings" | grep -q "XCUIElementType" && fail "a label is an XCUITest type name:
$settings"
echo "$settings" | grep -q "chevron" && fail "a row borrows the disclosure arrow's name:
$settings"
echo "$settings" | grep -q "^@e[0-9]* Accounts (button)" && fail "the Accounts header maps as a button"
echo "$settings" | grep -q "^@e[0-9]* On My iPhone (button)" || fail "the On My iPhone row is not mapped by its name:
$settings"
row "settings" "no type names, no chevrons, headers not buttons"
$M tap label=Done >/dev/null

# Swipe actions on an article row, in the feed that ships with the app.
$M tap "text=NetNewsWire Blog" >/dev/null
$M wait "label=Mark All as Read" >/dev/null
ROW=$($M map | awk '/NetNewsWire Blog, / && !/Starred, / { print $1; exit }')
[ -n "$ROW" ] || fail "no unstarred article row in NetNewsWire Blog"
TITLE=$($M map | grep "^$ROW " | sed 's/.*NetNewsWire Blog, \([^,]*\),.*/\1/')
$M swipe "$ROW" left >/dev/null
$M wait "label=Star,role=button" >/dev/null || fail "the swipe revealed no Star action"
$M map | grep -F "$TITLE" | grep -q "Starred, " && fail "the swipe performed the action instead of revealing it"
$M tap "label=Star,role=button" >/dev/null
$M map | grep -F "$TITLE" | grep -q "Starred, " || fail "tapping the revealed Star did not star \"$TITLE\""
row "swipe" "a row's actions revealed, Star tapped by name, row starred"
ROW=$($M map | grep -F "$TITLE" | awk '{ print $1; exit }')
$M swipe "$ROW" left >/dev/null
$M tap "label=Unstar,role=button" >/dev/null
$M map | grep -F "$TITLE" | grep -q "Starred, " && fail "the row could not be unstarred"
row "restore" "unstarred again"

# The back button against a link of the same name.
ROW=$($M map | grep -F "$TITLE" | awk '{ print $1; exit }')
$M tap "$ROW" >/dev/null
$M wait "label=Star Article" >/dev/null || fail "the article did not open"
$M map | grep -q "NetNewsWire Blog (link)" || fail "the article has no NetNewsWire Blog link to be ambiguous with"
$M tap "label=NetNewsWire Blog,role=button" >/dev/null || fail "the back button's locator is ambiguous with the link"
$M wait "label=Mark All as Read" >/dev/null
row "link" "role=button found the back button, not the link"

# The selected search scope, and its diff.
$M fill "label=Search Articles" "NetNewsWire" >/dev/null
$M wait "label=All Articles" >/dev/null
scope=$($M map | grep -E "^@e[0-9]+ (Here|All Articles) \(button")
echo "$scope" | grep -c "selected)" | grep -q "^1$" || fail "not exactly one scope reads as selected:
$scope"
other=$(echo "$scope" | grep -v "selected)" | sed 's/^@e[0-9]* \(.*\) (button)$/\1/')
$M map >/dev/null
$M tap "label=$other" >/dev/null
$M map --diff | grep -q "$other (button, selected) — was not selected" || fail "map --diff did not say the scope moved to $other"
row "selected" "one scope selected, and map --diff saw it move"
$M tap label=close >/dev/null 2>&1 || true

# The lowest article row on screen lies partly behind the toolbar, which iOS
# 26 draws over the list: on the iPhone 15 Plus its center was behind it,
# and a tap there touched the toolbar and was reported done (197). It must
# open the article, scrolled out from under the bar first when it has to be.
LOW=$($M map --json | python3 -c '
import json, sys
rows = [e for e in json.load(sys.stdin)["elements"] if "NetNewsWire Blog, " in e["label"]]
e = max(rows, key=lambda e: e["bounds"]["y1"])
print(e["ref"], (e["bounds"]["y1"] + e["bounds"]["y2"]) // 2)')
set -- $LOW
said=$($M tap "$1")
$M wait "label=Star Article" >/dev/null || fail "tapping the lowest row did not open its article: $said"
at=$(echo "$said" | sed -n 's/.* at ([0-9]*, \([0-9]*\))$/\1/p')
if [ -n "$at" ] && [ "$at" -lt "$2" ]; then moved="scrolled out from under the toolbar first"; else moved="tapped where it was"; fi
row "toolbar" "the lowest row opened — $moved"
$M tap testid=BackButton >/dev/null
$M wait "label=Mark All as Read" >/dev/null

# A locator under other words.
$M tap "label=Feeds,role=button" >/dev/null
$M tap label=Add >/dev/null
$M tap "label=Add Feed" >/dev/null
# Add Feed reads the clipboard, so iOS asks first whenever it holds
# something another app put there — after mobium-app.sh's clipboard check,
# every time. Declined, as for notifications.
for _ in 1 2 3; do
  $M map | grep -q "^@e[0-9]* Don’t Allow Paste (button)" || break
  $M tap "label=Don’t Allow Paste" >/dev/null
done
$M wait "text=URL" >/dev/null
out=$($M fill label=URL x 2>&1) && fail "label=URL was accepted: $out"
echo "$out" | grep -q "but text=URL does" || fail "the refusal does not name the locator that works: $out"
echo "$out" | grep -q -i "keyboard" && fail "the refusal still blames the keyboard: $out"
$M tap label=Cancel >/dev/null
row "near miss" "label=URL refused, naming text=URL"

$M terminate "$APP" >/dev/null 2>&1 || true
echo PASS
