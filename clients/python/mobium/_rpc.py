"""JSON-RPC over stdio to a `mobium pipe` subprocess.

The client speaks the same tool layer the CLI and MCP clients use, so a script
and a command cannot drift apart. `pipe` forwards to the shared daemon rather
than starting a session of its own: a device-side server holds one session per
device, so a client with its own would invalidate the CLI's and vice versa.
"""

from __future__ import annotations

import json
import os
import queue
import subprocess
import sys
import threading
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
    """A live `mobium pipe` subprocess.

    Safe to share between threads, one call at a time: there is one pipe and
    replies are told apart only by id, so calls are serialized rather than
    interleaved.
    """

    def __init__(self, binary: str, args: list[str], call_timeout: float | None = None):
        if call_timeout is not None and not call_timeout > 0:
            raise ValueError("call_timeout must be a positive number of seconds, or None")
        self._timeout = call_timeout
        try:
            self._proc = subprocess.Popen(
                [binary, "pipe", *args],
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                # Progress notes about downloading a device-side server go to
                # stderr; passing them through keeps a slow first run
                # explicable instead of looking like a hang.
                stderr=None,
                encoding="utf-8",
                bufsize=1,
            )
        except OSError as e:
            raise MobiumError(f"could not start {binary}: {e}") from e
        self._next_id = 0

        # _gate serializes whole request/response pairs, as the Go client's
        # mutex does: two calls in flight would race to read each other's
        # answer.
        self._gate = threading.Lock()
        # _write_lock keeps one message whole on the pipe. close() writes a
        # detach while a call may hold _gate, and must not wait for it:
        # close() is how another thread gives up on a call that hangs.
        self._write_lock = threading.Lock()
        # _dead says why the connection can no longer be used, once something
        # has made that true: it was closed, mobium exited, or a call timed
        # out half-way. A call abandoned half-way cannot be resynchronized --
        # its answer would be read as the answer to the next one -- so the
        # connection ends, and every call after says so.
        self._dead: str | None = None
        self._closed = False

        # Every line mobium writes, in order, put here by one reader thread,
        # so a call can wait for the next one with a deadline. _END comes last.
        self._lines: queue.Queue[str | object] = queue.Queue()
        reader = threading.Thread(target=self._read, name="mobium-pipe-reader", daemon=True)
        reader.start()

        # A handshake that fails leaves a process nobody will ever close, so
        # it is ended here rather than leaked -- measured: a refused handshake
        # left mobium running.
        try:
            self._initialize()
        except BaseException:
            self.close()
            raise

    def _read(self) -> None:
        assert self._proc.stdout is not None
        try:
            for line in self._proc.stdout:
                self._lines.put(line)
        except (OSError, ValueError):
            pass
        finally:
            self._lines.put(_END)

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

    def _die(self, why: str) -> MobiumError:
        """Mark the connection unusable, end mobium, and return the error that says why."""
        if self._dead is None:
            self._dead = why
        try:
            self._proc.kill()
        except OSError:
            pass
        return MobiumError(why)

    def _write(self, payload: dict[str, Any]) -> None:
        if self._proc.poll() is not None:
            raise self._die(f"mobium exited with status {self._proc.returncode}")
        try:
            # allow_nan=False: JSON has no NaN or Infinity, and written bare
            # mobium cannot parse the line and answers with no id -- measured,
            # set_location(nan, 0) then waited forever.
            line = json.dumps(payload, allow_nan=False) + "\n"
        except ValueError as e:
            raise InvalidArgumentError(f"{e}: JSON has no NaN or Infinity") from None
        assert self._proc.stdin is not None
        try:
            with self._write_lock:
                self._proc.stdin.write(line)
                self._proc.stdin.flush()
        except (OSError, ValueError) as e:
            raise self._die(f"mobium closed the connection: {e}") from e

    def _notify(self, method: str) -> None:
        with self._gate:
            self._write({"jsonrpc": "2.0", "method": method})

    def _next_line(self, method: str) -> str | None:
        try:
            line = self._lines.get(timeout=self._timeout)
        except queue.Empty:
            raise self._die(
                f"{method} got no answer within {self._timeout}s, so the connection was closed: "
                "a reply arriving later would be read as the answer to the next call. "
                "Connect again, with a longer call_timeout if the device is slow"
            ) from None
        if line is _END:
            self._lines.put(_END)  # every later read sees the end too
            return None
        return line  # type: ignore[return-value]

    def request(self, method: str, params: dict[str, Any] | None = None) -> Any:
        with self._gate:
            if self._dead is not None:
                raise MobiumError(f"this mobium connection is no longer usable: {self._dead}")
            return self._request(method, params)

    def _request(self, method: str, params: dict[str, Any] | None) -> Any:
        self._next_id += 1
        message_id = self._next_id
        payload: dict[str, Any] = {"jsonrpc": "2.0", "id": message_id, "method": method}
        if params is not None:
            payload["params"] = params
        self._write(payload)

        while True:
            line = self._next_line(method)
            if line is None:
                status = ""
                try:
                    status = f" (it exited with status {self._proc.wait(timeout=2)})"
                except subprocess.TimeoutExpired:
                    pass
                raise self._die(f"mobium closed the connection without answering {method}{status}")
            try:
                message = json.loads(line)
            except (ValueError, RecursionError):
                continue  # not a message we can read; keep looking
            if not isinstance(message, dict):
                continue  # JSON, but not a message: null, a number
            got = message.get("id")
            if got is None and isinstance(message.get("error"), dict):
                # An error with no id is mobium saying it could not read a
                # request at all, which JSON-RPC answers without an id. The
                # pipe answers one request at a time, in order, and the gate
                # keeps one in flight, so it is this call's answer: skipping
                # it, as a notification is skipped, left the call waiting
                # forever.
                err = message["error"]
                raise InvalidArgumentError(
                    f"mobium could not read the request: {err.get('message')}: {err.get('data', '')}".strip(": ")
                )
            if got != message_id or isinstance(got, bool):
                continue  # a notification, or a reply to something else
            if "error" in message:
                err = message["error"] if isinstance(message["error"], dict) else {"message": str(message["error"])}
                # A protocol error: the request itself was refused.
                raise InvalidArgumentError(f"{err.get('message')}: {err.get('data', '')}".strip(": "))
            return message.get("result")

    def call_tool(self, name: str, arguments: dict[str, Any] | None = None) -> dict[str, Any]:
        """Run a tool, raising when the tool itself reports failure."""
        result = self.request("tools/call", {"name": name, "arguments": arguments or {}})
        if not isinstance(result, dict):
            raise MobiumError(f"mobium answered {name} with {type(result).__name__}, not a result")
        # A failing tool answers with isError rather than a protocol error, so
        # the reason is in the content and has to be lifted out deliberately.
        if result.get("isError"):
            raise error_from(_text_of(result), result.get("structuredContent"))
        return result

    def close(self) -> None:
        """Ask mobium to exit and wait for it, killing it after ten seconds.

        Safe to call more than once, and from another thread while a call is
        waiting: that call then fails, saying the connection was closed.
        """
        if self._closed:
            return
        self._closed = True
        if self._dead is None:
            self._dead = "it was closed"
        try:
            assert self._proc.stdin is not None
            # mobium ends the sessions a client started when the client goes
            # away, and a crash closes stdin just as this does -- so say
            # first that this is a deliberate close, or it would quit.
            with self._write_lock:
                self._proc.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "mobium/detach"}) + "\n")
                self._proc.stdin.flush()
        except (OSError, ValueError):
            pass
        try:
            self._proc.stdin.close()
        except (OSError, ValueError):
            pass
        try:
            self._proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self._proc.kill()
            self._proc.wait()


# The reader's last item: stdout closed or failed.
_END = object()


def _text_of(result: dict[str, Any]) -> str:
    parts = [c.get("text", "") for c in result.get("content", []) if c.get("type") == "text"]
    return "\n".join(parts).strip()


def _data_of(result: dict[str, Any]) -> Any:
    """The tool's structured answer, or None when it only returned prose."""
    return result.get("structuredContent")
