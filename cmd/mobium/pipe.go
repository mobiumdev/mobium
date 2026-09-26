package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

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

				resp := handlePipeMessage(line)
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

// handlePipeMessage answers one client message, forwarding tool calls to the
// daemon and answering the handshake locally.
func handlePipeMessage(line []byte) *agent.Response {
	var req agent.Request
	if err := json.Unmarshal(line, &req); err != nil {
		return &agent.Response{JSONRPC: "2.0", Error: &agent.Error{
			Code: agent.ParseError, Message: "Parse error", Data: err.Error(),
		}}
	}
	if req.ID == nil {
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
		result, err := daemonCall(params.Name, orEmpty(params.Arguments))
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
