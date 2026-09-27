#!/bin/sh
# The raw source, end to end, on MobiumApp: the native hierarchy as the
# device-side server sent it — well-formed, in the platform's units — with a
# typed password nowhere in it; then a WebView's markup, with a password
# value planted in the page's own markup and hidden in the answer while the
# page itself keeps it.
#
#   docs/checks/source.sh <android-serial | simulator-udid | iphone-udid>
#
# Needs MobiumApp installed (mobiumdev/mobium-app). Its password field reports
# bullets on both platforms, so the native half cannot show the redaction
# working — only that nothing leaks. That half's positive control is
# internal/uitree's test on a captured Aegis screen, where Android put the
# typed value in the field's text.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
APP=dev.mobium.mobiumapp
SECRET='Hunter2x9'
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

case "$DEV" in
  ????????-????????????????) PLATFORM=iphone; UNITS=pt; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *-*-*-*-*) PLATFORM=ios; UNITS=pt; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *)         PLATFORM=android; UNITS=px; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($PLATFORM)"

fresh() { $M terminate "$APP" >/dev/null 2>&1 || true; $M launch "$APP" >/dev/null; }

fresh
$M tap testid=loginBtn >/dev/null
$M type testid=password "$SECRET" >/dev/null
$M source --json > "$TMP/native.json"
python3 - "$TMP/native.json" "$UNITS" "$SECRET" <<'EOF' || exit 1
import json, sys, xml.dom.minidom
d = json.load(open(sys.argv[1]))
src, units, secret = d["source"], sys.argv[2], sys.argv[3]
def fail(m): print("FAIL: " + m, file=sys.stderr); sys.exit(1)
if d["format"] != "xml": fail("format is %r" % d["format"])
if d["units"] != units: fail("units are %r, want %r" % (d["units"], units))
if units == "pt" and not d.get("scale", 0) > 1: fail("an iOS source gave no scale")
xml.dom.minidom.parseString(src.encode())  # well-formed, or this raises
if secret in src: fail("the typed password is in the source")
if "password" not in src: fail("the password field is not in the source at all")
print("    native         %d bytes of well-formed XML in %s, no password     ok" % (len(src), units))
EOF

fresh
$M tap "label=WebViews" >/dev/null
$M tap testid=webviewBtn >/dev/null
# A WebView registers with the inspector a moment after its screen appears.
ctx=""
for i in 1 2 3 4 5; do
  ctx=$($M contexts | awk '/^WEBVIEW_/ {print $1; exit}')
  [ -n "$ctx" ] && break
  sleep 1
done
[ -n "$ctx" ] || fail "no WebView context appeared"
$M context "$ctx" >/dev/null
$M eval "document.body.insertAdjacentHTML('beforeend', '<input id=pw type=password value=\"$SECRET\">'); 'ok'" >/dev/null
$M source --json > "$TMP/page.json"
kept=$($M eval "document.getElementById('pw').getAttribute('value')")
$M context NATIVE_APP >/dev/null
python3 - "$TMP/page.json" "$SECRET" "$kept" <<'EOF' || exit 1
import json, sys
d = json.load(open(sys.argv[1]))
src, secret, kept = d["source"], sys.argv[2], sys.argv[3]
def fail(m): print("FAIL: " + m, file=sys.stderr); sys.exit(1)
if d["format"] != "html": fail("format is %r" % d["format"])
if secret in src: fail("the planted password is in the page source")
if 'value="%s"' % ("•" * len(secret)) not in src: fail("the value was not hidden with its length kept")
if d["redacted"] != 1: fail("redacted %d, want 1" % d["redacted"])
if kept != secret: fail("the page itself was changed: the value now reads %r" % kept)
print("    page           %d bytes of markup, the planted password hidden,   ok" % len(src))
print("                   and still in the page itself")
EOF
$M terminate "$APP" >/dev/null 2>&1 || true
