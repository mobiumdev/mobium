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
// activity on top of it; a permission prompt is also another package on top
// of an app's task, and must not read as a web app. The WebAPK capture is the
// five lines of its task, taken on the Pixel 8 Pro, Android 17.
func TestWebApkHost(t *testing.T) {
	read := func(f string) string {
		raw, err := os.ReadFile("testdata/" + f)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	webapk := "org.chromium.webapk.a4110a3ad6380587f_v2"
	if host, ok := webApkHost(read("activities-webapk-api37.txt"), webapk); !ok || host != "com.android.chrome" {
		t.Errorf("a WebAPK in front: host %q, %v; want com.android.chrome", host, ok)
	}
	if _, ok := webApkHost(read("activities-webapk-api37.txt"), "com.example.other"); ok {
		t.Error("a WebAPK's task was claimed for another package")
	}
	if _, ok := webApkHost(read("activities-permission-prompt-api35.txt"), "dev.mobium.mobiumapp"); ok {
		t.Error("a permission prompt over an app's task was taken for a web app")
	}
}
