package device

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const infoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>CFBundleIdentifier</key><string>dev.mobium.mobiumapp</string></dict></plist>`

// plutil is macOS's; the phone is driven only from a Mac.
func needPlutil(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/usr/bin/plutil"); err != nil {
		t.Skip("needs plutil, which ships with macOS")
	}
}

func TestBundleIDOfAnApp(t *testing.T) {
	needPlutil(t)
	app := filepath.Join(t.TempDir(), "MobiumApp.app")
	os.MkdirAll(app, 0o755)
	os.WriteFile(filepath.Join(app, "Info.plist"), []byte(infoPlist), 0o644)
	id, err := BundleIDOf(context.Background(), app)
	if err != nil || id != "dev.mobium.mobiumapp" {
		t.Fatalf("BundleIDOf = %q, %v", id, err)
	}
}

func TestBundleIDOfAnIPA(t *testing.T) {
	needPlutil(t)
	ipa := filepath.Join(t.TempDir(), "MobiumApp.ipa")
	f, _ := os.Create(ipa)
	z := zip.NewWriter(f)
	w, _ := z.Create("Payload/MobiumApp.app/Info.plist")
	w.Write([]byte(infoPlist))
	z.Close()
	f.Close()
	id, err := BundleIDOf(context.Background(), ipa)
	if err != nil || id != "dev.mobium.mobiumapp" {
		t.Fatalf("BundleIDOf = %q, %v", id, err)
	}
}

func TestBundleIDOfRefusesWhatIsNotABundle(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app.apk")
	os.WriteFile(file, []byte("x"), 0o644)
	if _, err := BundleIDOf(context.Background(), file); err == nil || !strings.Contains(err.Error(), "neither") {
		t.Errorf("an .apk was read as a bundle: %v", err)
	}
	if _, err := BundleIDOf(context.Background(), filepath.Join(t.TempDir(), "missing.app")); err == nil {
		t.Error("a missing bundle was read")
	}
}

// devicectl's listing of MobiumApp's container straight after an uninstall
// and install on an iPhone 15 Plus, iOS 26.6.2: nine entries, and the only
// file is the launch snapshot iOS writes at install.
func TestAFreshContainerHoldsOnlyTheInstallSnapshot(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "devicectl-container-fresh.json"))
	if err != nil {
		t.Fatal(err)
	}
	files, entries, err := parseContainerFiles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if entries != 9 || len(files) != 1 || !installSnapshot(files[0]) {
		t.Errorf("entries %d, files %v", entries, files)
	}
	if installSnapshot("Library/Preferences/dev.mobium.mobiumapp.plist") {
		t.Error("the app's own preferences counted as iOS's install snapshot")
	}
}
