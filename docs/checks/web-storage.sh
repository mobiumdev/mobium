#!/bin/sh
# app_cookies and app_storage, checked against the page's own view.
#
#   docs/checks/web-storage.sh <emulator-serial | simulator-udid>
#
# Both tools answer from the page's side of the wire — document.cookie,
# localStorage — so their own report of success is not evidence. MobiumApp's
# Web storage page reports what it holds itself, read again every second, and
# every step here asks the page, not the tool:
#
# - it starts empty after a clear, which is the control the rest needs: a page
#   that always read empty could not show a restore;
# - what the page writes (Save a visit: a cookie, a localStorage and a
#   sessionStorage entry) is what `cookies` and `storage -o` report;
# - `storage clear` empties what the page sees, and `storage restore` puts
#   all three back, from the saved file;
# - a cookie set by name reaches the page, and clearing it by name removes
#   that one and leaves the other.
#
# The page is inline HTML loaded with the base URL https://mobiumapp.test/:
# every other page in MobiumApp has no origin, so both tools refuse there.
# .test never resolves, so nothing leaves the device. Emulators and
# simulators; it leaves the page's storage cleared.
set -e
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
case "$DEV" in
  *-*-*-*-*) PLATFORM=ios; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *)         PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
esac
APP=dev.mobium.mobiumapp
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-14s %-58s ok\n' "$1" "$2"; }
OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT
appContexts() { $M contexts | awk -v id="WEBVIEW_$1" \
  '$1 == id || (index($1, id "_") == 1 && substr($1, length(id) + 2) ~ /^[0-9]+$/) { print $1 }'; }

"$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true
echo "--- $DEV ($PLATFORM)"
$M apps 2>/dev/null | grep -q "$APP" || fail "$APP is not installed"

$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
$M tap testid=webviewhubBtn >/dev/null
$M tap testid=webstorageBtn >/dev/null || fail "no Web storage page: this MobiumApp predates it"
$M wait text=Back >/dev/null
CTX=""
for _ in 1 2 3 4 5; do CTX=$(appContexts "$APP" | head -1); [ -n "$CTX" ] && break; sleep 1; done
[ -n "$CTX" ] || fail "no WebView context for the Web storage page"
$M context "$CTX" >/dev/null

# What the page sees, from its own script: {origin, cookies, local, session}.
state() { $M eval 'JSON.stringify(storageState())' | tail -1; }
has() { state | python3 -c '
import json, sys
st = json.loads(sys.stdin.read())
want = sys.argv[1:]
seen = st["cookies"] + st["local"] + st["session"]
missing = [w for w in want if not w.startswith("!") and w not in seen]
present = [w[1:] for w in want if w.startswith("!") and w[1:] in seen]
if missing or present:
    sys.exit("the page holds %s; missing %s, and should not hold %s" % (seen, missing, present))
' "$@"; }
empty() { state | python3 -c '
import json, sys
st = json.loads(sys.stdin.read())
if st["cookies"] or st["local"] or st["session"]:
    sys.exit("the page still holds %s" % (st["cookies"] + st["local"] + st["session"]))
'; }

# --- the control: an origin, and nothing held -------------------------------
[ "$(state | python3 -c 'import json,sys; print(json.load(sys.stdin)["origin"])')" = "https://mobiumapp.test" ] \
  || fail "the page's origin is not https://mobiumapp.test: $(state)"
$M storage clear >/dev/null
r=$(empty 2>&1) || fail "after a clear, $r"
row "clean" "origin https://mobiumapp.test, and nothing held"

# --- what the page writes, the tools read ----------------------------------
# Inside a page an action takes a ref from its map, not a locator.
SAVE=$($M map --json | python3 -c '
import json, sys
print(next((e["ref"] for e in json.load(sys.stdin)["elements"] if e.get("label") == "Save a visit"), ""))')
[ -n "$SAVE" ] || fail "the page's map has no Save a visit: $($M map)"
$M tap "$SAVE" >/dev/null
sleep 1
r=$(has visited=yes visits=1 'lastVisit=visit 1' 2>&1) || fail "Save a visit: $r"
$M cookies | grep -q 'visited' || fail "cookies does not list the page's cookie: $($M cookies)"
$M storage -o "$OUT/state.json" >/dev/null
python3 - "$OUT/state.json" <<'EOF' || exit 1
import json, sys
st = json.load(open(sys.argv[1]))
cookies = {c["name"]: c["value"] for c in st.get("cookies", [])}
origins = {o["origin"]: o for o in st.get("origins", [])}
o = origins.get("https://mobiumapp.test")
if cookies.get("visited") != "yes" or not o:
    sys.exit("FAIL: the saved state is %s" % st)
local = {i["name"]: i["value"] for i in o.get("localStorage", [])}
session = {i["name"]: i["value"] for i in o.get("sessionStorage", [])}
if local.get("visits") != "1" or session.get("lastVisit") != "visit 1":
    sys.exit("FAIL: the saved origin holds %s and %s" % (local, session))
EOF
row "read" "the page's cookie, localStorage and sessionStorage saved"

# --- clear empties what the page sees, restore puts it back ----------------
$M storage clear >/dev/null
r=$(empty 2>&1) || fail "storage clear: $r"
$M storage restore "$OUT/state.json" >/dev/null
r=$(has visited=yes visits=1 'lastVisit=visit 1' 2>&1) || fail "storage restore: $r"
row "restore" "cleared to nothing, then all three back, as the page sees it"

# --- a cookie by name ---------------------------------------------------------
$M cookies session abc123 >/dev/null
r=$(has session=abc123 visited=yes 2>&1) || fail "a cookie set by name: $r"
$M cookies clear session >/dev/null
r=$(has '!session=abc123' visited=yes 2>&1) || fail "clearing a cookie by name: $r"
row "cookies" "one set by name reached the page; cleared by name, only it went"

$M storage clear >/dev/null
$M context NATIVE_APP >/dev/null
echo PASS
