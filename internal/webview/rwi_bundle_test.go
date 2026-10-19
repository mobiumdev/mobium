package webview

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// Under Xcode 27.1 a simulator's webinspectord names an app it did not ship
// "process-<name>", with the identifier PID:<n>, and a simulator's app is a
// process on this Mac: the bundle is read from the executable's app. A real
// process in a made-up app stands in for it. A phone's PIDs are not this
// Mac's, a bundle id as given is kept, and so is the fallback when the
// process runs no app. CHALLENGES 344.
func TestBundleOfAProcessWebKitCouldNotName(t *testing.T) {
	// A simulator runs only on a Mac, and the lookup reads macOS's ps, whose
	// comm is the executable's whole path; Linux's is its name alone, and
	// Windows will not run the stand-in without an .exe.
	if runtime.GOOS != "darwin" {
		t.Skip("simulators, and this lookup, are macOS's")
	}
	app := filepath.Join(t.TempDir(), "Demo.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	plistXML := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>CFBundleIdentifier</key><string>dev.mobium.demo</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Info.plist"), []byte(plistXML), 0o644); err != nil {
		t.Fatal(err)
	}
	// The app's executable is this test binary, copied in and run as a
	// helper that sleeps: a copy of /bin/sleep is killed at once, since
	// macOS runs system binaries only from where they are installed.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(app, "Demo")
	if err := os.WriteFile(exe, bin, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestHelperSleeps$")
	cmd.Env = append(os.Environ(), "MOBIUM_HELPER_SLEEP=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	id := "PID:" + strconv.Itoa(cmd.Process.Pid)

	sim := &inspector{hostPIDs: true}
	if got := sim.bundleOf(id, "process-Demo"); got != "dev.mobium.demo" {
		t.Errorf("on a simulator, %s named process-Demo is %q, want dev.mobium.demo", id, got)
	}
	if got := sim.bundleOf(id, "com.apple.mobilesafari"); got != "com.apple.mobilesafari" {
		t.Errorf("a bundle id as given became %q", got)
	}
	if got := sim.bundleOf("PID:"+strconv.Itoa(os.Getpid()), "process-test"); got != "process-test" {
		t.Errorf("a process that runs no app became %q", got)
	}
	phone := &inspector{}
	if got := phone.bundleOf(id, "process-Demo"); got != "process-Demo" {
		t.Errorf("on a phone, %s was looked up on this Mac: %q", id, got)
	}
}

// TestHelperSleeps is the stand-in app's process, run by the test above.
func TestHelperSleeps(t *testing.T) {
	if os.Getenv("MOBIUM_HELPER_SLEEP") == "" {
		t.Skip("a helper process for TestBundleOfAProcessWebKitCouldNotName")
	}
	time.Sleep(30 * time.Second)
}
