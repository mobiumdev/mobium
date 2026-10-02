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

// A WebAPK's task is its own and its pages are the browser's web-app
// activity on top of it; a Trusted Web Activity's is a custom tab over the TWA
// library's launcher; a permission prompt is also another package on top of
// an app's task, and must not read as either. Each capture is the five lines
// of its task, taken on the Pixel 8 Pro, Android 17.
func TestHostedInFront(t *testing.T) {
	read := func(f string) string {
		raw, err := os.ReadFile("testdata/" + f)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	cases := []struct {
		file string
		want Hosted
		ok   bool
	}{
		{"activities-webapk-api37.txt", Hosted{"org.chromium.webapk.a4110a3ad6380587f_v2", "com.android.chrome", "installed web app"}, true},
		{"activities-twa-api37.txt", Hosted{"com.oyo.consumerlite", "com.android.chrome", "Trusted Web Activity"}, true},
		{"activities-permission-prompt-api35.txt", Hosted{}, false},
		{"activities-home-api35.txt", Hosted{}, false},
	}
	for _, c := range cases {
		got, ok := hostedInFront(read(c.file))
		if ok != c.ok || got != c.want {
			t.Errorf("%s: %+v, %v; want %+v, %v", c.file, got, ok, c.want, c.ok)
		}
	}
}
