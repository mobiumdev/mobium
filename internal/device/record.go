package device

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// Recording is a screen recording in progress.
type Recording interface {
	// Started is when recording began, by the host's clock.
	Started() time.Time
	// Stop ends the recording cleanly, writes it to dest, and reports what
	// the file holds.
	Stop(ctx context.Context, dest string) (MP4Info, error)
	// Discard ends the recording cleanly and keeps nothing, anywhere.
	Discard(ctx context.Context)
}

// Both recorders finish their file only on SIGINT; stopWait bounds the wait
// for one to do so.
const stopWait = 15 * time.Second

// ---- Android --------------------------------------------------------------

// androidRecordPath is where screenrecord writes on the device. One per
// device: a start removes any left by a recording that was killed, since a
// killed recorder leaves an unplayable file — and this project leaves
// nothing on a device.
const androidRecordPath = "/data/local/tmp/mobium-recording.mp4"

type androidRecording struct {
	adb     *ADB
	pid     int
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	started time.Time
}

// StartScreenRecord starts screenrecord with no time limit.
//
// The recorder's own limit is 180 seconds by default; `--time-limit 0`
// removes it, measured on API 35 and 37. It is started through `echo $$;
// exec`, so the pid printed is the recorder's own — the one SIGINT must
// reach, because SIGINT is what makes it write the header a player needs.
func (a *ADB) StartScreenRecord(ctx context.Context) (Recording, error) {
	_, _ = a.Shell(ctx, "rm", "-f", androidRecordPath)
	// The recorder outlives the call that starts it, so it gets a context
	// of its own; Stop and Discard end it.
	rctx, cancel := context.WithCancel(context.Background())
	cmd, buf, err := a.Start(rctx, "shell", "echo $$; exec screenrecord --time-limit 0 "+androidRecordPath)
	if err != nil {
		cancel()
		return nil, err
	}
	r := &androidRecording{adb: a, cmd: cmd, cancel: cancel, started: time.Now()}
	deadline := time.Now().Add(5 * time.Second)
	for r.pid == 0 && time.Now().Before(deadline) {
		if line, _, ok := strings.Cut(buf.String(), "\n"); ok {
			r.pid, _ = strconv.Atoi(strings.TrimSpace(line))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if r.pid == 0 {
		cancel()
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "screenrecord did not start: %s", strings.TrimSpace(buf.String()))
	}
	// Alive a moment later, or it refused its arguments and said why.
	time.Sleep(700 * time.Millisecond)
	if !r.alive(ctx) {
		cancel()
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "screenrecord exited as soon as it started: %s",
			strings.TrimSpace(strings.TrimPrefix(buf.String(), strconv.Itoa(r.pid))))
	}
	return r, nil
}

func (r *androidRecording) Started() time.Time { return r.started }

func (r *androidRecording) alive(ctx context.Context) bool {
	out, err := r.adb.Shell(ctx, "kill -0 "+strconv.Itoa(r.pid)+" 2>/dev/null && echo alive")
	return err == nil && strings.Contains(string(out), "alive")
}

// finish sends SIGINT and waits for the recorder to write its file and exit.
func (r *androidRecording) finish(ctx context.Context) error {
	_, _ = r.adb.Shell(ctx, "kill", "-2", strconv.Itoa(r.pid))
	deadline := time.Now().Add(stopWait)
	for r.alive(ctx) {
		if time.Now().After(deadline) {
			return mobiumerr.New(mobiumerr.NotConfirmed, "screenrecord did not finish within %s of being asked to", stopWait)
		}
		time.Sleep(200 * time.Millisecond)
	}
	done := make(chan struct{})
	go func() { _ = r.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	r.cancel()
	return nil
}

func (r *androidRecording) Stop(ctx context.Context, dest string) (MP4Info, error) {
	defer func() { _, _ = r.adb.Shell(ctx, "rm", "-f", androidRecordPath) }()
	if err := r.finish(ctx); err != nil {
		return MP4Info{}, err
	}
	if _, err := r.adb.Run(ctx, "pull", androidRecordPath, dest); err != nil {
		return MP4Info{}, fmt.Errorf("copy the recording off the device: %w", err)
	}
	return ProbeMP4(dest)
}

func (r *androidRecording) Discard(ctx context.Context) {
	_ = r.finish(ctx)
	_, _ = r.adb.Shell(ctx, "rm", "-f", androidRecordPath)
}

// ---- iOS simulator ----------------------------------------------------------

type simRecording struct {
	cmd     *exec.Cmd
	tmp     string
	started time.Time
	out     *SyncBuffer
	// done closes when simctl exits, whoever asked it to.
	done chan struct{}
}

// StartScreenRecord records the simulator's screen to a file on the Mac,
// which is where a simulator's display is.
func (s *Simctl) StartScreenRecord(ctx context.Context, dir string) (Recording, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	tmp := filepath.Join(dir, fmt.Sprintf("recording-%s-%d.mp4", s.UDID, time.Now().UnixNano()))
	cmd := exec.Command(s.Path, "simctl", "io", s.UDID, "recordVideo", "--codec", "h264", "--force", tmp)
	out := &SyncBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start simctl recordVideo: %w", err)
	}
	r := &simRecording{cmd: cmd, tmp: tmp, started: time.Now(), out: out, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(r.done) }()
	// simctl prints "Recording started" once it is; a failure prints why
	// and exits, so wait for one or the other.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "Recording started") {
			return r, nil
		}
		select {
		case <-r.done:
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "simctl recordVideo exited: %s", strings.TrimSpace(out.String()))
		case <-time.After(50 * time.Millisecond):
		}
	}
	return r, nil
}

func (r *simRecording) Started() time.Time { return r.started }

func (r *simRecording) finish() error {
	_ = r.cmd.Process.Signal(os.Interrupt)
	select {
	case <-r.done:
		return nil
	case <-time.After(stopWait):
		_ = r.cmd.Process.Kill()
		return mobiumerr.New(mobiumerr.NotConfirmed, "simctl did not finish the recording within %s", stopWait)
	}
}

func (r *simRecording) Stop(ctx context.Context, dest string) (MP4Info, error) {
	defer os.Remove(r.tmp)
	if err := r.finish(); err != nil {
		return MP4Info{}, err
	}
	if err := moveFile(r.tmp, dest); err != nil {
		return MP4Info{}, err
	}
	return ProbeMP4(dest)
}

func (r *simRecording) Discard(ctx context.Context) {
	_ = r.finish()
	os.Remove(r.tmp)
}

// moveFile renames, and copies when the rename crosses a filesystem.
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, 0o644)
}
