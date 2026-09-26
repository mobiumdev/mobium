package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// fakeRecording records nothing and reports what it is told to.
type fakeRecording struct {
	info      device.MP4Info
	err       error
	stopped   string
	discarded bool
}

func (f *fakeRecording) Started() time.Time { return time.Now().Add(-5 * time.Second) }
func (f *fakeRecording) Stop(ctx context.Context, dest string) (device.MP4Info, error) {
	f.stopped = dest
	return f.info, f.err
}
func (f *fakeRecording) Discard(ctx context.Context) { f.discarded = true }

type recordDriver struct {
	fakeDriver
	rec *fakeRecording
}

func (d *recordDriver) StartRecording(ctx context.Context) (device.Recording, error) {
	return d.rec, nil
}

func withRecorder(t *testing.T, rec *fakeRecording) (func(map[string]interface{}) (*ToolsCallResult, error), *session) {
	t.Helper()
	h := NewHandlers()
	s := &session{dev: fakeDevice(), driver: &recordDriver{rec: rec}, backend: BackendUIA2}
	return func(args map[string]interface{}) (*ToolsCallResult, error) {
		return h.recordOn(context.Background(), s, args)
	}, s
}

func TestRecordStartsStopsAndRefusesOutOfOrder(t *testing.T) {
	rec := &fakeRecording{info: device.MP4Info{Frames: 233, Duration: 6 * time.Second}}
	call, _ := withRecorder(t, rec)
	if _, err := call(map[string]interface{}{"action": "stop", "path": "/tmp/x.mp4"}); mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Errorf("stop with nothing recording: %v", err)
	}
	if _, err := call(map[string]interface{}{"action": "start"}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(map[string]interface{}{"action": "start"}); mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Errorf("a second start: %v", err)
	}
	if _, err := call(map[string]interface{}{"action": "stop"}); mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Errorf("stop with no path: %v", err)
	}
	res, err := call(map[string]interface{}{"action": "stop", "path": "/tmp/x.mp4"})
	if err != nil || rec.stopped != "/tmp/x.mp4" || !strings.Contains(textOf(res), "233 frames") {
		t.Errorf("stop: %v, %q, %q", err, rec.stopped, textOf(res))
	}
}

// One frame is what a still screen records, and is said to be, not reported
// as a broken recording.
func TestAStillRecordingIsExplained(t *testing.T) {
	call, _ := withRecorder(t, &fakeRecording{info: device.MP4Info{Frames: 1}})
	_, _ = call(map[string]interface{}{"action": "start"})
	res, err := call(map[string]interface{}{"action": "stop", "path": "/tmp/x.mp4"})
	if err != nil || !strings.Contains(textOf(res), "1 frame,") || !strings.Contains(textOf(res), "did not change") {
		t.Errorf("%v, %q", err, textOf(res))
	}
}

// Ending a session finishes its recording cleanly and keeps nothing: a
// recorder left running would record a device no session holds.
func TestClosingASessionDiscardsItsRecording(t *testing.T) {
	rec := &fakeRecording{}
	call, s := withRecorder(t, rec)
	_, _ = call(map[string]interface{}{"action": "start"})
	s.close()
	if !rec.discarded || s.recording != nil {
		t.Errorf("discarded = %v, recording = %v", rec.discarded, s.recording)
	}
}
