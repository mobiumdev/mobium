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
# - Iteration 2: the soft control fails with both of its soft failures and
#   still reaches its last step; a retain-on-failure trace keeps a failed
#   test's steps, each with a screenshot, and nothing of a passing one — and
#   the failed test as a Playwright trace zip, holding its calls and none of
#   the runner's own screenshots and maps; and
#   --debug, answered through a pipe, stops before each step and quits.
#   The suite's form.test.json is written in the step shorthand.
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

# --- soft: every soft failure reported, and the test carried on ------------
set +e; r=$("$M" test controls/soft.test.json $P --reporter json --output "$OUT/soft" 2>&1); st=$?; set -e
[ $st -eq 1 ] || fail "the soft control exited $st, not 1: $r"
python3 - "$OUT/soft" <<'EOF' || exit 1
import json, os, sys
out = sys.argv[1]
for r in json.load(open(os.path.join(out, "results.json")))["results"]:
    fs = r.get("failures") or []
    if r["status"] != "failed" or [f["step"] for f in fs] != [2, 4] or not all(f.get("soft") for f in fs):
        sys.exit("FAIL: %s: %s, soft failures at %s — want failed, at steps 2 and 4" %
                 (r["project"], r["status"], [f["step"] for f in fs]))
    if len({f.get("screenshot") for f in fs}) != 2:
        sys.exit("FAIL: %s: the two soft failures do not each keep a screenshot" % r["project"])
print("    %-14s %-58s ok" % ("soft", "both soft failures reported, and the last step ran"))
EOF

# --- trace: a failed test's steps kept, a passing test's not ----------------
set +e
"$M" test mobiumapp/form.test.json controls/must-fail.test.json -g 'checkbox|screen assertion' $P \
  --trace retain-on-failure --reporter json,html --output "$OUT/trace" >/dev/null 2>&1
set -e
python3 - "$OUT/trace" <<'EOF' || exit 1
import json, os, sys
out = sys.argv[1]
for r in json.load(open(os.path.join(out, "results.json")))["results"]:
    tr = r.get("trace") or []
    if r["status"] == "passed" and tr:
        sys.exit("FAIL: retain-on-failure kept a trace of %s, which passed" % r["title"])
    if r["status"] == "failed":
        if len(tr) != 2 or not tr[-1].get("error") or tr[0].get("error"):
            sys.exit("FAIL: %s's trace is %s" % (r["title"], [(t["step"], bool(t.get("error"))) for t in tr]))
        for t in tr:
            if not t.get("map") or not os.path.getsize(os.path.join(out, t["screenshot"])):
                sys.exit("FAIL: step %d of %s's trace has no screen" % (t["step"], r["title"]))
if 'class="film"' not in open(os.path.join(out, "index.html")).read():
    sys.exit("FAIL: the HTML report shows no filmstrip")
print("    %-14s %-58s ok" % ("trace", "a failed test's every step kept; a passing test's, none"))

# The same test as a Playwright trace: a zip trace.playwright.dev opens,
# holding the test's calls and not the runner's own screenshots and maps.
import zipfile
for r in json.load(open(os.path.join(out, "results.json")))["results"]:
    zf = r.get("trace_file")
    if r["status"] == "passed":
        if zf:
            sys.exit("FAIL: retain-on-failure kept a Playwright trace of %s, which passed" % r["title"])
        continue
    if not zf or not os.path.exists(os.path.join(out, zf)):
        sys.exit("FAIL: %s has no Playwright trace (%r)" % (r["title"], zf))
    with zipfile.ZipFile(os.path.join(out, zf)) as z:
        events = [json.loads(l) for l in z.read("trace.trace").decode().splitlines() if l.strip()]
        frames = [n for n in z.namelist() if n.startswith("resources/")]
    methods = [e["method"] for e in events if e.get("type") == "before"]
    if any(m in ("mobium:app_map", "mobium:app_screenshot") for m in methods):
        sys.exit("FAIL: the runner's own screenshots or maps are in %s's trace: %s" % (r["title"], methods))
    if len(methods) < len(r["trace"]) or not frames:
        sys.exit("FAIL: %s's Playwright trace has %d calls and %d frames for %d steps" % (r["title"], len(methods), len(frames), len(r["trace"])))
    calls = len(methods)
if "trace.playwright.dev" not in open(os.path.join(out, "index.html")).read():
    sys.exit("FAIL: the HTML report does not link the Playwright trace")
print("    %-14s %-58s ok" % ("trace zip", "the failed test as a Playwright trace, %d calls, none the runner's" % calls))
EOF

# --- debug: stops before each step, and quits ------------------------------
first=$(echo "$PROJECTS" | cut -d, -f1)
set +e
dbg=$(printf '\n\nq\n' | "$M" test mobiumapp/form.test.json -g checkbox --project "$first" --debug \
  --output "$OUT/debug" 2>&1); st=$?
set -e
[ $st -eq 1 ] || fail "a debugged run that was quit exited $st, not 1"
[ "$(echo "$dbg" | grep -c "^\[$first\] .* — ")" -eq 3 ] || fail "the debugger did not stop exactly three times: $dbg"
echo "$dbg" | grep -q '^@e1 ' || fail "the debugger did not show the screen's map"
# The map before step 1 is the Form Demo that beforeEach opened: a map read
# the moment a tap returns showed the screen being left.
echo "$dbg" | awk '/ — step 1$/{f=1} / — step 2$/{f=0} f' | grep -q 'Accept terms' ||
  fail "the map before step 1 is not the screen beforeEach opened: $dbg"
echo "$dbg" | grep -q '\[stopped\] stopped in the debugger' || fail "a quit test was not reported stopped: $dbg"
row "debug" "stopped at beforeEach, steps 1 and 2; quit, reported stopped"

echo PASS
