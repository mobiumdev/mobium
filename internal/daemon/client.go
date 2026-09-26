package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"net"
	"time"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/paths"
)

// Vars, not consts, so tests can shrink them.
var (
	dialTimeout = 2 * time.Second
	readTimeout = 120 * time.Second

	// progressGrace is the new read deadline set each time the daemon reports
	// progress. Sized for the slowest single step, downloading the server APK.
	progressGrace = 5 * time.Minute
)

// OnProgress is called, when set, for each progress notification the daemon
// sends during a call. The CLI uses it to explain a long first run.
var OnProgress func(string)

// ToolError is an error the daemon itself reported. It means the daemon was
// reached and answered, so a caller must not mistake it for the daemon being
// down and try to start another one — "no devices attached" reads a lot like
// an unreachable socket if you only look at the text.
//
// It carries the classified error the daemon sent, when it sent one, so the
// CLI can choose an exit status and --json can print the code.
type ToolError struct {
	Msg string
	Err *mobiumerr.Error
}

func (e *ToolError) Error() string { return e.Msg }

// Unwrap exposes the classified error to mobiumerr.CodeOf and errors.As.
func (e *ToolError) Unwrap() error {
	if e.Err == nil {
		return nil
	}
	return e.Err
}

// IsConnectionError reports whether the daemon could not be reached at all.
func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	return errors.Is(err, io.EOF)
}

// Call sends a tools/call request to the daemon.
func Call(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
	params, err := json.Marshal(agent.ToolsCallParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, err
	}
	resp, err := sendRequest("tools/call", params)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		// A protocol error: the request was malformed or named no tool.
		code := mobiumerr.Internal
		if resp.Error.Code == agent.InvalidParams || resp.Error.Code == agent.MethodNotFound {
			code = mobiumerr.InvalidArgument
		}
		return nil, &ToolError{Msg: resp.Error.Message, Err: mobiumerr.New(code, "%s", resp.Error.Message)}
	}

	resultJSON, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}
	var result agent.ToolsCallResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		return nil, err
	}
	// A tool that failed answers with isError, not a JSON-RPC error.
	if result.IsError {
		msg := "tool call failed"
		if len(result.Content) > 0 {
			msg = result.Content[0].Text
		}
		te := &ToolError{Msg: msg}
		// The structured half arrives as a generic map after the round trip
		// through JSON; decode it into the payload it was.
		if result.StructuredContent != nil {
			if b, err := json.Marshal(result.StructuredContent); err == nil {
				var p mobiumerr.Payload
				if json.Unmarshal(b, &p) == nil && p.Code != "" {
					te.Err = mobiumerr.FromPayload(p)
				}
			}
		}
		return nil, te
	}
	return &result, nil
}

// Status asks a running daemon about itself.
func Status() (*StatusResult, error) {
	resp, err := sendRequest("daemon/status", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, &ToolError{Msg: resp.Error.Message}
	}
	resultJSON, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}
	var result StatusResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Shutdown asks a running daemon to exit.
func Shutdown() error {
	// Note the PID before asking, because the daemon removes its own PID file
	// on the way out and there would be nothing left to wait on.
	pid, _ := ReadPID()

	resp, err := sendRequest("daemon/shutdown", nil)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return &ToolError{Msg: resp.Error.Message}
	}

	// Wait for it to actually be gone. The reply is sent before teardown
	// finishes — closing a UiAutomator2 session takes about a second — and
	// returning early made `mobium daemon stop && mobium <anything>` fail
	// every time: the next command's auto-start found a daemon still holding
	// the socket, gave up, and then waited ten seconds for a daemon that was
	// never going to appear. Reproduced deterministically; one second of sleep
	// in between was enough to hide it.
	if pid > 0 {
		waitGone(pid, shutdownGrace)
	}
	CleanStale()
	return nil
}

// shutdownGrace bounds the wait for the daemon process to exit. Generous
// against the second or so teardown really takes, because overrunning it only
// costs the next command a retry, while cutting it short reintroduces the bug.
const shutdownGrace = 5 * time.Second

// waitGone polls until the process exits or the deadline passes. It reports
// whether the process actually went away, though callers currently proceed
// either way: a daemon that will not die is better reported by the next
// command failing to reach it than by refusing to return from `stop`.
func waitGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !Running(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !Running(pid)
}

func sendRequest(method string, params json.RawMessage) (*agent.Response, error) {
	socketPath, err := paths.SocketPath()
	if err != nil {
		return nil, err
	}
	conn, err := dial(socketPath, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	req := agent.Request{JSONRPC: "2.0", ID: 1, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(conn, "%s\n", data); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	conn.SetReadDeadline(time.Now().Add(readTimeout))
	// bufio.Reader grows as needed. A Scanner fails with "token too long" on
	// a long screen's text or a base64 screenshot, and raising its cap only
	// moves the ceiling.
	reader := bufio.NewReader(conn)
	var line []byte
	for {
		line, err = reader.ReadBytes('\n')
		if err != nil && len(line) == 0 {
			if err == io.EOF {
				return nil, mobiumerr.New(mobiumerr.Internal, "daemon closed the connection without responding")
			}
			return nil, fmt.Errorf("read response: %w", err)
		}
		if err != nil {
			break // partial final line; let the parse below report it
		}

		// The daemon may send progress notifications ahead of the response.
		// Each one means it is still working, so the deadline is pushed out:
		// downloading an 18MB server APK on a slow link legitimately outlasts
		// the plain read timeout, and cutting it off would look like a hang
		// the user caused.
		var msg struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
			Params struct {
				Message string `json:"message"`
			} `json:"params"`
		}
		if json.Unmarshal(line, &msg) == nil && msg.Method != "" && len(msg.ID) == 0 {
			if msg.Method == progressMethod {
				conn.SetReadDeadline(time.Now().Add(progressGrace))
				if OnProgress != nil {
					OnProgress(msg.Params.Message)
				}
			}
			continue
		}
		break
	}

	var resp agent.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &resp, nil
}
