//go:build windows

package daemon

import "golang.org/x/sys/windows"

// stillActive is STILL_ACTIVE, the exit code Windows reports for a process
// that has not exited.
const stillActive = 259

// Running reports whether a process with this PID exists and has not exited.
//
// The Unix check, signal 0, is unsupported on Windows and always errors, so
// every daemon would be judged dead and CleanStale would delete a live one's
// PID file. Nor is a successful OpenProcess enough — the handle-based check
// WINDOWS.md first proposed: a process that has exited stays openable for as
// long as anything holds a handle to it. So the exit code is read too.
func Running(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Including access denied: a PID that now belongs to another user's
		// process was reused, so our daemon is gone. Unix answers the same,
		// since signal 0 fails with EPERM there.
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
