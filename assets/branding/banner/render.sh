#!/bin/sh
# Rebuilds ../mobium-banner.{mp4,gif,png} from banner.html: headless Chrome
# draws each frame at a time given in the URL's hash (8 s at 30 fps), and
# ffmpeg encodes them. Needs Google Chrome and ffmpeg; nothing else.
#
#   assets/branding/banner/render.sh [chrome]
set -eu
here=$(cd "$(dirname "$0")" && pwd)
chrome=${1:-"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/frames" "$work/profiles"

frame() {
  n=$1
  t=$(echo "scale=4; $n/30" | bc)
  "$chrome" --headless=new --disable-gpu --hide-scrollbars \
    --force-device-scale-factor=1 --window-size=1584,396 \
    --virtual-time-budget=3000 --user-data-dir="$work/profiles/$n" \
    --screenshot="$(printf '%s/frames/f%03d.png' "$work" "$n")" \
    "file://$here/banner.html#$t" 2>/dev/null
}

n=0
while [ $n -lt 240 ]; do
  for k in 0 1 2 3 4 5 6 7; do frame $((n + k)) & done
  wait
  n=$((n + 8))
done
count=$(ls "$work/frames" | wc -l | tr -d ' ')
[ "$count" -eq 240 ] || { echo "rendered $count frames of 240" >&2; exit 1; }

out="$here/.."
ffmpeg -loglevel error -y -framerate 30 -i "$work/frames/f%03d.png" \
  -c:v libx264 -pix_fmt yuv420p -crf 18 -preset slow -movflags +faststart \
  "$out/mobium-banner.mp4"
ffmpeg -loglevel error -y -framerate 30 -i "$work/frames/f%03d.png" \
  -vf "fps=15,scale=1000:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=128:stats_mode=full[p];[b][p]paletteuse=dither=bayer:bayer_scale=3:diff_mode=rectangle" \
  -loop 0 "$out/mobium-banner.gif"
cp "$work/frames/f210.png" "$out/mobium-banner.png"
echo "wrote mobium-banner.mp4, .gif and .png in $out"
