package audio

import (
	"math"
	"testing"
)

func sweepTone(f0, f1 float64, ms int, amp float64) []int16 {
	n := rate * ms / 1000
	out := make([]int16, n)
	ph := 0.0
	for i := range out {
		ph += 2 * math.Pi * (f0 + (f1-f0)*float64(i)/float64(n)) / rate
		out[i] = int16(amp * 32767 * math.Sin(ph))
	}
	return out
}

func vibratoTone(hz, depth float64, ms int, amp float64) []int16 {
	n := rate * ms / 1000
	out := make([]int16, n)
	ph := 0.0
	for i := range out {
		ph += 2 * math.Pi * (hz + depth*math.Sin(2*math.Pi*6*float64(i)/rate)) / rate
		out[i] = int16(amp * 32767 * math.Sin(ph))
	}
	return out
}

func click() []int16 {
	out := make([]int16, rate/10)
	for i := 0; i < rate/200; i++ {
		out[i] = 3000
	}
	return out
}

// CHALLENGES 287: the timeline splits a sound wherever its pitch moves, and
// each piece of 200ms or less was dropped before the check — so all of
// these passed `expect` silence.
func TestExpectHearsSoundsMadeOfShortPieces(t *testing.T) {
	quiet := tone(0, 500, 0)
	notes := func(ms int, hz ...float64) []int16 {
		var out []int16
		for _, h := range hz {
			out = append(out, tone(h, ms, 0.25)...)
		}
		return out
	}
	beeps := func() []int16 {
		var out []int16
		for i := 0; i < 8; i++ {
			out = append(out, tone(440, 80, 0.25)...)
			out = append(out, tone(0, 120, 0)...)
		}
		return out
	}()
	for _, c := range []struct {
		name string
		x    []int16
		want []Expected // what it should pass as
	}{
		{"a sweep", sweepTone(200, 2000, 2000, 0.25), []Expected{{Hz: 0}}},
		{"a melody of 150ms notes", notes(150, 262, 294, 330, 349, 392, 440, 494, 523, 494, 440, 392, 349, 330), []Expected{{Hz: 0}}},
		{"a melody of 250ms notes", notes(250, 262, 330, 392, 523, 392, 330, 262, 330), []Expected{{Hz: 0}}},
		{"a vibrato of ±20 Hz", vibratoTone(440, 20, 2000, 0.25), []Expected{{Hz: 440}}},
		{"a train of 80ms beeps", beeps, []Expected{{Hz: 440}}},
	} {
		tl := Analyze(join(quiet, c.x, quiet), rate)
		if Check(tl, nil, DefaultIgnore) == "" {
			t.Errorf("%s passed as silence", c.name)
		}
		if miss := Check(tl, c.want, DefaultIgnore); miss != "" {
			t.Errorf("%s: %s", c.name, miss)
		}
	}
}

// What the change must not touch: a lone click is still no sound, nor two
// a second apart; a click just before a tone leaves the tone; two notes
// that each last are still two.
func TestExpectStillIgnoresClicks(t *testing.T) {
	quiet := tone(0, 500, 0)
	for _, c := range []struct {
		name string
		x    []int16
		want []Expected
	}{
		{"a click", join(quiet, click(), quiet), nil},
		{"two clicks a second apart", join(quiet, click(), tone(0, 1000, 0), click(), quiet), nil},
		{"a click, then a tone", join(quiet, click(), tone(0, 100, 0), tone(440, 2000, 0.25), quiet), []Expected{{Hz: 440}}},
		{"440 then 660, a second each", join(quiet, tone(440, 1000, 0.25), tone(660, 1000, 0.25), quiet), []Expected{{Hz: 440}, {Hz: 660}}},
	} {
		if miss := Check(Analyze(c.x, rate), c.want, DefaultIgnore); miss != "" {
			t.Errorf("%s: %s", c.name, miss)
		}
	}
}

// An offset alone is inaudible: it read as -30 dBFS of sound.
func TestAnOffsetIsNoSound(t *testing.T) {
	x := make([]int16, rate*2)
	for i := range x {
		x[i] = 1000
	}
	if tl := Analyze(x, rate); len(tl) != 1 || tl[0].Sound {
		t.Errorf("an offset alone: %+v", tl)
	}
}
