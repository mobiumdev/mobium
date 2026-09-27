#!/bin/sh
# The third-party driver protocol, end to end against a real Android device.
#
# What this proves is not "the code runs". It is that a driver written from
# docs/decisions/0003 alone, in a different language, importing nothing from
# this repository, sees the same screen mobium's own backend sees. The
# load-bearing step is the diff: `examples/drivers/mobium-driver-adb` covers
# deliberately the same ground as --driver uiautomator, so the two maps can be
# compared on one screen. A driver that returns something plausible but wrong
# passes every check except that one.
#
# Usage: docs/checks/external-driver.sh <serial>
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial>   (see: mobium devices)" >&2; exit 2; fi

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium --device $DEV"
MOBIUM_DRIVER_ADB="$ROOT/examples/drivers/mobium-driver-adb"
export MOBIUM_DRIVER_ADB
[ -x "$MOBIUM_DRIVER_ADB" ] || { echo "the reference driver is not executable" >&2; exit 1; }

# The daemon looks the driver up, in the environment it started with. One
# left running by an earlier check never sees the variable above, and every
# command below fails with "no mobium-driver-adb on your PATH".
"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

echo "--- $DEV"

# A known screen both backends can read. Settings' root has no ticking clock;
# its About page does, which is why that one is not used here (defect 25).
$M --driver adb terminate com.android.settings >/dev/null
$M --driver adb launch com.android.settings >/dev/null

# The daemon holds one session per device, so the backend is switched with the
# daemon stopped. Leaving it up would hand the second map the first backend's
# cached session and the diff would be comparing a backend against itself —
# which would pass.
$M --driver adb map | grep '^@' > "$TMP/external" || fail "the external driver could not map"
"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true
$M --driver uiautomator map | grep '^@' > "$TMP/builtin" || fail "the built-in backend could not map"
"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true

n=$(wc -l < "$TMP/external" | tr -d ' ')
[ "$n" -gt 3 ] || fail "the external driver mapped only $n elements; the screen is wrong"
if diff -u "$TMP/builtin" "$TMP/external" > "$TMP/diff"; then
  echo "    map            $n elements, identical to --driver uiautomator  ok"
else
  cat "$TMP/diff" >&2
  fail "the two backends disagree about the same screen"
fi

# Gestures, advertised. scroll-to needs several swipes and re-reads the
# hierarchy between each, so it exercises the pipe under repetition.
$M --driver adb scroll-to 'text=About' >/dev/null || fail "scroll-to through the driver"
echo "    scroll-to      reached an element below the fold                ok"

# A tap that changes the screen, verified by reading the screen rather than by
# the command's own say-so.
#
# Settings is relaunched first: the scroll above left the list at the bottom,
# and scrolling is downward-only, so a target above the fold would be
# unreachable. Settings also resumes wherever it was last left, so terminating
# is what actually resets it — launching alone does not.
$M --driver adb terminate com.android.settings >/dev/null
$M --driver adb launch com.android.settings >/dev/null
$M --driver adb tap 'text=Connected devices' >/dev/null
$M --driver adb map | grep -q 'Pair new device' || fail "the tap did not open Connected devices"
echo "    tap            opened Connected devices, confirmed by re-reading ok"

# Screenshot: a real PNG of a real size, not an error message where the image goes.
$M --driver adb screenshot -o "$TMP/shot.png" >/dev/null
# Compared as bytes. `head -c 8 … | grep -q PNG` looks like it works and does
# not: grep skips binary input on several implementations, so a perfectly good
# PNG failed this check while `file` reported it correctly.
magic=$(head -c 4 "$TMP/shot.png" | od -An -tx1 | tr -d ' \n')
[ "$magic" = "89504e47" ] || fail "what came back starts with $magic, which is not a PNG"
size=$(wc -c < "$TMP/shot.png" | tr -d ' ')
[ "$size" -gt 10000 ] || fail "the screenshot is $size bytes, which is too small to be a screen"
echo "    screenshot     $size bytes of PNG                              ok"

# Inventory, advertised.
$M --driver adb apps | grep -q . || fail "listing apps through the driver"
echo "    apps           listed                                          ok"

# And the point of capability negotiation: what the driver did NOT advertise
# must be refused with advice, not attempted and failed.
if $M --driver adb type 'testid=x' hello >/dev/null 2>&1; then
  fail "text entry was attempted against a driver that never advertised it"
fi
$M --driver adb type 'testid=x' hello 2>&1 | grep -q 'cannot type' \
  || fail "the refusal did not explain itself"
echo "    text entry     refused, as never advertised                     ok"

if $M --driver adb appearance >/dev/null 2>&1; then
  fail "appearance was attempted against a driver that never advertised it"
fi
echo "    appearance     refused, as never advertised                     ok"

# A driver nobody installed must say where it looked.
if out=$(env -u MOBIUM_DRIVER_ADB PATH=/usr/bin:/bin "$ROOT/bin/mobium" --driver nosuch map 2>&1); then
  fail "a backend with no driver behind it succeeded"
fi
echo "$out" | grep -q 'mobium-driver-nosuch' || fail "the missing-driver message does not name what it looked for"
echo "    missing driver named mobium-driver-nosuch and PATH             ok"

"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true
echo "--- passed"
