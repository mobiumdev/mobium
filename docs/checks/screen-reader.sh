#!/bin/sh
# A screen reader stays on while the default driver drives the device.
#
#   docs/checks/screen-reader.sh <android-serial>
#
# Android's test-automation connection silences every other accessibility
# service unless it is opened not to (CHALLENGES 208). The UiAutomator2
# server opens it so only when its instrumentation is started with
# DISABLE_SUPPRESS_ACCESSIBILITY_SERVICES — not a setting or a capability —
# and Mobium starts it that way. This turns TalkBack on and checks, with a
# UiAutomator2 session open:
#
#   - TalkBack, and any service already on, stay bound, and touch
#     exploration stays on;
#   - a tap, a scroll and typing still land, as MobiumApp says;
#   - the device's accessibility settings are back exactly as found after.
#
# The suppression it guards against shows as TalkBack missing from the bound
# services, and touch exploration off, for as long as the session is open.
#
# It changes a setting a person relies on, and TalkBack takes over touch for
# whoever holds the device: an emulator only, unless ALLOW_PHONE=1. Needs
# TalkBack (com.google.android.marvin.talkback) and MobiumApp installed.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <android-serial>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-14s %-58s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
TB=com.google.android.marvin.talkback/com.google.android.marvin.talkback.TalkBackService
export MOBIUM_SESSION="${MOBIUM_SESSION:-srcheck}"
M="$ROOT/bin/mobium --device $DEV"
a() { adb -s "$DEV" shell "$@" | tr -d '\r'; }

case "$DEV" in
  emulator-*) ;;
  *) [ -n "$ALLOW_PHONE" ] || fail "$DEV is not an emulator: TalkBack would take over a person's phone. ALLOW_PHONE=1 to go ahead" ;;
esac
a pm list packages com.google.android.marvin.talkback | grep -q talkback || fail "TalkBack is not installed on $DEV"
echo "--- $DEV"

bound() {
  a dumpsys accessibility | python3 -c '
import re, sys
t = sys.stdin.read()
m = re.search(r"Bound services:\{(.*?)\}\s*\n\s*Enabled services", t, re.S)
print(",".join(re.findall(r"label=([^,]+)", m.group(1))) if m else "")'
}
exploring() { a dumpsys accessibility | grep -o -m1 'touchExplorationEnabled=[a-z]*' | cut -d= -f2; }

was_svc=$(a settings get secure enabled_accessibility_services)
was_on=$(a settings get secure accessibility_enabled)
# adb shell joins its arguments into one line for the device's shell, so an
# empty value vanishes and a phone's "" would be put back as nothing at all:
# every value goes over quoted.
restore() {
  $M daemon stop >/dev/null 2>&1 || true
  if [ "$was_svc" = null ]; then a settings delete secure enabled_accessibility_services >/dev/null
  else a "settings put secure enabled_accessibility_services '$was_svc'"; fi
  if [ "$was_on" = null ]; then a settings delete secure accessibility_enabled >/dev/null
  else a "settings put secure accessibility_enabled '$was_on'"; fi
}
trap restore EXIT INT TERM

$M daemon stop >/dev/null 2>&1 || true
if [ "$was_svc" = null ] || [ -z "$was_svc" ]; then on="$TB"; else on="$was_svc:$TB"; fi
a settings put secure enabled_accessibility_services "$on"
a settings put secure accessibility_enabled 1
# Touch exploration comes on a moment after TalkBack is bound: wait for both.
i=0; until bound | grep -q TalkBack && [ "$(exploring)" = true ]; do
  i=$((i + 1)); [ $i -lt 30 ] || fail "TalkBack did not come up with touch exploration after being enabled"; sleep 1
done
before=$(bound)
row "talkback" "on, bound: $before"

# A session: the server starts with the screen reader running.
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
[ "$(bound)" = "$before" ] || fail "with a session open the bound services are '$(bound)', were '$before' — the screen reader was silenced"
[ "$(exploring)" = true ] || fail "touch exploration went off when the session opened"
row "session" "open, every service still bound, touch exploration on"

$M scroll-to "label=Motion Demo" >/dev/null 2>&1 || true
$M tap "label=Motion Demo" >/dev/null
$M tap "label=Replay Ignoring" >/dev/null
$M tap "label=Ignoring target" >/dev/null
$M text testid=ignoringResult | grep -q "tapped" || fail "a tap did not land with TalkBack on: $($M text testid=ignoringResult)"
row "tap" "landed, as the app says"

$M press back >/dev/null
$M scroll-to "label=Slider Demo" >/dev/null || fail "scrolling did not reach Slider Demo with TalkBack on"
$M scroll-to "label=Login Demo" --direction up >/dev/null
$M tap "label=Login Demo" >/dev/null
$M fill testid=username "screen-reader-on" >/dev/null
[ "$($M text testid=username)" = "screen-reader-on" ] || fail "typing did not land with TalkBack on: $($M text testid=username)"
row "scroll, type" "both landed"

[ "$(bound)" = "$before" ] || fail "after the actions the bound services are '$(bound)'"
[ "$(exploring)" = true ] || fail "touch exploration went off during the actions"
row "after" "every service still bound, touch exploration on"

$M terminate "$APP" >/dev/null 2>&1 || true
restore
trap - EXIT INT TERM
[ "$(a settings get secure enabled_accessibility_services)" = "$was_svc" ] || fail "enabled services not put back: $(a settings get secure enabled_accessibility_services)"
[ "$(a settings get secure accessibility_enabled)" = "$was_on" ] || fail "accessibility_enabled not put back"
row "restored" "the device's accessibility settings as found"
echo PASS
