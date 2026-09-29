#!/bin/sh
# app_battery, held to what the device itself says.
#
#   docs/checks/battery.sh <emulator-serial | simulator-udid>
#
# mobium reads the battery from outside — dumpsys on Android — and until
# something on the device said what it saw, a level read back was only mobium
# agreeing with itself. MobiumApp's Battery Demo reads the same battery
# through the platform's API, every second, and shows it drawn and as text.
#
# On an emulator the battery is the console's to set, which is the control:
# the level goes to 42 and then to 73, values nobody would guess, and both
# mobium and the app must follow, with the charging state. A simulator has no
# battery; there both must say so rather than invent one. The emulator's
# battery is put back — full, on AC — at the end.
set -e
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
case "$DEV" in
  *-*-*-*-*) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  emulator-*) PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
  *) echo "an emulator or a simulator: a phone's battery cannot be set" >&2; exit 2 ;;
esac
APP=dev.mobium.mobiumapp
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-14s %-58s ok\n' "$1" "$2"; }

"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true
echo "--- $DEV ($PLATFORM)"
$M apps 2>/dev/null | grep -q "$APP" || fail "$APP is not installed"

$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
$M tap testid=batteryBtn >/dev/null || fail "no Battery Demo: this MobiumApp predates it"
$M wait testid=batteryReads --for text --text 'reads: ' >/dev/null

field() { $M --json battery | python3 -c "import json,sys; print(json.load(sys.stdin).get('$1'))"; }
app() { $M text "testid=$1" 2>/dev/null | head -1; }

if [ "$PLATFORM" = ios ]; then
  # No battery to set, and none to read: both say so.
  [ "$(field present)" != "True" ] || fail "mobium reports a battery on a simulator: $($M battery)"
  $M wait testid=batteryLevel --for text --text 'level: none' --timeout 5s >/dev/null \
    || fail "the app shows $(app batteryLevel) on a simulator, which has no battery"
  # With nothing to watch, the app stops reading, rather than count reads of
  # nothing forever.
  n1=$(app batteryReads | sed -n 's/^reads: \([0-9]*\).*/\1/p'); sleep 2; n2=$(app batteryReads | sed -n 's/^reads: \([0-9]*\).*/\1/p')
  [ -n "$n1" ] || fail "the app's reads line could not be read: $(app batteryReads)"
  [ "$n1" = "$n2" ] || fail "the app went on reading a battery that is not there ($n1 then $n2)"
  row "no battery" "mobium and the app both say none; the app stopped reading"
  echo PASS
  exit 0
fi

ADB="adb -s $DEV"
restore() { $ADB emu power ac on >/dev/null 2>&1; $ADB emu power status charging >/dev/null 2>&1
  $ADB emu power capacity 100 >/dev/null 2>&1; }
trap restore EXIT

for want in 42 73; do
  $ADB emu power ac off >/dev/null
  $ADB emu power status discharging >/dev/null
  $ADB emu power capacity "$want" >/dev/null
  # The app hears of it when the platform announces it; wait for its own line.
  $M wait testid=batteryLevel --for text --text "level: $want%" --exact --timeout 10s >/dev/null \
    || fail "the app shows $(app batteryLevel), not level: $want%"
  [ "$(field level)" = "$want" ] || fail "mobium reads $(field level)%, the app and the console say $want%"
  [ "$(field state)" = "discharging" ] || fail "mobium reads $(field state), not discharging"
  [ "$(app batteryState)" = "state: unplugged" ] || fail "the app shows $(app batteryState), not unplugged"
  row "level $want" "set by the console; mobium and the app both read it"
done

$ADB emu power ac on >/dev/null
$ADB emu power status charging >/dev/null
$M wait testid=batteryState --for text --text 'state: charging' --exact --timeout 10s >/dev/null \
  || fail "the app shows $(app batteryState) after plugging in"
[ "$(field state)" = "charging" ] || fail "mobium reads $(field state) after plugging in"
row "charging" "plugged in: mobium and the app both say charging"

n1=$(app batteryReads | sed -n 's/^reads: \([0-9]*\).*/\1/p'); sleep 2; n2=$(app batteryReads | sed -n 's/^reads: \([0-9]*\).*/\1/p')
[ -n "$n1" ] && [ "$n2" -gt "$n1" ] || fail "the app's reads stopped at $n1, so what it shows may be stale"
row "live" "the app read again while watched ($n1 then $n2)"
echo PASS
