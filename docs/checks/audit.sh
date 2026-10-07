#!/bin/sh
# The platform's own accessibility audit, held to two screens whose answer is
# known.
#
#   docs/checks/audit.sh <simulator-udid | iphone-udid | android-serial>
#
# On iOS it is Apple's, XCUITest's performAccessibilityAudit through
# WebDriverAgent:
#
#   - Settings, the positive control: at least one finding, each with a
#     type and Apple's summary; every one that names an element has bounds
#     on the screen, and a locator that resolves. A real iPhone's contrast
#     findings name no element, which is reported, not dropped. On a phone
#     Settings is its owner's, so only counts and types are printed.
#   - MobiumApp's Layout Demo, the negative control: no findings — and on
#     the same screen `screen --inspect` names the 24pt tiny target, since
#     Apple's hit-region rule is its own and far below the 44pt guideline.
#     The two disagree by design, and the check holds them to it.
#
# Android refuses, saying why: its audits run inside the app.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-10s %-58s ok\n' "$1" "$2"; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }
M="$ROOT/bin/mobium --device $DEV"
case "$DEV" in
  ????????-????????????????) KIND=phone ;;
  *-*-*-*-*) KIND=simulator ;;
  *) KIND=android ;;
esac
echo "--- $DEV ($KIND)"

if [ "$KIND" = android ]; then
  out=$($M audit 2>&1) && fail "Android ran an audit: $out"
  echo "$out" | grep -q "outside the app" || fail "the refusal did not say why: $out"
  echo "$out" | grep -q -- "--inspect" || fail "the refusal did not name what Android has instead: $out"
  row "refused" "Android, whose audits run inside the app"
  echo PASS; exit 0
fi

$M terminate com.apple.Preferences >/dev/null 2>&1 || true
$M launch com.apple.Preferences >/dev/null
out=$($M audit --json)
n=$(echo "$out" | json 'len(d["findings"])')
[ "$n" -ge 1 ] || fail "Settings gave no findings — the audit may not be reaching the app"
echo "$out" | json 'all(f["type"] and f["summary"] for f in d["findings"])' | grep -q True ||
  fail "a finding has no type or summary"
echo "$out" | python3 -c '
import json, sys
d = json.load(sys.stdin)
for f in d["findings"]:
    b = f.get("bounds")
    if b and not (0 <= b["x1"] < b["x2"] and 0 <= b["y1"] < b["y2"]):
        sys.exit("bounds not on screen: %r" % b)' || fail "a finding's bounds are not a rectangle on the screen"
types=$(echo "$out" | json '",".join(sorted(set(f["type"] for f in d["findings"])))')
nameless=$(echo "$out" | json 'sum(1 for f in d["findings"] if not f.get("bounds"))')
loc=$(echo "$out" | json 'next((f["locator"] for f in d["findings"] if f.get("locator","").startswith("testid=")), "")')
if [ -n "$loc" ]; then
  $M find "$loc" >/dev/null 2>&1 || fail "a finding's locator does not resolve"
fi
row "settings" "$n finding(s): $types; $nameless with no element; a locator resolves"
$M terminate com.apple.Preferences >/dev/null 2>&1 || true

APP=dev.mobium.mobiumapp
$M terminate $APP >/dev/null 2>&1 || true
$M launch $APP >/dev/null
$M scroll-to "label=Layout Demo" >/dev/null 2>&1 || true
$M tap "label=Layout Demo" >/dev/null
$M wait testid=tinyTarget >/dev/null
n=$($M audit --json | json 'len(d["findings"])')
[ "$n" = 0 ] || fail "the Layout Demo, clean to Apple's audit when measured, gave $n finding(s)"
$M screen --inspect | grep -q 'tiny-target Tiny target' || fail "screen --inspect no longer names the tiny target"
row "layout" "no findings, while screen --inspect names the 24pt target"
$M terminate $APP >/dev/null 2>&1 || true
echo PASS
