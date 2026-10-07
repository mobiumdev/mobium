#!/bin/sh
# Files and folders at a path you name: upload and download with
# --device-path, held to what comes back, byte for byte.
#
#   docs/checks/device-paths.sh <android-serial | simulator-udid | iphone-udid>
#
# Android: a file and a folder through a shell path in /data/local/tmp, which
# belongs to the shell and not to anyone's files; and the refusal of an app
# that is not debuggable (MobiumApp's release build). With
# MOBIUM_DEBUGGABLE_APP=<package> — MobiumApp's flutter/ demo built with
# `flutter build apk --debug` — also a file and a folder in that app's
# private data, through run-as, which Android allows only for a debuggable
# build.
#
# iOS: a file and a folder in MobiumApp's data container, tmp/, which iOS
# clears itself; and a folder of the app's own, Library/Preferences, brought
# back.
#
# Everywhere: a path climbing out with ".." is refused, and a folder is never
# downloaded into one already there. Everything the check writes is removed
# afterwards, and its absence read back. Nothing is written to shared storage.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <android-serial | simulator-udid | iphone-udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
case "$DEV" in
  ????????-????????????????) PLATFORM=ios; PHONE=1 ;;
  *-*-*-*-*) PLATFORM=ios; PHONE="" ;;
  *) PLATFORM=android ;;
esac
if [ "$PLATFORM" = ios ]; then M="$ROOT/bin/mobium --driver wda --device $DEV"; else M="$ROOT/bin/mobium --device $DEV"; fi
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-60s ok\n' "$1" "$2"; }
hash() { shasum -a 256 "$1" | cut -c1-16; }
echo "--- $DEV ($PLATFORM)"

W=$(mktemp -d)
at_exit 'rm -rf "$W"'
head -c 70000 /dev/urandom > "$W/bin.dat"
mkdir -p "$W/tree/sub"; echo one > "$W/tree/a.txt"; echo two22 > "$W/tree/sub/b.txt"

# roundtrip <label> <device path> [--app <id>]: a file and a folder there and back.
roundtrip() {
  label=$1; base=$2; shift 2
  out=$($M upload "$W/bin.dat" --device-path "$base/bin.dat" "$@" 2>&1) || fail "$label: uploading a file: $out"
  echo "$out" | grep -q "confirmed by its size" || fail "$label: the upload did not say how it was confirmed: $out"
  out=$($M download --device-path "$base/bin.dat" -o "$W/back-$label.dat" "$@" 2>&1) || fail "$label: downloading the file: $out"
  [ "$(hash "$W/bin.dat")" = "$(hash "$W/back-$label.dat")" ] || fail "$label: the file came back different"
  out=$($M upload "$W/tree" --device-path "$base/tree" "$@" 2>&1) || fail "$label: uploading a folder: $out"
  echo "$out" | grep -q "a folder of 2 files" || fail "$label: the folder upload: $out"
  out=$($M download --device-path "$base/tree" -o "$W/back-$label" "$@" 2>&1) || fail "$label: downloading the folder: $out"
  diff -r "$W/tree" "$W/back-$label" >/dev/null || fail "$label: the folder came back different"
  out=$($M download --device-path "$base/tree" -o "$W/back-$label" "$@" 2>&1) && fail "$label: a folder was downloaded into one already there"
  echo "$out" | grep -q "already exists" || fail "$label: the refusal to merge did not say why: $out"
  row "$label" "a file and a folder there and back, byte for byte"
}

if [ "$PLATFORM" = ios ]; then APPARG="--app dev.mobium.mobiumapp"; else APPARG=""; fi
out=$($M download --device-path "../../etc" -o "$W/esc" $APPARG 2>&1) && fail "a path climbing out with .. was taken"
echo "$out" | grep -q 'climbs out' || fail "the .. refusal did not say why: $out"
row "refused" "a path that climbs out with .."

if [ "$PLATFORM" = android ]; then
  T=/data/local/tmp/mobium-check-$$
  roundtrip shell "$T"
  adb -s "$DEV" shell rm -rf "$T"
  [ "$(adb -s "$DEV" shell ls -d "$T" 2>/dev/null)" = "" ] || fail "$T is still on the device"
  row "removed" "$T, and read back as gone"

  if adb -s "$DEV" shell pm list packages | grep -q "^package:dev.mobium.mobiumapp\$"; then
    out=$($M upload "$W/bin.dat" --app dev.mobium.mobiumapp --device-path files/x 2>&1) && fail "a release build's data was written"
    echo "$out" | grep -q "not debuggable" || fail "the release build's refusal did not say why: $out"
    row "refused" "a release build's private data, saying it is not debuggable"
  fi

  if [ -n "$MOBIUM_DEBUGGABLE_APP" ]; then
    A=$MOBIUM_DEBUGGABLE_APP
    roundtrip private "files/mobium-check-$$" --app "$A"
    adb -s "$DEV" shell run-as "$A" rm -rf "files/mobium-check-$$"
    adb -s "$DEV" shell run-as "$A" ls files | grep -q "mobium-check-$$" && fail "the app's files/mobium-check-$$ is still there"
    row "removed" "the files in $A's data, and read back as gone"
  fi
  echo PASS; exit 0
fi

# iOS
A=dev.mobium.mobiumapp
apps=$($M apps 2>&1) || fail "cannot list the apps: $(echo "$apps" | tail -1)"
echo "$apps" | grep -q "$A" || fail "$A is not installed — MobiumApp is what the container is checked in"
roundtrip container "tmp/mobium-check-$$" --app "$A"
out=$($M download --device-path Library/Preferences -o "$W/prefs" --app "$A" 2>&1) || fail "the app's own folder: $out"
row "app's own" "$(echo "$out" | sed -n 's/.*(\(a folder of [^)]*\)).*/Library\/Preferences, \1/p')"
if [ -z "$PHONE" ]; then
  C=$(xcrun simctl get_app_container "$DEV" "$A" data)
  rm -rf "$C/tmp/mobium-check-$$"
  [ -e "$C/tmp/mobium-check-$$" ] && fail "tmp/mobium-check-$$ is still in the container"
  row "removed" "tmp/mobium-check-$$, and read back as gone"
else
  # CoreDevice has no delete, and will not copy an empty folder. A folder
  # holding one empty marker, copied over with its existing content removed,
  # leaves only the marker — in the app's tmp/, which iOS clears itself.
  mkdir -p "$W/marker"; : > "$W/marker/.mobium-emptied"
  xcrun devicectl device copy to --device "$DEV" --domain-type appDataContainer --domain-identifier "$A" \
    --source "$W/marker" --destination "tmp/mobium-check-$$" --remove-existing-content true >/dev/null 2>&1 ||
    fail "could not empty tmp/mobium-check-$$ on the phone"
  left=$(xcrun devicectl device info files --device "$DEV" --domain-type appDataContainer --domain-identifier "$A" \
    --subdirectory "tmp/mobium-check-$$" --json-output - 2>/dev/null | python3 -c '
import json, sys
print(" ".join(f["relativePath"] for f in json.load(sys.stdin)["result"]["files"]))')
  [ "$left" = ".mobium-emptied" ] || fail "tmp/mobium-check-$$ still holds: $left"
  row "emptied" "tmp/mobium-check-$$ down to an empty marker, which iOS clears"
fi
echo PASS
