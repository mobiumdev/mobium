package device

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A real iPhone's log is lockdown's com.apple.syslog_relay: a live stream,
// with no history and no way to ask for any. So it is captured from the start
// of the session into a bounded buffer, and a read drains that — the same
// shape as a WebView's console, whose capture also starts when Mobium
// attaches.
//
// Measured on an iPhone 15 Plus on iOS 26.6.2: about 740 lines a second at
// rest, 140KB/s, as NUL-separated records of
//
//	Sep 25 11:14:59 <host> maild(libxpc.dylib)[3153] <Notice>: message
//
// The phone's own stamp is whole seconds with no year, which cannot order two
// lines in one second and is ambiguous across New Year. So each line is
// stamped when Mobium receives it, strictly increasing — a stream read live
// is behind the phone by milliseconds, and the stamps only have to order
// lines and mark reads.

// phoneLogCap bounds the buffer: a little over two minutes at the rate
// measured at rest.
const phoneLogCap = 100_000

// syslogRecordRe reads one relay record. The image in parentheses is the
// library that logged, when it is not the executable itself.
var syslogRecordRe = regexp.MustCompile(`(?s)^[A-Z][a-z]{2} [ \d]\d \d\d:\d\d:\d\d \S+ ([^\[(]+?)(?:\(([^)]*)\))?\[(\d+)\] <([A-Za-z]+)>: (.*)$`)

var syslogLevels = map[string]string{
	"Debug": "debug", "Info": "info", "Notice": "info", "Warning": "warn",
	"Error": "error", "Fault": "fatal", "Critical": "fatal", "Alert": "fatal", "Emergency": "fatal",
}

// phoneLogEntry is a captured line with the process it came from, which an
// app filter needs and LogEntry does not carry.
type phoneLogEntry struct {
	LogEntry
	process string
}

// PhoneLog captures a phone's syslog relay for the life of a session.
type PhoneLog struct {
	udid string
	// dial opens the relay. A field so a test can stand in for a phone that
	// hangs up, which is the one path otherwise reachable only by pulling
	// the cable.
	dial func(ctx context.Context) (net.Conn, error)

	mu      sync.Mutex
	entries []phoneLogEntry
	last    time.Time
	// evictedUpTo is the stamp of the newest line pushed out of the full
	// buffer. A read whose mark is older has lost lines it can count only
	// as "some".
	evictedUpTo time.Time
	// stopped is why capture ended, when it has.
	stopped error
	conn    net.Conn
	closed  bool
	// gray hears every line as it is captured, when a gray-box app runs.
	gray *GrayBox
}

// StartPhoneLog connects to the phone's syslog relay and starts capturing.
func StartPhoneLog(ctx context.Context, udid string) (*PhoneLog, error) {
	p := &PhoneLog{udid: udid}
	p.dial = func(ctx context.Context) (net.Conn, error) {
		return LockdownService(ctx, udid, "com.apple.syslog_relay")
	}
	if err := p.connect(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *PhoneLog) connect(ctx context.Context) error {
	conn, err := p.dial(ctx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.conn, p.stopped = conn, nil
	p.mu.Unlock()
	go p.capture(conn)
	return nil
}

func (p *PhoneLog) capture(conn net.Conn) {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, 0); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	for sc.Scan() {
		e, ok := parseSyslogRecord(sc.Bytes())
		if !ok {
			continue
		}
		p.add(e)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.stopped = sc.Err()
	if p.stopped == nil {
		p.stopped = mobiumerr.New(mobiumerr.DeviceServer, "the phone closed the log stream")
	}
}

func (p *PhoneLog) add(e phoneLogEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	if !now.After(p.last) {
		now = p.last.Add(time.Nanosecond)
	}
	p.last = now
	e.Time = now
	if len(p.entries) >= phoneLogCap {
		drop := len(p.entries) / 10
		p.evictedUpTo = p.entries[drop-1].Time
		p.entries = append(p.entries[:0], p.entries[drop:]...)
	}
	p.entries = append(p.entries, e)
	if p.gray != nil {
		p.gray.Feed(e.Message, now)
	}
}

// FeedGrayBox passes every line captured from now on to g as it arrives,
// which is how a gray-box wait hears the app within milliseconds instead of
// on the next read.
func (p *PhoneLog) FeedGrayBox(g *GrayBox) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gray = g
}

func parseSyslogRecord(rec []byte) (phoneLogEntry, bool) {
	m := syslogRecordRe.FindSubmatch(bytes.TrimRight(rec, "\n"))
	if m == nil {
		return phoneLogEntry{}, false
	}
	pid, _ := strconv.Atoi(string(m[3]))
	process := strings.TrimSpace(string(m[1]))
	tag := string(m[2])
	if tag == "" {
		tag = process
	}
	level, ok := syslogLevels[string(m[4])]
	if !ok {
		level = "info"
	}
	return phoneLogEntry{
		LogEntry: LogEntry{Level: level, Tag: tag, PID: pid, Message: unvis(string(m[5]))},
		process:  process,
	}, true
}

// Read returns captured lines after q.After for the process exe, or every
// process when exe is empty. A capture that stopped — the phone was
// unplugged, or locked long enough to drop the stream — is restarted, and the
// result says there is a gap.
func (p *PhoneLog) Read(ctx context.Context, q LogQuery, exe string) (LogResult, error) {
	var res LogResult
	if q.Level == "verbose" || q.Level == "warn" {
		// As on the simulator, whose unified log this relay carries: no such
		// level, and an empty answer would claim nothing was logged at it.
		return res, mobiumerr.New(mobiumerr.InvalidArgument, "the iOS unified log has no %q level — "+
			"use debug, info, error or fatal", q.Level)
	}
	p.mu.Lock()
	stopped := p.stopped
	p.mu.Unlock()
	if stopped != nil {
		if err := p.connect(ctx); err != nil {
			return res, mobiumerr.New(mobiumerr.DeviceServer, "the phone's log stopped (%v) and could not be restarted: %v", stopped, err)
		}
		res.Note = fmt.Sprintf("capture had stopped (%v) and was restarted just now, so lines from the gap are missing", stopped)
	}

	p.mu.Lock()
	var matched []LogEntry
	for _, e := range p.entries {
		if exe == "" || e.process == exe ||
			(e.process == lifecycleProcess && strings.Contains(e.Message, "app<"+q.App)) {
			matched = append(matched, e.LogEntry)
		}
	}
	lost := !q.After.IsZero() && q.After.Before(p.evictedUpTo)
	p.mu.Unlock()

	res.Entries, res.Skipped = filterLog(matched, q)
	if lost {
		note := "the capture buffer filled since the last read, so some older lines were dropped before this one"
		if res.Note != "" {
			note = res.Note + "; " + note
		}
		res.Note = note
	}
	return res, nil
}

// Close stops capturing.
func (p *PhoneLog) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.conn != nil {
		p.conn.Close()
	}
}

// unvis decodes the BSD vis(3) escaping the relay applies to anything outside
// printable ASCII: \M-x is x with the high bit set, \M^x and \^x are control
// characters, with and without it, and \\ is a backslash. A curly quote
// arrives as \M-b\M^@\M^Y; decoded, the bytes are UTF-8 again.
func unvis(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b = append(b, c)
			continue
		}
		switch {
		case s[i+1] == '\\':
			b = append(b, '\\')
			i++
		case strings.HasPrefix(s[i+1:], "M-") && i+3 < len(s):
			b = append(b, s[i+3]|0x80)
			i += 3
		case strings.HasPrefix(s[i+1:], "M^") && i+3 < len(s):
			b = append(b, ctrl(s[i+3])|0x80)
			i += 3
		case s[i+1] == '^' && i+2 < len(s):
			b = append(b, ctrl(s[i+2]))
			i += 2
		default:
			b = append(b, c)
		}
	}
	return string(b)
}

func ctrl(c byte) byte {
	if c == '?' {
		return 0x7f
	}
	return c & 0x1f
}

// PhoneExecutable is the name an installed app's process runs under, from
// lockdown's installation service. System apps are included: they are what
// the phone's log is mostly about.
func PhoneExecutable(ctx context.Context, udid, bundleID string) (string, error) {
	conn, err := LockdownService(ctx, udid, "com.apple.mobile.installation_proxy")
	if err != nil {
		return "", err
	}
	defer conn.Close()
	m, err := lockdownCall(conn, map[string]any{"Command": "Lookup", "ClientOptions": map[string]any{
		"ApplicationType":  "Any",
		"BundleIDs":        []any{bundleID},
		"ReturnAttributes": []any{"CFBundleIdentifier", "CFBundleExecutable"},
	}})
	if err != nil {
		return "", err
	}
	found, _ := m["LookupResult"].(map[string]any)
	app, _ := found[bundleID].(map[string]any)
	exe, _ := app["CFBundleExecutable"].(string)
	if exe == "" {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed on this iPhone — `mobium apps` lists what is", bundleID)
	}
	return exe, nil
}
