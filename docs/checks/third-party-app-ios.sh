#!/bin/sh
# Rollout gate G4, on iOS: Wikipedia from the App Store, on a real iPhone.
#
# The iOS half of third-party-app.sh, which hands a phone's UDID to this file.
# Same app, same questions — is `map` readable on an app nobody at Apple
# wrote, can a search be typed and answered, can an article be reached and
# read — asked of a different platform, so that what differs is the platform
# and not the choice of app.
#
# It could not exist before a phone: an App Store app is device-signed and
# will not run on a simulator. And it drives the article natively. A phone's
# WebViews can be attached, but this one publishes nothing — Wikipedia's App
# Store build does not opt into inspection — and WebKit publishes the page's
# links in the native accessibility tree anyway, where Mobium maps them
# (CHALLENGES 78), so a link is followed with an ordinary tap.
#
# The first run on this app found four defects (CHALLENGES 77–80): a selected
# button labeled "1", feed cards and article links missing from `map`
# entirely, a label with a newline in it, and — once the phone locked itself
# mid-session — a two-minute wait for a runner that could never start.
#
# Slow, and that is measured rather than tolerated: reading an article's
# hierarchy takes around 30 seconds on the phone, because WebDriverAgent asks
# every one of a long page's elements whether it is visible. Allow ten minutes.
#
#   docs/checks/third-party-app-ios.sh <udid>
#
# Install Wikipedia from the App Store first. On a fresh install the
# onboarding is stepped through; on a later run it is skipped.
set -e
DEV="${1:?usage: third-party-app-ios.sh <iphone-udid>}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium --driver wda --device $DEV"
APP=org.wikimedia.wikipedia
# A simulator is refused by name: an App Store app does not run on one, and
# "install Wikipedia from the App Store" — what a sweep of the simulator
# checks was told — is advice a simulator cannot take.
if xcrun simctl list devices 2>/dev/null | grep -q "$DEV"; then
  echo "third-party-app-ios.sh is for a real iPhone: Wikipedia from the App Store is device-signed and does not run on a simulator" >&2
  exit 2
fi
fail() { echo "FAIL: $*" >&2; exit 1; }

# refOf prints the ref of the first map entry whose label contains $1.
refOf() { $M map --json 2>/dev/null | python3 -c "
import json,sys
for e in json.load(sys.stdin)['elements']:
    if '''$1''' in (e.get('label') or ''): print(e['ref']); break
"; }

echo "--- $DEV (iPhone)"
# A phone that cannot be reached answers apps with an error, which is not
# the same as an app that is not installed: a locked phone was reported as
# missing Wikipedia.
apps=$($M apps 2>&1) || fail "cannot list the apps: $(echo "$apps" | tail -1)"
echo "$apps" | grep -q "^$APP " || fail "$APP is not installed — install Wikipedia from the App Store"
echo "    install        $APP present, confirmed by listing"

$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null

# Onboarding, when this is a fresh install: a language step, a privacy step
# and a three-page personalization series, each with a Next button. Stepped
# through by what is on screen rather than by count.
i=0
while [ $i -lt 8 ]; do
  screen="$($M map)"
  echo "$screen" | grep -q 'Next (button)' || break
  # The personalization pages offer two options as plain buttons, the chosen
  # one carrying value "1". That printed as `1 (button)` (CHALLENGES 77).
  if echo "$screen" | grep -qE '^@e[0-9]+ [01] \(button\)'; then
    fail "an onboarding option is labeled by its selected state, not its name"
  fi
  $M tap 'label=Next,role=button' >/dev/null
  i=$((i + 1))
done
[ $i -gt 0 ] && echo "    onboarding     stepped through $i pages"

# Back to the feed from wherever the app reopened: an article has a "return
# to Home" button, and the tab bar has Home.
for _ in 1 2 3; do
  $M map | grep -q 'Search (button)' && break
  r=$(refOf 'return to Home'); [ -n "$r" ] || r=$(refOf 'Home')
  [ -n "$r" ] || break
  $M tap "$r" >/dev/null
done

# The measurement the gate exists for, with one more rule than Android: one
# entry per line.
JSON="$($M map --json)"
echo "$JSON" | python3 -c '
import json, sys
els = json.load(sys.stdin)["elements"]
if len(els) < 5:
    sys.exit("only %d elements on the feed; the app is not where it should be" % len(els))
worst = max(len(e["label"]) for e in els)
markup = [e["ref"] for e in els if "<" in e["label"] and ">" in e["label"]]
blank  = [e["ref"] for e in els if not e["label"].strip()]
broken = [e["ref"] for e in els if "\n" in e["label"] or "\r" in e["label"]]
print("    map            %d entries, longest label %d chars" % (len(els), worst))
if worst > 130: sys.exit("a label of %d chars: descendant text is running away again" % worst)
if markup: sys.exit("raw markup reached the label on %s" % ", ".join(markup))
if blank:  sys.exit("empty labels on %s" % ", ".join(blank))
if broken: sys.exit("a label spans lines on %s" % ", ".join(broken))
'
echo "    labels         bounded, no markup, none empty, one line each   ok"

# A card is something to tap, so it has to be in the map. The feed's first
# card is Wikipedia's featured article, whatever today's is: the first long
# button that owns a Save for later. Since 237 a row is no longer named by
# the buttons inside it, so Save for later is its own entry, right after the
# card; before, it was in the card's label. Either shape is the card.
CARD=$($M map --json | python3 -c '
import json, sys
els = json.load(sys.stdin)["elements"]
for i, e in enumerate(els):
    long = e["role"] == "button" and len(e["label"]) > 60
    owns = i + 1 < len(els) and els[i + 1]["label"] == "Save for later"
    if long and ("Save for later" in e["label"] or owns):
        print(e["ref"]); break
')
[ -n "$CARD" ] || fail "no feed card in the map — the featured article is on screen and not a target"
echo "    feed card      the featured article is a target               ok"

# Search, typed into the real field, answered by the app.
$M tap 'label=Search,role=button' >/dev/null
$M fill 'role=input' 'Ada Lovelace' >/dev/null
# The first search on an install raises an "Add languages" tooltip, and while
# it is up iOS hides everything else from accessibility — the results report
# visible="false" and do not map, correctly: VoiceOver cannot reach them
# either. Close it if it is there.
if ! $M map | grep -q 'English mathematician'; then
  r=$(refOf 'Close'); [ -n "$r" ] && $M tap "$r" >/dev/null
fi
$M map | grep -q 'English mathematician' || fail "the search results never arrived"
echo "    search         typed a query and the app answered              ok"

# The article, reached by the result's ref: its label is composed from two
# child texts, so no single node carries it.
$M tap "$(refOf 'English mathematician')" >/dev/null
$M wait 'label=Charles Babbage' --timeout 120s >/dev/null \
  || fail "the article did not load"
links=$($M map | grep -c '(link)' || true)
[ "$links" -gt 10 ] || fail "only $links links in a Wikipedia article; the page did not map"
echo "    article        $links links from the page, natively              ok"

# The article's WebView cannot be attached, and not because of the phone:
# a phone's WebViews are reachable, and MobiumApp's are. The App Store build
# of Wikipedia does not set isInspectable, so WebKit publishes no target for
# it: a WebView that has not opted in is unreachable, measured on a shipped
# app. The article is read
# through the native links above instead.
if $M contexts | grep -q "WEBVIEW_$APP"; then
  echo "    webview        Wikipedia now opts into inspection — this check can attach and should"
else
  echo "    webview        not inspectable: the App Store build does not opt in  ok"
fi

# And a link is followed by tapping it. Found by ref: the same word is often
# linked more than once in an article, and a label locator is then refused as
# ambiguous — correctly.
# Asserted on his article's own short description: "Analytical Engine" or
# his dates alone also appear on Ada Lovelace's page, and would pass without
# the link ever being followed.
$M tap "$(refOf 'Charles Babbage')" >/dev/null
$M wait 'label=English mathematician, philosopher, and engineer (1791–1871)' --timeout 120s >/dev/null \
  || fail "following the Charles Babbage link did not reach his article"
echo "    follow link    a native tap on a web link navigated            ok"

echo "--- passed"
