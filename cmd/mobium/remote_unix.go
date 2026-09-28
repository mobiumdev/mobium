//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func remoteSupported() error { return nil }

// remoteTempRoot is /tmp, not os.TempDir: the forwarded socket's path has to
// fit the OS's ~104 bytes, and macOS's per-user temporary directory is about
// half of that on its own.
func remoteTempRoot() string { return "/tmp" }

func setOwnGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// ownedByMe says a leftover directory is this user's to remove.
func ownedByMe(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
