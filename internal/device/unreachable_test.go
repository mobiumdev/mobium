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
