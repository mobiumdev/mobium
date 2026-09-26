package mobiumdriver

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mobiumdev/mobium/internal/device"
	"github.com/mobiumdev/mobium/internal/uitree"
)

// External drives a device through a driver program that Mobium did not
// compile in — the extension point described in
// docs/decisions/0003-drivers-are-processes-not-plugins.md.
//
// The program reads JSON-RPC 2.0 on stdin and writes it on stdout, one object
// per line. Its stderr is its log, not part of the protocol, which is what
// makes a stray print in somebody's driver a nuisance rather than a corrupted
// channel.

// ProtocolVersion is the driver protocol Mobium speaks. A driver announcing
// anything else is refused: a version mismatch that is allowed through shows
// up later as a field quietly missing, which is far harder to diagnose than a
// refusal at startup.
const ProtocolVersion = "1"

// externalStartTimeout bounds the handshake. A driver that has not answered
// initialize by then is not going to.
const externalStartTimeout = 20 * time.Second

// externalStopTimeout is how long a driver gets to exit after being asked
// nicely, before it is killed.
const externalStopTimeout = 5 * time.Second

// stderrTail is how many bytes of a driver's log are kept to quote back when
// something goes wrong. A driver that dies during startup usually says why on
// stderr, and without this the user sees only "the driver exited".
const stderrTail = 4096

// External is a driver backed by a child process.
type External struct {
	name    string // the backend name the user asked for
	path    string // the executable
	deviceR string // the --device string, passed through verbatim

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	log    *tailBuffer

	// mu serializes calls. The protocol allows ids to interleave, but nothing
	// above here issues concurrent commands to one device, and a driver author
	// should not have to handle it.
	mu     sync.Mutex
	nextID int

	// reported is what the driver said in the handshake.
	reportedName string
	caps         map[string]bool

	// exited is closed when the process has gone, so a read blocked on a dead
	// driver fails with the reason rather than hanging.
	exited   chan struct{}
	waitErr  error
	stopOnce sync.Once

	// strays is output the driver wrote to stdout that was not a reply, kept
	// to quote back when a call fails. Its own lock: it is written by the read
	// goroutine while the caller may be reading it to build an error.
	strayMu sync.Mutex
	strays  []string

	// lines carries every message the driver writes, fed by the single
	// readLoop goroutine. Closed when its stdout does.
	lines       chan []byte
	readLoopErr atomic.Pointer[error]
}

// NewExternal prepares a driver process. Nothing is spawned until Start.
func NewExternal(name, path, deviceRef string) *External {
	return &External{
		name:    name,
		path:    path,
		deviceR: deviceRef,
		exited:  make(chan struct{}),
		lines:   make(chan []byte, 1),
	}
}

// FindDriver locates the executable for a backend name, or explains why it
// could not. MOBIUM_DRIVER_<NAME> wins, which is how a driver is developed
// without installing it.
func FindDriver(name string) (string, error) {
	if name == "" {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "no backend name given")
	}
	env := "MOBIUM_DRIVER_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(name))
	if p := os.Getenv(env); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("%s points at %s, which is not there: %w", env, p, err)
		}
		return p, nil
	}
	exe := "mobium-driver-" + name
	path, err := exec.LookPath(exe)
	if err != nil {
		return "", mobiumerr.New(mobiumerr.InvalidArgument, "there is no built-in backend named %q, and no %s on your PATH.\n"+
			"A third-party backend is an executable of that name — see "+
			"docs/decisions/0003-drivers-are-processes-not-plugins.md.\n"+
			"To point at one without installing it, set %s=/path/to/driver and then run "+
			"`mobium daemon stop`: the daemon is what looks for drivers, and a running one keeps "+
			"the environment it started with.", name, exe, env)
	}
	return path, nil
}

func (e *External) Name() string {
	if e.reportedName != "" {
		return e.reportedName
	}
	return e.name
}

// HasCapability reports what the driver advertised in the handshake.
func (e *External) HasCapability(cap string) bool { return e.caps[cap] }

// Capabilities lists what the driver advertised, for diagnostics.
func (e *External) Capabilities() []string {
	var out []string
	for _, c := range KnownCapabilities {
		if e.caps[c] {
			out = append(out, c)
		}
	}
	return out
}

// Start spawns the driver and performs the handshake.
func (e *External) Start(ctx context.Context, progress func(string)) error {
	if progress != nil {
		progress(fmt.Sprintf("Starting driver %s (%s)", e.name, e.path))
	}

	cmd := exec.Command(e.path)
	// A driver may want to know how it was invoked without parsing argv.
	cmd.Env = append(os.Environ(), "MOBIUM_DRIVER_NAME="+e.name)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("could not open a pipe to driver %s: %w", e.name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("could not open a pipe from driver %s: %w", e.name, err)
	}
	e.log = newTailBuffer(stderrTail)
	cmd.Stderr = e.log

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not run driver %s at %s: %w", e.name, e.path, err)
	}
	e.cmd, e.stdin, e.stdout = cmd, stdin, bufio.NewReader(stdout)

	go e.readLoop()
	go func() {
		e.waitErr = cmd.Wait()
		close(e.exited)
	}()

	hctx, cancel := context.WithTimeout(ctx, externalStartTimeout)
	defer cancel()

	var reply struct {
		ProtocolVersion string   `json:"protocolVersion"`
		Name            string   `json:"name"`
		Capabilities    []string `json:"capabilities"`
	}
	err = e.call(hctx, "initialize", map[string]interface{}{
		"protocolVersion": ProtocolVersion,
		"device":          e.deviceR,
	}, &reply)
	if err != nil {
		e.Close()
		return fmt.Errorf("driver %s did not start: %w", e.name, err)
	}
	if reply.ProtocolVersion != ProtocolVersion {
		e.Close()
		return mobiumerr.New(mobiumerr.Unsupported, "driver %s speaks protocol version %q, and this mobium speaks %q. "+
			"Update whichever is older.", e.name, reply.ProtocolVersion, ProtocolVersion)
	}

	e.reportedName = reply.Name
	e.caps = map[string]bool{}
	known := map[string]bool{}
	for _, c := range KnownCapabilities {
		known[c] = true
	}
	var unknown []string
	for _, c := range reply.Capabilities {
		if known[c] {
			e.caps[c] = true
		} else {
			unknown = append(unknown, c)
		}
	}
	if progress != nil {
		// Worth saying out loud: a capability Mobium does not know about is
		// ignored rather than an error, and silently ignoring it is how a
		// driver author loses an afternoon.
		if len(unknown) > 0 {
			progress(fmt.Sprintf("Driver %s advertises %s, which this mobium does not use",
				e.Name(), strings.Join(unknown, ", ")))
		}
		progress(fmt.Sprintf("Driver %s ready (%s)", e.Name(),
			orNone(strings.Join(e.Capabilities(), ", "))))
	}
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "no optional capabilities"
	}
	return s
}

// Close asks the driver to shut down, then makes sure it has.
//
// The two-step matters: a driver may hold a real connection to a device, and
// killing it outright leaves that connection open on the far side — the same
// reason the daemon is stopped before the emulator.
func (e *External) Close() error {
	var err error
	e.stopOnce.Do(func() {
		if e.cmd == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), externalStopTimeout)
		defer cancel()
		// Best effort: a driver that has already crashed cannot be asked.
		_ = e.call(ctx, "shutdown", map[string]interface{}{}, nil)
		if e.stdin != nil {
			e.stdin.Close()
		}
		select {
		case <-e.exited:
		case <-time.After(externalStopTimeout):
			if e.cmd.Process != nil {
				_ = e.cmd.Process.Kill()
			}
			<-e.exited
			err = mobiumerr.New(mobiumerr.Internal, "driver %s did not exit when asked and was killed", e.name)
		}
	})
	return err
}

// Snapshot asks the driver for the hierarchy and rebuilds Mobium's derived
// fields from it.
func (e *External) Snapshot(ctx context.Context) (*uitree.Tree, error) {
	var raw json.RawMessage
	if err := e.call(ctx, "snapshot", nil, &raw); err != nil {
		return nil, err
	}
	tree, err := uitree.UnmarshalWire(raw)
	if err != nil {
		return nil, fmt.Errorf("driver %s: %w", e.Name(), err)
	}
	return tree, nil
}

// Screenshot decodes the base64 PNG the driver sends.
func (e *External) Screenshot(ctx context.Context) ([]byte, error) {
	var reply struct {
		PNG string `json:"png"`
	}
	if err := e.call(ctx, "screenshot", nil, &reply); err != nil {
		return nil, err
	}
	png, err := base64.StdEncoding.DecodeString(reply.PNG)
	if err != nil {
		return nil, fmt.Errorf("driver %s sent a screenshot that is not valid base64: %w", e.Name(), err)
	}
	// The same check the Android backend makes, for the same reason: a driver
	// that puts an error message where the image goes should be caught here
	// rather than by whatever tries to display it.
	if !hasPNGMagic(png) {
		return nil, mobiumerr.New(mobiumerr.DeviceServer, "driver %s sent %d bytes that are not a PNG", e.Name(), len(png))
	}
	return png, nil
}

func hasPNGMagic(b []byte) bool {
	if len(b) < len(pngMagic) {
		return false
	}
	for i := range pngMagic {
		if b[i] != pngMagic[i] {
			return false
		}
	}
	return true
}

func (e *External) Tap(ctx context.Context, x, y int) error {
	return e.call(ctx, "tap", map[string]interface{}{"x": x, "y": y}, nil)
}

func (e *External) Swipe(ctx context.Context, x1, y1, x2, y2 int, d time.Duration) error {
	return e.call(ctx, "swipe", map[string]interface{}{
		"x1": x1, "y1": y1, "x2": x2, "y2": y2, "durationMs": d.Milliseconds(),
	}, nil)
}

func (e *External) LongPress(ctx context.Context, x, y int, d time.Duration) error {
	return e.call(ctx, "longPress", map[string]interface{}{
		"x": x, "y": y, "durationMs": d.Milliseconds(),
	}, nil)
}

// SetText addresses the element by path. The driver sent the hierarchy, so it
// can find its own node; sending the node back would put Mobium's bookkeeping
// on the wire.
func (e *External) SetText(ctx context.Context, n *uitree.Node, text string) error {
	return e.call(ctx, "setText", map[string]interface{}{"path": n.Path, "text": text}, nil)
}

func (e *External) Clear(ctx context.Context, n *uitree.Node) error {
	return e.call(ctx, "clear", map[string]interface{}{"path": n.Path}, nil)
}

func (e *External) Launch(ctx context.Context, appID string) error {
	return e.call(ctx, "launch", map[string]interface{}{"appId": appID}, nil)
}

func (e *External) Terminate(ctx context.Context, appID string) error {
	return e.call(ctx, "terminate", map[string]interface{}{"appId": appID}, nil)
}

func (e *External) Install(ctx context.Context, path string) error {
	return e.call(ctx, "install", map[string]interface{}{"path": path}, nil)
}

func (e *External) OpenURL(ctx context.Context, url string) error {
	return e.call(ctx, "openUrl", map[string]interface{}{"url": url}, nil)
}

func (e *External) Appearance(ctx context.Context) (string, error) {
	var reply struct {
		Mode string `json:"mode"`
	}
	if err := e.call(ctx, "appearance", nil, &reply); err != nil {
		return "", err
	}
	return reply.Mode, nil
}

func (e *External) SetAppearance(ctx context.Context, mode string) error {
	return e.call(ctx, "setAppearance", map[string]interface{}{"mode": mode}, nil)
}

func (e *External) ListApps(ctx context.Context, includeSystem bool) ([]device.InstalledApp, error) {
	var reply struct {
		Apps []device.InstalledApp `json:"apps"`
	}
	if err := e.call(ctx, "listApps", map[string]interface{}{"includeSystem": includeSystem}, &reply); err != nil {
		return nil, err
	}
	return reply.Apps, nil
}

func (e *External) Uninstall(ctx context.Context, appID string) error {
	return e.call(ctx, "uninstall", map[string]interface{}{"appId": appID}, nil)
}

func (e *External) SetPermission(ctx context.Context, appID, permission string, grant bool) error {
	return e.call(ctx, "setPermission", map[string]interface{}{
		"appId": appID, "permission": permission, "grant": grant,
	}, nil)
}

func (e *External) ResetPermissions(ctx context.Context, appID string) error {
	return e.call(ctx, "resetPermissions", map[string]interface{}{"appId": appID}, nil)
}

func (e *External) PermissionState(ctx context.Context, appID string) (map[string]bool, error) {
	var reply struct {
		Permissions map[string]bool `json:"permissions"`
	}
	if err := e.call(ctx, "permissionState", map[string]interface{}{"appId": appID}, &reply); err != nil {
		return nil, err
	}
	return reply.Permissions, nil
}

// Healthy reports whether the driver still answers. A driver whose process has
// gone is not healthy whatever it advertised, which is what lets a cached
// session notice a crashed driver instead of failing every later command.
func (e *External) Healthy(ctx context.Context) bool {
	select {
	case <-e.exited:
		return false
	default:
	}
	if !e.caps[CapHealth] {
		return true
	}
	var reply struct {
		Healthy bool `json:"healthy"`
	}
	if err := e.call(ctx, "health", nil, &reply); err != nil {
		return false
	}
	return reply.Healthy
}
