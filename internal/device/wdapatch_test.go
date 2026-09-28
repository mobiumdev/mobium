package device

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// As WebDriverAgent 16.12.8 has it, abbreviated.
const fbWebServerSample = `- (void)initScreenshotsBroadcaster
{
  [self readMjpegSettingsFromEnv];
  self.mjpegServer = [[FBMjpegServer alloc] init];
  self.screenshotsBroadcaster = [[FBTCPSocket alloc]
                                 initWithPort:(uint16_t)FBConfiguration.sharedInstance.mjpegServerPort];
  self.mjpegServer.socket = self.screenshotsBroadcaster;
  self.screenshotsBroadcaster.delegate = self.mjpegServer;
`

func TestBindStreamSetsTheInterfaceBeforeTheSocketStarts(t *testing.T) {
	got, err := bindStream(fbWebServerSample)
	if err != nil {
		t.Fatal(err)
	}
	set := strings.Index(got, "self.screenshotsBroadcaster.interface = FBConfiguration.sharedInstance.bindingIPAddress;")
	alloc := strings.Index(got, "[[FBTCPSocket alloc]")
	if set < alloc || set > strings.Index(got, "self.mjpegServer.socket =") {
		t.Errorf("the interface is not set between creating the socket and handing it over:\n%s", got)
	}
	again, err := bindStream(got)
	if err != nil || again != got {
		t.Errorf("patching twice changed it again: %v", err)
	}
}

// A source that no longer has the anchor is refused rather than built open.
func TestBindStreamRefusesAChangedSource(t *testing.T) {
	if _, err := bindStream("- (void)initScreenshotsBroadcaster\n{\n}\n"); err == nil {
		t.Error("a source without the anchor was accepted")
	}
}

// The real pinned source, when it is on this machine, takes the patch.
func TestPatchAppliesToThePinnedSource(t *testing.T) {
	src := filepath.Join(cacheRoot(), "webdriveragent-device", WDAVersion, "WebDriverAgent-"+WDAVersion)
	raw, err := os.ReadFile(filepath.Join(src, wdaStreamBind.file))
	if err != nil {
		t.Skip("the pinned source is not unpacked here")
	}
	if _, err := bindStream(string(raw)); err != nil {
		t.Error(err)
	}
}
