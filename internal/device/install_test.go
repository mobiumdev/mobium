package device

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mobiumdev/mobium/internal/fakecmd"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// installADB is an adb whose install takes two seconds and then succeeds, and
// whose `dumpsys activity activities` puts top in front.
func installADB(t *testing.T, top string) *ADB {
	t.Helper()
	dir := t.TempDir()
	dump := filepath.Join(dir, "activities.txt")
	if err := os.WriteFile(dump, []byte("  topResumedActivity=ActivityRecord{156587441 u0 "+top+" t1579}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := fakecmd.Script(t, dir, "adb", `case "$*" in
  *dumpsys*) cat `+fakecmd.Path(dump)+` ;;
  *install*) sleep 2; echo "Performing Streamed Install"; echo Success ;;
esac`)
	old := installWatchEvery
	installWatchEvery = 50 * time.Millisecond
	t.Cleanup(func() { installWatchEvery = old })
	return &ADB{Path: bin, Serial: "fake"}
}

// Measured on the Pixel 8 Pro: an APK Play Protect did not know put this
// activity in front and adb install printed nothing for over a minute.
func TestInstallNamesPlayProtectsDialog(t *testing.T) {
	a := installADB(t, "com.android.vending/com.google.android.finsky.protectdialogs.activity.PlayProtectDialogsActivity")
	err := a.InstallApp(context.Background(), "/tmp/app-release.apk")
	var me *mobiumerr.Error
	if !errors.As(err, &me) || me.Code != mobiumerr.DeviceNotReady {
		t.Fatalf("want device_not_ready, got %v", err)
	}
	if !strings.Contains(err.Error(), "Google Play Protect") || !strings.Contains(err.Error(), "app-release.apk") {
		t.Errorf("the error does not name the dialog and the app: %v", err)
	}
	if !strings.Contains(me.Remedy, "app_list_apps") || !strings.Contains(me.Remedy, "ask again") {
		t.Errorf("the remedy does not say how to finish or that installing again asks again: %q", me.Remedy)
	}
}

// The positive control: with the launcher in front the same install finishes
// and is reported as done, so the test above cannot pass by refusing every
// install.
func TestInstallWithoutTheDialogSucceeds(t *testing.T) {
	a := installADB(t, "com.google.android.apps.nexuslauncher/.NexusLauncherActivity")
	if err := a.InstallApp(context.Background(), "/tmp/app-release.apk"); err != nil {
		t.Fatalf("install with nothing in the way: %v", err)
	}
}

// The Play Store itself in front is not the dialog.
func TestInstallIgnoresThePlayStoresOtherScreens(t *testing.T) {
	a := installADB(t, "com.android.vending/com.google.android.finsky.activities.MainActivity")
	if err := a.InstallApp(context.Background(), "/tmp/app-release.apk"); err != nil {
		t.Fatalf("install with the Play Store open: %v", err)
	}
}
