package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/mobiumdev/mobium/internal/mobiumerr"
	"io"
	"os"
	"time"
)

// Server speaks MCP over stdio. It shares its Handlers with nothing else, so
// an editor's MCP client gets its own tool state without contending with the
// CLI's daemon.
type Server struct {
	reader   *bufio.Reader
	writer   io.Writer
	handlers *Handlers
	version  string

	// busy holds one slot while a request is handled, and is taken by Close
	// so teardown never runs under an in-flight call. A channel rather than a
	// mutex so Close can bound its wait.
	busy chan struct{}
}

// NewServer creates an MCP server reading stdin and writing stdout.
func NewServer(version string) *Server {
	return &Server{
		reader:   bufio.NewReader(os.Stdin),
		writer:   os.Stdout,
		handlers: NewHandlers(),
		version:  version,
		busy:     make(chan struct{}, 1),
	}
}

// SetDefaultDevice pins tool calls that do not name a device.
func (s *Server) SetDefaultDevice(serial string) { s.handlers.SetDefaultDevice(serial) }

// Run reads requests until stdin closes.
func (s *Server) Run() error {
	for {
		// bufio.Reader grows as needed. A Scanner would fail with "token too
		// long" on a large request, and raising its cap only moves the wall.
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read error: %w", err)
		}
		if len(line) <= 1 {
			continue
		}

		s.busy <- struct{}{}
		resp := s.handleRequest(line)
		<-s.busy

		if resp != nil {
			if err := s.writeResponse(resp); err != nil {
				return fmt.Errorf("write error: %w", err)
			}
		}
	}
}

// HandleRequest parses and routes one JSON-RPC message, returning nil for a
// notification. Exported for tests that drive the protocol without stdio.
func (s *Server) HandleRequest(data []byte) *Response {
	return s.handleRequest(data)
}

func (s *Server) handleRequest(data []byte) *Response {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return &Response{JSONRPC: "2.0", Error: &Error{
			Code: ParseError, Message: "Parse error", Data: err.Error(),
		}}
	}
	if req.JSONRPC != "2.0" {
		return &Response{JSONRPC: "2.0", ID: req.ID, Error: &Error{
			Code: InvalidRequest, Message: "Invalid Request", Data: "jsonrpc must be '2.0'",
		}}
	}

	result, rpcErr := Route(req, s.handlers, s.version, nil)

	// Notifications get no response, even on error.
	if req.ID == nil {
		return nil
	}
	if rpcErr != nil {
		return &Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
	}
	return &Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) writeResponse(resp *Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.writer, "%s\n", data)
	return err
}

// closeGrace bounds how long Close waits for an in-flight request.
const closeGrace = 2 * time.Second

// Close releases handler resources, waiting briefly for an in-flight call.
func (s *Server) Close() {
	select {
	case s.busy <- struct{}{}:
		defer func() { <-s.busy }()
	case <-time.After(closeGrace):
	}
	s.handlers.Close()
}

// Route dispatches one JSON-RPC method against a set of handlers.
//
// The MCP server and the daemon share this so the two front doors cannot
// answer initialize, tools/list or tools/call differently. extra handles
// methods only one of them serves (the daemon's daemon/status and
// daemon/shutdown); it may be nil.
func Route(req Request, h *Handlers, version string,
	extra func(Request) (interface{}, *Error, bool)) (interface{}, *Error) {

	if extra != nil {
		if result, err, handled := extra(req); handled {
			return result, err
		}
	}

	switch req.Method {
	case "initialize":
		return InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities:    ServerCapabilities{Tools: &ToolsCapability{}},
			ServerInfo:      ServerInfo{Name: "mobium", Version: version},
		}, nil
	case "initialized", "notifications/initialized":
		return nil, nil
	case "tools/list":
		return ToolsListResult{Tools: GetToolSchemas()}, nil
	case "tools/call":
		return CallTool(req.Params, h)
	default:
		return nil, &Error{Code: MethodNotFound, Message: "Method not found", Data: req.Method}
	}
}

// CallTool executes a tools/call request.
//
// A tool that fails returns a result with isError set, not a JSON-RPC error:
// the call itself succeeded, and the agent needs to read why the tool did not.
// A protocol-level error is reserved for a malformed request.
func CallTool(params json.RawMessage, h *Handlers) (interface{}, *Error) {
	var p ToolsCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &Error{Code: InvalidParams, Message: "Invalid params", Data: err.Error()}
	}
	result, err := h.Call(p.Name, p.Arguments)
	if err != nil {
		return ErrorResult(err), nil
	}
	return result, nil
}

// ErrorResult is a failed tool call as a result, the one way every front
// door reports it. The text is unchanged, so an agent reading Content sees
// what it always has; the structured half carries the code, the remedy and
// whether a retry can help, for callers that act on the failure rather than
// read it. docs/decisions/0005.
//
// Shared because a second copy of it went wrong: `mobium pipe`, which every
// client spawns, built its own result without the structured half, so every
// client received every failure as an unclassified error and none of their
// exception types was ever raised against a real daemon (CHALLENGES 91).
func ErrorResult(err error) ToolsCallResult {
	return ToolsCallResult{
		Content:           []Content{{Type: "text", Text: err.Error()}},
		StructuredContent: mobiumerr.PayloadOf(err),
		IsError:           true,
	}
}

// SetBackend chooses the Android driver implementation.
func (s *Server) SetBackend(b Backend) { s.handlers.SetBackend(b) }

// SetProgress installs a callback for slow one-time setup.
func (s *Server) SetProgress(fn func(string)) { s.handlers.SetProgress(fn) }
