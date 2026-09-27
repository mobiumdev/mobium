#!/bin/sh
# Actionability inside a WebView, end to end: a tap on a web element waits
# until the page says it can be touched — visible and in view, enabled,
# holding still, not covered — aims around a cover over its center, and
# refuses in Vibium's words when it stays untouchable. Judged by what
# MobiumApp's Actionability page says each tap reached, never by the tool's
# own report.
#
# The pointer-events:none layer is the negative control: covered to the eye
# and not to a finger, so a refusal there would be wrong. Unlike a native
# tree, the page hit-tests, so the plain div that swallows a tap is refused
# here where a native screen can only report it (CHALLENGES 115, 118).
#
#   docs/checks/web-actionability.sh <serial|udid>
#
# Needs MobiumApp installed (mobiumdev/mobium-app). Runs on emulators,
# simulators and phones: nothing on the device is changed.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-24s %-50s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp

case "$DEV" in
  ????????-????????????????|*-*-*-*-*) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *)                                   PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($PLATFORM)"
# A listing that fails is not an empty one: a phone gone from its cable
# answered "not installed" here while the real error was the device.
APPS=$($M apps 2>&1) || fail "could not list the apps: $APPS"
echo "$APPS" | grep -q "$APP" || fail "$APP is not installed — build it first (see mobium-app.sh's header)"
trap '$M context NATIVE_APP >/dev/null 2>&1; $M terminate "$APP" >/dev/null 2>&1 || true' EXIT

$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M tap testid=webviewhubBtn >/dev/null
$M scroll-to testid=actionableBtn --direction down >/dev/null 2>&1 || true
$M tap testid=actionableBtn >/dev/null
sleep 3
CTX=$($M contexts | awk '/^WEBVIEW_/{print $1; exit}')
[ -n "$CTX" ] || fail "no WebView context: $($M contexts 2>&1 | grep -m1 "^error:" || echo "mobium listed none for this app")"
$M context "$CTX" >/dev/null
MAP=$($M map)

ref() { echo "$MAP" | awk -v l="$1" 'index($0, l" (") {print $1; exit}'; }
# last is what the page says the latest tap reached; taps is how many it saw.
last() { $M eval 'actState().last' | tr -d '"'; }
taps() { $M eval 'actState().taps'; }
press() { # press <label>: tap it, keeping what mobium said and whether it refused
  r=$(ref "$1"); [ -n "$r" ] || fail "\"$1\" is not in the page's map"
  if SAID=$($M tap "$r" 2>&1); then REFUSED=""; else REFUSED=1; fi
}
refused() { # refused <label> <check> <reason words>
  before=$(taps); press "$1"
  [ -n "$REFUSED" ] || fail "\"$1\" was tapped: $SAID"
  echo "$SAID" | grep -q "failed check $2" || fail "\"$1\" was refused for the wrong reason: $SAID"
  echo "$SAID" | grep -q "$3" || fail "\"$1\"'s refusal does not say \"$3\": $SAID"
  [ "$(taps)" = "$before" ] || fail "a refused tap on \"$1\" still reached $(last)"
}
reached() { # reached <label> <what the page must say>
  press "$1"
  [ -z "$REFUSED" ] || fail "\"$1\" was refused: $SAID"
  case "$(last)" in "$2"*) ;; *) fail "the tap on \"$1\" reached \"$(last)\", not $2" ;; esac
}

# --- stable: a target sliding in is tapped once it holds still ------------
press "Replay the slide"
reached "Sliding in" "target wSlide"
ms=$(last | sed -n 's/.*wSlide \([0-9]*\)ms.*/\1/p')
[ "${ms:-0}" -ge 1900 ] || fail "the sliding target was tapped ${ms}ms after Replay, before its 2s slide ended"
row "stable" "tapped ${ms}ms after Replay, once it stopped"

# --- enabled -------------------------------------------------------------
refused "Always disabled" enabled "disabled attribute"
row "disabled" "refused: failed check enabled"
press "Arm: disable the next one for 2 s"
reached "Enabled after Arm + 2 s" "target wEnabling"
row "enabled late" "waited for it, then reached it"
refused "aria-disabled" enabled "aria-disabled"
row "aria-disabled" "refused, as Playwright and Vibium do"

# --- receives events -----------------------------------------------------
refused "Fully covered" receivesEvents "full cover"
row "full cover" "refused, naming the cover"
reached "Center covered" "target wHalf"
echo "$SAID" | grep -q "clear point" || fail "the result does not say the aim moved: $SAID"
row "center covered" "aimed at a clear point, and reached it"
reached "Pass-through" "target wPass"
row "pointer-events: none" "reached: the negative control"
refused "Under a plain div" receivesEvents "plain div"
row "plain div" "refused: the page's hit test sees it"

# --- in view -------------------------------------------------------------
reached "Below the fold" "target wBelow"
row "below the fold" "scrolled into view, and reached it"

echo PASS
