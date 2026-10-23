//go:build windows

package device

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Detach is the Windows form of Setsid. A new process group keeps a Ctrl-C in
// the terminal from reaching the process, and DETACHED_PROCESS gives it no
// console at all, so closing the terminal window does not end it either.
//
// Neither leaves a job object, and Windows' OpenSSH server runs each session
// in one that kills everything in it when the session ends: the daemon died
// with the SSH session that started it, where setsid outlives a logout. So a
// process in a job that allows breakaway is started outside it. CHALLENGES 359.
func Detach(cmd *exec.Cmd) {
	flags := uint32(windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS)
	if jobAllowsBreakaway() {
		flags |= windows.CREATE_BREAKAWAY_FROM_JOB
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
}

// jobAllowsBreakaway reports whether this process is in a job whose children
// may leave it. Asking to leave a job that does not allow it fails the start
// with access denied, so the flag is only ever set when it is allowed. A null
// handle asks about this process's own job; outside one the query fails.
func jobAllowsBreakaway() bool {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if err := windows.QueryInformationJobObject(0, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
		return false
	}
	return info.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK != 0
}
