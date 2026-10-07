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

// A tone that starts partway into a window is that tone in it, not a sound
// with no pitch: measured on Android 17, the Audio Demo's 880 began 50ms
// into a window five times in eight, and the sliver read as a separate
// pitchless sound inside what should have been silence. A click is still a
// sound with no pitch.
func TestAnalyzeToneStartingMidWindow(t *testing.T) {
	quiet := 0.00832 * math.Sqrt2
	for _, lead := range []int{10, 30, 50, 70, 90} {
		got := Analyze(join(tone(440, 2000, quiet), tone(0, 1000+lead, 0), tone(880, 2000-lead, quiet), tone(0, 500, 0)), rate)
		var sounds []Segment
		for _, s := range got {
			if s.Sound {
				sounds = append(sounds, s)
			}
		}
		if len(sounds) != 2 || sounds[0].Hz != 440 || math.Abs(sounds[1].Hz-880) > 2 {
			t.Errorf("880 starting %dms into a window: sounds %+v", lead, sounds)
		}
	}
	click := make([]int16, rate/10)
	for i := 0; i < 48; i++ {
		click[rate/20+i] = int16(8000 * (1 - float64(i)/48) * float64(1-2*(i%2)))
	}
	for _, seg := range Analyze(click, rate) {
		if seg.Hz != 0 {
			t.Errorf("a click read with a pitch: %+v", seg)
		}
	}
}

// A click is not a pitch, even one that pushes the samples one way: on the
// Android 15 emulator a tap's click shared a window with nothing else and,
// read over its sounding part only, came out as "12 Hz" — the lowest bin,
// where a one-sided pulse puts its energy. Nothing under 50 Hz is a pitch.
func TestAnalyzeAOneSidedClickHasNoPitch(t *testing.T) {
	x := make([]int16, rate/10)
	for i := 0; i < rate/100; i++ { // 10 ms, decaying, all positive
		x[rate/20+i] = int16(9000 * math.Exp(-float64(i)/80))
	}
	for _, seg := range Analyze(x, rate) {
		if seg.Hz != 0 {
			t.Errorf("a one-sided click read as %v Hz: %+v", seg.Hz, seg)
		}
	}
	// A real low tone above the floor keeps its pitch.
	if got := Analyze(tone(60, 1000, 0.25), rate); len(got) != 1 || math.Abs(got[0].Hz-60) > 2 {
		t.Errorf("60 Hz: %+v", got)
	}
}
