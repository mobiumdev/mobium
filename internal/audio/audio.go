// Package audio says what a stretch of captured sound holds — when there was
// sound and when silence, and the pitch of each sound — and writes it as a
// WAV file. It knows nothing about devices: a backend captures the samples,
// and this reads them.
//
// The answer is a timeline, not a comparison with a recording, because the
// level of what arrives depends on the device's volume: an emulator at its
// default media volume delivered a tone 26 dB under what the app wrote. So
// sound and silence are told apart by a floor far from both — the quiet
// floor measured at -97 to -103 dBFS, a quiet tone at -42 — and a pitch by
// the strongest frequency, which no volume moves.
package audio

import (
	"encoding/binary"
	"math"
	"math/cmplx"
	"os"
	"sort"
	"time"
)

// Window is how much sound each reading covers. A tenth of a second is short
// enough to place a tone's start and end, and long enough to tell 440 Hz
// from 450.
const Window = 100 * time.Millisecond

// SoundFloor is the level, in dBFS, below which a window is silence.
const SoundFloor = -70.0

// tonal is the share of a window's energy its strongest frequency must hold
// to be called that frequency's pitch; below it the sound has no one pitch.
const tonal = 0.5

// Segment is one stretch of the timeline: sound or silence, from From to To
// since the capture began.
type Segment struct {
	From  time.Duration `json:"from"`
	To    time.Duration `json:"to"`
	Sound bool          `json:"sound"`
	// Hz is the sound's pitch, or 0 for silence and for a sound with no one
	// pitch — a click, speech, noise.
	Hz float64 `json:"hz,omitempty"`
	// Level is the sound's loudness in dBFS: 0 is full scale. Silence has
	// none.
	Level float64 `json:"level,omitempty"`
}

// window is one Window's reading.
type window struct {
	level float64 // dBFS
	hz    float64 // 0 when silent or not tonal
}

// Analyze reads samples, mono at rate per second, into a timeline.
// Neighboring windows join one segment when both are silent, or both sound
// at the same pitch: within 2% or 5 Hz, whichever is wider, so a tone that
// reads 440 and 441 is one tone.
func Analyze(samples []int16, rate int) []Segment {
	per := rate * int(Window) / int(time.Second)
	if per == 0 || len(samples) == 0 {
		return nil
	}
	var ws []window
	for at := 0; at < len(samples); at += per {
		end := min(at+per, len(samples))
		ws = append(ws, read(samples[at:end], rate))
	}
	var out []Segment
	var levels, pitches []float64
	flush := func() {
		if len(out) == 0 || !out[len(out)-1].Sound {
			return
		}
		last := &out[len(out)-1]
		last.Level = round1(10 * math.Log10(meanPower(levels)))
		if pitches != nil {
			last.Hz = math.Round(median(pitches))
		}
	}
	for i, w := range ws {
		from := time.Duration(i) * Window
		to := min(from+Window, time.Duration(len(samples))*time.Second/time.Duration(rate))
		sound := w.level >= SoundFloor
		if n := len(out); n > 0 && out[n-1].Sound == sound && (!sound || samePitch(out[n-1].Hz, w.hz)) {
			out[n-1].To = to
			if sound {
				levels = append(levels, w.level)
				if w.hz > 0 {
					pitches = append(pitches, w.hz)
				}
				// The pitch so far decides what joins next.
				if pitches != nil {
					out[n-1].Hz = median(pitches)
				}
			}
			continue
		}
		flush()
		levels, pitches = nil, nil
		seg := Segment{From: from, To: to, Sound: sound}
		if sound {
			levels = []float64{w.level}
			if w.hz > 0 {
				pitches = []float64{w.hz}
				seg.Hz = w.hz
			}
		}
		out = append(out, seg)
	}
	flush()
	return out
}

// SamePitch reports whether two readings are one sound: both without a
// pitch, or both within 2% or 5 Hz of each other.
func SamePitch(a, b float64) bool { return samePitch(a, b) }

func samePitch(a, b float64) bool {
	if a == 0 || b == 0 {
		return a == b
	}
	return math.Abs(a-b) <= math.Max(5, 0.02*math.Max(a, b))
}

// read measures one window: its level, and its pitch when one frequency
// holds most of its energy.
func read(x []int16, rate int) window {
	var sum float64
	for _, s := range x {
		v := float64(s) / 32768
		sum += v * v
	}
	power := sum / float64(len(x))
	level := 10 * math.Log10(power+1e-18)
	if level < SoundFloor {
		return window{level: level}
	}
	// The pitch is the sounding part's: a tone that starts or stops inside
	// the window fills only part of it, and weighed over the whole window
	// it is a short burst with no one pitch. Five milliseconds is too short
	// to hold one.
	first, last := -1, -1
	for i, s := range x {
		if s > edge || s < -edge {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 || last-first < rate/200 {
		return window{level: level}
	}
	return window{level: level, hz: pitch(x[first:last+1], rate)}
}

// edge is the amplitude that marks where sound begins inside a window: well
// above the quiet floor's few counts, and below a tone at the sound floor.
const edge = 8

// pitch is the strongest frequency in x, refined between bins, or 0 if no
// frequency holds the tonal share of the energy. A Hann window keeps a tone
// that does not fit the window whole from smearing over its neighbors.
func pitch(x []int16, rate int) float64 {
	n := 1
	for n < len(x) {
		n <<= 1
	}
	// The average offset is no pitch: a one-sided pulse — a tap's click —
	// puts its energy there, and read as the lowest bin it came out as
	// "12 Hz" on the emulator.
	var mean float64
	for _, s := range x {
		mean += float64(s)
	}
	mean /= float64(len(x))
	buf := make([]complex128, n)
	for i, s := range x {
		w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(len(x)-1))
		buf[i] = complex((float64(s)-mean)*w, 0)
	}
	fft(buf)
	half := n / 2
	mag := make([]float64, half)
	var total float64
	best := 1
	for k := 1; k < half; k++ {
		m := cmplx.Abs(buf[k])
		mag[k] = m * m
		total += mag[k]
		if mag[k] > mag[best] {
			best = k
		}
	}
	if total == 0 {
		return 0
	}
	// A Hann window spreads one tone over two bins either side of its peak
	// for every window's length of samples: wider the shorter the sound.
	spread := 2 * n / len(x)
	var near float64
	for k := max(1, best-spread); k <= min(half-1, best+spread); k++ {
		near += mag[k]
	}
	if near/total < tonal {
		return 0
	}
	// Parabolic interpolation on the log magnitude finds the peak between
	// bins: bins are rate/n apart, 11.7 Hz at 48 kHz.
	k := float64(best)
	if best > 1 && best < half-1 {
		a, b, c := math.Log(mag[best-1]+1e-18), math.Log(mag[best]+1e-18), math.Log(mag[best+1]+1e-18)
		if d := a - 2*b + c; d != 0 {
			k += 0.5 * (a - c) / d
		}
	}
	hz := k * float64(rate) / float64(n)
	// A pitch is a repetition: fewer than three of its cycles in the sound
	// is not one, and nothing below 50 Hz is heard as one.
	if hz < minPitch || hz*float64(len(x))/float64(rate) < 3 {
		return 0
	}
	return hz
}

// minPitch is the lowest frequency called a pitch.
const minPitch = 50.0

// fft is an in-place radix-2 transform; len(a) is a power of two.
func fft(a []complex128) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		step := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			w := complex(1, 0)
			for k := 0; k < size/2; k++ {
				u, v := a[start+k], a[start+k+size/2]*w
				a[start+k], a[start+k+size/2] = u+v, u-v
				w *= step
			}
		}
	}
}

func meanPower(levels []float64) float64 {
	var p float64
	for _, l := range levels {
		p += math.Pow(10, l/10)
	}
	return p / float64(len(levels))
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// WriteWAV writes samples, mono 16-bit at rate per second, as a WAV file,
// and returns its size.
func WriteWAV(path string, samples []int16, rate int) (int64, error) {
	data := len(samples) * 2
	b := make([]byte, 0, 44+data)
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(36+data))
	b = append(b, "WAVEfmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1) // PCM
	b = binary.LittleEndian.AppendUint16(b, 1) // mono
	b = binary.LittleEndian.AppendUint32(b, uint32(rate))
	b = binary.LittleEndian.AppendUint32(b, uint32(rate*2))
	b = binary.LittleEndian.AppendUint16(b, 2)
	b = binary.LittleEndian.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(data))
	for _, s := range samples {
		b = binary.LittleEndian.AppendUint16(b, uint16(s))
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return 0, err
	}
	return int64(len(b)), nil
}
