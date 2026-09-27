"""JSON-RPC over stdio to a `mobium pipe` subprocess.

The client speaks the same tool layer the CLI and MCP clients use, so a script
and a command cannot drift apart. `pipe` forwards to the shared daemon rather
than starting a session of its own: a device-side server holds one session per
device, so a client with its own would invalidate the CLI's and vice versa.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
from typing import Any


from ._errors import InvalidArgumentError, MobiumError, error_from


def find_binary(explicit: str | None = None) -> str:
    """Locate the mobium binary: the explicit path, then MOBIUM_BIN_PATH,
    then PATH — and nothing else.

    MOBIUM_BIN_PATH wins, so a test run can pin a specific build — the same
    escape hatch vibium's clients have. The current directory is never
    searched, not in ./bin and not through a relative PATH entry: a library
    that runs whatever mobium sits where a test was started runs a binary
    anyone could have planted there. PATH is walked here rather than through
    shutil.which, which on Windows looks in the current directory first.
    """
    for candidate in (explicit, os.environ.get("MOBIUM_BIN_PATH")):
        if candidate:
            if os.path.isfile(candidate) and os.access(candidate, os.X_OK):
                return candidate
            raise MobiumError(f"{candidate} is not an executable mobium binary")

    name = "mobium.exe" if sys.platform == "win32" else "mobium"
    for directory in os.environ.get("PATH", "").split(os.pathsep):
        if not os.path.isabs(directory):
            continue
        found = os.path.join(directory, name)
        if os.path.isfile(found) and os.access(found, os.X_OK):
            return found
    raise MobiumError(
        "mobium not found — put it on PATH or set MOBIUM_BIN_PATH to the binary"
    )


class Connection:
    """A live `mobium pipe` subprocess."""

    def __init__(self, binary: str, args: list[str]):
        self._proc = subprocess.Popen(
            [binary, "pipe", *args],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            # Progress notes about downloading a device-side server go to
            # stderr; passing them through keeps a slow first run explicable
            # instead of looking like a hang.
            stderr=None,
            text=True,
            bufsize=1,
        )
        self._next_id = 0
        self._initialize()

    def _initialize(self) -> None:
        self.request(
            "initialize",
            {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {"name": "mobium-python", "version": "0.1.0"},
            },
        )
        self._notify("notifications/initialized")

    def _write(self, payload: dict[str, Any]) -> None:
        if self._proc.poll() is not None:
            raise MobiumError(f"mobium exited with status {self._proc.returncode}")
        assert self._proc.stdin is not None
        self._proc.stdin.write(json.dumps(payload) + "\n")
        self._proc.stdin.flush()

    def _notify(self, method: str) -> None:
        self._write({"jsonrpc": "2.0", "method": method})

    def request(self, method: str, params: dict[str, Any] | None = None) -> Any:
        self._next_id += 1
        message_id = self._next_id
        payload: dict[str, Any] = {"jsonrpc": "2.0", "id": message_id, "method": method}
        if params is not None:
            payload["params"] = params
        self._write(payload)

        assert self._proc.stdout is not None
        while True:
            line = self._proc.stdout.readline()
            if not line:
                raise MobiumError("mobium closed the connection without responding")
            try:
                message = json.loads(line)
            except json.JSONDecodeError:
                continue
            if message.get("id") != message_id:
                continue  # a notification, or a reply to something else
            if "error" in message:
                err = message["error"]
                # A protocol error: the request itself was refused.
                raise InvalidArgumentError(f"{err.get('message')}: {err.get('data', '')}".strip(": "))
            return message.get("result")

    def call_tool(self, name: str, arguments: dict[str, Any] | None = None) -> dict[str, Any]:
        """Run a tool, raising when the tool itself reports failure."""
        result = self.request("tools/call", {"name": name, "arguments": arguments or {}})
        # A failing tool answers with isError rather than a protocol error, so
        # the reason is in the content and has to be lifted out deliberately.
        if result.get("isError"):
            raise error_from(_text_of(result), result.get("structuredContent"))
        return result

    def close(self) -> None:
        if self._proc.poll() is None:
            try:
                assert self._proc.stdin is not None
                self._proc.stdin.close()
                self._proc.wait(timeout=10)
            except Exception:
                self._proc.kill()


def _text_of(result: dict[str, Any]) -> str:
    parts = [c.get("text", "") for c in result.get("content", []) if c.get("type") == "text"]
    return "\n".join(parts).strip()


def _data_of(result: dict[str, Any]) -> Any:
    """The tool's structured answer, or None when it only returned prose."""
    return result.get("structuredContent")
