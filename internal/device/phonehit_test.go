package device

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
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

// An attach that never stopped is blamed on a missing shared cache only
// when Xcode has none for the phone's iOS on this Mac, matched by version in
// the folder name Xcode gives it, and the refusal names the Devices window.
// CHALLENGES 343.
func TestHasDeviceSupport(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "Library", "Developer", "Xcode", "iOS DeviceSupport", "iPhone15,5 26.6.2 (23G90)")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasDeviceSupport(home, "26.6.2") {
		t.Error("26.6.2's folder is not found")
	}
	for _, v := range []string{"27.0.1", "26.6", "6.6.2"} {
		if hasDeviceSupport(home, v) {
			t.Errorf("%s is taken as present from 26.6.2's folder", v)
		}
	}
	if hasDeviceSupport(t.TempDir(), "27.0.1") {
		t.Error("a Mac with no DeviceSupport folder has 27.0.1's")
	}
	err := noDeviceSupport(Phone{Model: "iPhone 15 Plus", OSVersion: "27.0.1"})
	if mobiumerr.CodeOf(err) != mobiumerr.DeviceNotReady || !strings.Contains(err.Error(), "iOS 27.0.1") ||
		!strings.Contains(mobiumerr.Text(err), "Devices and Simulators") {
		t.Errorf("the refusal: %s", mobiumerr.Text(err))
	}
}

// The phone's expression has no CGPointMake, CGRectMake or CG struct
// variable: Xcode 27.1's lldb on iOS 27 turned CGPointMake(259, 365) into
// (259, 0), so every hit test asked about the top edge of the screen, and
// refused a declared CGRect as having no matching constructor. CHALLENGES 345.
func TestThePhoneProbeMakesNoPointWithCGPointMake(t *testing.T) {
	_, body, ok := strings.Cut(string(phoneHitScript), "EXPR = r'''")
	expr, _, closed := strings.Cut(body, "'''")
	if !ok || !closed {
		t.Fatal("the probe's expression is not where it was")
	}
	for _, bad := range []string{"CGPointMake(", "CGRectMake(", "CGSizeMake(", "CGRect ", "CGPoint ", "CGSize "} {
		if strings.Contains(expr, bad) {
			t.Errorf("the expression has %q", bad)
		}
	}
	if !strings.Contains(expr, `CGPointFromString(@"{%(x)f, %(y)f}")`) {
		t.Error("the expression no longer makes its point with CGPointFromString")
	}
}
