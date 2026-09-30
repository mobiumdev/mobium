package mobiumdriver

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Device logs and crash reports come from the device, not from the
// automation server, so both Android backends read them the same way through
// adb.

// DeviceLogs reads logcat.
func (a *Android) DeviceLogs(ctx context.Context, q device.LogQuery) (device.LogResult, error) {
	return a.adb.Logcat(ctx, q)
}

// Crashes lists the crashes dropbox recorded.
func (a *Android) Crashes(ctx context.Context, app string) ([]device.CrashReport, error) {
	return a.adb.Crashes(ctx, app)
}

// Crash reads one dropbox crash report in full.
func (a *Android) Crash(ctx context.Context, id string) (device.CrashReport, error) {
	return a.adb.Crash(ctx, id)
}

// DeviceLogs reads logcat.
func (u *UIA2) DeviceLogs(ctx context.Context, q device.LogQuery) (device.LogResult, error) {
	return u.adb.Logcat(ctx, q)
}

// Crashes lists the crashes dropbox recorded.
func (u *UIA2) Crashes(ctx context.Context, app string) ([]device.CrashReport, error) {
	return u.adb.Crashes(ctx, app)
}

// Crash reads one dropbox crash report in full.
func (u *UIA2) Crash(ctx context.Context, id string) (device.CrashReport, error) {
	return u.adb.Crash(ctx, id)
}

// DeviceLogs reads the simulator's unified log, or what the session has
// captured of a phone's.
func (w *WDA) DeviceLogs(ctx context.Context, q device.LogQuery) (device.LogResult, error) {
	if w.phone == nil {
		return w.sim.UnifiedLog(ctx, q)
	}
	w.mu.Lock()
	plog, perr := w.plog, w.plogErr
	w.mu.Unlock()
	if plog == nil {
		if perr == nil {
			perr = mobiumerr.New(mobiumerr.DeviceNotReady, "the session has not started")
		}
		return device.LogResult{}, mobiumerr.New(mobiumerr.DeviceServer, "the iPhone's log could not be captured: %v", perr)
	}
	exe := ""
	if q.App != "" {
		var err error
		if exe, err = device.PhoneExecutable(ctx, w.phone.Phone.UDID, q.App); err != nil {
			return device.LogResult{}, err
		}
	}
	res, err := plog.Read(ctx, q, exe)
	if err == nil && q.After.IsZero() {
		// Said on the first read, because a phone keeps no history: what
		// is here began when this session did.
		note := "a phone keeps no log history, so this began when the session started; lines are stamped when Mobium received them"
		if res.Note != "" {
			note = res.Note + "; " + note
		}
		res.Note = note
	}
	return res, err
}

// Crashes lists crash reports: the Mac's for a simulator, the phone's own
// for a phone.
func (w *WDA) Crashes(ctx context.Context, app string) ([]device.CrashReport, error) {
	if w.phone != nil {
		return device.PhoneCrashReports(ctx, w.phone.Phone.UDID, app)
	}
	return w.sim.CrashReports(ctx, app)
}

// Crash reads one crash report in full.
func (w *WDA) Crash(ctx context.Context, id string) (device.CrashReport, error) {
	if w.phone != nil {
		return device.PhoneCrashReport(ctx, w.phone.Phone.UDID, id)
	}
	return w.sim.CrashReport(ctx, id)
}

// StartRecording records with screenrecord on the device.
func (a *Android) StartRecording(ctx context.Context) (device.Recording, error) {
	return a.adb.StartScreenRecord(ctx)
}

// StartRecording records with screenrecord on the device.
func (u *UIA2) StartRecording(ctx context.Context) (device.Recording, error) {
	return u.adb.StartScreenRecord(ctx)
}

// StartRecording records the simulator's screen to a file on the Mac, and a
// phone's from WebDriverAgent's screen stream.
func (w *WDA) StartRecording(ctx context.Context) (device.Recording, error) {
	if w.phone != nil {
		return w.phoneScreenRecord(ctx)
	}
	return w.sim.StartScreenRecord(ctx, filepath.Join(os.TempDir(), "mobium-recordings"))
}
