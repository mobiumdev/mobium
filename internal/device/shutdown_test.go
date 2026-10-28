package device

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/fakecmd"
)

// slowExitADB is an adb whose emulator answers `emu kill` at once, then stays
// listed for as long as the gone file is absent — as one on a Windows laptop
// stayed listed for 21 seconds after "bye bye", its discovery file deleted
// within two (CHALLENGES 376). answer false is a frozen console instead.
func slowExitADB(t *testing.T, answer bool) (gone string) {
	t.Helper()
	dir := t.TempDir()
	gone = filepath.Join(dir, "gone")
	kill := `echo "OK: killing emulator, bye bye"; echo OK`
	if !answer {
		kill = `echo "error: connection refused" >&2; exit 1`
	}
	bin := fakecmd.Script(t, dir, "adb", `case "$*" in
  *"devices -l"*) echo "List of devices attached"; [ -f `+fakecmd.Path(gone)+` ] || echo "emulator-5554 device product:sdk_gphone64_arm64 model:sdk_gphone64_arm64 device:emu64a transport_id:1" ;;
  *"emu kill"*) `+kill+` ;;
esac`)
	t.Setenv("MOBIUM_ADB_PATH", bin)
	empty := t.TempDir()
	old, oldWait, oldGone := discoveryDirs, emuKillWait, goneTimeout
	discoveryDirs = func() []string { return []string{empty} }
	emuKillWait, goneTimeout = 300*time.Millisecond, 10*time.Second
	t.Cleanup(func() { discoveryDirs, emuKillWait, goneTimeout = old, oldWait, oldGone })
	return gone
}

// An emulator that said it was killing itself and has already deleted its
// discovery file is exiting, only slowly: shutdown waits for it instead of
// reporting a console that did not answer.
func TestShutdownWaitsOutASlowExit(t *testing.T) {
	gone := slowExitADB(t, true)
	go func() {
		time.Sleep(1500 * time.Millisecond) // well past emuKillWait
		os.WriteFile(gone, nil, 0o644)
	}()
	if err := ShutdownEmulator(context.Background(), "emulator-5554"); err != nil {
		t.Fatalf("a slow but answered shutdown failed: %v", err)
	}
}

// A console that did not answer is still reported as one, with the reason
// the process could not be ended either.
func TestShutdownStillNamesAConsoleThatDidNotAnswer(t *testing.T) {
	slowExitADB(t, false)
	err := ShutdownEmulator(context.Background(), "emulator-5554")
	if err == nil || !strings.Contains(err.Error(), "did not answer its console") ||
		!strings.Contains(err.Error(), "not named by any discovery file") {
		t.Fatalf("got %v; want the console named as not answering, and why the process was not ended", err)
	}
}
