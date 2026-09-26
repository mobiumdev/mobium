package device

import (
	"os"
	"path/filepath"
	"testing"
)

// launchctl's list, as an iPhone 17 Pro simulator printed it with MobiumApp
// running — and the same job known to launchd with no process.
func TestRunningIn(t *testing.T) {
	marker := "UIKitApplication:dev.mobium.mobiumapp["
	running := "PID\tStatus\tLabel\n3682\t0\tUIKitApplication:dev.mobium.mobiumapp[2f7f][rb-legacy]\n"
	if !runningIn(running, marker) {
		t.Error("a listed pid was not seen as running")
	}
	stopped := "PID\tStatus\tLabel\n-\t0\tUIKitApplication:dev.mobium.mobiumapp[2f7f][rb-legacy]\n"
	if runningIn(stopped, marker) {
		t.Error("a job with no pid was seen as running")
	}
	// A bundle id that is a prefix of another is not that other app.
	other := "3682\t0\tUIKitApplication:dev.mobium.mobiumapp.extra[2f7f]\n"
	if runningIn(other, marker) {
		t.Error("a longer bundle id matched")
	}
}

// emptyDir keeps the directory, since a fresh install has it, and removes
// everything under it however deep.
func TestEmptyDir(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "Library")
	if err := os.MkdirAll(filepath.Join(lib, "Preferences", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lib, "Preferences", "deep", "x.plist"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := emptyDir(lib); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(lib)
	if err != nil {
		t.Fatalf("the directory itself went: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("%d entries left", len(entries))
	}
	if err := emptyDir(filepath.Join(dir, "absent")); err != nil {
		t.Errorf("an absent directory is already empty: %v", err)
	}
}
