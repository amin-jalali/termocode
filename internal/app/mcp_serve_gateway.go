package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// Gateway (MCPX-36, RT-22): `termocode mcp` re-serves the tools of the
// user's own MCP servers that have "gateway": true, so one setup works
// for every agent. Every call asks on the same Deny-first card as the IDE
// tools: the server's read-only hints are not trusted for outside agents.
// Untrusted servers are never started for this (no trust card from an
// outside agent); servers without "gateway" stay private (MC-11).

// mcpGatewayListTimeout caps listing the gateway tools (a server may have
// to start once to list them).
const mcpGatewayListTimeout = 20 * time.Second

// mcpGatewayAITools turns the gateway servers' tools into agent tools
// that always ask.
func mcpGatewayAITools(list []mcp.ServerTools, call mcp.CallFunc, o mcp.AdapterOptions) []ai.Tool {
	var out []ai.Tool
	for _, st := range list {
		if !st.Server.Gateway() {
			continue
		}
		for _, t := range mcp.AITools(st.Server, st.Tools, call, o) {
			t.Mutating, t.Class = true, ""
			out = append(out, t)
		}
	}
	return out
}

// mcpGatewayFunc is the production gateway list: the chat's manager,
// trusted servers only (ask nil never shows a trust card).
func mcpGatewayFunc(call aiIDECaller) func(context.Context) []ai.Tool {
	return func(ctx context.Context) []ai.Tool {
		set := mcp.LoadSettings(mcpConfigPath())
		if !set.Enabled || !mcpGatewayConfigured(mcpUserPath()) {
			return nil // the common case: no UI round trip, no manager
		}
		var mgr *mcp.Manager
		if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			mgr = m.aiMCP()
			return "", nil, nil
		}); err != nil || mgr == nil {
			return nil
		}
		mgr.Reload()
		ctx, cancel := context.WithTimeout(ctx, mcpGatewayListTimeout)
		defer cancel()
		o := mcp.AdapterOptions{MaxResult: set.MaxResultKB << 10, Clean: aiCleanText}
		return mcpGatewayAITools(mgr.Tools(ctx, nil), mgr.Caller(nil), o)
	}
}

// mcpGatewayConfigured reports whether the user's mcp.json has a
// server with "gateway": true (read from the file, nothing starts).
func mcpGatewayConfigured(userPath string) bool {
	list, _ := mcp.Load(userPath, "")
	for _, sv := range list {
		if sv.Gateway() {
			return true
		}
	}
	return false
}

// gatewaySpecs is the tools/list part of the gateway servers.
func (s *mcpServeSource) gatewaySpecs() []mcp.Tool {
	if s.gateway == nil {
		return nil
	}
	var out []mcp.Tool
	for _, t := range s.gateway(context.Background()) {
		out = append(out, mcp.Tool{Name: t.Name, Description: t.Description, InputSchema: t.Schema})
	}
	return out
}

// callGateway runs one gateway tool: Validate, the card (always), then
// the server.
func (s *mcpServeSource) callGateway(ctx context.Context, client, name string, args map[string]any, audit func(gate, result string)) (mcp.CallResult, error) {
	var tool ai.Tool
	found := false
	if s.gateway != nil {
		tool, found = ai.FindTool(s.gateway(ctx), name)
	}
	if !found {
		audit("refused", "not an exposed tool")
		return mcp.CallResult{}, &mcp.CallError{Code: mcp.CodeInvalidParams, Message: fmt.Sprintf("unknown tool %q", name)}
	}
	if tool.Validate != nil {
		if err := tool.Validate(args); err != nil {
			msg := ai.Redact(err.Error())
			audit(ai.GateInvalid, "error: "+msg)
			return mcp.CallResult{}, &mcp.CallError{Code: mcp.CodeInvalidParams, Message: msg}
		}
	}
	gate := ai.GateConfirm
	key := ""
	if client != mcpServeGenericClient {
		key = tool.Name
	}
	if s.allow.has(client, key) {
		gate += ":always"
	} else {
		human := tool.Display
		if tool.Describe != nil {
			human = tool.Describe(args)
		}
		cardCtx := withMCPServeCard(ctx, mcpServeCardInfo{args: aiAuditArgs(args), always: key})
		if err := s.ask(cardCtx, client, tool, human); err != nil {
			audit(gate, "declined: "+err.Error())
			return mcp.CallResult{}, err
		}
		gate += ":allowed"
	}
	out, err := mcpServeRunTool(ctx, tool, args)
	if err != nil {
		text := "error: " + ai.Redact(err.Error())
		audit(gate, text)
		return mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: text}}, IsError: true}, nil
	}
	out = ai.RedactSecrets(out)
	audit(gate, out)
	return mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: out}}}, nil
}

// isGatewayName: a name only the gateway serves.
func isGatewayName(name string) bool { return strings.HasPrefix(name, mcp.WirePrefix) }
