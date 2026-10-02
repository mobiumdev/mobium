package device

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The shape of `devicectl list devices --json-output -`'s result, with the
// fields read here, as Xcode 26.6 reported a real iPhone 15 Plus on iOS
// 26.6.2 over USB. The name, identifiers and address are made up; the rest is
// verbatim. The second entry is a paired Mac, which devicectl lists too.
const phonesFixture = `{"devices": [
 {
  "identifier": "11111111-2222-3333-4444-555555555555",
  "connectionProperties": {
   "pairingState": "paired", "transportType": "wired",
   "tunnelIPAddress": "fd00:1:2::1", "tunnelState": "connected"
  },
  "deviceProperties": {"name": "Test iPhone", "osVersionNumber": "26.6.2", "developerModeStatus": "enabled"},
  "hardwareProperties": {
   "udid": "00008120-0000000000000001", "platform": "iOS", "reality": "physical",
   "marketingName": "iPhone 15 Plus", "productType": "iPhone15,5", "deviceType": "iPhone"
  }
 },
 {
  "identifier": "66666666-7777-8888-9999-000000000000",
  "connectionProperties": {"pairingState": "paired", "transportType": "localNetwork"},
  "deviceProperties": {"name": "A Mac", "osVersionNumber": "26.0"},
  "hardwareProperties": {"udid": "mac-udid", "platform": "macOS", "reality": "physical"}
 }
]}`

func TestParsePhones(t *testing.T) {
	phones, err := parsePhones(json.RawMessage(phonesFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(phones) != 1 {
		t.Fatalf("got %d phones, want only the iPhone: %+v", len(phones), phones)
	}
	p := phones[0]
	want := Phone{
		UDID: "00008120-0000000000000001", Identifier: "11111111-2222-3333-4444-555555555555",
		Name: "Test iPhone", Model: "iPhone 15 Plus", OSVersion: "26.6.2",
		Transport: "wired", TunnelIP: "fd00:1:2::1", Paired: true, DeveloperMode: true,
	}
	if p != want {
		t.Errorf("parsed\n %+v\nwant\n %+v", p, want)
	}
	if !p.Connected() || p.Usable() != nil {
		t.Errorf("a paired, wired phone with Developer Mode on is not usable: %v", p.Usable())
	}

	// Serials, CoreDevice ids and names all select it; nothing else does.
	for _, ref := range []string{p.UDID, strings.ToLower(p.Identifier), "test iphone"} {
		if !p.Matches(ref) {
			t.Errorf("%q does not select the phone", ref)
		}
	}
	if p.Matches("iPhone 15 Plus") {
		t.Error("the model name selected one specific phone")
	}
}

// Each reason a connected phone cannot be driven is named, and names a fix,
// because each otherwise surfaces later as an xcodebuild error about
// something else.
func TestPhoneUsable(t *testing.T) {
	ok := Phone{Name: "P", Paired: true, Transport: "wired", DeveloperMode: true}
	for name, tc := range map[string]struct {
		mutate func(*Phone)
		want   string
	}{
		"unpaired":       {func(p *Phone) { p.Paired = false }, "Trust"},
		"unreachable":    {func(p *Phone) { p.Transport = "" }, "cable"},
		"developer mode": {func(p *Phone) { p.DeveloperMode = false }, "Developer Mode"},
	} {
		p := ok
		tc.mutate(&p)
		err := p.Usable()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error mentioning %q", name, err, tc.want)
		}
	}
	if err := ok.Usable(); err != nil {
		t.Errorf("positive control refused: %v", err)
	}
}

// App kinds as `devicectl device info apps --include-all-apps` reported them
// on the phone: Apple's are defaultApp, App Store and developer apps are not.
func TestParsePhoneApps(t *testing.T) {
	raw := json.RawMessage(`{"apps": [
	 {"bundleIdentifier": "com.apple.Preferences", "name": "Settings", "version": "1.0", "defaultApp": true, "hidden": false},
	 {"bundleIdentifier": "com.example.store", "name": "Store App", "version": "2.1", "defaultApp": false, "hidden": false},
	 {"bundleIdentifier": "dev.mobium.wda.TEAM.xctrunner", "name": "WebDriverAgentRunner-Runner", "version": "1.0", "defaultApp": false, "hidden": false},
	 {"bundleIdentifier": "com.apple.hidden", "name": "Hidden", "defaultApp": true, "hidden": true}
	]}`)

	user, err := parsePhoneApps(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, a := range user {
		ids = append(ids, a.ID)
	}
	if got := strings.Join(ids, ","); got != "com.example.store,dev.mobium.wda.TEAM.xctrunner" {
		t.Errorf("user apps = %s", got)
	}

	all, err := parsePhoneApps(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].ID != "com.apple.Preferences" || !all[0].System {
		t.Errorf("with system apps = %+v", all)
	}
}

// makeCert returns a PEM certificate for a team, and its SHA-1 as
// `security` prints it.
func makeCert(t *testing.T, team string, notAfter time.Time) (pemText, sha1 string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:         "Apple Development: Someone (ABCDE12345)",
			OrganizationalUnit: []string{team},
		},
		NotBefore: notAfter.Add(-365 * 24 * time.Hour),
		NotAfter:  notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		fmt.Sprintf("%X", sha1Sum(der))
}

// The keychain's output puts a SHA-256 line before the SHA-1 line. The first
// version of teamsFrom handed that to pem.Decode, which skips any text before
// a block — the SHA-1 line with it — so it found no team on a Mac that had one.
func TestTeamsFrom(t *testing.T) {
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	valid, validHash := makeCert(t, "TEAMAAAAAA", now.AddDate(1, 0, 0))
	expired, expiredHash := makeCert(t, "TEAMEXPIRD", now.AddDate(0, 0, -1))
	nokey, _ := makeCert(t, "TEAMNOKEYS", now.AddDate(1, 0, 0))

	certs := "SHA-256 hash: 00\nSHA-1 hash: " + validHash + "\n" + valid +
		"SHA-256 hash: 11\nSHA-1 hash: " + expiredHash + "\n" + expired +
		// Its hash is the start of the valid one's: a substring of an
		// identity's hash, which is not that identity. As "FFFF" it matched
		// a random hash that contained it, now and then.
		"SHA-256 hash: 22\nSHA-1 hash: " + validHash[:8] + "\n" + nokey
	identities := fmt.Sprintf("  1) %s \"Apple Development: Someone (ABCDE12345)\"\n"+
		"  2) %s \"Apple Development: Someone (ABCDE12345)\"\n", validHash, expiredHash)

	got := teamsFrom(identities, []byte(certs), now)
	if strings.Join(got, ",") != "TEAMAAAAAA" {
		t.Errorf("teams = %v, want only the unexpired one with a private key", got)
	}
}

func TestPhoneWDABundleID(t *testing.T) {
	// The runner's id carries the team — a free team cannot register an id
	// another team owns, and com.facebook.* is Meta's.
	if got := PhoneWDABundleID("ABCDE12345"); got != "dev.mobium.wda.ABCDE12345.xctrunner" {
		t.Errorf("bundle id = %s", got)
	}
}

func writeTarGz(t *testing.T, entries []*tar.Header, bodies []string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for i, h := range entries {
		h.Size = int64(len(bodies[i]))
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(bodies[i])); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	path := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUntarGz(t *testing.T) {
	// Positive control: a GitHub-shaped archive, pax header first.
	good := writeTarGz(t, []*tar.Header{
		{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader},
		{Name: "WDA/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "WDA/run.sh", Typeflag: tar.TypeReg, Mode: 0o755},
	}, []string{"", "", "#!/bin/sh\n"})
	dest := t.TempDir()
	if err := untarGz(good, dest); err != nil {
		t.Fatalf("a well-formed archive was refused: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dest, "WDA", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no executable bit to keep, and WebDriverAgent is never
	// built there.
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
		t.Error("the executable bit was lost")
	}

	for name, h := range map[string]*tar.Header{
		"file":    {Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o644},
		"symlink": {Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../etc"},
	} {
		bad := writeTarGz(t, []*tar.Header{h}, []string{""})
		if err := untarGz(bad, t.TempDir()); err == nil {
			t.Errorf("%s escaping the destination was extracted", name)
		}
	}
}

func sha1Sum(b []byte) [20]byte { return sha1.Sum(b) }

// xcodebuild waits, rather than exits, on a locked phone — so the lock has to
// be read out of its log or the caller waits out its whole timeout. The line
// is verbatim from Xcode 26.6 with the iPhone locked.
func TestPhoneRunnerBlockedOnALockedPhone(t *testing.T) {
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked.log")
	os.WriteFile(locked, []byte(`Error Domain=com.apple.dt.deviceprep Code=-3 "Unlock Test iPhone to Continue" UserInfo={NSLocalizedRecoverySuggestion=Xcode cannot launch WebDriverAgentRunner on Test iPhone because the device is locked.}`), 0o644)
	if why := (&PhoneRunner{Log: locked}).Blocked(); !strings.Contains(why, "unlock") {
		t.Errorf("a locked phone was not reported: %q", why)
	}
	fine := filepath.Join(dir, "fine.log")
	os.WriteFile(fine, []byte("ServerURLHere->http://10.0.0.1:8100<-ServerURLHere\n"), 0o644)
	if why := (&PhoneRunner{Log: fine}).Blocked(); why != "" {
		t.Errorf("a running runner was reported blocked: %q", why)
	}
}
