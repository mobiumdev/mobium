"""The error mapping, with no test framework and no device: run it with
`python3 clients/python/tests/test_errors.py`. make ci runs it."""

import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent.parent))

from mobium import (  # noqa: E402
    MobiumError, NoSuchElementError, TimedOutError, UnsupportedError,
)
from mobium._errors import _BY_CODE, error_from  # noqa: E402

CODES = ["no_device", "device_not_ready", "toolchain_missing", "no_such_element",
         "ambiguous_locator", "element_not_reachable", "no_such_context", "no_such_alert",
         "unsupported", "not_confirmed", "timeout", "invalid_argument", "device_server",
         "internal"]

failures = []


def check(cond, what):
    if not cond:
        failures.append(what)


# Every code has its own exception, and each is a MobiumError.
for code in CODES:
    check(code in _BY_CODE, f"no exception for {code}")
    check(issubclass(_BY_CODE.get(code, object), MobiumError), f"{code} is not a MobiumError")

# A structured failure becomes the specific exception, remedy and details kept.
e = error_from("no element matches text=Go", {
    "code": "no_such_element", "message": "no element matches text=Go",
    "remedy": "run app_map again", "retryable": False, "details": {"locator": "text=Go"}})
check(type(e) is NoSuchElementError, f"got {type(e).__name__}")
check(str(e) == "no element matches text=Go", f"message {str(e)!r}")
check(e.remedy == "run app_map again" and e.details == {"locator": "text=Go"}, "remedy/details lost")
check(isinstance(e, MobiumError), "not catchable as MobiumError")
check(not isinstance(e, UnsupportedError), "matched another code")

# timeout is TimedOutError, and does not shadow the builtin.
t = error_from("timed out", {"code": "timeout", "retryable": True})
check(type(t) is TimedOutError and t.retryable, "timeout not TimedOutError/retryable")
check(not isinstance(t, TimeoutError), "TimedOutError is the builtin TimeoutError")

# An unknown code, and an older daemon that sends no structure, still raise.
u = error_from("new kind", {"code": "something_new"})
check(type(u) is MobiumError and u.code == "something_new", "unknown code mishandled")
o = error_from("plain text", None)
check(type(o) is MobiumError and o.code == "error", "text-only failure mishandled")

# The binary is never looked for in the current directory, in ./bin or
# through a relative PATH entry: anything could have been planted there.
import os  # noqa: E402
import tempfile  # noqa: E402

from mobium._rpc import find_binary  # noqa: E402

saved = (os.getcwd(), os.environ.get("PATH"), os.environ.pop("MOBIUM_BIN_PATH", None))
with tempfile.TemporaryDirectory() as tmp:
    name = "mobium.exe" if sys.platform == "win32" else "mobium"
    for where in (tmp, os.path.join(tmp, "bin")):
        os.makedirs(where, exist_ok=True)
        planted = os.path.join(where, name)
        pathlib.Path(planted).write_text("#!/bin/sh\nexit 99\n")
        os.chmod(planted, 0o755)
    try:
        os.chdir(tmp)
        for path in (".", "bin", "./bin", ""):
            os.environ["PATH"] = path
            try:
                found = find_binary()
                check(False, f"PATH={path!r}: found {found}, a binary in the current directory")
            except MobiumError:
                pass
        # The positive control: the same file, by its absolute directory.
        os.environ["PATH"] = os.path.join(tmp, "bin")
        check(find_binary() == os.path.join(tmp, "bin", name), "an absolute PATH entry was not searched")
    finally:
        os.chdir(saved[0])
        if saved[1] is None:
            os.environ.pop("PATH", None)
        else:
            os.environ["PATH"] = saved[1]
        if saved[2] is not None:
            os.environ["MOBIUM_BIN_PATH"] = saved[2]

if failures:
    print("\n".join("FAIL: " + f for f in failures))
    sys.exit(1)
print(f"python errors: {len(CODES)} codes mapped, all checks passed")
