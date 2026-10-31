package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Tool list checks (TS-29): one bad tool must not break a server. A
// tool that does not parse, has no usable name, an input schema that is
// not an object schema, or a name seen before is dropped and logged;
// the others stay.

// maxToolName is the spec's longest tool name.
const maxToolName = 128

// parseTools parses tools/list entries one by one; dropped says
// "name: why" for each dropped one.
func parseTools(raw []json.RawMessage) (tools []Tool, dropped []string) {
	seen := map[string]bool{}
	for i, r := range raw {
		var t Tool
		if err := json.Unmarshal(r, &t); err != nil {
			var n struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(r, &n)
			dropped = append(dropped, fmt.Sprintf("%s: %s", toolLabel(n.Name, i), badSchemaWhy(err)))
			continue
		}
		if why := checkTool(t); why != "" {
			dropped = append(dropped, toolLabel(t.Name, i)+": "+why)
			continue
		}
		if seen[t.Name] {
			dropped = append(dropped, t.Name+": listed twice")
			continue
		}
		seen[t.Name] = true
		tools = append(tools, t)
	}
	return tools, dropped
}

func toolLabel(name string, i int) string {
	if name == "" {
		return fmt.Sprintf("tool #%d", i+1)
	}
	return fmt.Sprintf("%q", name)
}

func badSchemaWhy(err error) string {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) && te.Field != "" {
		return "bad " + te.Field
	}
	return "not a tool entry"
}

// metaKey finds a _meta key by its name, also with a vendor prefix
// ("example.com/name").
func (t Tool) metaKey(name string) (any, bool) {
	for k, v := range t.Meta {
		if k == name || strings.HasSuffix(k, "/"+name) {
			return v, true
		}
	}
	return nil, false
}

// RequiresUserInteraction: the server says a person must approve each
// call (_meta requiresUserInteraction: true, TS-16).
func (t Tool) RequiresUserInteraction() bool {
	v, _ := t.metaKey("requiresUserInteraction")
	b, _ := v.(bool)
	return b
}

// checkTool says why a parsed tool can not be used ("" when it can).
func checkTool(t Tool) string {
	switch {
	case t.Name == "":
		return "no name"
	case len(t.Name) > maxToolName:
		return "name too long"
	case strings.IndexFunc(t.Name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		return "name has spaces or control characters"
	}
	if t.InputSchema == nil {
		return ""
	}
	if ty, ok := t.InputSchema["type"]; ok && ty != "object" {
		return "inputSchema is not an object schema"
	}
	if p, ok := t.InputSchema["properties"]; ok {
		if _, isMap := p.(map[string]any); !isMap {
			return "inputSchema properties is not an object"
		}
	}
	if r, ok := t.InputSchema["required"]; ok {
		list, isList := r.([]any)
		if !isList {
			return "inputSchema required is not a list"
		}
		for _, x := range list {
			if _, isStr := x.(string); !isStr {
				return "inputSchema required is not a list of names"
			}
		}
	}
	return ""
}
