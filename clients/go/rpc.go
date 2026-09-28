package mobium

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Error is a tool reporting that it could not do what was asked.
//
// A failing tool answers with isError and its reason in the content rather
// than with a JSON-RPC error, so the reason has to be lifted out deliberately;
// this is what that becomes. Code says what kind of failure it was — compare
// with errors.Is and the Err sentinels in errors.go — Remedy what to do about
// it, and Retryable whether the same call can succeed if made again.
type Error struct {
	Tool      string
	Reason    string
	Code      Code
	Remedy    string
	Retryable bool
	Details   map[string]any
}

func (e *Error) Error() string {
	if e.Tool == "" {
		return e.Reason
	}
	return e.Tool + ": " + e.Reason
}

// FindBinary locates the mobium executable: the explicit path, then
// MOBIUM_BIN_PATH, then PATH — and nothing else.
//
// MOBIUM_BIN_PATH wins, so a test run can pin a specific build — the same
// escape hatch the other clients have. The current directory is never
// searched, not even through a relative PATH entry: a library that runs
// whatever ./bin/mobium happens to sit where a test was started runs a
// binary anyone could have planted there. exec.LookPath already refuses a
// result from a relative PATH entry (exec.ErrDot).
func FindBinary(explicit string) (string, error) {
	for _, candidate := range []string{explicit, os.Getenv("MOBIUM_BIN_PATH")} {
		if candidate == "" {
			continue
		}
		if executable(candidate) {
			return candidate, nil
		}
		return "", fmt.Errorf("%s is not an executable mobium binary", candidate)
	}
	// PATH's directories one by one, rather than exec.LookPath("mobium"):
	// on Windows that looks in the current directory first, and on finding
	// a mobium.exe there refuses it and gives up, so the real one on PATH
	// was never reached. A relative entry would be the current directory
	// by another name, and is skipped too. CHALLENGES 142.
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if found, err := exec.LookPath(filepath.Join(dir, "mobium")); err == nil {
			return found, nil
		}
	}
	return "", errors.New("mobium not found — put it on PATH or set MOBIUM_BIN_PATH to the binary")
}

func executable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		// Windows has no executable bit, and os.Stat reports none, so every
		// mobium.exe was refused. A program there is known by its
		// extension, from PATHEXT as the shell reads it. CHALLENGES 141.
		exts := os.Getenv("PATHEXT")
		if exts == "" {
			exts = ".COM;.EXE;.BAT;.CMD"
		}
		ext := filepath.Ext(path)
		for _, e := range strings.Split(exts, ";") {
			if e != "" && strings.EqualFold(e, ext) {
				return true
			}
		}
		return false
	}
	return info.Mode()&0o111 != 0
}

// conn is a live `mobium pipe` subprocess.
//
// pipe rather than mcp, and this is not a detail: a device-side server holds
// one session per device, so a client that started its own would invalidate
// the CLI's and the CLI's would invalidate this one. pipe forwards to the
// shared daemon instead.
type conn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	// mu serializes whole request/response pairs. The transport is one pipe
	// with replies identified only by id, so two calls in flight would race
	// to read each other's answer.
	mu     sync.Mutex
	nextID int

	// writeMu keeps one message whole on the pipe. Close writes a detach
	// while a call may hold mu, and must not wait for it: Close is how a
	// caller gives up on a call that hangs.
	writeMu sync.Mutex

	// dead records why the connection can no longer be used, once something
	// has made that true. Guarded separately from mu because it is read by
	// the next caller while the previous one may still hold mu.
	deadMu sync.Mutex
	dead   error
}

func (c *conn) kill(cause error) {
	c.deadMu.Lock()
	if c.dead == nil {
		c.dead = cause
	}
	c.deadMu.Unlock()
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

func (c *conn) why() error {
	c.deadMu.Lock()
	defer c.deadMu.Unlock()
	return c.dead
}

func dial(binary string, args []string, session string) (*conn, error) {
	cmd := exec.Command(binary, append([]string{"pipe"}, args...)...)
	if session != "" {
		// Appended, so it wins over a MOBIUM_SESSION already in the
		// environment: the option is the more specific of the two.
		cmd.Env = append(os.Environ(), "MOBIUM_SESSION="+session)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// Progress notes about downloading a device-side server go to stderr.
	// Passing them through keeps a slow first run explicable instead of
	// looking like a hang.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mobium: %w", err)
	}
	c := &conn{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}
	if err := c.initialize(context.Background()); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *conn) initialize(ctx context.Context) error {
	_, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "mobium-go", "version": "0.1.0"},
	})
	if err != nil {
		return err
	}
	return c.write(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
}

func (c *conn) write(payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("mobium closed the connection: %w", err)
	}
	return nil
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// rpcDetail is an error's data as ": detail", or nothing.
func rpcDetail(e *rpcError) string {
	var detail string
	if len(e.Data) > 0 && json.Unmarshal(e.Data, &detail) == nil && detail != "" {
		return ": " + detail
	}
	return ""
}

type rpcResponse struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func (c *conn) request(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	if err := c.why(); err != nil {
		return nil, fmt.Errorf("this mobium connection is no longer usable: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.nextID++
	id := c.nextID
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	if err := c.write(payload); err != nil {
		return nil, err
	}

	type reply struct {
		raw json.RawMessage
		err error
	}
	answered := make(chan reply, 1)
	go func() {
		for {
			line, err := c.stdout.ReadBytes('\n')
			if err != nil {
				answered <- reply{err: fmt.Errorf(
					"mobium closed the connection without responding to %s", method)}
				return
			}
			var resp rpcResponse
			if err := json.Unmarshal(line, &resp); err != nil {
				continue // not a message we can read; keep looking
			}
			if resp.ID == nil && resp.Error != nil {
				// An error with no id is mobium saying it could not read a
				// request at all, which JSON-RPC answers without an id. The
				// pipe answers one request at a time, in order, and mu keeps
				// one in flight, so it is this call's answer: skipping it, as
				// a notification is skipped, left the call waiting forever.
				answered <- reply{err: &Error{
					Reason: "mobium could not read the request: " + resp.Error.Message + rpcDetail(resp.Error),
					Code:   CodeInvalidArgument,
				}}
				return
			}
			if resp.ID == nil || *resp.ID != id {
				continue // a notification, or a reply to something else
			}
			if resp.Error != nil {
				// A protocol error: the request itself was refused.
				answered <- reply{err: &Error{Reason: resp.Error.Message + rpcDetail(resp.Error), Code: CodeInvalidArgument}}
				return
			}
			answered <- reply{raw: resp.Result}
			return
		}
	}()

	select {
	case r := <-answered:
		return r.raw, r.err
	case <-ctx.Done():
		// There is one pipe and replies are told apart only by id, so a call
		// abandoned half-way cannot be resynchronized: the answer to it would
		// be read as the answer to the next one. Canceling therefore ends
		// the connection rather than leaving it subtly wrong, and the next
		// call says so plainly. Give the session a generous context, or none.
		c.kill(ctx.Err())
		return nil, ctx.Err()
	}
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Data string `json:"data"`
}

type toolResult struct {
	Content           []content       `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

// call runs a tool, turning a tool-level failure into an error.
func (c *conn) call(ctx context.Context, name string, args map[string]any) (*toolResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	raw, err := c.request(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, err
	}
	var result toolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%s returned something unreadable: %w", name, err)
	}
	if result.IsError {
		return nil, toolError(name, result.text(), result.StructuredContent)
	}
	return &result, nil
}

func (r *toolResult) text() string {
	var parts []string
	for _, c := range r.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// into decodes the tool's structured answer. A tool that returned only prose
// leaves the target untouched rather than failing: the caller asked for a
// shape the tool does not produce, and an empty result says that best.
func (r *toolResult) into(target any) error {
	if len(r.StructuredContent) == 0 {
		return nil
	}
	return json.Unmarshal(r.StructuredContent, target)
}

// closeGrace is how long Close waits for mobium to exit on its own after its
// stdin is closed, before killing it.
const closeGrace = 10 * time.Second

// Close shuts the subprocess down, closing its stdin so it can exit cleanly
// and killing it only if it will not.
//
// It tells mobium first that the client is leaving on purpose. mobium ends
// the sessions a client started when the client goes away — a crash closes
// stdin just as this does — so without the detach, Close would quit.
func (c *conn) Close() error {
	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}
	_ = c.write(map[string]any{"jsonrpc": "2.0", "method": "mobium/detach"})
	_ = c.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(closeGrace):
		_ = c.cmd.Process.Kill()
		<-done
		return nil
	}
}
