package device

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A console that answered nothing is a failure, and with the token file
// present but empty the refusal names it and how to recover.
func TestConsoleSilentNamesAnEmptyToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir's source on Windows
	err := consoleSilent([]string{"sensor", "get", "acceleration"})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || strings.Contains(err.Error(), "auth_token") {
		t.Errorf("no token file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".emulator_console_auth_token"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err = consoleSilent([]string{"sensor", "get", "acceleration"})
	if !strings.Contains(err.Error(), ".emulator_console_auth_token is empty") {
		t.Errorf("empty token file: %v", err)
	}
}
