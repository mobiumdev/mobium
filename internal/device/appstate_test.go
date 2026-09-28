package device

import (
	"os"
	"testing"
)

// Captured on the Pixel 7 AVD, Android 15. In the first, MobiumApp's camera
// prompt is up: the activity in front is the permission controller's, in
// MobiumApp's task. In the second, the home screen is in front, with
// MobiumApp and Settings behind it.
func TestTaskInFront(t *testing.T) {
	cases := []struct {
		file, top, root string
	}{
		{"activities-permission-prompt-api35.txt", "com.google.android.permissioncontroller", "dev.mobium.mobiumapp"},
		{"activities-home-api35.txt", "com.google.android.apps.nexuslauncher", "com.google.android.apps.nexuslauncher"},
	}
	for _, c := range cases {
		raw, err := os.ReadFile("testdata/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		top, root := taskInFront(string(raw))
		if top != c.top || root != c.root {
			t.Errorf("%s: top=%q root=%q, want %q and %q", c.file, top, root, c.top, c.root)
		}
	}
	if top, root := taskInFront("nothing resumed"); top != "" || root != "" {
		t.Errorf("an empty dump gave %q, %q", top, root)
	}
}
