#!/bin/sh
# Audio capture, on MobiumApp's Audio Demo: known tones and silence, heard
# through `mobium audio` and checked by the timeline it answers with.
#
#   docs/checks/audio.sh <emulator-serial>
#
# An Android emulator only: the capture reads the emulator's control port,
# and a phone or an iOS device is refused — this check refuses them by name
# rather than reporting what it could not hear.
#
#   - 440 Hz for 2 s is heard as 440 Hz for about 2 s;
#   - 440, a second of silence, then 880, in that order, the silence between;
#   - 660 Hz played as an alarm is heard as 660 Hz;
#   - a tone until stopped ends when Stop is pressed, not when it would have;
#   - silence, the negative control, is heard as silence — while the
#     platform reports a player started for it, just as it does for a tone.
#     That contrast is what the capture is for;
#   - an incoming call silences the tone while it rings, and it comes back
#     after the hang-up, while the app says it played throughout.
#
# Each capture may also hold a tap's own click when touch sounds are on: a
# tenth of a second of sound with no one pitch, or one near 780 Hz. The
# assertions allow sounds of 0.2 s or less besides the tones, and say how
# many they saw.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <emulator-serial>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
case "$DEV" in
  emulator-*) ;;
  *-*-*-*-*|????????-????????????????)
    echo "audio.sh is for an Android emulator: iOS audio is not captured yet" >&2; exit 2 ;;
  *) echo "audio.sh is for an Android emulator: $DEV is a phone, whose audio is not captured yet" >&2; exit 2 ;;
esac
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-10s %-60s ok\n' "$1" "$2"; }
M="$ROOT/bin/mobium --device $DEV"
APP=dev.mobium.mobiumapp
echo "--- $DEV"

APPS=$($M apps 2>&1) || fail "could not list the apps: $APPS"
echo "$APPS" | grep -q "$APP" || fail "$APP is not installed — build it first (see docs/checks/mobium-app.sh)"
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M scroll-to "text=Audio Demo" >/dev/null 2>&1 || true
$M tap "text=Audio Demo" >/dev/null || fail "MobiumApp has no Audio Demo — rebuild it"
$M wait testid=audioState >/dev/null || fail "the Audio Demo did not open"
at_exit "$M tap testid=audioStop >/dev/null 2>&1 || true; $M audio stop -o '$CHECK_TMP/left.wav' >/dev/null 2>&1 || true"

state() { $M text testid=audioState; }

# ended waits for the app to say the sound is over, finished or stopped.
ended() {
  i=0
  while [ $i -lt 40 ]; do
    case "$(state)" in finished:*|stopped:*) return 0 ;; esac
    sleep 0.25; i=$((i + 1))
  done
  fail "the Audio Demo still says \"$(state)\" after 10 s"
}

# capture BUTTON NAME: capture while BUTTON plays, and keep the timeline as
# $CHECK_TMP/NAME.json.
capture() {
  $M audio start >/dev/null || fail "audio start was refused"
  $M tap "testid=$1" >/dev/null
  ended
  sleep 0.5
  $M --json audio stop -o "$CHECK_TMP/$2.wav" > "$CHECK_TMP/$2.json" || fail "audio stop failed: $(cat "$CHECK_TMP/$2.json")"
}

# expect NAME PITCHES...: the timeline's sounds longer than 0.2 s are these
# pitches, in this order, each about 2 s unless given as HZ:MIN:MAX seconds.
# Prints how many short sounds it allowed besides.
expect() {
  name=$1; shift
  python3 - "$CHECK_TMP/$name.json" "$@" <<'EOF'
import json, sys
v = json.load(open(sys.argv[1]))
want = []
for w in sys.argv[2:]:
    hz, lo, hi = (w.split(":") + ["1.7", "2.3"])[:3] if ":" in w else (w, "1.7", "2.3")
    want.append((float(hz), float(lo), float(hi)))
segs = v.get("timeline") or []
long_ = [s for s in segs if s["sound"] and (s["to"] - s["from"]) / 1e9 > 0.2]
short = [s for s in segs if s["sound"] and (s["to"] - s["from"]) / 1e9 <= 0.2]
def show(s): return "%.0f Hz %.1f-%.1fs" % (s.get("hz", 0), s["from"] / 1e9, s["to"] / 1e9)
if len(long_) != len(want):
    sys.exit("heard %s, want %s" % ([show(s) for s in long_] or "silence", [w[0] for w in want] or "silence"))
for s, (hz, lo, hi) in zip(long_, want):
    secs = (s["to"] - s["from"]) / 1e9
    if abs(s.get("hz", 0) - hz) > max(5, 0.02 * hz) or not lo <= secs <= hi:
        sys.exit("heard %s, want %.0f Hz for %s to %s s" % (show(s), hz, lo, hi))
print(len(short))
EOF
}

out=$($M audio stop -o "$CHECK_TMP/none.wav" 2>&1) && fail "audio stop with nothing capturing was accepted: $out"
$M audio start >/dev/null
out=$($M audio start 2>&1) && fail "a second audio start was accepted: $out"
$M audio stop -o "$CHECK_TMP/empty.wav" >/dev/null
row "order" "stop before start and a second start are refused"

capture audioTone tone
n=$(expect tone 440) || fail "440 Hz for 2 s: $n"
row "tone" "440 Hz for about 2 s ($n short sound(s) besides)"

capture audioSequence sequence
n=$(expect sequence 440 880) || fail "440, a pause, then 880: $n"
python3 - "$CHECK_TMP/sequence.json" <<'EOF' > "$CHECK_TMP/gap.txt" 2>&1 || fail "the pause between 440 and 880 was not about a second of silence: $(cat "$CHECK_TMP/gap.txt")"
import json, sys
segs = json.load(open(sys.argv[1]))["timeline"]
tones = [s for s in segs if s["sound"] and (s["to"] - s["from"]) / 1e9 > 0.2]
gap = (tones[1]["from"] - tones[0]["to"]) / 1e9
quiet = [s for s in segs if s["from"] >= tones[0]["to"] and s["to"] <= tones[1]["from"]]
if not (0.8 <= gap <= 1.2 and all(not s["sound"] for s in quiet)):
    sys.exit("heard " + ", ".join("%.1f-%.1fs %s" % (s["from"] / 1e9, s["to"] / 1e9,
        ("%.0f Hz" % s.get("hz", 0)) if s["sound"] else "silence") for s in segs))
EOF
row "sequence" "440 Hz, about a second of silence, then 880 Hz"

capture audioAlarm alarm
n=$(expect alarm 660) || fail "660 Hz as an alarm: $n"
row "alarm" "660 Hz played as an alarm, heard as 660 Hz"

$M audio start >/dev/null
$M tap testid=audioLoop >/dev/null
sleep 1.5
$M tap testid=audioStop >/dev/null
ended
sleep 1
$M --json audio stop -o "$CHECK_TMP/stop.wav" > "$CHECK_TMP/stop.json"
case "$(state)" in stopped:*) ;; *) fail "the loop says \"$(state)\" after Stop" ;; esac
n=$(expect stop 440:0.8:2.5) || fail "a tone stopped after 1.5 s: $n"
row "stop" "a tone until stopped ends at Stop, not a minute later"

# The platform's own word, during the silence: a player started, as for a
# tone. Sampled from dumpsys while the silence plays.
uid=$(adb -s "$DEV" shell cmd package list packages -U "$APP" | sed -n 's/.*uid:\([0-9]*\).*/\1/p' | head -1)
$M audio start >/dev/null
$M tap testid=audioSilent >/dev/null
started=$(adb -s "$DEV" shell dumpsys audio | grep "AudioPlaybackConfiguration" | grep "u/pid:$uid/" | grep -c "state:started" || true)
ended
sleep 0.5
$M --json audio stop -o "$CHECK_TMP/silent.wav" > "$CHECK_TMP/silent.json"
[ "${started:-0}" -ge 1 ] || fail "the platform reported no player started during the silence — the contrast is not shown"
n=$(expect silent) || fail "silence: $n"
row "silence" "heard as silence ($n short sound(s)), while the platform said playing"

# An incoming call, while the app plays: Android silences the app for as
# long as it rings and gives it back after, while the app itself says it is
# playing throughout. The capture hears the ring in its place — so the tone
# stops before the call and comes back after the hang-up, and something
# else sounds in between.
$M audio start >/dev/null
$M tap testid=audioLoop >/dev/null
sleep 2
$M call ring >/dev/null || fail "the emulator would not ring"
sleep 5
$M call hang >/dev/null || fail "the call would not end"
sleep 2.5
$M tap testid=audioStop >/dev/null
ended
$M --json audio stop -o "$CHECK_TMP/call.wav" > "$CHECK_TMP/call.json"
python3 - "$CHECK_TMP/call.json" > "$CHECK_TMP/call.txt" 2>&1 <<'EOF' || fail "a call during a tone: $(cat "$CHECK_TMP/call.txt")"
import json, sys
segs = json.load(open(sys.argv[1]))["timeline"]
def show(): return ", ".join("%.1f-%.1fs %s" % (s["from"] / 1e9, s["to"] / 1e9,
    ("%.0f Hz" % s.get("hz", 0)) if s["sound"] else "silence") for s in segs)
tones = [s for s in segs if s["sound"] and abs(s.get("hz", 0) - 440) <= 9 and (s["to"] - s["from"]) / 1e9 > 0.5]
if len(tones) < 2:
    sys.exit("the tone did not stop and come back: " + show())
first, last = tones[0], tones[-1]
between = [s for s in segs if s["from"] >= first["to"] and s["to"] <= last["from"]]
if any(s["sound"] and abs(s.get("hz", 0) - 440) <= 9 for s in between):
    sys.exit("the tone went on while it rang: " + show())
if not any(s["sound"] for s in between):
    sys.exit("nothing rang between: " + show())
print("%.1fs" % ((last["from"] - first["to"]) / 1e9))
EOF
row "call" "a call silences the tone while it rings ($(cat "$CHECK_TMP/call.txt")), then gives it back"

# The same through mobium test: an expect at the stop passes on what was
# played and fails, saying what was heard, on what was not — with the
# capture still saved.
(cd "$ROOT/tests" && MOBIUM_SESSION= "$ROOT/bin/mobium" test audio/audio.test.json --project android \
  --output "$CHECK_TMP/report" > "$CHECK_TMP/test-pass.txt" 2>&1) || fail "tests/audio failed: $(tail -5 "$CHECK_TMP/test-pass.txt")"
(cd "$ROOT/tests" && MOBIUM_SESSION= "$ROOT/bin/mobium" test controls/audio-must-fail.test.json --project android \
  --output "$CHECK_TMP/report" > "$CHECK_TMP/test-fail.txt" 2>&1) && fail "the must-fail audio tests passed"
grep -q "expected 880 Hz; heard 440 Hz" "$CHECK_TMP/test-fail.txt" || fail "a wrong pitch did not say what was heard: $(tail -5 "$CHECK_TMP/test-fail.txt")"
grep -q "expected silence; heard 440 Hz" "$CHECK_TMP/test-fail.txt" || fail "silence expected did not say what was heard"
[ "$(grep -c "the capture is saved at" "$CHECK_TMP/test-fail.txt")" -ge 2 ] || fail "a failed expect did not keep its capture"
row "test" "mobium test: expect passes on what played, fails saying what was heard"

echo "PASS"
