//go:build !windows

package device

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CHALLENGES 285: a frozen emulator ignores `adb emu kill`, so shutdown
// ends it through the process its discovery file names — and only a
// process ps calls an emulator, since a stale file can name a reused pid.
func TestKillEmulatorProcess(t *testing.T) {
	dir := t.TempDir()
	start := func(name string) *exec.Cmd {
		bin := filepath.Join(t.TempDir(), name)
		sleep, err := exec.LookPath("sleep")
		if err != nil {
			t.Skip("no sleep")
		}
		// A link, not a copy: macOS will not run a copy of a signed system
		// binary, and ps names a process for the path it was started by.
		if err := os.Symlink(sleep, bin); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin, "30")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		return cmd
	}
	file := func(pid int) {
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("pid_%d.ini", pid)), []byte("port.serial=5554\n"), 0o600)
	}

	other := start("sleep")
	file(other.Process.Pid)
	if err := killEmulatorProcess("emulator-5554", []string{dir}); err == nil || !strings.Contains(err.Error(), "not an emulator") {
		t.Errorf("a file naming a process that is no emulator: %v", err)
	}
	if other.ProcessState != nil {
		t.Fatal("a process that is no emulator was killed")
	}

	time.Sleep(20 * time.Millisecond) // the emulator's file is the newer
	emu := start("qemu-system-aarch64")
	file(emu.Process.Pid)
	if err := killEmulatorProcess("emulator-5554", []string{dir}); err != nil {
		t.Fatalf("the emulator's own process: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- emu.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the emulator's process was not ended")
	}

	if err := killEmulatorProcess("emulator-5556", []string{dir}); err == nil {
		t.Error("a serial no file names was not refused")
	}
}
