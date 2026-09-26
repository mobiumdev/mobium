package device

import (
	"archive/zip"
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// WebDriverAgent is Appium's, under Apache-2.0. Appium publishes a prebuilt
// runner for arm64 simulators, so mobium downloads that rather than requiring
// an xcodebuild of WDA from source — the step that makes an Appium iOS setup
// slow and version-fragile.
//
// Pinned, not tracked, for the same reason as the UiAutomator2 server: a
// device-side agent that changes underneath you is the drift this tool exists
// to avoid, and the checksum only means something against a fixed release.
const (
	WDAVersion  = "16.12.8"
	wdaRelease  = "https://github.com/appium/WebDriverAgent/releases/download/v" + WDAVersion
	WDABundleID = "com.facebook.WebDriverAgentRunner.xctrunner"
	wdaAppName  = "WebDriverAgentRunner-Runner.app"
	// WDAPort is where WDA listens. A simulator shares the host network
	// stack, so this is reachable on localhost with no forwarding — unlike
	// Android, which needs `adb forward`.
	WDAPort = 8100
)

// wdaSimArm64 is the prebuilt runner for Apple Silicon simulators.
var wdaSimArm64 = uia2Artifact{
	name:   "WebDriverAgentRunner-Build-Sim-arm64.zip",
	url:    wdaRelease + "/WebDriverAgentRunner-Build-Sim-arm64.zip",
	sha256: "99bca36962e6f06bb140971f467e851f4cebf9e89c20af8d45bcd6f3bd00aab4",
}

// WDACacheDir is where the downloaded runner lives, one directory per version.
func WDACacheDir() string {
	return filepath.Join(cacheRoot(), "webdriveragent", WDAVersion)
}

// EnsureWDARunner downloads and unpacks the runner, returning the .app path.
func EnsureWDARunner(ctx context.Context, progress func(string)) (string, error) {
	dir := WDACacheDir()
	appPath := filepath.Join(dir, wdaAppName)

	// An unpacked runner with its executable present is ready to install.
	if _, err := os.Stat(filepath.Join(appPath, "WebDriverAgentRunner-Runner")); err == nil {
		return appPath, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create WebDriverAgent cache: %w", err)
	}

	zipPath := filepath.Join(dir, wdaSimArm64.name)
	if ok, _ := fileMatches(zipPath, wdaSimArm64.sha256); !ok {
		if progress != nil {
			progress(fmt.Sprintf("downloading WebDriverAgent %s", WDAVersion))
		}
		if err := download(ctx, wdaSimArm64, zipPath); err != nil {
			return "", err
		}
	}

	if progress != nil {
		progress("unpacking WebDriverAgent")
	}
	if err := unzipTo(zipPath, dir); err != nil {
		return "", err
	}
	if _, err := os.Stat(appPath); err != nil {
		return "", mobiumerr.New(mobiumerr.ToolchainMissing, "the WebDriverAgent archive did not contain %s", wdaAppName)
	}
	return appPath, nil
}

// unzipTo extracts an archive, refusing entries that would escape the
// destination directory.
func unzipTo(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(src), err)
	}
	defer r.Close()

	for _, f := range r.File {
		// A zip entry may name any path it likes; joining blindly lets an
		// archive write outside the cache directory.
		target := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return mobiumerr.New(mobiumerr.ToolchainMissing, "archive entry %q escapes the destination directory", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeZipEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}

func writeZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	// Preserve the executable bit: the runner binary and the bundled
	// frameworks will not launch without it.
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("extract %s: %w", f.Name, err)
	}
	return nil
}

// EnsureWDAInstalled makes sure the runner is installed on the simulator.
func (s *Simctl) EnsureWDAInstalled(ctx context.Context, progress func(string)) error {
	if s.AppInstalled(ctx, WDABundleID) {
		return nil
	}
	app, err := EnsureWDARunner(ctx, progress)
	if err != nil {
		return err
	}
	if progress != nil {
		progress("installing WebDriverAgent on " + s.UDID)
	}
	return s.InstallApp(ctx, app)
}
