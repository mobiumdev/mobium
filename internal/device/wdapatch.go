package device

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// wdaPatchLevel names the changes Mobium makes to WebDriverAgent's pinned
// source, or to how it is built, before building it for a phone. A build made
// at an older level is built again, so a phone never keeps running a runner
// from before a fix. Level 2 renamed the runner (PhoneWDAName).
const wdaPatchLevel = "2"

// wdaStreamBind is the one change, to WebDriverAgentLib/Routing/FBWebServer.m.
//
// WebDriverAgent listens where USE_IP says for its server, but creates its
// MJPEG stream's socket without an interface, so the stream listens on every
// interface whatever USE_IP says — measured on a simulator (CHALLENGES 152)
// and on the iPhone 15 Plus, where it answered anyone on the phone's Wi-Fi
// with the screen after the server had been bound (153). The socket can bind
// one interface; the server never tells it which. This tells it, before it
// starts. Upstream's fix would be the same line; when a release has it, the
// pin moves and this goes.
var wdaStreamBind = struct {
	file, anchor, line string
}{
	file:   filepath.Join("WebDriverAgentLib", "Routing", "FBWebServer.m"),
	anchor: "  self.mjpegServer.socket = self.screenshotsBroadcaster;\n",
	line:   "  self.screenshotsBroadcaster.interface = FBConfiguration.sharedInstance.bindingIPAddress;\n",
}

// patchWDASource applies Mobium's changes to an unpacked source tree. Applying
// twice is a no-op. A source whose text no longer has the anchor is refused:
// building it unpatched would bring back what the patch exists to stop.
func patchWDASource(src string) error {
	path := filepath.Join(src, wdaStreamBind.file)
	raw, err := os.ReadFile(path)
	if err != nil {
		return mobiumerr.Wrap(mobiumerr.ToolchainMissing, err, "cannot read %s to bind the screen stream: %v", path, err)
	}
	patched, err := bindStream(string(raw))
	if err != nil {
		return err
	}
	if patched == string(raw) {
		return nil
	}
	return os.WriteFile(path, []byte(patched), 0o644)
}

func bindStream(text string) (string, error) {
	if strings.Contains(text, wdaStreamBind.line) {
		return text, nil
	}
	if strings.Count(text, wdaStreamBind.anchor) != 1 {
		return "", mobiumerr.New(mobiumerr.Internal, "WebDriverAgent %s's FBWebServer.m does not have the line Mobium's "+
			"patch attaches to, so its screen stream cannot be bound to the tunnel; building it would leave the "+
			"stream open to the phone's network", WDAVersion)
	}
	return strings.Replace(text, wdaStreamBind.anchor, wdaStreamBind.line+wdaStreamBind.anchor, 1), nil
}

// patchMarker is where a phone's build records the patch level it was built at.
func patchMarker(dir string) string { return filepath.Join(dir, "mobium-patch-level") }

func builtAtCurrentPatch(dir string) bool {
	raw, err := os.ReadFile(patchMarker(dir))
	return err == nil && strings.TrimSpace(string(raw)) == wdaPatchLevel
}
