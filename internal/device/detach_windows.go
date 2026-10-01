//go:build windows

package device

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Detach is the Windows form of Setsid. A new process group keeps a Ctrl-C in
// the terminal from reaching the process, and DETACHED_PROCESS gives it no
// console at all, so closing the terminal window does not end it either.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
}
