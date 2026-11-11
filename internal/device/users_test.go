package device

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseCurrentUser(t *testing.T) {
	for in, want := range map[string]int{"10\n": 10, "0": 0, "": 0, "Unknown command: get-current-user": 0, "-1": 0} {
		if got := parseCurrentUser(in); got != want {
			t.Errorf("parseCurrentUser(%q) = %d, want %d", in, got, want)
		}
	}
}

// The shape of `dumpsys package` on the Android Automotive 15 emulator, with
// CAMERA granted to user 0 and not to user 10, the driver.
const twoUserDump = `Packages:
  Package [dev.mobium.mobiumapp] (5c1e0a2):
    User 0: ceDataInode=557714 installed=true hidden=false stopped=true
      runtime permissions:
        android.permission.CAMERA: granted=true, flags=[ USER_SENSITIVE_WHEN_GRANTED|USER_SENSITIVE_WHEN_DENIED]
    User 10: ceDataInode=492273 installed=true hidden=false stopped=true
      runtime permissions:
        android.permission.CAMERA: granted=false, flags=[ USER_SENSITIVE_WHEN_GRANTED|USER_SENSITIVE_WHEN_DENIED]
Dexopt state:
    User 0:
    User 10:
`

func TestUserSection(t *testing.T) {
	all, ok := userSection(twoUserDump, 0)
	if !ok || all != twoUserDump {
		t.Error("user 0 is not the whole dump")
	}
	ten, ok := userSection(twoUserDump, 10)
	if !ok || !strings.HasPrefix(strings.TrimSpace(ten), "User 10: ceDataInode") {
		t.Fatalf("user 10's section starts %q", firstLine(ten))
	}
	perms := parseRuntimePermissions(ten)
	if len(perms) != 1 || perms[0].granted {
		t.Errorf("user 10's permissions = %+v, want CAMERA not granted", perms)
	}
	if _, ok := userSection(twoUserDump, 11); ok {
		t.Error("a user with no section was found")
	}
	// User 1 must not match User 10.
	if _, ok := userSection(twoUserDump, 1); ok {
		t.Error("user 1 matched user 10's header")
	}
}

// The main display's focus decides. On the Android Automotive 15 emulator the
// first section is the cluster's display 3, never focused; display 0 comes
// last. A Pixel 7 AVD has the one section.
func TestFocusedIn(t *testing.T) {
	car := `WINDOW MANAGER DISPLAY CONTENTS (dumpsys window displays)
  Display: mDisplayId=3
  mCurrentFocus=null
  Display: mDisplayId=2 (organized)
  mCurrentFocus=Window{dffec1b u0 com.android.car.cluster.osdouble/com.android.car.cluster.osdouble.ClusterOsDoubleActivity}
  Display: mDisplayId=0 (organized)
  mCurrentFocus=%s
`
	if !focusedIn(fmt.Sprintf(car, "Window{12f7970 u10 com.android.car.carlauncher/.CarLauncher}")) {
		t.Error("a car whose main display has focus read as not focused")
	}
	if focusedIn(fmt.Sprintf(car, "null")) {
		t.Error("a car whose main display has no focus read as focused, from the cluster's")
	}
	phone := "  Display: mDisplayId=0 (organized)\n  mCurrentFocus=Window{904043c u0 dev.mobium.mobiumapp/.MainActivity}\n"
	if !focusedIn(phone) || focusedIn(strings.Replace(phone, "Window{904043c u0 dev.mobium.mobiumapp/.MainActivity}", "null", 1)) {
		t.Error("a phone's one display misread")
	}
	if !focusedIn("  mCurrentFocus=Window{1 u0 a/b}\n") || focusedIn("  mCurrentFocus=null\n") {
		t.Error("a dump with no display sections misread")
	}
	if focusedIn("  Display: mDisplayId=10\n  mCurrentFocus=Window{1 u0 a/b}\n") {
		t.Error("display 10 read as display 0")
	}
}
