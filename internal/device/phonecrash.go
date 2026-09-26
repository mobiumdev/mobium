package device

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A real iPhone's crash reports are files, served by lockdown's
// com.apple.crashreportcopymobile over AFC. They are the same .ips format a
// simulator's are on the Mac, so parseIPS reads both.
//
// Measured on an iPhone 15 Plus on iOS 26.6.2: 112 files at the top level, of
// which 35 were crashes — the rest resource reports, jetsam events, spindumps
// and diagnostics — and reading every header took 0.24s.
//
// com.apple.crashreportmover files new reports into that directory first; it
// answers "ping" when it has. Without it a crash from the last few moments
// may not be listed yet.

// phoneCrashLimit refuses a report too large to be a crash worth reading.
const phoneCrashLimit = 8 << 20

// ipsHeaderPrefix is enough of a file to read its one-line header.
const ipsHeaderPrefix = 4096

func phoneCrashes(ctx context.Context, udid string) (*afcClient, func(), error) {
	if mv, err := LockdownService(ctx, udid, "com.apple.crashreportmover"); err == nil {
		_ = mv.SetReadDeadline(time.Now().Add(20 * time.Second))
		ping := make([]byte, 8)
		_, _ = mv.Read(ping)
		mv.Close()
	}
	conn, err := LockdownService(ctx, udid, "com.apple.crashreportcopymobile")
	if err != nil {
		return nil, nil, err
	}
	return &afcClient{conn: conn}, func() { conn.Close() }, nil
}

// PhoneCrashReports lists a phone's crashes, newest first.
func PhoneCrashReports(ctx context.Context, udid, app string) ([]CrashReport, error) {
	afc, done, err := phoneCrashes(ctx, udid)
	if err != nil {
		return nil, err
	}
	defer done()
	names, err := afc.ReadDir("/")
	if err != nil {
		return nil, err
	}
	var list []CrashReport
	for _, name := range names {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !strings.HasSuffix(name, ".ips") {
			continue
		}
		path := joinAFC("/", name)
		// The header says whether this is a crash at all; most are not, and
		// a jetsam report is a quarter of a megabyte.
		head, err := afc.ReadPrefix(path, ipsHeaderPrefix)
		if err != nil || !strings.Contains(string(head), `"bug_type":"`+iosCrashBugType+`"`) {
			continue
		}
		raw, err := afc.ReadFile(path, phoneCrashLimit)
		if err != nil {
			continue
		}
		c, _, ok := parseIPS(raw, strings.TrimSuffix(name, ".ips"), path, time.Time{}, false)
		if !ok || (app != "" && c.App != app) {
			continue
		}
		list = append(list, c)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Time.After(list[j].Time) })
	return list, nil
}

// PhoneCrashReport reads one of a phone's crash reports in full.
func PhoneCrashReport(ctx context.Context, udid, id string) (CrashReport, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return CrashReport{}, mobiumerr.New(mobiumerr.InvalidArgument,
			"%q is not a crash id — use one from `mobium crashes`, which look like Wikipedia-2026-09-25-111500", id)
	}
	afc, done, err := phoneCrashes(ctx, udid)
	if err != nil {
		return CrashReport{}, err
	}
	defer done()
	path := joinAFC("/", id+".ips")
	raw, err := afc.ReadFile(path, phoneCrashLimit)
	if err == nil {
		if c, _, ok := parseIPS(raw, id, "the iPhone's "+path, time.Time{}, true); ok {
			return c, nil
		}
	}
	return CrashReport{}, mobiumerr.New(mobiumerr.InvalidArgument,
		"no crash %s on this iPhone — `mobium crashes` lists what is there now", id)
}
