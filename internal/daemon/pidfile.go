package daemon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/paths"
)

// WritePID records this process as the running daemon.
func WritePID() error {
	pidPath, err := paths.PIDPath()
	if err != nil {
		return err
	}
	if err := paths.EnsureDaemonDir(); err != nil {
		return err
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}
	return nil
}

// ReadPID returns the recorded daemon PID, or 0 when there is no PID file.
func ReadPID() (int, error) {
	pidPath, err := paths.PIDPath()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(pidPath)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file content: %w", err)
	}
	return pid, nil
}

// RemovePID deletes the PID file.
//
// It tries for up to a second, because on Windows a file cannot be deleted
// while anything has it open, and a client waiting for this daemon to go is
// reading it every 20ms. One collision left the file naming a daemon that had
// stopped, and the client waited out its whole 35s grace for it.
// CHALLENGES 140.
func RemovePID() error {
	pidPath, err := paths.PIDPath()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(time.Second)
	for {
		err := os.Remove(pidPath)
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// CleanStale removes the PID and socket files of a daemon that is no longer
// running. Without this, a crashed daemon's socket file makes every later
// command fail to connect and every auto-start fail to bind.
func CleanStale() {
	pid, err := ReadPID()
	if err != nil || pid == 0 {
		return
	}
	if Running(pid) {
		return
	}
	RemovePID()
	if socketPath, err := paths.SocketPath(); err == nil {
		removeSocket(socketPath)
	}
}
