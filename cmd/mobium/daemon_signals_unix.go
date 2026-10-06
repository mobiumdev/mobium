//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/mobiumdev/mobium/internal/daemon"
)

// dropLogStreamsOnSignal makes SIGUSR1 drop every phone session's log
// stream, the way an unplugged cable or a restarted relay would, and leave
// the sessions as they were. A real iPhone's log is a connection inside
// this process, with nothing outside to stop, so this is how
// docs/checks/graybox-edges.sh drops it mid-work and holds the gray box to
// what a real drop does.
func dropLogStreamsOnSignal(d *daemon.Daemon) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGUSR1)
	go func() {
		for range c {
			logf("SIGUSR1: dropped %d phone log stream(s); each reconnects at its session's next action",
				d.DropLogStreams())
		}
	}()
}
