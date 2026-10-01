#!/bin/sh
# Chrome on Android, end to end: a page in a mobile browser, reached as a web
# context and held to what the page itself says.
#
#   docs/checks/chrome.sh <emulator-serial>
#
# Needs the network: the page is https://example.com, opened in a new tab
# with a query string of its own so that its context is found by URL — the
# tabs a browser keeps are numbered in listing order, so a name from one
# listing is not a name for the next.
#
# - The page opens in com.android.chrome, and its context is listed with the
#   page's title and URL.
# - text, map and eval answer from the page.
# - A tap on a ref in the web context lands where the page sees it: a button
#   put on the page counts its clicks, and the count is read from the page.
# - A tap on the page's own link navigates: the page's host is then iana.org.
#
# Safari's page on iOS is ios-webview.sh, where a tap in the web context is
# refused; this is the platform where it lands. A real phone is refused: the
# check opens tabs in somebody's browser.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <emulator-serial>" >&2; exit 2; fi
case "$DEV" in
  emulator-*) ;;
  *) echo "$DEV is not an emulator: this check opens tabs in the device's browser, and a phone's is somebody's" >&2; exit 2 ;;
esac
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium --device $DEV"
fail() { $M context NATIVE_APP >/dev/null 2>&1 || true; echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
echo "--- $DEV"

T=$(date +%s)
URL="https://example.com/?chrome=$T"
$M context NATIVE_APP >/dev/null 2>&1 || true
# Chrome started fresh: once an installed web app has been opened while
# Chrome was running, Chrome stops putting its WebView in the accessibility
# tree — for its tabs too — and a tap has nowhere to be measured from.
# CHALLENGES 200; pwa.sh holds that case.
$M terminate com.android.chrome >/dev/null 2>&1 || true
$M open "$URL" >/dev/null
sleep 3
[ "$($M current)" = com.android.chrome ] || fail "Chrome is not in front after opening $URL"
CTX=$($M contexts | grep -F "$URL" | awk '{print $1}' | head -1)
[ -n "$CTX" ] || fail "the page did not appear as a context (is the emulator online?): $($M contexts | head -3)"
$M contexts | grep -F "$URL" | grep -q "Example Domain" || fail "the listing does not carry the page's title"
row "listed" "$CTX, with the page's title and URL"

$M context "$CTX" >/dev/null
$M text | grep -q "This domain is for use in" || fail "the page's own text did not come back"
[ "$($M eval 'document.title')" = "Example Domain" ] || fail "eval did not answer from the page"
LINK=$($M map | grep '(link)' | head -1 | awk '{print $1}') || true
[ -n "$LINK" ] || fail "the page's link is not in the map: $($M map)"
row "read" "text, eval and map answer from the page"

$M eval '(() => { document.querySelectorAll("button").forEach(e => { if (e.textContent === "Mobium probe") e.remove() });
  const b = document.createElement("button"); b.id = "mobium-probe"; b.textContent = "Mobium probe";
  b.style.cssText = "position:fixed;left:30%;top:60%;width:40%;height:60px;font-size:20px;z-index:9";
  window.__taps = 0; b.onclick = () => { window.__taps++ }; document.body.append(b); return "ok" })()' >/dev/null
PROBE=$($M map | grep 'Mobium probe' | awk '{print $1}') || true
[ -n "$PROBE" ] || fail "the button put on the page is not in the map"
out=$($M tap "$PROBE" 2>&1) || fail "a tap in the web context was refused: $out"
[ "$($M eval 'window.__taps')" = 1 ] || fail "a tap in the web context did not reach the page: it counted $($M eval 'window.__taps')"
row "tap" "a tap on $PROBE landed: the page counted one click"

LINK=$($M map | grep '(link)' | head -1 | awk '{print $1}')
$M tap "$LINK" >/dev/null
host=""
for _ in 1 2 3 4 5 6 7 8 9 10; do
  sleep 1
  host=$($M eval 'location.host' 2>/dev/null || true)
  case "$host" in *iana.org) break ;; esac
done
case "$host" in *iana.org) ;; *) fail "the page's link did not navigate: the page is at \"$host\"" ;; esac
row "navigate" "a tap on the page's link took it to $host"

$M context NATIVE_APP >/dev/null
echo PASS
