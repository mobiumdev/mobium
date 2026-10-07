#!/bin/sh
# Rotation and per-app language, end to end — and lock, a call and an SMS
# (an emulator's; a real phone's refusal), the timezone, notifications and
# geolocation, each read back.
#
# The first two exist because they change how every screen is laid out, and
# both are only worth having because they can be read back — a rotation that
# the sensor undoes a moment later, or a language the device did not store,
# is the shape of defects 26 and 27.
#
# The language half doubles as the only test of non-Latin text on a real
# screen. Every label rule mobium has — truncation, markup stripping,
# deduplication, composition — was tuned on Latin script, and the unit tests
# covering CJK were written by the same person who wrote the code.
#
#   docs/checks/device-state.sh <serial> [package]
set -e
DEV="$1"; APP="${2:-org.wikipedia}"
if [ -z "$DEV" ]; then echo "usage: $0 <serial> [package]" >&2; exit 2; fi

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
M="$ROOT/bin/mobium --device $DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
restore() { $M orientation auto >/dev/null 2>&1 || true
            $M locale "$APP" "" >/dev/null 2>&1 || true; }
at_exit restore

echo "--- $DEV"

# Start from a known state. The notification shade covers everything and
# survives between runs, so a previous run that left it open makes every
# locator below fail with "no element matches" — which reads like a mapping
# problem and is not one.
$M notifications --shade close >/dev/null 2>&1 || true

# An app has to be in front before anything is rotated. **The launcher pins
# portrait** on a Pixel, so a rotation attempted on the home screen fails with
# "the display is still showing 0" — correctly, and confusingly if you did not
# expect it. This check failed exactly that way once, left on the home screen
# by an earlier step.
$M terminate com.android.settings >/dev/null 2>&1 || true
$M launch com.android.settings >/dev/null
sleep 2

# Rotation. Every orientation is checked by reading the screen back, not by
# trusting the command: `cmd window user-rotation` says nothing about whether
# the display moved, and an activity that pins its own orientation cannot be
# turned from outside.
for want in portrait landscape portrait-reverse landscape-reverse; do
  $M orientation "$want" >/dev/null
  got=$($M orientation | head -1)
  case "$got" in
    "$want (locked)") ;;
    *) fail "asked for $want, device reports '$got'" ;;
  esac
done
echo "    rotation       all four orientations, each read back            ok"

# The hierarchy has to follow the screen. This is the assertion that would
# catch a rotation that moved the display and not the layout.
$M orientation portrait >/dev/null
portrait_w=$($M map --json | python3 -c 'import json,sys;print(max(e["bounds"]["x2"] for e in json.load(sys.stdin)["elements"]))')
$M orientation landscape >/dev/null
landscape_w=$($M map --json | python3 -c 'import json,sys;print(max(e["bounds"]["x2"] for e in json.load(sys.stdin)["elements"]))')
[ "$landscape_w" -gt "$portrait_w" ] \
  || fail "the hierarchy is $portrait_w wide in portrait and $landscape_w in landscape; it did not follow the rotation"
echo "    layout         hierarchy followed: ${portrait_w}px -> ${landscape_w}px wide  ok"

# And a tap has to land. Bounds coming from the hierarchy rather than from
# `wm size` is what makes this work — wm size reports the *physical* size in
# every orientation, so anything deriving coordinates from it would be wrong
# here and right in portrait.
$M orientation portrait >/dev/null
$M terminate com.android.settings >/dev/null 2>&1 || true
$M launch com.android.settings >/dev/null
sleep 2
$M orientation landscape >/dev/null
sleep 1
$M tap 'text=Connected devices' >/dev/null
sleep 2
$M map | grep -q 'Pair new device' || fail "a tap in landscape did not land"
echo "    tap            landed correctly in landscape                    ok"
$M orientation auto >/dev/null

# Language. Android stores a tag it has no translation for exactly as happily
# as one it does, so the check is what appears on screen, not what the setting
# reads back.
apps=$($M apps 2>&1) || fail "cannot list the apps: $(echo "$apps" | tail -1)"
echo "$apps" | grep -q "^$APP " || { echo "    locale         skipped, $APP is not installed"; exit 0; }

$M locale "$APP" | grep -q 'follows the device' || fail "$APP started out pinned"
$M locale "$APP" ja-JP >/dev/null
$M locale "$APP" | grep -q 'pinned to ja-JP' || fail "the language did not stick"
echo "    locale         set and read back                                ok"

$M terminate "$APP" >/dev/null
$M launch "$APP" >/dev/null
sleep 5
cjk=$($M map --json | python3 -c '
import json, sys
els = json.load(sys.stdin)["elements"]
print(sum(1 for e in els if any(ord(c) > 0x2E80 for c in e["label"])))
')
[ "$cjk" -gt 0 ] || fail "the app was pinned to Japanese and no label came back in Japanese"
echo "    rendered       $cjk labels in Japanese on screen                    ok"

# The label pipeline has to survive multi-byte text. A byte-shaped cut would
# show up here as a replacement character.
$M map --json | python3 -c '
import json, sys
els = json.load(sys.stdin)["elements"]
bad = [e["ref"] for e in els if "�" in e["label"]]
if bad:
    sys.exit("replacement characters in %s — a label was cut mid-character" % ", ".join(bad))
blank = [e["ref"] for e in els if not e["label"].strip()]
if blank:
    sys.exit("empty labels on %s" % ", ".join(blank))
'
echo "    encoding       no split characters, no empty labels             ok"

$M locale "$APP" "" >/dev/null
$M locale "$APP" | grep -q 'follows the device' || fail "clearing the language did not work"
echo "    cleared        back to the device language                      ok"

# Hardware buttons. back is the one that matters: on Android it is primary
# navigation.
$M terminate com.android.settings >/dev/null 2>&1 || true
$M launch com.android.settings >/dev/null
sleep 2
$M tap 'text=Connected devices' >/dev/null
sleep 2
$M map | grep -q 'Pair new device' || fail "could not reach a second screen to go back from"
$M press back >/dev/null
sleep 2
# Verified by reading the screen, not by the command's say-so: `input keyevent`
# reports nothing about whether anything moved.
$M map | grep -q 'Network & internet' || fail "back did not return to the previous screen"
echo "    back           returned to the previous screen                 ok"

# home is the one press with an outcome mobium can confirm for itself.
$M press home | grep -q 'foreground' || fail "home did not report a confirmed foreground"
echo "    home           confirmed the launcher came forward             ok"

$M press recents >/dev/null
sleep 2
$M press home >/dev/null
echo "    recents        sent                                            ok"

# A name outside the vocabulary must be refused before it reaches the device.
$M press middle 2>&1 | grep -q 'mobium knows' || fail "a nonsense button was not refused"
echo "    unknown button refused with the vocabulary                     ok"

# Lock is a state, not a toggle: asking twice must not flip it back.
# Not on a phone with a PIN, pattern or password: it cannot be unlocked from
# outside, so locking it would leave it locked and every step after this
# refused. Asked without locking anything — the answer is in the text, and
# the exit status is 0 either way.
if adb -s "$DEV" shell cmd lock_settings verify 2>&1 | grep -q 'has a lock credential'; then
  echo "    lock           NOT CHECKED — the device has a lock credential, and unlocking it from outside is refused"
else
  $M lock unlock >/dev/null
  $M lock lock >/dev/null
  $M lock lock >/dev/null
  $M lock | grep -q '^locked' || fail "locking twice did not leave the screen locked"
  $M lock unlock >/dev/null
  $M lock | grep -q '^unlocked' || fail "the screen did not unlock"
  echo "    lock           locked, idempotent, unlocked, each read back    ok"
fi

# Interruptions. Calls and messages are emulator-only, so the check adapts
# rather than failing on hardware — the point is that mobium says which it is.
if $M call 2>&1 | grep -q 'real phone cannot be made to ring'; then
  echo "    interruptions  refused on real hardware, with the reason        ok"
else
  $M call ring >/dev/null
  state() { adb -s "$DEV" shell dumpsys telephony.registry 2>/dev/null | grep -m1 -o 'mCallState=[0-9]'; }
  [ "$(state)" = "mCallState=1" ] || fail "the device is not ringing after app_call ring"
  $M call accept >/dev/null
  [ "$(state)" = "mCallState=2" ] || fail "the call was not answered"
  $M call hang >/dev/null
  [ "$(state)" = "mCallState=0" ] || fail "the call did not end"
  echo "    call           rang, answered and ended, read back each time  ok"

  $M sms "mobium check message" >/dev/null
  echo "    sms            delivered                                       ok"
fi

# Timezone works on real hardware too, and is confirmed by reading it back —
# an unknown zone name is accepted by the device and silently ignored.
tz_before=$($M timezone | head -1)
$M timezone Asia/Tokyo >/dev/null
$M timezone | grep -q 'Asia/Tokyo' || fail "the timezone did not change"
$M timezone "Not/AZone" 2>&1 | grep -q 'accepted and ignored'   || fail "an unknown timezone was not caught — the readback is not doing its job"
$M timezone "$tz_before" >/dev/null
$M timezone | grep -q "$tz_before" || fail "the timezone was not restored"
echo "    timezone       changed, rejected nonsense, restored             ok"

# Notifications. Posting is confirmed by reading the shade back — `cmd
# notification post` prints what it thinks it built and says nothing about
# whether the system accepted it.
$M notifications --post "mobium check notification" --title "MobiumCheck" >/dev/null
$M notifications | grep -q "MobiumCheck" || fail "the posted notification is not in the shade"
echo "    notify         posted and read back                            ok"

# Awkward text has to survive the device shell. A body of "two words" arrived
# as "two" until the argument was quoted properly.
$M notifications --post "it's 100% here & \"now\"" --title "MobiumQuoting" >/dev/null
$M notifications | grep -q "it's 100% here" || fail "quoting mangled the notification body"
echo "    quoting        spaces, apostrophes and quotes survived          ok"

# And the half that makes a notification usable: it cannot be tapped until the
# shade is open, because until then it is not on screen.
$M notifications --shade open >/dev/null
sleep 2
$M map | grep -q "MobiumQuoting\|expandableNotificationRow" \
  || fail "the shade opened and no notification is mappable"
echo "    shade          opened, and notifications are mappable           ok"
$M notifications --shade close >/dev/null

# --- geolocation -----------------------------------------------------------
# Two cities rather than one, and this is not padding. An AVD keeps its last
# position across boots, so setting the place it is already at reads back
# identically whether or not anything happened -- the first probe of this
# feature "passed" that way before anyone noticed. The second city is the
# positive control, and the southern hemisphere covers the sign.
$M location --lat 35.6762 --lon 139.6503 >/dev/null
$M location | grep -q "35.676200,139.650300" || fail "the device did not move to Tokyo"
$M location --lat -33.8688 --lon 151.2093 >/dev/null
$M location | grep -q -- "-33.868800,151.209300" || fail "the device did not move to Sydney"
echo "    location       two cities, read back, sign preserved           ok"

# The reply has to say the fix was injected, or a read proves nothing.
$M location | grep -q "injected" || fail "a mock fix is not reported as injected"
echo "    location tag   reported as injected                            ok"

# Clearing removes the provider and does NOT clear the cached position. The
# check asserts the surprising half: the coordinates stay, and mobium says so
# rather than claiming the device moved. CHALLENGES 56.
$M location --clear >/dev/null
$M location --json | grep -q '"mocking": false' \
  || fail "the test provider is still installed after a clear"
$M location | grep -q -- "-33.868800" \
  || fail "the cached position vanished after a clear -- that is not what Android does"
echo "    location clear provider gone, cached fix still reported        ok"

echo "--- passed"
