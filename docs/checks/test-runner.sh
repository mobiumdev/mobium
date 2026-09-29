#!/bin/sh
# `mobium test`, held to the controls decision 0006 promised: a runner is
# only believed once each of its verdicts has been seen to come out the other
# way.
#
#   docs/checks/test-runner.sh                    # Android emulator-5554 only
#   MOBIUM_IOS_DEVICE=<simulator-udid> docs/checks/test-runner.sh   # and iOS
#
# - The suite in tests/mobiumapp passes on every project, the projects run at
#   once, and the run is shorter than its projects one after another.
# - Every test in tests/controls/must-fail.test.json fails, the run exits 1,
#   each failure names its step and code, a screenshot of it is kept, and the
#   JUnit file — read by Python's XML parser, a consumer that is not ours —
#   counts every one.
# - --last-failed then runs exactly those tests again.
# - The flaky control, from cleared app data, is reported flaky with a retry
#   and failed without one.
# - A project whose device is unset is refused before any test runs.
#
# Needs MobiumApp installed. Emulators and simulators: the flaky control
# clears the app's data, which a phone cannot.
set -e
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium"
APP=dev.mobium.mobiumapp
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-14s %-58s ok\n' "$1" "$2"; }
cd "$ROOT/tests"
OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT

PROJECTS="android"
if [ -n "$MOBIUM_IOS_DEVICE" ]; then PROJECTS="android,ios"; fi
P="--project $PROJECTS"
echo "--- projects: $PROJECTS"

# --- a project that cannot start is refused before anything runs -----------
set +e; r=$(env -u MOBIUM_IOS_DEVICE "$M" test --project ios --output "$OUT/refused" 2>&1); st=$?; set -e
[ $st -eq 2 ] || fail "an unset device was not refused as invalid_argument ($st): $r"
echo "$r" | grep -q 'MOBIUM_IOS_DEVICE' || fail "the refusal does not name the variable: $r"
[ ! -e "$OUT/refused/.last-run.json" ] || fail "a refused run left a last run behind, so something ran"
row "refused" "an unset device, named, before any test"

# --- the suite passes, the projects at once ---------------------------------
t0=$(date +%s)
r=$("$M" test $P --reporter json --output "$OUT/suite" 2>&1) || fail "the suite failed: $r"
t1=$(date +%s)
python3 - "$OUT/suite/results.json" "$((t1 - t0))" "$PROJECTS" <<'EOF' || exit 1
import json, sys
d = json.load(open(sys.argv[1]))
wall, projects = int(sys.argv[2]), sys.argv[3].split(",")
rs = d["results"]
bad = [r["title"] for r in rs if r["status"] != "passed"]
if bad:
    sys.exit("FAIL: not passed: %s" % bad)
seen = sorted({r["project"] for r in rs})
if seen != sorted(projects):
    sys.exit("FAIL: ran on %s, want %s" % (seen, projects))
if len(projects) > 1:
    per = {p: sum(r["duration_ns"] for r in rs if r["project"] == p) / 1e9 for p in projects}
    if wall >= sum(per.values()) * 0.85:
        sys.exit("FAIL: %ds for projects that took %s one after another — not at once" % (wall, per))
    print("    %-14s %-58s ok" % ("workers", "%d tests, %ds for %ds of work" % (len(rs), wall, sum(per.values()))))
else:
    print("    %-14s %-58s ok" % ("suite", "%d tests passed" % len(rs)))
EOF

# --- every must-fail test fails, and is reported ---------------------------
set +e; r=$("$M" test controls/must-fail.test.json $P --reporter json,junit,html --output "$OUT/controls" 2>&1); st=$?; set -e
[ $st -eq 1 ] || fail "the must-fail run exited $st, not 1: $r"
python3 - "$OUT/controls" "$PROJECTS" <<'EOF' || exit 1
import json, os, sys
import xml.etree.ElementTree as ET
out, projects = sys.argv[1], sys.argv[2].split(",")
rs = json.load(open(os.path.join(out, "results.json")))["results"]
want = 3 * len(projects)
if len(rs) != want or any(r["status"] != "failed" for r in rs):
    sys.exit("FAIL: must-fail gave %s" % [(r["title"], r["status"]) for r in rs])
for r in rs:
    f = r["failure"]
    if not f.get("code") or f.get("step", 0) < 1:
        sys.exit("FAIL: %s's failure does not name its step and code: %s" % (r["title"], f))
    if not f.get("screenshot") or not os.path.getsize(os.path.join(out, f["screenshot"])):
        sys.exit("FAIL: %s kept no screenshot" % r["title"])
codes = sorted({r["failure"]["code"] for r in rs})
root = ET.parse(os.path.join(out, "junit.xml")).getroot()
if int(root.get("tests")) != want or int(root.get("failures")) != want:
    sys.exit("FAIL: junit.xml counts %s tests and %s failures, want %d" % (root.get("tests"), root.get("failures"), want))
print("    %-14s %-58s ok" % ("must fail", "%d of %d failed, codes %s; JUnit agrees" % (want, want, ", ".join(codes))))
EOF

# --- --last-failed runs exactly those ---------------------------------------
set +e; r=$("$M" test controls/must-fail.test.json $P --last-failed --reporter json --output "$OUT/controls" 2>&1); set -e
n=$(python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))["results"]))' "$OUT/controls/results.json")
[ "$n" -eq $((3 * $(echo "$PROJECTS" | tr ',' '\n' | wc -l))) ] || fail "--last-failed ran $n tests"
row "last failed" "ran the $n that failed, and no others"

# --- flaky: failed without a retry, flaky with one -------------------------
for p in $(echo "$PROJECTS" | tr ',' ' '); do
  dev=emulator-5554; drv=""
  [ "$p" = ios ] && dev="$MOBIUM_IOS_DEVICE" && drv="--driver wda"
  # shellcheck disable=SC2086
  "$M" --device "$dev" $drv clear-data $APP >/dev/null
done
set +e; "$M" test controls/flaky.test.json $P --output "$OUT/flaky" >/dev/null 2>&1; st=$?; set -e
[ $st -eq 1 ] || fail "the flaky control passed with no retry — it cannot tell a retry from a pass"
for p in $(echo "$PROJECTS" | tr ',' ' '); do
  dev=emulator-5554; drv=""
  [ "$p" = ios ] && dev="$MOBIUM_IOS_DEVICE" && drv="--driver wda"
  # shellcheck disable=SC2086
  "$M" --device "$dev" $drv clear-data $APP >/dev/null
done
r=$("$M" test controls/flaky.test.json $P --retries 1 --reporter list,json --output "$OUT/flaky" 2>&1) ||
  fail "the flaky control failed with a retry: $r"
python3 -c '
import json, sys
rs = json.load(open(sys.argv[1]))["results"]
if not rs or any(r["status"] != "flaky" or r["attempts"] != 2 for r in rs):
    sys.exit("FAIL: flaky gave %s" % [(r["project"], r["status"], r["attempts"]) for r in rs])
' "$OUT/flaky/results.json" || exit 1
echo "$r" | grep -q 'flaky' || fail "the list report does not say flaky: $r"
row "flaky" "failed with no retry; flaky, on attempt 2, with one"

echo PASS
