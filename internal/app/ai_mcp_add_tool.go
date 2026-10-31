package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// mcp_server_add (MCPX-37, DI-19): the AI proposes one MCP server from a
// block the user pasted or a remote URL. It never adds on its own: the
// tool is mutating with no class, so every call shows the Deny-first card
// with the whole entry and what would run, and there is no "Always
// allow". Known services go through service_connect (the wizard signs in).
// Refused before the card: ${cmd:}, literal secrets (the user adds those
// with Import), and the keys that widen access (autoApprove, gateway).

// aiMCPAddProposal is one checked proposal.
type aiMCPAddProposal struct {
	name  string
	entry map[string]any
	srv   mcp.Server
}

// aiMCPAddRefusedKeys may only be set by the user by hand.
var aiMCPAddRefusedKeys = []string{"autoApprove", "gateway", "auth"}

func aiMCPAddPropose(a map[string]any) (aiMCPAddProposal, error) {
	block := strings.TrimSpace(ai.ArgString(a, "config"))
	raw := strings.TrimSpace(ai.ArgString(a, "url"))
	name := strings.TrimSpace(ai.ArgString(a, "name"))
	var p aiMCPAddProposal
	switch {
	case block != "" && raw != "":
		return p, errors.New("give config or url, not both")
	case block != "":
		if secrets.HasCmdRef(block) {
			return p, errors.New("refused: the entry uses ${cmd:}; the user must add such a server by hand")
		}
		list, problems, err := mcp.ParseImport(block)
		if err != nil {
			return p, err
		}
		if len(problems) > 0 {
			return p, errors.New(strings.Join(problems, "; "))
		}
		if len(list) != 1 {
			return p, errors.New("propose one server at a time")
		}
		if len(list[0].Secrets) > 0 {
			return p, errors.New("refused: the entry has a literal secret; ask the user to add it with Paste a config… in the Connect list, which stores secrets safely")
		}
		p.name, p.entry = list[0].Name, list[0].Entry
		if name != "" {
			p.name = name
		}
	case raw != "":
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return p, errors.New("url is not a URL")
		}
		if name == "" {
			name = aiMCPNameFromHost(u.Hostname())
		}
		p.name, p.entry = name, map[string]any{"type": "http", "url": raw}
	default:
		return p, errors.New("give config (a pasted mcpServers block) or url (a remote MCP server)")
	}
	for _, k := range aiMCPAddRefusedKeys {
		if _, ok := p.entry[k]; ok {
			return p, fmt.Errorf("refused: %q can only be set by the user", k)
		}
	}
	b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{p.name: p.entry}})
	list, _, err := mcp.ParseFile(b, mcp.SourceUser, "mcp.json")
	if err != nil || len(list) != 1 {
		return p, errors.New("not a valid server entry")
	}
	if list[0].Problem != "" {
		return p, errors.New(list[0].Problem)
	}
	p.srv = list[0]
	all, _ := mcp.LoadAll(mcpUserPath(), "")
	for _, s := range all {
		if s.Name == p.name {
			return p, fmt.Errorf("your mcp.json already has a server named %s", p.name)
		}
	}
	return p, nil
}

// aiMCPNameFromHost: "mcp.example.com" → "example".
func aiMCPNameFromHost(host string) string {
	parts := strings.Split(host, ".")
	n := host
	if len(parts) >= 2 {
		n = parts[len(parts)-2]
	}
	var b strings.Builder
	for _, r := range n {
		if r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "server"
	}
	return b.String()
}

// aiMCPAddCard is the card text: the entry as written and what it does.
func aiMCPAddCard(p aiMCPAddProposal) string {
	b, _ := json.MarshalIndent(p.entry, "", "  ")
	var what string
	if p.srv.Type == mcp.TypeStdio {
		what = "It runs on this computer: " + strings.TrimSpace(p.srv.Command+" "+strings.Join(p.srv.Args, " "))
	} else {
		what = "It connects to " + p.srv.URL
	}
	return fmt.Sprintf("Add the MCP server %q to your mcp.json:\n\n%s\n\n%s\nNothing starts now. Its tools reach the AI from the next message. Deny keeps your file as it is.",
		p.name, b, what)
}

func aiMCPAddTools() []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	return []ai.Tool{{
		Name: "mcp_server_add",
		Description: "Propose adding one MCP server to the user's mcp.json, from an mcpServers block the user gave (config) " +
			"or a remote MCP server URL (url). The user sees the entry on a card and decides; nothing starts. For a known " +
			"service (" + strings.Join(aiPresetNames(), ", ") + ") use service_connect instead. Never put secrets in the entry.",
		Params: map[string]any{
			"config": str("a pasted block: {\"mcpServers\": {\"name\": {…}}} or \"name\": {…}"),
			"url":    str("a remote MCP server URL (https)"),
			"name":   str("optional server name (letters, digits, - and _)"),
		},
		Mutating: true, // Class "": always asks, no Always allow (AH-1)
		Validate: func(a map[string]any) error { _, err := aiMCPAddPropose(a); return err },
		Describe: func(a map[string]any) string {
			p, err := aiMCPAddPropose(a)
			if err != nil {
				return "Add an MCP server (" + err.Error() + ")"
			}
			return aiMCPAddCard(p)
		},
		Run: func(_ context.Context, a map[string]any) (string, error) {
			p, err := aiMCPAddPropose(a)
			if err != nil {
				return "", err
			}
			path := mcpUserPath()
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return "", err
			}
			if err := mcp.AddServer(path, p.name, p.entry); err != nil {
				return "", err
			}
			return "added " + p.name + " to mcp.json; it starts on its first use (AI: MCP Servers… shows it)", nil
		},
	}}
}
