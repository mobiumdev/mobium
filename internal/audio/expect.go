package audio

import (
	"fmt"
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

// Sounds are the timeline's sounds longer than ignore, in order.
func Sounds(timeline []Segment, ignore time.Duration) []Segment {
	var out []Segment
	for _, s := range timeline {
		if s.Sound && s.To-s.From > ignore {
			out = append(out, s)
		}
	}
	return out
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
