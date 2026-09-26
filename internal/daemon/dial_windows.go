//go:build windows

package daemon

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// dial connects to the daemon's named pipe. go-winio reports a missing pipe as
// an *os.PathError, where a Unix dial reports a *net.OpError, and
// IsConnectionError — which decides whether to auto-start a daemon — knows
// only the second. Wrapping it keeps that decision the same on both.
func dial(pipeName string, timeout time.Duration) (net.Conn, error) {
	conn, err := winio.DialPipe(pipeName, &timeout)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: "pipe", Err: err}
	}
	return conn, nil
}
