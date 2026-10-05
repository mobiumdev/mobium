#!/bin/sh
# A fifth third-party app on iOS, and the first whose WebView opens: Kiwix,
# the offline Wikipedia reader, built from its source for a simulator.
#
#   docs/checks/kiwix-ios.sh <simulator-udid>
#
# Kiwix sets isInspectable on its WKWebView unless its build is a
# "production" one; the App Store's iOS build on an iPhone published its
# page too, and its taps were measured there by hand. This check is for a
# simulator because it starts from cleared data and uploads the ZIM, which
# a phone allows only into apps installed for development (CHALLENGES 249).
# Its first look found five things wrong
# (CHALLENGES 244–248), held here against one pinned ZIM, Wikipedia's Ray
# Charles articles, so what the page shows does not move:
#
#   - each catalog card is one entry, not one per word in it (244);
#   - an ambiguous locator none of whose matches is on screen is told to
#     name only one, not to take a ref app_map cannot give (245);
#   - an article tile on the ZIM's main page is one link, named once (246);
#   - a tap on the disabled List button, by its own ref, is refused rather
#     than reported done through the wrapper around it (247);
#   - a tap on an article in the WebView, which runs under the bars and is
#     taller than its page, opens that article (248).
#
# The app's data is cleared first. The ZIM is uploaded to its Documents and
# opened through the system document picker, as a person would: its own
# download fails in a build from source. KIWIX_ZIM names the file; without
# it the pinned one is downloaded and its checksum checked.
#
# To build it (no signing, and no `brew bundle`, which installs pre-commit
# and a git hook — its last steps are all the build needs):
#   git clone https://github.com/kiwix/kiwix-apple.git && cd kiwix-apple
#   curl -L -o - https://download.kiwix.org/release/libkiwix/libkiwix_xcframework-14.2.1-2.tar.gz | tar -x --strip-components 2
#   for p in ios-arm64 ios-arm64_x86_64-simulator macos-arm64_x86_64; do
#     cp Support/CoreKiwix.modulemap CoreKiwix.xcframework/$p/Headers/module.modulemap; done
#   python3 localizations.py generate && xcodegen
#   xcodebuild -project Kiwix.xcodeproj -scheme Kiwix -destination 'generic/platform=iOS Simulator' \
#     -derivedDataPath ../kiwix-dd -skipMacroValidation CODE_SIGNING_ALLOWED=NO build
#   mobium install ../kiwix-dd/Build/Products/Debug-iphonesimulator/Kiwix.app
set -e
DEV="${1:?usage: kiwix-ios.sh <simulator-udid>}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium --driver wda --device $DEV"
APP=self.Kiwix
ZIM_NAME=wikipedia_en_ray-charles_mini_2026-08.zim
ZIM_SHA256=9aa68a83cf9a7d9449724b6c722f797e2a307e290a34b083df717028cd42330d
fail() { $M context NATIVE_APP >/dev/null 2>&1 || true; echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
# ref <pattern>: the first map entry whose line matches, by ref only.
ref() { $M map | grep -E "^@e[0-9]+ $1" | awk '{ print $1; exit }'; }

xcrun simctl list devices 2>/dev/null | grep -q "$DEV" ||
  fail "$DEV is not a simulator: this check clears the app's data and uploads into it, which a phone does not allow"

ZIM="${KIWIX_ZIM:-}"
if [ -z "$ZIM" ]; then
  ZIM="${TMPDIR:-/tmp}/$ZIM_NAME"
  [ -f "$ZIM" ] || curl -sfL -o "$ZIM" "https://download.kiwix.org/zim/wikipedia/$ZIM_NAME" ||
    fail "could not download $ZIM_NAME"
fi
[ "$(shasum -a 256 "$ZIM" | cut -c1-64)" = "$ZIM_SHA256" ] ||
  fail "$ZIM is not the pinned $ZIM_NAME: what this check reads would move"

echo "--- $DEV"
$M clear-data "$APP" >/dev/null
$M launch "$APP" >/dev/null

echo "  the catalog"
# A first launch with nothing to read opens the Library by itself.
$M wait "label=Categories" >/dev/null || fail "the Library did not come up"
$M tap "$(ref 'Wikipedia \(button')" >/dev/null
$M wait "label=Astronomy by Wikipedia" >/dev/null || fail "the Wikipedia category did not load — is the network up?"
lines=$($M map)
for part in 'maxi' 'nopic' '1\.77 GB' '153K pages'; do
  if echo "$lines" | grep -qE "^@e[0-9]+ $part \\(button\\)$"; then
    fail "a card's own words are a target of their own: $(echo "$lines" | grep -E "^@e[0-9]+ $part \\(" | head -1)"
  fi
done
echo "$lines" | grep -qE '^@e[0-9]+ Astronomy by Wikipedia, A selection of Wikipedia articles on astronomy, .+, maxi, ' ||
  fail "a card is not one entry named by all its words"
row 244 "each card is one entry"
out=$($M scroll-to "label=Ray Charles" --direction down 2>&1 || true)
case "$out" in
  *"use a ref from app_map"*) fail "off-screen matches were sent to app_map for a ref: $out" ;;
  *"make it name only one"*) ;;
  *) fail "label=Ray Charles was not refused as naming several cards: $out" ;;
esac
row 245 "off-screen matches: name only one, not take a ref"
# Back to the categories, then out of the Library: inside a category its bar
# has no Done.
$M tap "$(ref 'Categories \(button\)')" >/dev/null
$M tap "$(ref 'Done \(button')" >/dev/null

echo "  the ZIM"
$M upload "$ZIM" --app "$APP" >/dev/null
$M tap "label=Open File" >/dev/null
$M wait "label=Browse" >/dev/null || fail "the document picker did not come up"
# It reopens where it was last left; Browse shows the app's own folder.
if ! $M map | grep -q "$ZIM_NAME"; then $M tap "$(ref 'Browse \(button')" >/dev/null; fi
$M wait "label=$ZIM_NAME" >/dev/null || fail "the document picker does not show the uploaded ZIM"
$M tap "$(ref "$ZIM_NAME")" >/dev/null
$M wait "label=Tabs Manager" >/dev/null || fail "the ZIM did not open"
lines=$($M map)
[ "$(echo "$lines" | grep -cE '^@e[0-9]+ America the Beautiful \(link\)$')" = 1 ] ||
  fail "the America the Beautiful tile is not one link: $(echo "$lines" | grep 'America' | tr '\n' ' ')"
if echo "$lines" | grep -q 'America the Beautiful America the Beautiful'; then
  fail "a tile is named twice over"
fi
row 246 "an article tile is one link, named once"
list=$(ref 'List \(button, disabled\)')
[ -n "$list" ] || fail "the disabled List button is not in map as disabled"
if out=$($M tap "$list" 2>&1); then fail "a tap on the disabled List button was reported done: $out"; fi
case "$out" in *"check enabled"*) ;; *) fail "the List button was refused, but not as disabled: $out" ;; esac
row 247 "a tap on the disabled List button is refused"

echo "  the WebView"
web=$($M contexts | awk '/WEBVIEW/ { print $1; exit }')
[ -n "$web" ] || fail "Kiwix published no WebView: is this a build from source?"
$M context "$web" >/dev/null
tile=$($M map | grep -E '^@e[0-9]+ Hank Crawford \(link\)$' | awk '{ print $1; exit }')
[ -n "$tile" ] || fail "the page's Hank Crawford link is not in its map"
if $M map | grep -q "cannot be tapped"; then fail "the page says its elements cannot be tapped"; fi
$M tap "$tile" >/dev/null || fail "the tap on Hank Crawford was refused"
opened=""
for _ in 1 2 3 4 5 6 7 8 9 10; do
  case "$($M eval 'location.href' 2>/dev/null)" in *Hank_Crawford*) opened=1; break ;; esac
  sleep 1
done
[ -n "$opened" ] || fail "the tap did not open Hank Crawford: $($M eval 'location.href' 2>&1)"
row 248 "a tap in the page opens the article it names"

$M context NATIVE_APP >/dev/null
$M press home >/dev/null
echo "  pass"
