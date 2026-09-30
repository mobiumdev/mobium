package device

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeJPEG is enough of a JPEG for the recorder: a start-of-frame marker with
// dimensions, and a body that differs per frame.
func fakeJPEG(w, h, n int) []byte {
	b := []byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x11, 0x08}
	b = binary.BigEndian.AppendUint16(b, uint16(h))
	b = binary.BigEndian.AppendUint16(b, uint16(w))
	b = append(b, make([]byte, 10)...)
	b = append(b, []byte(fmt.Sprintf("frame %d", n))...)
	return append(b, 0xFF, 0xD9)
}

// wdaStream serves frames the way WebDriverAgent does: a boundary declared
// as "--BoundaryString" and used as it is, which mime/multipart rejects.
func wdaStream(frames int, every time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=--BoundaryString")
		fl := w.(http.Flusher)
		for i := 0; i < frames; i++ {
			jpeg := fakeJPEG(1290, 2796, i)
			fmt.Fprintf(w, "--BoundaryString\r\nContent-type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(jpeg))
			w.Write(jpeg)
			w.Write([]byte("\r\n\r\n"))
			fl.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(every):
			}
		}
		<-r.Context().Done()
	})
}

func TestMJPEGRecordingWritesAPlayableHeader(t *testing.T) {
	srv := httptest.NewServer(wdaStream(8, 50*time.Millisecond))
	defer srv.Close()
	dir := t.TempDir()
	rec, err := StartMJPEGRecording(context.Background(), srv.URL, dir)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	dest := filepath.Join(dir, "out.mp4")
	info, err := rec.Stop(context.Background(), dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Frames != 8 {
		t.Errorf("frames = %d, want the 8 sent", info.Frames)
	}
	// The last frame lasts until the stop, so the video spans the recording.
	if info.Duration < 500*time.Millisecond || info.Duration > 900*time.Millisecond {
		t.Errorf("duration = %s, want about the 600ms recorded", info.Duration)
	}
	data, _ := os.ReadFile(dest)
	entry := findBox(data, "moov", "trak", "mdia", "minf", "stbl", "stsd")
	if entry == nil || !strings.Contains(string(entry), "jpeg") {
		t.Fatal("no jpeg sample entry")
	}
	if w, h := binary.BigEndian.Uint16(entry[8+8+24:]), binary.BigEndian.Uint16(entry[8+8+26:]); w != 1290 || h != 2796 {
		t.Errorf("sample entry says %dx%d, want 1290x2796", w, h)
	}
	// The chunk offset points at the first frame.
	co64 := findBox(data, "moov", "trak", "mdia", "minf", "stbl", "co64")
	off := binary.BigEndian.Uint64(co64[8:])
	if want := fakeJPEG(1290, 2796, 0); string(data[off:off+uint64(len(want))]) != string(want) {
		t.Error("the chunk offset does not point at the first frame")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.mjpeg")); len(left) != 0 {
		t.Errorf("left behind %v", left)
	}
}

func TestMJPEGRecordingDiscardKeepsNothing(t *testing.T) {
	srv := httptest.NewServer(wdaStream(100, 20*time.Millisecond))
	defer srv.Close()
	dir := t.TempDir()
	rec, err := StartMJPEGRecording(context.Background(), srv.URL, dir)
	if err != nil {
		t.Fatal(err)
	}
	rec.Discard(context.Background())
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("a discarded recording left %d files", len(left))
	}
}

// A stream that never sends a frame is not a recording.
func TestMJPEGRecordingRefusesAStreamWithNoFrames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer srv.Close()
	dir := t.TempDir()
	if _, err := StartMJPEGRecording(context.Background(), srv.URL, dir); err == nil {
		t.Fatal("a stream that answered 404 was recorded")
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("a refused recording left %d files", len(left))
	}
}

func TestNextMJPEGPartReadsWDAFraming(t *testing.T) {
	in := "--BoundaryString\r\nContent-type: image/jpeg\r\nContent-Length: 3\r\n\r\nabc\r\n\r\n" +
		"--BoundaryString\r\nContent-Length: 2\r\nContent-type: image/jpeg\r\n\r\nde\r\n\r\n"
	br := bufio.NewReader(strings.NewReader(in))
	for _, want := range []string{"abc", "de"} {
		got, err := nextMJPEGPart(br)
		if err != nil || string(got) != want {
			t.Fatalf("part = %q, %v; want %q", got, err, want)
		}
	}
}
