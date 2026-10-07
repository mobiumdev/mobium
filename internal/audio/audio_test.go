package audio

import (
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const rate = 48000

// tone is hz for ms at amplitude (full scale is 1); hz 0 is silence.
func tone(hz float64, ms int, amplitude float64) []int16 {
	n := rate * ms / 1000
	out := make([]int16, n)
	for i := range out {
		if hz > 0 {
			out[i] = int16(amplitude * 32767 * math.Sin(2*math.Pi*hz*float64(i)/rate))
		}
	}
	return out
}

func join(parts ...[]int16) []int16 {
	var out []int16
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// The Audio Demo's sequence at the level the emulator delivered it.
func TestAnalyzeSequence(t *testing.T) {
	quiet := 0.00832 // -41.6 dBFS as an RMS, as measured
	got := Analyze(join(tone(0, 900, 0), tone(440, 2000, quiet*math.Sqrt2), tone(0, 1000, 0),
		tone(880, 2000, quiet*math.Sqrt2), tone(0, 1000, 0)), rate)
	want := []struct {
		from, to time.Duration
		sound    bool
		hz       float64
	}{
		{0, 900 * time.Millisecond, false, 0},
		{900 * time.Millisecond, 2900 * time.Millisecond, true, 440},
		{2900 * time.Millisecond, 3900 * time.Millisecond, false, 0},
		{3900 * time.Millisecond, 5900 * time.Millisecond, true, 880},
		{5900 * time.Millisecond, 6900 * time.Millisecond, false, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d segments, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.From != w.from || g.To != w.to || g.Sound != w.sound || g.Hz != w.hz {
			t.Errorf("segment %d = %+v, want %+v", i, g, w)
		}
	}
	if l := got[1].Level; math.Abs(l+41.6) > 0.3 {
		t.Errorf("440 Hz level = %.1f dBFS, want about -41.6", l)
	}
}

// Silence is silence however long, and a quiet floor of a few least
// significant bits is under the floor: the emulator's read -97 to -103.
func TestAnalyzeSilence(t *testing.T) {
	floor := make([]int16, rate*2)
	for i := range floor {
		if i%997 == 0 {
			floor[i] = 1
		}
	}
	got := Analyze(floor, rate)
	if len(got) != 1 || got[0].Sound || got[0].To != 2*time.Second {
		t.Fatalf("got %+v, want one silent segment of 2s", got)
	}
}

// Noise is sound with no pitch: Hz stays 0 rather than naming whatever
// frequency happened to be strongest.
func TestAnalyzeNoiseHasNoPitch(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	noise := make([]int16, rate)
	for i := range noise {
		noise[i] = int16(r.Intn(2000) - 1000)
	}
	got := Analyze(noise, rate)
	if len(got) != 1 || !got[0].Sound || got[0].Hz != 0 {
		t.Fatalf("got %+v, want one sound with no pitch", got)
	}
}

// Pitches a tenth of a window apart are told apart, and close readings of
// one tone are not split.
func TestAnalyzePitches(t *testing.T) {
	for _, hz := range []float64{220, 440, 441, 660, 880, 1000, 3000} {
		got := Analyze(tone(hz, 1000, 0.25), rate)
		if len(got) != 1 || math.Abs(got[0].Hz-hz) > 1 {
			t.Errorf("%v Hz: got %+v", hz, got)
		}
	}
	got := Analyze(join(tone(440, 500, 0.25), tone(660, 500, 0.25)), rate)
	if len(got) != 2 || got[0].Hz != 440 || got[1].Hz != 660 {
		t.Errorf("440 then 660: got %+v", got)
	}
}

func TestAnalyzeEmpty(t *testing.T) {
	if got := Analyze(nil, rate); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestWriteWAV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.wav")
	samples := tone(440, 100, 0.25)
	n, err := WriteWAV(path, samples, rate)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(b)) || len(b) != 44+2*len(samples) {
		t.Fatalf("size %d, file %d bytes, want %d", n, len(b), 44+2*len(samples))
	}
	if string(b[0:4]) != "RIFF" || string(b[8:16]) != "WAVEfmt " || string(b[36:40]) != "data" {
		t.Fatalf("header %q", b[:44])
	}
	if r := binary.LittleEndian.Uint32(b[24:]); r != rate {
		t.Errorf("rate %d", r)
	}
	if s := int16(binary.LittleEndian.Uint16(b[44+2*10:])); s != samples[10] {
		t.Errorf("sample 10 = %d, want %d", s, samples[10])
	}
}
