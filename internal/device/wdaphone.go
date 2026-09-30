package device

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// A real iPhone cannot run the prebuilt simulator runner. WebDriverAgent has
// to be built from source and code-signed with the user's own development
// team, so on a phone Mobium does what an Appium setup does by hand — and does
// it from a pinned, checksummed source release rather than a git checkout, so
// the runner on the phone is the same version the simulator uses.
//
// Measured on an iPhone 15 Plus, iOS 26.6.2, Xcode 26.6: the untouched source
// builds with signing supplied entirely on the command line (DEVELOPMENT_TEAM,
// CODE_SIGN_STYLE=Automatic, PRODUCT_BUNDLE_IDENTIFIER and
// -allowProvisioningUpdates), with no edits to the project. The bundle id has
// to change because `com.facebook.*` belongs to another team.

// wdaSource is the source release the phone runner is built from. GitHub
// generates tag archives on request rather than storing an uploaded file, so
// this checksum is a claim about GitHub's archiver as well as the tag — and a
// mismatch is a refusal either way, never a shrug.
var wdaSource = uia2Artifact{
	name:   "WebDriverAgent-" + WDAVersion + ".tar.gz",
	url:    "https://github.com/appium/WebDriverAgent/archive/refs/tags/v" + WDAVersion + ".tar.gz",
	sha256: "8e9f904c763941197ced629adf591840eded949d18af1c4381db9083c72cb838",
}

// wdaBuildTimeout bounds one xcodebuild of WebDriverAgent. A warm build took
// 13s; a cold one compiles the whole library and registers an app id.
const wdaBuildTimeout = 10 * time.Minute

// PhoneWDABundleID is the runner's bundle id when built for a team. The id has
// to be one the team may register, so it carries the team; xcodebuild appends
// `.xctrunner` for the test host.
func PhoneWDABundleID(team string) string {
	return "dev.mobium.wda." + team + ".xctrunner"
}

// PhoneWDAName is the product name the phone runner is built under, so the
// app on the phone is MobiumWDA-Runner rather than WebDriverAgentRunner-Runner.
//
// Tools that install their own WebDriverAgent find the runners already on a
// phone by that CFBundleName and uninstall every one but their own, whatever
// its bundle id: on 2026-09-30 appium-xcuitest-driver's session removed
// Mobium's runner from the iPhone 15 Plus that way (CHALLENGES 189). A name
// of Mobium's own takes it out of that sweep. Only the runner target is
// renamed — the build setting is looked up by target name, so
// WebDriverAgentLib keeps its name and the runner still links it.
const PhoneWDAName = "MobiumWDA"

// phoneWDANameSettings are the xcodebuild settings that rename the runner
// target and nothing else. A setting given on the command line applies to
// every target, so PRODUCT_NAME looks up a per-target setting and falls back
// to the target's own name. Measured: the runner came out as
// MobiumWDA-Runner.app with CFBundleName MobiumWDA-Runner, beside an
// unrenamed WebDriverAgentLib.framework.
var phoneWDANameSettings = []string{
	"MOBIUM_PRODUCT_WebDriverAgentRunner=" + PhoneWDAName,
	"PRODUCT_NAME=$(MOBIUM_PRODUCT_$(TARGET_NAME):default=$(TARGET_NAME))",
}

// SigningTeam returns the development team to sign WebDriverAgent with.
//
// MOBIUM_IOS_TEAM wins. Otherwise the team is read from the keychain: an
// "Apple Development" certificate carries its team id as the subject's OU, so
// a Mac with exactly one team needs no configuration. More than one is
// refused rather than guessed between.
func SigningTeam(ctx context.Context) (string, error) {
	if t := strings.TrimSpace(os.Getenv("MOBIUM_IOS_TEAM")); t != "" {
		return t, nil
	}
	teams, err := keychainTeams(ctx)
	if err != nil {
		return "", err
	}
	switch len(teams) {
	case 0:
		return "", mobiumerr.New(mobiumerr.ToolchainMissing, "no Apple Development signing certificate in the keychain. "+
			"In Xcode, open Settings > Accounts, add an Apple ID (a free one works), then "+
			"Manage Certificates > + > Apple Development")
	case 1:
		return teams[0], nil
	default:
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "the keychain holds signing certificates for %d teams (%s) — "+
			"choose one with MOBIUM_IOS_TEAM=<team id>", len(teams), strings.Join(teams, ", "))
	}
}

// keychainTeams lists the teams of unexpired Apple Development certificates
// that have a private key — a certificate without one cannot sign anything.
func keychainTeams(ctx context.Context) ([]string, error) {
	ids, err := exec.CommandContext(ctx, "security", "find-identity", "-v", "-p", "codesigning").Output()
	if err != nil {
		return nil, fmt.Errorf("read signing identities: %w", err)
	}
	pems, err := exec.CommandContext(ctx, "security", "find-certificate", "-a", "-Z", "-p",
		"-c", "Apple Development").Output()
	if err != nil {
		// No matching certificate is an exit status, not an empty answer.
		return nil, nil
	}
	return teamsFrom(string(ids), pems, time.Now()), nil
}

// teamsFrom pairs `security find-certificate -Z -p` output with the hashes of
// valid identities and returns each distinct team, sorted.
func teamsFrom(identities string, certs []byte, now time.Time) []string {
	seen := map[string]bool{}
	var hash string
	rest := certs
	for len(rest) > 0 {
		// Each certificate is preceded by its hash lines — "SHA-256 hash:"
		// first, then "SHA-1 hash:", which is the one find-identity prints.
		// Walked line by line because pem.Decode skips any text before a
		// block, and handed the SHA-256 line it skips the SHA-1 line with it.
		line, after, _ := bytes.Cut(rest, []byte("\n"))
		trimmed := bytes.TrimSpace(line)
		if h, ok := bytes.CutPrefix(trimmed, []byte("SHA-1 hash: ")); ok {
			hash = string(h)
			rest = after
			continue
		}
		if !bytes.HasPrefix(trimmed, []byte("-----BEGIN")) {
			rest = after
			continue
		}
		block, remaining := pem.Decode(rest)
		if block == nil {
			rest = after
			continue
		}
		rest = remaining
		if hash == "" || !strings.Contains(identities, hash) {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || now.After(cert.NotAfter) || len(cert.Subject.OrganizationalUnit) == 0 {
			continue
		}
		seen[cert.Subject.OrganizationalUnit[0]] = true
	}
	var out []string
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// phoneWDADir is where one team's build for one phone lives. Per phone as
// well as per team, because a development profile names the devices it may
// run on, and a second phone means a new profile.
func phoneWDADir(team, udid string) string {
	return filepath.Join(cacheRoot(), "webdriveragent-device", WDAVersion, team, udid)
}

// PhoneWDALog is where the running runner's xcodebuild output is kept, for the
// error message that points at it.
func PhoneWDALog(team, udid string) string {
	return filepath.Join(phoneWDADir(team, udid), "run.log")
}

// EnsurePhoneWDA builds WebDriverAgent for one phone if it is not built
// already, and returns the .xctestrun file that runs it.
func EnsurePhoneWDA(ctx context.Context, team, udid string, progress func(string)) (string, error) {
	dir := phoneWDADir(team, udid)
	derived := filepath.Join(dir, "build")
	if run, err := findXCTestRun(derived); err == nil && builtAtCurrentPatch(dir) {
		return run, nil
	}
	// Built before the current patch, or not at all: from scratch.
	_ = os.RemoveAll(derived)

	src, err := ensureWDASource(ctx, progress)
	if err != nil {
		return "", err
	}
	if err := patchWDASource(src); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create WebDriverAgent build directory: %w", err)
	}

	if progress != nil {
		progress(fmt.Sprintf("building WebDriverAgent %s for this iPhone, signed for team %s "+
			"(a minute or two, once)", WDAVersion, team))
	}
	ctx, cancel := context.WithTimeout(ctx, wdaBuildTimeout)
	defer cancel()
	logPath := filepath.Join(dir, "build.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return "", err
	}
	defer logFile.Close()

	args := []string{"build-for-testing",
		"-project", filepath.Join(src, "WebDriverAgent.xcodeproj"),
		"-scheme", "WebDriverAgentRunner",
		"-destination", "id=" + udid,
		"-derivedDataPath", derived,
		"-allowProvisioningUpdates",
		"DEVELOPMENT_TEAM=" + team,
		"CODE_SIGN_STYLE=Automatic",
		"PRODUCT_BUNDLE_IDENTIFIER=" + strings.TrimSuffix(PhoneWDABundleID(team), ".xctrunner"),
	}
	cmd := exec.CommandContext(ctx, "xcodebuild", append(args, phoneWDANameSettings...)...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	runErr := cmd.Run()

	if run, err := findXCTestRun(derived); err == nil && runErr == nil {
		if err := os.WriteFile(patchMarker(dir), []byte(wdaPatchLevel+"\n"), 0o644); err != nil {
			return "", err
		}
		return run, nil
	}
	return "", mobiumerr.New(mobiumerr.ToolchainMissing, "building WebDriverAgent failed: %s. The full log is %s",
		buildFailure(logPath), logPath)
}

// installedMarker records that a runner built here was installed on the
// phone and answered, so a later session that finds it gone can tell a
// removal from a first install.
func installedMarker(team, udid string) string {
	return filepath.Join(phoneWDADir(team, udid), "mobium-installed")
}

// MarkPhoneWDAInstalled records that the runner is on the phone.
func MarkPhoneWDAInstalled(team, udid string) {
	_ = os.WriteFile(installedMarker(team, udid), []byte(PhoneWDABundleID(team)+"\n"), 0o644)
}

// PhoneWDAInstallNotice is what to say before starting a runner that
// xcodebuild is about to install, or "" when it is already on the phone.
// Without it a runner another tool removed was put back in silence
// (CHALLENGES 189), and a person watching could not tell why the phone
// gained an app or why the start took longer.
func PhoneWDAInstallNotice(team string, p Phone, installed []InstalledApp) string {
	id := PhoneWDABundleID(team)
	for _, a := range installed {
		if a.ID == id {
			return ""
		}
	}
	if _, err := os.Stat(installedMarker(team, p.UDID)); err == nil {
		return "Mobium's WebDriverAgent (" + id + ") is no longer installed on " + p.Label() +
			" — something removed it since the last session, often another tool installing its own; installing it again"
	}
	return "installing WebDriverAgent (" + id + ") on " + p.Label()
}

// buildFailure pulls the first xcodebuild error out of a build log.
func buildFailure(logPath string) string {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return "no log was written"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "error: "); i >= 0 {
			return strings.TrimSpace(line[i+len("error: "):])
		}
	}
	return lastLine(strings.TrimSpace(string(data)))
}

// findXCTestRun locates the run description xcodebuild wrote. Its name carries
// the SDK version — WebDriverAgentRunner_iphoneos26.5-arm64.xctestrun — so it
// is found rather than constructed.
func findXCTestRun(derived string) (string, error) {
	matches, _ := filepath.Glob(filepath.Join(derived, "Build", "Products", "*iphoneos*.xctestrun"))
	if len(matches) == 0 {
		return "", mobiumerr.New(mobiumerr.ToolchainMissing, "no xctestrun under %s", derived)
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

// ensureWDASource downloads and unpacks the pinned source release, returning
// the directory holding WebDriverAgent.xcodeproj.
func ensureWDASource(ctx context.Context, progress func(string)) (string, error) {
	dir := filepath.Join(cacheRoot(), "webdriveragent-device", WDAVersion)
	src := filepath.Join(dir, "WebDriverAgent-"+WDAVersion)
	if _, err := os.Stat(filepath.Join(src, "WebDriverAgent.xcodeproj")); err == nil {
		return src, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	archive := filepath.Join(dir, wdaSource.name)
	if ok, _ := fileMatches(archive, wdaSource.sha256); !ok {
		if progress != nil {
			progress(fmt.Sprintf("downloading WebDriverAgent %s source", WDAVersion))
		}
		if err := download(ctx, wdaSource, archive); err != nil {
			return "", err
		}
	}
	if err := untarGz(archive, dir); err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(src, "WebDriverAgent.xcodeproj")); err != nil {
		return "", mobiumerr.New(mobiumerr.ToolchainMissing, "the WebDriverAgent source archive did not contain %s",
			filepath.Base(src))
	}
	return src, nil
}

// UntarGz is untarGz for callers outside this package: an app sent as
// content by a caller whose disk is not the daemon's arrives as a .tar.gz.
func UntarGz(src, dest string) error { return untarGz(src, dest) }

// TarGz archives a directory as a .tar.gz whose one top-level entry is the
// directory's own name — how a .app, which is a directory, travels as content
// to a daemon on another machine. Links are kept as links, as a bundle's
// frameworks need; untarGz keeps them only when they stay inside.
func TarGz(dir string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	parent := filepath.Dir(filepath.Clean(dir))
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(parent, p)
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(p); err != nil {
				return err
			}
		}
		h, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// untarGz extracts a .tar.gz, refusing entries that would escape dest, the
// same guard unzipTo has.
func untarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(src), err)
	}
	tr := tar.NewReader(gz)
	clean := filepath.Clean(dest) + string(os.PathSeparator)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(src), err)
		}
		target := filepath.Join(dest, h.Name)
		if !strings.HasPrefix(target, clean) {
			return mobiumerr.New(mobiumerr.ToolchainMissing, "archive entry %q escapes the destination directory", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode).Perm())
			if err != nil {
				return err
			}
			_, cerr := io.Copy(out, tr)
			out.Close()
			if cerr != nil {
				return fmt.Errorf("extract %s: %w", h.Name, cerr)
			}
		case tar.TypeSymlink:
			// A link may point anywhere; only one that stays inside is kept.
			if !strings.HasPrefix(filepath.Join(filepath.Dir(target), h.Linkname), clean) {
				return mobiumerr.New(mobiumerr.ToolchainMissing, "archive link %q escapes the destination directory", h.Name)
			}
			_ = os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		}
		// Anything else — the pax global header GitHub archives start with
		// among them — carries no file.
	}
}

// PhoneRunner is WebDriverAgent running on a phone: an `xcodebuild
// test-without-building` process on this Mac, which is what keeps the XCTest
// session on the device alive. Stopping it stops the runner — measured.
type PhoneRunner struct {
	cmd  *exec.Cmd
	done chan struct{}
	Log  string

	// adopted is the pid of a runner this process did not start but has
	// taken over (AdoptPhoneRunner); cmd and done are nil then.
	adopted int
}

// AdoptPhoneRunner finds a runner a daemon that died left behind on this
// phone, and takes it over, so this session's end stops it.
//
// A daemon killed without tearing down left its xcodebuild running, and the
// next daemon found WebDriverAgent answering and used it as someone else's —
// so nothing ever stopped it: not the next session's end, not `daemon stop`.
// Measured on an iPhone 15 Plus (iOS 26.6.2). CHALLENGES 135.
//
// Only a runner that is certainly ours, and certainly orphaned: an xcodebuild
// running this cache's WebDriverAgent build for this phone, whose parent is
// pid 1 — what a process becomes when the one that started it dies. A runner
// started from Xcode runs a different build, and one a live daemon owns still
// has that daemon for a parent; both are left alone. nil if there is none.
func AdoptPhoneRunner(ctx context.Context, udid string) *PhoneRunner {
	out, err := exec.CommandContext(ctx, "ps", "-Ao", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil
	}
	pid := orphanedRunner(string(out), filepath.Join(cacheRoot(), "webdriveragent-device"), udid)
	if pid == 0 {
		return nil
	}
	return &PhoneRunner{adopted: pid}
}

// orphanedRunner reads `ps -Ao pid=,ppid=,command=` for the pid of an
// orphaned xcodebuild running a build under buildRoot for udid, or 0.
//
// The separator is "/" rather than the host's: ps and xcodebuild run only on
// macOS, and a host-dependent one made the parser's test fail on Windows.
func orphanedRunner(ps, buildRoot, udid string) int {
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[1] != "1" {
			continue
		}
		cmd := strings.Join(f[2:], " ")
		if strings.Contains(cmd, "xcodebuild test-without-building") &&
			strings.Contains(cmd, "-xctestrun "+buildRoot+"/") &&
			strings.Contains(cmd, "-destination id="+udid) {
			if pid, err := strconv.Atoi(f[0]); err == nil {
				return pid
			}
		}
	}
	return 0
}

// StartPhoneWDA launches the runner. It is deliberately not bound to the
// caller's context: the process outlives the request that started it and
// ends when the session does.
//
// bindIP is the address the runner's server listens on — the phone's end of
// the CoreDevice tunnel. Left to itself, WebDriverAgent listens on every
// interface, and on Wi-Fi it answered anyone on the network with the live
// session (CHALLENGES 153). xcodebuild hands TEST_RUNNER_-prefixed
// variables to the runner without the prefix, so this reaches it as USE_IP.
func StartPhoneWDA(xctestrun, udid, logPath, bindIP string) (*PhoneRunner, error) {
	if bindIP == "" {
		return nil, mobiumerr.New(mobiumerr.Internal, "no address to bind WebDriverAgent to on %s", udid)
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("xcodebuild", "test-without-building",
		"-xctestrun", xctestrun, "-destination", "id="+udid)
	cmd.Env = append(os.Environ(), "TEST_RUNNER_USE_IP="+bindIP)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start xcodebuild: %w", err)
	}
	r := &PhoneRunner{cmd: cmd, done: make(chan struct{}), Log: logPath}
	go func() {
		_ = cmd.Wait()
		logFile.Close()
		close(r.done)
	}()
	return r, nil
}

// Exited reports whether the runner's xcodebuild has already ended — which,
// while waiting for it to become ready, means it failed.
func (r *PhoneRunner) Exited() bool {
	if r.adopted != 0 {
		return !processAlive(r.adopted)
	}
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// Stop ends the runner and waits for xcodebuild to go.
func (r *PhoneRunner) Stop() {
	if r == nil {
		return
	}
	if r.adopted != 0 {
		// Not our child, so there is no Wait: signal it, and watch it go.
		p, err := os.FindProcess(r.adopted)
		if err != nil {
			return
		}
		_ = p.Signal(os.Interrupt)
		for deadline := time.Now().Add(10 * time.Second); processAlive(r.adopted); time.Sleep(100 * time.Millisecond) {
			if time.Now().After(deadline) {
				_ = p.Kill()
				return
			}
		}
		return
	}
	if r.cmd.Process == nil {
		return
	}
	_ = r.cmd.Process.Signal(os.Interrupt)
	select {
	case <-r.done:
	case <-time.After(10 * time.Second):
		_ = r.cmd.Process.Kill()
		<-r.done
	}
}

// processAlive reports whether pid is still running, by the null signal.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// Blocked reports a reason the runner is waiting on the person holding the
// phone rather than failing — xcodebuild does not exit for these, it waits,
// so without this the caller waits out its whole timeout and then gives a
// generic answer. Measured: a locked iPhone made xcodebuild log "Unlock
// <name> to Continue … because the device is locked" and sit there.
func (r *PhoneRunner) Blocked() string {
	data, _ := os.ReadFile(r.Log)
	if bytes.Contains(data, []byte("because the device is locked")) {
		return "the phone is locked — unlock it, and set Settings > Display & Brightness > " +
			"Auto-Lock long enough that it stays unlocked while it is driven"
	}
	return ""
}

// Failure explains why a runner did not come up, from its log.
//
// One cause is named because it was met: with UI Automation switched off on
// the phone, the runner installs, launches, and then fails with "Timed out
// while enabling automation mode" — nothing in that sentence says which
// switch. Anything else is quoted from the log rather than guessed at.
func (r *PhoneRunner) Failure() string {
	data, _ := os.ReadFile(r.Log)
	log := string(data)
	if strings.Contains(log, "Timed out while enabling automation mode") {
		return "the phone refused UI automation. Turn on Settings > Developer > " +
			"Enable UI Automation, keep the phone unlocked, and try again"
	}
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "encountered an error") || strings.Contains(line, "error: ") {
			return strings.TrimSpace(line)
		}
	}
	return "no error was logged"
}
