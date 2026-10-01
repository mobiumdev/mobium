#!/bin/sh
# Clearing an app's data, end to end, with the app as the witness. MobiumApp's
# Storage Demo keeps a count in a file in its documents directory: the check
# raises it, shows it survives a relaunch — so the store is real and "0"
# afterwards can mean something — clears the data, relaunches, and reads 0
# and "file: absent" back from the app itself.
#
#   docs/checks/clear-data.sh <android-serial | simulator-udid | iphone-udid>
#
# Needs MobiumApp installed (mobiumdev/mobium-app). On Android it also grants a
# location permission first and checks `pm clear` took it away, read back.
#
# A real iPhone cannot clear in place, and without a bundle it refuses: the
# check asserts the refusal names the reason and the reset it has. With
# MOBIUMAPP_BUNDLE=<MobiumApp.app> the reset itself is checked like the
# others, with the app as the witness — `clear-data --bundle` uninstalls and
# installs it again — and a bundle for another app is refused before
# anything is uninstalled:
#
#   MOBIUMAPP_BUNDLE=<MobiumApp.app> docs/checks/clear-data.sh <iphone-udid>
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }
APP=dev.mobium.mobiumapp

case "$DEV" in
  ????????-????????????????) PLATFORM=iphone; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *-*-*-*-*) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *)         PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($PLATFORM)"

CLEAR="clear-data $APP"
if [ "$PLATFORM" = iphone ]; then
  out=$($M clear-data "$APP" 2>&1) && fail "a real iPhone cleared an app's data in place, which nothing there can do: $out"
  echo "$out" | grep -q "cannot delete from an app's container" || fail "the refusal did not say why: $out"
  echo "$out" | grep -q -- "--bundle" || fail "the refusal did not name the reset a phone has: $out"
  echo "    refused        no bundle: the reason, and --bundle named          ok"
  if [ -z "$MOBIUMAPP_BUNDLE" ]; then
    echo "    NOT CHECKED    the reset itself — set MOBIUMAPP_BUNDLE=<MobiumApp.app>"
    exit 0
  fi
  CLEAR="clear-data $APP --bundle $MOBIUMAPP_BUNDLE"
fi

# Open the Storage screen from a cold start, so it reads the file afresh.
storage() {
  $M terminate "$APP" >/dev/null 2>&1 || true
  $M launch "$APP" >/dev/null
  $M tap testid=storageBtn >/dev/null
}
count() { $M text testid=storedCount | sed 's/^stored: //'; }

storage
before=$(count)
$M tap testid=saveOneMoreBtn >/dev/null
$M tap testid=saveOneMoreBtn >/dev/null
raised=$(count)
[ "$raised" -eq $((before + 2)) ] || fail "two saves took the count from $before to $raised"
storage
[ "$(count)" -eq "$raised" ] || fail "the count did not survive a relaunch: $raised became $(count)"
echo "    witness        $raised stored, and still $raised after a relaunch            ok"

if [ "$PLATFORM" = android ]; then
  adb -s "$DEV" shell pm grant "$APP" android.permission.ACCESS_FINE_LOCATION
  # pm grant exits 0 having changed nothing, so the grant is read back too:
  # without it the revocation below would pass for having nothing to revoke.
  adb -s "$DEV" shell dumpsys package "$APP" | grep -q 'ACCESS_FINE_LOCATION: granted=true' ||
    fail "the location grant did not take, so its revocation proves nothing"
fi
out=$($M $CLEAR --json)
echo "$out" | json 'len(d["emptied"])' | grep -qv '^0$' || fail "nothing was reported emptied: $out"
if [ "$PLATFORM" = iphone ]; then
  # Permissions are reset by the uninstall and cannot be read from outside,
  # so they must be said apart from what was read back.
  echo "$out" | json 'len(d.get("not_read_back") or [])' | grep -qv '^0$' ||
    fail "the reset did not say what it could not read back: $out"
  echo "    reset          reinstalled from the bundle, container read back   ok"
fi
if [ "$PLATFORM" = android ]; then
  echo "$out" | json 'd.get("still_granted")' | grep -q ACCESS_FINE_LOCATION &&
    fail "the location grant survived pm clear: $out"
  echo "    permissions    the location grant was revoked, read back          ok"
fi

storage
[ "$(count)" -eq 0 ] || fail "the count read $(count) after clearing"
[ "$($M text testid=storedFile)" = "file: absent" ] || fail "the app still finds its file"
echo "    cleared        the app reads 0 and no file after a relaunch       ok"

if [ "$PLATFORM" = iphone ]; then
  # MobiumApp's bundle named for another app: refused on the bundle id,
  # before the other app is touched, and MobiumApp is left installed.
  out=$($M clear-data com.example.not.installed --bundle "$MOBIUMAPP_BUNDLE" 2>&1) &&
    fail "a reset ran from another app's bundle: $out"
  echo "$out" | grep -q "is $APP, not com.example.not.installed" || fail "the refusal did not say why: $out"
  $M apps | grep -q "^$APP " || fail "MobiumApp is gone after a refused reset"
  echo "    refused        another app's bundle, and nothing uninstalled      ok"
else
  out=$($M clear-data com.example.not.installed 2>&1) && fail "an app that is not installed was cleared: $out"
  echo "$out" | grep -q "is not installed" || fail "the refusal did not say why: $out"
  echo "    refused        an app that is not installed                       ok"
fi
$M terminate "$APP" >/dev/null 2>&1 || true
