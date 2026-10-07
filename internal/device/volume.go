package device

import (
	"bufio"
	"bytes"
	"context"
	"strconv"
	"strings"
)

// StreamVolume is one of Android's volume streams as `dumpsys audio` reports
// it: its index on the device's own scale, and whether it is muted. The scale
// is the device's — 0 to 15 for media on the emulator, 0 to 25 on a Pixel 8
// Pro — so an index means nothing without its maximum.
type StreamVolume struct {
	Stream string `json:"stream"`
	Index  int    `json:"index"`
	Min    int    `json:"min"`
	Max    int    `json:"max"`
	Muted  bool   `json:"muted,omitempty"`
}

// audioStreams are the streams an audio capture reports, by what the Audio
// Demo plays through: media, and an alarm.
var audioStreams = map[string]string{"STREAM_MUSIC": "media", "STREAM_ALARM": "alarm"}

// StreamVolumes reads the media and alarm volumes. Read, never set from
// here: `cmd media_session volume --set` prints that it is connecting,
// exits 0 and changes nothing (measured on Android 15); it is `cmd audio
// set-volume` that sets one.
func (a *ADB) StreamVolumes(ctx context.Context) ([]StreamVolume, error) {
	out, err := a.Shell(ctx, "dumpsys", "audio")
	if err != nil {
		return nil, err
	}
	return parseStreamVolumes(out), nil
}

// parseStreamVolumes reads each "- STREAM_X:" block of `dumpsys audio` for
// the streams in audioStreams. A block missing its index or maximum — an
// Android whose dump is laid out otherwise — is left out rather than guessed.
func parseStreamVolumes(out []byte) []StreamVolume {
	var vols []StreamVolume
	var cur *StreamVolume
	var haveIndex, haveMax bool
	done := func() {
		if cur != nil && haveIndex && haveMax {
			vols = append(vols, *cur)
		}
		cur = nil
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "- ") {
			done()
			name, ok := audioStreams[strings.TrimSuffix(strings.TrimPrefix(line, "- "), ":")]
			if ok {
				cur, haveIndex, haveMax = &StreamVolume{Stream: name}, false, false
			}
			continue
		}
		if cur == nil {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "Muted":
			cur.Muted = v == "true"
		case "Min":
			cur.Min, _ = strconv.Atoi(v)
		case "Max":
			n, err := strconv.Atoi(v)
			cur.Max, haveMax = n, err == nil
		case "streamVolume":
			n, err := strconv.Atoi(v)
			cur.Index, haveIndex = n, err == nil
		}
	}
	done()
	return vols
}
