#!/bin/sh
# What WebDriverAgent says about an iOS slider, read only: its value and the
# range it reports, asked of every XCUIElementTypeSlider on the screen in
# front. Nothing is moved. CHALLENGES 219 (measured 2026-10-13).
#
#   docs/probes/slider-attributes-ios.sh <simulator-udid>
#
# Open the screen first with Mobium (Settings > Accessibility > Display &
# Text Size > Larger Text; Ice Cubes' Display Settings). The probe asks the
# runner Mobium's session started, found by its listening port, and asks
# for normalizedSliderPosition too, which it does not have.
set -e
UDID="$1"
if [ -z "$UDID" ]; then echo "usage: $0 <simulator-udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
"$ROOT/bin/mobium" --device "$UDID" current >/dev/null
PORT=$(lsof -nP -iTCP -sTCP:LISTEN 2>/dev/null | awk '/^WebDriver/ && /127\.0\.0\.1/ {sub(/.*:/, "", $9); print $9; exit}')
[ -n "$PORT" ] || { echo "no WebDriverAgent listening on 127.0.0.1" >&2; exit 1; }
B="http://127.0.0.1:$PORT"
S=$(curl -s "$B/status" | python3 -c 'import json, sys; print(json.load(sys.stdin).get("sessionId") or "")')
[ -n "$S" ] || { echo "WebDriverAgent on $PORT has no session" >&2; exit 1; }
attr() { curl -s "$B/session/$S/element/$1/attribute/$2" | python3 -c '
import json, sys
v = json.load(sys.stdin)["value"]
print("(unknown attribute)" if isinstance(v, dict) and "error" in v else v)'; }
curl -s -X POST "$B/session/$S/elements" -d '{"using":"class name","value":"XCUIElementTypeSlider"}' |
  python3 -c 'import json, sys; [print(v["ELEMENT"]) for v in json.load(sys.stdin)["value"]]' |
  while read -r E; do
    printf 'value=%s  min=%s  max=%s  normalizedSliderPosition=%s\n' \
      "$(attr "$E" value)" "$(attr "$E" minValue)" "$(attr "$E" maxValue)" "$(attr "$E" normalizedSliderPosition)"
  done
