"""Mobium quick start over MCP: start a session, drive Settings, quit.

    MOBIUM_PLATFORM=android python3 quickstart.py    # or ios

An agent does all of this for you once `mobium mcp` is registered with it.
This is the same conversation by hand: the requests an MCP client sends, one
JSON-RPC message per line on the server's stdin, and the answers it reads
back. Standard library only.
"""
import json
import os
import subprocess

# Settings is on every emulator, simulator and phone, with nothing to install.
PLATFORMS = {
    "android": {"app": "com.android.settings", "row": "Network & internet", "next": "text=Airplane mode"},
    "ios": {"app": "com.apple.Preferences", "row": "General", "next": "label=About,role=button"},
}
platform = os.environ.get("MOBIUM_PLATFORM", "android")
p = PLATFORMS[platform]

# With one device running, no --device is needed; MOBIUM_DEVICE picks one of several.
cmd = ["mobium", "mcp"]
if os.environ.get("MOBIUM_DEVICE"):
    cmd += ["--device", os.environ["MOBIUM_DEVICE"]]
server = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
next_id = 0


def request(method, params):
    global next_id
    next_id += 1
    server.stdin.write(json.dumps({"jsonrpc": "2.0", "id": next_id, "method": method, "params": params}) + "\n")
    server.stdin.flush()
    while True:
        reply = json.loads(server.stdout.readline())
        if reply.get("id") == next_id:
            return reply["result"]


def call(tool, arguments):
    """One tools/call. A tool that fails answers isError, with a code."""
    print(f"→ {tool} {json.dumps(arguments, ensure_ascii=False)}")
    result = request("tools/call", {"name": tool, "arguments": arguments})
    if result.get("isError"):
        code = result.get("structuredContent", {}).get("code")
        raise SystemExit(f"{tool} failed [{code}]: {result['content'][0]['text']}")
    return result


# The handshake every MCP client opens with.
request("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                       "clientInfo": {"name": "quickstart", "version": "1"}})
server.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
tools = request("tools/list", {})["tools"]
print(f"{len(tools)} tools, among them app_session, app_map, app_tap")

try:
    # 1. Start the session: the driver is started on the device and Settings
    #    is launched. Every call after it uses this session.
    started = call("app_session", {"action": "start", "platform": platform, "app": p["app"]})
    print("  " + started["content"][0]["text"])

    # 2. Map the screen: every element you can act on, each with a @ref. The
    #    text is for reading; structuredContent is the same list, as data.
    elements = call("app_map", {})["structuredContent"]["elements"]
    for e in elements[:5]:
        print(f"  {e['ref']} {e['label']} ({e['role']})")

    # 3. Tap a row by its ref, then wait for the screen it opens. A row's label
    #    can carry its summary too ("Network & internet Mobile, Wi-Fi, ..."),
    #    so match its start.
    row = next(e for e in elements if e["label"].startswith(p["row"]))
    call("app_tap", {"target": row["ref"]})
    waited = call("app_wait_for", {"target": p["next"]})
    print("  " + waited["content"][0]["text"])

    # 4. Take a screenshot. The image comes back in the answer; path also
    #    writes it to disk.
    call("app_screenshot", {"path": f"quickstart-{platform}.png"})
    print(f"  saved quickstart-{platform}.png")
finally:
    # 5. End the session. Closing stdin would end it too, as an agent's
    #    client does when it exits.
    ended = call("app_session", {"action": "end"})
    print("  " + ended["content"][0]["text"])
    server.stdin.close()
    server.wait()
