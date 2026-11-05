package agent

import (
	"os"
	"testing"
)

// TestMain keeps what the tools write beside the daemon's socket, such as a
// lost capture's note, out of the home of whoever runs the tests.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "mobium-agent-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("MOBIUM_HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
