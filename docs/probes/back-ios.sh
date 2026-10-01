#!/bin/sh
# Back on iOS, measured: press back, a swipe from the left edge, and the
# screen's own back button, in MobiumApp and in Settings > Search, the
# control. On a phone use another app as the control: Settings' first page
# names the owner. docs/BACK.md.
#
#   docs/probes/back-ios.sh <simulator-udid>
DEV="$1"; ROOT="$(cd "$(dirname "$0")/../.." && pwd)"; M="$ROOT/bin/mobium --driver wda --device $DEV"
APP=dev.mobium.mobiumapp
SHOT=$(mktemp -t backprobe).png; $M screenshot -o "$SHOT" >/dev/null 2>&1
size="$(sips -g pixelWidth "$SHOT" | awk '/pixelWidth/{print $2}') $(sips -g pixelHeight "$SHOT" | awk '/pixelHeight/{print $2}')"; rm -f "$SHOT"
W=${size% *}; H=${size#* }; Y=$((H/2)); TO=$((W*65/100))
where() {
  f=$($M current 2>/dev/null)
  case "$f" in
    $APP) $M map 2>/dev/null | grep -q "Back (button)" && echo app-demo || echo app-home ;;
    com.apple.Preferences) $M map 2>/dev/null | grep -q "^@e[0-9]* Settings (button)" && echo settings-sub || echo settings-root ;;
    *) echo "other:$f" ;;
  esac
}
echo "screen ${W}x${H} px"
for how in key edge; do
  $M terminate $APP >/dev/null 2>&1; $M launch $APP >/dev/null; sleep 2
  $M tap "label=Login Demo" >/dev/null; sleep 1
  before=$(where)
  if [ $how = key ]; then out=$($M press back 2>&1); else $M swipe 0 $Y $TO $Y --duration 300ms >/dev/null; out=swiped; fi
  sleep 2; echo "mobiumapp $how: $before -> $(where) [$(echo "$out" | head -1 | cut -c1-90)]"
done
for how in edge control; do
  $M terminate com.apple.Preferences >/dev/null 2>&1; $M launch com.apple.Preferences >/dev/null; sleep 2
  $M tap "label=Settings,role=button" >/dev/null 2>&1; sleep 1; $M tap "label=Search,role=button" >/dev/null; sleep 2
  before=$(where)
  if [ $how = edge ]; then $M swipe 0 $Y $TO $Y --duration 300ms >/dev/null; else $M tap "label=Settings,role=button" >/dev/null; fi
  sleep 2; echo "settings $how: $before -> $(where)"
done
