package device

import (
	"encoding/binary"
	"os"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// MP4Info is what a recording's own header says about it.
type MP4Info struct {
	Duration time.Duration `json:"duration"`
	Frames   int           `json:"frames"`
	Bytes    int64         `json:"bytes"`
}

// ProbeMP4 reads an MP4's duration and frame count from its header, so a
// recording is judged by what it holds, not by a file existing. Nothing but
// the standard library: the single-binary rule rules out ffprobe.
//
// A recorder stopped with SIGKILL leaves no moov box — measured, Android's
// screenrecord wrote 3,232 bytes of ftyp, free and mdat and nothing to play
// them with — so a missing header is an error, not an empty recording.
func ProbeMP4(path string) (MP4Info, error) {
	var info MP4Info
	data, err := os.ReadFile(path)
	if err != nil {
		return info, err
	}
	info.Bytes = int64(len(data))
	mvhd := findBox(data, "moov", "mvhd")
	if mvhd == nil {
		return info, mobiumerr.New(mobiumerr.NotConfirmed, "the recording at %s has no header, so nothing can play "+
			"it — the recorder was stopped without being allowed to finish", path)
	}
	if len(mvhd) >= 20 && mvhd[0] == 0 {
		scale, dur := binary.BigEndian.Uint32(mvhd[12:]), binary.BigEndian.Uint32(mvhd[16:])
		if scale > 0 {
			info.Duration = time.Duration(float64(dur) / float64(scale) * float64(time.Second))
		}
	} else if len(mvhd) >= 32 {
		scale, dur := binary.BigEndian.Uint32(mvhd[20:]), binary.BigEndian.Uint64(mvhd[24:])
		if scale > 0 {
			info.Duration = time.Duration(float64(dur) / float64(scale) * float64(time.Second))
		}
	}
	if stsz := findBox(data, "moov", "trak", "mdia", "minf", "stbl", "stsz"); len(stsz) >= 12 {
		info.Frames = int(binary.BigEndian.Uint32(stsz[8:]))
	}
	return info, nil
}

// findBox walks nested boxes by type and returns the last one's payload.
func findBox(data []byte, path ...string) []byte {
	for off := 0; off+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[off:]))
		typ := string(data[off+4 : off+8])
		hdr := 8
		if size == 1 && off+16 <= len(data) {
			size, hdr = int(binary.BigEndian.Uint64(data[off+8:])), 16
		} else if size == 0 {
			size = len(data) - off
		}
		if size < hdr || off+size > len(data) {
			return nil
		}
		if typ == path[0] {
			payload := data[off+hdr : off+size]
			if len(path) == 1 {
				return payload
			}
			return findBox(payload, path[1:]...)
		}
		off += size
	}
	return nil
}
