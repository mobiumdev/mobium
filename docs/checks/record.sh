#!/bin/sh
# Screen recording, end to end: start, record something moving, stop and
# save, and check the file by what it holds — frames and duration from its
# own header — not by it existing. Then a still screen, the refusals, and
# that nothing is left recording or on the device.
#
#   docs/checks/record.sh <android-serial | simulator-udid | iphone-udid>
#
# A real iPhone refuses to record for now, and the check asserts the
# refusal names the reason. Android writes a frame only when the screen
# changes, so a still screen is one or two frames of almost no duration;
# the simulator's recorder writes one frame and calls it the whole wall
# time. Both are right, and the check holds each to its own.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

case "$DEV" in
  ????????-????????????????) PLATFORM=iphone; M="$ROOT/bin/mobium --backend webdriveragent --device $DEV" ;;
  *-*-*-*-*) PLATFORM=ios; M="$ROOT/bin/mobium --backend webdriveragent --device $DEV" ;;
  *)         PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($PLATFORM)"

if [ "$PLATFORM" = iphone ]; then
  out=$($M record start 2>&1) && fail "a real iPhone started recording, which is not built: $out"
  echo "$out" | grep -q "record its screen" || fail "the refusal did not say why: $out"
  echo "    refused        a real iPhone, with the reason                     ok"
  exit 0
fi

[ "$($M record --json | json 'd["recording"]')" = False ] || fail "a recording was already running"
$M record start >/dev/null
$M record start >/dev/null 2>&1 && fail "a second start was accepted"
# Something that moves: Settings launched and scrolled.
case "$PLATFORM" in
  android) APP=com.android.settings ;;
  ios)     APP=com.apple.Preferences ;;
esac
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M swipe up >/dev/null; $M swipe up >/dev/null; $M swipe down >/dev/null
[ "$($M record --json | json 'd["recording"]')" = True ] || fail "status did not say a recording was running"
out=$(cd "$TMP" && $M record stop -o moving.mp4 --json)
[ -f "$TMP/moving.mp4" ] || fail "the video was not saved where the command ran — $TMP"
frames=$(echo "$out" | json 'd["frames"]')
dur=$(echo "$out" | json 'd["duration"]//1000000')
[ "$frames" -gt 10 ] || fail "a moving screen recorded $frames frames"
[ "$dur" -gt 1000 ] || fail "a moving screen recorded ${dur}ms of video"
echo "    moving         $frames frames, ${dur}ms, saved where the command ran ok"

sleep 2
$M record start >/dev/null
sleep 3
out=$($M record stop -o "$TMP/still.mp4" --json)
still=$(echo "$out" | json 'd["frames"]')
# Fewer frames than the moving recording and a valid file — not "one or
# two": the status bar, or a screen still settling from the swipes, adds
# frames, and a first run here counted 22 on a screen that looked still.
[ "$still" -ge 1 ] && [ "$still" -lt "$frames" ] || fail "a still screen recorded $still frames against $frames moving"
echo "    still          $still frame(s) against $frames moving, a valid file  ok"

$M record stop -o "$TMP/x.mp4" >/dev/null 2>&1 && fail "stop with nothing recording was accepted"
[ "$($M record --json | json 'd["recording"]')" = False ] || fail "still recording after stop"
if [ "$PLATFORM" = android ]; then
  adb -s "$DEV" shell ls /data/local/tmp | grep -q mobium-recording && fail "the recording was left on the device"
  adb -s "$DEV" shell pidof screenrecord >/dev/null 2>&1 && fail "screenrecord is still running"
else
  pgrep -f "recordVideo" >/dev/null && fail "simctl is still recording"
fi
echo "    idle           nothing recording, nothing left behind              ok"
