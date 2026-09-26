package mobiumdriver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
)

// fakeADB writes a stand-in adb that logs its arguments and replays canned
// responses, so the driver's command construction and output handling are
// exercised without an emulator.
func fakeADB(t *testing.T, script string) (*device.ADB, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	bin := filepath.Join(dir, "adb")

	body := "#!/bin/sh\necho \"$@\" >> " + logPath + "\n" + script
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MOBIUM_ADB_PATH", bin)

	adb, err := device.New("emulator-5554")
	if err != nil {
		t.Fatal(err)
	}
	return adb, logPath
}

func calls(t *testing.T, logPath string) string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	return string(data)
}

const miniDump = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<hierarchy rotation="0"><node index="0" text="Hello" resource-id="com.x:id/greeting" class="android.widget.Button" package="com.x" content-desc="" checkable="false" checked="false" clickable="true" enabled="true" focusable="true" focused="false" scrollable="false" long-clickable="false" password="false" selected="false" bounds="[10,20][110,80]" /></hierarchy>`

func TestAndroidSnapshot(t *testing.T) {
	script := `
case "$*" in
  *"uiautomator dump"*) echo "UI hierchary dumped to: /data/local/tmp/mobium-dump.xml" ;;
  *"cat /data/local/tmp/mobium-dump.xml"*) cat <<'XML'
` + miniDump + `
XML
  ;;
esac
`
	adb, logPath := fakeADB(t, script)
	tree, err := NewAndroid(adb).Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	entries := tree.Map()
	if len(entries) != 1 {
		t.Fatalf("mapped %d entries, want 1", len(entries))
	}
	if entries[0].Line() != "@e1 Hello (button)" {
		t.Errorf("entry = %q", entries[0].Line())
	}

	log := calls(t, logPath)
	if !strings.Contains(log, "-s emulator-5554") {
		t.Error("driver did not pin the device serial")
	}
	// exec-out, not shell: shell mangles bytes on some platforms.
	if !strings.Contains(log, "exec-out cat") {
		t.Errorf("hierarchy was not read with exec-out:\n%s", log)
	}
}

func TestAndroidSnapshotRetriesThenFails(t *testing.T) {
	// uiautomator's transient failure: exits 0, prints an error, writes no file.
	script := `
case "$*" in
  *"uiautomator dump"*) echo "ERROR: null root node returned by UiTestAutomationBridge." ;;
esac
`
	adb, logPath := fakeADB(t, script)
	_, err := NewAndroid(adb).Snapshot(context.Background())
	if err == nil {
		t.Fatal("expected an error when uiautomator produces no dump")
	}
	if !strings.Contains(err.Error(), "attempts") {
		t.Errorf("error %q does not mention the retries", err)
	}
	if got := strings.Count(calls(t, logPath), "uiautomator dump"); got != snapshotRetries {
		t.Errorf("dumped %d times, want %d", got, snapshotRetries)
	}
}

func TestAndroidSnapshotRejectsEmptyDump(t *testing.T) {
	script := `
case "$*" in
  *"uiautomator dump"*) echo "UI hierchary dumped to: /data/local/tmp/mobium-dump.xml" ;;
  *"cat /data/local/tmp/mobium-dump.xml"*) : ;;
esac
`
	adb, _ := fakeADB(t, script)
	if _, err := NewAndroid(adb).Snapshot(context.Background()); err == nil {
		t.Fatal("expected an error for an empty hierarchy")
	}
}

func TestAndroidScreenshot(t *testing.T) {
	script := `
case "$*" in
  *"screencap -p"*) printf '\211PNG\r\n\032\n fake png bytes' ;;
esac
`
	adb, logPath := fakeADB(t, script)
	png, err := NewAndroid(adb).Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !strings.HasPrefix(string(png), "\x89PNG") {
		t.Errorf("returned %d bytes that are not a PNG", len(png))
	}
	if !strings.Contains(calls(t, logPath), "exec-out screencap -p") {
		t.Error("screenshot did not use exec-out")
	}
}

func TestAndroidScreenshotRejectsNonPNG(t *testing.T) {
	// A device that prints an error instead of image bytes must not produce a
	// corrupt "screenshot" file that looks like a successful capture.
	script := `
case "$*" in
  *"screencap -p"*) echo "error: device offline" ;;
esac
`
	adb, _ := fakeADB(t, script)
	_, err := NewAndroid(adb).Screenshot(context.Background())
	if err == nil {
		t.Fatal("expected an error for non-PNG output")
	}
	if !strings.Contains(err.Error(), "not a PNG") {
		t.Errorf("error = %v", err)
	}
}

func TestAndroidTap(t *testing.T) {
	adb, logPath := fakeADB(t, "")
	if err := NewAndroid(adb).Tap(context.Background(), 540, 930); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	if !strings.Contains(calls(t, logPath), "shell input tap 540 930") {
		t.Errorf("unexpected tap command:\n%s", calls(t, logPath))
	}
}

func TestAndroidSurfacesADBErrorOnStderr(t *testing.T) {
	// adb exits 0 and reports "error:" on stderr for a surprising number of
	// real failures; treating exit 0 as success hides them.
	adb, _ := fakeADB(t, `echo "error: device 'emulator-5554' not found" >&2`)
	err := NewAndroid(adb).Tap(context.Background(), 1, 2)
	if err == nil {
		t.Fatal("expected an error from adb's stderr")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v", err)
	}
}

func TestAndroidSnapshotExplainsANeverIdleScreen(t *testing.T) {
	// `uiautomator dump` waits for the screen to stop changing, and reports
	// giving up on stderr — where ADB.Run does not look, because adb's own
	// convention is a lowercase "error:" prefix. Reproduced on a Pixel 7: the
	// About page has an uptime counter, and this backend cannot read it at
	// all while the message said only "produced no file:".
	script := `
case "$*" in
  *"uiautomator dump"*) echo "ERROR: could not get idle state." >&2 ;;
esac
`
	adb, _ := fakeADB(t, script)
	_, err := NewAndroid(adb).Snapshot(context.Background())
	if err == nil {
		t.Fatal("a dump that produced nothing was accepted")
	}
	if !strings.Contains(err.Error(), "could not get idle state") {
		t.Errorf("error %q drops the reason, which is on stderr", err)
	}
	// A backend that cannot do something must name the fix.
	if !strings.Contains(err.Error(), "uiautomator2") {
		t.Errorf("error %q does not point at the backend that can read it", err)
	}
}

func TestAndroidSnapshotStillReadsFailuresOnStdout(t *testing.T) {
	// The other half of defect 13: uiautomator puts some failures on stdout.
	// Reading stderr must not have stopped it reading those.
	script := `
case "$*" in
  *"uiautomator dump"*) echo "killed" ;;
esac
`
	adb, _ := fakeADB(t, script)
	_, err := NewAndroid(adb).Snapshot(context.Background())
	if err == nil {
		t.Fatal("a dump that reported 'killed' was accepted")
	}
	if !strings.Contains(err.Error(), "killed") {
		t.Errorf("error %q lost what stdout said", err)
	}
}

func TestAndroidSnapshotRemovesItsDumpFromTheDevice(t *testing.T) {
	// The dump is a serialization of whatever is on screen. Found on a real
	// Pixel after a session: 46KB of somebody's screen still sitting in
	// /data/local/tmp, because nothing ever deleted it.
	script := `
case "$*" in
  *"uiautomator dump"*) echo "UI hierchary dumped to: /data/local/tmp/mobium-dump.xml" ;;
  *"cat /data/local/tmp/mobium-dump.xml"*) printf '%s' '` + miniDump + `' ;;
esac
`
	adb, logPath := fakeADB(t, script)
	if _, err := NewAndroid(adb).Snapshot(context.Background()); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !strings.Contains(calls(t, logPath), "rm -f /data/local/tmp/mobium-dump.xml") {
		t.Errorf("the dump was left on the device; calls were:\n%s", calls(t, logPath))
	}
}

func TestSnapshotSucceedsEvenIfTheCleanupFails(t *testing.T) {
	// Tidying up is not the job. A snapshot that worked must not be reported
	// as a failure because the device would not delete a file.
	script := `
case "$*" in
  *"uiautomator dump"*) echo "UI hierchary dumped to: /data/local/tmp/mobium-dump.xml" ;;
  *"cat /data/local/tmp/mobium-dump.xml"*) printf '%s' '` + miniDump + `' ;;
  *"rm -f"*) echo "rm: permission denied" >&2; exit 1 ;;
esac
`
	adb, _ := fakeADB(t, script)
	if _, err := NewAndroid(adb).Snapshot(context.Background()); err != nil {
		t.Errorf("a failed cleanup broke the snapshot: %v", err)
	}
}
