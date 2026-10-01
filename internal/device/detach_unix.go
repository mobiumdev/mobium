//go:build !windows

package device

import (
	"os/exec"
	"syscall"
)

// Detach puts a process in its own session, so a Ctrl-C in the terminal that
// started it does not take it down with the foreground command: the daemon,
// and an emulator `mobium boot` starts.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
