package device

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// CrashReport is one crash the device recorded: a Java exception, a native
// signal, or an app that stopped responding.
type CrashReport struct {
	// ID names the report for a later read. Opaque to the caller; on Android
	// it is the dropbox entry, tag@epoch-millis.
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	// Kind is "crash", "native_crash" or "anr".
	Kind string `json:"kind"`
	// App is the process that crashed, which is the package for an app.
	App string `json:"app,omitempty"`
	// Summary is the one line worth reading first: the exception, the abort
	// message or signal, or the ANR's reason.
	Summary string `json:"summary,omitempty"`
	// Text is the whole report. Only a single-report read fills it.
	Text string `json:"text,omitempty"`
	// Dropped counts crashes of the same process Android declined to record
	// before this one. Android rate-limits crash records per process: on
	// the emulator, after ten records for MobiumApp in nine minutes, two
	// more crashes — the app visibly died both times, and Android showed
	// "keeps stopping" — produced no record at all. They are counted here,
	// on the next record it does write, and nowhere else.
	Dropped int `json:"dropped,omitempty"`
}

// crashTags are the dropbox tags that record a crash, and what kind each is.
//
// Dropbox, not logcat's crash buffer, because dropbox survives a reboot and
// keeps every entry under its own id — measured on API 35, it still held
// crashes from three days and several boots earlier. The crash buffer is a
// ring that anything noisy can overrun. SYSTEM_TOMBSTONE is left out: it is
// the full dump behind a native_crash entry that already names the signal.
var crashTags = map[string]string{
	"data_app_crash":          "crash",
	"system_app_crash":        "crash",
	"system_server_crash":     "crash",
	"data_app_native_crash":   "native_crash",
	"system_app_native_crash": "native_crash",
	"data_app_anr":            "anr",
	"system_app_anr":          "anr",
	"system_server_anr":       "anr",
}

// dropboxSeparator opens every entry `dumpsys dropbox -p` prints.
const dropboxSeparator = "========================================"

var dropboxPathRe = regexp.MustCompile(`^/data/system/dropbox/([A-Za-z_]+)@(\d+)\.`)

// Crashes lists the crashes the device recorded, newest first. An empty app
// lists every process's.
//
// Dropbox's search terms combine with AND — measured: a tag and the time of
// an entry under a different tag match nothing — so each tag is its own
// search, all of them in one shell round trip.
func (a *ADB) Crashes(ctx context.Context, app string) ([]CrashReport, error) {
	tags := make([]string, 0, len(crashTags))
	for t := range crashTags {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	out, err := a.Shell(ctx, "for t in "+strings.Join(tags, " ")+"; do dumpsys dropbox -p -f $t; done")
	if err != nil {
		return nil, err
	}
	var list []CrashReport
	for _, r := range parseDropbox(string(out)) {
		if app != "" && r.App != app {
			continue
		}
		r.Text = ""
		list = append(list, r)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Time.After(list[j].Time) })
	return list, nil
}

// Crash reads one crash report in full.
func (a *ADB) Crash(ctx context.Context, id string) (CrashReport, error) {
	tag, ms, ok := strings.Cut(id, "@")
	if _, known := crashTags[tag]; !ok || !known || strings.Trim(ms, "0123456789") != "" || ms == "" {
		return CrashReport{}, mobiumerr.New(mobiumerr.InvalidArgument,
			"%q is not a crash id — use one from `mobium crashes`, which look like data_app_crash@1790357773461", id)
	}
	out, err := a.Shell(ctx, "dumpsys", "dropbox", "-p", "-f", tag)
	if err != nil {
		return CrashReport{}, err
	}
	for _, r := range parseDropbox(string(out)) {
		if r.ID == id {
			if r.Kind == "anr" {
				r.Text = anrLead(r.Summary, r.Text) + r.Text
			}
			return r, nil
		}
	}
	return CrashReport{}, mobiumerr.New(mobiumerr.InvalidArgument,
		"no crash %s on this device — dropbox keeps a bounded number of entries, so an old one "+
			"may have aged out; `mobium crashes` lists what is there now", id)
}

// parseDropbox reads `dumpsys dropbox -p -f` output: each entry is the
// separator, a date line, the file's path — which carries the id — and the
// report, whose own header lines run up to the first blank line.
func parseDropbox(out string) []CrashReport {
	var list []CrashReport
	for _, block := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), dropboxSeparator+"\n")[1:] {
		lines := strings.Split(block, "\n")
		if len(lines) < 3 {
			continue
		}
		m := dropboxPathRe.FindStringSubmatch(strings.TrimSpace(lines[1]))
		if m == nil {
			continue
		}
		kind, ok := crashTags[m[1]]
		if !ok {
			continue
		}
		ms, _ := strconv.ParseInt(m[2], 10, 64)
		body := strings.TrimRight(strings.Join(lines[2:], "\n"), "\n")
		r := CrashReport{
			ID:   m[1] + "@" + m[2],
			Time: time.UnixMilli(ms).UTC(),
			Kind: kind,
			Text: body,
		}
		r.App, r.Summary = crashHeadline(kind, body)
		r.Dropped = droppedCount(body)
		if r.App == "" && strings.HasPrefix(m[1], "system_server") {
			r.App = "system_server"
		}
		list = append(list, r)
	}
	return list
}

// droppedCount reads the header's Dropped-Count, which is how Android says
// it skipped recording earlier crashes of this process.
func droppedCount(body string) int {
	header, _, _ := strings.Cut(body, "\n\n")
	for _, line := range strings.Split(header, "\n") {
		if v, ok := strings.CutPrefix(line, "Dropped-Count: "); ok {
			n, _ := strconv.Atoi(strings.TrimSpace(v))
			return n
		}
	}
	return 0
}

// anrLead is what to read first in an ANR: why it happened, and where the
// main thread was stuck.
//
// An ANR is not a crash — the app is frozen, not gone — and the question it
// raises is what the main thread was doing. Android's own ANR entry answers
// it without root: on a Pixel 8 Pro the dropbox entry carried the whole
// thread dump, the same content as the /data/anr trace, which is otherwise
// what a bugreport.zip is for. But it is 1,582 lines, with the main thread
// at line 73, so this puts that thread first. A process that could not be
// asked for its stacks — one stopped with SIGSTOP, measured on the emulator
// — leaves no "main" section at all, and that is said instead of implied.
func anrLead(summary, body string) string {
	var b strings.Builder
	b.WriteString("ANR: " + summary + "\n\n")
	if i := strings.Index(body, "\n\"main\" "); i >= 0 {
		main := body[i+1:]
		if j := strings.Index(main, "\n\n"); j >= 0 {
			main = main[:j]
		}
		b.WriteString("Main thread:\n" + main + "\n")
	} else {
		b.WriteString("No thread stacks: the process did not answer the request to dump them, " +
			"as a stopped or wedged process cannot.\n")
	}
	b.WriteString("\n----- the full report -----\n")
	return b.String()
}

// crashHeadline finds the process and the line worth reading first.
func crashHeadline(kind, body string) (app, summary string) {
	header, detail, _ := strings.Cut(body, "\n\n")
	for _, line := range strings.Split(header, "\n") {
		if v, ok := strings.CutPrefix(line, "Process: "); ok {
			app = strings.TrimSpace(v)
		}
	}
	if kind == "anr" {
		// The reason is a Subject: line, which Android 17 puts well below
		// the header — after a blank line and the memory, CPU and I/O
		// pressure dumps, at line 47 of a real ANR on a Pixel 8 Pro. Looking
		// only in the header summarized that ANR with a pressure heading.
		for _, line := range strings.Split(body, "\n") {
			if v, ok := strings.CutPrefix(line, "Subject: "); ok {
				return app, strings.TrimSpace(v)
			}
		}
	}
	var signal string
	for _, line := range strings.Split(detail, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case kind == "native_crash" && strings.HasPrefix(t, "Abort message: "):
			return app, t
		case kind == "native_crash" && signal == "" && strings.HasPrefix(t, "signal "):
			signal = t
		case kind != "native_crash" && t != "":
			return app, t
		}
	}
	return app, signal
}
