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

from mobium import InvalidArgumentError, MobiumError, connect, start  # noqa: E402
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


t0 = time.monotonic()
threads = [threading.Thread(target=one, args=(i,)) for i in range(8)]
for t in threads:
    t.start()
for t in threads:
    t.join(20)
check(answers == list(range(8)), f"8 threads did not each get their own answer: {answers}")
check(time.monotonic() - t0 >= 8 * 0.3 - 0.05, "8 slow calls did not run one at a time")
c.close()

# -- map_diff asks for a diff and parses it as map() parses elements --------
d = connect(binary=FAKE)
sent = []
real = d._data
d._data = lambda tool, args=None: sent.append((tool, args)) or real(tool, args)
got = d.map_diff()
check(sent == [("app_map", {"diff": True})], f"map_diff sent {sent}, want app_map with diff true")
check(got["first"] is True and not (got["added"] or got["removed"] or got["changed"]),
      f"the first map_diff did not say there was nothing to compare with: {got}")
d._data = real
got = d.map_diff()
check(got["first"] is False and got["since"] == "2026-09-29T10:00:00Z", f"a later map_diff was reported as first: {got}")
check([(e.ref, e.label, e.locator, e.bounds.center) for e in got["added"]] == [("@e1", "OK", "text=OK", (5, 5))],
      f"added was not parsed as map() parses elements: {got['added']}")
check([e.ref for e in got["removed"]] == ["@e9"], f"removed was not parsed: {got['removed']}")
ch = got["changed"]
check(len(ch) == 1 and ch[0]["before"].checked is False and ch[0]["after"].checked is True
      and ch[0]["what"] == ["checked"], f"changed was not parsed into before, after and what: {ch}")
d.close()

# -- a disabled control says so, and anything else does not ----------------
from mobium._device import _element  # noqa: E402

check(_element({"ref": "@e6", "role": "button", "disabled": True}).disabled is True
      and _element({"ref": "@e7", "role": "button"}).disabled is False, "the disabled state was not read")

# -- NaN is refused, not sent bare ----------------------------------------
d = connect(binary=FAKE)
e = raises(InvalidArgumentError, lambda: d.set_location(float("nan"), 0))
check(e is not None, "set_location(nan) was sent: mobium cannot parse it and answers with no id")
check(d._conn.call_tool("app_current")["structuredContent"]["tool"] == "app_current",
      "the connection broke after refusing NaN")
d.close()

# -- files: only the arguments that are set are sent -----------------------
d = connect(binary=FAKE)
t = d.upload("/tmp/report.pdf")
check(t["echo"] == {"path": "/tmp/report.pdf"}, f"upload sent {t['echo']}, want only the path")
check((t["name"], t["bytes"], t["checked"]) == ("report.pdf", 9, "size"), f"upload did not return the transfer: {t}")
t = d.upload("/tmp/report.pdf", name="in.pdf", app="com.example")
check(t["echo"] == {"path": "/tmp/report.pdf", "name": "in.pdf", "app": "com.example"},
      f"upload sent {t['echo']}, want path, name and app")
got = d.download("report.pdf")
check(got == b"fake\x00file", f"download with no path returned {got!r}, want the file's bytes")
t = d.download("report.pdf", path="/tmp/out.pdf", app="com.example")
check(isinstance(t, dict) and t["path"] == "/tmp/out.pdf", f"download with a path did not return the transfer: {t!r}")
check(t["echo"] == {"name": "report.pdf", "path": "/tmp/out.pdf", "app": "com.example"},
      f"download sent {t['echo']}, want name, path and app")
files = d.downloads()
check([f["name"] for f in files] == ["report.pdf"], f"downloads did not return the folder's files: {files}")
sent = []
real = d._data
d._data = lambda tool, args=None: sent.append((tool, args)) or real(tool, args)
d.downloads()
d.downloads(app="com.example")
d._data = real
check(sent == [("app_download", {}), ("app_download", {"app": "com.example"})],
      f"downloads sent {sent}, want app_download with no name, and app only when given")
d.close()

# -- trace: only the arguments that are set are sent ------------------------
d = connect(binary=FAKE)
t = d.trace_start(name="login flow", screenshots=False)
check(t["tool"] == "app_trace" and t["echo"] == {"action": "start", "name": "login flow", "screenshots": False},
      f"trace start sent {t}, want action, name and screenshots=False, and no maps")
t = d.trace_stop("/tmp/trace.zip")
check(t["echo"] == {"action": "stop", "path": "/tmp/trace.zip"}, f"trace stop sent {t['echo']}, want action and path")
sent = []
real = d._data
d._data = lambda tool, args=None: sent.append((tool, args)) or real(tool, args)
d.trace()
d._data = real
check(sent == [("app_trace", None)], f"trace status sent {sent}, want app_trace with no arguments")
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
t0 = time.monotonic()
e = raises(MobiumError, lambda: c.call_tool("app_map"))
check(e is not None and time.monotonic() - t0 < 5, "a call with no answer did not time out")
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
t0 = time.monotonic()
e = raises(MobiumError, lambda: fake("mute", timeout=0.5))
check(e is not None and time.monotonic() - t0 < 5, "a silent handshake did not fail within call_timeout")
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

# -- start opens the session, quit ends it --------------------------------
os.environ["MOBIUM_FAKE"] = "ok"
d = start(platform="ios", app="com.apple.Preferences", binary=FAKE)
s = d.session
check(s is not None and (s.device, s.platform, s.driver, s.app) == ("fake-device", "ios", "wda", "com.apple.Preferences"),
      f"start did not report the device, platform, driver and app it was given: {s}")
d.quit()
check(raises(Exception, d.quit) is None, "a second quit raised")
check(raises(MobiumError, d.map) is not None, "a call after quit succeeded")
check(connect(binary=FAKE).session is None, "connect reported a session it did not start")

# -- a with block around start() quits on the way out ------------------------
with start(platform="android", binary=FAKE) as d:
    check(d.session is not None and d.session.driver == "uiautomator2", "start in a with block did not open a session")
check(raises(MobiumError, d.map) is not None, "the with block did not quit")
with start(platform="android", binary=FAKE) as d:
    d.quit()
check(True, "a with block after an explicit quit did not raise")

# -- close() says it is leaving on purpose -----------------------------------
# mobium ends the sessions a client started when the client goes away, and a
# crash closes stdin just as close() does; without the detach, close() quits.
log = os.path.join(tempfile.mkdtemp(prefix="mobium-notify-"), "log")
os.environ["MOBIUM_FAKE_NOTIFYLOG"] = log
connect(binary=FAKE).close()
os.environ["MOBIUM_FAKE_NOTIFYLOG"] = ""
sent = pathlib.Path(log).read_text().split() if os.path.exists(log) else []
check(sent == ["session=", "notifications/initialized", "mobium/detach"],
      f"close() sent {sent}, not the handshake's notification and then mobium/detach")

# -- session= gives the connection a daemon of its own ------------------------
log = os.path.join(tempfile.mkdtemp(prefix="mobium-notify-"), "log")
os.environ["MOBIUM_FAKE_NOTIFYLOG"] = log
os.environ["MOBIUM_SESSION"] = "outer"
connect(binary=FAKE, session="run7").close()
os.environ.pop("MOBIUM_SESSION")
os.environ["MOBIUM_FAKE_NOTIFYLOG"] = ""
sent = pathlib.Path(log).read_text().split() if os.path.exists(log) else []
check(sent[:1] == ["session=run7"], f"the pipe saw {sent[:1]}, want MOBIUM_SESSION=run7 over the environment's")

os.remove(FAKE)
if failures:
    print("\n".join("FAIL: " + f for f in failures))
    sys.exit(1)
print(f"python connection: {checks} checks passed")
