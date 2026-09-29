#!/bin/sh
# `mobium trace`, held to what the trace viewers read. A sign-in on
# MobiumApp's Login Demo is traced — a password typed, a wait, and a tap on
# something that is not there — and the zip is checked:
#
# - context-options first, titled by --name;
# - a before and an after for every call, sharing its id, and the failed
#   tap's after carrying its error;
# - after every call a screencast frame whose image is in resources/, and a
#   frame snapshot the after event names, with the map drawn over it;
# - the tap's point, and the password nowhere in the zip, only its length;
# - a second start refused while one runs, and a stop with none refused.
#
# With MOBIUM_TRACE_VIEWER=1 and Vibium installed, the zip is also opened in
# Playwright's own viewer at trace.playwright.dev — a consumer that is not
# ours — and every step must be listed there. That needs the network; the
# viewer reads the file in the browser and sends it nowhere.
#
#   docs/checks/trace.sh <emulator-serial | simulator-udid>
#
# Emulators and simulators.
set -e
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEV="${1:?usage: trace.sh <serial|udid>}"
case "$DEV" in
  *-*-*-*-*) M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *) M="$ROOT/bin/mobium --device $DEV" ;;
esac
APP=dev.mobium.mobiumapp
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-14s %-58s ok\n' "$1" "$2"; }
OUT=$(mktemp -d)
trap 'rm -rf "$OUT"; $M trace stop -o "$OUT/left.zip" >/dev/null 2>&1 || true' EXIT
SECRET="Tr4ce-$(od -An -N3 -tx1 /dev/urandom | tr -d ' ')"

"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true
echo "--- $DEV"
set +e
$M trace stop -o "$OUT/none.zip" >/dev/null 2>&1; st=$?
set -e
[ $st -eq 2 ] || fail "a stop with no trace running was not refused (exit $st)"

$M trace start --name "trace.sh sign in" >/dev/null
set +e
$M trace start >/dev/null 2>&1; st=$?
set -e
[ $st -eq 2 ] || fail "a second start was not refused (exit $st)"
$M trace | grep -q '^tracing for' || fail "status did not say a trace is running"
row "controls" "stop with none, and a second start, refused; status"

$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
$M tap "label=Login Demo" >/dev/null
$M fill testid=username mobium >/dev/null
$M fill testid=password "$SECRET" >/dev/null
$M tap "label=Log In" >/dev/null 2>&1 || $M tap testid=loginBtn >/dev/null
$M wait testid=errorText --timeout 10s >/dev/null 2>&1 || true
$M tap testid=noSuchButton >/dev/null 2>&1 || true
$M trace stop -o "$OUT/t.zip" >/dev/null
[ -s "$OUT/t.zip" ] || fail "no zip was saved"
row "record" "a sign-in traced, a wrong password typed, a tap that fails"

mkdir "$OUT/z" && (cd "$OUT/z" && unzip -q ../t.zip)
grep -r -a -q -F "$SECRET" "$OUT/z" && fail "the typed password is in the trace"
python3 - "$OUT/z" "${#SECRET}" <<'EOF' || fail "the trace is not what the viewers read"
import json, os, sys
root, n = sys.argv[1], int(sys.argv[2])
ev = [json.loads(l) for l in open(os.path.join(root, "trace.trace"))]
assert ev[0]["type"] == "context-options" and ev[0]["title"] == "trace.sh sign in", ev[0]
befores = {e["callId"]: e for e in ev if e["type"] == "before"}
afters = {e["callId"]: e for e in ev if e["type"] == "after"}
assert befores and set(befores) == set(afters), "before and after do not pair"
snaps = {e["snapshot"]["snapshotName"]: e for e in ev if e["type"] == "frame-snapshot"}
frames = [e for e in ev if e["type"] == "screencast-frame"]
for f in frames:
    assert os.path.exists(os.path.join(root, "resources", f["sha1"])), "a frame's image is missing"
for cid, a in afters.items():
    assert a.get("afterSnapshot") in snaps, cid + " has no snapshot"
titled = [s for s in snaps.values() if '"title": "@e' in json.dumps(s)]
assert titled, "no snapshot has the map drawn over it"
typed = [b for b in befores.values() if b["method"] == "mobium:app_fill" and b["params"].get("target") == "testid=password"]
assert typed and typed[0]["params"]["text"] == "(%d characters, not recorded)" % n, typed
failed = [b for b in befores.values() if b["params"].get("target") == "testid=noSuchButton"]
assert failed and "error" in afters[failed[0]["callId"]], "the failed tap has no error"
assert any(e["type"] == "input" for e in ev), "no tap's point was recorded"
print("   ", len(befores), "calls,", len(frames), "frames")
EOF
row "format" "paired calls, frames, snapshots with the map, the error"
row "password" "not in the zip, only its length"

if [ "${MOBIUM_TRACE_VIEWER:-}" = 1 ] && command -v vibium >/dev/null 2>&1; then
  V="vibium --headless --session mobium-trace-check"
  $V go https://trace.playwright.dev >/dev/null
  # The page renders after the load: wait for its Select file button.
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    [ "$($V eval '[...document.querySelectorAll("button")].some(b => b.textContent.includes("Select file"))' 2>/dev/null)" = true ] && break
    sleep 1
  done
  # The page makes its file input only when Select file is clicked.
  $V eval 'HTMLInputElement.prototype.click = function(){ this.id = "pwfile"; document.body.appendChild(this); };
    [...document.querySelectorAll("button")].find(b => b.textContent.includes("Select file")).click(); "ok"' >/dev/null
  $V upload '#pwfile' "$OUT/t.zip" >/dev/null
  sleep 5
  page=$($V eval 'document.body.innerText')
  for step in "launch $APP" "tap label=Login Demo" "fill testid=password" "tap testid=noSuchButton"; do
    echo "$page" | grep -q -F "$step" || fail "Playwright's viewer does not list \"$step\""
  done
  $V quit >/dev/null 2>&1 || true
  row "viewer" "trace.playwright.dev lists every step"
fi
echo PASS
