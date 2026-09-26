//go:build windows

package daemon

import (
	"fmt"
	"net"

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
	return winio.ListenPipe(pipeName, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;" + user.User.Sid.String() + ")",
	})
}

// removeSocket does nothing: a named pipe is gone when its last handle closes,
// and there is no file to delete.
func removeSocket(string) {}
