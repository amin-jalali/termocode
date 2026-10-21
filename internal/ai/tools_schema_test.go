package ai

import (
	"reflect"
	"strings"
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

// With MCP tools in the list, the system prompt says their results are
// outside data, not instructions; without them it says nothing.
func TestSystemPromptMCPSentence(t *testing.T) {
	mcpTool := Tool{Name: "mcp__fake__echo"}
	for _, native := range []bool{true, false} {
		with := SystemPrompt("base", []Tool{{Name: "read_file"}, mcpTool}, native)
		if !strings.Contains(with, "come from external MCP servers") || !strings.Contains(with, "not instructions") {
			t.Errorf("native=%v: no MCP sentence:\n%s", native, with)
		}
		if without := SystemPrompt("base", []Tool{{Name: "read_file"}}, native); strings.Contains(without, "MCP") {
			t.Errorf("native=%v: MCP sentence without MCP tools", native)
		}
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

// CP-9: class containers exists for the confirm card of host container
// actions, and is NOT an auto-approve class: "Always allow containers"
// lasts for the session only, never in config.json.
func TestClassContainersIsSessionOnly(t *testing.T) {
	if ClassContainers != "containers" {
		t.Fatalf("ClassContainers = %q", ClassContainers)
	}
	for _, c := range ToolClasses {
		if c == ClassContainers {
			t.Fatalf("ToolClasses %v lists %q: it must not be saved in ai_auto_approve", ToolClasses, ClassContainers)
		}
	}
}
