package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// defaultLogLines bounds a first read, which has no mark to start from. A
// busy device writes thousands of lines a minute, and an agent pays for every
// one it is handed.
const defaultLogLines = 100

// deviceLogsOn is app_logs with source "device": the device's own log.
//
// It keeps the WebView console's contract — each read reports what arrived
// since the last one — by remembering the newest entry it handed out, by the
// device's clock rather than the host's, so a skewed emulator clock cannot
// hide lines or repeat them. The first read has nothing to start from and
// returns the most recent lines instead.
//
// One thing the mark cannot promise: a line stamped with the same millisecond
// as the newest one already returned, arriving after that read, is taken as
// already read. logcat stamps in milliseconds, so this needs two lines in one
// millisecond straddling a read.
func (h *Handlers) deviceLogsOn(ctx context.Context, s *session, args map[string]interface{}) (*ToolsCallResult, error) {
	logs, ok := mobiumdriver.AsDeviceLogs(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapDeviceLogs, "read the device log")
	}
	level := strings.ToLower(stringArg(args, "level"))
	if level != "" && !slices.Contains(device.LogLevels, level) {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "unknown device log level %q (want one of %s)",
			level, strings.Join(device.LogLevels, ", "))
	}
	lines := intArgOr(args, "lines", defaultLogLines)
	if lines < 1 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "lines must be at least 1, got %d", lines)
	}
	app := stringArg(args, "app")

	key := app + "|" + level
	// No locking: Call holds h.mu for the whole of every tool call.
	q := device.LogQuery{App: app, Level: level, Limit: lines, After: s.logMarks[key]}

	res, err := logs.DeviceLogs(ctx, q)
	if err != nil {
		return nil, err
	}
	if n := len(res.Entries); n > 0 {
		if s.logMarks == nil {
			s.logMarks = map[string]time.Time{}
		}
		s.logMarks[key] = res.Entries[n-1].Time
	}

	// Never null on the wire: an empty read is an empty list, which every
	// client can iterate, where null makes each one special-case it.
	if res.Entries == nil {
		res.Entries = []device.LogEntry{}
	}
	view := DeviceLogView{Entries: res.Entries, App: app, Note: res.Note,
		First: q.After.IsZero(), Skipped: res.Skipped, Device: s.dev.Serial}
	var b strings.Builder
	if res.Note != "" {
		b.WriteString(res.Note + "\n")
	}
	if len(res.Entries) == 0 {
		// "since the last read", never "nothing was logged": the mark moves
		// on every read that returns something, so the difference is the
		// whole meaning of an empty answer. A first read says "recently",
		// because how far back it looked is the platform's: all of logcat's
		// ring on Android, the last few seconds on iOS.
		msg := "nothing logged since the last read"
		if q.After.IsZero() {
			msg = "nothing logged recently"
		}
		if app != "" {
			msg += " by " + app
		}
		if level != "" {
			msg += fmt.Sprintf(" at level %q", level)
		}
		b.WriteString(msg)
		return Result(b.String(), view), nil
	}
	switch {
	case q.After.IsZero() && res.Skipped > 0:
		fmt.Fprintf(&b, "the most recent %s — later reads return only what is new\n", countOf(lines, "line"))
	case res.Skipped > 0:
		// Said first and plainly: these lines are gone for good, because
		// the next read starts after the newest one here.
		fmt.Fprintf(&b, "%d earlier lines since the last read were skipped to keep the newest %d — "+
			"narrow with app or level, or raise lines, to see them\n", res.Skipped, lines)
	}
	for i, e := range res.Entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s %s %s: %s", e.Time.Format("15:04:05.000"), levelLetter(e.Level), e.Tag, e.Message)
	}
	return Result(b.String(), view), nil
}

// DeviceLogView is the result of app_logs reading the device log.
type DeviceLogView struct {
	Entries []device.LogEntry `json:"entries"`
	App     string            `json:"app,omitempty"`
	// First is true when no earlier read returned a line to continue from,
	// so the entries are the most recent ones rather than everything new. An
	// empty read leaves it true: a mark is a line's stamp, and an empty read
	// has none to give — the device's clock is not the host's, so "now" is
	// not a mark either.
	First bool `json:"first"`
	// Skipped counts newer-than-the-last-read entries dropped by the limit.
	// They will not be returned by a later read.
	Skipped int    `json:"skipped,omitempty"`
	Note    string `json:"note,omitempty"`
	Device  string `json:"device"`
}

func levelLetter(level string) string {
	if level == "" {
		return "?"
	}
	return strings.ToUpper(level[:1])
}

// defaultCrashLimit bounds a crash listing. Dropbox keeps hundreds of
// entries, and the ones worth reading are the latest.
const defaultCrashLimit = 20

// crashes is app_crashes: the crashes the device recorded, or one in full.
//
// An agent that drives an app into a crash and can then read why is doing
// something different in kind from one that reports a tap failed. The listing
// is not drained: a crash is a record, not a stream, and asking twice should
// show the same crash twice.
func (h *Handlers) crashes(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	reports, ok := mobiumdriver.AsCrashReports(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapCrashes, "read crash reports")
	}

	if id := stringArg(args, "id"); id != "" {
		if stringArg(args, "app") != "" {
			return nil, mobiumerr.New(mobiumerr.InvalidArgument, "pass id or app, not both — an id already names one report")
		}
		r, err := reports.Crash(ctx, id)
		if err != nil {
			return nil, err
		}
		return Result(r.Text, CrashesView{Crashes: []device.CrashReport{r}, Device: s.dev.Serial}), nil
	}

	limit := intArgOr(args, "limit", defaultCrashLimit)
	if limit < 1 {
		return nil, mobiumerr.New(mobiumerr.InvalidArgument, "limit must be at least 1, got %d", limit)
	}
	app := stringArg(args, "app")
	list, err := reports.Crashes(ctx, app)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []device.CrashReport{}
	}
	total := len(list)
	if total > limit {
		list = list[:limit]
	}
	view := CrashesView{Crashes: list, App: app, Total: total, Device: s.dev.Serial}
	if total == 0 {
		// "Recorded" is the word that matters: Android rate-limits crash
		// records per app, so an app that has crashed often in the last few
		// minutes can crash again and leave nothing here.
		if app != "" {
			return Result(fmt.Sprintf("no crashes recorded for %s", app), view), nil
		}
		return Result("no crashes recorded", view), nil
	}
	var b strings.Builder
	if total > len(list) {
		fmt.Fprintf(&b, "the newest %d of %d\n", len(list), total)
	}
	for i, r := range list {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s  %s  %s  %s — %s", r.ID, r.Time.Format(time.RFC3339), r.Kind, r.App, r.Summary)
		if r.Dropped > 0 {
			noun := "crashes"
			if r.Dropped == 1 {
				noun = "crash"
			}
			fmt.Fprintf(&b, " (and %d %s before it Android did not record)", r.Dropped, noun)
		}
	}
	return Result(b.String(), view), nil
}

// CrashesView is the result of app_crashes.
type CrashesView struct {
	Crashes []device.CrashReport `json:"crashes"`
	App     string               `json:"app,omitempty"`
	// Total is how many matched before the limit; zero on a single read.
	Total  int    `json:"total,omitempty"`
	Device string `json:"device"`
}

func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
