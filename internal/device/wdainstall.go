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

// WebDriverAgent's LICENSE file is BSD-3-Clause. Its project publishes a prebuilt runner
// for arm64 simulators, so mobium downloads that rather than requiring an
// xcodebuild of WDA from source — the step that makes an iOS setup slow and
// version-fragile.
//
// Pinned, not tracked, for the same reason as the UiAutomator2 server: a
// device-side agent that changes underneath you is the drift this tool exists
// to avoid, and the checksum only means something against a fixed release.
const (
	WDAVersion = "16.12.8"
	wdaRelease = "https://github.com/appium/WebDriverAgent/releases/download/v" + WDAVersion
	// WDABundleID is the runner's id on an iOS simulator and an Apple TV
	// simulator alike: the tvOS build differs in its app's name, not its id.
	WDABundleID = "com.facebook.WebDriverAgentRunner.xctrunner"
	// WDAPort is where WDA listens on a phone, reached at the phone's own
	// tunnel address. A simulator's runner listens on the Mac itself, shared
	// with every other simulator's, so each is given free ports at launch
	// instead (FreePorts).
	WDAPort = 8100
)

// wdaRunner is one prebuilt runner for Apple Silicon simulators: the archive,
// the app inside it, and WebDriverAgent's own test bundle inside that.
type wdaRunner struct {
	artifact   uia2Artifact
	app        string
	exe        string
	testBinary string
}

// wdaSimArm64 is the runner for iOS simulators.
var wdaSimArm64 = wdaRunner{
	artifact: uia2Artifact{
		name:   "WebDriverAgentRunner-Build-Sim-arm64.zip",
		url:    wdaRelease + "/WebDriverAgentRunner-Build-Sim-arm64.zip",
		sha256: "99bca36962e6f06bb140971f467e851f4cebf9e89c20af8d45bcd6f3bd00aab4",
	},
	app:        "WebDriverAgentRunner-Runner.app",
	exe:        "WebDriverAgentRunner-Runner",
	testBinary: "PlugIns/WebDriverAgentRunner.xctest/WebDriverAgentRunner",
}

// wdaTVSimArm64 is the runner for Apple TV simulators, from the same
// release: an iOS runner installs on a tvOS simulator's list of apps and
// never starts.
var wdaTVSimArm64 = wdaRunner{
	artifact: uia2Artifact{
		name:   "WebDriverAgentRunner_tvOS-Build-Sim-arm64.zip",
		url:    wdaRelease + "/WebDriverAgentRunner_tvOS-Build-Sim-arm64.zip",
		sha256: "c644569782ff51b2551802af6bf0ad3d341a5d648c9464265c99f02d38d9bfdb",
	},
	app:        "WebDriverAgentRunner_tvOS-Runner.app",
	exe:        "WebDriverAgentRunner_tvOS-Runner",
	testBinary: "PlugIns/WebDriverAgentRunner_tvOS.xctest/WebDriverAgentRunner_tvOS",
}

func wdaRunnerFor(tv bool) wdaRunner {
	if tv {
		return wdaTVSimArm64
	}
	return wdaSimArm64
}

// WDACacheDir is where the downloaded runner lives, one directory per version.
func WDACacheDir() string {
	return filepath.Join(cacheRoot(), "webdriveragent", WDAVersion)
}

// EnsureWDARunner downloads and unpacks the runner for an iOS simulator, or
// with tv for an Apple TV simulator, returning the .app path.
func EnsureWDARunner(ctx context.Context, tv bool, progress func(string)) (string, error) {
	r := wdaRunnerFor(tv)
	dir := WDACacheDir()
	appPath := filepath.Join(dir, r.app)

	// An unpacked runner with its executable present is ready to install.
	if _, err := os.Stat(filepath.Join(appPath, r.exe)); err == nil {
		return appPath, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create WebDriverAgent cache: %w", err)
	}

	zipPath := filepath.Join(dir, r.artifact.name)
	if ok, _ := fileMatches(zipPath, r.artifact.sha256); !ok {
		if progress != nil {
			progress(fmt.Sprintf("downloading WebDriverAgent %s", WDAVersion))
		}
		if err := download(ctx, r.artifact, zipPath); err != nil {
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
		return "", mobiumerr.New(mobiumerr.ToolchainMissing, "the WebDriverAgent archive did not contain %s", r.app)
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
	app, err := EnsureWDARunner(ctx, s.TV, progress)
	if err != nil {
		return err
	}
	msg := "installing WebDriverAgent on " + s.UDID
	if s.AppInstalled(ctx, WDABundleID) {
		if s.installedWDAIs(ctx, app, wdaRunnerFor(s.TV).testBinary) {
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
// the one at app, by its test bundle's contents: WebDriverAgent itself, in a
// host app that is a stub whose version reads "1.0" in every build.
func (s *Simctl) installedWDAIs(ctx context.Context, app, testBinary string) bool {
	out, err := s.Run(ctx, "get_app_container", s.UDID, WDABundleID)
	if err != nil {
		return false
	}
	want, err := fileSHA256(filepath.Join(app, testBinary))
	if err != nil {
		return false
	}
	ok, err := fileMatches(filepath.Join(strings.TrimSpace(string(out)), testBinary), want)
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
