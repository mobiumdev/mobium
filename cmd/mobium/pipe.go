package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"

	"github.com/mobiumdev/mobium/internal/agent"
	"github.com/spf13/cobra"
)

func newPipeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pipe",
		Short: "Proxy JSON-RPC between stdin/stdout and the daemon",
		Long: "The transport the language clients use.\n\n" +
			"Unlike `mobium mcp`, which runs its own device session in-process, this\n" +
			"forwards to the shared daemon. That matters: a device-side server holds one\n" +
			"session at a time, so a client that started its own would silently invalidate\n" +
			"the session the CLI is using, and vice versa. Going through the daemon means\n" +
			"a script and a terminal drive the same session and see the same refs.",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := newPipe(daemonCall)
			// The sessions this client started end with it, unless it said
			// it was leaving them open. A client that crashes, or a script
			// that returns without quit(), closes stdin the same way a
			// deliberate close() does, so close() says so first.
			defer p.release()

			// Ctrl-C reaches the whole process group, this process included,
			// and the client may be handling it — a `with` block or a
			// finally that calls quit() needs this pipe alive to send it.
			// The client's exit closes stdin, and that is the signal to act
			// on. A terminal closing, or a kill, does not come back.
			signal.Ignore(os.Interrupt)
			stop := make(chan os.Signal, 1)
			signal.Notify(stop, syscall.SIGTERM, syscall.SIGHUP)
			go func() {
				<-stop
				p.release()
				os.Exit(1)
			}()

			// Reader, not Scanner: a base64 screenshot easily exceeds a
			// Scanner's fixed buffer, and raising the cap only moves the wall.
			reader := bufio.NewReader(os.Stdin)
			out := bufio.NewWriter(os.Stdout)
			// The deferred flush is the last thing to run on the way out,
			// when stdin has already closed and there is nobody left to tell.
			defer func() { _ = out.Flush() }()

			for {
				line, err := reader.ReadBytes('\n')
				if err != nil && len(line) == 0 {
					return nil // stdin closed; the client is done
				}
				if len(line) <= 1 {
					continue
				}

				resp := p.handle(line)
				if resp == nil {
					continue // a notification takes no reply
				}
				data, mErr := json.Marshal(resp)
				if mErr != nil {
					continue
				}
				fmt.Fprintf(out, "%s\n", data)
				// A failed flush means the reply is sitting in a buffer the
				// client will never see, and a client that asked for a result
				// waits forever. Stop rather than keep answering into a pipe
				// that is not there.
				if err := out.Flush(); err != nil {
					return fmt.Errorf("write to stdout: %w", err)
				}
			}
		},
	}
}

// DetachMethod is the notification a client sends before a deliberate
// close(), to leave the sessions it started open. Without it, the pipe ends
// them when the client goes away.
const DetachMethod = "mobium/detach"

// pipe is one client's connection: the calls it forwards, and the sessions
// it started and so answers for.
type pipe struct {
	call func(tool string, args map[string]interface{}) (*agent.ToolsCallResult, error)

	mu       sync.Mutex
	owned    map[string]bool // devices whose session this client started
	detached bool
	released bool
}

func newPipe(call func(string, map[string]interface{}) (*agent.ToolsCallResult, error)) *pipe {
	return &pipe{call: call, owned: map[string]bool{}}
}

// release ends every session this client started and has not ended or left
// open, once. A session it found already open is not its to end: start
// reports that as reused, and another client, or the CLI, is using it.
func (p *pipe) release() {
	p.mu.Lock()
	if p.released || p.detached {
		p.released = true
		p.mu.Unlock()
		return
	}
	p.released = true
	var devices []string
	for d := range p.owned {
		devices = append(devices, d)
	}
	p.mu.Unlock()
	sort.Strings(devices)
	for _, d := range devices {
		if _, err := p.call("app_session", map[string]interface{}{"action": "end", "device": d}); err != nil {
			fmt.Fprintf(os.Stderr, "mobium pipe: the client went away and its session on %s could not be ended: %v\n", d, err)
		}
	}
}

// track follows app_session through a call, so the pipe knows which sessions
// are the client's own.
func (p *pipe) track(name string, result *agent.ToolsCallResult) {
	if name != "app_session" || result == nil || result.IsError {
		return
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return
	}
	var view agent.SessionView
	if json.Unmarshal(data, &view) != nil || view.Device == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch view.Action {
	case "start":
		if !view.Reused {
			p.owned[view.Device] = true
		}
	case "end":
		delete(p.owned, view.Device)
	}
}

// handle answers one client message, forwarding tool calls to the daemon and
// answering the handshake locally.
func (p *pipe) handle(line []byte) *agent.Response {
	var req agent.Request
	if err := json.Unmarshal(line, &req); err != nil {
		return &agent.Response{JSONRPC: "2.0", Error: &agent.Error{
			Code: agent.ParseError, Message: "Parse error", Data: err.Error(),
		}}
	}
	if req.ID == nil {
		if req.Method == DetachMethod {
			p.mu.Lock()
			p.detached = true
			p.mu.Unlock()
		}
		return nil
	}

	reply := func(result interface{}) *agent.Response {
		return &agent.Response{JSONRPC: "2.0", ID: req.ID, Result: result}
	}

	switch req.Method {
	case "initialize":
		return reply(agent.InitializeResult{
			ProtocolVersion: agent.ProtocolVersion,
			Capabilities:    agent.ServerCapabilities{Tools: &agent.ToolsCapability{}},
			ServerInfo:      agent.ServerInfo{Name: "mobium", Version: version},
		})
	case "tools/list":
		return reply(agent.ToolsListResult{Tools: agent.GetToolSchemas()})
	case "tools/call":
		var params agent.ToolsCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &agent.Response{JSONRPC: "2.0", ID: req.ID, Error: &agent.Error{
				Code: agent.InvalidParams, Message: "Invalid params", Data: err.Error(),
			}}
		}
		result, err := p.call(params.Name, orEmpty(params.Arguments))
		p.track(params.Name, result)
		if err != nil {
			// A tool that ran and failed is a result, not a protocol error:
			// the client needs to read why, and its code to raise the right
			// exception.
			return reply(agent.ErrorResult(err))
		}
		return reply(result)
	default:
		return &agent.Response{JSONRPC: "2.0", ID: req.ID, Error: &agent.Error{
			Code: agent.MethodNotFound, Message: "Method not found", Data: req.Method,
		}}
	}
}

func orEmpty(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return map[string]interface{}{}
	}
	return m
}
