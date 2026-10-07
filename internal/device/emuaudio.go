package device

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// An emulator's audio is what its device plays, and the emulator hands it out
// over its gRPC control port: streamAudio delivers it in packets of 20 to
// 30ms. That port is opened by default since the emulator required a token
// for it — on 127.0.0.1 only, with the token in a discovery file that only
// the user can read — so nothing is started for it, and nothing is put on
// the device. `-no-audio`, which Mobium boots with, silences the Mac's
// speakers and not the stream: measured, the Audio Demo's tone arrived the
// same either way.
//
// gRPC is HTTP/2 with five-byte framing, and the two messages here are small
// enough to write by hand, so this is the standard library's unencrypted
// HTTP/2 and no gRPC dependency.
//
// Measured on the mobium-test emulator (Android 15, emulator 37.1.11):
// 440 Hz then 880 with a second of silence between arrived in place to the
// tenth of a second, at -41.6 dBFS where the app wrote -15 — the device's
// media volume; and until the device has played anything since it booted,
// the stream sends nothing at all, not even its headers.

// AudioRate is the rate the stream is asked for. Android's own mixer runs at
// 48kHz, so nothing is resampled on the way.
const AudioRate = 48000

// AudioRecording is an audio capture in progress.
type AudioRecording interface {
	// Started is when the capture began, by the host's clock.
	Started() time.Time
	// Stop ends the capture and returns what was heard, mono at AudioRate,
	// one sample per sample period since Started: time the stream sent
	// nothing is silence, in its place.
	Stop(ctx context.Context) ([]int16, error)
	// Discard ends the capture and keeps nothing.
	Discard(ctx context.Context)
}

// emulatorEndpoint is where an emulator's control port answers, and the
// token it wants, read from the discovery file the emulator writes for each
// instance while it runs.
type emulatorEndpoint struct {
	addr, token string
}

// emulatorDiscoveryDirs are where the emulator writes its discovery files,
// one pid_<pid>.ini per running instance.
func emulatorDiscoveryDirs() []string {
	var dirs []string
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		dirs = append(dirs, filepath.Join(home, "Library", "Caches", "TemporaryItems", "avd", "running"))
	case "windows":
		if l := os.Getenv("LOCALAPPDATA"); l != "" {
			dirs = append(dirs, filepath.Join(l, "Temp", "avd", "running"))
		}
	default:
		if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" {
			dirs = append(dirs, filepath.Join(x, "avd", "running"))
		}
		dirs = append(dirs, filepath.Join(os.TempDir(), "android-"+os.Getenv("USER"), "avd", "running"))
	}
	return dirs
}

// findEmulatorEndpoint finds the control port of the emulator adb calls
// serial — emulator-5554 is the one whose console is on port 5554 — among
// the discovery files in dirs. The newest file wins: an emulator that died
// can leave its file behind.
func findEmulatorEndpoint(serial string, dirs []string) (emulatorEndpoint, error) {
	console, ok := strings.CutPrefix(serial, "emulator-")
	if !ok {
		return emulatorEndpoint{}, mobiumerr.New(mobiumerr.Unsupported, "%s is not an emulator", serial)
	}
	var best emulatorEndpoint
	var bestTime time.Time
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "pid_*.ini"))
		for _, f := range files {
			fields := readIni(f)
			if fields["port.serial"] != console || fields["grpc.port"] == "" {
				continue
			}
			info, err := os.Stat(f)
			if err != nil || !info.ModTime().After(bestTime) {
				continue
			}
			best = emulatorEndpoint{addr: "127.0.0.1:" + fields["grpc.port"], token: fields["grpc.token"]}
			bestTime = info.ModTime()
		}
	}
	if best.addr == "" {
		return emulatorEndpoint{}, mobiumerr.New(mobiumerr.DeviceNotReady, "%s has no control port Mobium "+
			"can find (looked in %s) — it was started with -no-grpc, or by an emulator older than 33; "+
			"restart it with `mobium boot`", serial, strings.Join(dirs, ", "))
	}
	return best, nil
}

func readIni(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// StartEmulatorAudio starts capturing what the emulator adb calls serial
// plays.
func StartEmulatorAudio(ctx context.Context, serial string) (AudioRecording, error) {
	ep, err := findEmulatorEndpoint(serial, emulatorDiscoveryDirs())
	if err != nil {
		return nil, err
	}
	return startStreamAudio(ctx, ep, serial)
}

// headerWait is how long a start waits to hear the stream refused. A refusal
// comes at once; an accepted stream says nothing until the device has played
// something, so silence past this is taken for acceptance.
const headerWait = 750 * time.Millisecond

// gap is how far behind the clock the samples may fall before the time the
// stream sent nothing is filled with silence. Packets come every 20 to 30ms,
// so a tenth of a second behind is a pause in the stream, not jitter.
const gap = 100 * time.Millisecond

type emuAudio struct {
	started time.Time
	cancel  context.CancelFunc
	done    chan struct{}

	mu      sync.Mutex
	samples []int16
	err     error
}

func startStreamAudio(ctx context.Context, ep emulatorEndpoint, serial string) (AudioRecording, error) {
	// AudioFormat{samplingRate: AudioRate, channels: Mono, format: S16,
	// mode: MODE_REAL_TIME} — real time, so a slow reader loses audio
	// rather than falling ever further behind the device.
	msg := append([]byte{0x08}, protoVarint(AudioRate)...)
	msg = append(msg, 0x18, 0x01, 0x20, 0x01)
	body := append([]byte{0}, binary.BigEndian.AppendUint32(nil, uint32(len(msg)))...)
	body = append(body, msg...)

	streamCtx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(streamCtx, http.MethodPost,
		"http://"+ep.addr+"/android.emulation.control.EmulatorController/streamAudio", bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("TE", "trailers")
	if ep.token != "" {
		req.Header.Set("Authorization", "Bearer "+ep.token)
	}
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: &http.Transport{Protocols: &protocols}}

	r := &emuAudio{started: time.Now(), cancel: cancel, done: make(chan struct{})}
	answered := make(chan error, 1)
	go func() {
		defer close(r.done)
		resp, err := client.Do(req)
		if err != nil {
			answered <- err
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if err := grpcRefusal(resp, serial); err != nil {
			answered <- err
			return
		}
		answered <- nil
		r.read(resp.Body)
	}()
	select {
	case err := <-answered:
		if err != nil {
			cancel()
			<-r.done
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			if _, ok := mobiumerr.As(err); ok {
				return nil, err
			}
			return nil, mobiumerr.New(mobiumerr.DeviceNotReady, "%s's control port at %s did not answer: %v "+
				"— is the emulator still running?", serial, ep.addr, err)
		}
	case <-time.After(headerWait):
	case <-ctx.Done():
		cancel()
		<-r.done
		return nil, ctx.Err()
	}
	return r, nil
}

// grpcRefusal is the error a response carries when the call was refused
// rather than started. gRPC says so in a header, with HTTP 200.
func grpcRefusal(resp *http.Response, serial string) error {
	status := resp.Header.Get("Grpc-Status")
	if resp.StatusCode == http.StatusOK && (status == "" || status == "0") {
		return nil
	}
	message := resp.Header.Get("Grpc-Message")
	if status == "16" || status == "7" {
		return mobiumerr.New(mobiumerr.DeviceServer, "%s's control port refused the token in its discovery "+
			"file (gRPC status %s %s) — restart the emulator with `mobium boot`", serial, status, message)
	}
	return mobiumerr.New(mobiumerr.DeviceServer, "%s's control port refused the audio stream: HTTP %d, gRPC "+
		"status %q %s", serial, resp.StatusCode, status, message)
}

// read appends each packet's samples, padding with silence wherever the
// stream fell more than gap behind the clock, until the stream ends.
func (r *emuAudio) read(body io.Reader) {
	head := make([]byte, 5)
	for {
		if _, err := io.ReadFull(body, head); err != nil {
			r.finish(err)
			return
		}
		n := binary.BigEndian.Uint32(head[1:])
		if n > 1<<20 {
			r.finish(mobiumerr.New(mobiumerr.DeviceServer, "an audio packet of %d bytes", n))
			return
		}
		msg := make([]byte, n)
		if _, err := io.ReadFull(body, msg); err != nil {
			r.finish(err)
			return
		}
		pcm := audioPacketPCM(msg)
		arrived := time.Since(r.started)
		r.mu.Lock()
		// Where this packet would start had nothing been lost.
		expected := int(arrived*AudioRate/time.Second) - len(pcm)/2
		if behind := expected - len(r.samples); behind > int(gap*AudioRate/time.Second) {
			r.samples = append(r.samples, make([]int16, behind)...)
		}
		for i := 0; i+1 < len(pcm); i += 2 {
			r.samples = append(r.samples, int16(binary.LittleEndian.Uint16(pcm[i:])))
		}
		r.mu.Unlock()
	}
}

func (r *emuAudio) finish(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		r.err = err
	}
}

func (r *emuAudio) Started() time.Time { return r.started }

func (r *emuAudio) Stop(ctx context.Context) ([]int16, error) {
	elapsed := time.Since(r.started)
	r.cancel()
	select {
	case <-r.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "the emulator's audio stream broke: %v", r.err)
	}
	// The stream says nothing until the device plays, and nothing once it
	// stops: the rest of the time it was capturing is silence.
	if want := int(elapsed * AudioRate / time.Second); len(r.samples) < want {
		r.samples = append(r.samples, make([]int16, want-len(r.samples))...)
	}
	return r.samples, nil
}

func (r *emuAudio) Discard(ctx context.Context) {
	r.cancel()
	select {
	case <-r.done:
	case <-ctx.Done():
	}
}

// audioPacketPCM returns an AudioPacket's audio: field 3, bytes.
func audioPacketPCM(msg []byte) []byte {
	for len(msg) > 0 {
		key, n := binary.Uvarint(msg)
		if n <= 0 {
			return nil
		}
		msg = msg[n:]
		switch key & 7 {
		case 0:
			_, n := binary.Uvarint(msg)
			if n <= 0 {
				return nil
			}
			msg = msg[n:]
		case 2:
			l, n := binary.Uvarint(msg)
			if n <= 0 || uint64(len(msg)-n) < l {
				return nil
			}
			msg = msg[n:]
			if key>>3 == 3 {
				return msg[:l]
			}
			msg = msg[l:]
		default:
			return nil
		}
	}
	return nil
}

func protoVarint(v uint64) []byte { return binary.AppendUvarint(nil, v) }
