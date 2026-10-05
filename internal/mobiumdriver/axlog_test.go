package mobiumdriver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/device"
)

// Every read, record, change and restore of a phone's accessibility setting
// is a line in accessibility.log, naming the phone by its UDID, so a restore
// that ends wrong says which read it followed (CHALLENGES 243). A log that
// grows past its size starts again, the old one kept beside it.
func TestTheAccessibilityLogKeepsEachStep(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MOBIUM_HOME", home)
	phone := &device.Devicectl{Phone: device.Phone{UDID: "00008120-TEST"}}
	axLog(phone, "read %s %s (from Settings)%s", "reduce_motion", "on", errNote(nil))
	axLog(phone, "restore %s to %s: reads %s%s", "reduce_motion", "off", "on", errNote(errors.New("not confirmed")))
	raw, err := os.ReadFile(filepath.Join(home, "accessibility.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "00008120-TEST read reduce_motion on (from Settings)") ||
		!strings.HasSuffix(lines[1], "restore reduce_motion to off: reads on, failed: not confirmed") {
		t.Errorf("log = %q", raw)
	}

	big := strings.Repeat("x", axLogMax+1)
	if err := os.WriteFile(filepath.Join(home, "accessibility.log"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	axLog(phone, "read %s %s", "bold_text", "off")
	if b, _ := os.ReadFile(filepath.Join(home, "accessibility.log")); !strings.Contains(string(b), "read bold_text off") || len(b) > 200 {
		t.Errorf("the log did not start again: %d bytes", len(b))
	}
	if _, err := os.Stat(filepath.Join(home, "accessibility.log.1")); err != nil {
		t.Errorf("the old log was not kept: %v", err)
	}
}
