package device

import (
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
