package device

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// firstReadWindow is how far back a read with no mark looks. A simulator at
// rest logs about 1,500 lines in ten seconds, read in 0.85s; right after boot
// it logged 242,000 in one minute and took 6.7s to read, so a longer window
// costs seconds for lines the limit would throw away.
const firstReadWindow = "10s"

// unifiedLevels folds the unified log's message types into LogLevels.
// Default is os_log's notice level, between info and error, and has no slot
// of its own there; it is reported as info.
var unifiedLevels = map[string]string{
	"Debug": "debug", "Info": "info", "Default": "info", "Error": "error", "Fault": "fatal",
}

// unifiedPredicates narrows the read on the simulator, so the lines a level
// filter would drop are never transferred.
var unifiedPredicates = map[string]string{
	"debug": `messageType == debug`, "info": `(messageType == info OR messageType == default)`,
	"error": `messageType == error`, "fatal": `messageType == fault`,
}

// unifiedTimeLayout is `log show --style ndjson`'s timestamp.
const unifiedTimeLayout = "2006-01-02 15:04:05.000000-0700"

// UnifiedLog reads the simulator's unified log.
//
// An app is filtered by its executable's name, looked up from the bundle id,
// because that is what the log records — and like Android's UID it survives
// the process being restarted under a new pid.
func (s *Simctl) UnifiedLog(ctx context.Context, q LogQuery) (LogResult, error) {
	var res LogResult
	if q.Level == "verbose" || q.Level == "warn" {
		// The unified log has no such level. An empty answer would read as
		// "nothing at that level was logged", which is a different claim.
		return res, mobiumerr.New(mobiumerr.InvalidArgument, "the iOS unified log has no %q level — "+
			"use debug, info, error or fatal", q.Level)
	}
	args := []string{"spawn", s.UDID, "log", "show", "--style", "ndjson", "--info", "--debug"}
	if q.After.IsZero() {
		args = append(args, "--last", firstReadWindow)
	} else {
		// --start takes whole seconds; the strict bound is applied below.
		args = append(args, "--start", q.After.Format("2006-01-02 15:04:05-0700"))
	}
	var preds []string
	if q.App != "" {
		exe, err := s.Executable(ctx, q.App)
		if err != nil {
			return res, err
		}
		preds = append(preds, "("+appPredicate(exe, q.App)+")")
	}
	if p, ok := unifiedPredicates[q.Level]; ok {
		preds = append(preds, p)
	}
	if len(preds) > 0 {
		args = append(args, "--predicate", strings.Join(preds, " AND "))
	}
	out, err := s.Run(ctx, args...)
	if err != nil {
		return res, err
	}
	res.Entries, res.Skipped = filterLog(parseUnifiedLog(out), q)
	return res, nil
}

// lifecycleProcess is the iOS process that starts, suspends and ends apps,
// and logs each one as app<bundle-id>. It is to an iOS app what system_server
// is to an Android one: the process that says, in its own log, that the app
// died — "[app<com.apple.Preferences((null))>:80109] termination reported by
// launchd (2, 6, 6)", measured on an iPhone 17 Pro simulator the moment an
// injected abort() killed Settings, the 6 being SIGABRT.
const lifecycleProcess = "runningboardd"

// appPredicate keeps an app's own lines and the lifecycle manager's lines
// about it. Filtered by the app's process alone, the log never held the line
// saying it had died; a dozen other system processes mention the app too —
// SpringBoard's audio checks, badges, location polls, 400 lines in eight
// seconds — and those are left out, as Android keeps only system_server's.
func appPredicate(exe, bundle string) string {
	return "process == " + strconv.Quote(exe) + " OR (process == " + strconv.Quote(lifecycleProcess) +
		" AND eventMessage CONTAINS " + strconv.Quote("app<"+bundle) + ")"
}

// parseUnifiedLog reads NDJSON from `log show`. Only log events are lines of
// the log; activity, state and timesync events are its bookkeeping and made
// up an eighth of a measured minute.
func parseUnifiedLog(out []byte) []LogEntry {
	var entries []LogEntry
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var e struct {
			EventType   string `json:"eventType"`
			MessageType string `json:"messageType"`
			Timestamp   string `json:"timestamp"`
			ProcessID   int    `json:"processID"`
			Subsystem   string `json:"subsystem"`
			Category    string `json:"category"`
			Process     string `json:"processImagePath"`
			Message     string `json:"eventMessage"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.EventType != "logEvent" {
			continue
		}
		t, err := time.Parse(unifiedTimeLayout, e.Timestamp)
		if err != nil {
			continue
		}
		tag := e.Subsystem
		if e.Category != "" {
			tag += ":" + e.Category
		}
		if tag == "" {
			tag = lastPathElement(e.Process)
		}
		entries = append(entries, LogEntry{
			Time:    t.UTC(),
			Level:   unifiedLevels[e.MessageType],
			Tag:     tag,
			PID:     e.ProcessID,
			Message: e.Message,
		})
	}
	return entries
}

func lastPathElement(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Executable is the name an installed app's process runs under.
func (s *Simctl) Executable(ctx context.Context, bundleID string) (string, error) {
	raw, err := s.Run(ctx, "appinfo", s.UDID, bundleID)
	if err != nil {
		return "", err
	}
	data, err := plistToJSON(ctx, raw)
	if err != nil {
		return "", err
	}
	var info struct {
		Executable string `json:"CFBundleExecutable"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return "", fmt.Errorf("could not read %s's app info: %w", bundleID, err)
	}
	if info.Executable == "" {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "%s is not installed on this simulator — `mobium apps` lists what is", bundleID)
	}
	return info.Executable, nil
}
