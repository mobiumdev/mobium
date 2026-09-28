#!/bin/sh
# Shake, end to end, against something that reacts to one.
#
# A shake is only as real as the detector that takes it for one, so each
# platform is checked against one, before and after — and against a shake
# that should do nothing, or the reaction proves nothing.
#
#   docs/checks/shake.sh <emulator-serial>
#   docs/checks/shake.sh <simulator-udid>
#   docs/checks/shake.sh <android-serial | iphone-udid>   # the refusal
#
# iOS simulator: Shake to Undo. Type into MobiumApp's Login Demo, shake, and
# iOS raises its own "Undo Typing" alert; shake with nothing typed and it
# raises none. Needs MobiumApp installed.
#
# Android emulator: stock Android reacts to no shake, so the detector is in a
# page. Chrome opens https://example.com (so this needs the network), a
# script listens to devicemotion and counts shakes the way Square's seismic
# library does — most readings over 13 m/s² for a quarter second — and
# reversals of the sideways reading. Still: none. After a shake: some.
#
# A real phone is refused, and says so: nothing outside it can shake it.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-14s %-56s ok\n' "$1" "$2"; }

case "$DEV" in
  ????????-????????????????) KIND=phone; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *-*-*-*-*) KIND=simulator; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  emulator-*) KIND=emulator; M="$ROOT/bin/mobium --device $DEV" ;;
  *) KIND=phone; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($KIND)"

if [ "$KIND" = phone ]; then
  set +e; out=$($M shake 2>&1); status=$?; set -e
  [ "$status" = 5 ] || fail "a phone was not refused as unsupported (exit $status): $out"
  echo "$out" | grep -q "nothing outside" || fail "the refusal does not say why: $out"
  row "refused" "unsupported, and says nothing outside a phone can shake it"
  echo PASS; exit 0
fi

if [ "$KIND" = simulator ]; then
  APP=dev.mobium.mobiumapp
  login() { $M terminate $APP >/dev/null 2>&1 || true; $M launch $APP >/dev/null; $M tap "label=Login Demo" >/dev/null; sleep 2; }
  login
  $M shake >/dev/null; sleep 2
  $M alert | grep -q 'no dialog' || fail "a shake with nothing typed raised a dialog"
  row "control" "nothing typed: no dialog"
  $M type testid=username "shake me" >/dev/null
  $M alert | grep -q 'no dialog' || fail "a dialog was up before the shake"
  $M shake >/dev/null; sleep 2
  $M alert | grep -q 'Undo Typing' || fail "no Undo Typing after a shake: $($M alert | head -1)"
  $M alert dismiss >/dev/null
  row "undo" "typed, shaken: iOS raised Undo Typing"
  echo PASS; exit 0
fi

# Android emulator.
T=$(date +%s)
$M open "https://example.com/?shake=$T" >/dev/null
sleep 3
CTX=$($M contexts | grep "shake=$T" | awk '{print $1}' | head -1)
[ -n "$CTX" ] || fail "Chrome's page did not appear as a context (is the emulator online?)"
$M context "$CTX" >/dev/null
$M eval '
window.__s={readings:0,shakes:0,flips:0,q:[],sign:0};
window.addEventListener("devicemotion", e=>{ const a=e.accelerationIncludingGravity; if(!a) return; __s.readings++;
  const t=performance.now(), g=Math.hypot(a.x,a.y,a.z); const q=__s.q; q.push([t,g>13]);
  while(q.length && t-q[0][0]>500) q.shift();
  if(q.length>=4 && t-q[0][0]>=250 && q.filter(s=>s[1]).length>=0.75*q.length){ __s.shakes++; q.length=0; }
  const sign=a.x>13?1:a.x<-13?-1:0; if(sign && sign!==__s.sign){ if(__s.sign) __s.flips++; __s.sign=sign; } });
"on"' >/dev/null
count() { $M eval "__s.$1"; }
sleep 2
[ "$(count readings)" -gt 0 ] || fail "the page heard no accelerometer at all"
[ "$(count shakes)" = 0 ] && [ "$(count flips)" = 0 ] || fail "the detector fired while the emulator was still"
row "control" "still: $(count readings) readings, no shake"
$M context NATIVE_APP >/dev/null
$M shake >/dev/null
$M context "$CTX" >/dev/null
sleep 1
s=$(count shakes); f=$(count flips)
[ "$s" -gt 0 ] && [ "$f" -gt 3 ] || fail "after a shake the detector saw $s shakes and $f reversals"
row "shaken" "$s shakes, $f sideways reversals"
sleep 2
[ "$(count shakes)" = "$s" ] || fail "the detector went on firing after the shake: the accelerometer is not at rest"
row "at rest" "nothing more after it"
$M context NATIVE_APP >/dev/null
echo PASS
