#!/bin/sh
# Accessibility settings, end to end: app_accessibility changes each setting
# the platform has, the app is told, and when the session ends the device is
# back exactly as it was — every raw value, an unset key unset again.
#
# The app's half is MobiumApp's Accessibility Demo, which reports what the
# platform tells it (a11yState). The device's half is a snapshot of the raw
# settings taken before anything is touched and compared after the daemon
# stops: adb `settings` on Android, the com.apple.Accessibility defaults and
# simctl's two ui settings on a simulator. A real iPhone is refused — nothing
# outside can change these there — and the refusal is what is asserted.
#
#   docs/checks/accessibility.sh <serial|udid>
#
# Needs MobiumApp installed (mobiumdev/mobium-app).
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <serial|udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-28s %-40s ok\n' "$1" "$2"; }
APP=dev.mobium.mobiumapp
export MOBIUM_SESSION="${MOBIUM_SESSION:-a11ycheck}"

case "$DEV" in
  ????????-????????????????) PLATFORM=phone; M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *-*-*-*-*)                 PLATFORM=ios;   M="$ROOT/bin/mobium --driver wda --device $DEV" ;;
  *)                         PLATFORM=android; M="$ROOT/bin/mobium --device $DEV" ;;
esac
echo "--- $DEV ($PLATFORM)"

if [ "$PLATFORM" = phone ]; then
  if out=$($M accessibility bold_text on 2>&1); then fail "a real iPhone accepted a change: $out"; fi
  echo "$out" | grep -q 'Settings > Accessibility' || fail "the refusal does not name the Settings route: $out"
  row "real iPhone" "refused, naming the Settings route"
  echo PASS; exit 0
fi

# raw prints every stored value the settings below touch, one per line.
raw() {
  if [ "$PLATFORM" = android ]; then
    for k in "global animator_duration_scale" "global transition_animation_scale" "global window_animation_scale" \
             "secure font_weight_adjustment" "secure high_text_contrast_enabled" \
             "secure accessibility_display_inversion_enabled" "secure accessibility_display_daltonizer_enabled" \
             "secure accessibility_display_daltonizer" "system font_scale"; do
      # shellcheck disable=SC2086
      echo "$k=$(adb -s "$DEV" shell settings get $k | tr -d '\r')"
    done
  else
    xcrun simctl spawn "$DEV" defaults read com.apple.Accessibility 2>/dev/null || true
    echo "contrast=$(xcrun simctl ui "$DEV" increase_contrast) size=$(xcrun simctl ui "$DEV" content_size)"
  fi
}
state() { $M text testid=a11yState; }

# live prints what the running system applies, which the raw values do not
# say: an unset key deleted again read back unset while the text stayed bold
# (CHALLENGES 143). Android's configuration carries the font weight.
live() {
  if [ "$PLATFORM" = android ]; then
    adb -s "$DEV" shell dumpsys window 2>/dev/null | grep -o -E 'fontWeightAdjustment=[-0-9]+|fontScale=[0-9.]+' | sort -u
  fi
}
$M daemon stop >/dev/null 2>&1 || true
BEFORE=$(raw)
LIVE_BEFORE=$(live)
trap '$M daemon stop >/dev/null 2>&1 || true' EXIT

# What each platform has, and the value each is changed to. The last column
# is the app's word for it in a11yState, or - where React Native tells the
# app nothing on that platform.
if [ "$PLATFORM" = android ]; then
  CHANGES="reduce_motion:on:motion=true
bold_text:on:-
increase_contrast:on:-
invert_colors:on:invert=true
grayscale:on:grayscale=true
text_scale:1.3:fontScale=1.30"
else
  CHANGES="reduce_motion:on:motion=true
bold_text:on:bold=true
increase_contrast:on:contrast=true
reduce_transparency:on:transparency=true
button_shapes:on:-
differentiate_without_color:on:-
text_size:accessibility-large:fontScale=2.14"
fi

echo "$CHANGES" | while IFS=: read -r name value _; do
  out=$($M accessibility "$name" "$value" 2>&1) || fail "$name $value: $out"
  echo "$out" | grep -q "put back when the session ends" || fail "$name $value did not say it would be put back: $out"
done
row "changed" "$(echo "$CHANGES" | wc -l | tr -d ' ') settings, each confirmed"

# The app's half. Opened after the changes: on Android a text-scale change
# recreates a running app's activity.
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null
$M scroll-to "label=Accessibility Demo" --direction down >/dev/null 2>&1 || true
$M tap "label=Accessibility Demo" >/dev/null
$M wait testid=a11yState >/dev/null
seen=$(state)
echo "$CHANGES" | while IFS=: read -r name _ word; do
  [ "$word" = - ] && continue
  echo "$seen" | grep -q "$word" || fail "$name: the app was not told — it says: $seen"
done
row "the app was told" "every setting it can see"

# Refusals name their reason.
if [ "$PLATFORM" = android ]; then
  $M accessibility reduce_transparency on >/dev/null 2>&1 && fail "Android accepted reduce_transparency"
  $M accessibility text_size large 2>&1 | grep -q 'text_scale' || fail "text_size on Android does not point at text_scale"
else
  $M accessibility grayscale on >/dev/null 2>&1 && fail "the simulator accepted grayscale"
  $M accessibility text_scale 1.3 2>&1 | grep -q 'text_size' || fail "text_scale on iOS does not point at text_size"
fi
row "what it lacks" "refused, with the alternative"

# The device's half: back exactly as it was once the session ends.
$M terminate "$APP" >/dev/null 2>&1 || true
$M daemon stop >/dev/null
AFTER=$(raw)
[ "$BEFORE" = "$AFTER" ] || { echo "$BEFORE" > /tmp/a11y-before.$$; echo "$AFTER" > /tmp/a11y-after.$$;
  diff /tmp/a11y-before.$$ /tmp/a11y-after.$$ >&2; rm -f /tmp/a11y-before.$$ /tmp/a11y-after.$$;
  fail "the device was not put back as it was"; }
row "session ended" "every raw value as it was before"
sleep 2
LIVE_AFTER=$(live)
[ "$LIVE_BEFORE" = "$LIVE_AFTER" ] || fail "the raw values were put back and the screen was not: $(echo $LIVE_BEFORE) before, $(echo $LIVE_AFTER) after"
[ "$PLATFORM" = android ] && row "on screen" "the font weight the system applies, as before"
echo PASS
