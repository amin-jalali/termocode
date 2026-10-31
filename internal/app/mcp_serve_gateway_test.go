package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

func gatewayTestTools(ran *int) func(context.Context) []ai.Tool {
	return func(context.Context) []ai.Tool {
		return []ai.Tool{{Name: "mcp__files__read", Display: "mcp.files.read", Description: "[MCP files] read a file",
			Params: map[string]any{"path": map[string]any{"type": "string"}}, Required: []string{"path"},
			Schema: map[string]any{"type": "object"},
			Validate: func(a map[string]any) error {
				if _, ok := a["path"]; !ok {
					return errors.New("missing required argument(s): path")
				}
				return nil
			},
			Run: func(context.Context, map[string]any) (string, error) { *ran++; return "file text", nil }}}
	}
}

// Spec (MCPX-36): the tools of gateway servers are listed for outside
// agents next to the IDE tools.
func TestMCPServeGatewayList(t *testing.T) {
	src, hub, _ := mcpServeTestSource(t.TempDir(), nil)
	defer hub.stop()
	var ran int
	src.gateway = gatewayTestTools(&ran)
	var found *mcp.Tool
	for _, tl := range src.List() {
		if tl.Name == "mcp__files__read" {
			tl := tl
			found = &tl
		}
	}
	if found == nil || !strings.Contains(found.Description, "[MCP files]") || found.InputSchema["type"] != "object" {
		t.Fatalf("gateway tool not listed: %+v", found)
	}
}

// Spec (MCPX-36): every call of a gateway tool asks on a card (even a
// read: the server's own hints are not trusted for outside agents); Deny
// does not run it, Allow runs it and the result goes back.
func TestMCPServeGatewayCallAsks(t *testing.T) {
	asked := 0
	allow := false
	src, hub, audit := mcpServeTestSource(t.TempDir(), func(_ context.Context, client string, tl ai.Tool, human string) error {
		asked++
		if !strings.Contains(human, "mcp.files.read") {
			t.Errorf("card text %q", human)
		}
		if !allow {
			return &mcp.CallError{Code: mcp.CodeDeclined, Message: "declined"}
		}
		return nil
	})
	defer hub.stop()
	var ran int
	src.gateway = gatewayTestTools(&ran)
	ctx := mcpServeCtx("codex")
	if _, err := src.Call(ctx, "mcp__files__read", map[string]any{"path": "a"}); err == nil || ran != 0 || asked != 1 {
		t.Fatalf("deny: err %v ran %d asked %d", err, ran, asked)
	}
	allow = true
	r, err := src.Call(ctx, "mcp__files__read", map[string]any{"path": "a"})
	if err != nil || ran != 1 || asked != 2 || mcpServeText(r) != "file text" {
		t.Fatalf("allow: %+v %v ran %d asked %d", r, err, ran, asked)
	}
	if _, err := src.Call(ctx, "mcp__files__read", map[string]any{}); err == nil || asked != 2 {
		t.Fatalf("missing arg must fail before the card: %v", err)
	}
	if _, err := src.Call(ctx, "mcp__other__x", nil); err == nil {
		t.Fatal("unknown gateway tool ran")
	}
	if a := audit.all(); !strings.Contains(a, "mcp__files__read") || !strings.Contains(a, "declined") {
		t.Fatalf("audit %s", a)
	}
}

// Spec (MCPX-36): without a gateway server an mcp__ name is refused as
// before (MC-11: nothing re-exported unless the user opted in).
func TestMCPServeGatewayOffRefuses(t *testing.T) {
	src, hub, _ := mcpServeTestSource(t.TempDir(), nil)
	defer hub.stop()
	src.gateway = func(context.Context) []ai.Tool { return nil }
	if _, err := src.Call(mcpServeCtx("codex"), "mcp__files__read", nil); err == nil {
		t.Fatal("ran without a gateway server")
	}
}

// Spec (MCPX-36): only trusted user servers with "gateway": true are
// re-served, and their tools always ask.
func TestMCPGatewayAITools(t *testing.T) {
	parse := func(src, body string) mcp.Server {
		list, _, err := mcp.ParseFile([]byte(`{"mcpServers":{"s":`+body+`}}`), src, "x")
		if err != nil || len(list) != 1 {
			t.Fatal(err)
		}
		return list[0]
	}
	ro := true
	tools := []mcp.Tool{{Name: "read", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: &ro}}}
	list := []mcp.ServerTools{
		{Server: parse(mcp.SourceUser, `{"command":"x","gateway":true}`), Tools: tools},
		{Server: parse(mcp.SourceUser, `{"command":"x"}`), Tools: tools},
		{Server: parse(mcp.SourceProject, `{"command":"x","gateway":true}`), Tools: tools},
	}
	out := mcpGatewayAITools(list, nil, mcp.AdapterOptions{TrustAnnotations: true})
	if len(out) != 1 || out[0].Name != "mcp__s__read" || !out[0].Mutating {
		t.Fatalf("%+v", out)
	}
}

// Spec (MCPX-36): the gateway is looked up only when the user's file has
// a "gateway" server (no manager, no UI round trip otherwise).
func TestMCPGatewayConfigured(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mcp.json")
	if mcpGatewayConfigured(p) {
		t.Fatal("missing file counts")
	}
	writeFileT(t, p, `{"mcpServers":{"a":{"command":"x"}}}`)
	if mcpGatewayConfigured(p) {
		t.Fatal("no gateway key counts")
	}
	writeFileT(t, p, `{"mcpServers":{"a":{"command":"x","gateway":true}}}`)
	if !mcpGatewayConfigured(p) {
		t.Fatal("gateway server not seen")
	}
	writeFileT(t, p, `{"mcpServers":{"a":{"command":"x","gateway":true,"disabled":true}}}`)
	if mcpGatewayConfigured(p) {
		t.Fatal("a disabled server counts")
	}
}

func writeFileT(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
