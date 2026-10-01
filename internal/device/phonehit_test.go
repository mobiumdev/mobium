package device

import (
	"encoding/json"
	"strings"
	"testing"
)

// An app's process is the one whose executable is inside its bundle, as
// devicectl lists both: an app list gives the bundle's path, a process list
// each executable's.
func TestAPhoneAppsProcessIsTheOneInsideItsBundle(t *testing.T) {
	procs := json.RawMessage(`{"runningProcesses":[
		{"processIdentifier":1,"executable":"file:///usr/libexec/launchd"},
		{"processIdentifier":13920,"executable":"file:///private/var/containers/Bundle/Application/AB/MobiumApp.app/MobiumApp"},
		{"processIdentifier":13921,"executable":"file:///private/var/containers/Bundle/Application/CD/Other.app/Other"}]}`)
	pid, err := pidUnder(procs, "file:///private/var/containers/Bundle/Application/AB/MobiumApp.app/", "dev.mobium.mobiumapp")
	if err != nil || pid != 13920 {
		t.Fatalf("pid = %d, %v", pid, err)
	}
	if _, err := pidUnder(procs, "file:///private/var/containers/Bundle/Application/EF/Gone.app/", "com.example.gone"); err == nil ||
		!strings.Contains(err.Error(), "not running") {
		t.Errorf("an app that is not running gave %v", err)
	}
}

// The phone's probe answers in the simulator's words: measured on an iPhone
// 15 Plus, the overlay hidden from accessibility read as this line.
func TestAPhoneHitAnswerReadsAsTheSimulatorsDoes(t *testing.T) {
	h, err := parseHitLine("covered\t1\tRCTParagraphComponentView\thidden overlay\t170.7\t732.3\t88.3\t15.7")
	if err != nil || h.Verdict != "covered" || !h.Hidden || h.Label != "hidden overlay" || h.Frame[2] != 88.3 {
		t.Fatalf("read %+v, %v", h, err)
	}
	if h, _ := parseHitLine("unknown\tthe app did not stop for the debugger"); h.Reason == "" {
		t.Error("an unknown answer lost its reason")
	}
}
