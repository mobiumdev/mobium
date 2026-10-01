package device

import (
	"archive/zip"
	"context"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// WebDriverAgent is under Apache-2.0. Its project publishes a prebuilt runner
// for arm64 simulators, so mobium downloads that rather than requiring an
// xcodebuild of WDA from source — the step that makes an iOS setup slow and
// version-fragile.
//
// Pinned, not tracked, for the same reason as the UiAutomator2 server: a
// device-side agent that changes underneath you is the drift this tool exists
// to avoid, and the checksum only means something against a fixed release.
const (
	WDAVersion  = "16.12.8"
	wdaRelease  = "https://github.com/appium/WebDriverAgent/releases/download/v" + WDAVersion
	WDABundleID = "com.facebook.WebDriverAgentRunner.xctrunner"
	wdaAppName  = "WebDriverAgentRunner-Runner.app"
	// WDAPort is where WDA listens on a phone, reached at the phone's own
	// tunnel address. A simulator's runner listens on the Mac itself, shared
	// with every other simulator's, so each is given free ports at launch
	// instead (FreePorts).
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

// wdaTestBinary is WebDriverAgent itself: the test bundle inside the runner.
// The host app around it is a stub whose version reads "1.0" in every build.
const wdaTestBinary = "PlugIns/WebDriverAgentRunner.xctest/WebDriverAgentRunner"

// EnsureWDAInstalled makes sure mobium's pinned runner is the one installed
// on the simulator.
//
// Every WebDriverAgent build has the same bundle id, and a tool that builds
// its own from source installs it over whatever is there. Checking only that
// the id was installed, mobium went on driving such a 16.12.11 build while it
// pinned 16.12.8 (CHALLENGES 181), and both report version "1.0". A
// simulator's app container is a folder on this Mac, so the installed test
// bundle is compared with the verified one instead, and replaced when they
// differ.
func (s *Simctl) EnsureWDAInstalled(ctx context.Context, progress func(string)) error {
	app, err := EnsureWDARunner(ctx, progress)
	if err != nil {
		return err
	}
	msg := "installing WebDriverAgent on " + s.UDID
	if s.AppInstalled(ctx, WDABundleID) {
		if s.installedWDAIs(ctx, app) {
			return nil
		}
		msg = "replacing a WebDriverAgent that is not mobium's " + WDAVersion + " on " + s.UDID
	}
	if progress != nil {
		progress(msg)
	}
	return s.InstallApp(ctx, app)
}

// installedWDAIs reports whether the runner installed on the simulator is
// the one at app, by its test bundle's contents.
func (s *Simctl) installedWDAIs(ctx context.Context, app string) bool {
	out, err := s.Run(ctx, "get_app_container", s.UDID, WDABundleID)
	if err != nil {
		return false
	}
	want, err := fileSHA256(filepath.Join(app, wdaTestBinary))
	if err != nil {
		return false
	}
	ok, err := fileMatches(filepath.Join(strings.TrimSpace(string(out)), wdaTestBinary), want)
	return err == nil && ok
}

// FreePorts asks the system for n TCP ports free on this Mac right now.
//
// Every simulator's WebDriverAgent listens on the Mac itself, so two
// simulators given the same port are one server: on 2026-09-28 a second
// daemon's reads and taps went to the first simulator's runner, and it
// reported that simulator's screen as its own (CHALLENGES 148). Each
// simulator's runner is given its own ports at launch instead.
func FreePorts(n int) ([]int, error) {
	var ports []int
	var held []net.Listener
	defer func() {
		for _, l := range held {
			l.Close()
		}
	}()
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("find a free port for WebDriverAgent: %w", err)
		}
		held = append(held, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports, nil
}
