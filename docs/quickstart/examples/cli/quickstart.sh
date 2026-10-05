#!/bin/sh
# Mobium quick start from the command line: start a session, drive Settings,
# quit.
#
#   MOBIUM_PLATFORM=android sh quickstart.sh    # or ios
set -e
PLATFORM="${MOBIUM_PLATFORM:-android}"
if [ "$PLATFORM" = ios ]; then
  APP=com.apple.Preferences ROW=General NEXT="label=About,role=button"
else
  APP=com.android.settings ROW="Network & internet" NEXT="text=Airplane mode"
fi
# With one device running, no --device is needed; MOBIUM_DEVICE picks one of several.
if [ -n "$MOBIUM_DEVICE" ]; then set -- --device "$MOBIUM_DEVICE"; else set --; fi

# 1. Start the session: the driver is started on the device and Settings is
#    launched. Every command after it uses this session. Quit it however the
#    script ends.
mobium session start --platform "$PLATFORM" --app "$APP" "$@"
trap 'mobium session end "$@"' EXIT

# 2. Map the screen: every element you can act on, each with a @ref.
mobium map "$@" | head -5

# 3. Tap a row by its ref, then wait for the screen it opens. A row's label can
#    carry its summary too ("Network & internet Mobile, Wi-Fi, ..."), so match
#    its start: the label begins right after the ref.
REF=$(mobium map "$@" | awk -v row="$ROW" 'index($0, $1 " " row) == 1 {print $1; exit}')
mobium tap "$REF" "$@"
mobium wait "$NEXT" "$@"

# 4. Take a screenshot.
mobium screenshot -o "quickstart-$PLATFORM.png" "$@"
