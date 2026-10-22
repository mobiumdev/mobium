#!/bin/sh
# A proxy between the app and its servers: `mobium network --proxy`, judged
# by what the app received, never by what the tool reports. docs/PROXY.md,
# step 2.
#
#   docs/checks/proxy.sh <android-serial>
#
# The proxy is the check's own, on the Mac: it answers every request itself
# and logs its request line. MobiumApp's Network Demo fetches
# http://127.0.0.1:9/proxy-check, a port nothing on the device listens on,
# so the body can only come from the proxy (and loopback is what the app's
# network security configuration lets it fetch in clear):
#
# - with no proxy the fetch fails, so the device has no other way there;
# - --proxy 127.0.0.1:<port> is the Mac's through adb reverse, read back as
#   such, and the fetch returns the proxy's body while the proxy logs it;
# - --no-proxy takes it off: every proxy key unset again, the reverse gone,
#   and the fetch fails again;
# - set again and the session ended, the device is put back as it was found;
# - a proxy given with a scheme is refused before anything is changed.
#
# Needs MobiumApp with its Network Demo (mobium-app#31) and python3 on the
# Mac. An emulator; a phone only with ALLOW_PHONE=1, since its proxy is the
# whole phone's while the check runs — put back, and read back, after.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <android-serial>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_platform android "$DEV"
case "$DEV" in
  emulator-*) ;;
  *) [ -n "${ALLOW_PHONE:-}" ] || { echo "a phone's proxy is the whole phone's: run with ALLOW_PHONE=1" >&2; exit 2; } ;;
esac
check_lock "$DEV"
M="$ROOT/bin/mobium --device $DEV"
A="adb -s $DEV"
APP=dev.mobium.mobiumapp
PORT=18899
URL=http://127.0.0.1:9/proxy-check
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
echo "--- $DEV"

KEYS="http_proxy global_http_proxy_host global_http_proxy_port global_http_proxy_exclusion_list global_proxy_pac_url"
# settings: every proxy key, as the device has it.
settings() { for k in $KEYS; do printf '%s=%s ' "$k" "$($A shell settings get global "$k" | tr -d '\r')"; done; }
UNSET="$(for k in $KEYS; do printf '%s=null ' "$k"; done)"
reversed() { $A reverse --list | grep -q " tcp:$PORT tcp:$PORT" && echo yes || echo no; }

before=$(settings)
[ "$before" = "$UNSET" ] || fail "the device has a proxy set already, which this check would not put back as it asserts: $before"
[ "$(reversed)" = no ] || fail "tcp:$PORT is reversed already; free it first"

TMP=$(mktemp -d)
PID=
cleanup() {
  [ -n "$PID" ] && { kill "$PID" 2>/dev/null || true; wait "$PID" 2>/dev/null || true; }
  # Whatever the check left, taken off the way Mobium takes it off.
  if [ "$(settings)" != "$UNSET" ]; then
    $A shell settings put global http_proxy :0 >/dev/null 2>&1 || true
    for k in $KEYS; do $A shell settings delete global "$k" >/dev/null 2>&1 || true; done
  fi
  $A reverse --remove "tcp:$PORT" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
at_exit cleanup

cat > "$TMP/proxy.py" <<'EOF'
import http.server, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        sys.stdout.write(f"{self.command} {self.path}\n"); sys.stdout.flush()
        body = b"answered by the check's proxy"
        self.send_response(200); self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    def log_message(self, *a): pass
http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
EOF
python3 -I "$TMP/proxy.py" "$PORT" > "$TMP/proxy.log" 2>&1 & PID=$!

apps=$($M apps 2>&1) || fail "cannot list the apps: $(echo "$apps" | tail -1)"
echo "$apps" | grep -q "$APP" || fail "$APP is not installed — build and install MobiumApp first"
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M scroll-to 'label="Network Demo"' >/dev/null || fail "MobiumApp has no Network Demo — it needs mobium-app#31"
$M tap 'label="Network Demo"' >/dev/null
$M wait testid=netFetch >/dev/null || fail "the Network Demo did not open"
$M fill testid=netUrl "$URL" >/dev/null

# fetch: tap Fetch and wait for a result line that says want.
fetch() {
  $M tap testid=netFetch >/dev/null
  $M wait testid=netFetchResult --for text --text "$1" --timeout 20s >/dev/null ||
    fail "the fetch did not say \"$1\": $($M text testid=netFetchResult 2>&1)"
}

r=$($M network) || fail "reading the network: $r"
echo "$r" | grep -q proxy && fail "a proxy was reported where the device has none: $r"
fetch "failed"
row "direct" "with no proxy, $URL cannot be reached"

r=$($M network --proxy "127.0.0.1:$PORT") || fail "setting the proxy: $r"
echo "$r" | grep -q "proxy 127.0.0.1:$PORT, which is this machine's through adb reverse" ||
  fail "the proxy was reported as: $r"
[ "$(reversed)" = yes ] || fail "the proxy was set and tcp:$PORT is not reversed"
fetch "answered by the check's proxy"
grep -q "^GET $URL$" "$TMP/proxy.log" || fail "the app showed the body and the proxy logged no request: $(cat "$TMP/proxy.log")"
row "proxy" "the fetch went through the Mac's proxy, which logged it"

r=$($M network --no-proxy) || fail "taking the proxy off: $r"
echo "$r" | grep -q proxy && fail "the proxy was taken off and reported as: $r"
[ "$(settings)" = "$UNSET" ] || fail "the proxy was taken off and left: $(settings)"
[ "$(reversed)" = no ] || fail "the proxy was taken off and tcp:$PORT is still reversed"
fetch "failed"
row "no-proxy" "every proxy key unset, the reverse gone, the fetch fails"

$M network --proxy "127.0.0.1:$PORT" >/dev/null || fail "setting the proxy again"
$M daemon stop >/dev/null
[ "$(settings)" = "$UNSET" ] || fail "the session ended and the proxy was left: $(settings)"
[ "$(reversed)" = no ] || fail "the session ended and tcp:$PORT is still reversed"
row "end" "the session's end put the device back as it was found"

set +e; r=$($M network --proxy "http://127.0.0.1:$PORT" 2>&1); st=$?; set -e
[ $st -ne 0 ] || fail "a proxy with a scheme was accepted: $r"
echo "$r" | grep -q "no scheme" || fail "the refusal does not say why: $r"
[ "$(settings)" = "$UNSET" ] || fail "a refused proxy changed the device: $(settings)"
row "refused" "a proxy with a scheme, before anything changed"
echo PASS
