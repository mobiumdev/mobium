//go:build !windows

package daemon

import (
	"fmt"
	"net"
	"os"
)

// listen binds the Unix socket and makes it owner-only. The socket accepts
// commands that drive a device; nobody else on the host should be able to
// send them.
func listen(socketPath string) (net.Listener, error) {
	// A socket left behind by a crashed daemon blocks the bind.
	os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("secure socket: %w", err)
	}
	return listener, nil
}

// removeSocket deletes the socket file, which outlives the listener on Unix.
func removeSocket(socketPath string) { os.Remove(socketPath) }
