#!/bin/sh
# A third third-party app on iOS, and the first in SwiftUI: Ice Cubes, a
# Mastodon client, on a simulator built from its source and on an iPhone
# from the App Store.
#
#   docs/checks/icecubes-ios.sh <simulator-udid | iphone-udid>
#
# Wikipedia and NetNewsWire are UIKit. SwiftUI builds its hierarchy its own
# way, as Jetpack Compose did on Android, and its first look found five
# things wrong (CHALLENGES 213–217, and 219–221). This check holds each of them to the
# app, signed out, on a server's public timeline:
#
#   - the timeline picker, a PopUpButton with the Header trait, is in map
#     and opens its menu by ref (213);
#   - a post's Reply, Boost, Favorite and Share buttons are in map, and the
#     post's Share and its image open by ref (214);
#   - label=Close in the image viewer, and label=Display Settings in
#     Settings, each find one control (215);
#   - a tap on the selected tab is not reported as covered, and goes
#     through: from a page inside Settings it returns to Settings (216);
#   - a tab behind the image viewer, and behind the Add Account sheet, is
#     refused rather than reported tapped (217);
#   - Display Settings' Font Scaling slider maps as a slider with its value,
#     is filled by position, and is put back to what it read (219);
#   - a post's menus: a tap behind one is refused naming a point outside
#     it, which closes it, and a menu taller than the screen is scrolled
#     inside (220, 221).
#
# The timeline is live, so nothing here asserts what a post says, and
# nothing a post says is printed. Nothing is posted, favorited or boosted:
# Share and the image viewer are opened and closed. On a fresh install iOS
# asks to send notifications, which is declined, and the app offers to add
# an account on launch and on returning to the timeline, which is canceled.
#
# To build it for a simulator (no signing). Its main branch needs the iOS 27
# SDK; 9efcb16~1 is the last commit that builds with Xcode 26.6:
#   git clone https://github.com/Dimillian/IceCubesApp.git && cd IceCubesApp
#   git checkout 9efcb16~1
#   cp IceCubesApp.xcconfig.template IceCubesApp.xcconfig
#   xcodebuild -project IceCubesApp.xcodeproj -scheme IceCubesApp \
#     -sdk iphonesimulator -destination 'generic/platform=iOS Simulator' \
#     -derivedDataPath ../ic-dd CODE_SIGNING_ALLOWED=NO build
#   mobium install "../ic-dd/Build/Products/Debug-iphonesimulator/Ice Cubes.app"
set -e
DEV="${1:?usage: icecubes-ios.sh <udid>}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium --driver wda --device $DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
# ref <pattern>: the first map entry whose line matches, by ref only.
ref() { $M map | grep -E "^@e[0-9]+ $1" | awk '{ print $1; exit }'; }
# signed_out: cancel the Add Account sheet if it is up.
signed_out() {
  if $M map | grep -q "^@e[0-9]* Instance URL (input)"; then
    $M tap label=Cancel >/dev/null
  fi
  $M wait "label=Timeline,role=button" >/dev/null || fail "the timeline did not come up"
}

echo "--- $DEV"
# Listed first, so a device that is not ready says so, rather than reading
# as an app that is not installed.
APPS=$($M apps) || fail "could not list the apps on $DEV — see the error above"
APP=$(echo "$APPS" | awk '/Ice Cubes/ { print $1; exit }')
[ -n "$APP" ] || fail "Ice Cubes is not installed — see the top of this file"
row "install" "$APP present, confirmed by listing"

$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
if $M alert 2>/dev/null | grep -q "Notifications"; then
  $M tap "label=Don’t Allow" >/dev/null
  row "prompt" "notifications declined"
fi
signed_out

# The timeline picker (213).
PICK=$(ref "(Trending|Local|Federated|Home) \(button\)")
[ -n "$PICK" ] || fail "the timeline picker is not in map"
$M tap "$PICK" >/dev/null
$M wait "label=Federated" >/dev/null || fail "the picker's menu did not open from its ref"
$M tap "label=Trending,role=button" >/dev/null
$M wait "label=Reply,role=button" >/dev/null || fail "choosing Trending did not bring the timeline back"
row "picker" "in map, and its menu opened by ref"

# A post's controls (214). The timeline is live, and a post can be taller
# than the screen — one filled the iPhone's on 2026-10-06, its controls below
# the edge — so swipe until a post's controls are in view; every post has a
# Reply, so scroll-to would be told it names several.
posts=$($M map)
tries=0
while ! echo "$posts" | grep -q "^@e[0-9]* Reply (button)" && [ $tries -lt 4 ]; do
  $M swipe up >/dev/null
  # A swipe coasts; a map taken while the timeline moves hands out refs
  # that are elsewhere by the time they are used.
  sleep 2
  posts=$($M map)
  tries=$((tries + 1))
done
for b in Reply Boost Favorite "Share post link"; do
  echo "$posts" | grep -q "^@e[0-9]* $b (button)" || fail "no post's $b button is in map"
done
row "post" "Reply, Boost, Favorite and Share are in map"

# A post's menus, the "…" one and a long press's (220, 221). Nothing in
# them is pressed. A tap behind one is refused, and the refusal names a
# point outside it, since a menu has no close control; that point closes
# it. The long press's menu runs past the bottom of the screen, and
# scroll-to brings its last item into view rather than calling it there.
closes_by_refusal() {
  out=$($M tap label=Trending 2>&1) && fail "a tap behind the $1 went through: $out"
  echo "$out" | grep -q "another screen" || fail "the tap behind the $1 was refused for another reason: $out"
  XY=$(echo "$out" | sed -n 's/.*app_tap at x \([0-9]*\), y \([0-9]*\).*/\1 \2/p')
  [ -n "$XY" ] || fail "the refusal behind the $1 offered no point outside it: $out"
  $M tap $XY >/dev/null
  $M wait "label=Copy Link" --for hidden >/dev/null 2>&1 || fail "the point the refusal offered did not close the $1"
}
$M tap "$(ref "status.action.context-menu \(button\)")" >/dev/null
$M wait "label=Copy Link" >/dev/null || fail "the post's … button opened no menu"
closes_by_refusal "… menu"
$M long-press "$(ref ".*, [0-9]+[smhd] .*\(button\)$")" >/dev/null
$M wait "label=Copy Link" >/dev/null || fail "a long press on a post opened no menu"
$M scroll-to "label=Report Post" >/dev/null || fail "the long press's menu could not be scrolled to its last item"
$M map | grep -q "^@e[0-9]* Report Post (button)" || fail "scroll-to called Report Post in view, and map does not list it"
closes_by_refusal "long press's menu"
row "menus" "both opened; behind refused, closed at the point it named"
$M tap "$(ref "Share post link \(button\)")" >/dev/null
$M wait "label=Copy" >/dev/null || fail "Share did not open the share sheet"
# The share sheet has no Close button; a tap above it closes it.
$M tap 600 400 >/dev/null
$M wait "label=Reply,role=button" >/dev/null || fail "the share sheet did not close"
row "share" "Share opened the share sheet by ref"

# The image viewer, and label=Close (214, 215).
IMG=$(ref "Image alt text: ")
if [ -n "$IMG" ]; then
  $M tap "$IMG" >/dev/null
  $M wait "label=Info" >/dev/null || fail "the image did not open the viewer"
  out=$($M tap label=Timeline 2>&1) && fail "the tab behind the viewer was reported tapped: $out"
  echo "$out" | grep -q "another screen" || fail "the tab behind the viewer was refused for another reason: $out"
  out=$($M tap label=Close 2>&1) || fail "label=Close in the image viewer: $out"
  $M wait "label=Reply,role=button" >/dev/null || fail "Close did not close the viewer"
  row "viewer" "opened by ref; tab behind refused; label=Close closed it"
else
  printf '    %-12s %-58s NOT CHECKED\n' "viewer" "no post on screen has an image"
fi

# Tabs: a label that names the tab and its icon, and the selected tab (215,
# 216). Settings' rows are the app's own, so this part is the same every run.
$M tap label=Settings,role=button >/dev/null
$M wait "label=Display Settings" >/dev/null || fail "the Settings tab did not open"
out=$($M tap "label=Display Settings" 2>&1) || fail "label=Display Settings: $out"
$M wait "label=Match System" >/dev/null || fail "Display Settings did not open"

# A slider (219): mapped as one, with its value as its state, and filled
# with a position from 0 to 1. Font Scaling is the app's own setting; it is
# put back to what it read, stepping from the start of the track, where a
# move lands exactly.
# Followed by the locator map gives it, not its ref: every map renumbers,
# and on the iPhone the screen shifted between two, so a ref taken from one
# named a button in the next.
slider() { $M map --json | python3 -c '
import json, sys
for e in json.load(sys.stdin)["elements"]:
    if e["role"] == "slider":
        l = e["locator"]; print(l["kind"] + "=" + l["value"]); break'; }
reads() { $M map --json | python3 -c '
import json, sys
for e in json.load(sys.stdin)["elements"]:
    l = e.get("locator") or {}
    if l.get("kind", "") + "=" + l.get("value", "") == sys.argv[1]:
        print(e.get("value", "")); break' "$1"; }
labeled_by_value() { $M map --json | python3 -c '
import json, sys
print(any(e["role"] == "slider" and e["value"] and e["label"] == e["value"]
          for e in json.load(sys.stdin)["elements"]))'; }
$M scroll-to "label=Font Scaling" >/dev/null || fail "Display Settings has no Font Scaling to scroll to"
SL=$(slider)
[ -n "$SL" ] || fail "no slider on Display Settings maps as one"
WAS=$(reads "$SL")
[ -n "$WAS" ] || fail "the slider maps with no value"
[ "$(labeled_by_value)" = False ] || fail "a slider is labeled by its value"
out=$($M fill "$SL" abc 2>&1) && fail "fill abc on a slider was accepted: $out"
[ "$(reads "$SL")" = "$WAS" ] || fail "a refused fill moved the slider"
$M fill "$SL" 1 >/dev/null
TOP=$(reads "$SL")
$M fill "$SL" 0 >/dev/null
BOTTOM=$(reads "$SL")
[ "$TOP" != "$BOTTOM" ] || fail "the slider read $TOP at both ends of its track"
# Where it was, worked out from the two ends when all three read as
# numbers ("50%", "150%"), is tried first: a fill on a phone takes seconds,
# and stepping up from the start took ten minutes on the iPhone.
guess=$(python3 -c '
import re, sys
n = [float(re.sub(r"[^0-9.\-]", "", v) or "nan") for v in sys.argv[1:]]
was, lo, hi = n
print(round((was - lo) / (hi - lo), 3) if hi != lo and was == was else "")' "$WAS" "$BOTTOM" "$TOP" 2>/dev/null)
back=""
for p in $guess 0 0.1 0.2 0.3 0.4 0.5 0.6 0.7 0.8 0.9 1; do
  $M fill "$SL" 0 >/dev/null
  $M fill "$SL" "$p" >/dev/null
  [ "$(reads "$SL")" = "$WAS" ] && { back=$p; break; }
done
[ -n "$back" ] || fail "the slider could not be put back to $WAS"
row "slider" "$BOTTOM to $TOP by position; back to $WAS at $back"
TAB=$($M map | awk '/^@e[0-9]+ Settings \(button, selected\)$/ { print $1; exit }')
[ -n "$TAB" ] || fail "the Settings tab does not read as selected"
said=$($M tap "$TAB" --json)
echo "$said" | grep -q '"cover"' && fail "the selected tab was reported as covered: $said"
$M wait "label=Display Settings" >/dev/null || fail "a tap on the selected tab did not return to Settings"
row "tabs" "one Display Settings; the selected tab not covered, went"

# Back to the timeline, where the app offers to add an account again.
$M tap label=Timeline,role=button >/dev/null
if $M wait "text=Instance URL" >/dev/null 2>&1; then
  out=$($M tap label=Settings,role=button 2>&1) && fail "the tab behind the sheet was reported tapped: $out"
  echo "$out" | grep -q "another screen" || fail "the tab behind the sheet was refused for another reason: $out"
  row "sheet" "the tab behind the Add Account sheet refused"
else
  printf '    %-12s %-58s NOT CHECKED\n' "sheet" "the app did not offer to add an account"
fi
signed_out
$M terminate "$APP" >/dev/null 2>&1 || true
echo PASS
