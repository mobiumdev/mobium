package device

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// The hit test below accessibility, on an iOS simulator. See
// hitprobe/probe.m for what it asks and why accessibility cannot answer it,
// and docs/decisions/0008 for why it is opt-in: it attaches a debugger to
// the app, which stops it for about two seconds.

//go:embed hitprobe/probe.m
var hitProbeSource []byte

// hitProbeTimeout bounds one attach, call and detach; each took about two
// seconds on an iPhone 17 Pro simulator.
const hitProbeTimeout = 30 * time.Second

// Hit is UIKit's answer for a touch at a point.
type Hit struct {
	// Verdict is "reaches", "covered", "nothing" (no view takes a touch
	// there) or "unknown".
	Verdict string
	// Reason says why the verdict is unknown.
	Reason string
	// For covered: the view that would take the touch, whether
	// accessibility can see it, its accessibility label if any, and its
	// frame in points.
	Class  string
	Hidden bool
	Label  string
	Frame  [4]float64
}

// hitProbeSymbol is the probe's function for this source, so a library an
// older mobium loaded into the same app is never the one called.
func hitProbeSymbol() string {
	sum := sha256.Sum256(hitProbeSource)
	return "mobium_hit_" + hex.EncodeToString(sum[:6])
}

// hitProbeLibrary compiles the probe for this Mac's simulators, once per
// source, and returns the library's path.
func (s *Simctl) hitProbeLibrary(ctx context.Context) (string, error) {
	sym := hitProbeSymbol()
	dir := filepath.Join(cacheRoot(), "hitprobe", strings.TrimPrefix(sym, "mobium_hit_"))
	lib := filepath.Join(dir, "probe.dylib")
	if _, err := os.Stat(lib); err == nil {
		return lib, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create the hit probe's cache: %w", err)
	}
	src := filepath.Join(dir, "probe.m")
	if err := os.WriteFile(src, hitProbeSource, 0o644); err != nil {
		return "", err
	}
	sdk, err := exec.CommandContext(ctx, s.Path, "--sdk", "iphonesimulator", "--show-sdk-path").Output()
	if err != nil {
		return "", mobiumerr.New(mobiumerr.ToolchainMissing, "Xcode has no iOS simulator SDK to build the hit probe against: %w", err)
	}
	arch := "arm64"
	if runtime.GOARCH == "amd64" {
		arch = "x86_64"
	}
	tmp := lib + ".tmp"
	cmd := exec.CommandContext(ctx, s.Path, "--sdk", "iphonesimulator", "clang", "-dynamiclib", "-fobjc-arc",
		"-target", arch+"-apple-ios15.0-simulator", "-isysroot", strings.TrimSpace(string(sdk)),
		"-framework", "UIKit", "-DMOBIUM_HIT="+sym, "-o", tmp, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", mobiumerr.New(mobiumerr.ToolchainMissing, "build the hit probe: %v: %s", err, firstLine(string(out)))
	}
	return lib, os.Rename(tmp, lib)
}

// appPIDRe finds an app's process in the simulator's launchctl list:
// "24456	0	UIKitApplication:dev.mobium.mobiumapp[b1f2][rb-legacy]".
func appPIDRe(bundleID string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^(\d+)\s+\S+\s+UIKitApplication:` + regexp.QuoteMeta(bundleID) + `\[`)
}

// AppPID is the running app's process id on the simulator.
func (s *Simctl) AppPID(ctx context.Context, bundleID string) (int, error) {
	out, err := s.Run(ctx, "spawn", s.UDID, "launchctl", "list")
	if err != nil {
		return 0, err
	}
	m := appPIDRe(bundleID).FindSubmatch(out)
	if m == nil {
		return 0, mobiumerr.New(mobiumerr.DeviceNotReady, "%s is not running on the simulator", bundleID)
	}
	return strconv.Atoi(string(m[1]))
}

// HitTest asks UIKit, inside the app, which view a touch at (x, y) would go
// to, and whether that is the element accessibility reports at frame with
// identifier id. All in points. It attaches lldb to the app, loads the probe,
// calls it once and detaches: the app is stopped for the time that takes.
func (s *Simctl) HitTest(ctx context.Context, bundleID string, x, y float64, frame [4]float64, id string) (Hit, error) {
	lib, err := s.hitProbeLibrary(ctx)
	if err != nil {
		return Hit{}, err
	}
	pid, err := s.AppPID(ctx, bundleID)
	if err != nil {
		return Hit{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, hitProbeTimeout)
	defer cancel()
	call := fmt.Sprintf(`expr -- (const char *)((const char *(*)(double, double, double, double, double, double, const char *))dlsym($mobium_probe, %q))(%g, %g, %g, %g, %g, %g, %s)`,
		hitProbeSymbol(), x, y, frame[0], frame[1], frame[2], frame[3], strconv.Quote(id))
	cmd := exec.CommandContext(ctx, s.Path, "lldb", "--batch", "--no-lldbinit", "-p", strconv.Itoa(pid),
		"-o", fmt.Sprintf(`expr void *$mobium_probe = (void *)dlopen(%q, 2)`, lib),
		"-o", call,
		"-o", "detach")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return Hit{}, mobiumerr.New(mobiumerr.Timeout, "the hit test did not finish within %s", hitProbeTimeout)
	}
	hit, perr := parseHitAnswer(out)
	if perr != nil {
		if err != nil {
			return Hit{}, mobiumerr.New(mobiumerr.DeviceServer, "attach lldb to %s: %v: %s", bundleID, err, lldbError(out))
		}
		return Hit{}, perr
	}
	return hit, nil
}

// hitAnswerRe is lldb's print of the probe's string:
// (const char *) $1 = 0x000000010aac0000 "covered\t1\t…".
var hitAnswerRe = regexp.MustCompile(`\(const char \*\) \$\d+ = 0x[0-9a-f]+ ("(?:[^"\\]|\\.)*")`)

func parseHitAnswer(out []byte) (Hit, error) {
	m := hitAnswerRe.FindSubmatch(out)
	if m == nil {
		return Hit{}, mobiumerr.New(mobiumerr.DeviceServer, "the hit probe did not answer: %s", lldbError(out))
	}
	line, err := strconv.Unquote(string(m[1]))
	if err != nil {
		return Hit{}, mobiumerr.New(mobiumerr.DeviceServer, "read the hit probe's answer %s: %w", m[1], err)
	}
	f := strings.Split(line, "\t")
	h := Hit{Verdict: f[0]}
	switch {
	case h.Verdict == "reaches" || h.Verdict == "nothing":
	case h.Verdict == "unknown" && len(f) == 2:
		h.Reason = f[1]
	case h.Verdict == "covered" && len(f) == 8:
		h.Hidden, h.Class, h.Label = f[1] == "1", f[2], f[3]
		for i := range h.Frame {
			h.Frame[i], _ = strconv.ParseFloat(f[4+i], 64)
		}
	default:
		return Hit{}, mobiumerr.New(mobiumerr.DeviceServer, "the hit probe answered %q", line)
	}
	return h, nil
}

// lldbError is the part of lldb's output worth quoting in an error.
func lldbError(out []byte) string {
	for _, line := range bytes.Split(out, []byte("\n")) {
		if t := strings.TrimSpace(string(line)); strings.HasPrefix(t, "error:") {
			return t
		}
	}
	return firstLine(strings.TrimSpace(string(out)))
}
