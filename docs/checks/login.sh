#!/bin/sh
# The login demo, end to end: mobile automation's hello world, on MobiumApp's
# Login screen, and the script to show when showing Mobium to someone.
#
#   docs/checks/login.sh <android-serial | simulator-udid | iphone-udid>
#
# Negative paths first, each judged by the message the app shows: an empty
# form, a username too short, one with characters it does not allow, a
# password too short, a wrong password and an unknown user — the last two
# told apart by nothing, as a real form does. Then the positive paths: one
# field valid while the other is not, a username sanitized on submit, the
# welcome screen reached by waiting for it rather than sleeping, and logging
# out. Throughout, a password typed is never printed by map or text.
#
# Needs MobiumApp installed (mobiumdev/mobium-app). The demo account is
# mobium / hunter2, and the screen says so.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
APP=dev.mobium.mobiumapp

case "$DEV" in
  *-*-*-*-*|????????-????????????????) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *) PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($PLATFORM)"

# says <testid> <text>: the element is on screen and reads exactly that.
says() {
  # The sheet can arrive before the wait as well as before the read, and
  # what it covers is not on screen to either.
  if ! out=$($M wait "testid=$1" --timeout 5s 2>&1); then
    case "$out" in
      *"Save Password"*) save_password
        $M wait "testid=$1" --timeout 5s >/dev/null 2>&1 || fail "$1 never appeared, waiting for \"$2\"" ;;
      *) fail "$1 never appeared, waiting for \"$2\": $out" ;;
    esac
  fi
  if ! got=$($M text "testid=$1" 2>&1); then
    case "$got" in
      *"Save Password"*) save_password; got=$($M text "testid=$1") ;;
      *) fail "$1 could not be read: $got" ;;
    esac
  fi
  [ "$got" = "$2" ] || fail "$1 reads \"$got\", want \"$2\""
}
# save_password answers iOS's "Save Password?" sheet with Not Now. It comes
# over the app after a successful login, once per username per device until
# answered, and a read of what it covers is refused, so a read that meets it
# answers it the way a person would and says so.
save_password() {
  $M tap "label=Not Now" >/dev/null || fail "\"Save Password?\" is up and Not Now could not be pressed"
  $M alert 2>/dev/null | grep -q 'a dialog is on screen' && fail "\"Save Password?\" is still up after Not Now"
  row "save password" "iOS offered to save it; Not Now pressed, the sheet gone"
}
# absent <testid>: it is not on screen.
absent() {
  $M wait "testid=$1" --for hidden --timeout 5s >/dev/null 2>&1 || fail "$1 is on screen and should not be"
}
# attempt <username> <password>: fill the form and submit it.
attempt() {
  # fill, not type: each attempt replaces the last, and type adds to it.
  $M fill testid=username "$1" >/dev/null
  $M fill testid=password "$2" >/dev/null
  $M tap testid=loginBtn >/dev/null
}
# unprinted <secret>: what was typed into the password field is shown by
# neither map nor a read of the field. Not the whole screen's text: the
# demo-account hint shows hunter2 on purpose, which is the app saying it.
unprinted() {
  { $M map; $M text testid=password; } 2>&1 | grep -qF "$1" && fail "the password \"$1\" was printed"
  return 0
}
row() { printf '    %-14s %-52s ok\n' "$1" "$2"; }

$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M tap "label=Login Demo" >/dev/null
$M wait testid=loginBtn >/dev/null
$M text | grep -q "Username: mobium" || fail "the demo-account hint is not on screen"
row "hint" "the demo account is shown to whoever picks this up"

# --- negative ---------------------------------------------------------------
$M tap testid=loginBtn >/dev/null
says userError "Enter your username."
says passError "Enter your password."
absent loginError
row "empty" "both fields say what is missing"

attempt ab hunter2
says userError "Username must be at least 3 characters."
says passOk "✓ Password meets the requirements."
row "short name" "too short, and the password confirmed valid"

attempt 'mob ium!' hunter2
says userError "Username can contain only letters, numbers, dots (.), dashes (-) and underscores (_)."
row "bad chars" "the allowed characters named"

attempt mobium abc
says userOk "✓ Username looks good."
says passError "Password must be at least 6 characters."
row "short pass" "too short, and the username confirmed valid"

attempt mobium wrongpass1
says loginError "Incorrect username or password."
absent welcomeText
unprinted wrongpass1
row "wrong pass" "rejected after the wait, and still on the form"

attempt nobody hunter2
says loginError "Incorrect username or password."
unprinted hunter2
row "no such user" "the same message: the form does not say which"

# --- positive ---------------------------------------------------------------
# Capitals are sanitized away on submit, never as typed. (Not leading
# spaces: iOS turns a double space into ". ", so a demo must not type one.)
attempt MoBium hunter2
# Signing in takes a moment and the button says so; wait, do not sleep.
says welcomeText "Welcome, mobium!"
says secretText "You are logged in."
unprinted hunter2
row "log in" "sanitized to mobium, welcomed once signed in"

# The sheet can also arrive after both reads, where it would swallow the
# log-out tap. That is what a declared rule is for: it answers the sheet when
# it is in the way of an action, and the tap's result says so.
$M dialogs --when "Save Password" --press "Not Now" >/dev/null
$M tap testid=logoutBtn >/dev/null
$M wait testid=loginBtn >/dev/null
absent loginError
absent userError
v=$($M text testid=username)
[ "$v" = "" ] || [ "$v" = "username" ] || fail "the username still reads \"$v\" after logging out"
row "log out" "back to an empty form"

$M dialogs --clear >/dev/null
$M terminate "$APP" >/dev/null 2>&1 || true
echo PASS
