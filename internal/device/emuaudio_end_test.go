package device

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// CHALLENGES 281: a stream that ended by itself — the emulator killed —
// ended with no error, and the stop padded the rest with silence.
func TestEmuAudioEndedBeforeStop(t *testing.T) {
	ended := func(how error) (AudioCapture, error) {
		done := make(chan struct{})
		close(done)
		r := &emuAudio{started: time.Now().Add(-5 * time.Second), cancel: func() {}, done: done,
			samples: make([]int16, AudioRate)}
		r.finish(how)
		return r.Stop(context.Background())
	}
	for _, how := range []error{io.EOF, io.ErrUnexpectedEOF} {
		_, err := ended(how)
		if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "nothing after it was heard") {
			t.Errorf("a stream that ended with %v before the stop: %v", how, err)
		}
	}
	got, err := ended(context.Canceled)
	if err != nil || len(got.Samples) < 4*AudioRate {
		t.Errorf("a stream the stop ended: %v, %d samples", err, len(got.Samples))
	}

	// The stop's own cancel can surface as an EOF; that is no failure.
	done := make(chan struct{})
	r := &emuAudio{started: time.Now().Add(-time.Second), done: done}
	r.cancel = func() { r.finish(io.ErrUnexpectedEOF); close(done) }
	if _, err := r.Stop(context.Background()); err != nil {
		t.Errorf("an EOF from the stop's own cancel: %v", err)
	}
}

// CHALLENGES 282: a capture left running would have been filled in, at the
// stop, with all the silence since it started — 8GB for a day.
func TestEmuAudioKeepsAtMostMaxAudioCapture(t *testing.T) {
	defer func(m time.Duration) { MaxAudioCapture = m }(MaxAudioCapture)
	MaxAudioCapture = 2 * time.Second
	for _, have := range []int{AudioRate, 3 * AudioRate} {
		done := make(chan struct{})
		close(done)
		r := &emuAudio{started: time.Now().Add(-5 * time.Second), cancel: func() {}, done: done,
			samples: make([]int16, have)}
		got, err := r.Stop(context.Background())
		if err != nil || len(got.Samples) != 2*AudioRate {
			t.Errorf("%d samples held, 5s elapsed, a 2s limit: kept %d (%v)", have, len(got.Samples), err)
		}
	}
}
