#!/bin/sh
# Typing inside a WebView, end to end: app_type fills a page's field the way
# Vibium's fill does — after the page says it can take text — and the page is
# asked what each field holds, never the tool's own report.
#
# The text input's listener mirrors it, so the check can tell "the value
# changed" from "the page heard it change". Read-only, aria-readonly and
# disabled fields must refuse, a checkbox is not a text field at all, and a
# password is confirmed by the page without ever being printed (CHALLENGES
# 119).
#
#   docs/checks/web-type.sh <serial|udid>
#
# Needs MobiumApp installed (mobiumdev/mobium-app). Runs on emulators,
# simulators and phones: nothing on the device is changed.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-22s %-52s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp

case "$DEV" in
  ????????-????????????????|*-*-*-*-*) PLATFORM=ios; M="$ROOT/bin/mobium --backend webdriveragent --device $DEV" ;;
  *)                                   PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($PLATFORM)"
APPS=$($M apps 2>&1) || fail "could not list the apps: $APPS"
echo "$APPS" | grep -q "$APP" || fail "$APP is not installed — build it first (see mobium-app.sh's header)"
trap '$M context NATIVE_APP >/dev/null 2>&1; $M terminate "$APP" >/dev/null 2>&1 || true' EXIT

$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M tap testid=webviewhubBtn >/dev/null
$M scroll-to testid=webformBtn --direction down >/dev/null 2>&1 || true
$M tap testid=webformBtn >/dev/null
sleep 3
CTX=$($M contexts | awk '/^WEBVIEW_/{print $1; exit}')
[ -n "$CTX" ] || fail "no WebView context: $($M contexts 2>&1 | grep -m1 "^error:" || echo "mobium listed none for this app")"
$M context "$CTX" >/dev/null
MAP=$($M map)

ref() { r=$(echo "$MAP" | awk -v l="$1" 'index($0, l" (") {print $1; exit}'); [ -n "$r" ] || fail "\"$1\" is not in the page's map"; echo "$r"; }
field() { $M eval "formState().$1" | sed 's/^"//; s/"$//'; }

# A checkbox is named by its label, not by its value "on".
echo "$MAP" | grep -q "A checkbox (checkbox)" || fail "the checkbox is not named by its label: $(echo "$MAP" | grep checkbox)"
row "map" "names the checkbox by its label"

# --- text that shell input would mangle, exactly ---------------------------
VALUE="O'Brien & café — naïve"
$M type "$(ref "text input")" "$VALUE" >/dev/null
[ "$(field text)" = "$VALUE" ] || fail "the text field holds \"$(field text)\", not \"$VALUE\""
[ "$(field mirror)" = "mirror: $VALUE" ] || fail "the page did not hear the change: $(field mirror)"
row "text" "quotes, & and non-ASCII exactly; the page heard it"
$M type "$(ref "email input")" "a@b.co" >/dev/null
[ "$(field email)" = "a@b.co" ] || fail "the email field holds \"$(field email)\""
$M type "$(ref "notes")" "two words" >/dev/null
[ "$(field area)" = "two words" ] || fail "the textarea holds \"$(field area)\""
row "email, textarea" "hold what was typed"

# --- clearing ------------------------------------------------------------
SAID=$($M type "$(ref "notes")" "")
echo "$SAID" | grep -q "cleared" || fail "an empty type did not say it cleared: $SAID"
[ -z "$(field area)" ] || fail "the textarea still holds \"$(field area)\" after clearing"
row "clear" "empty text clears the field"

# --- fields that must refuse ---------------------------------------------
refuse() { # refuse <label> <check> <reason words>
  if SAID=$($M type "$(ref "$1")" "nope" 2>&1); then fail "\"$1\" took text: $SAID"; fi
  echo "$SAID" | grep -q "failed check $2" || fail "\"$1\" was refused for the wrong reason: $SAID"
  echo "$SAID" | grep -q "$3" || fail "\"$1\"'s refusal does not say \"$3\": $SAID"
}
refuse "fixed" editable "readonly"
[ "$(field readonly)" = "fixed" ] || fail "the read-only field changed to \"$(field readonly)\""
refuse "aria-readonly" editable "aria-readonly"
refuse "disabled" enabled "disabled"
refuse "A checkbox" editable "not a text field"
row "refusals" "read-only, aria-readonly, disabled, checkbox"

# --- a password, confirmed and never printed ------------------------------
SECRET="s3cret!"
SAID=$($M type "$(ref "password")" "$SECRET")
echo "$SAID" | grep -q "$SECRET" && fail "the result printed the password: $SAID"
echo "$SAID" | grep -q "not echoed" || fail "the result does not say the password was not echoed: $SAID"
[ "$($M eval "passwordIs('$SECRET')")" = "true" ] || fail "the password field does not hold what was typed"
row "password" "confirmed by the page, not echoed"

echo PASS
