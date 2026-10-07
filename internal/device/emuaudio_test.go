package device

import (
	"context"
	"encoding/binary"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

func writeIni(t *testing.T, dir, name, body string, age time.Duration) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
}

// The discovery file is the emulator's own, as emulator 37.1.11 wrote it.
func TestFindEmulatorEndpoint(t *testing.T) {
	dir := t.TempDir()
	writeIni(t, dir, "pid_1.ini", "port.serial=5556\nport.adb=5557\ngrpc.port=8556\ngrpc.token=other\n", 0)
	// An emulator that died left its file; the newer one for 5554 wins.
	writeIni(t, dir, "pid_2.ini", "port.serial=5554\ngrpc.port=8000\ngrpc.token=stale\n", time.Hour)
	writeIni(t, dir, "pid_3.ini", "emulator.version=37.1.11.0\nport.serial=5554\nport.adb=5555\n"+
		"grpc.jwk=x\ngrpc.token=abc.def\ngrpc.port=8554\n", time.Minute)
	ep, err := findEmulatorEndpoint("emulator-5554", []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if ep.addr != "127.0.0.1:8554" || ep.token != "abc.def" {
		t.Fatalf("got %+v", ep)
	}
}

func TestFindEmulatorEndpointMissing(t *testing.T) {
	dir := t.TempDir()
	// Started with -no-grpc: a file, but no port.
	writeIni(t, dir, "pid_1.ini", "port.serial=5554\nport.adb=5555\n", 0)
	_, err := findEmulatorEndpoint("emulator-5554", []string{dir})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "mobium boot") {
		t.Fatalf("got %v", err)
	}
	if _, err := findEmulatorEndpoint("3C191FDJG001QX", []string{dir}); mobiumerr.CodeOf(err) != mobiumerr.Unsupported {
		t.Fatalf("a phone: got %v", err)
	}
}

// packet frames an AudioPacket{format, timestamp, audio} as gRPC sends it.
func packet(samples []int16) []byte {
	var pcm []byte
	for _, s := range samples {
		pcm = binary.LittleEndian.AppendUint16(pcm, uint16(s))
	}
	msg := []byte{0x0a, 0x02, 0x08, 0x01, 0x10, 0x07} // format{samplingRate:1}, timestamp 7
	msg = append(msg, 0x1a)
	msg = append(msg, protoVarint(uint64(len(pcm)))...)
	msg = append(msg, pcm...)
	return append(append([]byte{0}, binary.BigEndian.AppendUint32(nil, uint32(len(msg)))...), msg...)
}

func TestAudioPacketPCM(t *testing.T) {
	framed := packet([]int16{1, -1, 300})
	pcm := audioPacketPCM(framed[5:])
	if len(pcm) != 6 || int16(binary.LittleEndian.Uint16(pcm[4:])) != 300 {
		t.Fatalf("got %v", pcm)
	}
	if audioPacketPCM([]byte{0x1a, 0x7f}) != nil {
		t.Fatal("a length past the end must not be read")
	}
}

// fakeControlPort serves streamAudio over unencrypted HTTP/2, as the
// emulator does, with handler deciding what it sends.
func fakeControlPort(t *testing.T, handler http.HandlerFunc) emulatorEndpoint {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var p http.Protocols
	p.SetUnencryptedHTTP2(true)
	srv := &http.Server{Handler: handler, Protocols: &p}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return emulatorEndpoint{addr: ln.Addr().String(), token: "tok"}
}

// Without the right token the emulator answers status 16 at once, and the
// start says so rather than recording silence.
func TestStreamAudioRefused(t *testing.T) {
	ep := fakeControlPort(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "16")
		w.Header().Set("Grpc-Message", "unauthenticated")
		w.WriteHeader(http.StatusOK)
	})
	_, err := startStreamAudio(context.Background(), ep, "emulator-5554")
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceServer || !strings.Contains(err.Error(), "token") {
		t.Fatalf("got %v", err)
	}
}

// A stream that pauses is placed by the clock: what the stream did not send
// is silence where it fell, and the time after it stopped is silence too.
func TestStreamAudioPlacesByClock(t *testing.T) {
	gotToken := make(chan string, 1)
	ep := fakeControlPort(t, func(w http.ResponseWriter, r *http.Request) {
		gotToken <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/grpc")
		w.WriteHeader(http.StatusOK)
		f := w.(http.Flusher)
		tone := make([]int16, AudioRate/20) // 50ms
		for i := range tone {
			tone[i] = 1000
		}
		_, _ = w.Write(packet(tone))
		f.Flush()
		time.Sleep(400 * time.Millisecond)
		_, _ = w.Write(packet(tone))
		f.Flush()
		<-r.Context().Done()
	})
	rec, err := startStreamAudio(context.Background(), ep, "emulator-5554")
	if err != nil {
		t.Fatal(err)
	}
	if tok := <-gotToken; tok != "Bearer tok" {
		t.Errorf("Authorization %q", tok)
	}
	time.Sleep(800 * time.Millisecond)
	got, err := rec.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	samples := got.Samples
	if got := len(samples); got < AudioRate*7/10 || got > AudioRate*12/10 {
		t.Fatalf("%d samples for about 0.8s", got)
	}
	// The second packet sits about 0.4s after the first, not right after it.
	second := -1
	for i := AudioRate / 20; i < len(samples); i++ {
		if samples[i] != 0 {
			second = i
			break
		}
	}
	if second < AudioRate*3/10 {
		t.Fatalf("second packet at sample %d, want about %d", second, AudioRate*45/100)
	}
}

// A port nobody answers on is the emulator gone, said as such.
func TestStreamAudioNoAnswer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	_, err = startStreamAudio(context.Background(), emulatorEndpoint{addr: addr}, "emulator-5554")
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady {
		t.Fatalf("got %v", err)
	}
}
