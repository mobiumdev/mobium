package device

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// stubHelper is a helper that says events on stderr, writes the file
// frames on stdout, if any, and waits for its stdin to close, as the real
// one does.
func stubHelper(t *testing.T, events []string, frames []byte) {
	t.Helper()
	// The stub is a shell script, and a simulator's audio is the Mac's alone.
	if runtime.GOOS == "windows" {
		t.Skip("a simulator's audio is captured on macOS; the stub helper is a shell script")
	}
	dir := t.TempDir()
	data := filepath.Join(dir, "frames")
	if err := os.WriteFile(data, frames, 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n"
	for _, e := range events {
		script += "echo '" + e + "' >&2\n"
	}
	script += "cat " + data + "\ncat >/dev/null\n"
	bin := filepath.Join(dir, "helper")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	was := simAudioHelper
	simAudioHelper = func(context.Context) (string, error) { return bin, nil }
	t.Cleanup(func() { simAudioHelper = was })
}

// stereo is n frames of a sine at hz, float32, the same in both channels.
func stereo(n int, hz float64) []byte {
	b := make([]byte, 0, n*8)
	for i := 0; i < n; i++ {
		v := math.Float32bits(float32(0.25 * math.Sin(2*math.Pi*hz*float64(i)/AudioRate)))
		b = binary.LittleEndian.AppendUint32(b, v)
		b = binary.LittleEndian.AppendUint32(b, v)
	}
	return b
}

func capture(t *testing.T) (AudioCapture, error) {
	t.Helper()
	r, err := StartSimulatorAudio(context.Background(), "UDID", "dev.mobium.mobiumapp")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	return r.Stop(context.Background())
}

// What the tap gives — stereo float at the Mac's rate — becomes the mono
// samples every capture gives, padded to the time the capture ran.
func TestASimulatorCaptureIsMonoAtTheAudioRate(t *testing.T) {
	stubHelper(t, []string{`{"event":"tapping","rate":48000,"channels":2,"pid":1}`}, stereo(4800, 440))
	c, err := capture(t)
	if err != nil {
		t.Fatal(err)
	}
	if c.Note != "" {
		t.Errorf("a tapped app has a note: %s", c.Note)
	}
	if len(c.Samples) < AudioRate/4 {
		t.Errorf("%d samples for a capture of 0.3s", len(c.Samples))
	}
	peak := 0
	for _, s := range c.Samples {
		peak = max(peak, int(math.Abs(float64(s))))
	}
	if peak < 8000 || peak > 8300 {
		t.Errorf("a quarter of full scale peaked at %d of 32767", peak)
	}
}

// An app that never opened audio is not a silent app: the capture says so.
func TestAnAppThatNeverPlayedIsSaidSo(t *testing.T) {
	stubHelper(t, []string{`{"event":"waiting"}`}, nil)
	c, err := capture(t)
	if err != nil {
		t.Fatal(err)
	}
	if c.Note != simNeverOpened {
		t.Errorf("note %q", c.Note)
	}
}

// A tap with no audio at all is the permission missing, and that is
// refused naming it, not reported as silence.
func TestATapWithNoAudioNamesThePermission(t *testing.T) {
	stubHelper(t, []string{`{"event":"tapping","rate":48000,"channels":2,"pid":1}`, `{"event":"no-cycles"}`}, nil)
	_, err := capture(t)
	if mobiumerr.CodeOf(err) != mobiumerr.Unsupported || !strings.Contains(err.Error(), "System Audio Recording") {
		t.Errorf("%v", err)
	}
}

// The tap's rate is the Mac's output's, which need not be the capture's.
func TestMonoAtResamplesAnotherRate(t *testing.T) {
	in := make([]byte, 0, 44100*4)
	for i := 0; i < 44100; i++ {
		in = binary.LittleEndian.AppendUint32(in, math.Float32bits(0.5))
	}
	out := monoAt(in, 1, 44100)
	if len(out) != AudioRate {
		t.Errorf("a second at 44.1 kHz became %d samples", len(out))
	}
}

// The helper's source builds, where there is Xcode to build it: it is
// built on a user's Mac the first time a simulator's audio is captured,
// and a source that does not compile would fail there and nowhere else.
func TestTheSimulatorAudioHelperBuilds(t *testing.T) {
	if _, err := exec.LookPath("xcrun"); err != nil || runtime.GOOS != "darwin" {
		t.Skip("no Xcode here")
	}
	t.Setenv("MOBIUM_HOME", t.TempDir())
	bin, err := SimAudioHelper(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again, err := SimAudioHelper(context.Background()); err != nil || again != bin {
		t.Errorf("built again, or another path: %s, %v", again, err)
	}
	out, _ := exec.Command(bin).CombinedOutput()
	if !strings.Contains(string(out), "usage") {
		t.Errorf("the helper with no arguments said %q", out)
	}
}
