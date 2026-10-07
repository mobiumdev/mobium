# audio-runs.py heard.wav: runs of sound and silence in a float32 WAV from the
# iOS audio probes, by 100ms window: level in dBFS and pitch by zero
# crossings of the first channel. Rough on purpose: a window straddling a
# tone's edge reads as a stray pitch. Mobium's own analysis is not this.
import math, struct, sys
d = open(sys.argv[1], "rb").read()
ch, rate = struct.unpack("<HI", d[22:28])
n = struct.unpack("<I", d[40:44])[0] // 4
x = struct.unpack("<%df" % n, d[44:44 + 4 * n])[::ch]
w = rate // 10
runs = []
for i in range(0, len(x) - w + 1, w):
    s = x[i:i + w]
    rms = math.sqrt(sum(v * v for v in s) / w) or 1e-12
    db = 20 * math.log10(rms)
    zc = sum(1 for a, b in zip(s, s[1:]) if (a < 0) != (b < 0))
    hz = round(zc / 2 / 0.1 / 10) * 10
    label = f"{hz} Hz" if db > -60 else "silence"
    if runs and runs[-1][2] == label:
        runs[-1][1] = (i + w) / rate; runs[-1][3] = max(runs[-1][3], db)
    else:
        runs.append([i / rate, (i + w) / rate, label, db])
print(f"{len(x)/rate:.2f} s at {rate} Hz, {ch} ch")
for a, b, l, db in runs:
    print(f"  {a:5.1f}-{b:5.1f} s  {l:10s} peak window {db:6.1f} dBFS")
