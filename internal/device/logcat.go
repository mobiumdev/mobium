package device

import (
	"bufio"
	"bytes"
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// LogEntry is one line of a device's own log: logcat on Android, the unified
// log on iOS. Distinct from a WebView's console, which is the page's log and
// not the device's.
type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Tag     string    `json:"tag,omitempty"`
	PID     int       `json:"pid,omitempty"`
	Message string    `json:"message"`
	// uid is the Linux UID that logged it, when the format carried one, and
	// -1 when it did not. Used to keep system_server's lines about an app.
	uid int
}

// LogLevels is the device log's vocabulary, least severe first. Both
// platforms' levels fold into it: logcat's V/D/I/W/E/F, and the unified log's
// Debug/Info/Default/Error/Fault.
var LogLevels = []string{"verbose", "debug", "info", "warn", "error", "fatal"}

// LogQuery narrows a device log read.
type LogQuery struct {
	// App keeps only what one app logged. Empty is the whole device.
	App string
	// After keeps only entries strictly later than this, by the device's own
	// clock. Zero means no lower bound.
	After time.Time
	// Level keeps only entries at exactly this level. Empty keeps all.
	Level string
	// Limit keeps the most recent entries, after the other filters.
	Limit int
}

// LogResult is a device log read, with anything the caller must know about
// how far the filter can be trusted.
type LogResult struct {
	Entries []LogEntry
	// Skipped counts entries that matched but were older than the limit
	// kept. They are not coming back: the next read starts after the newest
	// entry returned, so a caller told nothing would take a burst of noise
	// for a quiet device and miss the line it was waiting for.
	Skipped int
	// Note qualifies the result in a sentence, or is empty.
	Note string
}

// logcatBuffers are the ones an app's own output and its crash land in.
// `events` is binary-tagged system bookkeeping and `kernel` is not the app's.
const logcatBuffers = "main,system,crash"

// firstAppUID is the first UID Android gives an installed app. Below it are
// the system's shared UIDs — Settings runs as 1000, alongside system_server —
// so filtering by one of those is filtering by the system, not by the app.
const firstAppUID = 10000

// logcatLineRe reads `-v epoch` and `-v epoch,uid`: seconds.millis, the UID
// when present, pid, tid, level, tag, message. The UID is a number for apps
// and the system, and a name for a few ("root", "shell", "radio") — measured
// on API 35.
var logcatLineRe = regexp.MustCompile(`^\s*(\d+)\.(\d{3})\s+(?:(\S+)\s+)?(\d+)\s+\d+\s+([VDIWEFA])\s+(.*?)\s*: (.*)$`)

var logcatLevels = map[string]string{
	"V": "verbose", "D": "debug", "I": "info", "W": "warn", "E": "error", "F": "fatal", "A": "fatal",
}

// Logcat reads the device log.
//
// The time bound is applied here, never trusted to logcat: `-T` with a time
// later than every entry still prints the most recent line, measured on
// API 35, so a read meant to return "nothing new" would return the last thing
// again. `-T` is passed only to keep the transfer small; the filter below is
// what decides.
//
// An app is filtered by UID rather than PID, because a crash restarts the
// process under a new PID and the lines worth reading are on both sides of it.
func (a *ADB) Logcat(ctx context.Context, q LogQuery) (LogResult, error) {
	var res LogResult
	args := []string{"logcat", "-d", "-v", "epoch,uid", "-b", logcatBuffers}
	if !q.After.IsZero() {
		args = append(args, "-T", logcatTime(q.After))
	}
	appUID := -1
	if q.App != "" {
		uid, err := a.AppUID(ctx, q.App)
		if err != nil {
			return res, err
		}
		appUID = uid
		if uid < firstAppUID {
			res.Note = q.App + " shares UID " + strconv.Itoa(uid) + " with the system, " +
				"so these are the system's lines as well as its own — Android cannot tell them apart"
			args = append(args, "--uid="+strconv.Itoa(uid))
		} else {
			// The system's UID too: an app's ANR is logged by system_server
			// ("ANR in dev.example"), not by the app, so a read narrowed to
			// the app's UID alone returned none of the lines saying it hung —
			// measured, zero, on the emulator. Only system_server's lines that
			// name the package are kept; see keepForApp.
			args = append(args, "--uid="+strconv.Itoa(uid)+","+strconv.Itoa(systemUID))
		}
	}
	out, err := a.Shell(ctx, args...)
	if err != nil {
		return res, err
	}
	entries := parseLogcat(out)
	if appUID >= firstAppUID {
		entries = keepForApp(entries, q.App, appUID, a.systemServerPID(ctx))
	}
	res.Entries, res.Skipped = filterLog(entries, q)
	return res, nil
}

// systemUID is the UID system_server runs as.
const systemUID = 1000

// systemServerPID is system_server's pid, or 0 when it cannot be read.
func (a *ADB) systemServerPID(ctx context.Context) int {
	out, err := a.Shell(ctx, "pidof", "system_server")
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return pid
}

// keepForApp narrows a read to the app's own lines and system_server's lines
// about it. Everything else sharing UID 1000 — Settings, other system apps —
// is dropped, and so is system_server's chatter about other packages, and
// any other UID. With no pid for system_server, only the app's lines are
// kept: over-including would be the quieter failure.
func keepForApp(entries []LogEntry, pkg string, appUID, serverPID int) []LogEntry {
	kept := entries[:0]
	for _, e := range entries {
		own := e.uid == appUID
		about := e.uid == systemUID && serverPID != 0 && e.PID == serverPID && strings.Contains(e.Message, pkg)
		if own || about {
			kept = append(kept, e)
		}
	}
	return kept
}

// logcatTime formats a time the way `-T` accepts it with `-v epoch`.
func logcatTime(t time.Time) string {
	ms := t.UnixMilli()
	return strconv.FormatInt(ms/1000, 10) + "." + strconv.FormatInt(1000+ms%1000, 10)[1:]
}

func parseLogcat(out []byte) []LogEntry {
	var entries []LogEntry
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		m := logcatLineRe.FindStringSubmatch(strings.TrimRight(sc.Text(), "\r"))
		if m == nil {
			continue // "--------- beginning of main", and anything else that is not a line
		}
		sec, _ := strconv.ParseInt(m[1], 10, 64)
		ms, _ := strconv.ParseInt(m[2], 10, 64)
		pid, _ := strconv.Atoi(m[4])
		entries = append(entries, LogEntry{
			Time:    time.UnixMilli(sec*1000 + ms).UTC(),
			Level:   logcatLevels[m[5]],
			Tag:     m[6],
			PID:     pid,
			Message: m[7],
			uid:     logcatUID(m[3]),
		})
	}
	return entries
}

// logcatUID reads the UID column: -1 when absent, a number, or one of the
// names logcat prints instead of a number.
func logcatUID(s string) int {
	if s == "" {
		return -1
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	switch s {
	case "root":
		return 0
	case "system":
		return systemUID
	}
	return -2 // some other named account; never the app's, never system_server's
}

// filterLog applies the time and level bounds and the limit, and reports how
// many the limit dropped. Shared by both platforms, so "strictly after" means
// the same thing on each.
func filterLog(entries []LogEntry, q LogQuery) (kept []LogEntry, skipped int) {
	kept = entries[:0]
	for _, e := range entries {
		if !q.After.IsZero() && !e.Time.After(q.After) {
			continue
		}
		if q.Level != "" && e.Level != q.Level {
			continue
		}
		kept = append(kept, e)
	}
	// logcat interleaves its buffers by time already; the unified log does
	// too. Sorted anyway, stably, because the limit keeps "the most recent"
	// and that is only true of a sorted list.
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Time.Before(kept[j].Time) })
	if q.Limit > 0 && len(kept) > q.Limit {
		skipped = len(kept) - q.Limit
		kept = kept[skipped:]
	}
	return kept, skipped
}

// AppUID is the Linux UID an installed app runs as.
//
// `pm list packages -U <name>` matches by substring, so asking for
// com.android.settings also returns com.android.settings.auto_generated_rro_…
// under a different UID; only the exact name is taken.
func (a *ADB) AppUID(ctx context.Context, pkg string) (int, error) {
	out, err := a.Shell(ctx, "pm", "list", "packages", "-U", shellQuote(pkg))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		name, uid, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "package:")), " uid:")
		if ok && name == pkg {
			// A package installed for several users lists "uid:10213,1010213".
			first, _, _ := strings.Cut(uid, ",")
			if n, err := strconv.Atoi(strings.TrimSpace(first)); err == nil {
				return n, nil
			}
		}
	}
	return 0, mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed on this device — `mobium apps` lists what is", pkg)
}
