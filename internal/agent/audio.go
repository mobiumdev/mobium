package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/audio"
	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// audioCapture is app_audio: start capturing what the device plays, stop and
// save it as a WAV with a timeline of what it holds, or say whether a capture
// is running.
//
// The timeline is the answer, not the file: when there was sound and when
// silence, and each sound's pitch. The platform's own word is no substitute
// — Android reports a player playing zeros exactly as it reports a tone, and
// the Audio Demo's silence is there to show it. And the timeline holds
// whatever the device played, the system's too: a tap with touch sounds on
// is a tenth of a second of sound near 780 Hz, which is reported where it
// fell rather than filtered out.
func (h *Handlers) audioCapture(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.audioOn(ctx, s, args)
}

// audioOn is app_audio once the device is resolved.
func (h *Handlers) audioOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	view := AudioView{Device: s.dev.Serial}
	switch action := stringArg(args, "action"); action {
	case "", "status":
		if s.audio == nil {
			return Result("not capturing audio", view), nil
		}
		view.Recording = true
		view.Elapsed = time.Since(s.audio.Started()).Round(time.Millisecond)
		return Result(fmt.Sprintf("capturing audio for %s", view.Elapsed), view), nil

	case "start":
		if s.audio != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "already capturing audio, for %s — stop it with "+
				"action \"stop\" and a path first", time.Since(s.audio.Started()).Round(time.Second))
		}
		rec, ok := mobiumdriver.AsAudioRecorder(s.driver)
		if !ok {
			return nil, cannot(s, mobiumdriver.CapAudio, "capture audio")
		}
		r, err := rec.StartAudio(ctx)
		if err != nil {
			return nil, err
		}
		s.audio = r
		view.Recording = true
		return Result("capturing audio — stop it with action \"stop\" and a path", view), nil

	case "stop":
		if s.audio == nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "no audio is being captured — start with action \"start\"")
		}
		path := stringArg(args, "path")
		returnData := path == "" && boolArg(args, "return_data")
		if path == "" && !returnData {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "stop needs a path to save the audio to, "+
				"e.g. \"capture.wav\"")
		}
		// Read before the capture stops: a mistake here must not cost it.
		want, expecting, err := audioExpectation(args)
		if err != nil {
			return nil, err
		}
		ignore := audio.DefaultIgnore
		if ms, ok := args["ignore_ms"].(float64); ok {
			if ms < 0 {
				return nil, mobiumerr.New(mobiumerr.InvalidArgument, "ignore_ms must not be negative")
			}
			ignore = time.Duration(ms) * time.Millisecond
		}
		if returnData {
			dir, err := os.MkdirTemp("", "mobium-audio-")
			if err != nil {
				return nil, err
			}
			defer func() { _ = os.RemoveAll(dir) }()
			path = filepath.Join(dir, "capture.wav")
		}
		r := s.audio
		s.audio = nil
		view.Elapsed = time.Since(r.Started()).Round(time.Millisecond)
		got, err := r.Stop(ctx)
		if err != nil {
			return nil, err
		}
		samples := got.Samples
		view.Volumes = got.VolumesAtEnd
		if !sameVolumes(got.VolumesAtStart, got.VolumesAtEnd) {
			view.VolumesAtStart = got.VolumesAtStart
		}
		n, err := audio.WriteWAV(path, samples, device.AudioRate)
		if err != nil {
			return nil, fmt.Errorf("save the audio: %w", err)
		}
		view.Path, view.Bytes = path, n
		view.Duration = (time.Duration(len(samples)) * time.Second / device.AudioRate).Round(time.Millisecond)
		view.Timeline = audio.Analyze(samples, device.AudioRate)
		var miss string
		if expecting {
			miss = audio.Check(view.Timeline, want, ignore)
		}
		if returnData {
			wav, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read the audio back: %w", err)
			}
			view.Path, view.Data = "", base64.StdEncoding.EncodeToString(wav)
			if miss != "" {
				// The capture is the evidence: it goes back with the
				// failure, for the CLI or pipe to save where it was asked.
				return nil, audioMissed(miss, view, "").WithDetail("data", view.Data)
			}
			return Result("captured "+AudioHeard(view), view), nil
		}
		if miss != "" {
			return nil, audioMissed(miss, view, " — the capture is saved at "+path).WithDetail("path", path)
		}
		return Result(AudioSavedMessage(path, view), view), nil

	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "action must be \"start\", \"stop\" or omitted, got %q", action)
	}
}

// audioMissed is the failure of a stop whose capture did not hold what was
// expected: what was heard, and the volume it was heard at, which at its
// lowest is why an app was silent.
func audioMissed(miss string, v AudioView, saved string) *mobiumerr.Error {
	return mobiumerr.New(mobiumerr.NotConfirmed, "%s%s%s", miss, volumeNote(v), saved).
		WithDetail("timeline", v.Timeline).WithDetail("volumes", v.Volumes)
}

// audioExpectation reads stop's expect: the sounds a capture must hold, in
// order. Present and empty is silence; absent is no expectation.
func audioExpectation(args map[string]interface{}) ([]audio.Expected, bool, error) {
	raw, ok := args["expect"]
	if !ok || raw == nil {
		return nil, false, nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil, false, mobiumerr.New(mobiumerr.InvalidArgument, "expect is a list of sounds, each "+
			"{\"hz\": 440} with optional min_ms and max_ms; [] is silence")
	}
	want := make([]audio.Expected, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]interface{})
		hz, hasHz := m["hz"].(float64)
		if !ok || !hasHz || hz < 0 {
			return nil, false, mobiumerr.New(mobiumerr.InvalidArgument, "expect[%d] needs hz, a pitch, or 0 for "+
				"a sound with no one pitch: {\"hz\": 440}", i)
		}
		e := audio.Expected{Hz: hz}
		for k, dst := range map[string]*int{"min_ms": &e.MinMs, "max_ms": &e.MaxMs} {
			if v, ok := m[k]; ok {
				n, ok := v.(float64)
				if !ok || n < 0 {
					return nil, false, mobiumerr.New(mobiumerr.InvalidArgument, "expect[%d].%s must be a "+
						"number of milliseconds", i, k)
				}
				*dst = int(n)
			}
		}
		for k := range m {
			if k != "hz" && k != "min_ms" && k != "max_ms" {
				return nil, false, mobiumerr.New(mobiumerr.InvalidArgument, "expect[%d] has %q; a sound "+
					"takes hz, min_ms and max_ms", i, k)
			}
		}
		if e.MaxMs > 0 && e.MinMs > e.MaxMs {
			return nil, false, mobiumerr.New(mobiumerr.InvalidArgument, "expect[%d]: min_ms is more than max_ms", i)
		}
		want = append(want, e)
	}
	return want, true, nil
}

// AudioSavedMessage is what a stop says once the audio is on disk — the
// daemon's when it saved it, and the CLI's or pipe's when it came back.
func AudioSavedMessage(path string, v AudioView) string {
	return fmt.Sprintf("saved %s: %s", path, AudioHeard(v))
}

// AudioHeard says what a capture holds, segment by segment.
func AudioHeard(v AudioView) string {
	head := fmt.Sprintf("%s of audio", v.Duration)
	var sound bool
	var parts []string
	for _, seg := range v.Timeline {
		span := fmt.Sprintf("%.1f–%.1fs", seg.From.Seconds(), seg.To.Seconds())
		switch {
		case !seg.Sound:
			parts = append(parts, span+" silence")
		case seg.Hz > 0:
			sound = true
			parts = append(parts, fmt.Sprintf("%s %.0f Hz (%.0f dBFS)", span, seg.Hz, seg.Level))
		default:
			sound = true
			parts = append(parts, fmt.Sprintf("%s sound, no one pitch (%.0f dBFS)", span, seg.Level))
		}
	}
	if !sound {
		return fmt.Sprintf("%s, silence throughout — nothing reached %.0f dBFS%s", head, audio.SoundFloor, volumeNote(v))
	}
	return head + ": " + strings.Join(parts, ", ") + volumeNote(v)
}

// volumeNote says the media volume a capture ended at, and what it started
// at if that was different: what arrives follows it, and at its lowest a
// playing app is heard as silence.
func volumeNote(v AudioView) string {
	media := func(vols []device.StreamVolume) (device.StreamVolume, bool) {
		for _, s := range vols {
			if s.Stream == "media" {
				return s, true
			}
		}
		return device.StreamVolume{}, false
	}
	end, ok := media(v.Volumes)
	if !ok {
		return ""
	}
	say := func(s device.StreamVolume) string {
		out := fmt.Sprintf("%d of %d", s.Index, s.Max)
		if s.Muted {
			out += ", muted"
		}
		return out
	}
	note := " — at media volume " + say(end)
	if start, ok := media(v.VolumesAtStart); ok {
		note += " (it was " + say(start) + " when the capture started)"
	}
	if end.Muted || end.Index <= end.Min {
		note += ", where nothing played as media is heard"
	}
	return note
}

func sameVolumes(a, b []device.StreamVolume) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// AudioView is the result of app_audio.
type AudioView struct {
	Recording bool          `json:"recording"`
	Elapsed   time.Duration `json:"elapsed,omitempty"`
	Path      string        `json:"path,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	Bytes     int64         `json:"bytes,omitempty"`
	// Timeline is what the capture holds: sound and silence, each sound's
	// pitch and level.
	Timeline []audio.Segment `json:"timeline,omitempty"`
	// Volumes are the device's media and alarm volumes as the capture
	// ended, and VolumesAtStart as it began, when they were different.
	Volumes        []device.StreamVolume `json:"volumes,omitempty"`
	VolumesAtStart []device.StreamVolume `json:"volumesAtStart,omitempty"`
	// Data is the WAV, base64, when it was returned rather than saved.
	Data   string `json:"data,omitempty"`
	Device string `json:"device"`
}
