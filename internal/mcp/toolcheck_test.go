package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Spec (TS-29): a tool with a bad schema or name is dropped (and
// logged); the server's other tools stay.
func TestBadToolsDropped(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"name":"ok","inputSchema":{"type":"object","properties":{"a":{"type":"string"}}}}`),
		json.RawMessage(`{"name":"no_schema"}`),
		json.RawMessage(`{"name":"str_schema","inputSchema":"x"}`),
		json.RawMessage(`{"name":"not_object","inputSchema":{"type":"string"}}`),
		json.RawMessage(`{"name":"bad_props","inputSchema":{"type":"object","properties":[1]}}`),
		json.RawMessage(`{"name":"bad name!","inputSchema":{"type":"object"}}`),
		json.RawMessage(`{"name":"","inputSchema":{"type":"object"}}`),
		json.RawMessage(`{"name":"ok","inputSchema":{"type":"object"}}`),
		json.RawMessage(`[1,2]`),
	}
	tools, dropped := parseTools(raw)
	var names []string
	for _, tl := range tools {
		names = append(names, tl.Name)
	}
	if strings.Join(names, ",") != "ok,no_schema" {
		t.Errorf("kept %v", names)
	}
	if len(dropped) != 7 {
		t.Errorf("dropped %v", dropped)
	}
	for _, d := range dropped {
		if !strings.Contains(d, ":") {
			t.Errorf("no reason in %q", d)
		}
	}
}

// Spec (TS-16): a tool whose _meta says requiresUserInteraction always
// asks, with no "Always allow", even when read-only or in autoApprove.
func TestRequiresUserInteraction(t *testing.T) {
	tl := Tool{Name: "pay", Annotations: ann(bp(true), nil), Meta: map[string]any{"vendor.example/requiresUserInteraction": true}}
	if !tl.RequiresUserInteraction() {
		t.Fatal("meta not read")
	}
	at := AITools(Server{Name: "s", AutoApprove: []string{"pay"}}, []Tool{tl}, bigCall("ok"), AdapterOptions{TrustAnnotations: true})[0]
	if !at.Mutating || at.Class != "" {
		t.Errorf("static %v %q", at.Mutating, at.Class)
	}
	if mut, class := at.Gate(map[string]any{}); !mut || class != "" {
		t.Errorf("gate %v %q", mut, class)
	}
	if (Tool{Name: "x", Meta: map[string]any{"requiresUserInteraction": false}}).RequiresUserInteraction() {
		t.Error("false read as true")
	}
}

// Spec (TS-30): structuredContent is checked against the tool's
// outputSchema; a mismatch is told to the model and logged, the result
// still comes through.
func TestOutputSchemaCheck(t *testing.T) {
	schema := map[string]any{"type": "object", "required": []any{"n"},
		"properties": map[string]any{"n": map[string]any{"type": "integer"}, "tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}}
	for raw, bad := range map[string]bool{
		`{"n":42}`:              false,
		`{"n":42,"tags":["a"]}`: false,
		`{"n":"42"}`:            true,
		`{"tags":["a"]}`:        true,
		`{"n":4.5}`:             true,
		`{"n":1,"tags":[1]}`:    true,
		`[1]`:                   true,
	} {
		err := checkSchema(schema, json.RawMessage(raw))
		if (err != nil) != bad {
			t.Errorf("%s: %v", raw, err)
		}
	}
	var warned int
	call := func(context.Context, string, string, map[string]any) (CallResult, error) {
		return CallResult{StructuredContent: json.RawMessage(`{"n":"x"}`)}, nil
	}
	tl := Tool{Name: "s", Annotations: ann(bp(true), nil), OutputSchema: schema}
	out, err := AITools(Server{Name: "srv"}, []Tool{tl}, call, AdapterOptions{Log: func(string, string) { warned++ }})[0].Run(context.Background(), nil)
	if err != nil || !strings.Contains(out, `{"n":"x"}`) || !strings.Contains(out, "does not match the tool's output schema") {
		t.Errorf("out %q %v", out, err)
	}
	if warned != 1 {
		t.Errorf("logged %d", warned)
	}
	if ToolsHash([]Tool{{Name: "s"}}) != ToolsHash([]Tool{{Name: "s", OutputSchema: schema}}) {
		t.Error("outputSchema changes the hash")
	}
}
