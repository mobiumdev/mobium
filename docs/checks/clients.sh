#!/bin/sh
# All five clients end to end, one after another, against a booted Android
# device — the release checklist's "one end-to-end script per language",
# written down so nobody has to invent them again.
#
#   docs/checks/clients.sh <android-serial>
#
# Each client runs the same flow through its own API: launch, wait for a row
# and tap it by the ref that came back, go back, scroll to a row below the
# fold, grant and revoke a permission, read the device log and crash reports,
# fail on a missing element with the client's own exception, and terminate.
# Every step names what is on both an emulator and a phone: Settings' last
# row is "About emulated device" on one and "About phone" on the other, so
# the flows scroll to "text=About", a substring match — as first written they
# named the emulator's and could only pass there.
# The flows live beside each client's unit tests and skip themselves unless
# MOBIUM_E2E_DEVICE is set, so `make ci` never needs a device.
#
# A client whose toolchain is missing is reported as skipped, not passed.
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <android-serial>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT" || exit 1
MOBIUM_E2E_DEVICE="$DEV"; MOBIUM_BIN_PATH="$ROOT/bin/mobium"
export MOBIUM_E2E_DEVICE MOBIUM_BIN_PATH

JAVA_HOME_GUESS="$(/usr/libexec/java_home 2>/dev/null || echo /opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home)"
JAVA="$(command -v java 2>/dev/null || echo "$JAVA_HOME_GUESS/bin/java")"
DOTNET="$(command -v dotnet 2>/dev/null || echo "$HOME/.dotnet/dotnet")"

failed=0
run() {
  name="$1"; shift
  echo "--- $name"
  if "$@"; then echo "    $name passed"; else echo "FAIL: $name" >&2; failed=1; fi
}
skip() { echo "--- $1"; echo "    $1 SKIPPED -- $2"; }

echo "--- $DEV"
run python python3 clients/python/tests/e2e.py
run javascript node clients/javascript/test/e2e.mjs
# No pipe into grep here: its exit status would stand in for go test's, and a
# failing test prints a line grep would happily match.
run go sh -c 'cd clients/go && go test -count=1 -run EndToEnd ./...'
if [ -x "$JAVA" ]; then
  run java sh -c "make -s java >/dev/null && \"$JAVA\" -cp clients/java/target/classes dev.mobium.E2E"
else
  skip java "no JDK"
fi
if [ -x "$DOTNET" ]; then
  run dotnet "$DOTNET" run --project clients/dotnet/Tests -- e2e
else
  skip dotnet "no dotnet SDK"
fi
exit $failed
