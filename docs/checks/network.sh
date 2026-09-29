#!/bin/sh
# Network conditions, end to end: `mobium network` on Android, judged by what
# the traffic does, never by what the tool reports.
#
#   docs/checks/network.sh <android-serial>
#
# On an emulator, against servers this script runs on the Mac (reached from
# the emulator as 10.0.2.2): latency by ping, download by timing 2MB from an
# HTTP server, upload by timing 1MB into a TCP sink — each measured before,
# shaped and after, so a limit that changed nothing fails. The emulator
# console's own `network speed` and `network delay` are read back as set and
# change nothing, measured; this is what shows Mobium's shaping is not them.
# Then offline: airplane mode, and a connection that must fail; back online,
# and the shaping that airplane mode took part of is put back. Last, a
# session ended while offline and shaped leaves the device online, unshaped.
#
# On a real phone: offline and back, the same way, and shaping refused — a
# user build has no root. A phone with wireless debugging on asks again
# whether to allow it on the network when the network comes back; that
# prompt is reported, never read out, since it names the network.
#
# Needs python3 on the Mac. Everything changed is put back, on failure too.
set -e
DEV="$1"
if [ -z "$DEV" ]; then echo "usage: $0 <android-serial>" >&2; exit 2; fi
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
M="$ROOT/bin/mobium --device $DEV"
A="adb -s $DEV"
fail() { echo "FAIL: $*" >&2; exit 1; }
row() { printf '    %-12s %-56s ok\n' "$1" "$2"; }
ms() { python3 -c 'import time; print(int(time.time()*1000))'; }
case "$DEV" in emulator-*) EMU=1 ;; *) EMU= ;; esac
echo "--- $DEV ($([ -n "$EMU" ] && echo emulator || echo phone))"

TMP=$(mktemp -d)
PIDS=
cleanup() {
  $M network --reset >/dev/null 2>&1 || true
  $A shell rm -f /data/local/tmp/mobium-net-up.bin >/dev/null 2>&1 || true
  for p in $PIDS; do kill "$p" 2>/dev/null || true; wait "$p" 2>/dev/null || true; done
  rm -rf "$TMP"
}
trap cleanup EXIT

# connects: can the device open a connection to the internet at all.
connects() { $A shell 'nc -z -w 5 connectivitycheck.gstatic.com 80 >/dev/null 2>&1 && echo yes || echo no' | tr -d '\r'; }

# --- offline, on any Android device ----------------------------------------
[ "$(connects)" = yes ] || fail "the device cannot reach the internet to begin with — nothing to take away"
r=$($M network --offline) || fail "going offline: $r"
echo "$r" | grep -q '^offline' || fail "offline was reported as: $r"
[ "$(connects)" = no ] || fail "airplane mode is on and a connection still got through"
r=$($M network --online) || fail "coming back: $r"
echo "$r" | grep -q '^online' || fail "online was reported as: $r"
[ "$(connects)" = yes ] || fail "back online and a connection still fails"
row "offline" "a connection failed in airplane mode, and worked after"

if [ -z "$EMU" ]; then
  set +e; r=$($M network --latency 300 2>&1); st=$?; set -e
  [ $st -eq 5 ] || fail "a phone was shaped, or refused as $st: $r"
  echo "$r" | grep -q "root" || fail "the refusal does not say why: $r"
  row "shaping" "refused: a user build has no root"
  if $M alert 2>/dev/null | grep -q 'a dialog is on screen'; then
    echo "    NOTE         a system dialog is up since the network came back — answer it on the phone"
  fi
  echo PASS
  exit 0
fi

# --- the servers the emulator measures against ------------------------------
head -c 2000000 /dev/urandom > "$TMP/blob"
head -c 1000000 /dev/urandom > "$TMP/up.bin"
(cd "$TMP" && exec python3 -m http.server 8765 --bind 127.0.0.1 >/dev/null 2>&1) & PIDS="$PIDS $!"
python3 -c '
import socket
s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", 8766)); s.listen(5)
while True:
    c, _ = s.accept()
    while c.recv(65536): pass
    c.close()
' & PIDS="$PIDS $!"
sleep 1
$A push "$TMP/up.bin" /data/local/tmp/mobium-net-up.bin >/dev/null

rtt() { $A shell ping -c 3 -i 0.3 10.0.2.2 | sed -n 's#.*= [0-9.]*/\([0-9.]*\)/.*#\1#p' | cut -d. -f1; }
down() { a=$(ms); $A shell 'printf "GET /blob HTTP/1.0\r\n\r\n" | nc -w 90 10.0.2.2 8765 | wc -c' >/dev/null; b=$(ms); echo $((b - a)); }
up() { a=$(ms); $A shell 'nc -w 90 10.0.2.2 8766 < /data/local/tmp/mobium-net-up.bin' >/dev/null; b=$(ms); echo $((b - a)); }

base_rtt=$(rtt); base_down=$(down); base_up=$(up)

# --- latency ----------------------------------------------------------------
$M network --latency 300 >/dev/null
got=$(rtt)
[ "$got" -ge 290 ] && [ "$got" -le 400 ] || fail "300ms asked for, and a ping took ${got}ms (${base_rtt}ms before)"
row "latency" "ping ${base_rtt}ms, then ${got}ms with 300ms asked for"

# --- download and upload: 2MB at 1000 kbit/s is 16s, 1MB at 500 is 16s ------
$M network --download 1000 --upload 500 >/dev/null
d=$(down); u=$(up)
[ "$d" -ge 14000 ] || fail "2MB at 1000 kbit/s took ${d}ms — 16s is due (${base_down}ms unshaped)"
[ "$u" -ge 14000 ] || fail "1MB at 500 kbit/s took ${u}ms — 16s is due (${base_up}ms unshaped)"
row "rates" "2MB down ${base_down}->${d}ms, 1MB up ${base_up}->${u}ms"

# --- offline takes part of it; coming back puts it back ---------------------
$M network --latency 100 --download 1000 >/dev/null
$M network --offline >/dev/null
r=$($M network --online)
echo "$r" | grep -q 'download 1000 kbit/s' || fail "back online, the download limit was not put back: $r"
d=$(down)
[ "$d" -ge 14000 ] || fail "back online, 2MB took ${d}ms — the limit is reported and not applied"
row "come back" "the download limit reapplied, and 2MB took ${d}ms"

# --- reset ------------------------------------------------------------------
$M network --reset >/dev/null
got=$(rtt); d=$(down)
[ "$got" -le 50 ] && [ "$d" -le 5000 ] || fail "after reset, ping ${got}ms and 2MB in ${d}ms"
row "reset" "ping ${got}ms, 2MB in ${d}ms"

# --- the end of a session puts it back --------------------------------------
$M session start >/dev/null
$M network --latency 200 --download 800 >/dev/null
$M network --offline >/dev/null
r=$($M session end)
echo "$r" | grep -q 'network is as it was' || fail "the session's end did not say it put the network back: $r"
[ "$($A shell cmd connectivity airplane-mode | tr -d '\r')" = disabled ] || fail "airplane mode is still on"
[ "$($A shell tc qdisc show | grep -c 'netem\|tbf')" = 0 ] || fail "shaping is left on the device"
[ "$(connects)" = yes ] || fail "after the session, the device cannot connect"
row "session end" "online again, and no shaping left"

echo PASS
