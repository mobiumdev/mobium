#!/bin/sh
# A genuine hybrid app on iOS: an embedded WKWebView, driven end to end.
#
# Until this existed, every iOS WebView run went through mobile Safari, and
# Safari is the pathological case — its WebView element covers the whole
# window, so `NewFrame` refuses to compute coordinates inside it (CHALLENGES
# 47). The success path had therefore never run on iOS at all. Neither had
# two live WebViews in one application: what was recorded as "done by
# accident" was two *tabs* in one browser, which is a different structure.
#
# It could not be closed with an app from the store. `simctl install` takes a
# simulator-SDK bundle and App Store apps ship device-signed arm64 IPAs, so on
# a simulator the only third-party apps available are ones compiled from
# source. And on iOS 16.4 and later a WKWebView is invisible to Remote Web
# Inspector unless the app itself sets `isInspectable` — measured here, it
# cannot be forced from outside, so an app that does not opt in is unreachable
# whatever mobium does. The app under test has to opt in, which means the app
# under test has to be ours.
#
# Android has the same shape and is older: a WebView publishes nothing to CDP
# unless the app called `WebView.setWebContentsDebuggingEnabled(true)`. One
# React Native prop, `webviewDebuggingEnabled`, sets both.
#
# MobiumApp is that app: React Native, `webviewDebuggingEnabled` on every
# WebView, pages shipped inline rather than fetched. It lives outside this
# repository because it needs Node, Gradle and CocoaPods, and mobium's
# no-runtime-dependencies property is load-bearing. See docs/decisions/0004.
#
# The same script drives both platforms. Only the backend differs, which is the
# whole point of where the driver seam was put: `map`, `contexts`, `context`
# and `text` are the same calls, and the transport underneath is CDP on
# Android and Remote Web Inspector on iOS.
#
#   docs/checks/mobium-app.sh <udid|serial>
#
# Get it and build it first:
#   git clone https://github.com/mobiumdev/mobium-app && cd mobium-app && npm install
#   npx expo run:ios --configuration Release --device <udid>
#
# On a real iPhone, build it signed for your own team -- the same team Mobium
# signs WebDriverAgent with, from Xcode's Apple ID -- then let mobium install it:
#   xcodebuild -workspace ios/MobiumApp.xcworkspace -scheme MobiumApp \
#     -configuration Release -destination id=<udid> -allowProvisioningUpdates \
#     DEVELOPMENT_TEAM=<team> CODE_SIGN_STYLE=Automatic -derivedDataPath build/device
#   mobium --backend webdriveragent --device <udid> install \
#     build/device/Build/Products/Release-iphoneos/MobiumApp.app
# and to rerun on a phone from a clean state, pass that same bundle:
#   MOBIUMAPP_BUNDLE=<path>/MobiumApp.app docs/checks/mobium-app.sh <udid>
#   npx expo run:android --variant release
set -e
DEV="${1:?usage: mobium-app.sh <udid|serial>}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# An iOS simulator is named by a UUID; an Android device never is. iOS is not
# inferred from the serial anywhere in mobium, so the backend is named here.
# A real iPhone's UDID is two groups, 00008120-0001234567890ABC, so it needs a
# pattern of its own -- without one, a phone was taken for an Android serial.
#
# PHONE is set on a real iPhone. It drives every screen the simulator does,
# WebViews included — reached through usbmuxd and lockdown rather than a
# socket on the Mac — and has no simulated location or clipboard: a simulator
# gets those from simctl and a phone has no equivalent, so those sections
# assert the refusal instead. A section that is skipped says so where it runs.
PHONE=""
case "$DEV" in
  [0-9A-Fa-f]*-[0-9A-Fa-f]*-[0-9A-Fa-f]*-[0-9A-Fa-f]*-[0-9A-Fa-f]*)
    BACKEND="--backend webdriveragent"; PLATFORM=ios ;;
  [0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f]-[0-9A-Fa-f]*)
    BACKEND="--backend webdriveragent"; PLATFORM=ios; PHONE=1 ;;
  *)
    BACKEND=""; PLATFORM=android ;;
esac
# A real Android phone can set a simulated position, so its location section
# runs — but what the app shows once the simulated one is cleared is the
# phone's own position, somebody's whereabouts, so nothing observed there is
# printed. Measured on a Pixel 8 Pro, where the check printed it once.
ANDROID_PHONE=""
[ "$PLATFORM" = android ] && case "$DEV" in emulator-*) ;; *) ANDROID_PHONE=1 ;; esac
# say <coords>: coordinates for a message, or on a phone only whether there were any.
say() { if [ -n "$ANDROID_PHONE" ]; then [ -n "$1" ] && echo "a position (not printed on a phone)" || echo "none"; else echo "$1"; fi; }
M="$ROOT/bin/mobium $BACKEND --device $DEV"
APP=dev.mobium.mobiumapp
fail() { echo "FAIL: $*" >&2; exit 1; }

# appContexts lists the WebView contexts that belong to one app — WEBVIEW_<id>,
# or WEBVIEW_<id>_<n> when it has several — and nothing else. `contexts`
# reports every inspectable page on the device, not only the app in front: on
# a real iPhone with Wikipedia in the foreground it listed Safari's page. So
# "the first WEBVIEW_ line" can be another app's, and a count of them can pass
# with one of ours and one of Safari's.
appContexts() { $M contexts | awk -v id="WEBVIEW_$1" \
  '$1 == id || (index($1, id "_") == 1 && substr($1, length(id) + 2) ~ /^[0-9]+$/) { print $1 }'; }

# Resolve a ref by label rather than hand-writing a locator. React Native
# repeats an accessibility identifier across the nested views it renders for
# one component — measured at 14 nodes for one button — so `label=` and
# `testid=` written by hand are ambiguous and mobium refuses them. The
# locators `map` derives are unique; these are what a caller should use.
ref() { $M map --json 2>&1 | python3 -c "
import json,sys
raw=sys.stdin.read()
try:
    d=json.loads(raw)
except ValueError:
    sys.exit('ref $1: map gave no JSON: ' + raw.strip()[:300])
if 'elements' not in d:
    # A failed map, said as itself rather than as a KeyError.
    sys.exit('ref $1: map failed: ' + (d.get('message') or d.get('error') or raw.strip())[:300])
for e in d['elements']:
    if (e.get('label') or '')=='$1': print(e['ref']); break
"; }

# Clear a dialog left over from a run that died. An unanswered system modal
# blocks WebDriverAgent from launching at all -- the runner cannot start behind
# it -- so an aborted run leaves the simulator unusable and the next one fails
# with "WebDriverAgent did not become ready", which names the wrong cause
# entirely. Cheap to do, and it costs nothing when there is no dialog, since
# finding none is an answer rather than an error. CHALLENGES 62.
#
# It runs after the daemon stop below, because it needs a session of its own.

# Start from no daemon. A Remote Web Inspector connection is held for the life
# of a session and a page's target belongs to one debugger at a time
# (CHALLENGES 45, 46), so a previous run's session can leave the next one
# reporting no attachable WebView at all -- observed running this twice in a
# row. ios-webview.sh has done this from the beginning; this script had not.
"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true

# readText reads the screen, retrying a read that failed rather than a screen
# that is wrong.
#
# Those are different things and conflating them cost two wrong fixes here. A
# read during an animation fails with `mSealed` (CHALLENGES 51) and returns
# nothing, so a `grep` over it finds nothing and the assertion reports the
# state as absent -- when the state was in fact correct and only the read was
# unlucky. Measured: the clipboard assertion below failed this way while the
# pasted text was plainly in the field, confirmed by mapping the same screen a
# moment later.
#
# So retry until the read produces something, then let the caller assert on it
# once. Retrying until the *expected string* appears would be the wrong shape:
# it could never express "this must be absent", which is what the interruption
# check needs.
readText() {
  i=0
  while [ $i -lt 20 ]; do
    out=$($M text 2>/dev/null)
    if [ -n "$out" ]; then
      printf '%s\n' "$out"
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done
  fail "the screen could not be read at all${1:+ after $1}"
}

# settle waits until the hierarchy can actually be read.
#
# A screen that is still animating fails the read with `mSealed` (CHALLENGES
# 51). Mobium retries that three times and it is not always enough: the pager
# below, given fifteen swipes, outlasts the retry budget. Two earlier versions
# of this slept a guessed number of seconds -- three, then six -- and both were
# wrong, because the screen is ready when it can be read and not when a clock
# says so.
settle() {
  i=0
  while [ $i -lt 20 ]; do
    $M map >/dev/null 2>&1 && return 0
    sleep 1
    i=$((i + 1))
  done
  fail "the screen never stopped moving${1:+ after $1}"
}

$M alert dismiss >/dev/null 2>&1 || true

echo "--- $DEV ($PLATFORM${PHONE:+, real iPhone})"
# A listing that fails is not an empty one: a phone gone from its cable
# answered "not installed" here while the real error was the device.
APPS=$($M apps 2>&1) || fail "could not list the apps: $APPS"
echo "$APPS" | grep -q "$APP" || fail "$APP is not installed — build it first (see the header)"

# A phone cannot reset a permission from outside, and an unanswered prompt left
# by a run that died is shown again the next time the app comes forward --
# over the home screen, where it hides every button this check taps. The
# only reset a phone has is reinstalling, so given the build, this does that.
if [ -n "$PHONE" ] && [ -n "$MOBIUMAPP_BUNDLE" ]; then
  $M uninstall $APP >/dev/null
  $M install "$MOBIUMAPP_BUNDLE" >/dev/null
  echo "    reinstall      a fresh install, so the permission is unanswered"
elif [ -n "$PHONE" ]; then
  echo "    reinstall      SKIPPED -- set MOBIUMAPP_BUNDLE to the .app to rerun from a clean state"
fi

$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
sleep 2
$M map | grep -q 'WebViews' || fail "the home screen did not map"
echo "    home           mapped                                          ok"

# --- an embedded WKWebView that opts into inspection -----------------------
# The WebView cases are one home entry that opens a list of them, as the
# gestures are; Back from a case returns to that list.
$M tap "$(ref 'WebViews')" >/dev/null; sleep 1
$M tap "$(ref 'Plain page')" >/dev/null; sleep 3
CTX=$(appContexts "$APP" | head -1)
# Say why. `contexts` fails for reasons of its own -- a phone off its cable
# cannot reach the web inspector at all -- and only when it succeeds and lists
# nothing for this app is the app not opting into inspection the answer.
[ -n "$CTX" ] || fail "no WebView context: $($M contexts 2>&1 | grep -m1 "^error:" || echo "mobium listed none for this app")"
echo "    contexts       $CTX"

$M context "$CTX" >/dev/null
# Load-bearing: a value the page itself computed. Everything before this could
# pass with the target wrapping broken.
$M text | grep -q 'quick brown fox' || fail "the page's own text did not come back"
echo "    read           page text through Evaluate                      ok"
$M map | grep -q 'Learn more' || fail "the page's link is not in the map"
echo "    map            found the page's link                           ok"

# --- where "Learn more" actually goes ---------------------------------------
# Asserted from inside the page rather than by following it. The href is a
# property of the app and is true whether or not anything is served at the
# other end; the navigation depends on DNS, a certificate and somebody keeping
# a server running, none of which this repository controls.
HREF=$($M eval "document.querySelector('a#link') && document.querySelector('a#link').href" 2>/dev/null | tr -d '"')
case "$HREF" in
  https://github.com/mobiumdev|https://github.com/mobiumdev/) ;;
  *) fail "the Learn more link points at '$HREF', not https://github.com/mobiumdev" ;;
esac
TAG=$($M eval "document.querySelector('a#link').tagName" 2>/dev/null | tr -d '"')
[ "$TAG" = "A" ] || fail "the Learn more element is a <$TAG>, so it is not a link"
echo "    link target    an <a> to https://github.com/mobiumdev          ok"

# The assertion this whole app exists for. In Safari this refuses, correctly,
# because the host element is not the content. Here the WebView's frame equals
# its content, so NewFrame must produce a scale instead.
LINK=$($M map --json 2>/dev/null | python3 -c "
import json,sys
for e in json.load(sys.stdin)['elements']:
    if 'Learn more' in (e.get('label') or ''): print(e['ref']); break
")
$M tap "$LINK" >/dev/null || fail "a tap inside an embedded WebView was refused"
echo "    tap-in-web     coordinates computed, not refused               ok"

# Following the link needs the network and a site at the other end, so it is
# opt-in -- the same rule the Go network tests follow. It is skipped loudly
# rather than quietly: a check that says nothing when it did nothing reads
# exactly like a check that passed.
if [ -n "$MOBIUM_NETWORK_TESTS" ]; then
  # Wait for the navigation rather than sleeping at it: a real page over a
  # real network takes as long as it takes, and a fixed sleep is a guess that
  # is either wasteful or wrong.
  HOST=""
  i=0
  while [ $i -lt 20 ]; do
    HOST=$($M eval "location.host" 2>/dev/null | tr -d '"')
    [ "$HOST" = "github.com" ] && break
    sleep 1
    i=$((i + 1))
  done
  if [ "$HOST" != "github.com" ]; then
    # An empty host means the page never navigated at all, which is what a
    # failed load looks like from inside the WebView -- so say that, rather
    # than reporting a bare mismatch and sending the next person to debug the
    # app when the problem is the network or the site.
    fail "tapping Learn more landed on '$HOST', not github.com. An empty host means \
the page never navigated: check the device has a network and that the target \
still serves over https"
  fi
  PATHNAME=$($M eval "location.pathname" 2>/dev/null | tr -d '"')
  [ "$PATHNAME" = "/mobiumdev" ] || fail "landed on github.com$PATHNAME, not /mobiumdev"
  # The title is asserted on the handle, not the display name: one is part of
  # the URL and stable, the other is a profile field somebody can edit.
  TITLE=$($M eval "document.title" 2>/dev/null | tr -d '"')
  case "$TITLE" in
    *mobiumdev*) ;;
    *) fail "the page title is \"$TITLE\", which does not name mobiumdev" ;;
  esac
  echo "    followed       landed on $HOST$PATHNAME, titled \"$TITLE\""
else
  echo "    followed       skipped — set MOBIUM_NETWORK_TESTS=1 to follow the link"
fi

$M context NATIVE_APP >/dev/null
$M tap "$(ref 'Back')" >/dev/null; sleep 2

# --- two live WebViews in one application ----------------------------------
$M tap "$(ref 'Two WebViews')" >/dev/null; sleep 3
N=$(appContexts "$APP" | wc -l | tr -d ' ')
[ "$N" -ge 2 ] || fail "expected 2 WebView contexts in one app, got $N"
echo "    dual           $N live contexts in one app                      ok"
$M tap "$(ref 'Back')" >/dev/null; sleep 2
$M tap "$(ref 'Back')" >/dev/null; sleep 2   # the list, then home

# --- a password field, on iOS ----------------------------------------------
# CHALLENGES 43 was found on Android, where the typed value lands in the
# node's `text` and so was printed in plaintext. iOS marks the field
# differently — XCUIElementTypeSecureTextField — so running this on both is
# checking one rule against two quite different mechanisms.
$M tap "$(ref 'Login Demo')" >/dev/null; sleep 2
$M type "$(ref 'password')" 'hunter2' >/dev/null
$M map | grep -q 'hunter2' && fail "the password was printed in map output"
echo "    redact         password not in map output                      ok"

if [ -n "$PHONE" ]; then
  # Simulated location is simctl's, and the Location screen would show the
  # phone's real position -- which is somebody's whereabouts, not test data.
  # Not opened; the refusal is asserted instead.
  $M location --lat 48.8566 --lon 2.3522 2>&1 | grep -qi 'cannot' \
    || fail "location on a real iPhone did not refuse"
  echo "    location       SKIPPED on a real iPhone -- setting one refused"
else
# --- geolocation, observed from inside the app ------------------------------
# The reason this app has a Location screen. iOS can set a position and cannot
# report one back, so until something on the device said what it sees,
# app_location on iOS confirmed only that simctl accepted the request.
$M tap "$(ref 'Back')" >/dev/null 2>&1 || true; sleep 1
$M tap "$(ref 'Location Demo')" >/dev/null; sleep 2

# The permission prompt is worded differently on each platform and may already
# have been answered. Try both, and do not fail when neither is present.
for label in 'Allow While Using App' 'While using the app'; do
  if $M map 2>/dev/null | grep -q "$label"; then
    $M tap "label=$label" >/dev/null 2>&1 || $M tap "text=$label" >/dev/null 2>&1 || true
    break
  fi
done
sleep 3

coords() { $M text 2>/dev/null | grep -oE '[0-9-]+\.[0-9]{6},-?[0-9]+\.[0-9]{6}' | head -1; }

# A set has to reach the app. On Android a test provider emits only when set,
# so a client that started watching earlier sees nothing until the next set --
# which is why this sets after the screen is open rather than before.
# Set more than once, and wait rather than sleep. A test provider emits only
# when it is set, and adding one changes the provider set under a client that
# is already watching -- so a single set can land in the gap while the app
# re-subscribes, and a fixed sleep then reads the device's own position and
# calls it a failure. Seen exactly once, after a fresh install.
found=""
i=0
while [ $i -lt 6 ]; do
  $M location --lat 48.8566 --lon 2.3522 >/dev/null
  sleep 2
  if [ "$(coords)" = "48.856600,2.352200" ]; then found=1; break; fi
  i=$((i + 1))
done
[ -n "$found" ] || fail "the app does not see the injected position (got $(say "$(coords)"))"
echo "    location       an injected position reached the app            ok"

# --- a route, which is the only thing a single position cannot prove --------
GPX="$(mktemp -t mobium-route).gpx"
cat > "$GPX" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="mobium-check" xmlns="http://www.topografix.com/GPX/1/1">
  <trk><trkseg>
    <trkpt lat="51.5074" lon="-0.1278"/>
    <trkpt lat="51.5200" lon="-0.1500"/>
    <trkpt lat="51.5350" lon="-0.1700"/>
  </trkseg></trk>
</gpx>
EOF
$M location --gpx "$GPX" --speed 40 >/dev/null
sleep 4; A="$(coords)"
sleep 5; B="$(coords)"
[ -n "$A" ] && [ -n "$B" ] || fail "the app reported no position during a route"
[ "$A" != "$B" ] || fail "the position did not move during a route (stuck at $(say "$A"))"
echo "    route          the app saw it move, $(say "$A") -> $(say "$B")"

# Clearing stops the route. The assertion is that the position *settles*, not
# that it stays where it was: clearing reverts the device to reporting its own
# position, which is itself one change. Comparing the wrong pair of samples
# reads as a failure on correct behavior.
$M location --clear >/dev/null
sleep 5; C="$(coords)"
sleep 5; D="$(coords)"
if [ -n "$ANDROID_PHONE" ]; then
  # A phone has its own position, and once the simulated one is cleared its
  # GPS takes over: the position leaves the route, and may keep moving while
  # the fix improves — correct, and not settled. What is checked is that it
  # left the route, without saying where it went; then the screen is left at
  # once, so the phone's position is not left on display.
  case "$D" in 51.5*,-0.1*) fail "the position is still on the route after a clear" ;; esac
  $M tap "$(ref 'Back')" >/dev/null 2>&1 || true
  echo "    route clear    the position left the route (the phone's own is not printed)"
else
  [ "$C" = "$D" ] || fail "the position is still moving after a clear ($C -> $D)"
  echo "    route clear    the position settled at $C"
fi
rm -f "$GPX"
fi

# --- a horizontal pager ----------------------------------------------------
# Nothing in the hierarchy says which way a container scrolls -- Android clips
# child bounds to the parent, so every scrollable reports zero overflow in both
# axes (CHALLENGES 21). The axis therefore comes from the caller, and the two
# assertions below are what make that honest rather than merely permissive:
# the right axis reaches the card, and the wrong one fails instead of finding
# it by luck.
$M tap "$(ref 'Back')" >/dev/null 2>&1 || true; sleep 1
$M tap "$(ref 'Pager Demo')" >/dev/null; sleep 2

$M map | grep -q 'Card 1' || fail "the pager did not map"
$M map | grep -q 'Card 8' && fail "Card 8 is already on screen, so scrolling to it proves nothing"
echo "    pager          Card 8 starts off screen                        ok"

# The locator differs by platform, which is React Native's doing rather than
# this app's. One Pressable with an accessibility label is a single labeled
# node on Android and three on iOS -- the pressable, its text and a wrapper --
# so the plain label is ambiguous there and mobium refuses it rather than
# tapping the first. Narrowing by role settles it.
CARD='label=Card 8'
[ "$PLATFORM" = "ios" ] && CARD='label=Card 8,role=button'

$M scroll-to "$CARD" --direction right | grep -q 'Card 8' \
  || fail "scrolling right did not reach Card 8"
echo "    scroll right   reached Card 8                                  ok"

# Reaching it is not the same as being able to touch it: a card scrolled to the
# edge is in the hierarchy with real bounds and a center that may be off
# screen. Tapping is the proof, and the app records which card it was.
$M tap "$CARD" >/dev/null; sleep 1
$M text | grep -q 'tapped: Card 8' || fail "Card 8 was found and could not be tapped"
echo "    tap after      the card it found was actually touchable        ok"

# The wrong axis must fail. If a vertical swipe found a horizontal card, the
# direction argument would be decoration.
$M tap "$(ref 'Back')" >/dev/null 2>&1 || true; sleep 1
$M tap "$(ref 'Pager Demo')" >/dev/null; sleep 2
if $M scroll-to "$CARD" --direction down >/dev/null 2>&1; then
  fail "scrolling down found a card that is off screen to the right"
fi
echo "    wrong axis     refused rather than found by luck               ok"
# That attempt swipes the full fifteen times before giving up, and a pager with
# that much momentum is still moving when it returns.
settle "the pager"


# --- clipboard ------------------------------------------------------------
# The two platforms answer different numbers of questions, so the check does
# too. iOS reads its own clipboard back. Android cannot -- since Android 10
# only an app with focus may read it and the UiAutomator2 server has no
# activity -- so the write is verified the only way left: paste it into a field
# and read the field. That is how the write was confirmed real in the first
# place, and it is a stronger check than a read-back anyway, because it proves
# the clipboard reached an app rather than reaching the tool that set it.
# One readable map is not the same as a settled screen: the section below
# navigates away from the pager, and the read after that navigation is where
# this failed even with the wait above in place.
settle "leaving the pager"

CLIP="mobium clip $$"
if [ -n "$PHONE" ]; then
  $M clipboard 2>&1 | grep -q "real iPhone's clipboard" \
    || fail "the clipboard on a real iPhone did not refuse with its own reason"
  echo "    clipboard      SKIPPED on a real iPhone -- refused, with the phone's reason"
elif [ "$PLATFORM" = "ios" ]; then
  $M clipboard "$CLIP" >/dev/null
  $M clipboard | grep -q "$CLIP" || fail "the clipboard did not read back"
  echo "    clipboard      written and read back                          ok"
else
  $M clipboard "$CLIP" >/dev/null
  $M clipboard 2>&1 | grep -q "cannot read the clipboard" \
    || fail "Android reported a clipboard it cannot read"
  $M tap "$(ref 'Back')" >/dev/null 2>&1 || true; sleep 1
  $M tap "$(ref 'Login Demo')" >/dev/null; sleep 2
  $M tap "$(ref 'username')" >/dev/null; sleep 1
  adb -s "$DEV" shell input keyevent 279 >/dev/null 2>&1   # KEYCODE_PASTE
  sleep 2
  readText "pasting" | grep -q "$CLIP" || fail "the clipboard did not reach a field when pasted"
  echo "    clipboard      refused the read, and the write pasted in     ok"
fi

# --- an interruption, and what the app keeps afterwards ----------------------
# Prompted by a senior SDET reading about this project, who picked flaky
# element interactions as the pain worth fixing and added permission popups --
# then made the sharper point: "the tricky part is what the app keeps after the
# popup clears. The user's work can vanish even when the screen looks fine."
#
# So the popup is the easy half and this checks the other one. The dialog is a
# real system window rather than an in-app modal, which is the automation
# problem; the state question is the testing problem, and they are different.
#
# Reset first, and before typing rather than after: the permission has already
# been granted by the location section above, so without this the dialog never
# appears -- and `pm reset-permissions` can restart the app, which would
# destroy the very state being measured and pass for the wrong reason.
if [ "$PLATFORM" = "ios" ]; then
  $M reset-permissions "$APP" >/dev/null 2>&1 || true
else
  $M reset-permissions >/dev/null 2>&1 || true
fi
$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
sleep 2
$M tap "$(ref 'Interruption Demo')" >/dev/null; sleep 2

KEEP="keep-$$"
LOSE="lose-$$"
$M type 'testid=keptDraft' "$KEEP" >/dev/null
$M type 'testid=lostDraft' "$LOSE" >/dev/null
sleep 1
readText "typing" | grep -q "$KEEP" || fail "the kept draft did not take the text"
readText "typing" | grep -q "$LOSE" || fail "the lost draft did not take the text"
echo "    popup setup    both drafts hold their text                     ok"

# The dialog is the OS's window, not the app's -- `current` reports
# com.google.android.permissioncontroller or SpringBoard while one is up.
$M tap 'testid=askPermission' >/dev/null; sleep 3

# Detected through app_alert rather than by looking for a button caption. The
# W3C alert endpoints answer "is something asking?" without knowing what the
# buttons say, which matters in a project that pins apps to ja-JP on purpose:
# an English caption is a check that passes until somebody changes the device
# language.
# A phone cannot reset a permission from outside -- that is simctl's too -- so
# on a phone the dialog appears only while the app has never been answered.
# Reinstalling is what resets it there, and the failure says so rather than
# reporting a missing dialog as a broken one.
if ! $M alert | grep -qi 'a dialog is on screen'; then
  [ -n "$PHONE" ] && fail "no permission dialog -- on a real iPhone it appears only once per install; rerun with MOBIUMAPP_BUNDLE=<path to MobiumApp.app> to reinstall first"
  fail "no system permission dialog appeared after asking"
fi
echo "    popup          the system dialog is seen without reading it     ok"

# Answered by tapping the button, **not** by `alert accept`. Accept and dismiss
# answer a dialog and do not choose an outcome: measured on iOS 26.5 from a
# fresh reset, `accept` left the permission denied and `dismiss` left it
# granted, because W3C accept presses the affirmative button and Apple puts
# "Don't Allow" last. This check wants a grant specifically, so it asks for one
# by name. CHALLENGES 63.
case "$PLATFORM" in
  ios)     ALLOW='Allow While Using App' ;;
  *)       ALLOW='While using the app' ;;
esac
$M tap "label=$ALLOW" >/dev/null 2>&1 || $M tap "text=$ALLOW" >/dev/null
sleep 3

# And the dialog has to be gone, or everything below is measuring a screen with
# a modal over it.
$M alert | grep -qi 'no dialog' || fail "the permission dialog is still on screen"
echo "    popup cleared  answered, and the dialog actually went away      ok"

# The interruption has to have actually happened, or everything below passes
# by having measured nothing.
readText "the popup" | grep -q 'rebuilds: 1' || fail "the screen never rebuilt, so nothing was interrupted"

# What the app kept. Both fields look identical now and only one still holds
# what was typed into it -- which is the whole point, and why a screenshot
# cannot answer this.
readText "the popup" | grep -q "$KEEP" \
  || fail "the kept draft lost its text across the popup: the app dropped the user's work"
echo "    kept           the draft survived the interruption              ok"

# And the positive control. If a lost draft still read as present, this check
# could not tell a surviving field from a vanished one, and the assertion
# above would mean nothing.
readText "the popup" | grep -q "$LOSE" \
  && fail "the volatile draft survived, so this check cannot detect state loss at all"
echo "    lost           state loss is detectable, not assumed            ok"

# --- the app's own alerts ---------------------------------------------------
# Different from the permission prompt above, which is another process's
# window. These are the app's: UIAlertController on iOS, a Dialog on Android.
#
# The two-button case is the control CHALLENGES 63 needed. There, accept left a
# permission denied and dismiss left it granted, and the explanation offered
# was that the mapping is right on an ordinary alert -- which had been written
# down without being measured. This measures it.
# They live on the Dialog Demo, with every other kind; dialogs.sh walks them
# all, and this keeps the two-button case and the prompt in the main flow.
$M tap "$(ref 'Back')" >/dev/null 2>&1 || true; sleep 1
$M tap "$(ref 'Dialog Demo')" >/dev/null; sleep 2

$M tap 'testid=twoButtonBtn' >/dev/null; sleep 2
$M alert | grep -qi 'a dialog is on screen' || fail "the app's own alert was not seen"
$M alert accept >/dev/null; sleep 2
readText "an app alert" | grep -q 'two-button: Discard' \
  || fail "accept did not press the affirmative button on a two-button alert"
echo "    app alert      accept pressed Discard                           ok"

$M tap 'testid=twoButtonBtn' >/dev/null; sleep 2
$M alert dismiss >/dev/null; sleep 2
readText "an app alert" | grep -q 'two-button: Keep Editing' \
  || fail "dismiss did not press the cancel button on a two-button alert"
echo "    app alert      dismiss pressed Keep Editing                     ok"

# A prompt is iOS-only -- React Native's Alert.prompt is marked @platform ios
# and UiAutomator2 does not implement alert text entry at all. Both halves are
# asserted rather than skipped on Android: a platform that lacks something
# should say so, and a check that stays silent cannot tell "unsupported" from
# "untested".
$M tap 'testid=promptBtn' >/dev/null; sleep 2
if [ "$PLATFORM" = "ios" ]; then
  $M alert accept --text "quarterly report" >/dev/null
  sleep 2
  readText "a prompt" | grep -q 'prompt: saved as quarterly report' \
    || fail "the text typed into the prompt did not reach the app"
  echo "    prompt         typed into the dialog, and the app got it       ok"
else
  readText "a prompt" | grep -q 'iOS-only' \
    || fail "the app did not say that a prompt is unavailable on Android"
  $M tap 'testid=twoButtonBtn' >/dev/null; sleep 2
  $M alert --text "nope" 2>&1 | grep -qi 'cannot type into a dialog' \
    || fail "typing into an Android dialog was not refused with a reason"
  $M alert dismiss >/dev/null 2>&1 || true
  echo "    prompt         refused, and the app says why                   ok"
fi

# --- controls that have a state ---------------------------------------------
# A checkbox is not a button. Tapping one toggles it, so a caller that cannot
# see whether it is already ticked cannot reach a desired state -- it can only
# flip whatever is there. `map` reported the state of nothing until 2026-09-21:
# it was parsed, carried on the wire, and dropped at the map layer, so a ticked
# box and an empty one printed the same line. CHALLENGES 65.
$M tap "$(ref 'Back')" >/dev/null 2>&1 || true; sleep 1
$M tap "$(ref 'Form Demo')" >/dev/null; sleep 2

$M map | grep -q 'Email me (checkbox, unchecked)' \
  || fail "the checkbox state is missing from map, or it did not start unchecked"
$M map | grep -q 'Free (radio, checked)' \
  || fail "the selected radio is not reported as checked"
echo "    form           state is in the map, not only the app            ok"

# The label has to be the label. On iOS the accessibility *value* of a custom
# control is the phrase VoiceOver speaks -- "checkbox, unchecked" -- and a real
# Switch's is "0" or "1"; both were being used as the element's text, so a
# control called "Dark mode" mapped as `0`. That was true of every iOS switch,
# not only this one.
$M map | grep -q 'Dark mode (switch' || fail "the switch is labeled by its value, not its name"
echo "    form labels    a switch is named, not numbered                  ok"

# Roles match across platforms, which on iOS needs the role the control
# declares for itself: there is no XCUIElementType for a checkbox, so React
# Native renders one as Other and says what it is in the value.
[ "$($M find 'role=checkbox' 2>/dev/null | grep -c checkbox)" -ge 2 ] \
  || fail "role=checkbox does not resolve to both checkboxes"
echo "    form roles     role=checkbox resolves on both platforms         ok"

# And the state has to move when the control does -- otherwise the reading
# above could be a constant that happens to look right.
$M tap 'testid=notifyCheck' >/dev/null; sleep 2
$M map | grep -q 'Email me (checkbox, checked)' \
  || fail "the checkbox was tapped and the map still reports it unchecked"
readText "toggling" | grep -q 'notify=true' || fail "the app disagrees that it was checked"
echo "    form toggle    the state changed, and the app agrees            ok"

# A radio group is the interesting case: choosing one must clear the other,
# and a tool that reported both as checked would be worse than useless.
$M tap 'testid=planPro' >/dev/null; sleep 2
$M map | grep -q 'Pro (radio, checked)' || fail "the chosen radio is not checked"
$M map | grep -q 'Free (radio, unchecked)' || fail "the previous radio is still checked"
echo "    radio group    choosing one cleared the other                   ok"

# --- reaching a state rather than toggling ----------------------------------
# `check` and `uncheck` set a state; `tap` flips whatever is there. The
# difference only shows on the second call, which is why it is made twice.
$M check 'testid=termsCheck' | grep -q 'is now checked' || fail "check did not tick the box"
$M check 'testid=termsCheck' | grep -q 'is already checked' \
  || fail "checking an already-checked box did not report it as a no-op"
$M map | grep -q 'Accept terms (checkbox, checked)' \
  || fail "the second check toggled the box off again"
echo "    check          idempotent: the second call changed nothing      ok"

$M uncheck 'testid=termsCheck' | grep -q 'is now unchecked' || fail "uncheck did not clear the box"
$M uncheck 'testid=termsCheck' | grep -q 'is already unchecked' \
  || fail "unchecking an already-clear box did not report it as a no-op"
echo "    uncheck        idempotent the other way                         ok"

# Refusing is half the tool. Tapping a button and calling it checked would be
# the failure this project is organized against, and a radio is not something
# that gets unchecked -- its group is cleared by choosing another member.
$M check 'testid=backBtn' 2>&1 | grep -q 'no checked state' \
  || fail "a button was accepted as a checkbox"
$M uncheck 'testid=planFree' 2>&1 | grep -q 'cannot be unchecked' \
  || fail "unchecking a radio was accepted"
echo "    check refuses  a button and an unchecked radio, with reasons    ok"

# And it has to reach the app, not just the hierarchy. The switch is the
# app's dark mode, so checking it is judged three ways that do not lean on
# each other: the switch's state in the map, the app's own theme= line, and
# the screen's colors in a screenshot -- which nothing that only echoes a
# value back can fake. Then back to light, for whatever runs next.
luma() { # the luminance of a background point near the right edge, 0-255
  $M screenshot -o "$TMPSHOT" >/dev/null
  python3 "$ROOT/docs/checks/pixel.py" "$TMPSHOT" 0.97 0.85
}
TMPSHOT="$(mktemp -t mobium-theme).png"
$M check 'testid=darkSwitch' >/dev/null
sleep 1
$M map | grep -q 'Dark mode (switch, checked)' || fail "the switch reads unchecked after checking it"
readText "checking the switch" | grep -q 'theme=dark' || fail "the switch was set and the app did not see it"
[ "$(luma)" -lt 60 ] || fail "the app says theme=dark and the screen is not dark"
$M uncheck 'testid=darkSwitch' >/dev/null
sleep 1
readText "unchecking the switch" | grep -q 'theme=light' || fail "the switch was cleared and the app stayed dark"
[ "$(luma)" -gt 200 ] || fail "the app says theme=light and the screen is not light"
rm -f "$TMPSHOT"
echo "    check reaches  dark mode: the map, the app and the pixels agree ok"

echo "PASS"
