#!/bin/sh
# app_upload and app_download, held to what MobiumApp's Files Demo sees.
#
#   docs/checks/files.sh <emulator-serial | simulator-udid>
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
# Emulators and simulators.
set -e
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
case "$DEV" in
  *-*-*-*-*) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  emulator-*) PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
  *) echo "an emulator or a simulator: this writes to the device's downloads" >&2; exit 2 ;;
esac
APP=dev.mobium.mobiumapp
fail() { echo "FAIL: $*" >&2; exit 1; }
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
  else
    docs="$(xcrun simctl get_app_container "$DEV" "$APP" data 2>/dev/null)/Documents"
    rm -f "$docs/$UPLOAD" "$docs/mobium-report.txt"
  fi
}
trap cleanup EXIT

"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true
echo "--- $DEV ($PLATFORM)"
$M apps 2>/dev/null | grep -q "$APP" || fail "$APP is not installed"

$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
$M tap testid=filesBtn >/dev/null || fail "no Files Demo: this MobiumApp predates it"
$M wait testid=savedFile >/dev/null

# --- download: what the app saved, brought back, twice ----------------------
for n in 1 2; do
  $M tap testid=saveReportBtn >/dev/null
  $M wait testid=savedFile --for text --text "MobiumApp report $n" >/dev/null ||
    fail "the app did not say it saved report $n: $($M text testid=savedFile)"
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
$M download | grep -q "$UPLOAD" || fail "the upload is not in the listing: $($M download)"
$M tap testid=pickFileBtn >/dev/null
sleep 2
if [ "$PLATFORM" = android ]; then
  $M tap "text=$UPLOAD" >/dev/null || fail "the picker does not show $UPLOAD: $($M map | head -20)"
else
  # The picker opens where it last was: at the folder list, where this
  # app's folder is labeled "MobiumApp, N items", or inside it, where a
  # file is "<name>, <extension>, <time>, <size>". Labels carry a count and
  # a time, so the refs come from map.
  ref() { $M map | grep -m1 "$1" | awk '{print $1}'; }
  folder=$(ref '^@e[0-9]* MobiumApp, [0-9]* item')
  [ -z "$folder" ] || $M tap "$folder" >/dev/null
  sleep 1
  file=$(ref "^@e[0-9]* $TOKEN, txt,")
  [ -n "$file" ] || fail "the picker does not show $UPLOAD: $($M map | head -20)"
  $M tap "$file" >/dev/null
fi
$M wait testid=pickedFile --for text --text "$TOKEN" --timeout 15s >/dev/null ||
  fail "the app did not get the upload: $($M text testid=pickedFile)"
picked=$($M text testid=pickedFile)
echo "$picked" | grep -q "$UPLOAD ($size bytes) — $TOKEN" ||
  fail "the app says '$picked', want $UPLOAD, $size bytes, first line $TOKEN"
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
