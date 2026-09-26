#!/usr/bin/env python3
# pixel.py <png> <x-fraction> <y-fraction>: print the luminance (0-255) of one
# pixel of an 8-bit RGB or RGBA PNG, with the standard library only.
#
# For checks that judge what is on screen by its pixels rather than by what
# the app says about itself — mobium-app.sh's dark mode. Both platforms'
# screenshots are 8-bit RGBA PNGs.
import struct, sys, zlib
data = open(sys.argv[1], 'rb').read()
assert data[:8] == b'\x89PNG\r\n\x1a\n', 'not a PNG'
pos, idat, w, h, ctype = 8, b'', 0, 0, 0
while pos < len(data):
    n, kind = struct.unpack('>I4s', data[pos:pos + 8])
    body = data[pos + 8:pos + 8 + n]
    if kind == b'IHDR':
        w, h, depth, ctype = struct.unpack('>IIBB', body[:10])
        assert depth == 8 and ctype in (2, 6), 'only 8-bit RGB or RGBA'
    elif kind == b'IDAT':
        idat += body
    pos += 12 + n
bpp = 4 if ctype == 6 else 3
raw, stride = zlib.decompress(idat), w * bpp
x, y = int(float(sys.argv[2]) * (w - 1)), int(float(sys.argv[3]) * (h - 1))
prev = bytearray(stride)
for row in range(y + 1):
    f = raw[row * (stride + 1)]
    line = bytearray(raw[row * (stride + 1) + 1:(row + 1) * (stride + 1)])
    for i in range(stride):
        a = line[i - bpp] if i >= bpp else 0
        b = prev[i]
        c = prev[i - bpp] if i >= bpp else 0
        if f == 1: line[i] = (line[i] + a) & 255
        elif f == 2: line[i] = (line[i] + b) & 255
        elif f == 3: line[i] = (line[i] + (a + b) // 2) & 255
        elif f == 4:
            p = a + b - c; pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
            line[i] = (line[i] + (a if pa <= pb and pa <= pc else b if pb <= pc else c)) & 255
    prev = line
r, g, bl = prev[x * bpp:x * bpp + 3]
print(round(0.299 * r + 0.587 * g + 0.114 * bl))
