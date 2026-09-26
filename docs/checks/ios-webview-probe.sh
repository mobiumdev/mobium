#!/bin/sh
# Proves the iOS WebView path end to end, without any Mobium code.
#
#   ./docs/checks/ios-webview-probe.sh <simulator-udid>
#
# Roadmap item 4 described WKWebView as reachable only through "a third
# protocol", which read as a wall. It is not one: the whole path is a Unix
# socket, and this script walks it. Kept runnable because it verifies a
# *platform* assumption, and those rot — if Apple changes any of this, this
# script fails and the plan built on it needs revisiting.
#
# It needs Safari open on a page. Run:
#   xcrun simctl openurl <udid> https://example.com
set -e
U="$1"
[ -n "$U" ] || { echo "usage: $0 <simulator-udid>"; exit 2; }

SOCK=$(xcrun simctl spawn "$U" launchctl print "system/com.apple.webinspectord" 2>/dev/null \
        | grep -oE "/private/var/tmp/[^ ]*webinspectord_sim.socket" | head -1)
[ -n "$SOCK" ] || { echo "no RWI_LISTEN_SOCKET — is the simulator booted?"; exit 1; }
echo "socket: $SOCK"

python3 - "$SOCK" <<'PY'
import json, plistlib, socket, struct, sys, time

CID = "mobium-probe"
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(10)
s.connect(sys.argv[1])

def send(selector, argument):
    """Framing: four bytes of big-endian length, then a binary plist."""
    body = plistlib.dumps({"__selector": selector, "__argument": argument},
                          fmt=plistlib.FMT_BINARY)
    s.sendall(struct.pack(">I", len(body)) + body)

def recv():
    head = b""
    while len(head) < 4:
        c = s.recv(4 - len(head))
        if not c: return None
        head += c
    n = struct.unpack(">I", head)[0]
    body = b""
    while len(body) < n:
        c = s.recv(n - len(body))
        if not c: return None
        body += c
    return plistlib.loads(body)

def forward(app, page, obj):
    send("_rpc_forwardSocketData:", {
        "WIRConnectionIdentifierKey": CID, "WIRApplicationIdentifierKey": app,
        "WIRPageIdentifierKey": page, "WIRSenderKey": "mobium-sender",
        "WIRSocketDataKey": json.dumps(obj).encode()})

send("_rpc_reportIdentifier:", {"WIRConnectionIdentifierKey": CID})

app = page = None
ok = False
deadline = time.time() + 20
while time.time() < deadline and not ok:
    msg = recv()
    if msg is None: break
    selector = msg.get("__selector", "")
    arg = msg.get("__argument", {})

    if selector == "_rpc_reportConnectedApplicationList:" and app is None:
        for key, val in arg.get("WIRApplicationDictionaryKey", {}).items():
            if val.get("WIRApplicationBundleIdentifierKey") == "com.apple.mobilesafari":
                app = key
                print("  application:", val.get("WIRApplicationNameKey"), "->", key)
                send("_rpc_forwardGetListing:",
                     {"WIRConnectionIdentifierKey": CID, "WIRApplicationIdentifierKey": app})

    elif selector == "_rpc_applicationSentListing:" and app and page is None:
        listing = arg.get("WIRListingKey", {})
        if not listing:
            continue  # the WebContent process reports an empty listing
        entry = list(listing.values())[0]
        page = entry.get("WIRPageIdentifierKey")
        print("  page:", entry.get("WIRTitleKey"), "->", entry.get("WIRURLKey"))
        send("_rpc_forwardSocketSetup:", {
            "WIRConnectionIdentifierKey": CID, "WIRApplicationIdentifierKey": app,
            "WIRPageIdentifierKey": page, "WIRSenderKey": "mobium-sender",
            "WIRAutomaticallyPause": False})

    elif selector == "_rpc_applicationSentData:":
        data = arg.get("WIRMessageDataKey")
        if not data: continue
        m = json.loads(bytes(data).decode())
        # WebKit since iOS 12.2 is multi-target: commands are wrapped in
        # Target.sendMessageToTarget and replies arrive wrapped too. Sending
        # Runtime.evaluate directly answers "'Runtime' domain was not found".
        if m.get("method") == "Target.targetCreated":
            target = m["params"]["targetInfo"]["targetId"]
            print("  target:", target)
            forward(app, page, {"id": 2, "method": "Target.sendMessageToTarget",
                "params": {"targetId": target, "message": json.dumps(
                    {"id": 10, "method": "Runtime.evaluate", "params": {
                        "expression": "document.title + ' @ ' + innerWidth + 'x' + innerHeight"}})}})
        elif m.get("method") == "Target.dispatchMessageFromTarget":
            inner = json.loads(m["params"]["message"])
            if inner.get("id") == 10:
                print("  Runtime.evaluate ->", json.dumps(inner.get("result", inner)))
                ok = True

print("PASS" if ok else "FAIL — the path did not complete")
sys.exit(0 if ok else 1)
PY
