#!/bin/sh
# app_upload and app_download, held to what MobiumApp's Files Demo sees.
#
#   docs/checks/files.sh <emulator-serial | simulator-udid>
#   docs/checks/files.sh <android-phone-serial>
#   MOBIUMAPP_BUNDLE=<MobiumApp.app> docs/checks/files.sh <iphone-udid>
#
# A transfer is only believed from the far end. So:
#
# - Download: the app saves its report twice — where the device keeps
#   downloads, Android's Download folder or its own Documents on iOS — and
#   each time `mobium download` must bring back that save's number. A copy
#   of an earlier file cannot pass the second.
# - Upload: a file with a name and a first line nobody could guess is sent,
#   listed by `mobium download`, picked in the app's own file picker, and the
#   app must say that name, size and first line.
# - A path given as a name is refused, and so is a file that is not there.
#
# Leaves nothing on the device: the files it made are removed at the end.
# On a real iPhone nothing can delete one file from an app's Documents
# (devicectl has no delete), so the check reinstalls MobiumApp from
# MOBIUMAPP_BUNDLE at the end, which empties MobiumApp's own container and
# nothing else, and refuses to start without it. On an Android phone the
# Download folder is its owner's: the check refuses to start if a
# mobium-report.txt is there already, and removes only the two files it made.
set -e
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
# A simulator is a UUID of five groups; a real iPhone's UDID is two,
# 00008120-0001234567890ABC. Anything else is an Android serial.
PHONE=
case "$DEV" in
  *-*-*-*-*) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  # Before the iPhone's pattern: emulator-5554 has a hyphen too, and was
  # taken for a phone and refused.
  emulator-*) PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
  *-*) PLATFORM=ios; PHONE=1; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *) PLATFORM=android; PHONE=1; M="$ROOT/bin/mobium --device $DEV" ;;
esac
if [ "$PLATFORM" = ios ] && [ -n "$PHONE" ] && [ ! -d "${MOBIUMAPP_BUNDLE:-}" ]; then
  echo "on a real iPhone, set MOBIUMAPP_BUNDLE to MobiumApp.app: reinstalling it is the only way to remove" \
    "the files this check puts in its Documents" >&2
  exit 2
fi
APP=dev.mobium.mobiumapp
fail() { echo "FAIL: $*" >&2; exit 1; }
# seen is what a failure quotes from the device, withheld on a real phone:
# there the picker opens on its owner's recent files, and a failure that
# printed the map once listed them (CHALLENGES 174). Assert whether, never
# what.
seen() { if [ -n "$PHONE" ]; then echo "(not shown on a real phone)"; else "$@" 2>&1 | head -20; fi; }
row() { printf '    %-14s %-58s ok\n' "$1" "$2"; }
OUT=$(mktemp -d)
TOKEN="mobium-$(od -An -N4 -tx4 /dev/urandom | tr -d ' ')"
UPLOAD="$TOKEN.txt"

cleanup() {
  rm -rf "$OUT"
  if [ "$PLATFORM" = android ]; then
    for f in "$UPLOAD" mobium-report.txt; do
      adb -s "$DEV" shell rm -f "/sdcard/Download/$f" >/dev/null 2>&1 || true
      adb -s "$DEV" shell content delete --uri content://media/external/downloads \
        --where "\"_display_name='$f'\"" >/dev/null 2>&1 || true
    done
  elif [ -n "$PHONE" ]; then
    $M uninstall $APP >/dev/null 2>&1 || true
    $M install "$MOBIUMAPP_BUNDLE" >/dev/null 2>&1 || echo "WARNING: reinstall MobiumApp from $MOBIUMAPP_BUNDLE" >&2
  else
    docs="$(xcrun simctl get_app_container "$DEV" "$APP" data 2>/dev/null)/Documents"
    rm -f "$docs/$UPLOAD" "$docs/mobium-report.txt"
  fi
}
trap cleanup EXIT

"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true
echo "--- $DEV ($PLATFORM)"
if [ "$PLATFORM" = ios ] && [ -n "$PHONE" ]; then
  # From a fresh install, so the Files Demo is this build's and the
  # Documents folder holds nothing from before.
  $M uninstall $APP >/dev/null 2>&1 || true
  $M install "$MOBIUMAPP_BUNDLE" >/dev/null || fail "could not install $MOBIUMAPP_BUNDLE"
fi
# A device that is not there answers apps with an error, which is not the
# same as an app that is not installed.
apps=$($M apps 2>&1) || fail "cannot list the apps: $(echo "$apps" | tail -1)"
echo "$apps" | grep -q "$APP" || fail "$APP is not installed"
if [ "$PLATFORM" = android ] && [ -n "$PHONE" ] &&
  adb -s "$DEV" shell ls /sdcard/Download/mobium-report.txt >/dev/null 2>&1; then
  trap - EXIT
  fail "a mobium-report.txt is in this phone's Download folder already, and this check would remove it"
fi

$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
$M tap testid=filesBtn >/dev/null || fail "no Files Demo: this MobiumApp predates it"
$M wait testid=savedFile >/dev/null

# --- download: what the app saved, brought back, twice ----------------------
for n in 1 2; do
  $M tap testid=saveReportBtn >/dev/null
  $M wait testid=savedFile --for text --text "MobiumApp report $n" >/dev/null ||
    fail "the app did not say it saved report $n: $(seen $M text testid=savedFile)"
  bytes=$($M text testid=savedFile | sed -n 's/.*(\([0-9]*\) bytes).*/\1/p')
  $M download mobium-report.txt -o "$OUT/report.txt" >/dev/null
  [ "$(head -1 "$OUT/report.txt")" = "MobiumApp report $n" ] ||
    fail "downloaded '$(head -1 "$OUT/report.txt")', and the app had just saved report $n"
  [ "$(wc -c < "$OUT/report.txt" | tr -d ' ')" = "$bytes" ] ||
    fail "downloaded $(wc -c < "$OUT/report.txt") bytes, and the app saved $bytes"
done
row "download" "the app's save brought back, twice, the second not the first"

# --- upload: sent, listed, picked in the app --------------------------------
printf '%s\nsent by files.sh\n' "$TOKEN" > "$OUT/$UPLOAD"
size=$(wc -c < "$OUT/$UPLOAD" | tr -d ' ')
$M upload "$OUT/$UPLOAD" >/dev/null
$M download | grep -q "$UPLOAD" || fail "the upload is not in the listing: $(seen $M download)"
$M tap testid=pickFileBtn >/dev/null
sleep 2
if [ "$PLATFORM" = android ]; then
  $M tap "text=$UPLOAD" >/dev/null || fail "the picker does not show $UPLOAD: $(seen $M map)"
else
  # The picker opens where it last was: on a phone first on Recents, which
  # are its owner's files; on the folder list, where this app's folder is
  # "MobiumApp, N items"; or inside it, where a file is "<name>, <extension>,
  # <time>, <size>". Labels carry a count and a time, so the refs come from
  # map, grepped and never printed. Browse is the tab bar's, the last one.
  ref() { $M map | grep "$1" | awk '{print $1}' | tail -1; }
  if [ -z "$(ref "^@e[0-9]* $TOKEN, txt,")" ]; then
    if [ -z "$(ref '^@e[0-9]* MobiumApp, [0-9]* item')" ]; then
      tab=$(ref '^@e[0-9]* Browse (button)')
      [ -z "$tab" ] || $M tap "$tab" >/dev/null
      sleep 1
      place=$(ref '^@e[0-9]* On My iPhone')
      [ -z "$place" ] || $M tap "$place" >/dev/null
      sleep 1
    fi
    folder=$(ref '^@e[0-9]* MobiumApp, [0-9]* item')
    [ -z "$folder" ] || $M tap "$folder" >/dev/null
    sleep 1
  fi
  file=$(ref "^@e[0-9]* $TOKEN, txt,")
  [ -n "$file" ] || fail "the picker does not show $UPLOAD: $(seen $M map)"
  $M tap "$file" >/dev/null
fi
$M wait testid=pickedFile --for text --text "$TOKEN" --timeout 15s >/dev/null ||
  fail "the app did not get the upload: $(seen $M text testid=pickedFile)"
picked=$($M text testid=pickedFile)
echo "$picked" | grep -q "$UPLOAD ($size bytes) — $TOKEN" ||
  fail "the app says '$(seen echo "$picked")', want $UPLOAD, $size bytes, first line $TOKEN"
row "upload" "sent, listed, picked in the app: name, size and first line"

# --- refusals ---------------------------------------------------------------
set +e
$M upload "$OUT/$UPLOAD" --name ../x >/dev/null 2>&1; st=$?
set -e
[ $st -eq 2 ] || fail "a path given as a name was not refused as invalid_argument ($st)"
set +e
$M download "not-$UPLOAD" -o "$OUT/x" >/dev/null 2>&1; st=$?
set -e
[ $st -eq 4 ] || fail "a file that is not there was not no_such_element ($st)"
row "refusals" "a path as a name, and a file not there"

echo PASS
