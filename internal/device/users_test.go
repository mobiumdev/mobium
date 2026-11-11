package device

import (
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
