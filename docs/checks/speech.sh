#!/bin/sh
# What the screen reader says: `mobium speech`.
#
#   docs/checks/speech.sh <android-serial | iphone-udid | simulator-udid>
#
# TalkBack on Android, moved by a keyboard Android makes for the call and
# read from its log; VoiceOver on an iPhone, through WebDriverAgent. On
# MobiumApp's Screen Reader Demo and Login Demo it checks:
#
#   - with the screen reader off, speech is refused, naming the setting;
#   - a walk hears what the audit only infers: the icon with no name is
#     spoken as a bare "Button", and the Send button as "Submit form" — and
#     on Android "Save button" is said with its kind twice, while VoiceOver
#     drops a label's own kind, so its two Save buttons sound alike and the
#     walk names neither;
#   - a walk ends: on Android back round at the first element, on an iPhone
#     where the cursor goes no further;
#   - a sentence about a password field is never shown: the cursor is put
#     on a typed one, and the answer holds Mobium's stand-in and not the
#     password. TalkBack said "16 characters" and VoiceOver its kind, neither
#     the value, so this shows the stand-in is applied on the device; that
#     it can fail is speech_test.go's;
#   - listening around an action hears what it makes the screen reader say:
#     a wrong sign-in's announcement on both platforms, and on Android the
#     live cart's new count, while the quiet cart's change is never said;
#   - ending the session puts the screen reader back off;
#   - a simulator is refused: its runner cannot reach VoiceOver.
#
# A screen reader takes over touch for whoever holds the device, and on
# Android this sets TalkBack's log output level to VERBOSE — every word it
# says goes to the device log while it is — and sets it back as found: a phone
# only with ALLOW_PHONE=1. Every restore is registered before the change.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-10s %-60s ok\n' "$1" "$2"; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); $1"; }
APP=dev.mobium.mobiumapp
PASSWORD=mobium-check-pw7
export MOBIUM_SESSION="${MOBIUM_SESSION:-speechcheck}"
M="$ROOT/bin/mobium --device $DEV"
case "$DEV" in
  ????????-????????????????) KIND=phone ;;
  *-*-*-*-*) KIND=simulator ;;
  emulator-*) KIND=emulator ;;
  *) KIND=android-phone ;;
esac
echo "--- $DEV ($KIND)"

if [ "$KIND" = simulator ]; then
  out=$($M speech 2>&1) && fail "a simulator's screen reader was read: $out"
  echo "$out" | grep -q "prebuilt release" || fail "the refusal did not say why: $out"
  row "refused" "a simulator, whose runner cannot reach VoiceOver"
  echo PASS; exit 0
fi
case "$KIND" in
  phone | android-phone) [ -n "$ALLOW_PHONE" ] || { echo "$DEV is a phone: a screen reader takes over its owner's touch. ALLOW_PHONE=1 to go ahead" >&2; exit 2; } ;;
esac

# The keys as found, read before any session: while a UiAutomator2 session is
# open Android sets accessibility_enabled to 1 — measured on the Pixel 8 Pro,
# where it is 0 — and a check that read it after its first call expected
# the 1 back and failed on the phone's own 0 (CHALLENGES 353).
if [ "$KIND" != phone ]; then
  a() { adb -s "$DEV" shell "$@" | tr -d '\r'; }
  was_svc=$(a settings get secure enabled_accessibility_services)
  was_on=$(a settings get secure accessibility_enabled)
fi

state=$($M accessibility screen_reader 2>/dev/null) || fail "the screen reader could not be read: $($M accessibility screen_reader 2>&1)"
[ "$state" = "screen_reader off" ] || { echo "the screen reader is already on ($state): left alone" >&2; exit 2; }
out=$($M speech 2>&1) && fail "speech answered with the screen reader off: $out"
echo "$out" | grep -q "screen_reader on" || fail "the refusal did not name the setting: $out"
row "refused" "with the screen reader off, naming screen_reader on"

# The session's restore puts the screen reader back; these are the backstops.
if [ "$KIND" = phone ]; then
  at_exit "$M daemon stop >/dev/null 2>&1; MOBIUM_SESSION=speechcheck-restore $M accessibility screen_reader off >/dev/null 2>&1; MOBIUM_SESSION=speechcheck-restore $M daemon stop >/dev/null 2>&1"
else
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

  # TalkBack's log output level, through its own settings: the level is a
  # preference of TalkBack's that nothing outside it can write.
  log_level_screen() {
    a am start -n com.google.android.marvin.talkback/com.android.talkback.TalkBackPreferencesActivity >/dev/null 2>&1
    $M wait 'text="Advanced settings"' >/dev/null 2>&1 || $M scroll-to 'text="Advanced settings"' --direction down >/dev/null
    $M tap 'text="Advanced settings"' >/dev/null
    $M scroll-to 'text="Developer settings"' --direction down >/dev/null
    $M tap 'text="Developer settings"' >/dev/null
    $M wait 'text="Log output level"' >/dev/null
  }
  log_level() { $M map 2>/dev/null | sed -n 's/.*Log output level \([A-Z]*\) .*/\1/p' | head -1; }
  set_log_level() {
    log_level_screen
    [ "$(log_level)" = "$1" ] && return 0
    $M tap 'text="Log output level"' >/dev/null
    $M tap "text=$1" >/dev/null
    [ "$1" = VERBOSE ] && $M alert --button "Yes, enable verbose logging" >/dev/null
    $M wait 'text="Log output level"' >/dev/null
    [ "$(log_level)" = "$1" ] || fail "TalkBack's log output level reads $(log_level), not $1"
  }
  log_level_screen
  was_level=$(log_level)
  [ -n "$was_level" ] || fail "TalkBack's Developer settings show no log output level"
  at_exit "set_log_level $was_level; $M press home >/dev/null 2>&1 || true"
  set_log_level VERBOSE
  row "verbose" "TalkBack's log output level VERBOSE (found $was_level)"
  # Registered last so it runs first: TalkBack off matters more on a phone
  # than its log level, which is restored after.
  at_exit backstop
fi

# screen_reader_on turns it on, and on Android closes the notification
# prompt the Accessibility Suite raises as TalkBack starts on the Pixel —
# with back, which answers nothing: the permission stays as it was, with no
# user decision recorded. It sat over the app and refused the next action.
screen_reader_on() {
  $M accessibility screen_reader on >/dev/null
  [ "$KIND" = phone ] && return 0
  i=0
  while [ $i -lt 8 ]; do
    if $M alert 2>/dev/null | grep -q "send you notifications"; then
      $M press back >/dev/null
      sleep 1
      $M alert 2>/dev/null | grep -q "send you notifications" && fail "the Accessibility Suite's notification prompt stayed up"
      a dumpsys package com.google.android.marvin.talkback | grep "POST_NOTIFICATIONS: granted" | grep -q USER_SET &&
        fail "closing the notification prompt recorded a decision"
      row "prompt" "the Accessibility Suite's notification prompt closed, answered nothing"
      return 0
    fi
    i=$((i + 1)); sleep 0.5
  done
}

# A password typed with the screen reader off, then the cursor put on it.
$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
$M tap 'label="Login Demo"' >/dev/null
$M type testid=username mobium >/dev/null
$M type testid=password "$PASSWORD" >/dev/null
screen_reader_on
got=$($M speech testid=password --json 2>&1) || fail "the cursor could not be put on the password field: $got"
echo "$got" | grep -q "$PASSWORD" && fail "the password is in what speech answered"
said=$(echo "$got" | json "print(d['steps'][0]['said'])")
case "$said" in
  *"password field, and not shown"*) ;;
  *) fail "what was said about the password field is shown: $said" ;;
esac
row "password" "the cursor on a typed password field: the sentence withheld"

# Listening around a wrong sign-in: the app announces it, on both platforms.
$M speech --listen start >/dev/null
$M tap testid=loginBtn >/dev/null
ann=$($M speech --listen stop --json) || fail "listening around Log in failed: $ann"
echo "$ann" | grep -q "$PASSWORD" && fail "the password is in what listening answered"
if [ "$KIND" = phone ]; then want="any(s['said'] == 'Incorrect username or password.' for s in d['steps'])"
else want="any(s['said'] == 'Incorrect username or password.' and s.get('cause') == 'announcement' for s in d['steps'])"; fi
[ "$(echo "$ann" | json "print($want)")" = True ] || fail "the app's announcement was not heard: $ann"
row "announced" "a wrong sign-in's \"Incorrect username or password.\" heard"

# The Screen Reader Demo, walked. Reached with the screen reader off: with
# VoiceOver on a swipe is refused (CHALLENGES 348).
$M accessibility screen_reader off >/dev/null
$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
$M scroll-to 'label="Screen Reader Demo"' --direction down >/dev/null
$M tap 'label="Screen Reader Demo"' >/dev/null
$M wait testid=spokenOutcome >/dev/null
screen_reader_on
walk=$($M speech --walk --limit 30 --json) || fail "the walk failed: $walk"
said() { echo "$walk" | json "print('\n'.join(s['said'] for s in d['steps']))"; }
# element_of SAID: the locator value of the first step that said it, or -.
element_of() { echo "$walk" | SAID="$1" json "
import os
s = next(s for s in d['steps'] if s['said'] == os.environ['SAID'])
print(((s.get('element') or {}).get('locator') or {}).get('value', '-'))"; }
said | grep -qx "Button" || fail "the icon with no name was not spoken as a bare Button: $(said | tr '\n' '|')"
said | grep -q "^Submit form" || fail "the Send button was not spoken as Submit form: $(said | tr '\n' '|')"
said | grep -q "^Send message" || fail "the matching Send button was not spoken: $(said | tr '\n' '|')"
row "spoken" "the nameless icon as \"Button\", Send as \"Submit form\""
ended=$(echo "$walk" | json "print(d.get('ended', ''))")
if [ "$KIND" = phone ]; then
  [ "$ended" = end ] || fail "the walk ended '$ended', not where the cursor went no further"
  [ "$(said | grep -cx 'Save Button')" = 2 ] || fail "VoiceOver did not say both Save buttons alike: $(said | tr '\n' '|')"
  [ "$(element_of 'Save Button')" = - ] || fail "a Save Button was paired with one of two elements that say it"
  row "alike" "both Save buttons said \"Save Button\", and neither named"
else
  [ "$ended" = wrapped ] || fail "the walk ended '$ended', not back round at the first element"
  said | grep -qx "Save button. Button" || fail "Save button was not said with its kind twice: $(said | tr '\n' '|')"
  [ "$(element_of 'Button')" = iconUnnamed ] || fail "the bare Button was not paired with iconUnnamed"
  row "paired" "each sentence beside its element, iconUnnamed's \"Button\" too"
fi
row "ended" "the walk ended $ended"

# Listening around a tap: the live region's count is said, the quiet one's
# never is. On an iPhone neither is: React Native's accessibilityLiveRegion
# is Android's alone, so MobiumApp's live cart is as silent there as its
# quiet one — the app's gap, heard as it is.
heard() { $M speech --listen start >/dev/null; $M tap "testid=$1" >/dev/null; $M speech --listen stop --json; }
if [ "$KIND" = phone ]; then
  live=$(heard addLive) || fail "listening around Add aloud failed: $live"
  [ "$($M text testid=liveStatus)" = "Live cart: 1" ] || fail "Add aloud did not count: $($M text testid=liveStatus)"
  echo "$live" | grep -q "Live cart: 1" && fail "the live cart's change was said on iOS, where React Native has no live region: $live"
  row "silent" "\"Live cart: 1\" never said: React Native's live region is Android's"
else
  quiet=$(heard addQuiet) || fail "listening around Add quietly failed: $quiet"
  echo "$quiet" | grep -q "Quiet cart: 1" && fail "the quiet cart's change was said: $quiet"
  live=$(heard addLive) || fail "listening around Add aloud failed: $live"
  [ "$(echo "$live" | json "print(any(s['said'] == 'Live cart: 1' and s.get('cause') == 'change' for s in d['steps']))")" = True ] ||
    fail "the live cart's change was not heard as a change on screen: $live"
  row "listened" "\"Live cart: 1\" said as a change; the quiet cart's never"
fi

$M daemon stop >/dev/null
if [ "$KIND" = phone ]; then
  got=$(MOBIUM_SESSION=speechcheck-read $M accessibility screen_reader 2>/dev/null); MOBIUM_SESSION=speechcheck-read $M daemon stop >/dev/null 2>&1 || true
else
  sleep 3
  got=$(MOBIUM_SESSION=speechcheck-read $M accessibility screen_reader 2>/dev/null)
  MOBIUM_SESSION=speechcheck-read $M daemon stop >/dev/null 2>&1 || true
fi
[ "$got" = "screen_reader off" ] || fail "after the session the screen reader reads '$got'"
row "restored" "the screen reader off when the session ended"
echo PASS
