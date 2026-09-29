"""A stand-in for `mobium pipe`, so the connection's failure paths are
exercised against a real subprocess with no device.

MOBIUM_FAKE picks the behavior:
  ok      answer every call, first writing lines that must be skipped: a
          notification, a line that is not JSON, JSON that is not a message
          (null, a number), 100000 levels of nesting, and a reply to another
          id. Tool "slow" answers after 300ms.
  hang    answer the handshake, never a tool call
  exit    answer the handshake, exit with status 3 on the first tool call
  mute    never answer anything, the handshake included
  refuse  answer the handshake with a protocol error, then wait for stdin
  noid    answer app_map as mobium answers a line it cannot parse -- an error
          with no id -- and every other call normally
MOBIUM_FAKE_PIDFILE, when set, receives this process's id.
MOBIUM_FAKE_NOTIFYLOG, when set, has each notification's method appended.
app_upload and app_download answer as the daemon does, and echo their
arguments in the answer's "echo" key; app_download's file is b"fake\x00file".
"""
import base64
import json
import os
import sys
import time

mode = os.environ.get("MOBIUM_FAKE", "ok")
if os.environ.get("MOBIUM_FAKE_PIDFILE"):
    with open(os.environ["MOBIUM_FAKE_PIDFILE"], "w") as f:
        f.write(str(os.getpid()))
out = sys.stdout
if os.environ.get("MOBIUM_FAKE_NOTIFYLOG"):
    with open(os.environ["MOBIUM_FAKE_NOTIFYLOG"], "a") as f:
        f.write("session=" + os.environ.get("MOBIUM_SESSION", "") + "\n")


def send(obj):
    out.write(json.dumps(obj) + "\n")
    out.flush()


for line in sys.stdin:
    msg = json.loads(line)
    if "id" not in msg:
        if os.environ.get("MOBIUM_FAKE_NOTIFYLOG") and "method" in msg:
            with open(os.environ["MOBIUM_FAKE_NOTIFYLOG"], "a") as f:
                f.write(msg["method"] + "\n")
        continue
    i, method = msg["id"], msg["method"]
    if mode == "mute":
        continue
    if method == "initialize":
        if mode == "refuse":
            send({"jsonrpc": "2.0", "id": i, "error": {"code": -32602, "message": "unsupported protocol version"}})
        else:
            send({"jsonrpc": "2.0", "id": i, "result": {}})
        continue
    if mode == "hang":
        continue
    if mode == "exit":
        sys.exit(3)
    name, args = msg["params"]["name"], msg["params"]["arguments"]
    if name == "slow":
        time.sleep(0.3)
    if name == "app_session":
        # Answers as the daemon does: start names the device it got, and
        # echoes the platform and app it was asked for.
        action = args.get("action")
        if action == "start":
            platform = args.get("platform") or "android"
            view = {"action": "start", "device": "fake-device", "platform": platform,
                    "driver": "wda" if platform == "ios" else "uiautomator2", "app": args.get("app", ""), "sessions": []}
        elif action == "end":
            view = {"action": "end", "device": args.get("device", ""), "ended": True, "sessions": []}
        else:
            view = {"action": "status", "sessions": []}
        send({"jsonrpc": "2.0", "id": i, "result": {"content": [{"type": "text", "text": action or "status"}],
                                                    "structuredContent": view}})
        continue
    if name in ("app_upload", "app_download"):
        # Answers as the daemon does: a list with no name, the file inline
        # with no path, a transfer that names the path otherwise.
        body = b"fake\x00file"
        view = {"device": "fake-device", "echo": args}
        if args.get("app"):
            view["app"] = args["app"]
        if name == "app_download" and not args.get("name"):
            view.update(folder="the Download folder",
                        files=[{"name": "report.pdf", "bytes": len(body), "modified": "2026-09-29T10:00:00Z"}])
        else:
            view.update(name=args.get("name") or os.path.basename(args.get("path", "")),
                        where="/sdcard/Download", bytes=len(body), checked="size")
            if args.get("path"):
                view["path"] = args["path"]
            elif name == "app_download":
                view["data"] = base64.b64encode(body).decode()
        send({"jsonrpc": "2.0", "id": i, "result": {"content": [{"type": "text", "text": name}],
                                                    "structuredContent": view}})
        continue
    if mode == "noid" and name == "app_map":
        out.write('{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error","data":"invalid character"}}\n')
        out.flush()
        continue
    out.write('{"jsonrpc":"2.0","method":"notifications/message","params":{}}\n')
    out.write("progress: this line is not JSON\n")
    out.write("null\n5\n" + "[" * 100000 + "]" * 100000 + "\n")
    send({"jsonrpc": "2.0", "id": i + 1000, "result": {"wrong": True}})
    send({"jsonrpc": "2.0", "id": i, "result": {"content": [{"type": "text", "text": "ok " + name}],
                                                "structuredContent": {"tool": name, "echo": args}}})
