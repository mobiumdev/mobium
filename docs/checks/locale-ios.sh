#!/bin/sh
# Per-app language on iOS, read back from the app: Settings, which ships a
# Japanese translation and is on every iPhone and simulator, is pinned to
# ja-JP and its General row must read 一般 — first relaunched by the pin, then
# on a launch of its own — and cleared, it must read General again.
#
#   docs/checks/locale-ios.sh <simulator-udid | iphone-udid>
#
# iOS stores no per-app language that can be set from outside, so Mobium
# passes it as launch arguments for the rest of the session; what the check
# holds it to is that the app shows it, not that something stored it. Only
# the one row is read, so nothing else in Settings is printed. Leaves
# Settings following the device's language. Android's half is
# device-state.sh.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-54s ok\n' "$1" "$2"; }
M="$ROOT/bin/mobium --device $DEV"
P=com.apple.Preferences
general() { $M map 2>/dev/null | grep -o -E '\b(General|一般)\b' | head -1; }
trap '$M locale $P "" >/dev/null 2>&1 || true' EXIT
echo "--- $DEV"

$M locale $P | grep -q 'follows the device' || fail "Settings started out pinned"
$M terminate $P >/dev/null 2>&1 || true
$M launch $P >/dev/null
[ "$(general)" = General ] || fail "Settings does not read General before the pin: '$(general)'"
row "before" "General, following the device"

$M locale $P ja-JP >/dev/null
[ "$(general)" = 一般 ] || fail "pinned to ja-JP, Settings reads '$(general)'"
$M locale $P | grep -q 'launched in ja-JP' || fail "the pin did not read back: $($M locale $P)"
row "pinned" "一般, relaunched by the pin, and read back as a launch argument"

$M terminate $P >/dev/null
$M launch $P >/dev/null
[ "$(general)" = 一般 ] || fail "a later launch did not carry the pin: '$(general)'"
row "launch" "一般 again on a launch of its own"

$M locale $P "" >/dev/null
$M terminate $P >/dev/null
$M launch $P >/dev/null
[ "$(general)" = General ] || fail "cleared, Settings still reads '$(general)'"
row "cleared" "General, following the device again"

out=$($M locale $P "not a tag" 2>&1) && fail "a malformed tag was accepted: $out"
echo "$out" | grep -q "not a language tag" || fail "the refusal did not say why: $out"
row "refused" "a malformed tag, before anything was launched"
$M terminate $P >/dev/null 2>&1 || true
echo "PASS"
