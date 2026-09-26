"""Mobium — native mobile app automation on virtual devices.

    from mobium import connect

    with connect() as device:
        for element in device.map():
            print(element.ref, element.label)
        device.tap("@e1")
"""

from ._device import Bounds, Device, DeviceInfo, Element, connect
from ._errors import (
    AmbiguousLocatorError,
    DeviceNotReadyError,
    DeviceServerError,
    ElementNotReachableError,
    InternalError,
    InvalidArgumentError,
    MobiumError,
    NoDeviceError,
    NoSuchAlertError,
    NoSuchContextError,
    NoSuchElementError,
    NotConfirmedError,
    TimedOutError,
    ToolchainMissingError,
    UnsupportedError,
)

__all__ = [
    "connect", "Device", "DeviceInfo", "Element", "Bounds",
    "MobiumError", "NoDeviceError", "DeviceNotReadyError", "ToolchainMissingError",
    "NoSuchElementError", "AmbiguousLocatorError", "ElementNotReachableError",
    "NoSuchContextError", "NoSuchAlertError", "UnsupportedError", "NotConfirmedError",
    "TimedOutError", "InvalidArgumentError", "DeviceServerError", "InternalError",
]
__version__ = "0.1.0"
