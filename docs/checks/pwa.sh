#!/bin/sh
# A progressive web app, end to end: Squoosh (https://squoosh.app), installed
# to the home screen and launched from its icon, held to what the page itself
# says.
#
#   docs/checks/pwa.sh <emulator-serial | android-serial | simulator-udid>
#
# Needs the network. If Squoosh is not installed the check installs it —
# through Chrome's menu on Android, Safari's share sheet on iOS, since Mobium
# does not install a PWA itself. On an emulator, with no Play services,
# Chrome makes a launcher shortcut, launched from its icon and left there so
# a second run reuses it. On a phone with Play services Chrome mints a WebAPK,
# a real package: the check launches it by its package name, holds
# app_launch to saying it is an installed web app (CHALLENGES 202), and
# uninstalls it afterwards if the check installed it. On a phone it prints no
# listing — a context list there is somebody's tabs — and closes the tabs it
# opened.
#
# - Launched from its icon, the app runs standalone: Chrome's WebappActivity
#   on Android, com.apple.webapp on iOS; the page reports display-mode
#   standalone and an active service worker, and on iOS
#   navigator.standalone.
# - Its context is found by URL, the manifest's start_url with
#   utm_source=launcher — on Android it is named for Chrome, on iOS for
#   SafariViewService, and on neither for the app.
# - Android: a tap on a ref in the web context lands — a button put on the
#   page counts it. On the emulator's shortcut, launched while Chrome is
#   running, Chrome stops reporting the app's WebView to accessibility within
#   about five seconds (CHALLENGES 200) and the tap is refused with that
#   explanation, the page counting nothing; that is accepted there, and said.
#   A WebAPK on the Pixel 8 Pro kept its WebView, so on a phone the tap must
#   land.
# - iOS: the same tap is refused, because the page's position in its host
#   cannot be known; from NATIVE_APP the button is in the tree, and a tap
#   there lands — the page counts it. docs/APP-TYPES.md, "Progressive web
#   apps".
#
# A real iPhone is refused: the check would add a web app to its home
# screen, and nothing outside can take it off again.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <emulator-serial | android-serial | simulator-udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
case "$DEV" in
  emulator-*) PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
  ????????-????-????-????-????????????)
    xcrun simctl list devices 2>/dev/null | grep -q "$DEV" ||
      { echo "$DEV is not a simulator: this check would add a web app to an iPhone's home screen" >&2; exit 2; }
    PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  ????????-????????????????)
    echo "$DEV is an iPhone: this check would add a web app to its home screen" >&2; exit 2 ;;
  *) PLATFORM=android; PHONE=1; M="$ROOT/bin/mobium --device $DEV" ;;
esac
# closeTabs closes the pages in the phone's Chrome whose URL matches $1, a
# Python regular expression, through Chrome's own DevTools endpoint: Mobium has
# no tool that closes a tab. Only this run's pages match, and nothing about the
# others is read beyond their URL or printed.
closeTabs() {
  port=$(adb -s "$DEV" forward tcp:0 localabstract:chrome_devtools_remote) || return 0
  curl -s "http://127.0.0.1:$port/json/list" | python3 -c '
import json, re, sys
pat = re.compile(sys.argv[1])
for t in json.load(sys.stdin):
    if t.get("type") == "page" and pat.search(t.get("url", "")):
        print(t["id"])' "$1" | while read -r id; do
    curl -s -X PUT "http://127.0.0.1:$port/json/close/$id" >/dev/null
  done
  adb -s "$DEV" forward --remove "tcp:$port" >/dev/null 2>&1 || true
}
RUN=$(date +%s)
# listing prints what mobium listed, except on a phone, where it is somebody's.
listing() { if [ -n "$PHONE" ]; then echo "(not printed on a phone)"; else $M contexts | head -4; fi; }
# squooshApk prints the package of Squoosh's WebAPK, if Chrome minted one: a
# WebAPK declares its scope's host in its intent filters.
squooshApk() {
  for p in $(adb -s "$DEV" shell pm list packages | sed -n 's/^package:\(org\.chromium\.webapk\..*\)/\1/p' | tr -d '\r'); do
    adb -s "$DEV" shell dumpsys package "$p" | grep -q "squoosh.app" && { echo "$p"; return; }
  done
}
fail() { $M context NATIVE_APP >/dev/null 2>&1 || true; echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
echo "--- $DEV ($PLATFORM)"
$M context NATIVE_APP >/dev/null 2>&1 || true

# icon prints the ref of the first Squoosh icon on the home screen, if any.
# The launcher is read more than once: just after a boot, or straight after
# leaving Chrome, its first map can come before its icons — a miss there
# installed a second copy, and then a third.
icon() {
  $M press home >/dev/null
  r=""
  for _ in 1 2 3; do
    sleep 2
    r=$($M map | grep -m1 'Squoosh (button)' | awk '{print $1}') || true
    [ -n "$r" ] && break
    if [ "$PLATFORM" = ios ]; then
      $M swipe left >/dev/null; sleep 2
      r=$($M map | grep -m1 'Squoosh (button)' | awk '{print $1}') || true
      [ -n "$r" ] && break
      $M press home >/dev/null
    fi
  done
  echo "$r"
}

# On Android the launcher shows Chrome's web-app icons only while Chrome is
# running — after a boot they are hidden, and the install path would find
# an installed app and offer a plain shortcut instead — so Chrome is started
# first, with a tab. That is also how the launch below is a warm one.
if [ "$PLATFORM" = android ]; then
  $M open "https://example.com/?pwa=$RUN" >/dev/null; sleep 3
fi
APK=""; INSTALLED=""
[ "$PLATFORM" = android ] && APK=$(squooshApk)
ICON=""
[ -z "$APK" ] && ICON=$(icon)
if [ -z "$APK" ] && [ -z "$ICON" ]; then
  $M open https://squoosh.app >/dev/null; sleep 5
  if [ "$PLATFORM" = android ]; then
    $M tap testid=com.android.chrome:id/menu_button >/dev/null; sleep 2
    $M tap "text=Install and create shortcut" >/dev/null 2>&1 || $M tap "text=Add to Home screen" >/dev/null 2>&1 ||
      $M tap "text=Install app" >/dev/null
    sleep 3
    # Chrome 154 asks first which of the two: a web app, or a shortcut that
    # opens in a tab.
    if $M map | grep -q "Create shortcut" && $M map | grep -q "^@e[0-9]* Install (button)"; then
      $M tap "text=Install" >/dev/null; sleep 3
    fi
    if $M map | grep -q "Create shortcut"; then
      # Chrome has the app installed and offers only a plain shortcut, which
      # opens a tab. Without Play services Chrome installs a web app as a
      # launcher shortcut, not an APK, and after a cold boot the launcher
      # showed none of them while Chrome still listed six.
      $M tap "text=Cancel" >/dev/null 2>&1 || true
      fail "Chrome has Squoosh installed but the home screen shows no icon for it, and Chrome will only add a plain shortcut — wipe the emulator's data (emulator -avd <name> -wipe-data) to install it again"
    fi
    $M tap "text=Install" >/dev/null; sleep 3
    $M tap "text=Add to home screen" >/dev/null 2>&1 || true   # the launcher's own confirmation, when it asks
    # With Play services the install is a WebAPK, minted and installed in the
    # background: twelve seconds on the Pixel 8 Pro.
    for _ in $(seq 1 20); do APK=$(squooshApk); [ -n "$APK" ] && break; [ -z "$PHONE" ] && break; sleep 3; done
    [ -n "$APK" ] && INSTALLED=1
  else
    # The menu is asked for until it opens: on a simulator that had never
    # opened the page, a tap on More while it was still loading opened
    # nothing.
    for _ in 1 2 3; do
      $M tap 'label=More,role=button' >/dev/null
      $M wait 'label=Share,role=button' --timeout 5s >/dev/null 2>&1 && break
    done
    $M tap 'label=Share,role=button' >/dev/null || fail "Safari's menu did not open"; sleep 3
    $M tap 'label=Add to Home Screen,role=button' >/dev/null 2>&1 ||
      { $M tap 'label=View More,role=button' >/dev/null; sleep 2; $M tap 'label=Add to Home Screen,role=button' >/dev/null; }
    sleep 3
    $M map | grep -q 'Open as Web App (switch, checked)' || fail "the sheet would not add Squoosh as a web app"
    $M tap 'label=Add,role=button' >/dev/null; sleep 3
  fi
  if [ -n "$APK" ]; then
    row "installed" "Squoosh, as a WebAPK: $APK"
  else
    ICON=$(icon)
    [ -n "$ICON" ] || fail "Squoosh is not on the home screen after installing it"
    row "installed" "Squoosh added to the home screen"
  fi
fi
cleanup() {
  [ -n "$PHONE" ] && closeTabs "example\\.com/\\?pwa=$RUN|squoosh\\.app/$"
  [ -n "$INSTALLED" ] || return 0
  $M context NATIVE_APP >/dev/null 2>&1 || true
  adb -s "$DEV" uninstall "$APK" >/dev/null 2>&1 || true
  [ -z "$(squooshApk)" ] || { echo "FAIL: $APK, which this check installed, is still installed" >&2; exit 1; }
  row "removed" "the WebAPK this check installed is uninstalled"
}
fail() { $M context NATIVE_APP >/dev/null 2>&1 || true; echo "FAIL: $*" >&2; cleanup; exit 1; }

# On Android, Chrome is running when the icon is tapped, which is how an
# install leaves it and the case CHALLENGES 200 is about. A launch with
# Chrome's process gone cannot be arranged from here: the launcher hides
# Chrome's web-app icons while Chrome is not running.
if [ -n "$APK" ]; then
  out=$($M launch "$APK" 2>&1) || fail "launching $APK failed: $out"
  echo "$out" | grep -q "an installed web app" || fail "the launch did not say it is an installed web app: $out"
  adb -s "$DEV" shell dumpsys activity activities | grep -m1 topResumedActivity | grep -q WebApkActivity ||
    fail "$APK is not in front in Chrome's web-app activity"
  row "launched" "by its package, and app_launch says it is a web app"
else
  $M tap "$ICON" >/dev/null; sleep 5
fi
front=$($M current)
if [ -n "$APK" ]; then
  :
elif [ "$PLATFORM" = android ]; then
  [ "$front" = com.android.chrome ] || fail "the icon opened $front, not Chrome"
  adb -s "$DEV" shell dumpsys activity activities | grep -m1 topResumedActivity | grep -q WebappActivity ||
    fail "the icon opened Chrome in a tab, not as a web app"
  row "launched" "from its icon, in Chrome's WebappActivity"
else
  [ "$front" = com.apple.webapp ] || fail "the icon opened $front, not a web app"
  row "launched" "from its icon, as com.apple.webapp"
fi

# Several launches leave several pages at the start URL; the app in front is
# the one that is visible.
CTX=""
for c in $($M contexts | grep -F 'squoosh.app/?utm_medium=PWA&utm_source=launcher' | awk '{print $1}'); do
  $M context "$c" >/dev/null 2>&1 || continue
  if [ "$($M eval 'document.visibilityState' 2>/dev/null)" = visible ]; then CTX=$c; break; fi
done
[ -n "$CTX" ] || fail "no visible page at Squoosh's start URL: $(listing)"
[ "$($M eval 'matchMedia("(display-mode: standalone)").matches')" = true ] || fail "the page is not running standalone"
# A service worker registered for the app and active: what makes it run
# offline. Whether it controls this very page is the browser's business — a
# page Chrome reloads into the web app is not controlled until the next load.
sw=$($M eval '(async () => (await navigator.serviceWorker.getRegistrations()).some(r => r.active))()' 2>/dev/null || true)
[ "$sw" = true ] || fail "no active service worker is registered for the app"
if [ "$PLATFORM" = ios ]; then
  [ "$($M eval 'navigator.standalone')" = true ] || fail "navigator.standalone is not true"
fi
row "standalone" "$CTX, found by its start URL; a service worker active"

$M map | grep -q 'Paste' || fail "the page's own controls are not in the map"
$M eval '(() => { document.querySelectorAll("button").forEach(e => { if (e.textContent === "Mobium probe") e.remove() });
  const b = document.createElement("button"); b.textContent = "Mobium probe";
  b.style.cssText = "position:fixed;left:30%;top:60%;width:40%;height:60px;font-size:20px;z-index:9";
  window.__taps = 0; b.onclick = () => { window.__taps++ }; document.body.append(b); return "ok" })()' >/dev/null
PROBE=$($M map | grep 'Mobium probe' | awk '{print $1}') || true
[ -n "$PROBE" ] || fail "the button put on the page is not in the map"

if [ "$PLATFORM" = android ]; then
  # Long enough after the launch for Chrome to stop reporting the WebView,
  # if it is going to: then the tap must be refused and say why, and the
  # page must count nothing. A tap that lands is right too, and is reported.
  sleep 5
  if out=$($M tap "$PROBE" 2>&1); then
    [ "$($M eval 'window.__taps')" = 1 ] || fail "a tap in the web context was reported but did not reach the page"
    row "tap" "a tap on $PROBE in the web context landed: the page counted one"
  elif [ -n "$APK" ]; then
    fail "a tap in the WebAPK's page was refused: $out"
  else
    echo "$out" | grep -q "stopped reporting its WebView" || fail "a tap in the web context was refused without saying why: $out"
    [ "$($M eval 'window.__taps')" = 0 ] || fail "the refused tap reached the page anyway"
    row "refused" "Chrome stopped reporting the WebView; the tap says so (CHALLENGES 200)"
  fi
else
  if $M tap "$PROBE" >/dev/null 2>&1; then fail "a tap in the web context was not refused"; fi
  $M map 2>&1 | grep -q "cannot be tapped" || fail "the map did not say why the page cannot be tapped"
  [ "$($M eval 'window.__taps')" = 0 ] || fail "the refused tap reached the page anyway"
  row "refused" "a tap in the web context, and the map says why"
  $M context NATIVE_APP >/dev/null
  $M tap 'label=Mobium probe' >/dev/null
  $M context "$CTX" >/dev/null
  [ "$($M eval 'window.__taps')" = 1 ] || fail "a tap from NATIVE_APP did not reach the page"
  row "tap" "the page's button from NATIVE_APP landed: the page counted one"
fi

$M context NATIVE_APP >/dev/null
cleanup
echo PASS
