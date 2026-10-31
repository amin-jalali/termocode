package mcp

import (
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
