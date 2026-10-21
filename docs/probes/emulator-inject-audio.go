//go:build ignore

// emulator-inject-audio.go plays a WAV into an Android emulator's virtual
// microphone through its gRPC control port (EmulatorController/injectAudio),
// the measurement behind ACCESSIBILITY.md step 7.
//
//	say -o open.wav --data-format=LEI16@16000 "Open Calculator"
//	adb shell input keyevent KEYCODE_ASSIST   # Google Assistant starts listening
//	go run docs/probes/emulator-inject-audio.go 5554 open.wav
//
// The WAV is 16-bit mono PCM; it is sent in 20ms packets, paced in real
// time. The control port and its token come from the emulator's discovery
// file, as Mobium's audio capture finds them. Nothing goes through the
// room: no speaker plays it and no microphone hears it.
//
// On emulator 37.1.11 this crashed the emulator twice in seventeen
// injections — SIGSEGV in audio_forwarder_enable, under injectAudio — and
// the next boot waited on a crash-report dialog. Run it on an emulator
// whose state you can lose.
package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func varint(v uint64) []byte { return binary.AppendUvarint(nil, v) }

func frame(msg []byte) []byte {
	b := append([]byte{0}, binary.BigEndian.AppendUint32(nil, uint32(len(msg)))...)
	return append(b, msg...)
}

func main() {
	console, wav := os.Args[1], os.Args[2]
	home, _ := os.UserHomeDir()
	files, _ := filepath.Glob(filepath.Join(home, "Library/Caches/TemporaryItems/avd/running/pid_*.ini"))
	var addr, token string
	for _, f := range files {
		data, _ := os.ReadFile(f)
		kv := map[string]string{}
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			if k, v, ok := strings.Cut(sc.Text(), "="); ok {
				kv[k] = v
			}
		}
		if kv["port.serial"] == console {
			addr, token = "127.0.0.1:"+kv["grpc.port"], kv["grpc.token"]
		}
	}
	raw, err := os.ReadFile(wav)
	if err != nil {
		panic(err)
	}
	i := bytes.Index(raw, []byte("data"))
	pcm := raw[i+8:]
	fm := bytes.Index(raw, []byte("fmt "))
	rate := binary.LittleEndian.Uint32(raw[fm+12 : fm+16])
	if rate == 0 {
		panic("no sample rate")
	}
	fmt.Println("rate", rate, "bytes", len(pcm), "addr", addr)

	pr, pw := io.Pipe()
	go func() {
		chunk := int(rate) * 2 / 50 // 20ms
		start := time.Now()
		first := true
		for off := 0; off < len(pcm); off += chunk {
			end := off + chunk
			if end > len(pcm) {
				end = len(pcm)
			}
			var msg []byte
			if first {
				// format = AudioFormat{samplingRate, channels Mono(0), format S16(1), mode REAL_TIME(1)}
				f := append([]byte{0x08}, varint(uint64(rate))...)
				f = append(f, 0x18, 0x01, 0x20, 0x01)
				msg = append([]byte{0x0a}, varint(uint64(len(f)))...)
				msg = append(msg, f...)
				first = false
			}
			ts := uint64(time.Since(start).Microseconds())
			msg = append(msg, 0x10)
			msg = append(msg, varint(ts)...)
			msg = append(msg, 0x1a)
			msg = append(msg, varint(uint64(end-off))...)
			msg = append(msg, pcm[off:end]...)
			if _, err := pw.Write(frame(msg)); err != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		pw.Close()
	}()
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/android.emulation.control.EmulatorController/injectAudio", pr)
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("TE", "trailers")
	req.Header.Set("Authorization", "Bearer "+token)
	var p http.Protocols
	p.SetUnencryptedHTTP2(true)
	resp, err := (&http.Client{Transport: &http.Transport{Protocols: &p}}).Do(req)
	if err != nil {
		panic(err)
	}
	body, _ := io.ReadAll(resp.Body)
	fmt.Println("status", resp.Status, "grpc-status", resp.Header.Get("Grpc-Status"), resp.Trailer.Get("Grpc-Status"), resp.Trailer.Get("Grpc-Message"), len(body))
}
