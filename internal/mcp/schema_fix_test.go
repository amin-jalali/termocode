package mcp

import (
	"testing"
)

func TestModelSchemaFixesRootCombinators(t *testing.T) {
	in := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}, "required": []any{"id"}},
			map[string]any{"type": "object", "properties": map[string]any{"url": map[string]any{"type": "string"}}},
		},
		"$defs": map[string]any{"x": map[string]any{"type": "string"}},
	}
	out := ModelSchema(in)
	if out["type"] != "object" {
		t.Fatalf("type = %v", out["type"])
	}
	for _, k := range []string{"anyOf", "oneOf", "allOf"} {
		if _, ok := out[k]; ok {
			t.Fatalf("root %s kept", k)
		}
	}
	props, _ := out["properties"].(map[string]any)
	if props["id"] == nil || props["url"] == nil {
		t.Fatalf("props = %v", props)
	}
	if out["$defs"] == nil {
		t.Fatal("$defs dropped")
	}
	if _, ok := in["type"]; ok {
		t.Fatal("input was changed in place")
	}
}

func TestModelSchemaKeepsAPlainObjectAndFixesNonObject(t *testing.T) {
	plain := map[string]any{"type": "object", "properties": map[string]any{}}
	if got := ModelSchema(plain); got["type"] != "object" || len(got) != 2 {
		t.Fatalf("plain = %v", got)
	}
	if got := ModelSchema(map[string]any{"type": "string"}); got["type"] != "object" {
		t.Fatalf("string root = %v", got)
	}
	if got := ModelSchema(nil); got["type"] != "object" {
		t.Fatalf("nil = %v", got)
	}
}

func TestAIToolsSendsAFixedSchema(t *testing.T) {
	tools := AITools(Server{Name: "s"}, []Tool{{Name: "t", InputSchema: map[string]any{
		"oneOf": []any{map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}}},
	}}}, nil, AdapterOptions{})
	if len(tools) != 1 || tools[0].Schema["type"] != "object" || tools[0].Schema["oneOf"] != nil {
		t.Fatalf("schema = %v", tools[0].Schema)
	}
	if tools[0].Params["a"] == nil {
		t.Fatalf("params = %v", tools[0].Params)
	}
}
