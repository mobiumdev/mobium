package agent

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// fakeAudio hands back the samples it is given.
type fakeAudio struct {
	samples    []int16
	start, end []device.StreamVolume
	discarded  bool
}

func (f *fakeAudio) Started() time.Time { return time.Now().Add(-2 * time.Second) }
func (f *fakeAudio) Stop(ctx context.Context) (device.AudioCapture, error) {
	return device.AudioCapture{Samples: f.samples, VolumesAtStart: f.start, VolumesAtEnd: f.end}, nil
}
func (f *fakeAudio) Discard(ctx context.Context) { f.discarded = true }

type audioDriver struct {
	fakeDriver
	rec *fakeAudio
}

func (d *audioDriver) StartAudio(ctx context.Context) (device.AudioRecording, error) {
	return d.rec, nil
}

// tone440 is half a second of silence, a second of 440 Hz, half a second of
// silence: what the Audio Demo's first button plays, give or take the wait.
func tone440() []int16 {
	r := device.AudioRate
	out := make([]int16, 2*r)
	for i := r / 2; i < r*3/2; i++ {
		out[i] = int16(2000 * math.Sin(2*math.Pi*440*float64(i)/float64(r)))
	}
	return out
}

func TestAudioCapturesAndSaysWhatItHeard(t *testing.T) {
	rec := &fakeAudio{samples: tone440()}
	h := NewHandlers()
	s := &session{dev: fakeDevice(), driver: &audioDriver{rec: rec}, backend: BackendUIA2}
	call := func(args map[string]interface{}) (*ToolsCallResult, error) {
		return h.audioOn(context.Background(), s, args)
	}
	if _, err := call(map[string]interface{}{"action": "stop", "path": "x.wav"}); mobiumerr.CodeOf(err) != mobiumerr.InvalidArgument {
		t.Errorf("stop with nothing capturing: %v", err)
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
	path := filepath.Join(t.TempDir(), "capture.wav")
	res, err := call(map[string]interface{}{"action": "stop", "path": path})
	if err != nil {
		t.Fatal(err)
	}
	msg := textOf(res)
	if !strings.Contains(msg, "0.5–1.5s 440 Hz") || !strings.Contains(msg, "0.0–0.5s silence") {
		t.Errorf("message %q", msg)
	}
	v := res.StructuredContent.(AudioView)
	if len(v.Timeline) != 3 || !v.Timeline[1].Sound || v.Timeline[1].Hz != 440 || v.Duration != 2*time.Second {
		t.Errorf("view %+v", v)
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() != v.Bytes {
		t.Errorf("file %v, %v, want %d bytes", fi, err, v.Bytes)
	}
}

// Nothing above the floor is said in so many words, not as an empty list.
func TestAudioSilenceIsSaid(t *testing.T) {
	h := NewHandlers()
	s := &session{dev: fakeDevice(), driver: &audioDriver{rec: &fakeAudio{samples: make([]int16, 2*device.AudioRate)}}, backend: BackendUIA2}
	_, _ = h.audioOn(context.Background(), s, map[string]interface{}{"action": "start"})
	res, err := h.audioOn(context.Background(), s, map[string]interface{}{"action": "stop", "path": filepath.Join(t.TempDir(), "q.wav")})
	if err != nil || !strings.Contains(textOf(res), "silence throughout") {
		t.Errorf("%v, %q", err, textOf(res))
	}
}

// A driver that cannot capture is told apart from one that refused.
func TestAudioWithoutTheCapability(t *testing.T) {
	h := NewHandlers()
	s := &session{dev: fakeDevice(), driver: &fakeDriver{}, backend: BackendUIA2}
	if _, err := h.audioOn(context.Background(), s, map[string]interface{}{"action": "start"}); mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
		t.Errorf("got %v", err)
	}
}

// Ending a session ends its capture: a stream left open would go on holding
// a device no session has.
func TestClosingASessionDiscardsItsAudio(t *testing.T) {
	rec := &fakeAudio{}
	s := &session{dev: fakeDevice(), driver: &audioDriver{rec: rec}, backend: BackendUIA2, audio: rec}
	s.close()
	if !rec.discarded || s.audio != nil {
		t.Errorf("discarded %v, audio %v", rec.discarded, s.audio)
	}
}

// A silent capture says the media volume it was taken at, and that at its
// lowest nothing played as media is heard; a change during the capture is
// said too.
func TestAudioSaysTheVolume(t *testing.T) {
	low := []device.StreamVolume{{Stream: "media", Index: 0, Max: 15}, {Stream: "alarm", Index: 6, Min: 1, Max: 7}}
	mid := []device.StreamVolume{{Stream: "media", Index: 5, Max: 15}, {Stream: "alarm", Index: 6, Min: 1, Max: 7}}
	for _, c := range []struct {
		start, end []device.StreamVolume
		want       []string
		not        []string
	}{
		{low, low, []string{"silence throughout", "media volume 0 of 15", "nothing played as media is heard"}, []string{"when the capture started"}},
		{mid, mid, []string{"media volume 5 of 15"}, []string{"nothing played", "when the capture started"}},
		{mid, low, []string{"media volume 0 of 15 (it was 5 of 15 when the capture started)"}, nil},
		{nil, nil, []string{"silence throughout"}, []string{"media volume"}},
	} {
		h := NewHandlers()
		s := &session{dev: fakeDevice(), driver: &audioDriver{rec: &fakeAudio{samples: make([]int16, device.AudioRate), start: c.start, end: c.end}}, backend: BackendUIA2}
		_, _ = h.audioOn(context.Background(), s, map[string]interface{}{"action": "start"})
		res, err := h.audioOn(context.Background(), s, map[string]interface{}{"action": "stop", "path": filepath.Join(t.TempDir(), "v.wav")})
		if err != nil {
			t.Fatal(err)
		}
		msg := textOf(res)
		for _, w := range c.want {
			if !strings.Contains(msg, w) {
				t.Errorf("%q lacks %q", msg, w)
			}
		}
		for _, n := range c.not {
			if strings.Contains(msg, n) {
				t.Errorf("%q has %q", msg, n)
			}
		}
	}
}
