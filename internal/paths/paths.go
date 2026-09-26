// Package paths resolves where mobium keeps its daemon socket, PID file and
// per-session state.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Root is mobium's state directory, overridable with MOBIUM_HOME so tests and
// parallel runs do not share a socket.
func Root() string {
	if p := os.Getenv("MOBIUM_HOME"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "mobium")
	}
	return filepath.Join(home, ".mobium")
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// SessionDir is the state directory for one device serial. Serials contain
// colons for network devices ("192.168.1.5:5555"), so they are sanitized
// before use as a path segment.
func SessionDir(serial string) string {
	return filepath.Join(Root(), "sessions", unsafeChars.ReplaceAllString(serial, "_"))
}

// DaemonDir holds the socket and PID file.
func DaemonDir() string {
	return filepath.Join(Root(), "daemon")
}

// SessionName returns the daemon session name from MOBIUM_SESSION. Empty means
// the default, shared session. A named session gets its own socket, PID file
// and daemon, so two people (or two test runs) on one host stay isolated.
func SessionName() string {
	return os.Getenv("MOBIUM_SESSION")
}

var sessionNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidateSessionName checks that a session name is safe to embed in a socket
// filename.
func ValidateSessionName(name string) error {
	if name == "" {
		return nil
	}
	if !sessionNameRe.MatchString(name) {
		return mobiumerr.New(mobiumerr.InvalidArgument, "invalid session name %q: use only letters, digits, '-' and '_' (max 64 chars)", name)
	}
	return nil
}

func sessionSuffix() (string, error) {
	name := SessionName()
	if err := ValidateSessionName(name); err != nil {
		return "", err
	}
	if name == "" {
		return "", nil
	}
	return "-" + name, nil
}

// maxSocketPathLen is the capacity of sockaddr_un.sun_path, minus room for the
// NUL terminator. 104 on the BSDs and macOS, 108 on Linux.
func maxSocketPathLen() int {
	if runtime.GOOS == "linux" {
		return 107
	}
	return 103
}

// SocketPath is where the daemon listens: a Unix socket, or on Windows a named
// pipe.
func SocketPath() (string, error) {
	suffix, err := sessionSuffix()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return pipeName(Root(), suffix), nil
	}
	path := filepath.Join(DaemonDir(), "mobium"+suffix+".sock")
	// Binding a socket longer than sun_path fails deep inside daemon startup
	// with an opaque error; reject it here with something actionable.
	if len(path) > maxSocketPathLen() {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "socket path %q is %d bytes, over the %d-byte OS limit: "+
			"use a shorter session name or point MOBIUM_HOME at a shorter path",
			path, len(path), maxSocketPathLen())
	}
	return path, nil
}

// pipeName is the named pipe for one state directory and session.
//
// A named pipe lives in one namespace for the whole machine, where a Unix
// socket lives inside MOBIUM_HOME. Naming it only `\\.\pipe\mobium` — Vibium's
// choice — would put every MOBIUM_HOME on one pipe: two test runs, or two
// users on a shared machine, would reach each other's daemon. So the name
// carries a hash of the state directory, and the isolation MOBIUM_HOME gives
// on Unix carries over unchanged. A pipe name's limit is 256 characters and
// this is under 40, so there is no length check to make.
func pipeName(root, suffix string) string {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	// Windows paths are case-insensitive, so C:\Users\A and c:\users\a must
	// reach the same daemon.
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(root))))
	return `\\.\pipe\mobium-` + hex.EncodeToString(sum[:6]) + suffix
}

// PIDPath is where the running daemon records its PID.
func PIDPath() (string, error) {
	suffix, err := sessionSuffix()
	if err != nil {
		return "", err
	}
	return filepath.Join(DaemonDir(), "mobium"+suffix+".pid"), nil
}

// EnsureDaemonDir creates the daemon directory with owner-only permissions.
// The socket lives there, and it accepts commands that drive a device.
func EnsureDaemonDir() error {
	dir := DaemonDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create daemon dir: %w", err)
	}
	return os.Chmod(dir, 0o700)
}
