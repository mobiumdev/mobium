package device

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/fakecmd"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// The stderr lines are what adb printed on 2026-10-02 while a Fire TV's
// Wi-Fi link was down, and the forms it uses for a serial it does not know.
func TestUnreachable(t *testing.T) {
	cases := []struct {
		serial, stderr string
		want           bool
		remedy         string
	}{
		{"10.0.0.77:5555", "adb: device offline\n", true, "adb connect 10.0.0.77:5555"},
		{"10.0.0.77:5555", "error: device offline\n", true, "adb connect 10.0.0.77:5555"},
		{"emulator-5554", "error: device 'emulator-5554' not found\n", true, "adb devices"},
		{"emulator-5554", "adb: no devices/emulators found\n", true, "adb devices"},
		// A command on the device printing similar words is not adb saying
		// the device is gone.
		{"emulator-5554", "ls: /sdcard/x: No such file or directory\n", false, ""},
		{"emulator-5554", "Error: device offline is not a package\n", false, ""},
		{"emulator-5554", "", false, ""},
	}
	for _, c := range cases {
		err := unreachable(c.serial, c.stderr)
		if (err != nil) != c.want {
			t.Errorf("%q: got %v, want unreachable=%v", c.stderr, err, c.want)
			continue
		}
		if err == nil {
			continue
		}
		me, _ := mobiumerr.As(err)
		if me.Code != mobiumerr.DeviceNotReady || !strings.Contains(me.Remedy, c.remedy) ||
			!strings.Contains(me.Message, me.Remedy) {
			t.Errorf("%q: %s, remedy %q; want device_not_ready naming %q", c.stderr, me.Code, me.Remedy, c.remedy)
		}
	}
}

// A device that cannot be asked whether the server is installed is not one
// without it: the session must fail as unreachable, and install nothing.
func TestEnsureUIA2InstalledOnAnOfflineDevice(t *testing.T) {
	dir := t.TempDir()
	installs := filepath.Join(dir, "installs")
	bin := fakecmd.Script(t, dir, "adb", `case "$*" in
  *install*) echo x >> `+fakecmd.Path(installs)+`; echo "adb: device offline" >&2; exit 1 ;;
  *) echo "adb: device offline" >&2; exit 1 ;;
esac`)
	a := &ADB{Path: bin, Serial: "10.0.0.77:5555"}
	err := a.EnsureUIA2Installed(context.Background(), nil)
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady {
		t.Fatalf("got %v, want device_not_ready", err)
	}
	if _, statErr := os.Stat(installs); statErr == nil {
		t.Error("an install was attempted on a device that could not be reached")
	}
}

// netADB is an adb that lists a network device as offline until it is
// disconnected and connected again, as a Fire TV's was in a latency spike —
// and, with stayDown, one whose connect never brings it back.
func netADB(t *testing.T, stayDown bool) (string, string) {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	connect := `echo "connected to 10.0.0.77:5555"; [ -f ` + fakecmd.Path(state) + `.cut ] && echo up > ` + fakecmd.Path(state)
	if stayDown {
		connect = `echo "failed to connect to 10.0.0.77:5555"`
	}
	bin := fakecmd.Script(t, dir, "adb", `case "$*" in
  *"devices -l"*) echo "List of devices attached"; if [ -f `+fakecmd.Path(state)+` ]; then echo "10.0.0.77:5555 device product:x model:AFTHA001"; else echo "10.0.0.77:5555 offline"; fi ;;
  *disconnect*) touch `+fakecmd.Path(state)+`.cut; echo "disconnected 10.0.0.77:5555" ;;
  *connect*) `+connect+` ;;
esac`)
	t.Setenv("MOBIUM_ADB_PATH", bin)
	return bin, state
}

func TestSelectConnectsANetworkDeviceAgain(t *testing.T) {
	netADB(t, false)
	_, d, err := Select(context.Background(), "10.0.0.77:5555")
	if err != nil || d == nil || !d.Ready() {
		t.Fatalf("got %v, %+v; want the device back after one reconnect", err, d)
	}
}

// A device that does not come back is refused as before, now marked as a
// call that never reached it — the mark the test runner waits on.
func TestSelectMarksAnUnreachedDevice(t *testing.T) {
	netADB(t, true)
	_, _, err := Select(context.Background(), "10.0.0.77:5555")
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !Unreached(err) {
		t.Fatalf("got %v; want device_not_ready marked as never reaching the device", err)
	}
	if e, _ := mobiumerr.As(err); !e.Retryable {
		t.Error("an unreached call is not marked retryable")
	}
	// A failure from inside a call carries no mark: it may have reached
	// the device.
	if Unreached(unreachable("10.0.0.77:5555", "adb: device offline\n")) {
		t.Error("an error from inside a call was marked as never reaching the device")
	}
}
