package webview

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/fakecmd"
)

// A frozen process's socket is never forwarded: on the Android 15 emulator
// each GET to one waited out the timeout and a few listings stopped adbd
// (CHALLENGES 379). The live socket beside it is still asked.
func TestContextsLeaveAFrozenProcessAlone(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	bin := fakecmd.Script(t, dir, "adb", "echo \"$@\" >> "+fakecmd.Path(logPath)+`
case "$*" in
  *"cat /proc/net/unix"*)
    echo "0000: 00000002 00000000 00010000 0001 01 1 @webview_devtools_remote_100"
    echo "0000: 00000002 00000000 00010000 0001 01 2 @webview_devtools_remote_200" ;;
  *cgroup.freeze*) echo "100 1"; echo "200 0" ;;
  *forward*tcp:0*) echo 1 ;;
esac
`)
	t.Setenv("MOBIUM_ADB_PATH", bin)
	adb, err := device.New("emulator-5554")
	if err != nil {
		t.Fatal(err)
	}

	sockets, err := Sockets(context.Background(), adb)
	if err != nil {
		t.Fatal(err)
	}
	if len(sockets) != 2 || !sockets[0].Frozen || sockets[1].Frozen {
		t.Fatalf("sockets = %+v, want 100 frozen and 200 not", sockets)
	}
	for _, list := range []func() error{
		func() error { _, err := Contexts(context.Background(), adb); return err },
		func() error { _, err := ContextsOf(context.Background(), adb, "com.example"); return err },
	} {
		if err := list(); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(logPath)
	calls := string(data)
	if strings.Contains(calls, "localabstract:webview_devtools_remote_100") {
		t.Errorf("the frozen process's socket was forwarded:\n%s", calls)
	}
	if !strings.Contains(calls, "localabstract:webview_devtools_remote_200") {
		t.Errorf("the live process's socket was not asked:\n%s", calls)
	}
}

// A device whose cgroup files cannot be read has nothing frozen, so every
// socket is asked as before.
func TestContextsWithNoFreezerAskEverySocket(t *testing.T) {
	dir := t.TempDir()
	bin := fakecmd.Script(t, dir, "adb", `
case "$*" in
  *"cat /proc/net/unix"*) echo "0000: 00000002 00000000 00010000 0001 01 1 @webview_devtools_remote_100" ;;
  *cgroup.freeze*) echo "100 " ;;
esac
`)
	t.Setenv("MOBIUM_ADB_PATH", bin)
	adb, err := device.New("emulator-5554")
	if err != nil {
		t.Fatal(err)
	}
	sockets, err := Sockets(context.Background(), adb)
	if err != nil {
		t.Fatal(err)
	}
	if len(sockets) != 1 || sockets[0].Frozen {
		t.Fatalf("sockets = %+v, want one, not frozen", sockets)
	}
}
