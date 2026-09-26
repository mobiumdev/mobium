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
	"strings"
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
	if run, err := findXCTestRun(derived); err == nil {
		return run, nil
	}

	src, err := ensureWDASource(ctx, progress)
	if err != nil {
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

	cmd := exec.CommandContext(ctx, "xcodebuild", "build-for-testing",
		"-project", filepath.Join(src, "WebDriverAgent.xcodeproj"),
		"-scheme", "WebDriverAgentRunner",
		"-destination", "id="+udid,
		"-derivedDataPath", derived,
		"-allowProvisioningUpdates",
		"DEVELOPMENT_TEAM="+team,
		"CODE_SIGN_STYLE=Automatic",
		"PRODUCT_BUNDLE_IDENTIFIER="+strings.TrimSuffix(PhoneWDABundleID(team), ".xctrunner"),
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	runErr := cmd.Run()

	if run, err := findXCTestRun(derived); err == nil && runErr == nil {
		return run, nil
	}
	return "", mobiumerr.New(mobiumerr.ToolchainMissing, "building WebDriverAgent failed: %s. The full log is %s",
		buildFailure(logPath), logPath)
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
}

// StartPhoneWDA launches the runner. It is deliberately not bound to the
// caller's context: the process outlives the request that started it and
// ends when the session does.
func StartPhoneWDA(xctestrun, udid, logPath string) (*PhoneRunner, error) {
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("xcodebuild", "test-without-building",
		"-xctestrun", xctestrun, "-destination", "id="+udid)
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
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// Stop ends the runner and waits for xcodebuild to go.
func (r *PhoneRunner) Stop() {
	if r == nil || r.cmd.Process == nil {
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
