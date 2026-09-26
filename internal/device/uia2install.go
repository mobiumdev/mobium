package device

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/paths"
)

// The UiAutomator2 server is Appium's, under Apache-2.0, downloaded from its
// GitHub releases the same way vibium downloads Chrome on first use.
//
// The version is pinned rather than tracking latest: a server that changes
// under the user is exactly the version-drift tax that makes Appium painful,
// and the checksums below only mean anything against a fixed release.
const (
	UIA2Version   = "10.6.6"
	uia2Release   = "https://github.com/appium/appium-uiautomator2-server/releases/download/v" + UIA2Version
	uia2ServerPkg = "io.appium.uiautomator2.server"
	uia2TestPkg   = "io.appium.uiautomator2.server.test"
)

// uia2Artifact is one APK of the pair.
type uia2Artifact struct {
	name   string
	url    string
	sha256 string
}

// The checksums are of the v10.6.6 release assets, verified against the files
// this was developed with. A mismatch means the download was corrupted or the
// release was re-cut, and is never something to shrug off: these APKs get
// installed with granted permissions on the user's device.
var uia2Artifacts = []uia2Artifact{
	{
		name:   "server.apk",
		url:    uia2Release + "/appium-uiautomator2-server-v" + UIA2Version + ".apk",
		sha256: "8ff760a2a86b487f53090fbdcd5b0360e67d02bb811887d527a9557b0d59c80d",
	},
	{
		name:   "test.apk",
		url:    uia2Release + "/appium-uiautomator2-server-debug-androidTest.apk",
		sha256: "e6f729287d72351388fd66597c35b69988b9091a7011954bf92a55d416cb1e1c",
	},
}

// cacheRoot is where downloaded device-side agents live.
func cacheRoot() string { return paths.Root() }

// UIA2CacheDir is where the downloaded APKs live, one directory per version.
func UIA2CacheDir() string {
	return filepath.Join(cacheRoot(), "uiautomator2", UIA2Version)
}

// downloadTimeout bounds fetching the ~18MB server APK.
const downloadTimeout = 5 * time.Minute

// EnsureUIA2APKs downloads the server pair if they are not already cached,
// verifying each against its pinned checksum. Returns the local paths.
//
// progress, when non-nil, is called once before a download starts so a CLI can
// explain the pause rather than appearing to hang on first use.
func EnsureUIA2APKs(ctx context.Context, progress func(string)) ([]string, error) {
	dir := UIA2CacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create UiAutomator2 cache: %w", err)
	}

	var out []string
	for _, a := range uia2Artifacts {
		path := filepath.Join(dir, a.name)
		if ok, _ := fileMatches(path, a.sha256); ok {
			out = append(out, path)
			continue
		}
		if progress != nil {
			progress(fmt.Sprintf("downloading UiAutomator2 server %s (%s)", UIA2Version, a.name))
		}
		if err := download(ctx, a, path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, nil
}

func fileMatches(path, want string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == want, nil
}

func download(ctx context.Context, a uia2Artifact, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", a.name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return mobiumerr.New(mobiumerr.ToolchainMissing, "download %s: %s returned %s", a.name, a.url, resp.Status)
	}

	// Write to a temp file and rename, so an interrupted download cannot
	// leave a truncated APK that the checksum check then has to catch on
	// every later run.
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("download %s: %w", a.name, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	if got := hex.EncodeToString(h.Sum(nil)); got != a.sha256 {
		os.Remove(tmp)
		return mobiumerr.New(mobiumerr.ToolchainMissing, "checksum mismatch for %s:\n  got  %s\n  want %s\n"+
			"This APK is installed on your device with permissions granted, so mobium "+
			"will not use it. Retry, and report the mismatch if it persists.", a.name, got, a.sha256)
	}
	return os.Rename(tmp, dest)
}

// InstalledUIA2Version reports the server version installed on the device, or
// "" when it is absent.
func (a *ADB) InstalledUIA2Version(ctx context.Context) string {
	out, err := a.Shell(ctx, "dumpsys", "package", uia2ServerPkg)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "versionName="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// StopUIA2 ends the server's instrumentation cleanly, by force-stopping both
// of its packages.
//
// Killing the host of `am instrument` instead — which is what canceling it
// does — takes the UiAutomation connection down with it, and the server then
// crashes on its way out, in Instrumentation.finish, disconnecting from a
// connection that is already dead. Android records that crash in dropbox, so
// every Mobium session used to end by leaving a crash report on the device:
// nine on a Pixel 8 Pro before it was noticed, and one more per session,
// reproduced. A force-stop leaves none, measured on the same phone.
func (a *ADB) StopUIA2(ctx context.Context) {
	for _, pkg := range []string{uia2TestPkg, uia2ServerPkg} {
		_, _ = a.Shell(ctx, "am", "force-stop", pkg)
	}
}

// EnsureUIA2Installed makes sure the right server version is on the device,
// downloading and installing it if not.
func (a *ADB) EnsureUIA2Installed(ctx context.Context, progress func(string)) error {
	if a.InstalledUIA2Version(ctx) == UIA2Version && a.hasPackage(ctx, uia2TestPkg) {
		return nil
	}

	apks, err := EnsureUIA2APKs(ctx, progress)
	if err != nil {
		return err
	}
	if progress != nil {
		progress("installing UiAutomator2 server on " + a.Serial)
	}
	for _, apk := range apks {
		// -r replaces an older version, -g grants the manifest permissions
		// the server needs to read the hierarchy and inject input.
		out, err := a.Run(ctx, "install", "-r", "-g", apk)
		if err != nil {
			return fmt.Errorf("install %s: %w", filepath.Base(apk), err)
		}
		// adb install prints failures on stdout and still exits 0.
		if !strings.Contains(string(out), "Success") {
			return mobiumerr.New(mobiumerr.DeviceServer, "install %s did not report success: %s",
				filepath.Base(apk), strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func (a *ADB) hasPackage(ctx context.Context, pkg string) bool {
	out, err := a.Shell(ctx, "pm", "list", "packages", pkg)
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "package:"+pkg)
}
