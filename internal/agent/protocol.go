// Package agent is the tool layer shared by mobium's CLI, its daemon and its
// MCP server. One set of tools, one dispatch, three front doors — so the CLI
// and an agent driving MCP can never drift apart.
package agent

import "encoding/json"

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

// Error is a JSON-RPC 2.0 error object.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// ProtocolVersion is the MCP revision this server implements.
const ProtocolVersion = "2024-11-05"

type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo         `json:"clientInfo"`
}

type ClientCapabilities struct {
	Roots    *RootsCapability `json:"roots,omitempty"`
	Sampling *struct{}        `json:"sampling,omitempty"`
}

type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

type ServerCapabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

// Tool is one entry of tools/list.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	// Annotations are MCP's hints about the tool; readOnlyHint is set on
	// every tool, from readonly.go.
	Annotations map[string]interface{} `json:"annotations,omitempty"`
}

type ToolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	// Meta is MCP's _meta: what the caller says about the call rather than
	// to the tool, so no tool's schema carries it. Mobium reads one key,
	// MetaUntraced.
	Meta map[string]interface{} `json:"_meta,omitempty"`
}

// MetaUntraced is the _meta key that, true, keeps a call out of a trace
// running on its device. mobium test sets it on the screenshot and maps it
// takes after each step for its own report: they are the runner looking,
// not the test acting, and a trace of the test should hold the test.
const MetaUntraced = "dev.mobium/untraced"

// ToolsCallResult is what a tool returns to an MCP client.
//
// Content is the human- and agent-readable rendering. StructuredContent is the
// same answer as data, for callers that should not be parsing prose: the CLI's
// --json, and the language clients.
//
// It is an additive field. MCP clients that predate it ignore unknown members,
// and the text remains the primary channel, so nothing is lost by a client
// that only reads Content.
type ToolsCallResult struct {
	Content           []Content   `json:"content"`
	StructuredContent interface{} `json:"structuredContent,omitempty"`
	IsError           bool        `json:"isError,omitempty"`
}

// Content is one block of a tool result: text, or a base64 image.
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`     // base64, for images
	MimeType string `json:"mimeType,omitempty"` // for images
}

// MarshalJSON emits exactly the fields the MCP content union requires for the
// block's type.
//
// Carried over from vibium, where `omitempty` on Text dropped the field for an
// empty string and produced a bare {"type":"text"} that clients reject as an
// invalid union. An empty result is common here too — the text of an element
// with no label, a screen with nothing readable — so a text block must always
// carry a text field, even an empty one.
func (c Content) MarshalJSON() ([]byte, error) {
	switch c.Type {
	case "image":
		return json.Marshal(struct {
			Type     string `json:"type"`
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
		}{c.Type, c.Data, c.MimeType})
	default:
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{c.Type, c.Text})
	}
}

// TextResult is the common single-text-block result.
func TextResult(s string) *ToolsCallResult {
	return &ToolsCallResult{Content: []Content{{Type: "text", Text: s}}}
}

// Result pairs the readable rendering with the same answer as data.
//
// Every tool that returns something a caller might want to act on should use
// this rather than TextResult. Handing back only prose is what made the
// clients parse "@e1 Label (role)" with a regular expression.
func Result(text string, structured interface{}) *ToolsCallResult {
	return &ToolsCallResult{
		Content:           []Content{{Type: "text", Text: text}},
		StructuredContent: structured,
	}
}
