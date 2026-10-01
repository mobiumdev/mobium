#!/bin/sh
# A real iPhone, end to end, through Apple's own Settings app.
#
# Settings rather than an app of our own because it is on every iPhone and
# nothing here needs installing — the one thing this check installs is
# WebDriverAgent, which Mobium builds and signs for the phone on first use.
# Nothing it taps changes a setting: it navigates, scrolls, types into the
# search field and reads.
#
# Three things in it exist because they failed on the first real phone:
#
# - **An app switch stalled the next read for 61 seconds** and returned the
#   previous app's screen. Every switch here is timed, and a limit well under
#   a minute is what would catch it coming back.
# - **scroll-to stopped before its first swipe** on an off-screen row, which a
#   phone reports with no bounds. Legal & Regulatory is off screen in General.
# - **Uninstalling an app that is not installed reported success.**
#
# Needs a phone that is paired, unlocked, with Developer Mode and
# Settings > Developer > Enable UI Automation on, and Auto-Lock long enough
# not to lock mid-run. See docs/SETUP.md, "iOS real device".
#
#   docs/checks/ios-device.sh <udid>
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <udid>   (see: mobium devices)" >&2; exit 2; fi

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium --driver wda --device $DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
now() { python3 -c 'import time; print(time.time())'; }
since() { python3 -c "import time; print(round(time.time() - $1, 1))"; }

# The stall this guards against was 61s. Anything past this is a regression,
# not a slow phone: a switch and its read-back took 6.1–16.2s on an iPhone 15 Plus.
LIMIT=40
timed() { # timed <label> <expected foreground> <mobium args...>
  label="$1"; want="$2"; shift 2
  t=$(now); $M "$@" >/dev/null; got=$($M current); s=$(since "$t")
  [ "$got" = "$want" ] || fail "$label: $want expected in front, $got is"
  python3 -c "import sys; sys.exit(0 if $s < $LIMIT else 1)" \
    || fail "$label took ${s}s — the app-switch stall is back (limit ${LIMIT}s)"
  printf '    %-14s %-40s %5ss  ok\n' "$label" "$want" "$s"
}

echo "--- $DEV"
$M current >/dev/null   # starts WebDriverAgent, building it on first use

# App switches in both directions and through Home — each one is where the
# stall appeared, and each is read back rather than trusted: devicectl
# reports a launch when it is dispatched, not when the app is in front.
timed launch com.apple.mobiletimer launch com.apple.mobiletimer
timed launch com.apple.Preferences launch com.apple.Preferences
timed home com.apple.springboard press home
$M terminate com.apple.Preferences >/dev/null
timed relaunch com.apple.Preferences launch com.apple.Preferences

# Settings reopens where it was left, so walk back to its root first. The
# back button is the first entry in a sub-page's map.
for _ in 1 2 3; do
  $M map | grep -q 'Airplane Mode' && break
  $M tap @e1 >/dev/null
done
$M map | grep -q 'Airplane Mode' || fail "could not get back to the Settings root"

# Scrolling, both ways, onto rows that are off screen when asked for. On a
# phone an off-screen row's button has no bounds at all, which is the case
# that used to stop scroll-to dead.
$M scroll-to 'label=General,role=button' --direction up >/dev/null 2>&1 || true
$M tap 'label=General,role=button' >/dev/null
$M wait 'label=About' >/dev/null || fail "General did not open"
$M scroll-to 'label=Legal & Regulatory,role=button' --direction down >/dev/null \
  || fail "scroll-to did not reach an off-screen row"
$M tap 'label=Legal & Regulatory,role=button' >/dev/null
$M text | grep -q 'Legal Notices' || fail "Legal & Regulatory did not open"
echo "    scroll         off-screen row reached and opened                     ok"

# Typing, into the search field at the root. WDA.SetText reads the value back,
# so a success here is a confirmed value, not a sent one.
$M terminate com.apple.Preferences >/dev/null
$M launch com.apple.Preferences >/dev/null
for _ in 1 2 3; do
  $M map | grep -q 'Airplane Mode' && break
  $M tap @e1 >/dev/null
done
$M fill 'label=Search,role=input' 'wallpaper' >/dev/null || fail "typing into Search failed"
$M map | grep -q 'wallpaper (input)' || fail "the search field does not hold what was typed"
echo "    type           search field holds what was typed                      ok"

# A screenshot in pixels, like the simulator's — devicectl has none, so it
# comes from WebDriverAgent.
shot="$(mktemp -t mobium-shot).png"
$M screenshot -o "$shot" >/dev/null
python3 -c "import sys; sys.exit(0 if open(sys.argv[1],'rb').read(8) == b'\\x89PNG\\r\\n\\x1a\\n' else 1)" "$shot" \
  || fail "the screenshot is not a PNG"
rm -f "$shot"
echo "    screenshot     PNG                                                    ok"

# What a phone cannot do must be refused, each with the reason.
refused() { # refused <what> <phrase> <mobium args...>
  what="$1"; phrase="$2"; shift 2
  if out=$($M "$@" 2>&1); then fail "$what was accepted on a real iPhone"; fi
  echo "$out" | grep -q "$phrase" || fail "$what refused without saying why: $out"
  printf '    %-14s refused: %s\n' "$what" "$phrase"
}
# The phrase is the reason, not the refusal: until 2026-09-25 these matched
# "cannot read or change the appearance", which says no more than "no", and
# the check passed on refusals that gave no reason at all.
refused appearance "switch light and dark (simctl ui)" appearance
refused permissions "grant or revoke permissions (simctl privacy)" grant com.apple.Preferences location
refused clipboard "real iPhone's clipboard" clipboard
refused call "cannot be made to ring" call
refused uninstall "is not installed" uninstall com.example.not.installed

# Orientation reads and turns through WebDriverAgent's rotation endpoint.
# Settings supports portrait alone on an iPhone, so it reads portrait and a
# turn to landscape is refused, naming where it stayed, rather than reported
# done; Safari turning is in orientation.sh.
$M launch com.apple.Preferences >/dev/null
$M orientation | grep -q '^portrait ' || fail "Settings did not read as portrait: $($M orientation 2>&1)"
out=$($M orientation landscape 2>&1) && fail "Settings, which is portrait only, was reported turned: $out"
echo "$out" | grep -q "still portrait" || fail "the refusal did not say where it stayed: $out"
echo "    orientation    portrait, and a turn Settings does not support refused  ok"

# Recording is not refused: a phone records from WebDriverAgent's screen
# stream (record.sh has the whole check). Two seconds are about twenty frames.
vid="$(mktemp -t mobium-rec).mp4"
$M record start >/dev/null
sleep 2
frames=$($M record stop -o "$vid" --json | python3 -c "import json,sys; print(json.load(sys.stdin)['frames'])")
rm -f "$vid"
[ "$frames" -ge 5 ] || fail "two seconds of recording held $frames frames"
echo "    recording      $frames frames in two seconds, from the screen stream        ok"

# WebViews are reachable on a phone — through usbmuxd and lockdown, not the
# simulator's socket — so contexts answers rather than refusing. Settings has
# no web content, so only the native context is certain to be there.
$M contexts | grep -q '^NATIVE_APP' || fail "contexts did not answer on a real iPhone"
echo "    contexts       answered, through lockdown                             ok"

$M terminate com.apple.Preferences >/dev/null
echo "PASS"
