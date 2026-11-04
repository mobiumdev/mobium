package device

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// WebDriverAgent's LICENSE file is BSD-3-Clause. Its project publishes a prebuilt runner
// for simulators, one for Apple Silicon and one for Intel, so mobium downloads
// that rather than requiring an xcodebuild of WDA from source — the step that
// makes an iOS setup slow and version-fragile. Each build holds its own
// architecture alone: the arm64 runner cannot start on an Intel Mac's
// simulators, and was for a time the only one mobium installed (CHALLENGES 381).
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

// wdaRunner is one prebuilt simulator runner: the archive, the app inside it,
// WebDriverAgent's own test bundle inside that, and the architecture it was
// built for.
type wdaRunner struct {
	artifact   uia2Artifact
	app        string
	exe        string
	testBinary string
	arch       string
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
	arch:       "arm64",
}

// wdaSimX86 is the runner for iOS simulators on an Intel Mac.
var wdaSimX86 = wdaRunner{
	artifact: uia2Artifact{
		name:   "WebDriverAgentRunner-Build-Sim-x86_64.zip",
		url:    wdaRelease + "/WebDriverAgentRunner-Build-Sim-x86_64.zip",
		sha256: "bf683d59a8ffc031031ddeeb3d5edf9556f3060692694904edcf4b8aa5ba05a2",
	},
	app:        "WebDriverAgentRunner-Runner.app",
	exe:        "WebDriverAgentRunner-Runner",
	testBinary: "PlugIns/WebDriverAgentRunner.xctest/WebDriverAgentRunner",
	arch:       "x86_64",
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
	arch:       "arm64",
}

// wdaTVSimX86 is the runner for Apple TV simulators on an Intel Mac.
var wdaTVSimX86 = wdaRunner{
	artifact: uia2Artifact{
		name:   "WebDriverAgentRunner_tvOS-Build-Sim-x86_64.zip",
		url:    wdaRelease + "/WebDriverAgentRunner_tvOS-Build-Sim-x86_64.zip",
		sha256: "ed92aafe73a63a7427b8f287a2b85085ddc861d2653573d50f66febcd421000e",
	},
	app:        "WebDriverAgentRunner_tvOS-Runner.app",
	exe:        "WebDriverAgentRunner_tvOS-Runner",
	testBinary: "PlugIns/WebDriverAgentRunner_tvOS.xctest/WebDriverAgentRunner_tvOS",
	arch:       "x86_64",
}

// wdaRunnerFor is the runner for this Mac's simulators.
func wdaRunnerFor(tv bool) wdaRunner {
	return wdaRunnerForArch(tv, SimulatorArch())
}

func wdaRunnerForArch(tv bool, arch string) wdaRunner {
	switch {
	case tv && arch == "x86_64":
		return wdaTVSimX86
	case tv:
		return wdaTVSimArm64
	case arch == "x86_64":
		return wdaSimX86
	default:
		return wdaSimArm64
	}
}

// SimulatorArch is the architecture this Mac's simulators run: "arm64" on
// Apple Silicon, "x86_64" on an Intel Mac.
//
// It is the hardware's, never the build's. An amd64 build of mobium runs on
// Apple Silicon under Rosetta, where runtime.GOARCH says amd64 and `uname -m`
// says x86_64 while every simulator is still arm64. hw.optional.arm64 is 1 on
// Apple Silicon under Rosetta too, measured; an Intel Mac has no such key, or
// reads 0.
var SimulatorArch = sync.OnceValue(func() string {
	out, err := exec.Command("/usr/sbin/sysctl", "-n", "hw.optional.arm64").Output()
	return simulatorArch(string(out), err, runtime.GOARCH)
})

// simulatorArch reads sysctl's answer. Only when sysctl could not be asked
// at all does it fall back to the build's architecture.
func simulatorArch(sysctl string, err error, goarch string) string {
	switch strings.TrimSpace(sysctl) {
	case "1":
		return "arm64"
	case "0":
		return "x86_64"
	}
	var exitErr *exec.ExitError
	if err != nil && errors.As(err, &exitErr) {
		// An unknown oid: a Mac that predates Apple Silicon.
		return "x86_64"
	}
	if goarch == "amd64" {
		return "x86_64"
	}
	return "arm64"
}

// WDACacheDir is where the downloaded runner lives, one directory per version.
// An Intel Mac's runner is in an x86_64 folder inside it, so a cache carried
// from one Mac to another never hands over the other architecture's runner
// under the same app name.
func WDACacheDir() string {
	return filepath.Join(cacheRoot(), "webdriveragent", WDAVersion)
}

func (r wdaRunner) cacheDir() string {
	if r.arch == "x86_64" {
		return filepath.Join(WDACacheDir(), "x86_64")
	}
	return WDACacheDir()
}

// WDARunnerCached reports whether the runner a simulator session on this Mac
// would install, iOS's or with tv the Apple TV's, is already unpacked: the
// same test EnsureWDARunner makes before it downloads. A cache holding only
// another architecture's runner, or another version's, is not this Mac's —
// an Intel Mac that ran rc1 first has the arm64 one (CHALLENGES 382).
func WDARunnerCached(tv bool) bool {
	return wdaRunnerCached(wdaRunnerFor(tv))
}

func wdaRunnerCached(r wdaRunner) bool {
	_, err := os.Stat(filepath.Join(r.cacheDir(), r.app, r.exe))
	return err == nil
}

// EnsureWDARunner downloads and unpacks the runner for an iOS simulator, or
// with tv for an Apple TV simulator, returning the .app path.
func EnsureWDARunner(ctx context.Context, tv bool, progress func(string)) (string, error) {
	return ensureRunner(ctx, wdaRunnerFor(tv), progress)
}

func ensureRunner(ctx context.Context, r wdaRunner, progress func(string)) (string, error) {
	dir := r.cacheDir()
	appPath := filepath.Join(dir, r.app)

	// An unpacked runner with its executable present is ready to install.
	if wdaRunnerCached(r) {
		return appPath, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create WebDriverAgent cache: %w", err)
	}

	zipPath := filepath.Join(dir, r.artifact.name)
	if ok, _ := fileMatches(zipPath, r.artifact.sha256); !ok {
		if progress != nil {
			progress(fmt.Sprintf("downloading WebDriverAgent %s for %s simulators", WDAVersion, r.arch))
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
