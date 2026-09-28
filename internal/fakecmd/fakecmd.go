// Package fakecmd writes stand-in executables for tests: a fake adb, say,
// whose behavior is a few lines of sh.
//
// On Unix the stand-in is the script itself. Windows will not run a script
// as a program, so there it is a small launcher, built once per test binary,
// that runs the script beside it under sh -- the sh Git for Windows installs,
// which is on PATH wherever these tests are run. The scripts are the same on
// both, so a fake cannot say one thing on a Mac and another on Windows.
package fakecmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Script writes an executable named name into dir that runs body under sh,
// and returns its path. On Windows the path ends in .exe.
func Script(t testing.TB, dir, name, body string) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Fatalf("a fake command needs sh on PATH, which Git for Windows provides: %v", err)
	}
	launcher := buildLauncher(t)
	p := filepath.Join(dir, name+".exe")
	data, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".sh"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Path spells a path the way a script can use it. sh reads a backslash as an
// escape, so a Windows path goes in with forward slashes, which it accepts.
func Path(p string) string {
	return filepath.ToSlash(p)
}

var (
	launcherOnce sync.Once
	launcherPath string
	launcherErr  string
)

func buildLauncher(t testing.TB) string {
	t.Helper()
	launcherOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mobium-fakecmd")
		if err != nil {
			launcherErr = err.Error()
			return
		}
		out := filepath.Join(dir, "launcher.exe")
		build := exec.Command("go", "build", "-o", out, "github.com/mobiumdev/mobium/internal/fakecmd/launcher")
		if b, err := build.CombinedOutput(); err != nil {
			launcherErr = err.Error() + "\n" + strings.TrimSpace(string(b))
			return
		}
		launcherPath = out
	})
	if launcherErr != "" {
		t.Fatalf("could not build the fake-command launcher: %s", launcherErr)
	}
	return launcherPath
}
