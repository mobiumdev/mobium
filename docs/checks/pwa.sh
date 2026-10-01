#!/bin/sh
# A progressive web app, end to end: Squoosh (https://squoosh.app), installed
# to the home screen and launched from its icon, held to what the page itself
# says.
#
#   docs/checks/pwa.sh <emulator-serial | simulator-udid>
#
# Needs the network. If no Squoosh icon is on the home screen the check
# installs one — through Chrome's menu on Android, Safari's share sheet on
# iOS, since Mobium does not install a PWA itself — and leaves it there, so a
# second run launches the same icon.
#
# - Launched from its icon, the app runs standalone: Chrome's WebappActivity
#   on Android, com.apple.webapp on iOS; the page reports display-mode
#   standalone and an active service worker, and on iOS
#   navigator.standalone.
# - Its context is found by URL, the manifest's start_url with
#   utm_source=launcher — on Android it is named for Chrome, on iOS for
#   SafariViewService, and on neither for the app.
# - Android: launched while Chrome is running, Chrome stops reporting the
#   app's WebView to accessibility within about five seconds (CHALLENGES
#   200), and nothing native can then say where the page is. So a tap on a
#   ref in the web context either lands — a button put on the page counts it
#   — or is refused with that explanation and the page counts nothing; never
#   a tap reported as made that went somewhere else. Which one is printed.
# - iOS: the same tap is refused, because the page's position in its host
#   cannot be known; from NATIVE_APP the button is in the tree, and a tap
#   there lands — the page counts it. docs/APP-TYPES.md, "Progressive web
#   apps".
#
# A real phone is refused: the check installs an app on it.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <emulator-serial | simulator-udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
case "$DEV" in
  emulator-*) PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
  ????????-????-????-????-????????????)
    xcrun simctl list devices 2>/dev/null | grep -q "$DEV" ||
      { echo "$DEV is not a simulator: this check installs an app, and a phone is somebody's" >&2; exit 2; }
    PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *) echo "$DEV is not an emulator or a simulator: this check installs an app, and a phone is somebody's" >&2; exit 2 ;;
esac
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
  $M open "https://example.com/?pwa=$(date +%s)" >/dev/null; sleep 3
fi
ICON=$(icon)
if [ -z "$ICON" ]; then
  $M open https://squoosh.app >/dev/null; sleep 5
  if [ "$PLATFORM" = android ]; then
    $M tap testid=com.android.chrome:id/menu_button >/dev/null; sleep 2
    $M tap "text=Add to Home screen" >/dev/null 2>&1 || $M tap "text=Install app" >/dev/null
    sleep 3
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
  ICON=$(icon)
  [ -n "$ICON" ] || fail "Squoosh is not on the home screen after installing it"
  row "installed" "Squoosh added to the home screen"
fi

# On Android, Chrome is running when the icon is tapped, which is how an
# install leaves it and the case CHALLENGES 200 is about. A launch with
# Chrome's process gone cannot be arranged from here: the launcher hides
# Chrome's web-app icons while Chrome is not running.
$M tap "$ICON" >/dev/null; sleep 5
front=$($M current)
if [ "$PLATFORM" = android ]; then
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
[ -n "$CTX" ] || fail "no visible page at Squoosh's start URL: $($M contexts | head -4)"
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

echo PASS
