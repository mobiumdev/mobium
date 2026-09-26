//go:build windows

package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/paths"
	"golang.org/x/sys/windows"
)

// socketPresent reports whether the named pipe accepts a connection. A pipe
// is not a file, so this is the only way to ask whether it exists.
func socketPresent(t *testing.T, path string) bool {
	t.Helper()
	conn, err := dial(path, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// checkOwnerOnly fails unless the pipe's DACL is protected and grants access
// to the current user and nobody else — the Windows reading of 0600.
func checkOwnerOnly(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read pipe security: %v", err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sddl := sd.String()
	if !strings.HasPrefix(sddl, "D:P") {
		t.Errorf("pipe DACL %q is not protected: it inherits entries", sddl)
	}
	if strings.Count(sddl, "(") != 1 || !strings.Contains(sddl, ";;;"+user.User.Sid.String()+")") {
		t.Errorf("pipe DACL %q should hold one entry, for %s", sddl, user.User.Sid)
	}
}

// A second daemon on the same pipe must fail to bind, not join the first as
// another instance and take half its connections.
func TestSecondListenerOnOnePipeIsRefused(t *testing.T) {
	defer startDaemon(t)()

	socket, err := paths.SocketPath()
	if err != nil {
		t.Fatal(err)
	}
	l, err := listen(socket)
	if err == nil {
		l.Close()
		t.Fatal("a second listener bound the live daemon's pipe")
	}
}
