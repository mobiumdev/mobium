package testrun

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
)

// A project's own connection, for a grid. A grid route is chosen once per
// mobium process and held for as long as it lives — the lease's holder, the
// device, the forward to its node are all the process's — so a run that
// drives several projects through a grid gives each its own `mobium pipe`,
// exactly as each client does. Its first call says what the project wants,
// the grid leases a device for it, and closing the pipe releases it.

// Pipe is a `mobium pipe` process spoken to over JSON-RPC on stdio.
type Pipe struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	out    *bufio.Reader
	mu     sync.Mutex
	nextID int
}

// DialPipe starts `<binary> pipe`, with env added to this process's, and
// completes the handshake.
func DialPipe(binary string, env ...string) (*Pipe, error) {
	cmd := exec.Command(binary, "pipe")
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// Progress notes — a server installing, a run waiting for the grid —
	// go to stderr, and pass through.
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, mobiumerr.New(mobiumerr.Internal, "could not start %s pipe: %v", binary, err)
	}
	p := &Pipe{cmd: cmd, stdin: stdin, out: bufio.NewReaderSize(stdout, 1<<20)}
	if _, err := p.request("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "mobium-test", "version": "0.1.0"},
	}); err != nil {
		p.Close()
		return nil, err
	}
	if err := p.write(map[string]interface{}{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func (p *Pipe) write(v map[string]interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

// request sends one request and reads until its answer; notifications in
// between — progress — are skipped.
func (p *Pipe) request(method string, params map[string]interface{}) (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	id := p.nextID
	if err := p.write(map[string]interface{}{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, mobiumerr.New(mobiumerr.Internal, "mobium pipe closed: %v", err)
	}
	for {
		line, err := p.out.ReadBytes('\n')
		if err != nil {
			return nil, mobiumerr.New(mobiumerr.Internal, "mobium pipe ended before answering %s: %v", method, err)
		}
		var resp struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(line, &resp) != nil || resp.ID == nil || *resp.ID != id {
			continue
		}
		if resp.Error != nil {
			code := mobiumerr.Internal
			if resp.Error.Code == agent.InvalidParams || resp.Error.Code == agent.MethodNotFound {
				code = mobiumerr.InvalidArgument
			}
			return nil, mobiumerr.New(code, "%s", resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// Call is a Caller over the pipe: a tool's answer, or its failure as the
// coded error it was.
func (p *Pipe) Call(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error) {
	raw, err := p.request("tools/call", map[string]interface{}{"name": tool, "arguments": args})
	if err != nil {
		return nil, err
	}
	var res agent.ToolsCallResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, mobiumerr.New(mobiumerr.Internal, "%s answered something unreadable: %v", tool, err)
	}
	if !res.IsError {
		return &res, nil
	}
	msg := fmt.Sprintf("%s failed", tool)
	if len(res.Content) > 0 {
		msg = res.Content[0].Text
	}
	if b, err := json.Marshal(res.StructuredContent); err == nil {
		var pl mobiumerr.Payload
		if json.Unmarshal(b, &pl) == nil && pl.Code != "" {
			return nil, mobiumerr.FromPayload(pl)
		}
	}
	return nil, mobiumerr.New(mobiumerr.Unclassified, "%s", msg)
}

// pipeCloseGrace bounds a pipe's goodbye: it ends its session and releases
// its lease on the way out.
const pipeCloseGrace = 30 * time.Second

// Close ends the connection and waits for the pipe to finish releasing what
// it held. Not with mobium/detach, which tells a pipe to leave its sessions
// open: closed without it, the pipe ends the session its first call started,
// and the session's end puts back what a test changed — the network,
// accessibility settings. With detach, a run left 300ms of added latency on
// the emulator after it finished, measured.
func (p *Pipe) Close() {
	_ = p.stdin.Close()
	done := make(chan struct{})
	go func() { _ = p.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(pipeCloseGrace):
		_ = p.cmd.Process.Kill()
		<-done
	}
}
