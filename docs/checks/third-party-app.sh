#!/bin/sh
# Rollout gate G4: a genuine third-party hybrid app, not one that ships with
# the OS.
#
# Everything Mobium had driven before this — Settings, Calculator, Clock,
# Chrome, Contacts, Photos — is Google's, and every rule about labeling,
# collapsing and filtering `map` output was tuned on those screens. The
# recorded risk was that "map output is unusable on large commercial apps".
# This check is what turns that from a worry into a measurement.
#
# The app is Wikipedia's official Android client: genuinely third-party
# (Wikimedia Foundation), large, widely used, and **hybrid** — its article view
# is a WebView, so one app exercises both halves of the gate. It is also open
# source and downloadable from F-Droid, so this check needs no account, no
# Play Store, and no APK of dubious provenance.
#
#   docs/checks/third-party-app.sh <serial> [path-to-apk]
#   docs/checks/third-party-app.sh <iphone-udid>
#
# Without an APK path it expects org.wikipedia already installed. To fetch one:
#   curl -L -o wikipedia.apk https://f-droid.org/repo/org.wikipedia_50606.apk
#
# On a real iPhone, install Wikipedia from the App Store first. An App Store
# app is device-signed and will not run on a simulator, which is why this gate
# could not be met on iOS until there was a phone — and why the iOS half runs
# on a phone only. It drives the native app and follows links through the
# accessibility tree WebKit exposes: a phone's WebViews can be attached, but
# the App Store build does not opt into inspection, so it publishes none. The
# Android half attaches to the WebView instead.
set -e
DEV="$1"; APK="$2"
if [ -z "$DEV" ]; then echo "usage: $0 <serial> [apk]" >&2; exit 2; fi

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
M="$ROOT/bin/mobium --device $DEV"
APP=org.wikipedia
fail() { echo "FAIL: $*" >&2; exit 1; }

# A real iPhone's UDID is two groups, 00008120-0001234567890ABC.
case "$DEV" in
  [0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f]-[0-9A-Fa-f]*)
    exec "$(dirname "$0")/third-party-app-ios.sh" "$DEV" ;;
esac

echo "--- $DEV"

if [ -n "$APK" ]; then
  $M install "$APK" >/dev/null
fi
# Verify by reading the state back: `install` reporting success is not the
# same as the package being there, and this whole project exists downstream of
# that distinction.
apps=$($M apps 2>&1) || fail "cannot list the apps: $(echo "$apps" | tail -1)"
echo "$apps" | grep -q "^$APP " || fail "$APP is not installed (pass the APK path as the second argument)"
echo "    install        $APP present, confirmed by listing"

$M terminate $APP >/dev/null
$M launch $APP >/dev/null

# Onboarding stands between a cold install and the app. Step through whatever
# of it is present rather than assuming a fixed number of panes: the count has
# changed between releases, and a fixed loop would fail on the next one.
i=0
while [ $i -lt 8 ]; do
  if $M map | grep -q 'Skip'; then $M tap 'text=Skip' >/dev/null; break; fi
  if $M map | grep -q 'Forward'; then $M tap 'label=Forward' >/dev/null; else break; fi
  i=$((i + 1))
done
sleep 3

# The measurement the gate exists for. A map nobody can read is a map that
# does not work, and one runaway entry is enough to ruin it.
JSON="$($M map --json)"
echo "$JSON" | python3 -c '
import json, sys
els = json.load(sys.stdin)["elements"]
if len(els) < 5:
    sys.exit("only %d elements on the feed; the app is not where it should be" % len(els))
lens = sorted((len(e["label"]) for e in els), reverse=True)
worst = lens[0]
markup = [e["ref"] for e in els if "<" in e["label"] and ">" in e["label"]]
blank  = [e["ref"] for e in els if not e["label"].strip()]
print("    map            %d entries, longest label %d chars" % (len(els), worst))
if worst > 130:
    sys.exit("a label of %d chars: descendant text is running away again" % worst)
if markup:
    sys.exit("raw markup reached the label on %s" % ", ".join(markup))
if blank:
    sys.exit("empty labels on %s" % ", ".join(blank))
'
echo "    labels         bounded, no markup, none empty                  ok"

# Search: type into a real field and read back what the app did with it.
$M tap 'label=Search' >/dev/null
sleep 2
$M map | grep -q 'Close' && { $M tap 'label=Close' >/dev/null; sleep 1; }
$M tap 'text=Search Wikipedia' >/dev/null 2>&1 || $M tap 'label=Search Wikipedia' >/dev/null
sleep 2
$M fill 'text=Search Wikipedia,role=input' 'Ada Lovelace' >/dev/null
sleep 4
$M map | grep -q 'English mathematician' || fail "the search results never arrived"
echo "    search         typed a query and the app answered              ok"

# The hybrid half. An article is a WebView, and it has to be discovered,
# switched into, and mapped as web content rather than as a native blob.
#
# Tapped by ref, not by text. A result row's label is composed from two child
# nodes — the title and the description — so no single node carries the whole
# string and `text=` cannot match it. That is what refs are for, and it is why
# `map` prints one on every line.
ref=$($M map --json | python3 -c '
import json, sys
for e in json.load(sys.stdin)["elements"]:
    if "English mathematician" in e["label"]:
        print(e["ref"]); break
')
[ -n "$ref" ] || fail "the Ada Lovelace result is not in the map"
$M tap "$ref" >/dev/null
sleep 6

# A real phone is a user build, and there a WebView publishes to DevTools only
# if the app opted in; an emulator's debug image publishes every one, which
# is why the WebView half below runs there. Measured on a Pixel 8 Pro: the
# article on screen, no devtools socket at all. So a phone reads the article
# natively and follows a link by tapping it, as third-party-app-ios.sh does
# for the App Store build.
case "$DEV" in emulator-*) ;; *)
  $M wait 'text=Charles Babbage' --timeout 60s >/dev/null || fail "the article did not load"
  if $M contexts | grep -q "WEBVIEW_$APP"; then
    echo "    webview        Wikipedia now opts into inspection — this check can attach and should"
  else
    echo "    webview        not inspectable: a release build on a user-build phone   ok"
  fi
  r=$($M map | awk '/ Charles Babbage \(button\)/{print $1; exit}')
  [ -n "$r" ] || fail "the Charles Babbage link is not in the map"
  $M tap "$r" >/dev/null
  # Android's Wikipedia answers a link with a preview sheet, not a page.
  $M wait 'text=Read article' --timeout 30s >/dev/null || fail "tapping the link raised no preview"
  $M tap 'text=Read article' >/dev/null
  # His article's own short description: his name alone is on hers too.
  $M wait 'text=English mathematician, philosopher, and engineer (1791–1871)' --timeout 120s >/dev/null \
    || fail "following the Charles Babbage link did not reach his article"
  echo "    follow link    a native tap on a web link, previewed, then read  ok"
  echo "--- passed"
  exit 0 ;;
esac

$M contexts | grep -q "WEBVIEW_$APP" || fail "no WebView context in a hybrid app"
url=$($M contexts | grep "WEBVIEW_$APP" | sed 's/.* //')
case "$url" in
  https://*wikipedia.org/*) ;;
  *) fail "the WebView reports an unexpected URL: $url" ;;
esac
echo "    webview        discovered, serving $url"

$M context "WEBVIEW_$APP" >/dev/null
links=$($M map | grep -c '(link)' || true)
[ "$links" -gt 20 ] || fail "only $links links in a Wikipedia article; the page did not map"
echo "    web map        $links links from the live page                  ok"

# A locator inside a WebView is not a ref. The message used to say "unknown
# ref … run app_map again", which is advice that can never work.
$M tap 'text=Charles Babbage' 2>&1 | grep -q 'locators do not work inside a WebView' \
  || fail "the WebView rejected a locator without explaining why"
echo "    web locator    refused with the reason, not 'unknown ref'       ok"

$M context NATIVE_APP >/dev/null
echo "--- passed"
