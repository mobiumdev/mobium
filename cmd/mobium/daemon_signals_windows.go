package main

import "github.com/mobiumdev/mobium/internal/daemon"

// dropLogStreamsOnSignal does nothing on Windows, which has no SIGUSR1 —
// and no real iPhone to drive.
func dropLogStreamsOnSignal(*daemon.Daemon) {}
