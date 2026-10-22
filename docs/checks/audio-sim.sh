#!/bin/sh
# Audio capture on an iOS simulator, on MobiumApp's Audio Demo: what the app
# plays, heard on the Mac through a process tap on the app alone, and judged
# against what the demo is known to play.
#
#   docs/checks/audio-sim.sh <simulator-udid>
#
# - A tone is heard as 440 Hz for about two seconds; the sequence as 440 Hz,
#   a second of silence and 880 Hz, in that order.
# - The Silent button plays a player writing zeros: the platform calls it
#   playing, and the capture hears silence — the negative control.
# - An expect that does not match fails as not_confirmed, says what was
#   heard, and keeps the WAV.
# - An app that never opens audio — Settings — is said to have never
#   opened it, not reported as an app that played silence.
#
# Needs MobiumApp's Audio Demo (mobiumdev/mobium-app#21), Xcode (the
# capture's helper is built with its swiftc on first use) and macOS's System
# Audio Recording allowed for what runs mobium; without it every capture is
# refused, naming it. The app's sound is muted on the Mac while tapped.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <simulator-udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_platform ios "$DEV"
case "$(xcrun simctl list devices | grep -c "$DEV")" in
  0) echo "audio-sim.sh is for a simulator; $DEV is not one — an iPhone's audio is not captured" >&2; exit 2 ;;
esac
check_lock "$DEV"
M="$ROOT/bin/mobium --driver wda --device $DEV"
APP=dev.mobium.mobiumapp
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
echo "--- $DEV"

apps=$($M apps 2>&1) || fail "cannot list the apps: $(echo "$apps" | tail -1)"
echo "$apps" | grep -q "$APP" || fail "$APP is not installed — build and install MobiumApp first"
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M scroll-to "label=Audio Demo" >/dev/null 2>&1 || true
$M tap "label=Audio Demo" >/dev/null || fail "MobiumApp has no Audio Demo — rebuild it"
$M wait testid=audioState >/dev/null || fail "the Audio Demo did not open"
at_exit "$M tap testid=audioStop >/dev/null 2>&1 || true; $M audio stop -o '$CHECK_TMP/left.wav' >/dev/null 2>&1 || true"

# play BUTTON NAME EXPECT: capture while BUTTON plays, and hold it to EXPECT.
play() {
  out=$($M audio start 2>&1) || fail "audio start was refused: $out"
  $M tap "testid=$1" >/dev/null
  $M wait testid=audioState --for text --text finished --timeout 15s >/dev/null ||
    fail "the Audio Demo still says \"$($M text testid=audioState)\""
  sleep 0.5
  out=$($M audio stop -o "$CHECK_TMP/$2.wav" --expect "$3" 2>&1) || fail "$2: $out"
  echo "$out" | grep -q "never opened audio" && fail "$2: the app was not heard at all: $out"
  [ -s "$CHECK_TMP/$2.wav" ] || fail "$2: no WAV saved"
}

play audioTone tone 440:1.7-2.3
row "tone" "440 Hz for about two seconds"
play audioSequence sequence 440:1.7-2.3,880:1.7-2.3
row "sequence" "440 Hz, then 880 Hz, in that order"
play audioSilent silent silence
row "silent" "a player writing zeros is heard as silence"

$M audio start >/dev/null
$M tap testid=audioTone >/dev/null
$M wait testid=audioState --for text --text finished --timeout 15s >/dev/null
set +e; out=$($M audio stop -o "$CHECK_TMP/wrong.wav" --expect 660 2>&1); st=$?; set -e
[ $st -ne 0 ] || fail "an expect of 660 Hz passed a 440 Hz tone: $out"
echo "$out" | grep -q "440 Hz" || fail "the failure does not say what was heard: $out"
[ -s "$CHECK_TMP/wrong.wav" ] || fail "the failed expect did not keep the WAV"
row "expect" "660 for a 440 tone fails, says 440, keeps the WAV"

$M launch com.apple.Preferences >/dev/null
$M audio start >/dev/null
sleep 2
out=$($M audio stop -o "$CHECK_TMP/settings.wav" 2>&1) || fail "Settings' capture: $out"
echo "$out" | grep -q "never opened audio" || fail "Settings, which opens no audio, was reported as: $out"
$M terminate com.apple.Preferences >/dev/null 2>&1 || true
row "never" "Settings, which opens no audio, is said never to have"
echo PASS
