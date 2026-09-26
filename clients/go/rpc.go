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

// FindBinary locates the mobium executable.
//
// MOBIUM_BIN_PATH wins, so a test run can pin a specific build — the same
// escape hatch the other clients have.
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
	if found, err := exec.LookPath("mobium"); err == nil {
		return found, nil
	}
	for _, rel := range []string{"./bin/mobium", "../bin/mobium", "../../bin/mobium"} {
		if executable(rel) {
			return filepath.Abs(rel)
		}
	}
	return "", errors.New("mobium not found — put it on PATH or set MOBIUM_BIN_PATH to the binary")
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
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

func dial(binary string, args []string) (*conn, error) {
	cmd := exec.Command(binary, append([]string{"pipe"}, args...)...)
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
			if resp.ID == nil || *resp.ID != id {
				continue // a notification, or a reply to something else
			}
			if resp.Error != nil {
				msg := resp.Error.Message
				if len(resp.Error.Data) > 0 {
					var detail string
					if json.Unmarshal(resp.Error.Data, &detail) == nil && detail != "" {
						msg += ": " + detail
					}
				}
				// A protocol error: the request itself was refused.
				answered <- reply{err: &Error{Reason: msg, Code: CodeInvalidArgument}}
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
		// abandoned half-way cannot be resynchronised: the answer to it would
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
func (c *conn) Close() error {
	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}
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
