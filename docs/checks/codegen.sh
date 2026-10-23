#!/bin/sh
# A recording as code: `mobium inspect` records a flow on MobiumApp's Table
# Demo through its own page's calls, downloads it in all five clients'
# languages, and each program is run on the device and judged by what the
# app says it received. docs/guides/inspector.md, "Download it as code".
#
#   docs/checks/codegen.sh <android-serial | simulator-udid>
#
# - The flow: from MobiumApp launched fresh, scroll to and open Table Demo,
#   tap Item 2's More — which map names only by its path, every row having
#   one — and expect the app's outcome line, "More: Item 2".
# - The More is recorded scoped to its row, `within text="Item 2"`, and not
#   by the path that moves when the list does.
# - --record writes the test file as it grows, the same as the download.
# - Each program — Python, JavaScript, Go, Java, .NET — starts from the app
#   launched fresh, as the test does, and passes only if its own wait for
#   Item 2's outcome holds. The device each is pointed at is $DEV: the
#   check adds it to the one line that starts the session, since a recorded
#   program names no device.
# - The test file runs under `mobium test` as well.
#
# A language whose toolchain is missing is reported as skipped, not passed.
# Nothing here changes the device: the outcome line is the app's own.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <android-serial | simulator-udid>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$ROOT/docs/checks/lib.sh"
check_lock "$DEV"
case "$DEV" in
  ????????-????-????-????-????????????) DRIVER="--driver wda" ;;
  *) DRIVER= ;;
esac
M="$ROOT/bin/mobium $DRIVER --device $DEV"
APP=dev.mobium.mobiumapp
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-58s ok\n' "$1" "$2"; }
echo "--- $DEV"

TMP=$(mktemp -d)
IPID=
cleanup() {
  [ -n "$IPID" ] && { kill "$IPID" 2>/dev/null || true; wait "$IPID" 2>/dev/null || true; }
  rm -rf "$TMP"
}
at_exit cleanup

apps=$($M apps 2>&1) || fail "cannot list the apps: $(echo "$apps" | tail -1)"
echo "$apps" | grep -q "$APP" || fail "$APP is not installed — build and install MobiumApp first"
$M terminate "$APP" >/dev/null 2>&1 || true
$M launch "$APP" >/dev/null

# --- record, through the page's own calls ----------------------------------
$ROOT/bin/mobium inspect $DRIVER --device "$DEV" --record "$TMP/live/recorded.test.json" > "$TMP/inspect.out" 2>&1 &
IPID=$!
i=0; while ! grep -q 'http://' "$TMP/inspect.out" && [ $i -lt 50 ]; do sleep 0.2; i=$((i + 1)); done
URL=$(grep -o 'http://[^ ]*' "$TMP/inspect.out") || fail "the inspector did not start: $(cat "$TMP/inspect.out")"
post() { curl -sf -X POST -H 'Content-Type: application/json' -d "$2" "$URL$1"; }
act() {
  out=$(post act "$1") || fail "the inspector refused $1"
  echo "$out" | grep -q '"error"' && fail "$1: $out"
  echo "$out"
}

post fresh '{}' >/dev/null || fail "starting fresh"
act '{"tool":"app_scroll_to","arguments":{"target":"text=\"Table Demo\""}}' >/dev/null
act '{"tool":"app_tap","arguments":{"target":"text=\"Table Demo\""}}' >/dev/null
$M wait testid=tableOutcome >/dev/null || fail "the Table Demo did not open"
# The page reads the screen before anything is picked, as a person's does.
MORE=$(curl -sf "${URL}state" | python3 -I -c '
import json, sys
mores = [e for e in json.load(sys.stdin)["elements"] if e["label"] == "More"]
print(mores[1]["target"] if len(mores) > 1 else "")')
[ -n "$MORE" ] || fail "the Table Demo shows fewer than two More buttons"
out=$(act "{\"tool\":\"app_tap\",\"arguments\":{\"target\":\"$MORE\"}}")
echo "$out" | grep -q 'recorded as label=\\"More\\",role=button within text=\\"Item 2\\"' ||
  fail "Item 2's More ($MORE) was not recorded within its row: $out"
[ "$($M text testid=tableOutcome)" = "More: Item 2" ] || fail "the tap reached $($M text testid=tableOutcome)"
act '{"tool":"app_wait_for","arguments":{"target":"testid=\"tableOutcome\"","condition":"text","text":"More: Item 2","exact":true}}' >/dev/null
row "record" "Item 2's More, by its path in map, recorded within its row"

curl -sf "${URL}test" > "$TMP/recorded.test.json" || fail "downloading the test file"
cmp -s "$TMP/recorded.test.json" "$TMP/live/recorded.test.json" ||
  fail "--record wrote a different file than the download: $(diff "$TMP/recorded.test.json" "$TMP/live/recorded.test.json")"
row "--record" "written as it grew, the same as the download"
for lang in python javascript go java dotnet; do
  curl -sf "${URL}test?lang=$lang" > "$TMP/$lang.src" || fail "downloading it as $lang"
done
kill "$IPID"; wait "$IPID" 2>/dev/null || true; IPID=

# --- run each ----------------------------------------------------------------
# reset leaves the app on another screen, so a program that skipped a step
# could not find Item 2's outcome waiting for it.
reset() { $M terminate "$APP" >/dev/null 2>&1 || true; }
MOBIUM_BIN_PATH="$ROOT/bin/mobium"; export MOBIUM_BIN_PATH
CLIENTS="$ROOT/clients"
JAVA_HOME_GUESS="$(/usr/libexec/java_home 2>/dev/null || echo /opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home)"
DOTNET="$(command -v dotnet 2>/dev/null || echo "$HOME/.dotnet/dotnet")"
case "$DRIVER" in *wda*) ON="--driver wda" ;; *) ON= ;; esac

# point <file> <from> <to>: name the device on the line that starts the
# session, and fail if that line is not there.
point() {
  grep -qF "$2" "$1" || fail "$1 has no \"$2\" to point at $DEV"
  python3 -I -c 'import sys; p, a, b = sys.argv[1:]; s = open(p).read(); open(p, "w").write(s.replace(a, b, 1))' "$1" "$2" "$3"
}
WDA_PY=; WDA_JS=; WDA_GO=; WDA_JAVA=; WDA_CS=
if [ -n "$ON" ]; then
  WDA_PY=', driver="wda"'; WDA_JS=", driver: 'wda'"; WDA_GO=', mobium.WithDriver("wda")'
  WDA_JAVA='.driver("wda")'; WDA_CS='.Driver("wda")'
fi

reset
mkdir -p "$TMP/py" && cp "$TMP/python.src" "$TMP/py/recorded.py"
point "$TMP/py/recorded.py" "start()" "start(device=\"$DEV\"$WDA_PY)"
PYTHONPATH="$CLIENTS/python" python3 "$TMP/py/recorded.py" || fail "the Python program failed"
row "python" "ran, and its wait for Item 2's outcome held"

reset
mkdir -p "$TMP/js/node_modules" && ln -s "$CLIENTS/javascript" "$TMP/js/node_modules/mobium"
cp "$TMP/javascript.src" "$TMP/js/recorded.mjs"
point "$TMP/js/recorded.mjs" "await start()" "await start({ device: '$DEV'$WDA_JS })"
(cd "$TMP/js" && node recorded.mjs) || fail "the JavaScript program failed"
row "javascript" "ran, and its wait for Item 2's outcome held"

reset
mkdir -p "$TMP/go" && cp "$TMP/go.src" "$TMP/go/main.go"
point "$TMP/go/main.go" "mobium.Start(ctx)" "mobium.Start(ctx, mobium.WithDevice(\"$DEV\")$WDA_GO)"
# go reads go.mod itself, so Git Bash never converts the path in it: on
# Windows it is written C:/..., not /c/....
GOCLIENTS="$CLIENTS"
case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) GOCLIENTS="$(cygpath -m "$CLIENTS")" ;; esac
printf 'module recorded\n\ngo 1.22\n\nrequire github.com/mobiumdev/mobium/clients/go v0.0.0\n\nreplace github.com/mobiumdev/mobium/clients/go => %s/go\n' "$GOCLIENTS" > "$TMP/go/go.mod"
(cd "$TMP/go" && GOFLAGS=-mod=mod GOWORK=off go run .) || fail "the Go program failed"
row "go" "ran, and its wait for Item 2's outcome held"

reset
if [ -x "$JAVA_HOME_GUESS/bin/javac" ]; then
  (cd "$CLIENTS/java" && JAVA_HOME="$JAVA_HOME_GUESS" ./mvnw -B -q --no-transfer-progress compile) || fail "building the Java client"
  mkdir -p "$TMP/java" && cp "$TMP/java.src" "$TMP/java/Recorded.java"
  point "$TMP/java/Recorded.java" "Mobium.builder().start()" "Mobium.builder().device(\"$DEV\")$WDA_JAVA.start()"
  "$JAVA_HOME_GUESS/bin/javac" -cp "$CLIENTS/java/target/classes" -d "$TMP/java" "$TMP/java/Recorded.java" ||
    fail "the Java program does not compile"
  "$JAVA_HOME_GUESS/bin/java" -cp "$CLIENTS/java/target/classes:$TMP/java" Recorded || fail "the Java program failed"
  row "java" "ran, and its wait for Item 2's outcome held"
else
  echo "    java         SKIPPED -- no JDK"
fi

reset
if [ -x "$DOTNET" ]; then
  mkdir -p "$TMP/cs" && cp "$TMP/dotnet.src" "$TMP/cs/Program.cs"
  point "$TMP/cs/Program.cs" "Device.Builder().Start()" "Device.Builder().OnDevice(\"$DEV\")$WDA_CS.Start()"
  cat > "$TMP/cs/recorded.csproj" <<EOF
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net10.0</TargetFramework><Nullable>enable</Nullable></PropertyGroup>
  <ItemGroup><ProjectReference Include="$CLIENTS/dotnet/Mobium/Mobium.csproj" /></ItemGroup>
</Project>
EOF
  "$DOTNET" run --project "$TMP/cs/recorded.csproj" || fail "the .NET program failed"
  row "dotnet" "ran, and its wait for Item 2's outcome held"
else
  echo "    dotnet       SKIPPED -- no dotnet SDK"
fi

reset
(cd "$TMP" && "$ROOT/bin/mobium" test $ON --device "$DEV" recorded.test.json > "$TMP/test.out" 2>&1) ||
  fail "mobium test failed the recording: $(tail -5 "$TMP/test.out")"
row "test" "mobium test passes the same recording"
echo PASS
