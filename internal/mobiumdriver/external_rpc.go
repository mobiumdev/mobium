package mobiumdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"strings"
	"sync"
)

// maxReplyBytes caps one line from a driver. A hierarchy from a real screen is
// tens of kilobytes and a base64 screenshot is a few megabytes, so the limit is
// generous — but unbounded would let a driver in a loop exhaust memory in the
// daemon, which is holding somebody's device session.
const maxReplyBytes = 64 << 20

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

// call sends one request and waits for the matching reply.
//
// Anything the driver writes that is not the reply to the request in flight is
// discarded with the reason recorded: a notification, a reply to an id that is
// no longer outstanding, or — much the most likely — a line of debug output
// somebody wrote to stdout instead of stderr. Treating that as fatal would make
// the protocol miserable to develop against; treating it as the reply would be
// worse.
// call sends one request and waits for the matching reply.
//
// Anything the driver writes that is not the reply to the request in flight is
// discarded with the reason recorded: a notification, a reply to an id that is
// no longer outstanding, or — much the most likely — a line of debug output
// somebody wrote to stdout instead of stderr. Treating that as fatal would make
// the protocol miserable to develop against; treating it as the reply would be
// worse.
func (e *External) call(ctx context.Context, method string, params interface{}, out interface{}) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cmd == nil {
		return mobiumerr.New(mobiumerr.Internal, "driver %s was not started", e.name)
	}
	select {
	case <-e.exited:
		return e.deadError(method)
	default:
	}

	e.nextID++
	id := e.nextID

	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("could not encode %s for driver %s: %w", method, e.Name(), err)
	}
	if _, err := e.stdin.Write(append(body, '\n')); err != nil {
		return e.deadError(method)
	}

	// Replies arrive on a channel fed by the one reader goroutine started with
	// the process. An earlier version read on a goroutine per call, which
	// leaked one on every canceled context and then had two goroutines
	// reading the same pipe — the second call would get half a message.
	for {
		select {
		case <-ctx.Done():
			return mobiumerr.New(mobiumerr.Timeout, "driver %s did not answer %s in time: %w", e.Name(), method, ctx.Err())
		case <-e.exited:
			return e.deadError(method)
		case line, ok := <-e.lines:
			if !ok {
				return e.deadError(method)
			}
			resp, matched := e.match(line, id)
			if !matched {
				continue
			}
			return e.finish(method, resp, out)
		}
	}
}

// match decodes one line and reports whether it answers the request in flight.
func (e *External) match(line []byte, id int) (*rpcResponse, bool) {
	if len(strings.TrimSpace(string(line))) == 0 {
		return nil, false
	}
	var resp rpcResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		// Not JSON at all: almost certainly a debug print on stdout.
		e.noteStray(line)
		return nil, false
	}
	if resp.ID == nil || *resp.ID != id {
		e.noteStray(line)
		return nil, false
	}
	return &resp, true
}

// finish turns a matched reply into a result or an error.
func (e *External) finish(method string, resp *rpcResponse, out interface{}) error {
	if resp.Error != nil {
		// The driver's own words, shown to the user. Prefixed with the driver
		// name so a confusing message is attributable.
		return mobiumerr.New(mobiumerr.DeviceServer, "%s: %s", e.Name(), resp.Error.Message)
	}
	if out == nil {
		return nil
	}
	if len(resp.Result) == 0 {
		return mobiumerr.New(mobiumerr.DeviceServer, "driver %s answered %s with no result", e.Name(), method)
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("driver %s answered %s with something unexpected: %w", e.Name(), method, err)
	}
	return nil
}

// readLoop is the only reader of the driver's stdout, for the life of the
// process. It ends when the pipe closes, which happens when the driver exits.
func (e *External) readLoop() {
	defer close(e.lines)
	for {
		line, err := e.readLine()
		if err != nil {
			if err != io.EOF {
				e.readLoopErr.Store(&err)
			}
			return
		}
		// A copy: readLine reuses nothing today, but the channel outlives the
		// call that reads from it and a future bufio change should not become
		// a data race here.
		buf := append([]byte(nil), line...)
		select {
		case e.lines <- buf:
		case <-e.exited:
			return
		}
	}
}

// readLine reads one newline-terminated message, refusing an unbounded one.
func (e *External) readLine() ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := e.stdout.ReadLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk...)
		if len(buf) > maxReplyBytes {
			return nil, mobiumerr.New(mobiumerr.DeviceServer, "a single message exceeded %d bytes", maxReplyBytes)
		}
		if !isPrefix {
			return buf, nil
		}
	}
}

// noteStray records output that was not the reply, so it can be quoted if the
// call goes on to fail. A driver printing to stdout is the commonest mistake in
// a stdio protocol, and the symptom without this is a timeout with no cause.
func (e *External) noteStray(line []byte) {
	s := strings.TrimSpace(string(line))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	e.strayMu.Lock()
	if len(e.strays) < 5 {
		e.strays = append(e.strays, s)
	}
	e.strayMu.Unlock()
}

// deadError explains that the process is gone, quoting its log.
func (e *External) deadError(method string) error {
	<-e.exited
	reason := "it exited"
	if e.waitErr != nil {
		reason = e.waitErr.Error()
	}
	if p := e.readLoopErr.Load(); p != nil {
		// A read failure that is not EOF says more than the exit status: an
		// oversized message, say, which the exit code would not mention.
		reason = fmt.Sprintf("%s; reading from it failed with: %v", reason, *p)
	}
	return mobiumerr.New(mobiumerr.DeviceServer, "driver %s is no longer running (%s), so %s could not be sent%s",
		e.Name(), reason, method, e.logTail())
}

// logTail quotes what the driver said on stderr, and any stray stdout, so a
// failure names its cause instead of only its symptom.
func (e *External) logTail() string {
	var parts []string
	if e.log != nil {
		if s := strings.TrimSpace(e.log.String()); s != "" {
			parts = append(parts, "the driver's log says:\n"+indent(s))
		}
	}
	e.strayMu.Lock()
	strays := append([]string(nil), e.strays...)
	e.strayMu.Unlock()
	if len(strays) > 0 {
		parts = append(parts, "it also wrote this to stdout, where only JSON-RPC belongs "+
			"(debug output goes to stderr):\n"+indent(strings.Join(strays, "\n")))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(parts, "\n\n")
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}

// tailBuffer keeps the last n bytes written to it. A driver that logs
// enthusiastically should not be able to grow the daemon's memory, and it is
// the end of the log that says why something failed.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newTailBuffer(max int) *tailBuffer { return &tailBuffer{max: max} }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
