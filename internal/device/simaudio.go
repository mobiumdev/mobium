package device

import (
	"bufio"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"github.com/mobiumdev/mobium/internal/paths"
)

// What an app in an iOS simulator plays is heard on the Mac: the app is a
// process there, and a CoreAudio process tap on it hears it alone. Mobium
// is built without cgo, so the tap is a small Swift helper, compiled with
// Xcode's swiftc on first use — a simulator means Xcode is installed — and
// kept under MOBIUM_HOME by the hash of its source, as WebDriverAgent is
// built and kept. Measured 2026-10-07 (docs/probes/ios-audio-sim.swift) and
// built 2026-10-22: the Audio Demo's 440 Hz, a second of silence and 880 Hz
// arrived at -15.1 dBFS, what the app writes.
//
//go:embed simaudio.swift
var simAudioSource []byte

// SimAudioHelper is the helper's path, built if it is not there yet.
func SimAudioHelper(ctx context.Context) (string, error) {
	sum := sha256.Sum256(simAudioSource)
	dir := filepath.Join(paths.Root(), "helpers")
	bin := filepath.Join(dir, "mobium-sim-audio-"+hex.EncodeToString(sum[:6]))
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	src := bin + ".swift"
	if err := os.WriteFile(src, simAudioSource, 0o644); err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(src) }()
	tmp := bin + ".building"
	out, err := exec.CommandContext(ctx, "xcrun", "swiftc", "-O", src, "-o", tmp).CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		return "", mobiumerr.New(mobiumerr.Unsupported, "building the simulator's audio helper with Xcode's "+
			"swiftc failed: %v: %s", err, firstLine(strings.TrimSpace(string(out)))).
			WithRemedy("check that Xcode is installed and selected: `xcode-select -p`, then `xcrun swiftc --version`")
	}
	if err := os.Rename(tmp, bin); err != nil {
		return "", err
	}
	return bin, nil
}

// simAudioHelper is SimAudioHelper, replaceable in tests by a stub that
// speaks the helper's protocol.
var simAudioHelper = SimAudioHelper

// simAudioRecordingNote is what a capture that heard nothing of the app says,
// since the timeline alone cannot tell "silent" from "not heard".
const (
	simNeverOpened = "the app never opened audio while the capture ran, so there was nothing of it to hear"
	simNoCycles    = "the tap received no audio at all: macOS's System Audio Recording is not allowed for " +
		"the program that started mobium"
)

// StartSimulatorAudio captures what app plays in the simulator udid.
func StartSimulatorAudio(ctx context.Context, udid, app string) (AudioRecording, error) {
	helper, err := simAudioHelper(ctx)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(helper, app, udid)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	r := &simAudio{started: time.Now(), cmd: cmd, stdin: stdin, done: make(chan struct{}), app: app}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	events := make(chan struct{})
	go func() { defer close(events); r.readEvents(stderr) }()
	go func() {
		r.read(stdout)
		<-events
		r.waited = cmd.Wait()
		close(r.done)
	}()
	return r, nil
}

type simAudio struct {
	started time.Time
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	done    chan struct{}
	app     string
	waited  error

	mu        sync.Mutex
	samples   []int16
	rate      float64
	channels  int
	tapped    bool
	noCycles  bool
	cycles    bool
	helperErr string
}

type simEvent struct {
	Event    string  `json:"event"`
	Rate     float64 `json:"rate"`
	Channels int     `json:"channels"`
	Message  string  `json:"message"`
}

func (r *simAudio) readEvents(stderr io.Reader) {
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		var e simEvent
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		r.mu.Lock()
		switch e.Event {
		case "tapping":
			r.tapped, r.rate, r.channels = true, e.Rate, max(e.Channels, 1)
		case "no-cycles":
			r.noCycles = true
		case "error":
			r.helperErr = e.Message
		}
		r.mu.Unlock()
	}
}

// read turns the tap's float frames into mono samples at AudioRate, placed
// by the clock: wherever the stream is behind where it would be had nothing
// been lost — before the app opened audio, or while it was gone — the gap
// is silence, as the emulator's capture does it.
func (r *simAudio) read(stdout io.Reader) {
	buf := make([]byte, 64*1024)
	var carry []byte
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			r.mu.Lock()
			ch := max(r.channels, 1)
			frame := 4 * ch
			data := append(carry, buf[:n]...)
			whole := len(data) / frame * frame
			mono := monoAt(data[:whole], ch, r.rate)
			carry = append([]byte(nil), data[whole:]...)
			r.cycles = true
			arrived := time.Since(r.started)
			if arrived <= MaxAudioCapture {
				expected := int(arrived*AudioRate/time.Second) - len(mono)
				if behind := expected - len(r.samples); behind > int(gap*AudioRate/time.Second) {
					r.samples = append(r.samples, make([]int16, behind)...)
				}
				r.samples = append(r.samples, mono...)
			}
			r.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// monoAt mixes interleaved float32 frames to mono int16 at AudioRate, from
// rate; a tap gives the Mac's output rate, 48 kHz where measured, which is
// AudioRate already.
func monoAt(data []byte, channels int, rate float64) []int16 {
	frames := len(data) / (4 * channels)
	mixed := make([]float64, frames)
	for i := 0; i < frames; i++ {
		var sum float64
		for c := 0; c < channels; c++ {
			bits := binary.LittleEndian.Uint32(data[4*(i*channels+c):])
			sum += float64(math.Float32frombits(bits))
		}
		mixed[i] = sum / float64(channels)
	}
	if rate > 0 && math.Abs(rate-AudioRate) > 1 && frames > 1 {
		n := int(float64(frames) * AudioRate / rate)
		out := make([]float64, n)
		for i := range out {
			pos := float64(i) * rate / AudioRate
			j := int(pos)
			if j+1 >= frames {
				out[i] = mixed[frames-1]
				continue
			}
			f := pos - float64(j)
			out[i] = mixed[j]*(1-f) + mixed[j+1]*f
		}
		mixed = out
	}
	pcm := make([]int16, len(mixed))
	for i, v := range mixed {
		pcm[i] = int16(math.Max(-32768, math.Min(32767, math.Round(v*32767))))
	}
	return pcm
}

func (r *simAudio) Started() time.Time { return r.started }

// Captures is true: the sound itself is heard.
func (r *simAudio) Captures() bool { return true }

func (r *simAudio) Stop(ctx context.Context) (AudioCapture, error) {
	elapsed := time.Since(r.started)
	if err := r.end(ctx); err != nil {
		return AudioCapture{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.helperErr != "" {
		return AudioCapture{}, mobiumerr.New(mobiumerr.DeviceServer, "the simulator's audio tap failed: %s", r.helperErr)
	}
	if r.tapped && !r.cycles && r.noCycles {
		return AudioCapture{}, mobiumerr.New(mobiumerr.Unsupported, "%s — so the capture could not tell %s's "+
			"sound from silence", simNoCycles, r.app).
			WithRemedy("allow it in System Settings > Privacy & Security > Screen & System Audio Recording, " +
				"under System Audio Recording Only, for the terminal or app that runs mobium; then stop the " +
				"daemon (`mobium daemon stop`) and capture again")
	}
	kept := min(elapsed, MaxAudioCapture)
	if want := int(kept * AudioRate / time.Second); len(r.samples) < want {
		r.samples = append(r.samples, make([]int16, want-len(r.samples))...)
	} else if len(r.samples) > want {
		r.samples = r.samples[:want]
	}
	c := AudioCapture{Samples: r.samples}
	if !r.tapped {
		c.Note = simNeverOpened
	}
	return c, nil
}

func (r *simAudio) Discard(ctx context.Context) { _ = r.end(ctx) }

// end closes the helper's stdin, which is its stop, and waits for it to put
// the tap away; a helper that does not answer is killed after a while.
func (r *simAudio) end(ctx context.Context) error {
	_ = r.stdin.Close()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		_ = r.cmd.Process.Kill()
		<-r.done
	case <-ctx.Done():
		_ = r.cmd.Process.Kill()
		return ctx.Err()
	}
	var exit *exec.ExitError
	if r.waited != nil && !errors.As(r.waited, &exit) {
		return r.waited
	}
	return nil
}
