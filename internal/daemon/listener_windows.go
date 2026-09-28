//go:build windows

package daemon

import (
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// listen creates the named pipe, readable and writable by the current user
// only — the 0600 the Unix socket gets, carried over.
//
// The owner is named by SID rather than by OWNER RIGHTS: an elevated process
// creates objects owned by Administrators, so an OWNER RIGHTS ACE would lock
// out the same user's unelevated CLI. go-winio creates the first instance with
// FILE_FLAG_FIRST_PIPE_INSTANCE, so a pipe that already exists — a live
// daemon, or somebody else's squatting on the name — fails the bind rather
// than being joined.
func listen(pipeName string) (net.Listener, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read current user: %w", err)
	}
	l, err := winio.ListenPipe(pipeName, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;" + user.User.Sid.String() + ")",
	})
	if err != nil {
		return nil, err
	}
	return pipeListener{l}, nil
}

// pipeListener works around go-winio v0.6.2 losing a Close. Close signals the
// listener's goroutine once; if an Accept is waiting for a client, the wait
// takes the signal instead, and when the aborted connect reports an error
// other than the one go-winio expects, the goroutine goes back to waiting for
// a signal that has already been spent. Close then blocks for good, and so
// did the daemon's shutdown: the first run on Windows hung
// TestDaemonWritesAndRemovesPIDAndSocket for ten minutes with exactly that
// stack. A second Close reaches the idle goroutine, and every Close returns
// once it has finished, so repeating it is safe. CHALLENGES 139.
type pipeListener struct{ net.Listener }

func (l pipeListener) Close() error {
	done := make(chan struct{})
	go func() {
		_ = l.Listener.Close()
		close(done)
	}()
	for {
		select {
		case <-done:
			return nil
		case <-time.After(100 * time.Millisecond):
			go func() { _ = l.Listener.Close() }()
		}
	}
}

// removeSocket does nothing: a named pipe is gone when its last handle closes,
// and there is no file to delete.
func removeSocket(string) {}
