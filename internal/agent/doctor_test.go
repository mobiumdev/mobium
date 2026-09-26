package agent

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
)

func runDoctor(t *testing.T) DoctorView {
	t.Helper()
	h := NewHandlers()
	res, err := h.doctor(context.Background(), nil)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	view, ok := res.StructuredContent.(DoctorView)
	if !ok {
		t.Fatalf("structured content = %T", res.StructuredContent)
	}
	return view
}

func TestDoctorRunsWithNothingAvailable(t *testing.T) {
	// The whole point: it has to work when nothing else does. An empty PATH
	// is the harshest version of that, and it must still produce a report
	// rather than an error.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("MOBIUM_ADB_PATH", "")
	t.Setenv("MOBIUM_XCRUN_PATH", "")
	t.Setenv("ANDROID_SDK_ROOT", "")
	t.Setenv("ANDROID_HOME", "")

	view := runDoctor(t)
	if len(view.Checks) == 0 {
		t.Fatal("no checks ran")
	}
	if view.Problems == 0 {
		t.Error("found nothing wrong with a machine that has no adb")
	}
	for _, c := range view.Checks {
		if c.Status == statusProblem && c.Fix == "" {
			t.Errorf("%q is reported as a problem with no way to fix it", c.Name)
		}
	}
}

func TestDoctorNamesTheRealCauseOfTheSDKRootTrap(t *testing.T) {
	// The emulator dies with "Broken AVD system path" when the SDK root has
	// no platform-tools directory, while adb sits on PATH working perfectly.
	// That error names the wrong cause and cost hours; this is the check that
	// exists to shortcut it.
	root := t.TempDir()
	t.Setenv("ANDROID_SDK_ROOT", root)

	var found *DoctorCheck
	for _, c := range runDoctor(t).Checks {
		if c.Name == "android sdk root" {
			found = &c
			break
		}
	}
	if found == nil {
		t.Fatal("no sdk root check")
	}
	if found.Status != statusProblem {
		t.Fatalf("an SDK root with no platform-tools passed: %+v", found)
	}
	if !strings.Contains(found.Fix, "Broken AVD system path") {
		t.Errorf("the fix does not connect it to the error a user actually sees: %q", found.Fix)
	}
}

func TestDoctorTreatsAMissingToolchainAsANoteNotAFailure(t *testing.T) {
	// Most machines have one platform, not both. Missing Xcode on a machine
	// doing Android work is normal, and calling it a problem would train
	// people to ignore the report.
	for _, c := range runDoctor(t).Checks {
		if c.Name == "xcode" && c.Status == statusProblem {
			t.Error("a missing Xcode is reported as a problem rather than a note")
		}
	}
}

func TestDoctorChecksTheSocketLengthLimit(t *testing.T) {
	// The OS caps a Unix socket path at ~104 bytes, which an ordinary
	// MOBIUM_HOME can exceed, and the resulting error is baffling.
	if runtime.GOOS == "windows" {
		t.Skip("the daemon listens on a named pipe, which has no such cap")
	}
	t.Setenv("MOBIUM_HOME", "/tmp/"+strings.Repeat("d", 120))

	var found *DoctorCheck
	for _, c := range runDoctor(t).Checks {
		if c.Name == "daemon socket" {
			found = &c
			break
		}
	}
	if found == nil {
		t.Fatal("no socket check")
	}
	if found.Status != statusProblem {
		t.Errorf("an over-long socket path passed: %+v", found)
	}
}

func TestDoctorFlagsADeviceThatCannotBeDriven(t *testing.T) {
	// Found by running doctor with a phone attached whose debugging prompt
	// had not been accepted: it listed "3C19... (unauthorized)" as ok. Only
	// "device" can be driven, and reporting anything else as fine defeats the
	// point of asking.
	dir := t.TempDir()
	adb := dir + "/adb"
	script := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  *devices*) echo 'List of devices attached'; echo 'ABC123\tunauthorized' ;;\n" +
		"  *version*) echo 'Android Debug Bridge version 1.0.41' ;;\n" +
		"esac\n"
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MOBIUM_ADB_PATH", adb)

	var found *DoctorCheck
	for _, c := range runDoctor(t).Checks {
		if c.Name == "android devices" {
			found = &c
			break
		}
	}
	if found == nil {
		t.Fatal("no devices check")
	}
	if found.Status != statusProblem {
		t.Fatalf("an unauthorized device was reported as %q: %+v", found.Status, found)
	}
	if !strings.Contains(found.Fix, "Allow USB debugging") {
		t.Errorf("the fix does not say what to do on the phone: %q", found.Fix)
	}
}

// A driver on Windows is mobium-driver-x.exe with no execute bits, because
// Windows has none. Judging it by mode bits reported "none on PATH" for a
// driver `--backend x` would have found.
func TestDriverNameFollowsThePlatformsIdeaOfExecutable(t *testing.T) {
	const pathext = ".COM;.EXE;.BAT;.CMD"
	for _, c := range []struct {
		file   string
		mode   os.FileMode
		goos   string
		want   string
		driver bool
	}{
		{"mobium-driver-x", 0o755, "linux", "x", true},
		{"mobium-driver-x", 0o644, "darwin", "", false},
		{"mobium-driver-x.exe", 0o666, "windows", "x", true},
		{"mobium-driver-x.CMD", 0o666, "windows", "x", true},
		{"mobium-driver-x.py", 0o666, "windows", "", false},
		{"mobium-driver-x", 0o666, "windows", "", false},
		{"adb.exe", 0o666, "windows", "", false},
	} {
		got, driver := driverName(c.file, c.mode, c.goos, pathext)
		if driver != c.driver || got != c.want {
			t.Errorf("driverName(%q, %o, %s) = %q, %v; want %q, %v",
				c.file, c.mode, c.goos, got, driver, c.want, c.driver)
		}
	}
}
