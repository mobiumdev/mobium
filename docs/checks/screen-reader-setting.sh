#!/bin/sh
# The screen reader as a setting: `mobium accessibility screen_reader on`.
#
#   docs/checks/screen-reader-setting.sh <android-serial | iphone-udid | simulator-udid>
#
# TalkBack on Android and VoiceOver on an iPhone, turned on for a session and
# put back when it ends, like every other accessibility setting. It checks:
#
#   - it starts off, and `on` is confirmed by the platform itself — on
#     Android TalkBack bound with touch exploration on, read here from
#     dumpsys, not from Mobium; on an iPhone WebDriverAgent's VoiceOver
#     state;
#   - with it on, a tap still lands: MobiumApp's Form Demo selects Pro;
#   - ending the session puts it back: on Android both keys exactly as found,
#     still so three seconds later (the system rewrites accessibility_enabled
#     while TalkBack unbinds, CHALLENGES 348), and on an iPhone VoiceOver off;
#   - a simulator refuses, naming the Settings switch instead.
#
# TalkBack and VoiceOver take over touch for whoever holds the device: a
# phone only with ALLOW_PHONE=1, and the restore is registered before
# anything is turned on.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-10s %-60s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
export MOBIUM_SESSION="${MOBIUM_SESSION:-srsetting}"
M="$ROOT/bin/mobium --device $DEV"
a() { adb -s "$DEV" shell "$@" | tr -d '\r'; }
case "$DEV" in
  ????????-????????????????) KIND=phone ;;
  *-*-*-*-*) KIND=simulator ;;
  emulator-*) KIND=emulator ;;
  *) KIND=android-phone ;;
esac
echo "--- $DEV ($KIND)"

if [ "$KIND" = simulator ]; then
  out=$($M accessibility screen_reader on 2>&1) && fail "a simulator turned VoiceOver on: $out"
  echo "$out" | grep -q "Settings > Accessibility > VoiceOver" || fail "the refusal did not name the Settings switch: $out"
  row "refused" "a simulator, naming its Settings switch"
  echo PASS; exit 0
fi
case "$KIND" in
  phone | android-phone) [ -n "$ALLOW_PHONE" ] || { echo "$DEV is a phone: a screen reader takes over its owner's touch. ALLOW_PHONE=1 to go ahead" >&2; exit 2; } ;;
esac

state=$($M accessibility screen_reader 2>/dev/null) || fail "the screen reader could not be read: $($M accessibility screen_reader 2>&1)"
[ "$state" = "screen_reader off" ] || { echo "the screen reader is already on ($state): left alone" >&2; exit 2; }

if [ "$KIND" = phone ]; then
  # The backstop, if the session's own restore never runs: VoiceOver off.
  at_exit "$M daemon stop >/dev/null 2>&1; MOBIUM_SESSION=srsetting-restore $M accessibility screen_reader off >/dev/null 2>&1; MOBIUM_SESSION=srsetting-restore $M daemon stop >/dev/null 2>&1"
else
  was_svc=$(a settings get secure enabled_accessibility_services)
  was_on=$(a settings get secure accessibility_enabled)
  explore() { a dumpsys accessibility | grep -o -m1 'touchExplorationEnabled=[a-z]*' | cut -d= -f2; }
  talkback() { a dumpsys accessibility | sed -n '/Bound services:{/,/Enabled services/p' | grep -c 'label=TalkBack' || true; }
  backstop() {
    $M daemon stop >/dev/null 2>&1 || true
    if [ "$(a settings get secure enabled_accessibility_services)" != "$was_svc" ]; then
      if [ "$was_svc" = null ]; then a settings delete secure enabled_accessibility_services >/dev/null
      else a "settings put secure enabled_accessibility_services '$was_svc'"; fi
      sleep 3
    fi
    if [ "$(a settings get secure accessibility_enabled)" != "$was_on" ]; then
      if [ "$was_on" = null ]; then a settings delete secure accessibility_enabled >/dev/null
      else a "settings put secure accessibility_enabled '$was_on'"; fi
    fi
  }
  at_exit backstop
fi

out=$($M accessibility screen_reader on 2>/dev/null) || fail "turning the screen reader on failed: $($M accessibility screen_reader on 2>&1)"
echo "$out" | grep -q "^screen_reader on (was off)" || fail "the answer was not on, from off: $out"
if [ "$KIND" = phone ]; then
  [ "$($M accessibility screen_reader)" = "screen_reader on" ] || fail "VoiceOver does not read back on"
  row "on" "VoiceOver, as WebDriverAgent reports it"
else
  [ "$(talkback)" -ge 1 ] && [ "$(explore)" = true ] || fail "Mobium said on, and TalkBack is not bound with touch exploration"
  row "on" "TalkBack bound, touch exploration on, read from dumpsys"
fi

# A tap with it on. TalkBack's own prompt may come up over the app first.
$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
$M scroll-to 'label="Form Demo"' --direction down >/dev/null 2>&1 || true
$M tap 'label="Form Demo"' >/dev/null
$M wait 'label=Pro' >/dev/null
$M tap 'label=Pro' >/dev/null || fail "the tap on Pro was refused with the screen reader on"
$M map 2>&1 | grep -q 'Pro (radio, checked)' || fail "the tap on Pro did not select it"
$M tap 'label=Free' >/dev/null
row "tap" "lands with it on: Pro selected, Free put back"

$M daemon stop >/dev/null
if [ "$KIND" = phone ]; then
  got=$(MOBIUM_SESSION=srsetting-read $M accessibility screen_reader 2>/dev/null); MOBIUM_SESSION=srsetting-read $M daemon stop >/dev/null 2>&1 || true
  [ "$got" = "screen_reader off" ] || fail "after the session VoiceOver reads '$got'"
  row "restored" "VoiceOver off when the session ended"
else
  i=0; until [ "$(explore)" != true ]; do i=$((i + 1)); [ $i -lt 15 ] || fail "touch exploration still on after the session"; sleep 1; done
  sleep 3
  [ "$(a settings get secure enabled_accessibility_services)" = "$was_svc" ] || fail "enabled services not as found: $(a settings get secure enabled_accessibility_services)"
  [ "$(a settings get secure accessibility_enabled)" = "$was_on" ] || fail "accessibility_enabled is $(a settings get secure accessibility_enabled), found $was_on"
  [ "$(talkback)" = 0 ] || fail "TalkBack still bound after the session"
  row "restored" "both keys as found, TalkBack unbound, three seconds on"
fi
echo PASS
