#!/bin/sh
# A fourth third-party app on iOS: Pocket Casts, a podcast player, on a
# simulator built from its source and on an iPhone from the App Store.
#
#   docs/checks/pocketcasts-ios.sh <simulator-udid | iphone-udid>
#
# Its first look found six things wrong (CHALLENGES 234–239). This check
# holds each of them to the app, signed out:
#
#   - onboarding's topic picker maps its continue button as disabled, and
#     map --diff says when choosing three topics enables it (239);
#   - the "Receive Notifications" row is a target and its checkmark,
#     `discover_tick`, is not (236);
#   - a podcast's rows and header are named by their words, not by the
#     Follow and Funding buttons or the star images inside them (237);
#   - Play in an episode's sheet is touched clear of the divider through its
#     center, with no note that something may take the touch (238);
#   - the player's first-run tip, a popover, is said by map and app_alert
#     with a point outside it, and touching that point closes it (235);
#   - the scrubber maps by its label as adjustable, with its position as its
#     value, and app_fill refuses it naming a drag (234).
#
# On a simulator the app's data is cleared first, so onboarding and the tip
# come up; a phone's cannot be, so there those parts run only on a fresh
# install, and the check says which were skipped. Nothing is followed or
# signed in to. The newest episode of the first podcast Discover ranks is
# played for a moment and paused at once, so audio is heard briefly. Notifications are switched
# off in the app before its preferences are saved, so iOS asks nothing.
#
# To build it for a simulator (no signing). Its main branch needs Xcode 27;
# 72b785001~1 builds with Xcode 26.6, and its scheme embeds a Watch app, so
# the watchOS simulator runtime is needed (xcodebuild -downloadPlatform
# watchOS). Leave out -sdk: with it the GRDB macros are built for the
# simulator and fail.
#   git clone https://github.com/Automattic/pocket-casts-ios.git && cd pocket-casts-ios
#   git checkout 72b785001~1 && make external_contributor
#   xcodebuild -project podcasts.xcodeproj -scheme pocketcasts \
#     -destination 'generic/platform=iOS Simulator' -derivedDataPath ../pc-dd \
#     -skipMacroValidation -skipPackagePluginValidation CODE_SIGNING_ALLOWED=NO build
#   mobium install ../pc-dd/Build/Products/Debug-iphonesimulator/podcasts.app
set -e
DEV="${1:?usage: pocketcasts-ios.sh <udid>}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
M="$ROOT/bin/mobium --driver wda --device $DEV"
APP=au.com.shiftyjelly.podcasts
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
skip() { printf '    %-12s %-58s skipped: %s\n' "$1" "$2" "$3"; }
# ref <pattern>: the first map entry whose line matches, by ref only.
ref() { $M map | grep -E "^@e[0-9]+ $1" | awk '{ print $1; exit }'; }
# tapref <label>: tap the first entry map lists by that label. By ref,
# because on an install that has been used the same label is also on
# hidden elements behind the screen in front, and a locator rightly
# refuses to choose (CHALLENGES 242).
tapref() {
  r=$(ref "$1 \\(")
  [ -n "$r" ] || fail "$1 is not in map"
  $M tap "$r"
}
# pause: stop playback if anything is playing.
pause() {
  if $M map | grep -qE '^@e[0-9]+ Pause \('; then tapref Pause >/dev/null; fi
}

echo "--- $DEV"
simulator=""
xcrun simctl list devices 2>/dev/null | grep -q "$DEV" && simulator=1
if [ -n "$simulator" ]; then
  $M clear-data "$APP" >/dev/null
fi
$M launch "$APP" >/dev/null

if $M map | grep -qE '^@e[0-9]+ Get Started \('; then
  echo "  onboarding"
  $M tap "label=Get Started" >/dev/null
  $M wait "label=Select at least 3" >/dev/null || fail "the topic picker did not come up"
  $M map | grep -qE '^@e[0-9]+ Select at least 3 \(button, disabled\)$' ||
    fail "the topic picker's button is not mapped as disabled"
  row 239 "Select at least 3 is mapped disabled"
  # By ref: on an iPad the picker is a sheet over Discover, whose category
  # buttons of the same names are behind it, reported hidden (CHALLENGES 242).
  $M map >/dev/null
  for topic in Technology News Comedy; do $M tap "$(ref "$topic \\(button")" >/dev/null; done
  $M map --diff | grep -qE '^~ @e[0-9]+ Continue \(button\) — was "Select at least 3", was disabled$' ||
    fail "map --diff did not say the button was enabled"
  row 239 "map --diff says it was enabled"
  for topic in Technology News Comedy; do $M tap "$(ref "$topic \\(button")" >/dev/null; done
  $M tap "label=Not Now" >/dev/null
  $M tap "label=Continue" >/dev/null
  $M tap "label=Not Now" >/dev/null
  $M wait "label=Save Preferences" >/dev/null || fail "the notification step did not come up"
  notify=$(ref 'Receive Notifications')
  [ -n "$notify" ] || fail "the Receive Notifications row is not in map"
  if $M map | grep -q discover_tick; then fail "the row's checkmark, discover_tick, is a target of its own"; fi
  row 236 "the row is a target, its checkmark is not"
  $M tap "$notify" >/dev/null
  $M tap "label=Save Preferences" >/dev/null
  if $M alert | grep -q 'a dialog is on screen'; then $M alert dismiss >/dev/null; fi
else
  skip 239 "onboarding" "not a fresh install"
  skip 236 "onboarding" "not a fresh install"
fi

# Anywhere else the app was left — the player open, a podcast's page — is
# put back to Discover's root: the player closed, then the Discover tab,
# the last entry by that name (a podcast page's back button is another).
if $M map | grep -qE '^@e[0-9]+ Close player \('; then
  pause
  tapref "Close player" >/dev/null
fi
# A search left open covers Discover's root, and the Discover tab does not
# close it: on the iPhone a run met one and found no Discover (2026-10-06).
if $M map | grep -qE '^@e[0-9]+ Cancel \(button'; then
  $M tap "label=Cancel" >/dev/null 2>&1 || true
fi
# Only when Discover's root is not already up: a tap on the selected tab
# there opens its search instead.
if ! $M map | grep -qE '^@e[0-9]+ Search podcasts or add RSS URL \(input\)'; then
  tab=$($M map | grep -E '^@e[0-9]+ Discover \(button' | tail -1 | awk '{ print $1 }')
  [ -n "$tab" ] || fail "the Discover tab is not in map"
  $M tap "$tab" >/dev/null
fi

echo "  a podcast"
# Discover is live, and its search reaches a backend that once answered
# "Search Failed" for an afternoon (CHALLENGES 257), so the check takes the
# first podcast Discover ranks, whatever it is, and holds the shape of its
# rows rather than their words — none of what it holds needs a search.
$M wait "text=Search podcasts or add RSS URL" >/dev/null || fail "Discover did not come up"
lines=$($M map)
if echo "$lines" | grep -E '^@e[0-9]+ .+ Follow \(button\)$' | grep -qvE '^@e[0-9]+ Follow \('; then
  fail "a Discover row is named with its Follow button: $(echo "$lines" | grep -E '.+ Follow \(button\)$' | grep -vE '^@e[0-9]+ Follow \(' | head -1)"
fi
podcast=$(echo "$lines" | sed -n '/SHOW ALL (button)/,$p' | grep -E '^@e[0-9]+ ' |
  grep -vE '^@e[0-9]+ (Follow|SHOW ALL|CollectionView|Page [0-9]+ of [0-9]+) ' | awk '{ print $1; exit }')
[ -n "$podcast" ] || fail "no ranked podcast is in map"
row 237 "Discover's rows are named without Follow"
$M tap "$podcast" >/dev/null
$M wait "label=Episodes" >/dev/null || fail "the podcast did not open"
lines=$($M map)
echo "$lines" | grep -qE '^@e[0-9]+ Follow \(button\)$' || fail "Follow lost its own entry"
header=$(echo "$lines" | grep -E '^@e[0-9]+ .+ · .+ \(button\)$' | head -1)
[ -n "$header" ] || fail "the podcast's header is not in map"
case "$header" in
  *Follow*|*Funding*|*star-*|*chevron-*) fail "the header is named by the controls or images inside it: $header" ;;
esac
row 237 "the header is named by its words"

echo "  an episode"
$M swipe up >/dev/null
# An episode row reads "SEPTEMBER 16. Title. 1 minute", or "TRAILER ,
# SEPTEMBER 16. …", or starts TODAY or YESTERDAY.
episode=$($M map | grep -E '^@e[0-9]+ ([A-Z]+ , )?([A-Z]{3,} [0-9]{1,2}|TODAY|YESTERDAY)\. ' | awk '{ print $1; exit }')
[ -n "$episode" ] || fail "no episode row is in map"
$M tap "$episode" >/dev/null
$M wait "label=Download" >/dev/null || fail "the episode sheet did not come up"
out=$(tapref Play)
pause
case "$out" in
  *"may take the touch"*) fail "Play was noted as possibly covered: $out" ;;
  *"touched where nothing is"*) ;;
  *) fail "Play was not touched clear of the divider: $out" ;;
esac
row 238 "Play is touched clear of the divider"

echo "  the player"
tapref Player >/dev/null
note=$($M map | grep 'a popover is in front' || true)
if [ -n "$note" ]; then
  $M alert | grep -q 'no dialog is on screen, but a popover is in front' ||
    fail "app_alert did not say the popover is up"
  xy=$(echo "$note" | sed -nE 's/.*app_tap at x ([0-9]+), y ([0-9]+) is outside it.*/\1 \2/p')
  [ -n "$xy" ] || fail "the note gives no point outside the popover: $note"
  # shellcheck disable=SC2086
  $M tap $xy >/dev/null
  if $M map | grep -q 'a popover is in front'; then fail "touching outside it did not close the popover"; fi
  row 235 "the tip is said, and closes where it says"
else
  skip 235 "the first-run tip" "not the player's first opening"
fi
scrubber=$($M map | grep -E '^@e[0-9]+ Episode Playback \(adjustable, .+ of .+\)$' | awk '{ print $1 }')
[ -n "$scrubber" ] || fail "the scrubber is not mapped as adjustable with its position"
if $M fill "$scrubber" 0.5 2>/tmp/pocketcasts-fill.$$; then fail "app_fill moved the scrubber"; fi
grep -q 'adjustable control' /tmp/pocketcasts-fill.$$ && grep -q 'app_swipe' /tmp/pocketcasts-fill.$$ ||
  { rm -f /tmp/pocketcasts-fill.$$; fail "app_fill was not refused naming a drag"; }
rm -f /tmp/pocketcasts-fill.$$
row 234 "the scrubber is adjustable, and fill names a drag"

pause
tapref "Close player" >/dev/null
$M press home >/dev/null
echo "  pass"
