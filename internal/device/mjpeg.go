package device

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A real iPhone's screen is not a file on the Mac, and devicectl has no
// recorder. WebDriverAgent serves one instead: its MJPEG server streams the
// screen as JPEG frames over HTTP, on port 9100 of the phone. Mobium reads
// that stream over the same tunnel address as the runner's HTTP port and
// writes the frames into an MP4 itself, so nothing is installed for it and
// nothing is written on the phone.
//
// Measured on an iPhone 15 Plus, iOS 26.6.2: 47 frames in 5s at
// WebDriverAgent's default of 10 per second, each a baseline JPEG of the
// full 1290x2796 screen at about 140KB, sent whether or not the screen
// changed.

// PhoneMJPEGPort is where WebDriverAgent serves its screen stream on a
// phone. Unlike a simulator's it is not moved: a phone's ports are its own,
// and the stream listens only on the tunnel (wdapatch.go).
const PhoneMJPEGPort = 9100

// firstFrameWait bounds the wait for the stream's first frame. A recording
// that has not received one is not started, whatever the connection said.
const firstFrameWait = 10 * time.Second

type mjpegRecording struct {
	started time.Time
	cancel  context.CancelFunc
	done    chan struct{}

	mu     sync.Mutex
	tmp    *os.File
	frames []mjpegFrame
	width  int
	height int
	err    error
	ended  time.Time
}

// mjpegFrame is one JPEG in the temporary file: where it is, how long, and
// when it arrived by the host's clock — its arrival is its timestamp, so
// the video shows the frame rate that was achieved, not the one asked for.
type mjpegFrame struct {
	size int
	at   time.Time
}

// StartMJPEGRecording records the MJPEG stream at url into a temporary file
// under dir, and answers once the first frame has arrived.
func StartMJPEGRecording(ctx context.Context, url, dir string) (Recording, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(dir, "recording-*.mjpeg")
	if err != nil {
		return nil, err
	}
	// The stream outlives the call that starts it; Stop and Discard end it.
	rctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, err
	}
	r := &mjpegRecording{started: time.Now(), cancel: cancel, done: make(chan struct{}), tmp: tmp}
	go r.read(req)

	deadline := time.Now().Add(firstFrameWait)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		n, rerr := len(r.frames), r.err
		r.mu.Unlock()
		if n > 0 {
			return r, nil
		}
		select {
		case <-r.done:
			r.Discard(ctx)
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "WebDriverAgent's screen stream at %s ended before "+
				"sending a frame: %v", url, rerr)
		case <-ctx.Done():
			r.Discard(ctx)
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	r.Discard(ctx)
	return nil, mobiumerr.New(mobiumerr.Timeout, "WebDriverAgent's screen stream at %s sent no frame within %s",
		url, firstFrameWait)
}

// read takes frames off the stream until it ends or is canceled.
func (r *mjpegRecording) read(req *http.Request) {
	defer close(r.done)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.fail(err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		r.fail(mobiumerr.New(mobiumerr.DeviceServer, "HTTP %s", resp.Status))
		return
	}
	br := bufio.NewReaderSize(resp.Body, 1<<20)
	for {
		jpeg, err := nextMJPEGPart(br)
		if err != nil {
			r.fail(err)
			return
		}
		at := time.Now()
		r.mu.Lock()
		if r.width == 0 {
			r.width, r.height = jpegSize(jpeg)
		}
		if _, err := r.tmp.Write(jpeg); err != nil {
			r.err = err
			r.mu.Unlock()
			return
		}
		r.frames = append(r.frames, mjpegFrame{size: len(jpeg), at: at})
		r.mu.Unlock()
	}
}

func (r *mjpegRecording) fail(err error) {
	r.mu.Lock()
	if r.err == nil {
		r.err = err
	}
	r.mu.Unlock()
}

// nextMJPEGPart reads one part of a multipart/x-mixed-replace stream and
// returns its body. Parsed by hand rather than with mime/multipart:
// WebDriverAgent declares the boundary as "--BoundaryString" and then
// delimits parts with that string itself, not with "--" before it as the
// format has it, so a conforming reader finds no parts at all.
func nextMJPEGPart(br *bufio.Reader) ([]byte, error) {
	length := -1
	inHeaders := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "" && inHeaders:
			if length < 0 {
				return nil, mobiumerr.New(mobiumerr.DeviceServer, "a stream part had no Content-Length")
			}
			body := make([]byte, length)
			if _, err := io.ReadFull(br, body); err != nil {
				return nil, err
			}
			return body, nil
		case line == "":
			// Between parts.
		case strings.HasPrefix(line, "--"):
			inHeaders = true
		default:
			name, value, ok := strings.Cut(line, ":")
			if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
				n, err := strconv.Atoi(strings.TrimSpace(value))
				if err != nil || n < 0 {
					return nil, mobiumerr.New(mobiumerr.DeviceServer, "a stream part's Content-Length is %q", value)
				}
				length = n
			}
		}
	}
}

// jpegSize reads a JPEG's dimensions from its start-of-frame marker, or
// zeros when it has none.
func jpegSize(b []byte) (int, int) {
	for i := 2; i+9 < len(b); {
		if b[i] != 0xFF {
			return 0, 0
		}
		marker := b[i+1]
		length := int(binary.BigEndian.Uint16(b[i+2:]))
		// SOF0 to SOF15, other than DHT (C4), JPG (C8) and DAC (CC).
		if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			return int(binary.BigEndian.Uint16(b[i+7:])), int(binary.BigEndian.Uint16(b[i+5:]))
		}
		i += 2 + length
	}
	return 0, 0
}

func (r *mjpegRecording) Started() time.Time { return r.started }

// end stops reading the stream and waits for the reader to let go.
func (r *mjpegRecording) end() {
	r.cancel()
	<-r.done
	r.mu.Lock()
	if r.ended.IsZero() {
		r.ended = time.Now()
	}
	r.mu.Unlock()
}

func (r *mjpegRecording) Stop(ctx context.Context, dest string) (MP4Info, error) {
	r.end()
	defer func() {
		r.tmp.Close()
		os.Remove(r.tmp.Name())
	}()
	r.mu.Lock()
	frames, w, h, ended := r.frames, r.width, r.height, r.ended
	r.mu.Unlock()
	if len(frames) == 0 {
		return MP4Info{}, mobiumerr.New(mobiumerr.NotConfirmed, "the screen stream sent no frames")
	}
	if _, err := r.tmp.Seek(0, io.SeekStart); err != nil {
		return MP4Info{}, err
	}
	if err := writeMJPEGMP4(dest, r.tmp, frames, w, h, ended); err != nil {
		return MP4Info{}, err
	}
	return ProbeMP4(dest)
}

func (r *mjpegRecording) Discard(ctx context.Context) {
	r.end()
	r.tmp.Close()
	os.Remove(r.tmp.Name())
}

// mp4Timescale is the media's clock: milliseconds, which is finer than any
// frame interval the stream achieves.
const mp4Timescale = 1000

// writeMJPEGMP4 writes frames, read in order from src, as an MP4 whose one
// video track holds them as JPEG samples ("jpeg", QuickTime's Photo-JPEG),
// each lasting until the next arrived and the last until the recording
// ended. The samples are one chunk after the header, so the file is
// written in one pass with the index first.
func writeMJPEGMP4(dest string, src io.Reader, frames []mjpegFrame, width, height int, ended time.Time) error {
	durations := make([]uint32, len(frames))
	var total uint64
	var payload uint64
	for i, f := range frames {
		next := ended
		if i+1 < len(frames) {
			next = frames[i+1].at
		}
		d := next.Sub(f.at).Milliseconds()
		if d < 1 {
			d = 1
		}
		durations[i] = uint32(d)
		total += uint64(d)
		payload += uint64(f.size)
	}

	ftyp := box("ftyp", []byte("qt  "), u32(0x200), []byte("qt  "))
	moov := mjpegMoov(frames, durations, total, width, height, 0)
	// The chunk offset depends on the header's size, which the offset's
	// own width does not change: build once to measure, again to fill in.
	offset := uint64(len(ftyp)+len(moov)) + 16
	moov = mjpegMoov(frames, durations, total, width, height, offset)

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(out, 1<<20)
	// A 64-bit mdat header, since an hour of stream is larger than 4GB.
	header := cat(ftyp, moov, u32(1), []byte("mdat"), u64(16+payload))
	if _, err := bw.Write(header); err != nil {
		out.Close()
		os.Remove(dest)
		return err
	}
	if _, err := io.CopyN(bw, src, int64(payload)); err != nil {
		out.Close()
		os.Remove(dest)
		return fmt.Errorf("write the recording's frames: %w", err)
	}
	if err := bw.Flush(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func mjpegMoov(frames []mjpegFrame, durations []uint32, total uint64, width, height int, offset uint64) []byte {
	// Consecutive frames of one duration share an entry, as stts has it.
	var stts []byte
	var entries uint32
	for i := 0; i < len(durations); {
		j := i
		for j < len(durations) && durations[j] == durations[i] {
			j++
		}
		stts = append(stts, u32(uint32(j-i))...)
		stts = append(stts, u32(durations[i])...)
		entries++
		i = j
	}
	sizes := make([]byte, 0, 4*len(frames))
	for _, f := range frames {
		sizes = append(sizes, u32(uint32(f.size))...)
	}
	n := uint32(len(frames))
	dur := uint32(total)

	sampleEntry := cat(
		make([]byte, 6), u16(1), // reserved, data reference index
		make([]byte, 16), // version, revision, vendor, temporal and spatial quality
		u16(uint16(width)), u16(uint16(height)),
		u32(0x00480000), u32(0x00480000), // 72 dpi
		u32(0), u16(1), // data size, frame count
		pascal32("Photo - JPEG"),
		u16(24), u16(0xFFFF), // depth, color table id
	)
	stbl := box("stbl",
		fullbox("stsd", 0, u32(1), box("jpeg", sampleEntry)),
		fullbox("stts", 0, u32(entries), stts),
		fullbox("stsc", 0, u32(1), u32(1), u32(n), u32(1)),
		fullbox("stsz", 0, u32(0), u32(n), sizes),
		fullbox("co64", 0, u32(1), u64(offset)),
	)
	minf := box("minf",
		fullbox("vmhd", 1, u16(0), u16(0), u16(0), u16(0)),
		box("dinf", fullbox("dref", 0, u32(1), fullbox("url ", 1))),
		stbl,
	)
	mdia := box("mdia",
		fullbox("mdhd", 0, u32(0), u32(0), u32(mp4Timescale), u32(dur), u16(0x55C4), u16(0)),
		fullbox("hdlr", 0, u32(0), []byte("vide"), make([]byte, 12), []byte("Mobium screen\x00")),
		minf,
	)
	tkhd := fullbox("tkhd", 0x3, u32(0), u32(0), u32(1), u32(0), u32(dur),
		make([]byte, 8), u16(0), u16(0), u16(0), u16(0), identityMatrix(),
		u32(uint32(width)<<16), u32(uint32(height)<<16))
	mvhd := fullbox("mvhd", 0, u32(0), u32(0), u32(mp4Timescale), u32(dur),
		u32(0x00010000), u16(0x0100), make([]byte, 10), identityMatrix(), make([]byte, 24), u32(2))
	return box("moov", mvhd, box("trak", tkhd, mdia))
}

func identityMatrix() []byte {
	return cat(u32(0x00010000), u32(0), u32(0), u32(0), u32(0x00010000), u32(0), u32(0), u32(0), u32(0x40000000))
}

func box(typ string, parts ...[]byte) []byte {
	body := cat(parts...)
	return cat(u32(uint32(8+len(body))), []byte(typ), body)
}

func fullbox(typ string, flags uint32, parts ...[]byte) []byte {
	return box(typ, append([][]byte{u32(flags & 0xFFFFFF)}, parts...)...)
}

// pascal32 is a compressor name: a length byte and the name, in 32 bytes.
func pascal32(s string) []byte {
	b := make([]byte, 32)
	b[0] = byte(copy(b[1:], s))
	return b
}

func cat(parts ...[]byte) []byte {
	var n int
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func u16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func u64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }

// mjpegRecordingDir is where a phone's frames are kept while it records.
func mjpegRecordingDir() string { return filepath.Join(os.TempDir(), "mobium-recordings") }

// StartPhoneScreenRecord records a phone's screen from the runner's stream
// at the tunnel address ip.
func StartPhoneScreenRecord(ctx context.Context, ip string) (Recording, error) {
	return StartMJPEGRecording(ctx, fmt.Sprintf("http://[%s]:%d/", ip, PhoneMJPEGPort), mjpegRecordingDir())
}
