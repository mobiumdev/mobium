//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func remoteSupported() error { return nil }

// remoteTempRoot is /tmp, not os.TempDir: the forwarded socket's path has to
// fit the OS's ~104 bytes, and macOS's per-user temporary directory is about
// half of that on its own.
func remoteTempRoot() string { return "/tmp" }

func setOwnGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
