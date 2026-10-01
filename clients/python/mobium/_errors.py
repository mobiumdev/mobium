"""Mobium's failures, one exception per error code.

Every failure a tool reports carries a stable code — the same in every client
and on the wire (the error codes in the mobium repository's
docs/guides/cli.md). Each code has
an exception here, all subclasses of MobiumError, so a script catches the kind
it can handle and lets the rest through::

    try:
        device.tap("text=Continue")
    except NoSuchElementError:
        device.scroll_to("text=Continue", direction="down")

The message is the same sentence the CLI prints; ``remedy`` repeats what to do
about it, ``retryable`` says whether the same call can succeed if made again,
and ``details`` holds any machine-readable facts (the locator, the W3C code a
device server sent).
"""

from __future__ import annotations

from typing import Any


class MobiumError(RuntimeError):
    """A tool reported that it could not do what was asked.

    Also the exception for a code this client does not recognize, and for a
    daemon too old to send codes at all — ``code`` is then ``"error"``.
    """

    code = "error"

    def __init__(self, message: str, *, code: str | None = None, remedy: str = "",
                 retryable: bool = False, details: dict[str, Any] | None = None) -> None:
        super().__init__(message)
        self.message = message
        if code is not None:
            self.code = code
        self.remedy = remedy
        self.retryable = retryable
        self.details = details or {}


class NoDeviceError(MobiumError):
    """Nothing to drive: no device matches, or none is connected."""
    code = "no_device"


class DeviceNotReadyError(MobiumError):
    """The device is there and cannot be driven yet — locked, not trusted, Developer Mode off."""
    code = "device_not_ready"


class ToolchainMissingError(MobiumError):
    """Something on this machine is missing: adb, Xcode, a signing certificate."""
    code = "toolchain_missing"


class NoSuchElementError(MobiumError):
    """A locator or ref matched nothing on screen. Worth scrolling for."""
    code = "no_such_element"


class AmbiguousLocatorError(MobiumError):
    """A locator matched more than one element. Narrow it; Mobium never guesses."""
    code = "ambiguous_locator"


class ElementNotReachableError(MobiumError):
    """The element was found and cannot be touched where it is — usually off screen."""
    code = "element_not_reachable"


class NoSuchContextError(MobiumError):
    """A WebView context that is not there."""
    code = "no_such_context"


class NoSuchAlertError(MobiumError):
    """A dialog was expected and none is on screen."""
    code = "no_such_alert"


class UnsupportedError(MobiumError):
    """This driver or platform cannot do it, and says why. Retrying cannot help."""
    code = "unsupported"


class NotConfirmedError(MobiumError):
    """The command reported success and reading the state back disagreed."""
    code = "not_confirmed"


class TimedOutError(MobiumError):
    """A wait ran out. Named TimedOut so it does not shadow the builtin TimeoutError."""
    code = "timeout"


class InvalidArgumentError(MobiumError):
    """The request itself is wrong."""
    code = "invalid_argument"


class DeviceServerError(MobiumError):
    """The device side failed: a device server, or adb, simctl, devicectl, lockdown.

    A device server's own W3C code, when it sent one, is in details["w3c"].
    """
    code = "device_server"


class InternalError(MobiumError):
    """A bug in Mobium."""
    code = "internal"


_BY_CODE: dict[str, type[MobiumError]] = {
    cls.code: cls for cls in MobiumError.__subclasses__()
}


def error_from(text: str, structured: Any) -> MobiumError:
    """Build the exception for a failed tool call: the text as always, and the
    code, remedy and details when the daemon sent them."""
    if isinstance(structured, dict) and structured.get("code"):
        code = str(structured["code"])
        cls = _BY_CODE.get(code, MobiumError)
        return cls(text, code=code, remedy=structured.get("remedy") or "",
                   retryable=bool(structured.get("retryable")),
                   details=structured.get("details") or {})
    return MobiumError(text)
