//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

// Running reports whether a process with this PID exists.
func Running(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix FindProcess always succeeds; signal 0 is the actual check.
	return proc.Signal(syscall.Signal(0)) == nil
}
