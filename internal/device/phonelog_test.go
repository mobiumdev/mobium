package device

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// Six records in the shape an iPhone 15 Plus on iOS 26.6.2 sends through
// com.apple.syslog_relay: the kernel, a vis-escaped message, one spanning
// several lines, and each level seen. The escaped and multi-line records are
// synthetic, in the captured form, so no real message content is kept.
func TestParseSyslogRecordsFromARealCapture(t *testing.T) {
	raw, err := os.ReadFile("testdata/syslog-relay-ios26.bin")
	if err != nil {
		t.Fatal(err)
	}
	var got []phoneLogEntry
	for _, rec := range bytes.Split(bytes.TrimRight(raw, "\x00"), []byte{0}) {
		e, ok := parseSyslogRecord(rec)
		if !ok {
			t.Errorf("did not parse %q", rec[:min(len(rec), 80)])
			continue
		}
		got = append(got, e)
	}
	if len(got) != 6 {
		t.Fatalf("parsed %d of 6", len(got))
	}
	kernel := got[0]
	if kernel.process != "kernel" || kernel.Tag != "Sandbox" || kernel.PID != 0 || kernel.Level != "error" {
		t.Errorf("kernel = %+v", kernel)
	}
	// \M-b\M^P\M^M is U+240D, the symbol for a carriage return, in UTF-8.
	if !strings.HasSuffix(got[1].Message, "line one␍␊'") {
		t.Errorf("vis escapes not decoded: %q", got[1].Message)
	}
	if !strings.Contains(got[2].Message, "\n    19459,") {
		t.Errorf("a multi-line record lost its lines: %q", got[2].Message)
	}
	if got[3].Level != "fatal" || got[4].Level != "error" || got[5].Level != "info" {
		t.Errorf("levels = %s %s %s", got[3].Level, got[4].Level, got[5].Level)
	}
	// No image in parentheses: the tag is the process.
	if got[5].process != "abm-helper" || got[5].Tag != "abm-helper" || got[5].PID != 4101 {
		t.Errorf("abm-helper = %+v", got[5])
	}
}

func TestUnvis(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:        "plain",
		`a\\b`:         `a\b`,
		`\M-b\M^@\M^Y`: "’",
		`tab\^Iend`:    "tab\tend",
		`del\^?`:       "del\x7f",
		`trailing\`:    `trailing\`,
		`caf\M-C\M-)`:  "caf\u00e9",
	} {
		if got := unvis(in); got != want {
			t.Errorf("unvis(%q) = %q, want %q", in, got, want)
		}
	}
}

// The buffer stamps lines as they arrive, strictly increasing, so a read's
// mark can never fall between two lines that share a stamp; and when it
// fills past a mark, the read says lines were lost rather than pretending.
func TestPhoneLogBufferMarksAndOverflow(t *testing.T) {
	p := &PhoneLog{}
	for i := 0; i < 5; i++ {
		p.add(phoneLogEntry{LogEntry: LogEntry{Level: "info", Message: "x"}, process: "a"})
	}
	for i := 1; i < len(p.entries); i++ {
		if !p.entries[i].Time.After(p.entries[i-1].Time) {
			t.Fatal("stamps are not strictly increasing")
		}
	}
	res, _ := p.Read(context.Background(), LogQuery{After: p.entries[2].Time}, "")
	if len(res.Entries) != 2 {
		t.Errorf("after the third line: %d entries", len(res.Entries))
	}
	if res, _ := p.Read(context.Background(), LogQuery{}, "b"); len(res.Entries) != 0 {
		t.Errorf("a process filter kept %d", len(res.Entries))
	}

	mark := p.entries[len(p.entries)-1].Time
	for i := 0; i < phoneLogCap; i++ {
		p.add(phoneLogEntry{LogEntry: LogEntry{Level: "info"}, process: "a"})
	}
	res, _ = p.Read(context.Background(), LogQuery{After: mark, Limit: 10}, "")
	if !strings.Contains(res.Note, "dropped") {
		t.Errorf("an overflow past the mark went unmentioned: %q", res.Note)
	}
}

// A fake phone at the other end of a pipe, answering the AFC requests a
// crash listing makes, so the framing is checked against the protocol as
// written rather than against itself.
func TestAFCAgainstAFakePhone(t *testing.T) {
	client, phone := net.Pipe()
	defer client.Close()
	files := map[string]string{"/a.ips": "0123456789"}
	go func() {
		defer phone.Close()
		for {
			head := make([]byte, 40)
			if _, err := io.ReadFull(phone, head); err != nil {
				return
			}
			total := binary.LittleEndian.Uint64(head[8:])
			op := binary.LittleEndian.Uint64(head[32:])
			args := make([]byte, total-40)
			if _, err := io.ReadFull(phone, args); err != nil {
				return
			}
			reply := func(op uint64, body []byte) {
				h := make([]byte, 40)
				copy(h, afcMagic)
				binary.LittleEndian.PutUint64(h[8:], uint64(40+len(body)))
				binary.LittleEndian.PutUint64(h[16:], 40)
				binary.LittleEndian.PutUint64(h[32:], op)
				_, _ = phone.Write(append(h, body...))
			}
			status := func(code uint64) {
				b := make([]byte, 8)
				binary.LittleEndian.PutUint64(b, code)
				reply(afcOpStatus, b)
			}
			switch op {
			case afcOpReadDir:
				reply(afcOpData, []byte(".\x00..\x00a.ips\x00Retired\x00"))
			case afcOpGetFileInfo:
				reply(afcOpData, []byte("st_size\x0010\x00st_ifmt\x00S_IFREG\x00st_mtime\x001790357773000000000\x00"))
			case afcOpFileOpen:
				if _, ok := files[strings.TrimRight(string(args[8:]), "\x00")]; !ok {
					status(8)
					continue
				}
				reply(afcOpFileOpenRes, []byte{7, 0, 0, 0, 0, 0, 0, 0})
			case afcOpFileRead:
				want := binary.LittleEndian.Uint64(args[8:])
				data := files["/a.ips"]
				if uint64(len(data)) > want {
					data = data[:want]
				}
				files["/a.ips"] = files["/a.ips"][len(data):]
				reply(afcOpData, []byte(data))
			case afcOpFileClose:
				status(0)
			}
		}
	}()

	a := &afcClient{conn: client}
	names, err := a.ReadDir("/")
	if err != nil || strings.Join(names, ",") != "a.ips,Retired" {
		t.Fatalf("ReadDir = %v, %v", names, err)
	}
	info, err := a.Stat("/a.ips")
	if err != nil || info.Dir || info.Size != 10 || !info.MTime.Equal(time.Unix(1790357773, 0)) {
		t.Errorf("Stat = %+v, %v", info, err)
	}
	head, err := a.ReadPrefix("/a.ips", 4)
	if err != nil || string(head) != "0123" {
		t.Errorf("ReadPrefix = %q, %v", head, err)
	}
	if _, err := a.ReadFile("/missing.ips", 100); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("a missing file: %v", err)
	}
}

// When the phone hangs up — unplugged, or the relay dropped — capture stops,
// and the next read reconnects and says lines from the gap are missing,
// rather than serving the old buffer as if it were current. The phone that
// hangs up is a pipe here; on a real one this path needs a cable pulled.
func TestPhoneLogRestartsAfterTheStreamDrops(t *testing.T) {
	dials := 0
	var phones []net.Conn
	p := &PhoneLog{}
	p.dial = func(ctx context.Context) (net.Conn, error) {
		dials++
		host, phone := net.Pipe()
		phones = append(phones, phone)
		return host, nil
	}
	if err := p.connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	record := "Sep 25 11:14:59 iPhone abm-helper[4101] <Notice>: before the drop\n\x00"
	if _, err := phones[0].Write([]byte(record)); err != nil {
		t.Fatal(err)
	}
	phones[0].Close() // the phone hangs up

	deadline := time.Now().Add(2 * time.Second)
	for {
		p.mu.Lock()
		stopped := p.stopped
		p.mu.Unlock()
		if stopped != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a dropped stream was never noticed")
		}
		time.Sleep(10 * time.Millisecond)
	}

	res, err := p.Read(context.Background(), LogQuery{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if dials != 2 {
		t.Errorf("dialed %d times, want a reconnect", dials)
	}
	if !strings.Contains(res.Note, "restarted") || !strings.Contains(res.Note, "missing") {
		t.Errorf("the gap went unmentioned: %q", res.Note)
	}
	if len(res.Entries) != 1 || res.Entries[0].Message != "before the drop" {
		t.Errorf("entries = %+v", res.Entries)
	}
	p.Close()
	for _, c := range phones {
		c.Close()
	}
}
