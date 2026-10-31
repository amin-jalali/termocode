package app

import (
	"context"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// ── Remote MCP servers: sign-out, secrets (MC-5 / MC-6) ─────────────────
//
// Signing in is the Connect wizard (ai_mcp_connect.go): AI: Connect a
// Service…, the servers picker's "+ Connect a service…" and a server's
// Sign in action all open it.
//
// Tokens go to credentials.json mcp_oauth (0600); they never reach the
// log, toasts or the transcript (ai.Redact, ai.RegisterSecret).

// mcpCIMDURL is termocode's OAuth Client ID Metadata Document, published
// with the docs site (docs/mcp/client.json).
const mcpCIMDURL = "https://amin-jalali.github.io/termocode/mcp/client.json"

const promptKindMCPSecret promptKindEnum = 951

// mcpOverSSH reports a session whose browser is on another machine.
func mcpOverSSH() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
}

// mcpSSHHost is this machine as the laptop reaches it: the server
// address of SSH_CONNECTION, else the host name.
func mcpSSHHost() string {
	if f := strings.Fields(os.Getenv("SSH_CONNECTION")); len(f) == 4 {
		return f[2]
	}
	return sshHost()
}

func mcpCredPath() string {
	p, _ := ai.CredentialsPath()
	return p
}

// handleMCPMsg routes the remote-server messages. ok=false: not ours.
func (m *Model) handleMCPMsg(msg tea.Msg) (tea.Cmd, bool) {
	if msg, ok := msg.(mcpSignedOutMsg); ok {
		return m.aiInfo("MCP " + msg.name + ": signed out"), true
	}
	if msg, ok := msg.(mcpRemovedMsg); ok {
		if msg.err != nil {
			return m.aiToastErr("MCP "+msg.name, msg.err), true
		}
		return m.aiInfo("MCP " + msg.name + ": removed"), true
	}
	if cmd, ok := m.handleMCPServeMsg(msg); ok { // termocode as an MCP server (mcp_serve.go)
		return cmd, true
	}
	return m.handleMCPConnectMsg(msg)
}

// handleMCPPromptSubmit routes the MCP prompt kinds. ok=false: not ours.
func (m *Model) handleMCPPromptSubmit(value string) (tea.Cmd, bool) {
	switch m.promptKind {
	case promptKindMCPSecret:
		return m.onMCPSecretEntered(value), true
	case promptKindMCPBundlePath: // mcp_bundle.go
		return m.onMCPBundlePath(value), true
	case promptKindMCPBundleField:
		return m.onMCPBundleField(value, false), true
	case promptKindMCPBundleSecret:
		return m.onMCPBundleField(value, true), true
	}
	return nil, false
}

// mcpAddID is the servers picker's add row (not a valid server name).
const mcpAddID = "+add"

// mcpSignOut forgets a server's tokens.
func (m *Model) mcpSignOut(name string) tea.Cmd {
	mgr := m.aiMCP()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		mgr.SignOut(ctx, name)
		return mcpSignedOutMsg{name: name}
	}
}

// mcpSignedOutMsg ends a Sign out (revoked and forgotten).
type mcpSignedOutMsg struct{ name string }

// mcpMissingSecrets are the ${secret:NAME} of an entry that have no
// value in credentials.json yet.
func mcpMissingSecrets(s mcp.Server) []string {
	c, _ := ai.LoadCredentials(mcpCredPath())
	var out []string
	for _, n := range s.SecretNames() {
		if c.MCPSecrets[n] == "" {
			out = append(out, n)
		}
	}
	return out
}

// mcpAskSecret asks for the value of ${secret:NAME} (masked).
func (m *Model) mcpAskSecret(name string) tea.Cmd {
	if c, wait := m.secretsOpenFirst(func(m *Model) tea.Cmd { return m.mcpAskSecret(name) }); wait {
		return c
	}
	m.aiS().mcpSecret = name
	m.prompt = prompt.New("MCP secret "+name, "Value of ${secret:"+name+"}:", "")
	m.prompt.SetMasked(true)
	m.prompt.SetNote(secretPromptNote("Saved in the secret store (only you can read it), never in mcp.json."))
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindMCPSecret
	return nil
}

func (m *Model) onMCPSecretEntered(v string) tea.Cmd {
	s := m.aiS()
	name := s.mcpSecret
	s.mcpSecret = ""
	v = strings.TrimSpace(v)
	if name == "" || v == "" {
		return nil
	}
	ai.RegisterSecret(v)
	if err := (mcp.CredStore{Path: mcpCredPath()}).SetSecret(name, v); err != nil {
		return m.aiToastErr("MCP secret", err)
	}
	if sel := s.mcpSel; sel != "" && s.mcp != nil {
		s.mcp.Stop(sel) // the next start uses the new value
	}
	return m.aiInfo("Saved secret " + name)
}
