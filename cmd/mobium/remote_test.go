package main

import (
	"os"
	"testing"

	"github.com/mobiumdev/mobium/internal/daemon"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Through a forward, a daemon that does not answer is the node's, and one must
// not be started here in its place: it would drive this machine's devices
// while the caller believed it was driving the node's.
func TestRemoteNeverStartsALocalDaemon(t *testing.T) {
	// Short, for the socket path's ~104 bytes; t.TempDir is too long on macOS.
	home, err := os.MkdirTemp(remoteTempRoot(), "mbt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("MOBIUM_HOME", home)
	t.Setenv("MOBIUM_SESSION", "")
	remoteActive = true
	defer func() { remoteActive = false }()

	_, err = daemonCall("app_current", map[string]interface{}{})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady {
		t.Fatalf("with the node unreachable the call answered %v, want device_not_ready", err)
	}
	if _, err := daemon.Status(); err == nil {
		t.Fatal("a local daemon was started in the node's place")
	}
}
