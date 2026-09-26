package paths

import (
	"runtime"
	"strings"
	"testing"
)

func TestSessionNameValidation(t *testing.T) {
	for _, ok := range []string{"", "ci", "run-1", "a_b-2", strings.Repeat("a", 64)} {
		if err := ValidateSessionName(ok); err != nil {
			t.Errorf("ValidateSessionName(%q) = %v, want nil", ok, err)
		}
	}
	// A session name lands in a socket filename, so path separators and
	// traversal must never survive it.
	for _, bad := range []string{"a/b", "../x", "a b", "a.b", strings.Repeat("a", 65)} {
		if err := ValidateSessionName(bad); err == nil {
			t.Errorf("ValidateSessionName(%q) accepted an unsafe name", bad)
		}
	}
}

func TestNamedSessionsGetSeparateSockets(t *testing.T) {
	t.Setenv("MOBIUM_HOME", "/tmp/mobtest")

	t.Setenv("MOBIUM_SESSION", "")
	shared, err := SocketPath()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MOBIUM_SESSION", "ci")
	named, err := SocketPath()
	if err != nil {
		t.Fatal(err)
	}
	if shared == named {
		t.Error("a named session shares the default session's socket")
	}
	if !strings.Contains(named, "-ci") {
		t.Errorf("named socket = %q", named)
	}
}

func TestSocketPathRejectsOverlongPaths(t *testing.T) {
	// Binding past sun_path fails deep inside daemon startup with an opaque
	// error; this must be caught here with something the user can act on.
	if runtime.GOOS == "windows" {
		t.Skip("the daemon listens on a named pipe, which has no sun_path")
	}
	t.Setenv("MOBIUM_HOME", "/tmp/"+strings.Repeat("d", 200))
	t.Setenv("MOBIUM_SESSION", "")

	_, err := SocketPath()
	if err == nil {
		t.Fatal("expected an error for an overlong socket path")
	}
	for _, want := range []string{"MOBIUM_HOME", "limit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestInvalidSessionNameFailsSocketAndPID(t *testing.T) {
	t.Setenv("MOBIUM_HOME", "/tmp/mobtest")
	t.Setenv("MOBIUM_SESSION", "../escape")

	if _, err := SocketPath(); err == nil {
		t.Error("SocketPath accepted an unsafe session name")
	}
	if _, err := PIDPath(); err == nil {
		t.Error("PIDPath accepted an unsafe session name")
	}
}

func TestSessionDirSanitisesSerials(t *testing.T) {
	t.Setenv("MOBIUM_HOME", "/tmp/mobtest")
	dir := SessionDir("192.168.1.5:5555")
	if strings.ContainsAny(strings.TrimPrefix(dir, "/tmp/mobtest/sessions/"), ":/") {
		t.Errorf("session dir %q kept an unsafe character", dir)
	}
}

// A named pipe is machine-wide, so the state directory has to be in its name
// or two MOBIUM_HOMEs share a daemon. Runs everywhere, because pipeName is
// plain string work.
func TestPipeNameIsPerHomeAndSession(t *testing.T) {
	a := pipeName(`C:\Users\a\.mobium`, "")
	if !strings.HasPrefix(a, `\\.\pipe\mobium-`) {
		t.Errorf("pipe name %q is not in the pipe namespace", a)
	}
	if b := pipeName(`C:\Users\b\.mobium`, ""); a == b {
		t.Error("two state directories got one pipe")
	}
	// Windows paths are case-insensitive: one directory, one daemon.
	if b := pipeName(`c:\users\A\.MOBIUM`, ""); a != b {
		t.Errorf("one directory spelled two ways got two pipes: %q, %q", a, b)
	}
	if named := pipeName(`C:\Users\a\.mobium`, "-ci"); named != a+"-ci" {
		t.Errorf("named session pipe = %q, want %q", named, a+"-ci")
	}
}
