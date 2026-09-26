package device

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Captured from the Pixel 7 AVD on API 35 with `logcat -d -v epoch -b
// main,system,crash`, including a line whose message has colons of its own.
func TestParseLogcatReadsARealCapture(t *testing.T) {
	raw, err := os.ReadFile("testdata/logcat-epoch-api35.txt")
	if err != nil {
		t.Fatal(err)
	}
	entries := parseLogcat(raw)
	if len(entries) < 20 {
		t.Fatalf("parsed %d entries from a 30-line capture", len(entries))
	}
	var probe *LogEntry
	for i, e := range entries {
		if e.Tag == "MobiumProbe" {
			probe = &entries[i]
		}
		if e.Level == "" {
			t.Errorf("entry with no level: %+v", e)
		}
	}
	if probe == nil {
		t.Fatal("the probe line was not parsed")
	}
	// The tag ends at the first ": ", not the last.
	if probe.Message != "fixture line: with colon: inside" {
		t.Errorf("message = %q", probe.Message)
	}
	if probe.Level != "info" || probe.PID != 4577 {
		t.Errorf("probe = %+v", probe)
	}
	if want := time.UnixMilli(1790358000876).UTC(); !probe.Time.Equal(want) {
		t.Errorf("time = %v, want %v", probe.Time, want)
	}
}

// logcat's -T cannot be trusted to exclude anything — with a time later than
// every entry it still printed the newest line, measured on API 35 — so the
// bound is applied here, strictly.
func TestFilterLogIsStrictlyAfter(t *testing.T) {
	base := time.UnixMilli(1790358000000).UTC()
	entries := []LogEntry{
		{Time: base, Level: "info", Message: "at the boundary"},
		{Time: base.Add(time.Millisecond), Level: "error", Message: "after"},
		{Time: base.Add(2 * time.Millisecond), Level: "info", Message: "later"},
	}
	got, _ := filterLog(append([]LogEntry(nil), entries...), LogQuery{After: base})
	if len(got) != 2 || got[0].Message != "after" {
		t.Errorf("after the boundary: %+v", got)
	}
	got, _ = filterLog(append([]LogEntry(nil), entries...), LogQuery{After: base.Add(time.Hour)})
	if len(got) != 0 {
		t.Errorf("a bound past every entry kept %+v", got)
	}
	got, _ = filterLog(append([]LogEntry(nil), entries...), LogQuery{Level: "error"})
	if len(got) != 1 || got[0].Message != "after" {
		t.Errorf("level: %+v", got)
	}
	// The limit keeps the newest and says how many it dropped: measured on
	// the AVD, UiAutomator2's startup logged over a hundred lines between two
	// reads, and a line logged among them vanished without a word.
	got, skipped := filterLog(append([]LogEntry(nil), entries...), LogQuery{Limit: 1})
	if len(got) != 1 || got[0].Message != "later" || skipped != 2 {
		t.Errorf("the limit must keep the newest and count the rest: %+v, skipped %d", got, skipped)
	}
}

func TestLogcatTimeKeepsMillis(t *testing.T) {
	for ms, want := range map[int64]string{
		1790358000876: "1790358000.876",
		1790358000005: "1790358000.005",
		1790358000000: "1790358000.000",
	} {
		if got := logcatTime(time.UnixMilli(ms)); got != want {
			t.Errorf("logcatTime(%d) = %s, want %s", ms, got, want)
		}
	}
}

// Captured from the same AVD: a Java crash in a data app, the shell-induced
// crash of Settings (a system app), a tag with no entries, and a native
// crash with its tombstone cut short.
func TestParseDropboxReadsARealCapture(t *testing.T) {
	raw, err := os.ReadFile("testdata/dropbox-crashes-api35.txt")
	if err != nil {
		t.Fatal(err)
	}
	list := parseDropbox(string(raw))
	if len(list) != 3 {
		t.Fatalf("parsed %d reports, want 3: %+v", len(list), list)
	}
	byID := map[string]CrashReport{}
	for _, r := range list {
		byID[r.ID] = r
	}

	settings, ok := byID["system_app_crash@1790357773461"]
	if !ok {
		t.Fatalf("no Settings crash among %v", list)
	}
	if settings.App != "com.android.settings" || settings.Kind != "crash" {
		t.Errorf("settings = %+v", settings)
	}
	if !strings.Contains(settings.Summary, "shell-induced crash") {
		t.Errorf("summary = %q, want the exception line", settings.Summary)
	}
	if !settings.Time.Equal(time.UnixMilli(1790357773461).UTC()) {
		t.Errorf("time = %v", settings.Time)
	}

	native := byID["system_app_native_crash@1790101942587"]
	if native.Kind != "native_crash" || native.App != "com.google.android.bluetooth" {
		t.Errorf("native = %+v", native)
	}
	// The abort message says why; the signal only says how.
	if !strings.HasPrefix(native.Summary, "Abort message: ") {
		t.Errorf("native summary = %q", native.Summary)
	}

	uia2 := byID["data_app_crash@1790101412202"]
	if uia2.App != "io.appium.uiautomator2.server" || !strings.Contains(uia2.Summary, "RuntimeException") {
		t.Errorf("data app crash = %+v", uia2)
	}
	if !strings.Contains(uia2.Text, "DeadObjectException") {
		t.Error("the full text lost the cause")
	}
}

func TestCrashHeadlineFallsBackToTheSignal(t *testing.T) {
	body := "Process: com.example\nPID: 1\n\n*** ***\nsignal 11 (SIGSEGV), code 1\n"
	app, summary := crashHeadline("native_crash", body)
	if app != "com.example" || summary != "signal 11 (SIGSEGV), code 1" {
		t.Errorf("got %q, %q", app, summary)
	}
	body = "Process: com.example\nSubject: Input dispatching timed out\n\nCPU usage\n"
	if _, summary := crashHeadline("anr", body); summary != "Input dispatching timed out" {
		t.Errorf("anr summary = %q", summary)
	}
}

// A real ANR from a Pixel 8 Pro on Android 17, trimmed — the CPU-usage list
// names the owner's apps and is cut to its heading. Its Subject: line is far
// below the header, after the pressure dumps; the summary must find it
// there, and the compressed entry's .txt.gz path must still give its id.
func TestParseDropboxFindsAnANRsReasonBelowTheHeader(t *testing.T) {
	raw, err := os.ReadFile("testdata/dropbox-anr-api37.txt")
	if err != nil {
		t.Fatal(err)
	}
	list := parseDropbox(string(raw))
	if len(list) != 1 {
		t.Fatalf("parsed %d entries", len(list))
	}
	r := list[0]
	if r.ID != "system_app_anr@1790219628243" || r.Kind != "anr" || r.App != "com.google.android.odad" {
		t.Errorf("report = %+v", r)
	}
	if !strings.HasPrefix(r.Summary, "Broadcast of Intent { act=android.safetycenter.action.REFRESH_SAFETY_SOURCES") {
		t.Errorf("summary = %q, want the Subject line", r.Summary)
	}
}

// An app's ANR is logged by system_server, under UID 1000, not by the app:
// a read narrowed to the app's UID returned none of the lines saying it hung
// (measured on the emulator, with MobiumApp frozen). keepForApp keeps the
// app's own lines and system_server's lines about it, and nothing else that
// shares UID 1000. Captured with `logcat -v epoch,uid` on API 35.
func TestAnAppsLogKeepsTheSystemsLinesAboutIt(t *testing.T) {
	raw, err := os.ReadFile("testdata/logcat-epoch-uid-api35.txt")
	if err != nil {
		t.Fatal(err)
	}
	all := parseLogcat(raw)
	if len(all) != 10 {
		t.Fatalf("parsed %d of 10 lines", len(all))
	}
	if all[2].uid != 0 || all[0].uid != systemUID || all[5].uid != 10214 {
		t.Errorf("uids = %d, %d, %d", all[2].uid, all[0].uid, all[5].uid)
	}
	kept := keepForApp(append([]LogEntry(nil), all...), "dev.mobium.mobiumapp", 10214, 567)
	var anr, own int
	for _, e := range kept {
		switch {
		case e.uid == systemUID && strings.Contains(e.Message, "ANR in"):
			anr++
		case e.uid == 10214:
			own++
		default:
			t.Errorf("kept a line that is neither the app's nor about it: %+v", e)
		}
	}
	if anr != 2 || own != 3 {
		t.Errorf("kept %d ANR lines and %d of the app's own, want 2 and 3", anr, own)
	}
	// Without system_server's pid, only the app's own lines: including too
	// much would be the quieter failure.
	if kept := keepForApp(append([]LogEntry(nil), all...), "dev.mobium.mobiumapp", 10214, 0); len(kept) != 3 {
		t.Errorf("with no server pid, kept %d, want the app's 3", len(kept))
	}
	// The old format, with no UID column, still parses — the uid is unknown.
	old := parseLogcat([]byte("         1790358000.876  4577  4577 I MobiumProbe: x\n"))
	if len(old) != 1 || old[0].uid != -1 || old[0].PID != 4577 {
		t.Errorf("the format without a uid parsed as %+v", old)
	}
}

// An ANR read in full leads with its reason and where the main thread was:
// the phone's real ANR carries the thread dump, and the emulator's frozen
// one — stopped with SIGSTOP, so it could not be asked — carries none, which
// is said rather than left to be noticed.
func TestAnANRLeadsWithItsMainThread(t *testing.T) {
	for _, c := range []struct {
		file, want string
	}{
		{"testdata/dropbox-anr-api37.txt", "Main thread:\n\"main\" prio=5 tid=1 Native"},
		{"testdata/dropbox-anr-frozen-api35.txt", "No thread stacks"},
	} {
		raw, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatal(err)
		}
		r := parseDropbox(string(raw))[0]
		lead := anrLead(r.Summary, r.Text)
		if !strings.HasPrefix(lead, "ANR: ") || !strings.Contains(lead, c.want) {
			t.Errorf("%s: lead = %q", c.file, lead)
		}
		if strings.Contains(c.want, "main") && !strings.Contains(lead, "at android.os.Looper.loop") {
			t.Errorf("%s: the main thread's stack was cut: %q", c.file, lead)
		}
	}
}

// Android rate-limits crash records per app. On the emulator, after ten
// records for MobiumApp in nine minutes, three crashes produced no record;
// the next one Android wrote, at 16:24:20, said Dropped-Count: 3 — exactly
// those three. That header is the only trace of them, so it is read.
func TestACrashRecordSaysHowManyWereDropped(t *testing.T) {
	raw, err := os.ReadFile("testdata/dropbox-dropped-api35.txt")
	if err != nil {
		t.Fatal(err)
	}
	list := parseDropbox(string(raw))
	if len(list) != 1 || list[0].Dropped != 3 {
		t.Fatalf("parsed %+v, want one record with Dropped 3", list)
	}
	other, _ := os.ReadFile("testdata/dropbox-crashes-api35.txt")
	for _, r := range parseDropbox(string(other)) {
		if r.Dropped != 0 {
			t.Errorf("%s: Dropped %d from a record that said 0", r.ID, r.Dropped)
		}
	}
}
