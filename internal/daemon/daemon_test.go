package daemon

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/paths"
)

// shortTempDir returns a temp directory short enough for a Unix socket path.
//
// Go's t.TempDir() embeds the test name under /var/folders/... on macOS, which
// overruns sockaddr_un's 104-byte sun_path for any test with a long name — the
// guard in paths.SocketPath catches it, and the failure is the guard, not the
// daemon. Real users hit the same wall with a deep MOBIUM_HOME. Windows has no
// such limit, since the daemon listens on a named pipe, and no /tmp.
func shortTempDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "mob")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// startDaemon runs a daemon in a temp home and returns a stop function. It
// exercises the real socket, the real client and the real tool layer — the
// three pieces that only fail together.
func startDaemon(t *testing.T) func() {
	t.Helper()
	home := shortTempDir(t)
	t.Setenv("MOBIUM_HOME", home)
	t.Setenv("MOBIUM_SESSION", "")

	d := New(Options{Version: "test", IdleTimeout: 0})
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := Status(); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := Status(); err != nil {
		cancel()
		t.Fatalf("daemon never came up: %v", err)
	}

	return func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
			t.Error("daemon did not shut down")
		}
	}
}

func TestDaemonStatus(t *testing.T) {
	defer startDaemon(t)()

	status, err := Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Version != "test" {
		t.Errorf("version = %q", status.Version)
	}
	if status.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", status.PID, os.Getpid())
	}
	if want, _ := paths.SocketPath(); status.Socket != want {
		t.Errorf("socket = %q, want %q", status.Socket, want)
	}
}

// Two state directories must reach two daemons. On Unix that is free, because
// the socket is inside MOBIUM_HOME; a named pipe is machine-wide, and without
// the directory in its name a second MOBIUM_HOME would talk to the first one's
// daemon.
func TestSeparateHomesReachSeparateDaemons(t *testing.T) {
	defer startDaemon(t)()

	t.Setenv("MOBIUM_HOME", shortTempDir(t))
	_, err := Status()
	if err == nil {
		t.Fatal("a second MOBIUM_HOME reached the first home's daemon")
	}
	if !IsConnectionError(err) {
		t.Errorf("no daemon should read as unreachable, so the CLI auto-starts one; got %T: %v", err, err)
	}
}

func TestDaemonServesTools(t *testing.T) {
	defer startDaemon(t)()

	// app_devices reaches the tool layer without needing a device: with no
	// adb on PATH it reports that, which still proves the round trip.
	_, err := Call("app_devices", nil)
	if err == nil {
		return // adb present and a device may be attached; either is fine
	}
	var toolErr *ToolError
	if !asToolError(err, &toolErr) {
		t.Fatalf("expected a tool error from the daemon, got %T: %v", err, err)
	}
	if IsConnectionError(err) {
		t.Error("a tool error was misreported as the daemon being unreachable")
	}
}

func TestDaemonRejectsUnknownTool(t *testing.T) {
	defer startDaemon(t)()

	_, err := Call("app_nope", nil)
	if err == nil {
		t.Fatal("expected an error for an unknown tool")
	}
	if IsConnectionError(err) {
		t.Errorf("unknown tool reported as a connection failure: %v", err)
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("error = %v", err)
	}
}

func TestDaemonWritesAndRemovesPIDAndSocket(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MOBIUM_HOME", home)
	t.Setenv("MOBIUM_SESSION", "")

	d := New(Options{Version: "test"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := Status(); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	pid, err := ReadPID()
	if err != nil || pid != os.Getpid() {
		t.Errorf("PID file has %d (err %v), want %d", pid, err, os.Getpid())
	}
	socket, err := paths.SocketPath()
	if err != nil {
		t.Fatal(err)
	}
	if !socketPresent(t, socket) {
		t.Errorf("socket %s not created", socket)
	}
	// The socket carries commands that drive a device; nobody else on the
	// host should be able to send them.
	checkOwnerOnly(t, socket)

	cancel()
	<-done

	if socketPresent(t, socket) {
		t.Errorf("socket %s survived shutdown", socket)
	}
	if pid, _ := ReadPID(); pid != 0 {
		t.Error("PID file survived shutdown")
	}
}

func TestDaemonShutdownOverTheWire(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MOBIUM_HOME", home)
	t.Setenv("MOBIUM_SESSION", "")

	d := New(Options{Version: "test"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := Status(); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The response must arrive before the socket closes.
	if err := Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not exit after daemon/shutdown")
	}
	if _, err := Status(); err == nil {
		t.Error("daemon still answering after shutdown")
	}
}

func TestIdleTimeoutShutsDown(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MOBIUM_HOME", home)
	t.Setenv("MOBIUM_SESSION", "")

	d := New(Options{Version: "test", IdleTimeout: 1200 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := Status(); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("idle daemon never shut itself down")
	}
}

func TestClientReportsUnreachableDaemon(t *testing.T) {
	t.Setenv("MOBIUM_HOME", shortTempDir(t))
	t.Setenv("MOBIUM_SESSION", "")

	_, err := Status()
	if err == nil {
		t.Fatal("expected an error with no daemon running")
	}
	// The CLI decides whether to auto-start on exactly this predicate.
	if !IsConnectionError(err) {
		t.Errorf("unreachable daemon not classified as a connection error: %v", err)
	}
}

func asToolError(err error, target **ToolError) bool {
	te, ok := err.(*ToolError)
	if ok {
		*target = te
	}
	return ok
}

func TestStructuredContentSurvivesTheSocket(t *testing.T) {
	// The CLI's --json and both clients read structuredContent, and every one
	// of them receives it through this socket. A field lost in the daemon's
	// marshal/unmarshal would send them all back to parsing prose.
	defer startDaemon(t)()

	// app_devices answers without a device attached, and carries a structured
	// payload either way.
	result, err := Call("app_devices", nil)
	if err != nil {
		var toolErr *ToolError
		if !asToolError(err, &toolErr) {
			t.Skipf("no toolchain available: %v", err)
		}
		return
	}
	if result.StructuredContent == nil {
		t.Fatal("the daemon dropped structuredContent")
	}
	payload, ok := result.StructuredContent.(map[string]interface{})
	if !ok {
		t.Fatalf("structuredContent came back as %T", result.StructuredContent)
	}
	if _, has := payload["devices"]; !has {
		t.Errorf("payload has no devices key: %v", payload)
	}
}

func TestWaitGone(t *testing.T) {
	// Shutdown used to return as soon as the daemon acknowledged the request,
	// while it was still closing its device session. `mobium daemon stop`
	// followed immediately by any command then failed every time: the next
	// command's auto-start found the socket still held, gave up, and waited
	// ten seconds for a daemon that never appeared. Reproduced five times out
	// of five, and one second of sleep in between hid it completely.
	//
	// Only waitGone is exercised here. The daemon in these tests runs in this
	// process rather than a child, so the PID file names the test binary and
	// nothing ever exits; the end-to-end sequence was verified against the
	// real CLI instead, five times in a row.
	proc := exec.Command("sh", "-c", "sleep 0.3")
	if err := proc.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := proc.Process.Pid
	go proc.Wait()

	if waitGone(pid, 20*time.Millisecond, false) {
		t.Error("reported a running process as gone")
	}
	if !waitGone(pid, 5*time.Second, false) {
		t.Error("did not notice the process exiting")
	}
	if !waitGone(pid, 0, false) {
		t.Error("an already-dead process was reported as running")
	}
}

// A tool call that never returns holds the lock that closing the sessions
// needs. Shutdown used to wait for it forever: SIGTERM and `daemon stop` both
// hung, with the socket already unlinked, so the daemon looked dead while
// its PID file refused a replacement. Closing is now bounded.
func TestShutdownIsBoundedWhenClosingHangs(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MOBIUM_HOME", home)
	t.Setenv("MOBIUM_SESSION", "")
	old := closeTimeout
	closeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { closeTimeout = old })

	d := New(Options{Version: "test"})
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	d.closeHandlers = func() { <-stuck }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := Status(); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown waited on a close that never finishes")
	}
	if pid, _ := ReadPID(); pid != 0 {
		t.Error("the PID file survived, so the next start would refuse")
	}
}
