//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setDetached puts the daemon in its own process group so a Ctrl-C in the
// terminal that started it does not take it down with the foreground command.
func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
