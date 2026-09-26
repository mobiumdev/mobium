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

if failures:
    print("\n".join("FAIL: " + f for f in failures))
    sys.exit(1)
print(f"python errors: {len(CODES)} codes mapped, all checks passed")
