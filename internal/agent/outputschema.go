package agent

import "encoding/json"

// OutputSchema is the tool's MCP outputSchema, or nil for a tool without
// one (outputschema_gen.go says which have one; make api regenerates it).
func OutputSchema(name string) map[string]interface{} {
	raw, ok := outputSchemas[name]
	if !ok {
		return nil
	}
	var s map[string]interface{}
	if json.Unmarshal([]byte(raw), &s) != nil {
		return nil
	}
	return s
}
