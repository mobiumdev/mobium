package audio

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Expected is one sound a capture should hold: a pitch, 0 for a sound with
// no one pitch, and optionally how long it lasts.
type Expected struct {
	Hz    float64 `json:"hz"`
	MinMs int     `json:"min_ms,omitempty"`
	MaxMs int     `json:"max_ms,omitempty"`
}

// DefaultIgnore is how short a sound may be and not count against an
// expectation: a tap's own click, with touch sounds on, is a tenth of a
// second.
const DefaultIgnore = 200 * time.Millisecond

// Sounds are the sounds that count against an expectation, in order: each
// of the timeline's sounds longer than ignore, and each run of shorter ones
// that together last longer than it — across silences no longer than a
// Window. The timeline splits a sound wherever its pitch moves, and dropping
// each short piece let a sweep, a melody of quarter-second notes, a wide
// vibrato and a train of 80ms beeps all pass `expect` silence. A run keeps a
// pitch when its pieces are all within 5% of one — under a semitone, so two
// notes are never one — and otherwise has none. CHALLENGES 287.
func Sounds(timeline []Segment, ignore time.Duration) []Segment {
	var out, run []Segment
	flush := func() {
		for len(run) > 0 && !run[len(run)-1].Sound {
			run = run[:len(run)-1]
		}
		if len(run) > 0 && run[len(run)-1].To-run[0].From > ignore {
			out = append(out, joinRun(run))
		}
		run = nil
	}
	for _, s := range timeline {
		switch {
		case s.Sound && s.To-s.From > ignore:
			flush()
			out = append(out, s)
		case s.Sound:
			run = append(run, s)
		case len(run) > 0 && s.To-s.From <= Window:
			run = append(run, s)
		default:
			flush()
		}
	}
	flush()
	return out
}

// joinRun is one sound made of short pieces: from the first to the last,
// at their mean power, with their pitch if they share one.
func joinRun(run []Segment) Segment {
	seg := Segment{From: run[0].From, To: run[len(run)-1].To, Sound: true}
	var power, sounding float64
	var pitches []float64
	pitched := true
	for _, r := range run {
		if !r.Sound {
			continue
		}
		d := (r.To - r.From).Seconds()
		power += d * math.Pow(10, r.Level/10)
		sounding += d
		if r.Hz == 0 {
			pitched = false
		}
		pitches = append(pitches, r.Hz)
	}
	if sounding > 0 {
		seg.Level = round1(10 * math.Log10(power/sounding))
	}
	if pitched {
		m := median(pitches)
		for _, hz := range pitches {
			if math.Abs(hz-m) > 0.05*m {
				pitched = false
			}
		}
		if pitched {
			seg.Hz = math.Round(m)
		}
	}
	return seg
}

// Check says whether the timeline's sounds longer than ignore are want, in
// order, each the same pitch and, where want says, the right length. An
// empty want is silence. The answer is "" when they are, and otherwise
// what was expected and what was heard.
func Check(timeline []Segment, want []Expected, ignore time.Duration) string {
	got := Sounds(timeline, ignore)
	ok := len(got) == len(want)
	for i := 0; ok && i < len(want); i++ {
		d := got[i].To - got[i].From
		w := want[i]
		ok = SamePitch(got[i].Hz, w.Hz) &&
			(w.MinMs == 0 || d >= time.Duration(w.MinMs)*time.Millisecond) &&
			(w.MaxMs == 0 || d <= time.Duration(w.MaxMs)*time.Millisecond)
	}
	if ok {
		return ""
	}
	return fmt.Sprintf("expected %s; heard %s", describeWant(want), describeGot(got, ignore))
}

func describeWant(want []Expected) string {
	if len(want) == 0 {
		return "silence"
	}
	var parts []string
	for _, w := range want {
		p := pitchName(w.Hz)
		switch {
		case w.MinMs > 0 && w.MaxMs > 0:
			p += fmt.Sprintf(" for %.1f–%.1fs", float64(w.MinMs)/1000, float64(w.MaxMs)/1000)
		case w.MinMs > 0:
			p += fmt.Sprintf(" for at least %.1fs", float64(w.MinMs)/1000)
		case w.MaxMs > 0:
			p += fmt.Sprintf(" for at most %.1fs", float64(w.MaxMs)/1000)
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, ", then ")
}

func describeGot(got []Segment, ignore time.Duration) string {
	if len(got) == 0 {
		return fmt.Sprintf("silence (no sound longer than %.1fs)", ignore.Seconds())
	}
	var parts []string
	for _, g := range got {
		parts = append(parts, fmt.Sprintf("%s for %.1fs (%.1f–%.1fs)", pitchName(g.Hz),
			(g.To-g.From).Seconds(), g.From.Seconds(), g.To.Seconds()))
	}
	return strings.Join(parts, ", then ")
}

func pitchName(hz float64) string {
	if hz == 0 {
		return "a sound with no one pitch"
	}
	return fmt.Sprintf("%.0f Hz", hz)
}
