package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// record is app_record: start recording the screen, stop and save it, or
// say whether a recording is running.
//
// A recording is judged by what the file holds — its duration and frame
// count, read from its header — not by a file existing. The two recorders
// count differently and both are right: Android writes a frame only when the
// screen changes, so four still seconds are one frame of 0.00s; the
// simulator's recorder also writes one frame, and calls it 3.92s. So the
// wall time is reported beside them, and a recording of a still screen is
// not an error.
func (h *Handlers) record(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.recordOn(ctx, s, args)
}

// recordOn is app_record once the device is resolved.
func (h *Handlers) recordOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	view := RecordView{Device: s.dev.Serial}
	switch action := stringArg(args, "action"); action {
	case "", "status":
		if s.recording == nil {
			return Result("not recording", view), nil
		}
		view.Recording = true
		view.Elapsed = time.Since(s.recording.Started()).Round(time.Millisecond)
		return Result(fmt.Sprintf("recording for %s", view.Elapsed), view), nil

	case "start":
		if s.recording != nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "already recording, for %s — stop it with "+
				"action \"stop\" and a path first", time.Since(s.recording.Started()).Round(time.Second))
		}
		rec, ok := mobiumdriver.AsScreenRecorder(s.driver)
		if !ok {
			return nil, cannot(s, mobiumdriver.CapRecording, "record the screen")
		}
		r, err := rec.StartRecording(ctx)
		if err != nil {
			return nil, err
		}
		s.recording = r
		view.Recording = true
		return Result("recording — stop it with action \"stop\" and a path", view), nil

	case "stop":
		if s.recording == nil {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "nothing is recording — start with action \"start\"")
		}
		path := stringArg(args, "path")
		if path == "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "stop needs a path to save the video to, "+
				"e.g. \"session.mp4\"")
		}
		r := s.recording
		s.recording = nil
		view.Elapsed = time.Since(r.Started()).Round(time.Millisecond)
		info, err := r.Stop(ctx, path)
		if err != nil {
			return nil, err
		}
		view.Path, view.Duration, view.Frames, view.Bytes = path, info.Duration.Round(time.Millisecond), info.Frames, info.Bytes
		frames := fmt.Sprintf("%d frames", info.Frames)
		if info.Frames == 1 {
			frames = "1 frame"
		}
		msg := fmt.Sprintf("saved %s: %s, %s of video, recorded over %s", path, frames, view.Duration, view.Elapsed)
		if info.Frames <= 1 {
			// Said so the caller does not take it for a failed recording.
			msg += " — one frame means the screen did not change while recording"
		}
		return Result(msg, view), nil

	default:
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "action must be \"start\", \"stop\" or omitted, got %q", action)
	}
}

// RecordView is the result of app_record.
type RecordView struct {
	Recording bool          `json:"recording"`
	Elapsed   time.Duration `json:"elapsed,omitempty"`
	Path      string        `json:"path,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	Frames    int           `json:"frames,omitempty"`
	Bytes     int64         `json:"bytes,omitempty"`
	Device    string        `json:"device"`
}
