package device

import (
	"os"
	"strings"
	"testing"
)

func TestOrphanedRunnerIsOnlyOursAndOnlyOrphaned(t *testing.T) {
	root := "/Users/x/.mobium/webdriveragent-device"
	run := root + "/10.2.2/TEAM/UDID-1/build/Build/Products/WebDriverAgentRunner_iphoneos26.6-arm64.xctestrun"
	ps := "  101     1 /usr/bin/xcodebuild test-without-building -xctestrun " + run + " -destination id=UDID-1\n" +
		// A live daemon's own runner: its parent is that daemon.
		"  102  4242 /usr/bin/xcodebuild test-without-building -xctestrun " + run + " -destination id=UDID-1\n" +
		// Xcode's, or a person's: a build outside this cache.
		"  103     1 /usr/bin/xcodebuild test-without-building -xctestrun /tmp/mine.xctestrun -destination id=UDID-1\n" +
		// Ours, for another phone.
		"  104     1 /usr/bin/xcodebuild test-without-building -xctestrun " + run + " -destination id=UDID-2\n"
	if got := orphanedRunner(ps, root, "UDID-1"); got != 101 {
		t.Errorf("orphanedRunner = %d, want 101, the only orphan of ours for UDID-1", got)
	}
	if got := orphanedRunner(ps[strings.Index(ps, "  102"):], root, "UDID-1"); got != 0 {
		t.Errorf("adopted %d, which is a live daemon's, Xcode's, or another phone's", got)
	}
}

// A runner with no address to bind to would listen on every interface of the
// phone, which answered anyone on its Wi-Fi (CHALLENGES 153). It is refused
// before anything is launched.
func TestStartPhoneWDARefusesToStartUnbound(t *testing.T) {
	if _, err := StartPhoneWDA("unused.xctestrun", "udid", t.TempDir()+"/run.log", ""); err == nil {
		t.Fatal("a runner was started with no address to bind to")
	}
}

// A runner xcodebuild is about to install is announced, and one that was
// installed before is announced as removed — the silence that hid another
// tool deleting it (CHALLENGES 189). One already on the phone says nothing.
func TestPhoneWDAInstallNotice(t *testing.T) {
	t.Setenv("MOBIUM_HOME", t.TempDir())
	p := Phone{UDID: "UDID-1", Name: "Test iPhone"}
	ours := []InstalledApp{{ID: PhoneWDABundleID("TEAM"), Name: PhoneWDAName + "-Runner"}}
	other := []InstalledApp{{ID: "com.facebook.WebDriverAgentRunner.xctrunner", Name: "WebDriverAgentRunner-Runner"}}

	if got := PhoneWDAInstallNotice("TEAM", p, ours); got != "" {
		t.Errorf("runner on the phone, notice %q, want none", got)
	}
	if got := PhoneWDAInstallNotice("TEAM", p, other); !strings.HasPrefix(got, "installing WebDriverAgent") {
		t.Errorf("first install announced as %q", got)
	}
	if err := os.MkdirAll(phoneWDADir("TEAM", p.UDID), 0o755); err != nil {
		t.Fatal(err)
	}
	MarkPhoneWDAInstalled("TEAM", p.UDID)
	if got := PhoneWDAInstallNotice("TEAM", p, other); !strings.Contains(got, "no longer installed") {
		t.Errorf("a removed runner announced as %q", got)
	}
	if got := PhoneWDAInstallNotice("TEAM", Phone{UDID: "UDID-2"}, other); strings.Contains(got, "no longer") {
		t.Errorf("another phone's install read as a removal: %q", got)
	}
}

// Only the runner target is renamed: a PRODUCT_NAME given outright would
// rename WebDriverAgentLib too, and the runner would not link.
func TestPhoneWDANameRenamesOnlyTheRunner(t *testing.T) {
	joined := strings.Join(phoneWDANameSettings, " ")
	for _, want := range []string{"MOBIUM_PRODUCT_WebDriverAgentRunner=" + PhoneWDAName, ":default=$(TARGET_NAME)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("settings %q lack %q", joined, want)
		}
	}
}
