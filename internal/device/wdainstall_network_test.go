package device

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// These tests reach the network and are opt-in:
//
//	MOBIUM_NETWORK_TESTS=1 go test ./internal/device/ -run Network -v
//
// They exist because the pinned checksums and the archive's shape are claims
// about a file on someone else's server. Unit tests can prove the download
// logic with a fake server; only this proves the pin is still correct.
func skipUnlessNetwork(t *testing.T) {
	t.Helper()
	if os.Getenv("MOBIUM_NETWORK_TESTS") != "1" {
		t.Skip("set MOBIUM_NETWORK_TESTS=1 to run tests that download from GitHub")
	}
}

func TestNetworkEnsureWDARunner(t *testing.T) {
	skipUnlessNetwork(t)
	t.Setenv("MOBIUM_HOME", shortTempDir(t))

	var progress []string
	app, err := EnsureWDARunner(context.Background(), func(m string) { progress = append(progress, m) })
	if err != nil {
		t.Fatalf("EnsureWDARunner: %v", err)
	}

	// A cold run must report what it is doing; a silent 6MB download looks
	// like a hang.
	if len(progress) == 0 {
		t.Error("a cold download reported no progress")
	}

	// The runner is an app bundle; these three files are what makes it
	// launchable, and the executable bit is what an unzip most easily loses.
	exe := filepath.Join(app, "WebDriverAgentRunner-Runner")
	fi, err := os.Stat(exe)
	if err != nil {
		t.Fatalf("runner executable missing: %v", err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("runner executable is not executable: mode %v", fi.Mode().Perm())
	}
	for _, rel := range []string{
		"Info.plist",
		"PlugIns/WebDriverAgentRunner.xctest",
		"Frameworks/XCTest.framework",
	} {
		if _, err := os.Stat(filepath.Join(app, rel)); err != nil {
			t.Errorf("bundle is missing %s", rel)
		}
	}

	// A second call must be served from the cache without downloading again.
	progress = nil
	again, err := EnsureWDARunner(context.Background(), func(m string) { progress = append(progress, m) })
	if err != nil {
		t.Fatalf("second EnsureWDARunner: %v", err)
	}
	if again != app {
		t.Errorf("second call returned %q, want %q", again, app)
	}
	if len(progress) != 0 {
		t.Errorf("a cached runner was re-downloaded: %v", progress)
	}
}

func TestNetworkUIA2APKsMatchTheirPins(t *testing.T) {
	skipUnlessNetwork(t)
	t.Setenv("MOBIUM_HOME", shortTempDir(t))

	apks, err := EnsureUIA2APKs(context.Background(), nil)
	if err != nil {
		t.Fatalf("EnsureUIA2APKs: %v", err)
	}
	if len(apks) != 2 {
		t.Fatalf("got %d APKs, want the server and test pair", len(apks))
	}
	for _, apk := range apks {
		fi, err := os.Stat(apk)
		if err != nil {
			t.Errorf("%s: %v", apk, err)
			continue
		}
		if fi.Size() == 0 {
			t.Errorf("%s is empty", apk)
		}
	}
}

// shortTempDir keeps paths clear of the sockaddr_un limit, and away from the
// user's real cache.
func shortTempDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "mobdev")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// The phone runner is built from a source archive GitHub generates on
// request, so its checksum is a claim about GitHub's archiver as much as
// about the tag. This is what notices if that claim stops holding.
func TestNetworkEnsureWDASource(t *testing.T) {
	skipUnlessNetwork(t)
	t.Setenv("MOBIUM_HOME", shortTempDir(t))

	var progress []string
	src, err := ensureWDASource(context.Background(), func(m string) { progress = append(progress, m) })
	if err != nil {
		t.Fatalf("ensureWDASource: %v", err)
	}
	if len(progress) == 0 {
		t.Error("a cold download reported no progress")
	}
	// What xcodebuild is pointed at, and the scheme it builds.
	for _, rel := range []string{
		"WebDriverAgent.xcodeproj/project.pbxproj",
		"WebDriverAgent.xcodeproj/xcshareddata/xcschemes/WebDriverAgentRunner.xcscheme",
	} {
		if _, err := os.Stat(filepath.Join(src, rel)); err != nil {
			t.Errorf("source is missing %s", rel)
		}
	}
}
