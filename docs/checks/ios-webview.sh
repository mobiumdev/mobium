#!/bin/sh
# iOS WebViews end to end through mobium's CLI, on a booted simulator.
#
# The sibling script ios-webview-probe.sh proves the *platform* assumption
# with no mobium code in the way — that the inspector is a plain Unix socket.
# This one proves mobium actually drives it: discovery, switching context,
# reading the page, and refusing what it cannot do correctly.
#
#   docs/checks/ios-webview.sh [udid]
#
# Safari is used because it is on every simulator. Note what that costs: its
# XCUIElementTypeWebView covers the whole window including the chrome, so the
# page's position inside it is unknowable and taps are refused. That is
# asserted here rather than worked around — an app's own WKWebView has a frame
# equal to its content and does not hit it.
set -e
UDID="${1:-booted}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium --backend webdriveragent"
# Named, not left to selection: with a phone connected as well as the
# simulator, "the iOS device" is two devices and every command is refused.
[ "$UDID" = booted ] || M="$M --device $UDID"
fail() { echo "FAIL: $*" >&2; exit 1; }

# appContexts lists the WebView contexts that belong to one app — WEBVIEW_<id>,
# or WEBVIEW_<id>_<n> when it has several — and nothing else. `contexts`
# reports every inspectable page on the device, not only the app in front: on
# a real iPhone with Wikipedia in the foreground it listed Safari's page. So
# "the first WEBVIEW_ line" can be another app's, and a count of them can pass
# with one of ours and one of Safari's.
appContexts() { $M contexts | awk -v id="WEBVIEW_$1" \
  '$1 == id || (index($1, id "_") == 1 && substr($1, length(id) + 2) ~ /^[0-9]+$/) { print $1 }'; }

command -v xcrun >/dev/null || { echo "no xcrun; skipping" >&2; exit 0; }
xcrun simctl list devices booted | grep -q Booted || {
  echo "no booted simulator; skipping" >&2; exit 0; }

echo "--- $UDID"

# Web Inspector has to be on for Safari, and the page has to exist. Both are
# preconditions rather than things mobium can arrange.
xcrun simctl spawn "$UDID" defaults write com.apple.mobilesafari \
    WebKitDeveloperExtrasEnabledPreferenceKey -bool true
xcrun simctl openurl "$UDID" "https://example.com"
sleep 5

# A page's target is announced to one debugger at a time, so a daemon left
# switched into a page blocks everything else — including this script.
"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true

$M launch com.apple.mobilesafari >/dev/null
sleep 3
[ "$($M current 2>/dev/null | tail -1)" = "com.apple.mobilesafari" ] \
  || fail "Safari is not in the foreground; the native WebView element will not be there"

ctx=$(appContexts com.apple.mobilesafari | head -1)
[ -n "$ctx" ] || fail "no WebView context on a simulator showing a web page: $($M contexts 2>&1 | grep -m1 "^error:" || echo "mobium listed none for this app")"
echo "    contexts       $ctx"

$M contexts | grep -q "example.com" || fail "the context does not name the page it is showing"
echo "    listing        carries the page title and URL                  ok"

$M context "$ctx" >/dev/null
echo "    attach         switched into the page                          ok"

# The load-bearing assertion: a value the page computed. Everything before
# this could pass with the target wrapping broken.
$M text | grep -q "This domain is for use in documentation examples" \
  || fail "the page's own text did not come back"
echo "    read           the page's text came back                       ok"

$M map | grep -q "Learn more" || fail "the page's link is not in the map"
echo "    map            found the page's link                           ok"

# Safari's WebView covers its chrome, so mobium must refuse to tap rather than
# land somewhere else. On an app's own WKWebView this branch does not trigger.
if $M tap 'Learn more' >/dev/null 2>&1; then
  fail "tapped inside Safari, where the page's position in its host is unknowable"
fi
$M map 2>&1 | grep -q "cannot be tapped" \
  || fail "the map did not say why these elements cannot be tapped"
echo "    geometry       refused to tap, and said why                    ok"

# Detach and re-attach on the same held connection. This failed until Close
# started sending _rpc_forwardDidClose:, because WebKit announces a page's
# target once per connection.
$M context NATIVE_APP >/dev/null
$M context "$ctx" >/dev/null
$M text | grep -q "Example Domain" || fail "re-attaching to the same page did not work"
echo "    re-attach      detached and attached again cleanly             ok"

$M context NATIVE_APP >/dev/null
"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true
echo "--- passed"
