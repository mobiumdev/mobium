#!/bin/sh
# `mobium test` through a grid: each project leases a device of its own, the
# projects run at once on different devices, every result names the device,
# and every lease is released — and a project the grid cannot serve refuses
# the run, releasing the leases the others took.
#
#   MOBIUM_GRID=<node> docs/checks/test-grid.sh
#
# Needs a node with at least two Android emulators and MobiumApp on them,
# reachable as docs/guides/grid.md says (MOBIUM_SSH, MOBIUM_REMOTE_BIN), and
# no other run holding them. Verified with a Mac standing in for its own node.
set -e
[ -n "$MOBIUM_GRID" ] || { echo "usage: MOBIUM_GRID=<node> $0" >&2; exit 2; }
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-60s ok\n' "$1" "$2"; }
DIR=$(mktemp -d)
trap 'rm -rf "$DIR"' EXIT
mkdir -p "$DIR/tests"
cp "$ROOT/tests/mobiumapp/form.test.json" "$DIR/tests/"
cd "$DIR"
held() { "$M" grid status 2>/dev/null | grep -c ' g[0-9a-f]\{8\} for ' || true; }
echo "--- grid: $MOBIUM_GRID"

[ "$(held)" -eq 0 ] || fail "a device on the grid is held before the check starts — nothing to compare against"

# --- two projects, two devices, at once --------------------------------------
cat > two.json <<'EOF'
{"testDir": "tests", "projects": [
  {"name": "a", "platform": "android"},
  {"name": "b", "platform": "android"}]}
EOF
( sleep 8; held > held-mid ) &
r=$("$M" test --config two.json --reporter list,json --output out 2>&1) || fail "the run through the grid failed: $r"
wait
[ "$(cat held-mid)" -eq 2 ] || fail "mid-run the grid showed $(cat held-mid) leases, not 2"
python3 - out/results.json <<'EOF' || exit 1
import json, sys
rs = json.load(open(sys.argv[1]))["results"]
dev = {r["project"]: {x["device"] for x in rs if x["project"] == r["project"]} for r in rs}
if any(r["status"] != "passed" for r in rs):
    sys.exit("FAIL: %s" % [(r["title"], r["status"]) for r in rs])
if any(len(d) != 1 or "" in d for d in dev.values()):
    sys.exit("FAIL: a project's results do not name one device: %s" % dev)
if dev["a"] == dev["b"]:
    sys.exit("FAIL: both projects ran on %s" % dev["a"])
print("    %-12s %-60s ok" % ("leased", "a on %s, b on %s, %d tests passed" % (min(dev["a"]), min(dev["b"]), len(rs))))
EOF
[ "$(held)" -eq 0 ] || fail "a lease was left behind after the run"
row "released" "two leases held mid-run, none after"

# --- a project the grid cannot serve ----------------------------------------
cat > three.json <<'EOF'
{"testDir": "tests", "projects": [
  {"name": "a", "platform": "android"},
  {"name": "b", "platform": "android"},
  {"name": "c", "platform": "android"}]}
EOF
set +e; r=$(MOBIUM_GRID_WAIT=5s "$M" test --config three.json --output out3 2>&1); st=$?; set -e
[ $st -eq 3 ] || fail "three projects on two devices exited $st, not 3 (no_device): $r"
echo "$r" | grep -q 'cannot start' || fail "the refusal does not say a project cannot start: $r"
[ ! -e out3/.last-run.json ] || fail "a refused run ran tests"
[ "$(held)" -eq 0 ] || fail "the refused run left a lease behind"
row "refused" "no device for the third project: exit 3, nothing held after"

echo PASS
