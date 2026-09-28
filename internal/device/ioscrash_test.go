package device

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fixture is a real report, trimmed: Settings on an iPhone 17 Pro
// simulator, aborted by a library injected at launch so the crash happened
// inside the process. Other-… is the same report stamped with another
// simulator's UDID.
const fixtureUDID = "457C7DC2-C706-45D9-8D68-1D26953E28B1"

func withCrashDir(t *testing.T, dir string) {
	t.Helper()
	old := crashReportDirs
	crashReportDirs = func() []string { return []string{dir} }
	t.Cleanup(func() { crashReportDirs = old })
}

func TestSimulatorCrashesAreThisSimulatorsOnly(t *testing.T) {
	withCrashDir(t, "testdata")
	s := &Simctl{UDID: fixtureUDID}
	list, err := s.CrashReports(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	// Nothing in the header names a simulator; coalitionName does, and a
	// report from another one must not be reported as this one's.
	if len(list) != 1 {
		t.Fatalf("got %d reports, want 1: %+v", len(list), list)
	}
	r := list[0]
	if r.ID != "Preferences-2026-09-25-105730" || r.App != "com.apple.Preferences" || r.Kind != "crash" {
		t.Errorf("report = %+v", r)
	}
	if r.Summary != "EXC_CRASH (SIGABRT) — Abort trap: 6" {
		t.Errorf("summary = %q", r.Summary)
	}
	if want := time.Date(2026, 9, 25, 17, 57, 28, 914100000, time.UTC); !r.Time.Equal(want) {
		t.Errorf("time = %v, want %v", r.Time, want)
	}
	if r.Text != "" {
		t.Error("a listing carried the full text")
	}

	other := &Simctl{UDID: "00000000-0000-0000-0000-000000000000"}
	if list, _ := other.CrashReports(context.Background(), ""); len(list) != 1 || list[0].ID != "Other-2026-09-25-105731" {
		t.Errorf("the other simulator saw %+v", list)
	}
	if list, _ := s.CrashReports(context.Background(), "com.example.none"); len(list) != 0 {
		t.Errorf("an app filter kept %+v", list)
	}
}

func TestSimulatorCrashReadsInFull(t *testing.T) {
	withCrashDir(t, "testdata")
	s := &Simctl{UDID: fixtureUDID}
	r, err := s.CrashReport(context.Background(), "Preferences-2026-09-25-105730")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Process:     Preferences [36981]", "Thread 0 crashed:",
		"abort + 116", "crash.dylib", "Full report: " + filepath.Join("testdata", "Preferences-2026-09-25-105730.ips")} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, r.Text)
		}
	}
	// Another simulator's report is not this one's, even by id.
	if _, err := s.CrashReport(context.Background(), "Other-2026-09-25-105731"); err == nil {
		t.Error("read another simulator's report by id")
	}
	for _, bad := range []string{"../etc/passwd", "a/b", ""} {
		if _, err := s.CrashReport(context.Background(), bad); err == nil {
			t.Errorf("accepted id %q", bad)
		}
	}
}

// Captured from the same simulator with `log show --style ndjson --info
// --debug`, plus one activity event, which is the log's bookkeeping rather
// than a line of it.
func TestParseUnifiedLogReadsARealCapture(t *testing.T) {
	raw, err := os.ReadFile("testdata/unifiedlog-ios26.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(string(raw), "\n")
	entries := parseUnifiedLog(raw)
	if len(entries) != lines-1 {
		t.Errorf("parsed %d of %d lines; only the activity event should be dropped", len(entries), lines)
	}
	first := entries[0]
	if first.Tag != "com.apple.locationd.Utility:QA" || first.PID != 35769 || first.Level != "info" {
		t.Errorf("first = %+v", first)
	}
	if want := time.Date(2026, 9, 25, 17, 57, 45, 858219000, time.UTC); !first.Time.Equal(want) {
		t.Errorf("time = %v, want %v", first.Time, want)
	}
	levels := map[string]bool{}
	for _, e := range entries {
		levels[e.Level] = true
	}
	// Default folds into info; Error stays error.
	if !levels["error"] || !levels["info"] || levels[""] {
		t.Errorf("levels = %v", levels)
	}
}
