#!/bin/sh
# `mobium test --ui`, driven as a person drives it: in a browser, by its
# buttons. The server's own tests cover what it answers; this covers the page
# — which tests its buttons ask for, the steps it draws as they come, the
# screens it loads under its content security policy, and what it means by
# "re-run failed" — none of which runs without a browser (docs/decisions/0009).
#
#   docs/checks/test-ui.sh                                # Android emulator-5554
#   MOBIUM_IOS_DEVICE=<simulator-udid> docs/checks/test-ui.sh   # and iOS
#
# - The page lists every test of the suite, on every project.
# - ▶ on one test runs it on each project; its steps appear while it runs,
#   and each step's screen loads.
# - With the page still open, the test file is changed on disk to expect a
#   message the app does not show: the next run is the changed test, and it
#   fails on every project, quoting both messages.
# - The file put back, "Re-run failed" runs exactly those, and they pass.
#
# Needs Vibium (`vibium`, headless) and MobiumApp installed. The test file is
# restored on exit whatever happens.
set -e
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-14s %-58s ok\n' "$1" "$2"; }
command -v vibium >/dev/null 2>&1 || { echo "    NOT CHECKED  vibium is not installed"; exit 0; }
OUT=$(mktemp -d)
FILE="$ROOT/tests/mobiumapp/login.test.json"
cp "$FILE" "$OUT/login.backup.json"
V="vibium --headless --session mobium-test-ui-check"
cleanup() {
  cp "$OUT/login.backup.json" "$FILE"
  $V quit >/dev/null 2>&1 || true
  [ -n "$PID" ] && kill "$PID" 2>/dev/null || true
  rm -rf "$OUT"
}
trap cleanup EXIT

PROJECTS=1
[ -n "$MOBIUM_IOS_DEVICE" ] && PROJECTS=2
(cd "$ROOT/tests" && exec "$ROOT/bin/mobium" test --ui --output "$OUT/report" > "$OUT/ui.log" 2>&1) &
PID=$!
for _ in $(seq 1 50); do
  URL=$(grep -o 'http://[^ ]*' "$OUT/ui.log" 2>/dev/null | head -1)
  [ -n "$URL" ] && break
  sleep 0.2
done
[ -n "$URL" ] || fail "mobium test --ui printed no URL: $(cat "$OUT/ui.log")"
echo "--- $(echo "$URL" | sed 's#/[0-9a-f]*/$#/<token>/#')"

$V go "$URL" >/dev/null
sleep 2
summary() { $V eval 'document.getElementById("summary").textContent'; }
finished() {
  sleep 4
  for _ in $(seq 1 100); do
    s=$(summary)
    case "$s" in running*) sleep 3 ;; *) echo "$s"; return ;; esac
  done
  fail "the run did not end"
}
tests=$($V eval 'document.querySelectorAll(".test").length')
[ "$tests" -ge 5 ] || fail "the page lists $tests tests"
row "list" "$tests tests, on $PROJECTS project(s)"

T="a short password"
click_run() {
  $V eval "(() => { const r = [...document.querySelectorAll('.test')].find(x => x.textContent.includes('$T'));
    r.querySelector('button.run').click(); return 'ok'; })()" >/dev/null
}
click_run
# A step on the page while the run is still going: the live view, not the
# report. The first step waits for each project's driver to start.
live=0
for _ in $(seq 1 90); do
  case "$(summary)" in running*) ;; *) break ;; esac
  live=$($V eval 'document.querySelectorAll("#detail .frame").length')
  [ "$live" -ge 1 ] && break
  sleep 1
done
[ "$live" -ge 1 ] || fail "no step appeared while the test ran"
s=$(finished)
echo "$s" | grep -q "^$PROJECTS passed, 0 failed" || fail "running \"$T\": $s"
loaded=$($V eval '[...document.querySelectorAll("#detail .frame img")].filter(i => i.naturalWidth > 0).length')
frames=$($V eval 'document.querySelectorAll("#detail .frame").length')
[ "$frames" -ge 5 ] && [ "$loaded" = "$frames" ] || fail "$loaded of $frames step screens loaded"
row "run one" "steps shown as they came; $frames screens, every one loaded"

sed 's/Password must be at least 6 characters\./Password must be at least 9 characters./' "$OUT/login.backup.json" > "$FILE"
click_run
s=$(finished)
echo "$s" | grep -q "^0 passed, $PROJECTS failed" || fail "the edited test did not fail: $s"
$V eval 'document.getElementById("detail").textContent' | grep -q 'at least 9 characters.*at least 6 characters' ||
  fail "the failure does not quote both messages"
row "edited" "the changed file ran, without a restart, and failed as written"

cp "$OUT/login.backup.json" "$FILE"
$V eval '(() => { document.getElementById("runFailed").click(); return "ok"; })()' >/dev/null
s=$(finished)
echo "$s" | grep -q "^$PROJECTS passed, 0 failed" || fail "re-run failed: $s"
[ -f "$OUT/report/index.html" ] && [ -f "$OUT/report/results.json" ] || fail "the run wrote no reports"
row "re-run" "re-run failed ran those $PROJECTS, and they passed; reports written"
echo PASS
