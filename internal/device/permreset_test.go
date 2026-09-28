package device

import "testing"

// Lines as the Pixel 7 AVD printed them: MobiumApp after a camera denial,
// a system app's permission fixed by the system, and MobiumApp's coarse
// location after a person chose approximate — whose flags end in a number
// Android has no name for, which once made the whole list unreadable — and
// two lines from the Pixel 8 Pro on Android 17.
func TestParseRuntimePermissions(t *testing.T) {
	dump := `    runtime permissions:
        android.permission.POST_NOTIFICATIONS: granted=false, flags=[ USER_SENSITIVE_WHEN_GRANTED|USER_SENSITIVE_WHEN_DENIED]
        android.permission.CAMERA: granted=false, flags=[ USER_SET|USER_FIXED|USER_SENSITIVE_WHEN_GRANTED|USER_SENSITIVE_WHEN_DENIED]
        android.permission.ACCESS_FINE_LOCATION: granted=true, flags=[ SYSTEM_FIXED|GRANTED_BY_DEFAULT]
        android.permission.READ_CONTACTS: granted=true
        android.permission.ACCESS_COARSE_LOCATION: granted=false, flags=[ USER_SET|USER_SENSITIVE_WHEN_GRANTED|USER_SENSITIVE_WHEN_DENIED|524288]
        android.permission.ACCESS_FINE_LOCATION: granted=true, flags=[ USER_SET|USER_SENSITIVE_WHEN_GRANTED|USER_SENSITIVE_WHEN_DENIED|SELECTED_LOCATION_ACCURACY]
        android.permission.ACCESS_LOCAL_NETWORK: granted=true, flags=[ REVOKE_WHEN_REQUESTED|USER_SENSITIVE_WHEN_GRANTED|USER_SENSITIVE_WHEN_DENIED]
    disabledComponents:
        android.permission.NOT_A_PERMISSION: granted=true
`
	got := parseRuntimePermissions(dump)
	if len(got) != 7 {
		t.Fatalf("parsed %d permissions, want 7: %+v", len(got), got)
	}
	if got[0].decided() || got[0].fixed() || got[0].granted {
		t.Errorf("notifications = %+v, want undecided", got[0])
	}
	if !got[1].decided() || !got[1].flags["USER_FIXED"] {
		t.Errorf("camera = %+v, want decided and fixed by the person", got[1])
	}
	if !got[2].fixed() || !got[2].granted {
		t.Errorf("fine location = %+v, want fixed by the system", got[2])
	}
	if !got[3].granted || got[3].decided() || len(got[3].flags) != 0 {
		t.Errorf("contacts = %+v, want granted with no flags", got[3])
	}
	if !got[4].decided() || !got[4].accuracyChosen() {
		t.Errorf("coarse location = %+v, want the person's answer and the accuracy choice", got[4])
	}
	// Android 17, on the Pixel 8 Pro: the flag by name, and a permission the
	// platform granted by itself.
	if !got[5].accuracyChosen() || got[5].platformGranted() {
		t.Errorf("fine location = %+v, want the accuracy choice by name", got[5])
	}
	if !got[6].platformGranted() || got[6].decided() {
		t.Errorf("local network = %+v, want granted by the platform", got[6])
	}
}
