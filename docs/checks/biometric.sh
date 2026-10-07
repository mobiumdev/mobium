#!/bin/sh
# Biometrics, end to end, against an app that signs in with them.
#
# A face or finger is only as real as the prompt that takes it, so each
# answer mobium gives is held to MobiumApp's Biometrics Demo, which says what
# the platform told it: enrolled or not, and each sign-in's outcome.
#
#   docs/checks/biometric.sh <emulator-serial>
#   docs/checks/biometric.sh <simulator-udid>      # Face ID or Touch ID model
#   docs/checks/biometric.sh <android-serial | iphone-udid>   # the refusal
#
# Both: enrollment reaches the running app; a match with nothing enrolled, and
# one with no prompt up, are refused rather than sent to nothing; a stranger
# is not recognized and the enrolled face or finger signs in.
#
# Emulator: enrolling walks Settings, setting PIN 1111 when there is no screen
# lock, and strangers lock the sensor out — on Android 15 on the fifth on a
# fresh emulator and sooner after earlier failures, which carry over between
# prompts; on Android 17 on the second. Android 15's app hears lockout;
# Android 17's prompt asks for the PIN instead, and backing out of it reaches
# the app as a cancel. The enrollment is put back as it was: an
# emulator that had none ends with none and no PIN.
#
# Face ID simulator: a second stranger to the Not Recognized alert is refused,
# and Cancel reaches the app as user_cancel. Touch ID simulator: the third
# stranger closes the prompt, and the app hears authentication_failed.
#
# A real phone is refused, status included, and nothing on it is touched.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-14s %-60s ok\n' "$1" "$2"; }

case "$DEV" in
  ????????-????????????????) KIND=phone; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *-*-*-*-*) KIND=simulator; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  emulator-*) KIND=emulator; M="$ROOT/bin/mobium --device $DEV" ;;
  *) KIND=phone; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($KIND)"

# expect <exit status> <words> <command...>: the command fails with that
# status and says those words.
expect() {
  want="$1"; words="$2"; shift 2
  set +e; out=$("$@" 2>&1); status=$?; set -e
  [ "$status" = "$want" ] || fail "$* exited $status, want $want: $out"
  echo "$out" | grep -q "$words" || fail "$* did not say \"$words\": $out"
}

if [ "$KIND" = phone ]; then
  expect 5 "nothing outside a real phone" $M biometric
  expect 5 "nothing outside a real phone" $M biometric match
  row "refused" "unsupported, status too, and says why"
  echo PASS; exit 0
fi

APP=dev.mobium.mobiumapp
# says <words>: the Demo shows them within five seconds.
says() {
  i=0
  until $M text | grep -q "$1"; do
    i=$((i + 1)); [ $i -lt 10 ] || fail "the app never said \"$1\": $($M text | grep -E 'enrolled|outcome' | tr '\n' ' ')"
    sleep 0.5
  done
}
signin() { $M tap testid=bioSignIn >/dev/null; sleep 1.5; }

was=$($M biometric)
$M biometric unenroll >/dev/null
restore() {
  case "$was" in
    "no "*) $M biometric unenroll >/dev/null 2>&1 || true ;;
    *) $M biometric enroll >/dev/null 2>&1 || true ;;
  esac
}
at_exit restore

$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
$M tap "label=Biometrics Demo" >/dev/null
says "enrolled: no"
kind=$($M biometric | sed -n 's/^no \(.*\) enrolled.*/\1/p')
[ -n "$kind" ] || fail "status did not name a face or fingerprint: $($M biometric)"
expect 3 "enroll one first" $M biometric match
row "not enrolled" "the app says so, and a $kind match is refused"

out=$($M biometric enroll)
echo "$out" | grep -q "enrolled" || fail "enroll: $out"
if [ "$KIND" = emulator ]; then
  echo "$out" | grep -q "PIN 1111" || fail "enroll did not say it set a PIN: $out"
  $M tap "label=Biometrics Demo" >/dev/null 2>&1 || true
fi
says "enrolled: yes"
row "enrolled" "the running app heard it"

expect 3 "nothing" $M biometric match
row "no prompt" "a $kind shown to nothing is refused"

# signs_in: the enrolled face or finger signs in. An emulator's sensor may
# be locked out by failures an earlier run left, and the touch that finds it
# so is answered locked out; then it waits and signs in again.
attempt=0
signs_in() {
  tries=0
  while :; do
    tries=$((tries + 1)); attempt=$((attempt + 1)); signin
    out=$($M biometric match 2>&1 || true)
    echo "$out" | grep -q "accepted" && break
    [ "$KIND" = emulator ] && [ $tries -lt 4 ] || fail "the enrolled $kind was not accepted: $out"
    sleep 15
  done
  says "#$attempt: success"
}
signs_in
row "match" "accepted, and the app signed in"
if [ "$KIND" = simulator ]; then
  # A stranger, then the enrolled one, on one prompt.
  signin
  $M biometric nomatch | grep -q "not recognized" || fail "a stranger was not reported as not recognized"
  attempt=$((attempt + 1))
  out=$($M biometric match 2>&1 || true)
  echo "$out" | grep -q "accepted" || fail "the enrolled $kind was not accepted after a stranger: $out"
  says "#$attempt: success"
  row "stranger" "not recognized, then the $kind accepted on that prompt"
fi

next=$((attempt + 1))
if [ "$KIND" = simulator ] && [ "$kind" = face ]; then
  signin
  $M biometric nomatch >/dev/null
  expect 3 "Not Recognized" $M biometric nomatch
  $M tap label=Cancel >/dev/null
  says "#$next: failed (user_cancel)"
  row "retry" "a second stranger refused; Cancel reached the app"
elif [ "$KIND" = simulator ]; then
  signin
  $M biometric nomatch | grep -q "not recognized" || fail "first stranger"
  $M biometric nomatch >/dev/null
  $M biometric nomatch | grep -q ": failed" || fail "the third stranger did not close the prompt"
  says "#$next: failed (authentication_failed)"
  row "three" "Touch ID gave up on the third, and the app heard it"
else
  # Strangers until the sensor locks out. How many that takes depends on
  # failures earlier prompts left — they carry over, and on an emulator that
  # has been used a while it is the second — so the count is not asserted:
  # each is not recognized until one is locked out, and the app hears it.
  signin
  n=0; last=""
  while [ $n -lt 6 ]; do
    n=$((n + 1)); last=$($M biometric nomatch 2>&1 || true)
    echo "$last" | grep -q "not recognized" || break
  done
  echo "$last" | grep -q "locked out" || fail "no lockout after $n strangers: $last"
  # Android 15 ends the prompt and the app hears lockout. Android 17 keeps
  # the prompt and asks for the PIN "to recover biometrics"; backing out of
  # it is how a person declines, and the app hears the cancel.
  i=0
  until [ "$($M current)" = "$APP" ] || $M text | grep -q "PIN"; do
    i=$((i + 1)); [ $i -lt 10 ] || break
    sleep 0.5
  done
  if [ "$($M current)" = "$APP" ]; then
    says "#$next: failed (lockout)"
    row "lockout" "locked out on stranger $n, and the app heard lockout"
  else
    $M text | grep -q "PIN" || fail "locked out, and neither the app nor a PIN prompt is in front: $($M current)"
    $M press back >/dev/null
    says "#$next: failed (user_cancel)"
    row "lockout" "locked out on stranger $n; the prompt asked for the PIN, and back reached the app"
  fi
  # And after it, the enrolled finger signs in again.
  attempt=$next
  sleep 15
  signs_in
  row "after" "the enrolled finger accepted once the lockout ended"
fi

at_exit_clear
restore
case "$was" in
  "no "*) now=$($M biometric); case "$now" in "no "*) ;; *) fail "not put back: $now" ;; esac
          row "put back" "unenrolled, as it was" ;;
  *) row "put back" "enrolled, as it was" ;;
esac
$M terminate $APP >/dev/null 2>&1 || true
echo PASS
