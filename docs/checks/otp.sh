#!/bin/sh
# A one-time code, end to end, on MobiumApp's OTP Demo.
#
#   docs/checks/otp.sh <android-serial | simulator-udid | iphone-udid>
#
# What the screen isolates is what automation finds hard about a second
# factor: six single-digit boxes that move focus on every digit and back on
# delete, and a code that arrives out of band. So this holds Mobium to both.
#
#   - The code is read where it arrives: from the notification shade on
#     Android, and on iOS from the banner, which Mobium reads through
#     (CHALLENGES 155) — a banner over an app once made every read land on
#     SpringBoard for seconds. The screen's own switch is the fallback.
#   - Digit by digit into the boxes, and `entered` must say exactly the code.
#   - The whole code into the first box: Android sets a field's text at once
#     and the screen spreads it; iOS types through the keyboard and the app
#     moves focus as it goes. Either every digit arrives, and `entered` says
#     so, or Mobium says which never arrived — never "iOS dropped a keystroke"
#     with retries that type the code again into the next boxes (156).
#   - The outcomes the app reports: no code, verified, already used, wrong,
#     locked, and expired — the last reached by sending the app to the
#     background past the code's life, so `background` is exercised too.
#   - A control the screen disables — Resend, during its cooldown — is
#     refused, not tapped.
#
# Needs MobiumApp with the OTP Demo installed. About four minutes: the
# expiry waits a minute. With MobiumApp's notifications off the code is read
# from the screen instead, and the row says so. On a phone that is worth
# knowing: a reinstall did not bring the prompt back, as it does on a
# simulator — the iPhone kept the denial — so the switch is in Settings.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-16s %-56s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp

case "$DEV" in
  ????????-????????????????) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *-*-*-*-*) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *) PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($PLATFORM)"

# An iPhone has no key that hides the keyboard; return does, here.
hide() { $M keyboard --hide >/dev/null 2>&1 || $M keyboard --key enter >/dev/null 2>&1 || true; }
out() { $M text testid=otpOutcome; }
entered() { $M text testid=otpEntered | sed 's/^entered: //'; }

open_otp() {
  $M terminate $APP >/dev/null 2>&1 || true
  $M launch $APP >/dev/null
  $M scroll-to "label=OTP Demo" >/dev/null 2>&1 || true
  $M tap "label=OTP Demo" >/dev/null
  $M wait testid=otpSend >/dev/null
}

# The notification prompt, the first time an app posts: answered by pressing
# the button that allows, by its ref — what the button is called is the only
# thing that says what it does (CHALLENGES 106).
allow_notifications() {
  if [ "$PLATFORM" = android ]; then
    $M grant $APP notifications >/dev/null 2>&1 || true
    return
  fi
  if $M alert 2>/dev/null | grep -q 'a dialog is on screen'; then
    ref=$($M map | grep ' Allow (button)' | awk '{print $1}' | head -1)
    [ -n "$ref" ] || fail "a dialog is up and has no Allow button"
    $M tap "$ref" >/dev/null
  fi
}

# send: press Send, and read the code where it arrived — first, because a
# banner is up for about five seconds. With "cooldown", then press it again:
# Resend is disabled for 20 seconds after a send.
send() {
  $M tap testid=otpSend >/dev/null
  allow_notifications
  CODE=""
  i=0
  # The app says when it could not post; there is no banner to wait for.
  case "$(out)" in *"notifications are off"*) i=10 ;; esac
  while [ -z "$CODE" ] && [ $i -lt 10 ]; do
    if [ "$PLATFORM" = android ]; then
      CODE=$($M --json notifications 2>/dev/null | python3 -c "
import json,sys,re
d=json.load(sys.stdin)
t=[n.get('text','') for n in d.get('notifications',[]) if 'MobiumApp code' in n.get('text','')]
m=re.search(r'(\d{6})', t[-1] if t else '')
print(m.group(1) if m else '')")
    else
      CODE=$($M map 2>/dev/null | grep -o 'code is [0-9]\{6\}' | grep -o '[0-9]\{6\}' | head -1)
    fi
    [ -n "$CODE" ] || { sleep 0.5; i=$((i + 1)); }
  done
  if [ -z "$CODE" ]; then
    # The fallback the screen offers: the code on screen.
    $M check testid=otpShowCode >/dev/null
    CODE=$($M text testid=otpCodeShown | grep -o '[0-9]\{6\}')
    VIA="the screen"
  elif [ "$PLATFORM" = android ]; then
    VIA="the notification shade"
  else
    VIA="the banner, read through"
  fi
  [ -n "$CODE" ] || fail "no code arrived"
  # While the banner is up the app is still what is read (CHALLENGES 155).
  $M wait testid=otpOutcome >/dev/null
  if [ "$1" = cooldown ]; then
    set +e; r=$($M tap testid=otpSend 2>&1); set -e
    echo "$r" | grep -q "disabled" || fail "Resend was pressed during its cooldown: $r"
    row "cooldown" "Resend refused while the screen disables it"
  fi
}

wrong() { python3 -c "print(str((int('$CODE') + 1) % 1000000).zfill(6))"; }
verify_field() { $M fill testid=otpField "$1" >/dev/null; hide; $M tap testid=otpVerifyField >/dev/null; }

# --- nothing sent -----------------------------------------------------------
open_otp
$M tap testid=otpVerifyField >/dev/null
[ "$(out)" = "no code sent" ] || fail "before any code the outcome was \"$(out)\""
row "control" "no code sent, and it says so"

# --- the code, read where it arrives -----------------------------------------
# --- and a control the screen disables -------------------------------------
send cooldown
case "$(out)" in sent*) ;; *) fail "after sending, the outcome was \"$(out)\"" ;; esac
row "arrives" "read from $VIA"

# --- digit by digit ---------------------------------------------------------
i=0
while [ $i -lt 6 ]; do
  $M type testid=otpBox$i "$(printf '%s' "$CODE" | cut -c$((i + 1)))" >/dev/null
  i=$((i + 1))
done
hide
[ "$(entered)" = "$CODE" ] || fail "digit by digit, the boxes hold $(entered)"
$M tap testid=otpVerifyBoxes >/dev/null
[ "$(out)" = "verified" ] || fail "the right code gave \"$(out)\""
$M tap testid=otpVerifyBoxes >/dev/null
case "$(out)" in "already used"*) ;; *) fail "a used code gave \"$(out)\"" ;; esac
row "digit by digit" "every box holds its digit; verified, then used"

# --- the whole code into the first box --------------------------------------
open_otp
send
set +e; r=$($M type testid=otpBox0 "$CODE" 2>&1); set -e
hide
got=$(entered)
case "$r" in
  typed*)
    [ "$got" = "$CODE" ] || fail "typing the whole code was reported done, and the boxes hold $got"
    row "whole code" "reported typed, and every box holds its digit" ;;
  *"never arrived"*)
    echo "$r" | grep -q "retrying" && fail "a spread text was retried: $r"
    [ "$got" != "$CODE" ] || fail "reported lost, and the boxes hold the whole code"
    row "whole code" "focus moved as it typed; what never arrived was named" ;;
  *) fail "typing the whole code into the first box: $r" ;;
esac

# --- wrong, then locked -----------------------------------------------------
open_otp
send
W=$(wrong)
verify_field "$W"; [ "$(out)" = "wrong code, 2 attempts left" ] || fail "first wrong code: \"$(out)\""
verify_field "$W"; [ "$(out)" = "wrong code, 1 attempt left" ] || fail "second wrong code: \"$(out)\""
verify_field "$W"; [ "$(out)" = "locked: too many attempts" ] || fail "third wrong code: \"$(out)\""
verify_field "$CODE"; [ "$(out)" = "locked: too many attempts" ] || fail "the right code unlocked it: \"$(out)\""
row "lockout" "two chances counted down, then locked, even for the code"

# --- expired, while the app was away ----------------------------------------
open_otp
send
r=$($M background 62 2>&1) || fail "sending the app away: $r"
[ "$($M text testid=otpExpires)" = "expired" ] || fail "after 62s away the code says \"$($M text testid=otpExpires)\""
verify_field "$CODE"
case "$(out)" in expired*) ;; *) fail "an expired code gave \"$(out)\"" ;; esac
row "expiry" "away 62s past a 60s code: expired, and refused"

$M terminate $APP >/dev/null 2>&1 || true
echo PASS
