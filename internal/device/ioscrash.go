package device

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A simulator's apps are processes on the Mac, so their crash reports are
// written where the Mac's are, as .ips files — JSON, a one-line header and
// then the report. Nothing in the header names the simulator; the report's
// coalitionName does, as com.apple.CoreSimulator.SimDevice.<UDID>, which is
// what keeps one simulator's crashes from being reported as another's.
//
// A report is not there the moment the app dies. Measured on an iPhone 17
// Pro simulator: one second after an abort inside the app, and 34 seconds
// after a SIGABRT sent from outside.

// iosCrashBugType is the .ips header's bug_type for a crash, as opposed to a
// hang, a spin or a jetsam report.
const iosCrashBugType = "309"

// watchdogCode is the termination code of an app the system killed for not
// responding — the iOS counterpart of an ANR.
const watchdogCode = 0x8badf00d

// crashReportDirs are where the Mac keeps the current user's crash reports.
// A list so a report moved elsewhere can be added without changing callers;
// the Retired subdirectory is left out, being reports macOS has already
// submitted and set aside.
//
// A variable so a test can point it at captured reports.
var crashReportDirs = func() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, "Library", "Logs", "DiagnosticReports")}
}

type ipsHeader struct {
	BugType  string `json:"bug_type"`
	BundleID string `json:"bundleID"`
	Name     string `json:"name"`
}

type ipsReport struct {
	ProcName      string `json:"procName"`
	PID           int    `json:"pid"`
	CaptureTime   string `json:"captureTime"`
	CoalitionName string `json:"coalitionName"`
	BundleInfo    struct {
		ID      string `json:"CFBundleIdentifier"`
		Short   string `json:"CFBundleShortVersionString"`
		Version string `json:"CFBundleVersion"`
	} `json:"bundleInfo"`
	Exception struct {
		Type    string `json:"type"`
		Signal  string `json:"signal"`
		Subtype string `json:"subtype"`
	} `json:"exception"`
	Termination struct {
		Namespace string `json:"namespace"`
		Code      int64  `json:"code"`
		Indicator string `json:"indicator"`
		ByProc    string `json:"byProc"`
		ByPid     int    `json:"byPid"`
	} `json:"termination"`
	ASI            map[string][]string `json:"asi"`
	FaultingThread int                 `json:"faultingThread"`
	Threads        []struct {
		Frames []struct {
			ImageIndex     int    `json:"imageIndex"`
			Symbol         string `json:"symbol"`
			SymbolLocation int    `json:"symbolLocation"`
			ImageOffset    int    `json:"imageOffset"`
		} `json:"frames"`
	} `json:"threads"`
	UsedImages []struct {
		Name string `json:"name"`
	} `json:"usedImages"`
}

// ipsCaptureLayout is captureTime's format, e.g. "2026-09-25 10:53:19.3465 -0700".
const ipsCaptureLayout = "2006-01-02 15:04:05.9999 -0700"

// CrashReports lists this simulator's crashes, newest first.
func (s *Simctl) CrashReports(ctx context.Context, app string) ([]CrashReport, error) {
	var list []CrashReport
	for _, dir := range crashReportDirs() {
		files, err := filepath.Glob(filepath.Join(dir, "*.ips"))
		if err != nil {
			continue
		}
		for _, f := range files {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			r, ok := s.readIPS(f, false)
			if !ok || (app != "" && r.App != app) {
				continue
			}
			list = append(list, r)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Time.After(list[j].Time) })
	return list, nil
}

// CrashReport reads one of this simulator's crash reports in full.
func (s *Simctl) CrashReport(ctx context.Context, id string) (CrashReport, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return CrashReport{}, mobiumerr.New(mobiumerr.InvalidArgument,
			"%q is not a crash id — use one from `mobium crashes`, which look like Preferences-2026-09-25-105320", id)
	}
	for _, dir := range crashReportDirs() {
		if r, ok := s.readIPS(filepath.Join(dir, id+".ips"), true); ok {
			return r, nil
		}
	}
	return CrashReport{}, mobiumerr.New(mobiumerr.InvalidArgument,
		"no crash %s from this simulator — `mobium crashes` lists what is there now", id)
}

// readIPS reads one .ips file, and reports false for anything that is not a
// crash on this simulator.
func (s *Simctl) readIPS(path string, full bool) (CrashReport, bool) {
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(raw, []byte(s.UDID)) {
		return CrashReport{}, false
	}
	var mtime time.Time
	if fi, err := os.Stat(path); err == nil {
		mtime = fi.ModTime()
	}
	c, r, ok := parseIPS(raw, strings.TrimSuffix(filepath.Base(path), ".ips"), path, mtime, full)
	if !ok || r.CoalitionName != "com.apple.CoreSimulator.SimDevice."+s.UDID {
		return CrashReport{}, false
	}
	return c, true
}

// parseIPS reads an .ips report wherever it came from — a simulator's, from
// the Mac's disk, or a phone's, over AFC; the format is the same — and
// reports false for anything that is not a crash. where names the file for
// the full text; fallback stands in for a capture time that will not parse.
func parseIPS(raw []byte, id, where string, fallback time.Time, full bool) (CrashReport, ipsReport, bool) {
	var r ipsReport
	head, body, ok := bytes.Cut(raw, []byte("\n"))
	if !ok {
		return CrashReport{}, r, false
	}
	var h ipsHeader
	if json.Unmarshal(head, &h) != nil || h.BugType != iosCrashBugType || json.Unmarshal(body, &r) != nil {
		return CrashReport{}, r, false
	}
	t, err := time.Parse(ipsCaptureLayout, r.CaptureTime)
	if err != nil {
		t = fallback
	}
	app := r.BundleInfo.ID
	if app == "" {
		app = h.BundleID
	}
	if app == "" {
		// A daemon has no bundle; its process name is what names it, as
		// Android's Process: line does.
		app = r.ProcName
	}
	kind := "crash"
	if r.Termination.Code == watchdogCode {
		kind = "anr"
	}
	c := CrashReport{
		ID:      id,
		Time:    t.UTC(),
		Kind:    kind,
		App:     app,
		Summary: ipsSummary(r),
	}
	if full {
		c.Text = ipsText(r, where)
	}
	return c, r, true
}

// ipsSummary is the exception and why the process ended, with the app's own
// last words when it left some — a Swift fatalError's message is in asi.
func ipsSummary(r ipsReport) string {
	parts := []string{}
	if r.Exception.Type != "" {
		e := r.Exception.Type
		if r.Exception.Signal != "" {
			e += " (" + r.Exception.Signal + ")"
		}
		parts = append(parts, e)
	}
	if r.Termination.Indicator != "" {
		parts = append(parts, r.Termination.Indicator)
	}
	for _, msgs := range r.ASI {
		if len(msgs) > 0 && strings.TrimSpace(msgs[0]) != "" {
			parts = append(parts, strings.TrimSpace(msgs[0]))
			break
		}
	}
	return strings.Join(parts, " — ")
}

// ipsText renders the parts of a report worth reading — the .ips itself is
// tens of kilobytes of JSON, most of it image lists and register state — and
// says where the whole thing is.
func ipsText(r ipsReport, path string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Process:     %s [%d]\n", r.ProcName, r.PID)
	if r.BundleInfo.ID != "" {
		fmt.Fprintf(&b, "Bundle:      %s %s (%s)\n", r.BundleInfo.ID, r.BundleInfo.Short, r.BundleInfo.Version)
	}
	fmt.Fprintf(&b, "Time:        %s\n", r.CaptureTime)
	fmt.Fprintf(&b, "Exception:   %s", r.Exception.Type)
	if r.Exception.Signal != "" {
		fmt.Fprintf(&b, " (%s)", r.Exception.Signal)
	}
	if r.Exception.Subtype != "" {
		fmt.Fprintf(&b, " %s", r.Exception.Subtype)
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "Termination: %s %#x %s", r.Termination.Namespace, r.Termination.Code, r.Termination.Indicator)
	if r.Termination.ByProc != "" {
		fmt.Fprintf(&b, ", by %s [%d]", r.Termination.ByProc, r.Termination.ByPid)
	}
	b.WriteByte('\n')
	for key, msgs := range r.ASI {
		for _, m := range msgs {
			fmt.Fprintf(&b, "%s: %s\n", key, strings.TrimSpace(m))
		}
	}
	if r.FaultingThread >= 0 && r.FaultingThread < len(r.Threads) {
		fmt.Fprintf(&b, "\nThread %d crashed:\n", r.FaultingThread)
		for i, f := range r.Threads[r.FaultingThread].Frames {
			image := "???"
			if f.ImageIndex >= 0 && f.ImageIndex < len(r.UsedImages) && r.UsedImages[f.ImageIndex].Name != "" {
				image = r.UsedImages[f.ImageIndex].Name
			}
			if f.Symbol != "" {
				fmt.Fprintf(&b, "%-3d %-28s %s + %d\n", i, image, f.Symbol, f.SymbolLocation)
			} else {
				fmt.Fprintf(&b, "%-3d %-28s %#x\n", i, image, f.ImageOffset)
			}
		}
	}
	fmt.Fprintf(&b, "\nFull report: %s", path)
	return b.String()
}
