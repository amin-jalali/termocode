package ai

import (
	"reflect"
	"testing"
)

// MC-3: a tool with a full JSON Schema (MCP inputSchema with $defs,
// anyOf …) sends it as is; the text protocol still lists Params.
func TestToolSpecUsesSchema(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"anyOf": []any{}}},
		"$defs": map[string]any{"x": map[string]any{"type": "string"}}}
	tool := Tool{Name: "mcp__fake__s", Schema: schema, Params: map[string]any{"q": map[string]any{"type": "string"}}}
	if got := tool.Spec().Schema; !reflect.DeepEqual(got, schema) {
		t.Errorf("Spec schema %v, want %v", got, schema)
	}
	plain := Tool{Name: "x", Params: map[string]any{"a": strParam("a")}, Required: []string{"a"}}
	if got := plain.Spec().Schema; got["type"] != "object" || got["required"] == nil {
		t.Errorf("plain schema %v", got)
	}
}

// Label is the display name (mcp.fake.echo) when set, else the name.
func TestToolLabel(t *testing.T) {
	if l := (Tool{Name: "mcp__fake__echo", Display: "mcp.fake.echo"}).Label(); l != "mcp.fake.echo" {
		t.Errorf("label %q", l)
	}
	if l := (Tool{Name: "read_file"}).Label(); l != "read_file" {
		t.Errorf("label %q", l)
	}
}
