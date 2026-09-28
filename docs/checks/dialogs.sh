#!/bin/sh
# Dialogs, end to end, on MobiumApp's Dialog Demo: every kind the app can
# raise, each judged by what the app says it received rather than by the
# dialog going away.
#
#   docs/checks/dialogs.sh <android-serial | simulator-udid>
#
# What it holds each platform to is what was measured (CHALLENGES 106):
# accept and dismiss press a button the platform picks, and it is not the
# same one twice — so the three-button alert and the action sheet are
# asserted as they are, not as anyone would guess. Answering by the button's
# caption is the way that means the same thing everywhere, and is checked
# for every kind. Then the refusals: a target under a dialog, and under the
# keyboard (CHALLENGES 105 and its keyboard half).
#
# Needs MobiumApp installed (mobiumdev/mobium-app). A real iPhone cannot reset
# permissions from outside, so there it needs MOBIUMAPP_BUNDLE and reinstalls
# the app instead; its clipboard cannot be seeded, so paste is not checked.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-18s %-50s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp

case "$DEV" in
  ????????-????????????????)
    # A phone cannot reset a permission from outside; reinstalling the app
    # is the one reset it has, so the bundle is required here.
    [ -n "$MOBIUMAPP_BUNDLE" ] || { echo "on a real iPhone set MOBIUMAPP_BUNDLE=<path to MobiumApp.app>: reinstalling is the only permission reset a phone has" >&2; exit 2; }
    PLATFORM=ios; PHONE=1; M="$ROOT/bin/mobium --driver wda --device $DEV"
    RESET=reinstall; CAP=label ;;
  *-*-*-*-*) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV"; RESET="$M reset-permissions $APP"; CAP=label ;;
  *)         PLATFORM=android; M="$ROOT/bin/mobium --device $DEV"; RESET="$M reset-permissions"; CAP=text ;;
esac
echo "--- $DEV ($PLATFORM)"
reinstall() { $M uninstall "$APP" >/dev/null 2>&1 || true; $M install "$MOBIUMAPP_BUNDLE" >/dev/null; }

fresh() {
  $M alert dismiss >/dev/null 2>&1 || true
  $M terminate "$APP" >/dev/null 2>&1 || true
  $M launch "$APP" >/dev/null
  $M tap "label=Dialog Demo" >/dev/null
}
# raise <button>: tap it and wait for a dialog to be up.
raise() {
  $M tap "testid=$1" >/dev/null
  i=0
  until $M alert 2>/dev/null | grep -q 'a dialog is on screen'; do
    i=$((i + 1)); [ $i -lt 8 ] || fail "$1 raised no dialog"
    sleep 1
  done
}
# outcome <text>: the app says it received exactly that.
outcome() {
  $M scroll-to testid=dialogOutcome --direction up >/dev/null 2>&1 || true
  got=$($M text testid=dialogOutcome)
  [ "$got" = "$1" ] || fail "the app received \"$got\", want \"$1\""
}
# verb <button> <accept|dismiss> <outcome>
verb() { fresh; raise "$1"; $M alert "$2" >/dev/null; sleep 1; outcome "$3"; }
# press <button> <caption> <outcome>: answer by the caption, the way that
# means the same thing on every platform. Captions match case-insensitively,
# since Android draws alert buttons in capitals, and are narrowed to buttons,
# since a message can contain one: "Stay signed in?" and Stay. A caption is
# a button's text on Android and its label on iOS — map prints it the same on
# both, but no one locator kind matches it on both, so CAP names the kind.
press() { fresh; raise "$1"; $M tap "$CAP=$2,role=button" >/dev/null; sleep 1; outcome "$3"; }

# --- the app's own ----------------------------------------------------------
verb oneButtonBtn accept "one-button: OK"
verb oneButtonBtn dismiss "one-button: OK"
row "one button" "accept and dismiss both press OK"

verb twoButtonBtn dismiss "two-button: Keep Editing"
verb twoButtonBtn accept "two-button: Discard"
press twoButtonBtn "Keep Editing" "two-button: Keep Editing"
row "two buttons" "the verbs read as cancel and confirm, here only"

if [ "$PLATFORM" = android ]; then
  # The positive and negative buttons, wherever they sit: React Native puts
  # the negative one in the middle of three.
  verb threeButtonBtn dismiss "three-button: Don't Save"
  verb threeButtonBtn accept "three-button: Save"
  row "three buttons" "dismiss pressed the middle one"
else
  # An alert's first and last — and iOS puts Cancel last.
  verb threeButtonBtn dismiss "three-button: Don't Save"
  verb threeButtonBtn accept "three-button: Cancel"
  row "three buttons" "accept pressed Cancel"
fi
press threeButtonBtn "Cancel" "three-button: Cancel"
row "three, by caption" "Cancel is Cancel on both"

fresh
$M tap testid=lateAlertBtn >/dev/null
$M alert 2>/dev/null | grep -q 'no dialog is on screen' || fail "the late alert came at once"
i=0; until $M alert 2>/dev/null | grep -q 'Session expiring'; do i=$((i + 1)); [ $i -lt 8 ] || fail "the late alert never came"; sleep 1; done
$M tap "$CAP=Stay,role=button" >/dev/null; sleep 1; outcome "late: Stay"
row "late" "absent at first, answered once it came"

if [ "$PLATFORM" = ios ]; then
  # A sheet's first and last, the other way round from an alert's.
  verb actionSheetBtn dismiss "action sheet: Cancel"
  verb actionSheetBtn accept "action sheet: Copy Link"
  press actionSheetBtn "Delete Photo" "action sheet: Delete Photo"
  row "action sheet" "dismiss Cancel, accept Copy Link, reversed"
else
  fresh; $M tap testid=actionSheetBtn >/dev/null; sleep 1
  outcome "action sheet: none on android: ActionSheetIOS is iOS-only"
  row "action sheet" "none on Android, and the app says so"
fi

# --- the system's -----------------------------------------------------------
# The share sheet is not an alert to either platform's server. On Android it
# is another activity; on iOS a view hosted in the app.
fresh
$M tap testid=shareBtn >/dev/null; sleep 3
$M alert 2>/dev/null | grep -q 'no dialog is on screen' || fail "the share sheet was seen as an alert"
if [ "$PLATFORM" = android ]; then
  [ "$($M current)" = com.android.intentresolver ] || fail "the share sheet is not the intent resolver: $($M current)"
  $M press back >/dev/null
else
  # iOS 26's sheet has no close button in the tree, only its targets; a
  # swipe down closes it, as it does for a person.
  $M swipe down >/dev/null
fi
sleep 2
if [ "$PLATFORM" = android ]; then
  # Android's share API reports "shared" however the sheet closed.
  outcome "share: closed (android cannot tell shared from dismissed)"
else
  outcome "share: dismissed"
fi
row "share sheet" "no alert to app_alert; closed without it"

# Camera, the permission both platforms reset: accept grants and dismiss
# denies on both. Location is the one that inverts on iOS (CHALLENGES 63).
$RESET >/dev/null 2>&1; verb cameraBtn dismiss "camera: denied"
$RESET >/dev/null 2>&1; verb cameraBtn accept "camera: granted"
row "camera" "dismiss denied, accept granted"
if [ "$PLATFORM" = ios ]; then
  $RESET >/dev/null 2>&1; verb locationBtn accept "location: denied"
  row "location" "accept denied: Don't Allow is last of three"
  $RESET >/dev/null 2>&1; verb trackingBtn dismiss "tracking: denied"
  row "tracking" "App Tracking Transparency, from SpringBoard"
else
  $RESET >/dev/null 2>&1; verb locationBtn accept "location: granted"
  row "location" "accept granted: the positive button"
fi

# Paste: iOS asks before an app reads what another app put on the clipboard —
# SpringBoard's prompt, Don't Allow Paste first and Allow Paste last — and
# Android does not ask at all. The clipboard is seeded from outside, so on
# iOS the text is another app's, which is what raises the prompt.
CLIP='seeded by mobium'
# below <button>: bring a button under the fold into view, once the screen
# has come up — a scroll sent while it is still arriving scrolls Home.
below() { $M wait testid=dialogOutcome >/dev/null; $M scroll-to "testid=$1" >/dev/null; }
if [ -n "$PHONE" ]; then
  # A phone's clipboard cannot be written from outside, so nothing can put
  # another app's text there for the prompt to be about.
  printf '    %-18s %s\n' "paste" "NOT CHECKED — a phone's clipboard cannot be seeded from outside"
elif ! $M clipboard "$CLIP" >/dev/null; then
  fail "the clipboard could not be seeded"
elif [ "$PLATFORM" = ios ]; then
  fresh; below pasteBtn; raise pasteBtn; $M alert dismiss >/dev/null; sleep 1; outcome "paste: empty, or not allowed"
  fresh; below pasteBtn; raise pasteBtn; $M alert accept >/dev/null; sleep 1; outcome "paste: ${#CLIP} characters"
  row "paste" "dismiss refused it, accept read ${#CLIP} characters"
else
  fresh; below pasteBtn; $M tap testid=pasteBtn >/dev/null; sleep 1
  $M alert 2>/dev/null | grep -q 'no dialog' || fail "Android asked before a paste"
  outcome "paste: ${#CLIP} characters"
  row "paste" "no prompt on Android; read ${#CLIP} characters"
fi

# --- declared rules ---------------------------------------------------------
# A rule names the button, and an action that meets the dialog carries on:
# the dialog answered, the action done, and the result saying which.
$M dialogs --clear >/dev/null
$M dialogs --when "Discard changes" --press "Keep Editing" >/dev/null
fresh
raise twoButtonBtn
out=$($M tap testid=oneButtonBtn 2>&1) || fail "the rule did not carry the tap through: $out"
echo "$out" | grep -q '"Discard changes?" — pressed "Keep Editing"' || fail "the tap did not say which dialog it answered: $out"
i=0; until $M alert 2>/dev/null | grep -q 'Saved'; do i=$((i + 1)); [ $i -lt 8 ] || fail "the tap after the rule never raised its own alert"; sleep 1; done
$M alert accept >/dev/null; sleep 1; outcome "one-button: OK"
$M dialogs | grep -q 'answered 1' || fail "the rule's count did not move"
row "a rule" "answered on the way, and the tap went on"

# A rule for a button the dialog does not have is refused, with the ones it has.
$M dialogs --when "Discard changes" --press "Delete" >/dev/null
fresh
raise twoButtonBtn
out=$($M tap testid=oneButtonBtn 2>&1) && fail "a rule for a missing button went through: $out"
echo "$out" | grep -qi 'keep editing' || fail "the refusal did not list the dialog's buttons: $out"
$M dialogs --clear >/dev/null
row "a wrong rule" "refused, listing the dialog's buttons"

# --- refusals ---------------------------------------------------------------
fresh
raise twoButtonBtn
out=$($M tap testid=backBtn 2>&1) && fail "a tap under a dialog went through: $out"
echo "$out" | grep -q 'a dialog is over the app' || fail "the refusal did not name the dialog: $out"
$M tap "$CAP=Keep Editing,role=button" >/dev/null
row "under a dialog" "refused, naming it; its own button still works"

fresh
$M scroll-to testid=coverField >/dev/null
$M tap testid=coverField >/dev/null; sleep 1
# With a hardware keyboard attached nothing covers the button: a simulator
# keeps its software keyboard below the screen's edge, and a headed emulator
# shows only Gboard's floating toolbar, at the left. That is a reason to
# skip, not a failure — so the check asks whether the button is covered.
covered=yes
if ! $M keyboard --json | grep -q '"shown": true'; then
  covered=""
elif [ "$PLATFORM" = android ]; then
  b=$($M find testid=coveredBtn --json 2>/dev/null | python3 -c '
import json,sys
e=json.load(sys.stdin)["elements"]
print("%d %d" % ((e[0]["bounds"]["x1"]+e[0]["bounds"]["x2"])//2, (e[0]["bounds"]["y1"]+e[0]["bounds"]["y2"])//2) if e else "")')
  if [ -n "$b" ]; then
    covered=$(adb -s "$DEV" shell dumpsys window InputMethod | python3 -c '
import re,sys
out=sys.stdin.read(); x,y=map(int,sys.argv[1:3])
m=re.search(r"touchable region=SkRegion\(((?:\(-?\d+,-?\d+,-?\d+,-?\d+\))+)\)", out)
rects=[tuple(map(int,r)) for r in re.findall(r"\((-?\d+),(-?\d+),(-?\d+),(-?\d+)\)", m.group(1))] if m and "isVisible=true" in out else []
print("yes" if any(a<=x<c and b<=y<d for a,b,c,d in rects) else "")' $b)
  fi
fi
if [ -z "$covered" ]; then
  printf '    %-18s %s\n' "under keyboard" "SKIPPED — the keyboard covers nothing there (a hardware keyboard is attached)"
  $M terminate "$APP" >/dev/null 2>&1 || true
  echo PASS
  exit 0
fi
out=$($M tap testid=coveredBtn 2>&1) && fail "a tap under the keyboard went through: $out"
echo "$out" | grep -q 'keyboard' || fail "the refusal did not name the keyboard: $out"
if [ "$PLATFORM" = ios ]; then $M keyboard --key enter >/dev/null; else $M keyboard --hide >/dev/null; fi
sleep 1
$M tap testid=coveredBtn >/dev/null; outcome "pinned button: tapped"
row "under keyboard" "refused, naming it; tapped once it was hidden"

$M terminate "$APP" >/dev/null 2>&1 || true
echo PASS
