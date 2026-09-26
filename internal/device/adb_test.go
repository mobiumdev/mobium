package device

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseDevices(t *testing.T) {
	out := `* daemon not running; starting now at tcp:5037
* daemon started successfully
List of devices attached
emulator-5554          device product:sdk_gphone64_arm64 model:sdk_gphone64_arm64 device:emu64a transport_id:1
emulator-5556          offline
R5CT10XXXXX            unauthorized
192.168.1.5:5555       device product:x model:Pixel_7 device:panther transport_id:3

`
	got := parseDevices(out)
	if len(got) != 4 {
		t.Fatalf("parsed %d devices, want 4: %+v", len(got), got)
	}

	if got[0].Serial != "emulator-5554" || got[0].State != "device" {
		t.Errorf("device 0 = %+v", got[0])
	}
	if got[0].Model != "sdk_gphone64_arm64" {
		t.Errorf("model = %q", got[0].Model)
	}
	if !got[0].Emulator || !got[0].Ready() {
		t.Error("emulator-5554 should be a ready emulator")
	}
	if got[1].Ready() {
		t.Error("an offline device must not be ready")
	}
	if got[2].Ready() {
		t.Error("an unauthorized device must not be ready")
	}
	if got[3].Emulator {
		t.Error("a network device is not an emulator")
	}
	if got[3].Model != "Pixel_7" {
		t.Errorf("network device model = %q", got[3].Model)
	}
}

func TestParseDevicesEmpty(t *testing.T) {
	if got := parseDevices("List of devices attached\n\n"); len(got) != 0 {
		t.Errorf("parsed %d devices from an empty list", len(got))
	}
	if got := parseDevices(""); len(got) != 0 {
		t.Errorf("parsed %d devices from empty output", len(got))
	}
}

func TestFindADBHonoursOverride(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "adb")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MOBIUM_ADB_PATH", fake)

	got, err := FindADB()
	if err != nil {
		t.Fatalf("FindADB: %v", err)
	}
	if got != fake {
		t.Errorf("FindADB = %q, want the override %q", got, fake)
	}
}

func TestFindADBRejectsBadOverride(t *testing.T) {
	t.Setenv("MOBIUM_ADB_PATH", filepath.Join(t.TempDir(), "nope"))
	_, err := FindADB()
	if err == nil {
		t.Fatal("expected an error for a nonexistent override")
	}
	// The message must name the variable, or the user has no idea what to fix.
	if !strings.Contains(err.Error(), "MOBIUM_ADB_PATH") {
		t.Errorf("error %q does not mention the override variable", err)
	}
}

// cmd.exe keeps the quotes in `set ANDROID_HOME="C:\Program Files\Android"`,
// and a quote cannot be in a Windows file name, so it is dropped there — and
// only there, since a quote is a legal path character on Unix.
func TestEnvPathDropsCmdQuotesOnWindowsOnly(t *testing.T) {
	quoted := `"C:\Program Files\Android\Sdk"`
	if got := cleanEnvPath(quoted, "windows"); got != `C:\Program Files\Android\Sdk` {
		t.Errorf("windows: %q", got)
	}
	if got := cleanEnvPath(` C:\sdk `, "windows"); got != `C:\sdk` {
		t.Errorf("windows, padded: %q", got)
	}
	if got := cleanEnvPath(quoted, "darwin"); got != quoted {
		t.Errorf("darwin changed a legal path: %q", got)
	}
	if got := cleanEnvPath(`"`, "windows"); got != `"` {
		t.Errorf("a lone quote: %q", got)
	}
}

func TestErrNoADBIsActionable(t *testing.T) {
	// This string is the entire recovery path for a first-time user.
	for _, want := range []string{"platform-tools", "ANDROID_HOME", "PATH"} {
		if !strings.Contains(ErrNoADB.Error(), want) {
			t.Errorf("ErrNoADB does not mention %q: %v", want, ErrNoADB)
		}
	}
}

func TestADBArgsPinSerial(t *testing.T) {
	a := &ADB{Path: "adb", Serial: "emulator-5554"}
	got := a.args("shell", "input", "tap", "1", "2")
	want := []string{"-s", "emulator-5554", "shell", "input", "tap", "1", "2"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v, want %v", got, want)
	}

	unpinned := &ADB{Path: "adb"}
	if got := unpinned.args("devices"); len(got) != 1 || got[0] != "devices" {
		t.Errorf("unpinned args = %v", got)
	}
}

// TestRotationParsesDegreesNotQuarters pins the confusion that broke this
// first time out.
//
// `mCurrentRotation=ROTATION_90` is **degrees**, while `user_rotation` and
// `cmd window user-rotation lock <n>` are **quarter turns**. Reading the
// degrees as quarters accepts 0 and rejects everything else, so a portrait
// screen looks perfectly fine and the very first rotation fails with "the
// display reported rotation 90" — which reads like the device is wrong.
func TestRotationParsesDegreesNotQuarters(t *testing.T) {
	for degrees, want := range map[string]int{
		"ROTATION_0": 0, "ROTATION_90": 1, "ROTATION_180": 2, "ROTATION_270": 3,
	} {
		m := rotationRe.FindStringSubmatch("  mCurrentRotation=" + degrees + " mFoo=1")
		if m == nil {
			t.Fatalf("no match for %q", degrees)
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		if n%90 != 0 || n/90 != want {
			t.Errorf("%s parsed to %d degrees, which is quarter %d, want %d", degrees, n, n/90, want)
		}
	}
}

// TestRotationRegexIgnoresTheOtherSpellings: `dumpsys window` prints three
// different rotation fields in one output, including `mRotation=undefined` on
// a display that is not the real one. Matching the wrong one gives a number
// that is sometimes right, which is the worst kind.
func TestRotationRegexIgnoresTheOtherSpellings(t *testing.T) {
	noise := `mDisplayRotation=ROTATION_0 mRotation=ROTATION_0
      mRotation=undefined
    mRotation=1
    mCurrentRotation=ROTATION_270`
	m := rotationRe.FindStringSubmatch(noise)
	if m == nil || m[1] != "270" {
		t.Errorf("matched %v, want the mCurrentRotation value 270", m)
	}
}

func TestUserRotationParsesBothModes(t *testing.T) {
	for in, wantLocked := range map[string]bool{
		"free":    false,
		"lock 0":  true,
		"lock 3":  true,
		" lock 1": true,
	} {
		m := userRotationRe.FindStringSubmatch(in)
		if m == nil {
			t.Fatalf("no match for %q", in)
		}
		if got := m[1] == "lock"; got != wantLocked {
			t.Errorf("%q parsed locked=%v, want %v", in, got, wantLocked)
		}
	}
}

func TestAppLocalesParsing(t *testing.T) {
	for in, want := range map[string]string{
		"Locales for org.wikipedia for user 0 are [ja-JP]": "ja-JP",
		"Locales for org.wikipedia for user 0 are []":      "",
		"Locales for x for user 0 are [en-GB,ja-JP]":       "en-GB,ja-JP",
		"Locales for x for user 0 are [ en-GB , ja-JP ]":   "en-GB,ja-JP",
	} {
		m := appLocalesRe.FindStringSubmatch(in)
		if m == nil {
			t.Fatalf("no match for %q", in)
		}
		var tags []string
		for _, tag := range strings.Split(m[1], ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
		if got := strings.Join(tags, ","); got != want {
			t.Errorf("%q parsed to %q, want %q", in, got, want)
		}
	}
}

// TestNotificationParsing works on a real `dumpsys notification --noredact`
// capture, trimmed to four records that between them cover every shape:
// an app's own notification, one posted through `cmd notification post`, a
// system one, and the ranker's synthetic grouping record, which has no content
// and is bookkeeping rather than anything a user sees.
func TestNotificationParsing(t *testing.T) {
	raw, err := os.ReadFile("testdata/notification-dump.txt")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)

	locs := notificationKeyRe.FindAllStringSubmatchIndex(text, -1)
	if len(locs) != 4 {
		t.Fatalf("found %d records, want 4 — the key regex is matching the wrong thing", len(locs))
	}

	var got []Notification
	for i, loc := range locs {
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		record := text[loc[1]:end]
		n := Notification{Key: text[loc[2]:loc[3]]}
		if parts := strings.Split(n.Key, "|"); len(parts) >= 4 {
			n.Package, n.Tag = parts[1], parts[3]
		}
		if m := notificationTitleRe.FindStringSubmatch(record); m != nil {
			n.Title = m[1]
		}
		if m := notificationTextRe.FindStringSubmatch(record); m != nil {
			n.Text = m[1]
		}
		if n.Title == "" && n.Text == "" {
			continue
		}
		got = append(got, n)
	}

	if len(got) != 3 {
		t.Fatalf("kept %d notifications, want 3 — the ranker's empty record should be dropped", len(got))
	}

	// An app's own notification, which is what an assertion would target.
	if got[0].Package != "com.google.android.apps.messaging" {
		t.Errorf("package = %q", got[0].Package)
	}
	if got[0].Title != "555-1234" || got[0].Text != "mobium check message" {
		t.Errorf("message notification = %+v", got[0])
	}
	// One posted through the shell, identified by its tag.
	if got[1].Tag != "tag7" || got[1].Title != "MobiumTitle" || got[1].Text != "MobiumBodyText" {
		t.Errorf("posted notification = %+v", got[1])
	}
	// A system one whose body contains a comma and runs long.
	if !strings.HasPrefix(got[2].Text, "For added security") {
		t.Errorf("system notification text = %q", got[2].Text)
	}
}

// TestNotificationKeyRegexIgnoresGroupKey: every record carries a `groupKey=`
// as well, and matching it would double the record count and split each one in
// half, giving notifications with a title and no text.
func TestNotificationKeyRegexIgnoresGroupKey(t *testing.T) {
	sample := "      key=0|pkg|1|tag|1000\n      seen=false\n      groupKey=0|pkg|1|tag|1000\n"
	if n := len(notificationKeyRe.FindAllString(sample, -1)); n != 1 {
		t.Errorf("matched %d keys in a record that has one", n)
	}
}

// TestShellQuote covers the trap that truncated a notification body to its
// first word. `adb shell` runs its argument through a shell on the device, so
// anything with a space, an apostrophe or a dollar sign has to survive that.
// Each of these was verified end to end against a device.
func TestShellQuote(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain", `'plain'`},
		{"two words", `'two words'`},
		{"it's here", `'it'\''s here'`},
		{`say "hi"`, `'say "hi"'`},
		{"a$b", `'a$b'`},
		{"100% & more", `'100% & more'`},
		{"", `''`},
	} {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// Captured from a Pixel 7 AVD on Android 15. The two states below are the
// whole point of this test: after `remove-test-provider` the provider label
// loses its `[mock]` and the *last known location does not change at all* —
// still there, still tagged mock, from a provider that no longer exists.
// Reporting only the fix would call that device "injected" when nothing is
// injecting any more. See CHALLENGES 56.
const dumpsysMocking = `
  gps provider [mock]:
      last location=Location[gps 35.676200,139.650300 hAcc=100.0 et=+23m mock]
  network provider:
      last location=null
  fused provider:
      last location=Location[fused 35.676200,139.650300 hAcc=100.0 et=+23m mock]
`

const dumpsysAfterClear = `
  gps provider:
      last location=Location[gps 35.676200,139.650300 hAcc=100.0 et=+23m mock]
  network provider:
      last location=null
`

const dumpsysReal = `
  gps provider:
      last location=Location[gps 51.507398,-0.127800 hAcc=5.0 et=+21s alt=0.0]
`

func TestParseFixSeparatesInjectedFromInjecting(t *testing.T) {
	got, err := parseFix(dumpsysMocking)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("no fix parsed while a test provider was installed")
	}
	if got.Lat != 35.6762 || got.Lon != 139.6503 {
		t.Errorf("parsed %v,%v", got.Lat, got.Lon)
	}
	if !got.Mock || !got.Mocking {
		t.Errorf("mock=%v mocking=%v, want both true", got.Mock, got.Mocking)
	}

	// The case that makes the two fields worth having.
	after, err := parseFix(dumpsysAfterClear)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Mock {
		t.Error("the cached fix is still an injected one and should say so")
	}
	if after.Mocking {
		t.Error("no test provider is installed after a clear, so Mocking must be false")
	}
	if after.Lat != got.Lat || after.Lon != got.Lon {
		t.Errorf("clearing changed the position to %v,%v — it does not, and a test "+
			"asserting otherwise would be asserting a fiction", after.Lat, after.Lon)
	}
}

func TestParseFixReadsARealPosition(t *testing.T) {
	got, err := parseFix(dumpsysReal)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("no fix parsed")
	}
	if got.Mock || got.Mocking {
		t.Errorf("a real fix read as mock=%v mocking=%v", got.Mock, got.Mocking)
	}
	// Negative longitude: the sign has to survive, and the CLI had a separate
	// bug where it never reached this far.
	if got.Lon != -0.1278 {
		t.Errorf("longitude = %v, want -0.1278", got.Lon)
	}
}

func TestParseFixOnADeviceWithNoPosition(t *testing.T) {
	got, err := parseFix("  gps provider:\n      last location=null\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("parsed %+v from a device holding no position", got)
	}
}
