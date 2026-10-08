package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
)

type heldAudio struct{}

func (heldAudio) Started() time.Time { return time.Now() }
func (heldAudio) Stop(context.Context) (device.AudioCapture, error) {
	return device.AudioCapture{}, nil
}
func (heldAudio) Discard(context.Context) {}
func (heldAudio) Captures() bool          { return true }

type fullAudio struct{ heldAudio }

func (fullAudio) Started() time.Time { return time.Now().Add(-2 * device.MaxAudioCapture) }

// CHALLENGES 282: the daemon's idle timer counted only calls, and shut down
// under a capture that ran longer than it. Recording is what it now asks.
func TestRecordingKeepsTheDaemon(t *testing.T) {
	h := NewHandlers()
	if h.Recording() {
		t.Error("nothing is recording, and Recording said otherwise")
	}
	s := &session{}
	h.sessions["emulator-5554"] = s
	if h.Recording() {
		t.Error("a session recording nothing counted as recording")
	}
	s.audio = heldAudio{}
	if !h.Recording() {
		t.Error("an audio capture in progress did not count")
	}
	s.audio = fullAudio{}
	if h.Recording() {
		t.Error("a capture past its limit kept counting, so one left running would keep the daemon for ever")
	}
	s.audio = nil
	s.trace = &sessionTrace{}
	if !h.Recording() {
		t.Error("a trace in progress did not count")
	}
	s.trace = nil
	h.heldTraces["emulator-5554"] = &sessionTrace{}
	if !h.Recording() {
		t.Error("a trace held for a retired session's replacement did not count")
	}
}

func TestAudioHeardSaysACaptureWasCut(t *testing.T) {
	v := AudioView{Duration: device.MaxAudioCapture, Elapsed: device.MaxAudioCapture + 12*time.Minute}
	if got := AudioHeard(v); !strings.Contains(got, "the first of a capture that ran 1h12m0s") {
		t.Errorf("a capture past its limit: %q", got)
	}
	v = AudioView{Duration: 3 * time.Second, Elapsed: 3*time.Second + 200*time.Millisecond}
	if got := AudioHeard(v); strings.Contains(got, "the first of") {
		t.Errorf("an ordinary capture read as cut: %q", got)
	}
}
