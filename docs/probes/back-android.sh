#!/bin/sh
# Back on Android, measured: the key and both edge swipes, each from a demo
# screen in MobiumApp and from a Settings sub-page, the control. Prints the
# app in front and counts, nothing else from the device. docs/BACK.md.
#
#   docs/probes/back-android.sh <serial>
DEV="$1"; ROOT="$(cd "$(dirname "$0")/../.." && pwd)"; M="$ROOT/bin/mobium --device $DEV"
W=$(adb -s $DEV shell wm size | tail -1 | sed 's/.*: //;s/x.*//'); H=$(adb -s $DEV shell wm size | tail -1 | sed 's/.*x//' | tr -d '\r')
Y=$((H/2)); RE=$((W-1)); RTO=$((W*35/100)); LTO=$((W*65/100))
APP=dev.mobium.mobiumapp
where() { # which screen: app-home, app-demo, settings-root, settings-sub, launcher, other
  f=$($M current 2>/dev/null)
  case "$f" in
    $APP) $M map 2>/dev/null | grep -q "Back (button)" && echo app-demo || echo app-home ;;
    com.android.settings) $M map 2>/dev/null | grep -q -i "Navigate up" && echo settings-sub || echo settings-root ;;
    *launcher*) echo launcher ;;
    *) echo "other:$f" ;;
  esac
}
alive() { [ -n "$(adb -s $DEV shell pidof $APP | tr -d '\r')" ] && echo alive || echo dead; }
tasks() { adb -s $DEV shell dumpsys activity activities | grep -c "Hist.*$APP/"; }
input() {
  case "$1" in
    key)   $M press back >/dev/null ;;
    right) $M swipe $RE $Y $RTO $Y --duration 250ms >/dev/null ;;
    left)  $M swipe 0 $Y $LTO $Y --duration 250ms >/dev/null ;;
  esac
}
for how in key right left; do
  $M terminate $APP >/dev/null 2>&1; $M launch $APP >/dev/null; sleep 2
  $M tap "label=Login Demo" >/dev/null 2>&1 || $M tap "testid=loginBtn" >/dev/null; sleep 1
  before=$(where); input $how; sleep 2
  after=$(where); a=$(alive); t=$(tasks)
  adb -s $DEV shell am start -n $APP/.MainActivity >/dev/null 2>&1; sleep 2
  echo "mobiumapp $how: $before -> $after; process $a; activities $t; reopened at $(where)"
done
for how in key right left; do
  $M terminate com.android.settings >/dev/null 2>&1; $M launch com.android.settings >/dev/null; sleep 2
  $M tap "text=Network & internet" >/dev/null 2>&1 || $M tap "text=Display" >/dev/null 2>&1; sleep 2
  before=$(where); input $how; sleep 2
  echo "settings $how: $before -> $(where)"
done
