#!/bin/sh
# A PWA from the Play Store, end to end: a Trusted Web Activity — an Android
# package whose screen is Chrome, drawing the site full screen in a custom
# tab, in the package's own task. docs/APP-TYPES.md, "Progressive web apps".
#
#   docs/checks/twa.sh <android-serial> [package]
#
# The package defaults to OYO Lite, com.oyo.consumerlite, the TWA in Google's
# own case study, installed from the Play Store by hand: Mobium does not
# install from Play. It is checked to be a TWA — Google's androidbrowserhelper
# launcher at the root of its task — before anything else.
#
# - app_launch says it launched a Trusted Web Activity drawn by Chrome, not
#   that Chrome is in front (CHALLENGES 205).
# - Its page is a WEBVIEW_com.android.chrome context, found by the site's
#   host; it runs standalone, and a tap on a ref lands — a button put on the
#   page counts it, and is taken off again.
# - A tap on one of the page's links navigates; back then returns to the
#   page before it and says the TWA is still in front, and a second back
#   leaves it and says so. The link is tapped, not followed with eval: Chrome
#   skips, on back, history entries a page made without a user's gesture, and
#   a navigation run through eval has none.
#
# Nothing is read beyond the TWA's own page and the app in front.
set -e
DEV="$1"; PKG="${2:-com.oyo.consumerlite}"
if [ -z "$DEV" ]; then echo "usage: $0 <android-serial> [package]" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
M="$ROOT/bin/mobium --device $DEV"
fail() { $M context NATIVE_APP >/dev/null 2>&1 || true; echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-60s ok\n' "$1" "$2"; }
echo "--- $DEV, $PKG"

adb -s "$DEV" shell pm list packages | grep -q "^package:$PKG\$" ||
  fail "$PKG is not installed — install it from the Play Store first"
$M context NATIVE_APP >/dev/null 2>&1 || true
$M terminate "$PKG" >/dev/null 2>&1 || true
out=$($M launch "$PKG")
echo "$out" | grep -q "a Trusted Web Activity" || fail "the launch did not say it is a TWA: $out"
adb -s "$DEV" shell dumpsys activity activities | grep -m1 "Hist.*$PKG/" | grep -q "androidbrowserhelper.trusted" ||
  fail "$PKG's task is not rooted in the TWA launcher"
row "launched" "a Trusted Web Activity, drawn by Chrome in its own task"

# page attaches to the TWA's visible page and prints its context — once HOST
# is known, only a page on the site the package opened.
HOST=""
page() {
  for c in $($M contexts | awk '/^WEBVIEW_com\.android\.chrome/ { print $1 }'); do
    line=$($M contexts | awk -v id="$c" '$1 == id')
    [ -n "$HOST" ] && ! echo "$line" | grep -q "$HOST" && continue
    $M context "$c" >/dev/null 2>&1 || continue
    if [ "$($M eval 'document.visibilityState' 2>/dev/null)" = visible ]; then echo "$c"; return; fi
  done
}
sleep 3
CTX=$(page)
[ -n "$CTX" ] || fail "no visible Chrome page while $PKG is in front"
HOST=$($M eval 'location.host')
[ "$($M eval 'matchMedia("(display-mode: standalone)").matches')" = true ] || fail "the page is not standalone"
row "page" "$CTX, $HOST, standalone"

$M eval '(() => { document.querySelectorAll("button").forEach(e => { if (e.textContent === "Mobium probe") e.remove() });
  const b = document.createElement("button"); b.textContent = "Mobium probe";
  b.style.cssText = "position:fixed;left:30%;top:60%;width:40%;height:60px;font-size:20px;z-index:99999";
  window.__taps = 0; b.onclick = () => { window.__taps++ }; document.body.append(b); return "ok" })()' >/dev/null
PROBE=$($M map | grep 'Mobium probe' | awk '{print $1}') || true
[ -n "$PROBE" ] || fail "the button put on the page is not in the map"
out=$($M tap "$PROBE" 2>&1) || fail "a tap in the TWA was refused: $out"
[ "$($M eval 'window.__taps')" = 1 ] || fail "a tap in the TWA did not reach the page"
$M eval 'document.querySelectorAll("button").forEach(e => { if (e.textContent === "Mobium probe") e.remove() }); 1' >/dev/null
row "tap" "a tap on $PROBE landed: the page counted one"

START=$($M eval 'location.pathname + location.search')
moved=""
for ref in $($M map | grep '(link)' | awk '{print $1}' | head -6); do
  $M tap "$ref" >/dev/null 2>&1 || continue
  sleep 4
  CTX=$(page)
  [ -n "$CTX" ] || continue
  now=$($M eval 'location.pathname + location.search')
  if [ "$now" != "$START" ]; then moved=$now; break; fi
done
[ -n "$moved" ] || fail "none of the page's first links navigated"
$M context NATIVE_APP >/dev/null
out=$($M press back)
echo "$out" | grep -q "$PKG is still in the foreground" || fail "back inside the TWA: $out"
CTX=$(page)
[ "$($M eval 'location.pathname + location.search')" = "$START" ] || fail "back did not return to the page before"
$M context NATIVE_APP >/dev/null
row "back" "a link followed, then back to the page before, $PKG in front"

out=$($M press back)
echo "$out" | grep -q "it left $PKG" || fail "back out of the TWA: $out"
row "back" "from its first page, and says it left $PKG"
echo PASS
