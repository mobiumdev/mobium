package device

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
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

// MaxAudioCapture is the most a capture keeps. Silence is not sent, so a
// capture holds little while it runs — 25MB at ten minutes, measured — and
// the stop fills the quiet in at once: 208MB after those ten minutes, and a
// capture left running for a day would have asked for 8GB. Past this the
// capture keeps what it had and the stop says so. A var for tests.
// CHALLENGES 282.
var MaxAudioCapture = time.Hour

// AudioRecording is an audio capture in progress.
type AudioRecording interface {
	// Started is when the capture began, by the host's clock.
	Started() time.Time
	// Stop ends the capture and returns what was heard, mono at AudioRate,
	// one sample per sample period since Started: time the stream sent
	// nothing is silence, in its place.
	Stop(ctx context.Context) (AudioCapture, error)
	// Discard ends the capture and keeps nothing.
	Discard(ctx context.Context)
	// Captures is whether the sound itself is captured; a phone's is not,
	// only what interrupted it.
	Captures() bool
}

// AudioCapture is what a capture heard, and the device's volumes as it began
// and as it ended: what arrives follows the media volume — a tone the
// Audio Demo wrote at a quarter of full scale arrived at -9 dBFS at 15 of 15,
// -42 at 5 and -63 at 1, and at 0 as silence (Android 15 emulator) — so a
// silent capture means little without them.
type AudioCapture struct {
	Samples []int16
	// Volumes are read at the start and at the end; nil when the device
	// could not say.
	VolumesAtStart, VolumesAtEnd []StreamVolume
	// Interruptions are what cut across the app's audio meanwhile, and App
	// the app they were read for.
	Interruptions []AudioInterruption
	App           string
}

// VolumeReader reads a device's volumes.
type VolumeReader func(ctx context.Context) ([]StreamVolume, error)

// emulatorEndpoint is where an emulator's control port answers, and the
// token it wants, read from the discovery file the emulator writes for each
// instance while it runs.
type emulatorEndpoint struct {
	addr, token string
	// avd is the AVD's name, from the discovery file, for a remedy to name.
	avd string
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
			best = emulatorEndpoint{addr: "127.0.0.1:" + fields["grpc.port"], token: fields["grpc.token"], avd: fields["avd.name"]}
			bestTime = info.ModTime()
		}
	}
	if best.addr == "" {
		// `mobium boot` on an emulator already running restarts nothing —
		// it says it is running — so the remedy names the stop as well.
		// CHALLENGES 284.
		return emulatorEndpoint{}, mobiumerr.New(mobiumerr.DeviceNotReady, "%s has no control port Mobium "+
			"can find (looked in %s) — it was started with -no-grpc, or by an emulator older than 33",
			serial, strings.Join(dirs, ", ")).
			WithRemedy(fmt.Sprintf("restart it: `mobium shutdown %s`, then `mobium boot <its AVD>`, which "+
				"starts it with the control port on", serial))
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
// plays, reading its volumes with volumes as it starts and stops.
func StartEmulatorAudio(ctx context.Context, serial string, volumes VolumeReader, interruptions InterruptionReader) (AudioRecording, error) {
	ep, err := findEmulatorEndpoint(serial, emulatorDiscoveryDirs())
	if err != nil {
		return nil, err
	}
	var before []StreamVolume
	if volumes != nil {
		before, _ = volumes(ctx)
	}
	r, err := startStreamAudio(ctx, ep, serial)
	if err != nil {
		return nil, err
	}
	a := r.(*emuAudio)
	a.volumes, a.before, a.interruptions = volumes, before, interruptions
	return a, nil
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
	// stopping is set by Stop before it cancels the stream; ended is when
	// the stream ended without being asked to — the emulator went away.
	stopping bool
	ended    time.Duration

	volumes       VolumeReader
	before        []StreamVolume
	interruptions InterruptionReader
}

func (r *emuAudio) Captures() bool { return true }

// connSet keeps the connections a transport dialed, to close them itself.
type connSet struct {
	mu    sync.Mutex
	conns []net.Conn
}

func (c *connSet) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
	if err == nil {
		c.mu.Lock()
		c.conns = append(c.conns, conn)
		c.mu.Unlock()
	}
	return conn, err
}

func (c *connSet) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, conn := range c.conns {
		_ = conn.Close()
	}
	c.conns = nil
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
	// The capture closes its own connection when the stream ends. Left to
	// the transport, which is this capture's alone, the connection stayed
	// open after the stream: one per capture, nine after nine, and closing
	// the transport's idle connections as the stream ended did not reach
	// it. CHALLENGES 283.
	var conns connSet
	transport := &http.Transport{Protocols: &protocols, DialContext: conns.dial}
	client := &http.Client{Transport: transport}

	r := &emuAudio{started: time.Now(), cancel: cancel, done: make(chan struct{})}
	answered := make(chan error, 1)
	go func() {
		defer close(r.done)
		defer conns.closeAll()
		resp, err := client.Do(req)
		if err != nil {
			answered <- err
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if err := grpcRefusal(resp, serial, ep); err != nil {
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
func grpcRefusal(resp *http.Response, serial string, ep emulatorEndpoint) error {
	status := resp.Header.Get("Grpc-Status")
	if resp.StatusCode == http.StatusOK && (status == "" || status == "0") {
		return nil
	}
	message := resp.Header.Get("Grpc-Message")
	if status == "16" || status == "7" {
		avd := ep.avd
		if avd == "" {
			avd = "<its AVD>"
		}
		restart := fmt.Sprintf("`mobium shutdown %s`, then `mobium boot %s`", serial, avd)
		return mobiumerr.New(mobiumerr.DeviceServer, "%s's control port refused the token in its discovery "+
			"file (gRPC status %s %s) — restart the emulator: %s", serial, status, message, restart).
			WithRemedy("restart the emulator, which writes a new token: " + restart)
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
		if arrived > MaxAudioCapture {
			continue
		}
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

// finish records how the stream ended. One the stop asked for is no
// failure, however it surfaced. One that ended by itself is: the stream stays
// open through minutes of silence (measured, 180s), and ended with no error
// when the emulator was killed — and the stop then padded the time after it
// with silence, as if it had been heard. CHALLENGES 281.
func (r *emuAudio) finish(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.stopping || errors.Is(err, context.Canceled):
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		r.ended = time.Since(r.started)
	default:
		r.err = err
	}
}

func (r *emuAudio) Started() time.Time { return r.started }

func (r *emuAudio) Stop(ctx context.Context) (AudioCapture, error) {
	elapsed := time.Since(r.started)
	r.mu.Lock()
	r.stopping = true
	r.mu.Unlock()
	r.cancel()
	select {
	case <-r.done:
	case <-ctx.Done():
		return AudioCapture{}, ctx.Err()
	}
	var after []StreamVolume
	if r.volumes != nil {
		after, _ = r.volumes(ctx)
	}
	var cut []AudioInterruption
	if r.interruptions != nil {
		cut, _ = r.interruptions(ctx)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return AudioCapture{}, mobiumerr.New(mobiumerr.DeviceServer, "the emulator's audio stream broke: %v", r.err)
	}
	if r.ended > 0 {
		return AudioCapture{}, mobiumerr.New(mobiumerr.DeviceNotReady, "the emulator's audio stream ended %s into "+
			"the capture, %s before the stop — the emulator stopped or restarted, and nothing after it was heard",
			r.ended.Round(100*time.Millisecond), (elapsed - r.ended).Round(100*time.Millisecond)).
			WithRemedy("check the emulator is running (`mobium devices`), then start the capture again")
	}
	// The stream says nothing until the device plays, and nothing once it
	// stops: the rest of the time it was capturing is silence.
	kept := min(elapsed, MaxAudioCapture)
	if want := int(kept * AudioRate / time.Second); len(r.samples) < want {
		r.samples = append(r.samples, make([]int16, want-len(r.samples))...)
	} else if len(r.samples) > want {
		r.samples = r.samples[:want]
	}
	return AudioCapture{Samples: r.samples, VolumesAtStart: r.before, VolumesAtEnd: after, Interruptions: cut}, nil
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
