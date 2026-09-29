#!/bin/sh
# A Jetpack Compose app: Seal, a video downloader from F-Droid, open source
# and needing no account. Everything third-party driven before it — Wikipedia,
# F-Droid, Aegis — is built from Android views, and Compose is how most new
# Android apps are written. It draws its own tree and hands the platform a
# semantics tree instead, and its first screens broke three things
# (CHALLENGES 176–178). Each is asserted here on the real app:
#
# - Its icon buttons map once each, as buttons: Compose wraps each in a
#   long-clickable tooltip box a pixel off the button's own bounds (176).
# - Its first-launch dialog is a dialog to Mobium, which the platform's alert
#   endpoint does not know: `alert` reads it and refuses to accept it, a tap on
#   what it covers is refused naming it, and a rule answers it (177).
# - A switch row is checked and unchecked by its words, where the state is
#   the row's and the words are a child, and read back each time (178).
#
#   docs/checks/compose-app.sh <serial> [path-to-apk]
#
# Without an APK it expects com.junkfood.seal installed. To fetch the arm64
# build F-Droid publishes (version codes end in 1-4 by ABI; 2 is arm64):
#   curl -L -o seal.apk https://f-droid.org/repo/com.junkfood.seal_11312.apk
#
# Seal's data is cleared at the start, so its guide shows. An app this check
# installed is uninstalled at the end; one that was there is left, with the
# setting it changes put back. On a real phone nothing read off it is ever
# printed.
set -e
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEV="${1:?usage: compose-app.sh <serial> [path-to-apk]}"
APK="$2"
APP=com.junkfood.seal
M="$ROOT/bin/mobium --device $DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
# On a phone a failure quotes nothing read off it: what is on screen when
# something goes wrong may not be Seal's (CHALLENGES 174). Assert whether.
PHONE=
case "$DEV" in emulator-*) ;; *) PHONE=1 ;; esac
seen() { if [ -n "$PHONE" ]; then echo "(not shown on a real phone)"; else printf '%s\n' "$*" | head -20; fi; }
row() { printf '    %-14s %-58s ok\n' "$1" "$2"; }

INSTALLED=
cleanup() {
  $M dialogs --clear >/dev/null 2>&1 || true
  if [ -n "$INSTALLED" ]; then
    $M uninstall $APP >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true
echo "--- $DEV"
apps=$($M apps 2>&1) || fail "cannot list the apps: $(echo "$apps" | tail -1)"
if ! echo "$apps" | grep -q "$APP"; then
  [ -n "$APK" ] || fail "$APP is not installed — pass the APK (see the header for where to get it)"
  $M install "$APK" >/dev/null
  INSTALLED=1
fi

$M clear-data $APP >/dev/null
$M launch $APP >/dev/null
$M wait "text=User guide" --timeout 15s >/dev/null || fail "Seal's first-launch guide did not show"

# --- the guide: an app's own dialog -----------------------------------------
$M alert | grep -q 'a dialog is on screen: "User guide' || fail "alert did not see Seal's dialog: $(seen "$($M alert 2>&1)")"
set +e
$M alert accept >/dev/null 2>&1; st=$?
out=$($M tap "label=Settings" 2>&1); st2=$?
set -e
[ $st -eq 5 ] || fail "alert accept on an app's own dialog exited $st, want 5 (unsupported)"
[ $st2 -eq 3 ] && echo "$out" | grep -q '"User guide"' ||
  fail "a tap under the dialog was not refused naming it (exit $st2): $(seen "$out")"
row "dialog" "read, not accepted, and a tap under it refused naming it"

$M dialogs --when "User guide" --press "Close" >/dev/null
$M tap "label=Settings" | grep -q 'answered by a dialog rule on the way: "User guide" — pressed "Close"' ||
  fail "the rule did not answer Seal's dialog"
$M dialogs --clear >/dev/null
$M wait "text=Look & feel" >/dev/null
row "rule" "answered the dialog by its Close button, then the tap went on"

# --- icon buttons, once each ------------------------------------------------
$M press back >/dev/null
$M wait "label=Settings" >/dev/null
map=$($M map)
for b in "Settings" "Running tasks" "Downloads"; do
  n=$(echo "$map" | grep -c "^@e[0-9]* $b\( (button)\)*\$")
  [ "$n" = 1 ] || fail "$b mapped $n times: $(seen "$map")"
  echo "$map" | grep -q "^@e[0-9]* $b (button)\$" || fail "$b is not a button: $(seen "$map")"
done
row "map" "each icon button once, as a button"

# --- a switch row, by its words ---------------------------------------------
$M tap "label=Settings" >/dev/null
$M tap "text=Look & feel" >/dev/null
$M wait "text=Dynamic color" >/dev/null
was=$($M map | grep 'Dynamic color' | grep -o 'checked\|unchecked' | tail -1)
[ -n "$was" ] || fail "the Dynamic color row reports no state"
other=checked; verb=check
if [ "$was" = checked ]; then other=unchecked; verb=uncheck; fi
$M $verb "text=Dynamic color" | grep -q "is now $other" || fail "$verb by its words did not report it $other"
$M map | grep 'Dynamic color' | grep -q "(button, $other)" || fail "map does not show it $other"
$M wait "text=Dynamic color" --for "$other" >/dev/null
back=check; [ "$was" = unchecked ] && back=uncheck
$M $back "text=Dynamic color" | grep -q "is now $was" || fail "could not put Dynamic color back to $was"
set +e
$M check "text=Display language" >/dev/null 2>&1; st=$?
set -e
[ $st -eq 2 ] || fail "a row with no state was not refused (exit $st)"
row "switch" "set by its words, read back, put back; a plain row refused"

echo PASS
