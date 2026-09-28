//go:build windows

package main

import (
	"os"
	"os/exec"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// remoteSupported refuses: the remote transport forwards the node's daemon
// socket to a Unix socket here, and Windows' daemon speaks a named pipe that
// an SSH forward does not produce. Said, rather than attempted.
func remoteSupported() error {
	return mobiumerr.New(mobiumerr.Unsupported, "--remote needs a Unix socket on this machine, and this is Windows; "+
		"run the client on macOS or Linux, or on the node itself")
}

func remoteTempRoot() string { return os.TempDir() }

func setOwnGroup(*exec.Cmd) {}
