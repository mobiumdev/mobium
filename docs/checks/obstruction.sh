#!/bin/sh
# Receives events, end to end: a tap reaches its target, not something the
# app drew over it — or says why it cannot. Judged by what MobiumApp's
# Obstruction Demo says it received, never by the tool's own report
# (CHALLENGES 115).
#
# Every cover on the screen is itself pressable, so the outcome line names
# whatever a tap really touched. A control over all of a target is refused,
# and naming it is part of the refusal; one over the center only is aimed
# around; a toast is waited out. Something over the point that is not a
# control is tapped through and reported: the pass-through case is the
# negative control, reaching its target, and the plain view swallows the tap
# with the report the only warning. On iOS an overlay hidden from
# accessibility is not in the tree at all, and the tap still lands on it —
# asserted, so that fixing it changes this check on purpose. `mobium
# hit-test` sees it, on a simulator and opt-in: hit-test.sh.
#
#   docs/checks/obstruction.sh <serial|udid>
#
# Needs MobiumApp installed (mobiumdev/mobium-app). Runs on emulators,
# simulators and phones: nothing on the device is changed.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-14s %-54s ok\n' "$1" "$2"; }
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
trap '$M terminate "$APP" >/dev/null 2>&1 || true' EXIT

$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M scroll-to "testid=obstructionBtn" --direction down >/dev/null 2>&1 || true
$M tap "testid=obstructionBtn" >/dev/null
$M wait "testid=obstructionOutcome" >/dev/null

# outcome is the app's line — "<taps>: <what received the last one>" — read
# after scrolling back to it, since a tap far down the list moves it away.
outcome() {
  $M scroll-to "testid=obstructionOutcome" --direction up >/dev/null 2>&1 || true
  $M text "testid=obstructionOutcome" | tr -d '\n'
}
# press <target>: tap it and keep what mobium said and whether it refused.
press() {
  if SAID=$($M tap "testid=$1" 2>&1); then REFUSED=""; else REFUSED=1; fi
}

before=$(outcome)

# --- a control over all of it: refused, naming the cover -------------------
press fullTarget
[ -n "$REFUSED" ] || fail "a tap under a full cover was not refused: $SAID"
echo "$SAID" | grep -q '"full cover"' || fail "the refusal does not name the cover: $SAID"
[ "$(outcome)" = "$before" ] || fail "a refused tap still touched something: $(outcome)"
row "full cover" "refused, naming \"full cover\""

# --- a control over the center: aimed around ------------------------------
press halfTarget
[ -z "$REFUSED" ] || fail "a partly covered target was refused: $SAID"
echo "$SAID" | grep -q 'clear point' || fail "the result does not say the aim moved: $SAID"
case "$(outcome)" in *"target halfTarget") ;; *) fail "the tap reached $(outcome), not halfTarget" ;; esac
row "center" "aimed at a clear point, and reached the target"

# --- the edge covered, the center clear: an ordinary tap ------------------
press edgeTarget
case "$(outcome)" in *"target edgeTarget") ;; *) fail "the tap reached $(outcome), not edgeTarget" ;; esac
echo "$SAID" | grep -q 'cover' && fail "a tap with a clear center reported a cover: $SAID"
row "edge" "reached the target, nothing reported"

# --- pointerEvents none: the negative control ------------------------------
press passTarget
[ -z "$REFUSED" ] || fail "the pass-through case, which a tap reaches, was refused: $SAID"
case "$(outcome)" in *"target passTarget") ;; *) fail "the tap reached $(outcome), not passTarget" ;; esac
echo "$SAID" | grep -q 'may take the touch' || fail "what is over the point was not reported: $SAID"
row "pass-through" "reached the target, and the view over it reported"

# --- a plain view with no handler: swallowed, and reported -----------------
before=$(outcome)
press plainTarget
[ -z "$REFUSED" ] || fail "the plain-view case was refused, which the tree cannot justify: $SAID"
echo "$SAID" | grep -q 'may take the touch' || fail "the view that swallows the tap was not reported: $SAID"
[ "$(outcome)" = "$before" ] || fail "a tap under a plain view reached $(outcome) — the screen has changed"
row "plain view" "swallowed, as measured, and reported"

# --- an overlay hidden from accessibility ----------------------------------
before=$(outcome)
press hiddenTarget
if [ "$PLATFORM" = android ]; then
  [ -n "$REFUSED" ] || fail "a tap under a hidden overlay was not refused: $SAID"
  [ "$(outcome)" = "$before" ] || fail "a refused tap still touched something: $(outcome)"
  row "hidden" "refused: UiAutomator2's tree holds the overlay"
else
  # The blind spot: WebDriverAgent's tree does not contain the overlay.
  [ -z "$REFUSED" ] || fail "iOS refused the hidden overlay — the blind spot is closed; update this check: $SAID"
  case "$(outcome)" in *"cover hidden overlay") ;; *) fail "the tap reached $(outcome), not the overlay" ;; esac
  row "hidden" "not in the tree, tap lands on it; hit-test.sh sees it"
fi

# --- a translucent scrim: refused -----------------------------------------
before=$(outcome)
press scrimTarget
[ -n "$REFUSED" ] || fail "a tap under a scrim was not refused: $SAID"
[ "$(outcome)" = "$before" ] || fail "a refused tap still touched something: $(outcome)"
row "scrim" "refused"

# --- a toast: waited out ---------------------------------------------------
$M tap "testid=toastBtn" >/dev/null
press toastTarget
[ -z "$REFUSED" ] || fail "the toast was not waited out: $SAID"
case "$(outcome)" in *"target toastTarget") ;; *) fail "the tap reached $(outcome), not toastTarget" ;; esac
row "toast" "waited out, then reached the target"

echo PASS
