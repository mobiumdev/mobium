package claim

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Tool is one thing the verifier may ask for, described as JSON Schema.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]interface{}
}

// Observation is what a tool answered: text, and a screenshot when asked for.
type Observation struct {
	Text    string
	JPEG    []byte
	IsError bool
}

const pageSize = 25

func object(props map[string]interface{}, required ...string) map[string]interface{} {
	if required == nil {
		required = []string{}
	}
	return map[string]interface{}{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

var (
	idProp     = map[string]interface{}{"type": "string", "description": "A call's id from trace_list_calls, such as \"call@3\"."}
	offsetProp = map[string]interface{}{"type": "integer", "description": "Where the page starts, from 0."}
)

// traceTools are the verifier's tools: read-only, over the recording alone.
var traceTools = []Tool{
	{"trace_summary", "The recording at a glance: its title, how many calls it holds, which tools, and which calls failed.", object(map[string]interface{}{})},
	{"trace_list_calls", "The calls in order, a page at a time: id, tool, its arguments in brief, and whether it succeeded.",
		object(map[string]interface{}{"offset": offsetProp})},
	{"trace_inspect_call", "One call in full: its arguments, its result or error, where it touched, and whether a screen was kept after it.",
		object(map[string]interface{}{"id": idProp}, "id")},
	{"trace_screen", "The screen after a call as text: each element of the map with its ref, label and role, and where it sat.",
		object(map[string]interface{}{"id": idProp}, "id")},
	{"trace_screenshot", "The screenshot after a call, as an image. Use it when the screen's text cannot settle the claim.",
		object(map[string]interface{}{"id": idProp}, "id")},
	{"trace_search", "Calls whose tool, arguments, result, error or screen mention a phrase, any case.",
		object(map[string]interface{}{"query": map[string]interface{}{"type": "string"}, "offset": offsetProp}, "query")},
}

// Execute answers one tool call over t. A bad request is an error the model
// sees and can correct, never a failure of the check.
func (t *Trace) Execute(ctx context.Context, name string, input json.RawMessage) Observation {
	if err := ctx.Err(); err != nil {
		return Observation{Text: err.Error(), IsError: true}
	}
	var in struct {
		ID     string `json:"id"`
		Offset int    `json:"offset"`
		Query  string `json:"query"`
	}
	if len(input) > 0 && json.Unmarshal(input, &in) != nil {
		return Observation{Text: "the arguments are not a JSON object", IsError: true}
	}
	call := func() (*Call, *Observation) {
		c := t.byID[in.ID]
		if c == nil {
			return nil, &Observation{Text: fmt.Sprintf("no call %q: ids come from trace_list_calls", in.ID), IsError: true}
		}
		return c, nil
	}
	switch name {
	case "trace_summary":
		var failed []string
		for _, c := range t.Calls {
			if c.Error != "" {
				failed = append(failed, c.ID+" "+c.Tool)
			}
		}
		return Observation{Text: jsonText(map[string]interface{}{
			"title": t.Title, "calls": len(t.Calls), "tools": t.Tools(), "failed": failed,
		})}
	case "trace_list_calls":
		return Observation{Text: jsonText(page(t.Calls, in.Offset, brief))}
	case "trace_inspect_call":
		c, bad := call()
		if bad != nil {
			return *bad
		}
		return Observation{Text: jsonText(map[string]interface{}{
			"call": c, "screen_kept": len(c.Screen) > 0 || c.Image != "",
		})}
	case "trace_screen":
		c, bad := call()
		if bad != nil {
			return *bad
		}
		if len(c.Screen) == 0 && c.Image == "" {
			return Observation{Text: "no screen was kept after " + c.ID}
		}
		return Observation{Text: jsonText(map[string]interface{}{"after": c.ID, "elements": c.Screen})}
	case "trace_screenshot":
		c, bad := call()
		if bad != nil {
			return *bad
		}
		img, err := t.Image(c.ID)
		if err != nil {
			return Observation{Text: err.Error(), IsError: true}
		}
		if img == nil {
			return Observation{Text: "no screenshot was kept after " + c.ID}
		}
		return Observation{Text: "the screen after " + c.ID, JPEG: img}
	case "trace_search":
		q := strings.ToLower(strings.TrimSpace(in.Query))
		if q == "" {
			return Observation{Text: "query is empty", IsError: true}
		}
		var hits []*Call
		for _, c := range t.Calls {
			if strings.Contains(strings.ToLower(searchable(c)), q) {
				hits = append(hits, c)
			}
		}
		return Observation{Text: jsonText(page(hits, in.Offset, brief))}
	}
	return Observation{Text: "no tool " + name, IsError: true}
}

func brief(c *Call) map[string]interface{} {
	m := map[string]interface{}{"id": c.ID, "tool": c.Tool, "ok": c.Error == ""}
	if c.Title != "" {
		m["title"] = clip(c.Title, 160)
	}
	if c.Error != "" {
		m["error"] = clip(c.Error, 200)
	}
	return m
}

func page(calls []*Call, offset int, f func(*Call) map[string]interface{}) map[string]interface{} {
	if offset < 0 {
		offset = 0
	}
	end := min(offset+pageSize, len(calls))
	var items []map[string]interface{}
	for _, c := range calls[min(offset, len(calls)):end] {
		items = append(items, f(c))
	}
	out := map[string]interface{}{"total": len(calls), "offset": offset, "calls": items}
	if end < len(calls) {
		out["next_offset"] = end
	}
	return out
}

func searchable(c *Call) string {
	var b strings.Builder
	b.WriteString(c.Tool + " " + c.Title + " " + c.Result + " " + c.Error + " ")
	if p, err := json.Marshal(c.Params); err == nil {
		b.Write(p)
	}
	for _, e := range c.Screen {
		b.WriteString(" " + e.Title)
	}
	return b.String()
}

func jsonText(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	return string(b)
}
