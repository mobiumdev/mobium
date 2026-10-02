#!/bin/sh
# An App Clip on a simulator, measured: built from MobiumApp's appclip/, the
# clip alone installed as iOS delivers one, then launched, read, tapped and
# backed through with Mobium. docs/APP-TYPES.md, "Try before you install".
#
#   docs/probes/appclip-sim.sh <simulator-udid> <path to mobium-app>
#
# Opening the clip from a link is not here: the simulator offers no way to.
set -e
UDID="$1"; APPREPO="$2"
if [ -z "$UDID" ] || [ -z "$APPREPO" ]; then echo "usage: $0 <simulator-udid> <path to mobium-app>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium --driver wda --device $UDID"
CLIP=dev.mobium.clipdemo.Clip
DD=$(mktemp -d)
xcodebuild -project "$APPREPO/appclip/MobiumClipDemo.xcodeproj" -scheme ClipDemo -sdk iphonesimulator \
  -configuration Debug -derivedDataPath "$DD" CODE_SIGN_IDENTITY=- build >/dev/null 2>&1 ||
  { echo "the clip did not build" >&2; exit 1; }
xcrun simctl install "$UDID" "$DD/Build/Products/Debug-iphonesimulator/ClipDemoClip.app"
rm -rf "$DD"
echo "apps:    $($M apps | grep -c "$CLIP") listing for $CLIP"
$M terminate "$CLIP" >/dev/null 2>&1 || true
echo "launch:  $($M launch "$CLIP")"
echo "current: $($M current)"
echo "before:  $($M text testid=taps)"
$M tap testid=tapMe >/dev/null
echo "after:   $($M text testid=taps)"
$M tap testid=detailsLink >/dev/null; sleep 1
echo "pushed:  $($M text testid=detailsNote)"
echo "back:    $($M press back --gesture)"
echo "opened:  $($M text testid=invocation)"
