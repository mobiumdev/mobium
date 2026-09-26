package device

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Two real recordings of a still screen, one from each recorder: Android's
// screenrecord wrote two frames and a quarter-second, the simulator's wrote
// one frame and called it 3.4 seconds. Both are right, and both are read.
func TestProbeMP4ReadsBothRecorders(t *testing.T) {
	for _, c := range []struct {
		file       string
		frames     int
		minD, maxD time.Duration
	}{
		{"testdata/record-android-still.mp4", 2, 0, time.Second},
		{"testdata/record-simulator-still.mp4", 1, 3 * time.Second, 4 * time.Second},
	} {
		info, err := ProbeMP4(c.file)
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
			continue
		}
		if info.Frames != c.frames || info.Duration < c.minD || info.Duration > c.maxD || info.Bytes == 0 {
			t.Errorf("%s: %+v", c.file, info)
		}
	}
}

// A recorder killed with SIGKILL writes its data and never its header —
// measured, Android's left ftyp, free and mdat and no moov. That must be an
// error, never an empty recording reported as saved.
func TestProbeMP4RefusesAFileWithNoHeader(t *testing.T) {
	raw, err := os.ReadFile("testdata/record-android-still.mp4")
	if err != nil {
		t.Fatal(err)
	}
	// ftyp only, as the killed recorder left it: the first box, then data.
	ftyp := int(raw[3]) | int(raw[2])<<8 | int(raw[1])<<16 | int(raw[0])<<24
	headless := append(append([]byte{}, raw[:ftyp]...), []byte{0, 0, 0, 16, 'm', 'd', 'a', 't', 1, 2, 3, 4, 5, 6, 7, 8}...)
	path := filepath.Join(t.TempDir(), "killed.mp4")
	if err := os.WriteFile(path, headless, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbeMP4(path); err == nil {
		t.Error("a recording with no header was read as fine")
	}
}
