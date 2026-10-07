#!/bin/sh
# The checks' prelude, lib.sh, tested without a device: handlers run newest
# first and before the daemon stops, a session of its own unless the caller
# gave one, its folder removed, the exit status kept, and one check per
# device — refused while the holder lives, taken from one that has gone,
# shared with a check it runs. Run by `make ci`.
#
#   docs/checks/lib-selftest.sh
set -e
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LIB="$ROOT/docs/checks/lib.sh"
H=$(mktemp -d); export MOBIUM_HOME=$H; unset MOBIUM_SESSION CHECK_LOCK_OWNER
pass() { echo "ok   $1"; }; bad() { echo "FAIL $1"; exit 1; }
# 1. handlers run newest first, then the session is gone, tmp removed
out=$(sh -c '. "$1"; echo "$MOBIUM_SESSION" > "$2/sess"; echo "$CHECK_TMP" > "$2/tmp"; at_exit "echo first"; at_exit "echo second"' x "$LIB" "$H")
[ "$out" = "second
first" ] && pass "handlers newest first" || bad "handlers order: $out"
case "$(cat $H/sess)" in ck*) pass "own session ($(cat $H/sess))";; *) bad "no session";; esac
[ ! -d "$(cat $H/tmp)" ] && pass "CHECK_TMP removed" || bad "tmp left"
# 2. a caller's session is kept
s=$(MOBIUM_SESSION=given sh -c '. "$1"; echo $MOBIUM_SESSION' x "$LIB"); [ "$s" = given ] && pass "caller's session kept" || bad "session overridden: $s"
# 3. at_exit_clear drops the check's handlers
out=$(sh -c '. "$1"; at_exit "echo dropped"; at_exit_clear' x "$LIB"); [ -z "$out" ] && pass "at_exit_clear" || bad "clear: $out"
# 4. exit status kept
sh -c '. "$1"; at_exit "true"; exit 3' x "$LIB" || st=$?; [ "${st:-0}" = 3 ] && pass "exit status kept" || bad "status $st"
# 5. a second check on the same device is refused; a stale lock is taken; a nested check shares
sh -c '. "$1"; check_lock DEV1; sleep 3' x "$LIB" & holder=$!; sleep 1
if sh -c '. "$1"; check_lock DEV1' x "$LIB" 2>"$H/err"; then bad "second check not refused"; else grep -q "in use by another check" "$H/err" && pass "second check refused"; fi
wait $holder
[ ! -d "$H/locks/DEV1" ] && pass "lock released at exit" || bad "lock left"
mkdir -p "$H/locks/DEV2"; echo 999999 > "$H/locks/DEV2/pid"
sh -c '. "$1"; check_lock DEV2' x "$LIB" && pass "stale lock taken" || bad "stale lock refused"
out=$(sh -c '. "$1"; check_lock DEV3; sh -c ". \"\$1\"; check_lock DEV3 && echo nested-ok" y "$1"' x "$LIB"); [ "$out" = nested-ok ] && pass "nested check shares the lock" || bad "nested: $out"
# 6. a check that does not parse is refused, not passed: under set -e the
#    trap was handed a syntax error as status 0. One edited while it runs
#    is not a pass either.
mkdir -p "$H/r/docs/checks"; cp "$LIB" "$H/r/docs/checks/"
printf '#!/bin/sh\nset -e\nROOT="$(cd "$(dirname "$0")/../.." && pwd)"\n. "$ROOT/docs/checks/lib.sh"\necho ran\nif true; then\n  echo x )\nfi\n' > "$H/r/docs/checks/broken.sh"
out=$(sh "$H/r/docs/checks/broken.sh" 2>&1) && bad "a check that does not parse passed" || st=$?
[ "$st" = 2 ] && ! echo "$out" | grep -q '^ran$' && pass "a check that does not parse is refused" || bad "unparsable check: exit $st, $out"
printf '#!/bin/sh\nset -e\nROOT="$(cd "$(dirname "$0")/../.." && pwd)"\n. "$ROOT/docs/checks/lib.sh"\necho "# edited" >> "$0"\n' > "$H/r/docs/checks/edited.sh"
sh "$H/r/docs/checks/edited.sh" 2>/dev/null && bad "a check edited while it ran passed" || st=$?
[ "$st" = 2 ] && pass "a check edited while it ran is not a pass" || bad "edited check: exit $st"
rm -rf "$H"
# 7. every check parses and is on the prelude, but for these, and none sets its own EXIT
#    trap, which would replace the prelude's. clean-stop.sh stops the
#    caller's daemon, so it must not have one of its own; the test-runner,
#    grid and test-ui checks test mobium test's own sessions, which it gives
#    each project only when none is set; the rest run no device check.
exempt=" clean-stop.sh test-runner.sh test-grid.sh test-ui.sh lib.sh lib-selftest.sh calculator.sh clock-timer.sh ios-webview-probe.sh "
for f in "$ROOT"/docs/checks/*.sh; do
  n=$(basename "$f")
  case "$exempt" in *" $n "*) continue ;; esac
  sh -n "$f" || bad "$n does not parse"
  grep -q '^\. "\$ROOT/docs/checks/lib.sh"' "$f" || bad "$n does not source lib.sh"
  if grep -qE '^[^#]*trap [^-].* EXIT' "$f"; then bad "$n sets its own EXIT trap — use at_exit"; fi
done
pass "every check is on the prelude"
echo "lib.sh: all ok"
