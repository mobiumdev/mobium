# Three of these at once, with two Android emulators on the grid: each asks
# for an Android device, holds it for a few seconds, and says which it got.
import sys, time
from mobium import start

name = sys.argv[1]
t0 = time.time()
with start(platform="android", app="com.android.settings") as device:
    got = time.time() - t0
    serial = device.sessions()[0]["device"]
    print(f"{name}: got {serial} after {got:.1f}s", flush=True)
    rows = len(device.map())
    time.sleep(8)
    print(f"{name}: mapped {rows} elements, releasing after {time.time() - t0:.1f}s", flush=True)
