//go:build !windows

package daemon

import (
	"os"
	"testing"
)

// socketPresent reports whether the socket file exists.
func socketPresent(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// checkOwnerOnly fails unless the socket is mode 0600.
func checkOwnerOnly(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("socket mode = %o, want 600", fi.Mode().Perm())
	}
}
