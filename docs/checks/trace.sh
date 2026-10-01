#!/bin/sh
# `mobium trace`, held to Vibium's record format and to what its player reads. A sign-in on
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
# Vibium's player at player.vibium.dev — a consumer that is not ours — which
# must count every call and name each as it steps through, a fill included.
# That needs the network; the player reads the file in the browser and sends
# it nowhere.
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
  # Vibium's player takes the zip from its file input, says how many actions
  # it holds, and names each as it steps — a fill by the record format's
  # selector and value, which Mobium records masked, a dot a character.
  $V go https://player.vibium.dev >/dev/null
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    [ "$($V eval 'document.querySelectorAll("input[type=file]").length' 2>/dev/null)" = 1 ] && break
    sleep 1
  done
  $V eval 'document.querySelector("input[type=file]").id = "vfile"; "ok"' >/dev/null
  $V upload '#vfile' "$OUT/t.zip" >/dev/null
  sleep 4
  calls=$(python3 -c "import json,zipfile,sys; z=zipfile.ZipFile(sys.argv[1]); print(sum(1 for l in z.read('trace.trace').decode().splitlines() if l.strip() and json.loads(l).get('type')=='before'))" "$OUT/t.zip")
  $V eval 'document.body.innerText' | grep -q "$calls actions" || fail "player.vibium.dev does not say $calls actions"
  seen=""
  for _ in $(seq 1 "$calls"); do
    $V eval '[...document.querySelectorAll("button")].filter(b => b.textContent.trim() === "▶")[1].click(); "ok"' >/dev/null
    sleep 1
    seen="$seen
$($V eval 'document.body.innerText')"
  done
  for step in "tap label=Login Demo" "into testid=password" "tap testid=noSuchButton"; do
    echo "$seen" | grep -q -F "$step" || fail "player.vibium.dev never showed \"$step\""
  done
  echo "$seen" | grep -q 'Type "" into' && fail "player.vibium.dev shows a fill with no value"
  $V quit >/dev/null 2>&1 || true
  row "player" "player.vibium.dev plays it: $calls actions, each named"
fi
echo PASS
