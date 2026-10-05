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

// An upload into an App Store app's Documents on a phone is refused by
// CoreDevice with error 7000, "The specified file could not be transferred",
// and that was all it said. It says why now, and what works. CHALLENGES 249.
func TestAnUploadIntoAClosedContainerSaysWhy(t *testing.T) {
	dir := t.TempDir()
	xcrun := fakecmd.Script(t, dir, "xcrun", `case "$*" in
  *"info apps"*) echo '{"info":{"outcome":"success"},"result":{"apps":[{"bundleIdentifier":"self.Kiwix","name":"Kiwix"}]}}' ;;
  *"copy to"*) echo '{"info":{"outcome":"failed"},"error":{"code":7000,"domain":"com.apple.dt.CoreDeviceError","userInfo":{"NSLocalizedDescription":{"string":"The specified file could not be transferred."}}}}'; exit 1 ;;
esac`)
	local := filepath.Join(dir, "ray.zim")
	if err := os.WriteFile(local, []byte("ZIM"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &Devicectl{Path: xcrun, Phone: Phone{UDID: "00008120-TEST"}}
	_, err := d.UploadFile(context.Background(), local, "ray.zim", "self.Kiwix")
	e, ok := mobiumerr.As(err)
	if !ok || e.Code != mobiumerr.Unsupported || !strings.Contains(e.Error(), "installed for development") ||
		!strings.Contains(e.Remedy, "through the app itself") {
		t.Errorf("the refusal did not say why: %v", err)
	}
}
