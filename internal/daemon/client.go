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
	return CallMeta(tool, args, nil)
}

// CallMeta is Call with the request's _meta passed through.
func CallMeta(tool string, args, meta map[string]interface{}) (*agent.ToolsCallResult, error) {
	params, err := json.Marshal(agent.ToolsCallParams{Name: tool, Arguments: args, Meta: meta})
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
	// The PID came from the file, so the file named this daemon: its going
	// is a sign the teardown finished.
	if pid > 0 && !waitGone(pid, shutdownGrace, true) {
		// Saying "stopped" here was reporting what was asked for, not what
		// happened: the daemon was still closing sessions, and the next
		// command either reached it or raced its exit.
		return mobiumerr.New(mobiumerr.Timeout,
			"the daemon (pid %d) acknowledged the stop and is still running %s later — it is closing device sessions "+
				"and will exit when that finishes or times out. Run `mobium daemon stop` again to wait longer; "+
				"docs/SHUTDOWN.md says how to clear what a session left on the device", pid, shutdownGrace)
	}
	CleanStale()
	return nil
}

// shutdownGrace bounds the wait for the daemon process to exit. It covers
// the daemon's own worst case — ten seconds for calls in flight, then
// closeTimeout for the sessions — plus a margin, because a stop that returns
// while the daemon is still tearing down reintroduces the bug above. At 5s
// it did exactly that whenever a session took longer than usual to close.
const shutdownGrace = 90 * time.Second

// waitGone polls until the daemon has finished, or the deadline passes, and
// reports which. Finished means the process has exited, or — when pid was
// read from the PID file, so the file named this daemon — the file no longer
// names it: removing it is the last thing Shutdown does, once every session
// is closed, so what is left is the process returning. A daemon run inside
// another process, as the tests run it, only ever does the second.
//
// fromFile is the caller's to say, not something to read here: a teardown
// quick enough to remove the file before the first look made it read as
// never having named the daemon, and the wait ran to its deadline — on
// Linux CI, every run.
func waitGone(pid int, timeout time.Duration, fromFile bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !Running(pid) {
			return true
		}
		if current, err := ReadPID(); fromFile && err == nil && current != pid {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// WaitStopping waits out a daemon on its way down: the PID file names a
// process still alive that no longer answers. A daemon told to stop closes
// its socket and exits about 0.1s later; a command in that gap found no
// daemon and started one, which refused — one is already running — and
// exited, and the command then waited ten seconds for an answer nothing
// would give. Nine in ten after a pkill. CHALLENGES 279.
func WaitStopping() {
	pid, err := ReadPID()
	if err != nil || pid == 0 || !Running(pid) {
		return
	}
	// Not shutdownGrace: a PID file can name a process that reused the
	// number, which never goes, and this wait must cost no more than the
	// ten seconds a start already had.
	waitGone(pid, stoppingWait, true)
	CleanStale()
}

const stoppingWait = 10 * time.Second

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
