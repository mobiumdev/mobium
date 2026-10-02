package device

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/fakecmd"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// launchADB is an adb that resolves a phone app by its bare package and a TV
// app only by LEANBACK_LAUNCHER, as a Fire TV did for Prime Video, and
// records what am start was asked to start.
func launchADB(t *testing.T) (*ADB, string) {
	t.Helper()
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	bin := fakecmd.Script(t, dir, "adb", `case "$*" in
  *resolve-activity*LEANBACK_LAUNCHER*tv.app) echo "priority=0 preferredOrder=0 match=0x108000"; echo tv.app/.Ignition ;;
  *resolve-activity*phone.app) echo phone.app/.Main ;;
  *resolve-activity*) echo "No activity found" ;;
  *"am start"*) echo "$*" > `+fakecmd.Path(started)+`; echo "Starting: Intent" ;;
esac`)
	return &ADB{Path: bin, Serial: "fake"}, started
}

func TestLaunchFallsBackToLeanback(t *testing.T) {
	cases := []struct{ pkg, component string }{
		{"phone.app", "phone.app/.Main"},
		{"tv.app", "tv.app/.Ignition"},
	}
	for _, c := range cases {
		a, started := launchADB(t)
		if err := a.LaunchApp(context.Background(), c.pkg); err != nil {
			t.Fatalf("%s: %v", c.pkg, err)
		}
		got, err := os.ReadFile(started)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "-n "+c.component) {
			t.Errorf("%s: am start got %q, want %s", c.pkg, got, c.component)
		}
	}

	a, _ := launchADB(t)
	err := a.LaunchApp(context.Background(), "missing.app")
	var me *mobiumerr.Error
	if !errors.As(err, &me) || me.Code != mobiumerr.InvalidArgument {
		t.Errorf("an app with neither launcher: %v, want invalid_argument", err)
	}
}
