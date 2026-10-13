#!/bin/sh
# Does getevent see a touch made on an Android emulator itself, and only
# those? A Mobium tap (injected above the kernel) and a touch through the
# emulator's virtual hardware (the console's `event mouse`, standing in for
# a finger) on MobiumApp's Table Demo. Positive controls: the app says what
# it received, and Show taps puts a dot in a screen recording. sendevent
# would be a closer stand-in and is refused: the shell can read the node,
# not write it. ROADMAP, "A recording as code", item 6.
#
#   docs/probes/touch-android.sh <out-dir>
D=emulator-5554
OUT="$1"; mkdir -p "$OUT"
export MOBIUM_HOME=/tmp/mbm-t
M="$HOME/mobium-public/bin/mobium --device $D"
A="adb -s $D"
DEVNODE=/dev/input/event1
APP=dev.mobium.mobiumapp

before=$($A shell settings get system show_touches | tr -d '\r')
restore() {
  if [ "$before" = null ]; then $A shell settings delete system show_touches >/dev/null
  else $A shell settings put system show_touches "$before"; fi
  $A shell rm -f /sdcard/touchprobe.mp4
  $M daemon stop >/dev/null 2>&1 || true
}
trap restore EXIT

$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
$M scroll-to 'text="Table Demo"' >/dev/null 2>&1 || true
$M tap 'text="Table Demo"' >/dev/null
$M wait testid=tableOutcome >/dev/null
$M --json find 'label=More within "Item 2"' > "$OUT/target.json"
read x1 y1 x2 y2 <<EOF
$(python3 -c 'import json,sys; b=json.load(open(sys.argv[1]))["elements"][0]["bounds"]; print(b["x1"],b["y1"],b["x2"],b["y2"])' "$OUT/target.json")
EOF
cx=$(( (x1+x2)/2 )); cy=$(( (y1+y2)/2 ))
echo "target More/Item 2 bounds [$x1,$y1][$x2,$y2] center $cx,$cy"

# Raw units, to compare with what getevent reports: 0 to 32767 on this
# emulator, which dumpsys input scales to the display.
rx=$(( cx * 32767 / 1080 )); ry=$(( cy * 32767 / 2400 ))
echo "raw $rx,$ry"

$A shell settings put system show_touches 1
$A shell screenrecord --time-limit 12 /sdcard/touchprobe.mp4 &
REC=$!
$A shell getevent -lt > "$OUT/getevent.txt" &
GE=$!
sleep 2

echo "--- 1. a mobium tap (injected above the kernel) on More in Item 1"
$M tap 'label=More within "Item 1"' >/dev/null
sleep 1
echo "app: $($M text testid=tableOutcome)"
sleep 1

echo "--- 2. a kernel-level tap on More in Item 2"
$A emu event mouse $cx $cy 0 1 >/dev/null
sleep 0.1
$A emu event mouse $cx $cy 0 0 >/dev/null
sleep 1
echo "app: $($M text testid=tableOutcome)"

sleep 3
kill $GE 2>/dev/null; pkill -f "getevent -lt" 2>/dev/null
wait $REC 2>/dev/null || true
$A pull /sdcard/touchprobe.mp4 "$OUT/touchprobe.mp4" >/dev/null
echo "--- getevent saw $(grep -c ABS_MT_TRACKING_ID "$OUT/getevent.txt") tracking-id lines"
grep -E "TRACKING_ID|POSITION" "$OUT/getevent.txt" || true
