#!/bin/sh
# Android Automotive: the Android backend on a car's screen.
#
# A car's screen is several windows: the system bars, the app between them,
# and the keyboard in a window of its own. Its apps run as user 10, the
# driver, while user 0 runs only the system. On the Android Automotive 15
# emulator (2026-11-11), before this check:
#   - map found nothing on the home screen (CHALLENGES 393);
#   - read as windows, the bars hid the app (394);
#   - an app's dialog was no dialog to app_alert (395);
#   - an untargeted swipe had no screen to cross (396);
#   - grant, clear-data and an app's logs acted for user 0 (397);
#   - app_alert quoted the instrument cluster (398);
#   - the permission prompt's buttons had no names (399);
#   - boot gave up on a cold car boot (400), a tile mapped twice (401),
#     car-ui's marker read as text (402), a car said three-button
#     navigation (403), and its back button was not mapped (404).
#
# Needs MobiumApp installed (mobiumdev/mobium-app). It is letterboxed on a
# landscape car screen, which is a case the dialog rule must tell from a
# dialog. Every change is put back: the temperature, and MobiumApp's
# permissions, reset to asking.
#
#   docs/checks/automotive.sh <serial>     an Android Automotive emulator
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_platform android "$DEV"
case "$(adb -s "$DEV" shell getprop ro.build.characteristics 2>&1)" in
  *automotive*) ;;
  *) echo "$(basename "$0") is for an Android Automotive device, and $DEV is not one" >&2; exit 2 ;;
esac
check_lock "$DEV"
M="$ROOT/bin/mobium --device $DEV"
APP=dev.mobium.mobiumapp
USER_ID=$(adb -s "$DEV" shell am get-current-user | tr -d '\r')
[ "$USER_ID" != 0 ] || { echo "$DEV is in front for user 0; this check is about a driver who is not" >&2; exit 2; }

fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf "  %-12s ok — %s\n" "$1" "$2"; }
no_dialog() {
  out=$($M alert 2>&1) || fail "alert: $out"
  [ "$out" = "no dialog is on screen" ] || fail "$1: $out"
}
# camera <user>: whether that user's MobiumApp has the camera, from the
# device rather than from Mobium.
camera() {
  adb -s "$DEV" shell dumpsys package $APP | awk -v u="User $1:" '
    index($0, u) { on = 1; next }
    on && /User [0-9]+:/ { exit }
    on && /android.permission.CAMERA:/ { sub(/.*granted=/, ""); sub(/,.*/, ""); print; exit }'
}

echo "--- $DEV (user $USER_ID)"
$M press home >/dev/null
out=$($M map 2>&1) || fail "map: $out"
echo "$out" | grep -q "Raise temperature (button)" || fail "map lists no climate control: $out"
echo "$out" | grep -q "Applications (button" || fail "map lists no app grid button: $out"
[ "$($M current)" = com.android.car.carlauncher ] || fail "current is $($M current), not the launcher"
no_dialog "the home screen read as a dialog"
row "home" "map lists the bars and the launcher; current is the launcher"

# The driver's side, the first of two, moves by one after a tap, and is put
# back. Both sides share their ids, so they are taken by ref. The emulator's
# climate control drops a press now and then, adb's as much as Mobium's (9 of
# 10 each, a second apart): one dropped press is pressed again, and said. At
# its highest, 82, raising does nothing, so it is lowered instead.
temp() { $M find testid=hvac_temperature_text | head -1 | awk '{print $2}'; }
ref() { $M find "label=$1" | head -1 | awk '{print $1}'; }
t=$(temp)
case "$t" in *[!0-9]*|"") fail "the temperature reads \"$t\"" ;; esac
if [ "$t" -ge 82 ]; then button="Lower temperature"; back="Raise temperature"; want=$((t - 1))
else button="Raise temperature"; back="Lower temperature"; want=$((t + 1)); fi
moved() {
  $M tap "$(ref "$button")" >/dev/null
  i=0; while [ $i -lt 10 ]; do [ "$(temp)" = "$want" ] && return 0; sleep 0.5; i=$((i + 1)); done
  return 1
}
again=""
moved || { again=" (pressed twice: the first was dropped)"; moved; } || fail "$button from $t gave $(temp), twice"
at_exit "$M tap \$($M find 'label=$back' | head -1 | awk '{print \$1}') >/dev/null 2>&1 || true"
row "bar" "a tap in the bottom bar, $button, took $t to $want$again"

out=$($M swipe up 2>&1) || fail "swipe: $out"
row "swipe" "$out"

$M tap 'label=Applications' >/dev/null
$M wait 'label=Settings' --timeout 10s >/dev/null || fail "the app grid did not open"
out=$($M map)
n=$(echo "$out" | grep -c "app_icon") || true
[ "$n" = 0 ] || fail "map lists $n tile icons as controls of their own"
[ "$(echo "$out" | grep -c '^@e[0-9]* Settings (button')" -ge 1 ] || fail "map has no Settings tile: $out"
$M tap 'label=Settings,role=button' >/dev/null 2>&1 || $M tap "$(echo "$out" | grep -m1 ' Settings (button' | awk '{print $1}')" >/dev/null
$M wait 'label=Display' --timeout 10s >/dev/null 2>&1 || $M wait 'text=Display' --timeout 10s >/dev/null || fail "Settings did not open"
$M text | grep -q "ROTARY" && fail "text reads car-ui's ROTARY marker as screen text"
$M map | grep -q "ROTARY" && fail "map names a list by car-ui's ROTARY marker"
row "launcher" "each tile maps once; Settings opens; car-ui's marker is not read as text"

case "$($ROOT/bin/mobium devices 2>&1 | grep "^$DEV ")" in
  *"navigation: car bars"*) ;;
  *) fail "devices does not say car bars: $($ROOT/bin/mobium devices 2>&1 | grep "^$DEV ")" ;;
esac
st=0; out=$($M press back --gesture 2>&1) || st=$?
[ $st -eq 5 ] || fail "a back gesture on a car exited $st, want 5 (refused): $out"
row "navigation" "devices says car bars; a back gesture is refused for the back key"

$M terminate $APP >/dev/null 2>&1 || true
$M reset-permissions $APP >/dev/null
at_exit "$M reset-permissions $APP >/dev/null 2>&1 || true"
$M launch $APP >/dev/null
$M wait 'label=WebViews' --timeout 15s >/dev/null || fail "MobiumApp did not show its menu"
[ "$($M current)" = $APP ] || fail "current is $($M current), not MobiumApp"
no_dialog "letterboxed MobiumApp read as a dialog"
row "letterbox" "MobiumApp, letterboxed, is the app in front and no dialog"

$M tap 'label=Dialog Demo' >/dev/null
# The car's back button lies over the app's left edge: mapped, and a tap on
# it goes back. It shows while an app is in front.
i=0; until $M map | grep -q "Back Button (button"; do
  i=$((i + 1)); [ $i -lt 6 ] || fail "map lists no car back button"; sleep 1
done
shows 'label=One-button alert' || fail "the Dialog Demo is not open"
$M tap 'label=Back Button' >/dev/null
i=0; while shows 'label=One-button alert'; do
  i=$((i + 1)); [ $i -lt 10 ] || fail "the car's back button did not go back"; sleep 0.5
done
[ "$($M current)" = $APP ] || fail "the car's back button left MobiumApp"
row "back" "the car's back button is mapped, and a tap goes back in the app"
$M tap 'label=Dialog Demo' >/dev/null
$M tap testid=lateAlertBtn >/dev/null
i=0; until $M alert 2>/dev/null | grep -q 'a dialog is on screen'; do
  i=$((i + 1)); [ $i -lt 8 ] || fail "the late alert never came"; sleep 1
done
out=$($M alert)
case "$out" in
  'a dialog is on screen: "Session expiring\nStay signed in?"'*) ;;
  *) fail "app_alert does not quote the dialog itself: $out" ;;
esac
$M alert --button Stay >/dev/null || fail "--button Stay was refused"
sleep 1
$M scroll-to testid=dialogOutcome --direction up >/dev/null 2>&1 || true
[ "$($M text testid=dialogOutcome)" = "late: Stay" ] || fail "the app did not receive Stay"
row "dialog" "app_alert reads the app's dialog by its own words; Stay pressed"

$M tap testid=notificationsBtn >/dev/null
i=0; until $M alert 2>/dev/null | grep -q 'a dialog is on screen'; do
  i=$((i + 1)); [ $i -lt 8 ] || fail "the notification prompt never came"; sleep 1
done
out=$($M alert)
case "$out" in
  *'"Allow MobiumApp to send you notifications?"'*'buttons: "Allow", "Don’t allow"'*) ;;
  *) fail "the permission prompt reads: $out" ;;
esac
st=0; out=$($M alert dismiss 2>&1) || st=$?
[ $st -eq 5 ] || fail "dismiss on the car's prompt exited $st, want 5 (refused): $out"
$M alert --button "Don’t allow" >/dev/null || fail "--button \"Don’t allow\" was refused"
sleep 1
$M scroll-to testid=dialogOutcome --direction up >/dev/null 2>&1 || true
[ "$($M text testid=dialogOutcome)" = "notifications: denied" ] || fail "the app did not receive the denial"
row "prompt" "the car's prompt names Allow and Don’t allow; dismiss refused; --button denies"

$M terminate $APP >/dev/null
$M launch $APP >/dev/null
$M tap 'label=Login Demo' >/dev/null
$M tap testid=username >/dev/null
$M keyboard | grep -q "the keyboard is up" || fail "tapping the username field raised no keyboard"
n=$($M map | grep -c -E '@e[0-9]+ (q|w|Space|Shift) \(button') || true
[ "$n" = 0 ] || fail "map lists $n keyboard keys"
no_dialog "the keyboard read as a dialog"
[ "$($M current)" = $APP ] || fail "with the keyboard up, current is $($M current)"
$M keyboard --hide >/dev/null
row "keyboard" "up, it is no dialog, not mapped, and MobiumApp stays in front"

# User 10's own state, read from the device. Revoked for both users first,
# so a grant to user 0 alone leaves user $USER_ID without it.
adb -s "$DEV" shell pm revoke --user 0 $APP android.permission.CAMERA
adb -s "$DEV" shell pm revoke --user "$USER_ID" $APP android.permission.CAMERA
[ "$(camera "$USER_ID")" = false ] || fail "the revoke did not take for user $USER_ID"
$M grant $APP camera >/dev/null || fail "grant camera was refused"
[ "$(camera "$USER_ID")" = true ] || fail "grant camera left user $USER_ID without it (user 0: $(camera 0))"
row "grant" "camera granted to user $USER_ID, read back from the device"

$M logs --app $APP >/dev/null 2>&1
$M terminate $APP >/dev/null
$M launch $APP >/dev/null
sleep 3
own=$($M logs --app $APP 2>/dev/null | grep -c -v -E 'ActivityManager|ActivityTaskManager|WindowManager|CoreBackPreview|ImeTracker|AppsFilter|Car[A-Za-z]*:' ) || true
[ "${own:-0}" -gt 0 ] || fail "logs --app gave none of MobiumApp's own lines"
row "logs" "$own of MobiumApp's own lines, from user $USER_ID's process"

# clear-data: a count MobiumApp keeps, raised, then cleared.
$M terminate $APP >/dev/null
$M launch $APP >/dev/null
$M tap 'label=Storage Demo' >/dev/null
$M tap testid=saveOneMoreBtn >/dev/null
before=$($M text testid=storedCount)
[ "$before" != "stored: 0" ] || fail "saving left the count at 0"
$M clear-data $APP >/dev/null || fail "clear-data was refused"
$M launch $APP >/dev/null
$M tap 'label=Storage Demo' >/dev/null
after=$($M text testid=storedCount)
[ "$after" = "stored: 0" ] || fail "after clear-data the count reads \"$after\", was \"$before\""
row "clear-data" "MobiumApp's count went from \"$before\" to \"$after\""

$M press home >/dev/null
echo "PASS"
