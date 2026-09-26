//go:build !windows

package daemon

import (
	"net"
	"time"
)

func dial(socketPath string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", socketPath, timeout)
}
