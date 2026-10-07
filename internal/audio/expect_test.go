package audio

import (
	"strings"
	"testing"
	"time"
)

func seg(from, to float64, sound bool, hz float64) Segment {
	return Segment{From: time.Duration(from * float64(time.Second)), To: time.Duration(to * float64(time.Second)), Sound: sound, Hz: hz}
}

// The Audio Demo's sequence as the emulator heard it, the tap's click first.
var sequence = []Segment{
	seg(0, 0.3, false, 0), seg(0.3, 0.4, true, 0), seg(0.4, 2.4, true, 440), seg(2.4, 3.4, false, 0),
	seg(3.4, 5.4, true, 880), seg(5.4, 6.6, false, 0),
}

func TestCheck(t *testing.T) {
	for _, c := range []struct {
		name string
		want []Expected
		ok   bool
		says string
	}{
		{"the sequence", []Expected{{Hz: 440}, {Hz: 880}}, true, ""},
		{"with lengths", []Expected{{Hz: 441, MinMs: 1800, MaxMs: 2200}, {Hz: 880, MinMs: 1800}}, true, ""},
		{"the wrong pitch", []Expected{{Hz: 440}, {Hz: 660}}, false, "expected 440 Hz, then 660 Hz; heard 440 Hz for 2.0s (0.4–2.4s), then 880 Hz"},
		{"too short", []Expected{{Hz: 440, MinMs: 3000}, {Hz: 880}}, false, "for at least 3.0s"},
		{"too long", []Expected{{Hz: 440, MaxMs: 1000}, {Hz: 880}}, false, "for at most 1.0s"},
		{"one missing", []Expected{{Hz: 440}}, false, "then 880 Hz"},
		{"silence wanted", []Expected{}, false, "expected silence; heard 440 Hz"},
	} {
		got := Check(sequence, c.want, DefaultIgnore)
		if (got == "") != c.ok || !strings.Contains(got, c.says) {
			t.Errorf("%s: %q", c.name, got)
		}
	}
	// The click alone is silence; with nothing ignored it is a sound.
	click := []Segment{seg(0, 0.3, false, 0), seg(0.3, 0.4, true, 0), seg(0.4, 2, false, 0)}
	if got := Check(click, nil, DefaultIgnore); got != "" {
		t.Errorf("a click is not silence: %q", got)
	}
	if got := Check(click, nil, 0); !strings.Contains(got, "a sound with no one pitch") {
		t.Errorf("ignoring nothing: %q", got)
	}
}
