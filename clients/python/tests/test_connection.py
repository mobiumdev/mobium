"""The connection, against fake_mobium.py standing in for mobium, with no
test framework and no device: `python3 clients/python/tests/test_connection.py`.
make ci runs it."""

import os
import pathlib
import stat
import sys
import tempfile
import threading
import time

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent.parent))

from mobium import InvalidArgumentError, MobiumError, connect  # noqa: E402
from mobium._rpc import Connection  # noqa: E402

HERE = pathlib.Path(__file__).resolve().parent
failures = []
checks = 0


def check(cond, what):
    global checks
    checks += 1
    if not cond:
        failures.append(what)


def launcher() -> str:
    """The fake as an executable, on this interpreter."""
    d = tempfile.mkdtemp(prefix="mobium-fake-")
    if sys.platform == "win32":
        p = os.path.join(d, "mobium.cmd")
        pathlib.Path(p).write_text(f'@"{sys.executable}" "{HERE / "fake_mobium.py"}" %*\r\n')
    else:
        p = os.path.join(d, "mobium")
        pathlib.Path(p).write_text(f"#!/bin/sh\nexec '{sys.executable}' '{HERE / 'fake_mobium.py'}' \"$@\"\n")
        os.chmod(p, os.stat(p).st_mode | stat.S_IXUSR)
    return p


FAKE = launcher()


def fake(mode, timeout=None, pidfile=None):
    os.environ["MOBIUM_FAKE"] = mode
    os.environ["MOBIUM_FAKE_PIDFILE"] = pidfile or ""
    return Connection(FAKE, [], timeout)


def raises(kind, fn):
    try:
        fn()
    except kind as e:
        return e
    return None


# -- what is not the answer is skipped -----------------------------------
c = fake("ok")
r = c.call_tool("app_echo", {"k": "v"})["structuredContent"]
check(r["tool"] == "app_echo", "the answer was not found past the lines that are not it")
check(r["echo"] == {"k": "v"}, "arguments did not arrive")
c.close()

# -- calls from several threads are serialized ---------------------------
c = fake("ok")
answers = [None] * 8


def one(n):
    try:
        answers[n] = c.call_tool("slow", {"n": n})["structuredContent"]["echo"]["n"]
    except Exception as e:  # kept, so a failure is a check and not a crash
        answers[n] = type(e).__name__


start = time.monotonic()
threads = [threading.Thread(target=one, args=(i,)) for i in range(8)]
for t in threads:
    t.start()
for t in threads:
    t.join(20)
check(answers == list(range(8)), f"8 threads did not each get their own answer: {answers}")
check(time.monotonic() - start >= 8 * 0.3 - 0.05, "8 slow calls did not run one at a time")
c.close()

# -- NaN is refused, not sent bare ----------------------------------------
d = connect(binary=FAKE)
e = raises(InvalidArgumentError, lambda: d.set_location(float("nan"), 0))
check(e is not None, "set_location(nan) was sent: mobium cannot parse it and answers with no id")
check(d._conn.call_tool("app_current")["structuredContent"]["tool"] == "app_current",
      "the connection broke after refusing NaN")
d.close()

# -- an answer with no id fails the call in flight -----------------------
c = fake("noid", timeout=5)
e = raises(InvalidArgumentError, lambda: c.call_tool("app_map"))
check(e is not None and "could not read the request" in str(e), f"an answer with no id did not fail the call: {e!r}")
check(c.call_tool("app_current")["structuredContent"]["tool"] == "app_current", "the connection fell out of step")
c.close()

# -- a timed-out call ends the connection --------------------------------
check(raises(ValueError, lambda: Connection(FAKE, [], 0)) is not None, "call_timeout=0 was accepted")
c = fake("hang", timeout=0.5)
start = time.monotonic()
e = raises(MobiumError, lambda: c.call_tool("app_map"))
check(e is not None and time.monotonic() - start < 5, "a call with no answer did not time out")
check(e is not None and "call_timeout" in str(e), f"the timeout does not say what to do: {e}")
e = raises(MobiumError, lambda: c.call_tool("app_map"))
check(e is not None and "no longer usable" in str(e), f"the next call was not refused: {e}")
c.close()

# -- an exit mid-call ------------------------------------------------------
c = fake("exit")
e = raises(MobiumError, lambda: c.call_tool("app_map"))
check(e is not None and "status 3" in str(e), f"an exit mid-call does not name its status: {e}")
e = raises(MobiumError, lambda: c.call_tool("app_map"))
check(e is not None and "no longer usable" in str(e), f"the connection did not stay closed: {e}")
c.close()

# -- a failed handshake leaves no process --------------------------------
start = time.monotonic()
e = raises(MobiumError, lambda: fake("mute", timeout=0.5))
check(e is not None and time.monotonic() - start < 5, "a silent handshake did not fail within call_timeout")
# Refused rather than silent: a timeout kills the process on its own, so
# only a handshake that fails some other way shows whether the constructor
# cleans up after itself. Measured before the fix: it did not.
with tempfile.TemporaryDirectory() as tmp:
    pidfile = os.path.join(tmp, "pid")
    e = raises(MobiumError, lambda: fake("refuse", pidfile=pidfile))
    check(e is not None, "a refused handshake did not fail")
    pid = int(pathlib.Path(pidfile).read_text())
    gone = False
    for _ in range(50):
        try:
            os.kill(pid, 0)
        except (ProcessLookupError, OSError):
            gone = True
            break
        time.sleep(0.1)
    check(gone, f"a refused handshake left mobium running (pid {pid})")

# -- close is idempotent and ends a waiting call -------------------------
c = fake("hang")
seen = []
t = threading.Thread(target=lambda: seen.append(raises(MobiumError, lambda: c.call_tool("app_map"))))
t.start()
time.sleep(0.3)
c.close()
t.join(15)
check(not t.is_alive() and seen and seen[0] is not None, "close from another thread did not end a waiting call")
c.close()
e = raises(MobiumError, lambda: c.call_tool("app_map"))
check(e is not None and "closed" in str(e), f"a call after close does not say it was closed: {e}")

os.remove(FAKE)
if failures:
    print("\n".join("FAIL: " + f for f in failures))
    sys.exit(1)
print(f"python connection: {checks} checks passed")
