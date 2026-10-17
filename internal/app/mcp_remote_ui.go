package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/auth"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// ── Remote MCP servers: sign-in, sign-out, secrets (MC-5 / MC-6) ────────
//
// Sign in (AI: MCP Servers… → server → Sign in):
//
//   - local: the browser opens the authorize URL (also copied); it comes
//     back to a one-shot listener on 127.0.0.1 and the prompt closes by
//     itself;
//   - over SSH (the browser is on the user's laptop): the prompt says to
//     open the copied URL there, and paste the address the browser shows
//     after "can't connect"; or to forward the port with ssh -L.
//
// Tokens go to credentials.json mcp_oauth (0600); they never reach the
// log, toasts or the transcript (ai.Redact, ai.RegisterSecret).

// mcpCIMDURL is termocode's OAuth Client ID Metadata Document, published
// with the docs site (docs/mcp/client.json).
const mcpCIMDURL = "https://amin-jalali.github.io/termocode/mcp/client.json"

const (
	promptKindMCPSignIn promptKindEnum = 950 + iota
	promptKindMCPSecret
)

// mcpSignInReadyMsg: discovery and registration are done, the flow
// waits for the browser.
type mcpSignInReadyMsg struct {
	name string
	flow *auth.Flow
	err  error
}

// mcpSignInDoneMsg ends a sign-in (tokens saved and connected, or err).
type mcpSignInDoneMsg struct {
	name string
	flow *auth.Flow
	err  error
}

// mcpSignInState is the open sign-in.
type mcpSignInState struct {
	name string
	flow *auth.Flow
}

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
	switch msg := msg.(type) {
	case mcpSignInReadyMsg:
		return m.onMCPSignInReady(msg), true
	case mcpSignInDoneMsg:
		return m.onMCPSignInDone(msg), true
	case mcpSignedOutMsg:
		return m.aiInfo("MCP " + msg.name + ": signed out"), true
	}
	return nil, false
}

// handleMCPPromptSubmit routes the MCP prompt kinds. ok=false: not ours.
func (m *Model) handleMCPPromptSubmit(value string) (tea.Cmd, bool) {
	switch m.promptKind {
	case promptKindMCPSignIn:
		return m.onMCPSignInPasted(value), true
	case promptKindMCPSecret:
		return m.onMCPSecretEntered(value), true
	}
	return nil, false
}

// mcpSignInPromptClosed ends the open sign-in when its prompt is closed
// with Esc (prompt.CloseMsg).
func (m *Model) mcpSignInPromptClosed() tea.Cmd {
	if m.promptKind != promptKindMCPSignIn || m.ai == nil || m.ai.mcpSignIn == nil {
		return nil
	}
	m.ai.mcpSignIn.flow.Close() // Wait ends with "closed"
	return nil
}

// mcpSignIn starts the sign-in of a server (after the trust dialog for a
// project server).
func (m *Model) mcpSignIn(name string) tea.Cmd {
	mgr := m.aiMCP()
	if s := m.aiS(); s.mcpSignIn != nil {
		s.mcpSignIn.flow.Close()
		s.mcpSignIn = nil
	}
	return tea.Batch(m.aiInfo("MCP "+name+": starting the sign-in…"), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		fl, err := mgr.BeginSignIn(ctx, name)
		return mcpSignInReadyMsg{name: name, flow: fl, err: err}
	})
}

func (m *Model) onMCPSignInReady(msg mcpSignInReadyMsg) tea.Cmd {
	if msg.err != nil {
		if errors.Is(msg.err, auth.ErrNeedsApp) {
			return m.aiToastErr("MCP "+msg.name+" needs an app", msg.err)
		}
		return m.aiToastErr("MCP "+msg.name+": sign-in failed", msg.err)
	}
	fl := msg.flow
	m.aiS().mcpSignIn = &mcpSignInState{name: msg.name, flow: fl}
	u := fl.AuthURL()
	m.copyToClipboard(u)
	var note string
	if mcpOverSSH() {
		note = "The sign-in address is copied: open it in your laptop's browser and allow the access. " +
			"The browser then shows \"can't connect\": copy the whole address from its address bar and paste it here.\n" +
			fmt.Sprintf("Or forward the port first: ssh -L %d:127.0.0.1:%d %s", fl.Port(), fl.Port(), mcpSSHHost())
	} else {
		_ = osOpen(u)
		note = "The browser opened (the address is also copied). Allow the access there; this closes by itself. " +
			"If the browser is on another machine, paste the address it shows after the sign-in here."
	}
	m.prompt = prompt.New("Sign in to MCP "+msg.name, "Waiting for the browser… or paste the address:", "")
	m.prompt.SetNote(note)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindMCPSignIn
	mgr := m.aiMCP()
	name := msg.name
	return func() tea.Msg {
		tok, err := fl.Wait(context.Background())
		if err == nil {
			err = mgr.FinishSignIn(name, tok)
		}
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err = mgr.Connect(ctx, nil, name)
		}
		return mcpSignInDoneMsg{name: name, flow: fl, err: err}
	}
}

// onMCPSignInPasted feeds the pasted callback address to the flow.
func (m *Model) onMCPSignInPasted(v string) tea.Cmd {
	s := m.aiS()
	if s.mcpSignIn == nil {
		return m.aiInfo("No MCP sign-in in progress")
	}
	err := s.mcpSignIn.flow.Paste(v)
	if err == nil {
		return m.aiInfo("MCP " + s.mcpSignIn.name + ": signing in…")
	}
	if strings.Contains(err.Error(), "paste the whole address") {
		// Not a callback at all: ask again, the flow goes on.
		m.promptOpen = true
		return m.aiToastErr("MCP sign-in", err)
	}
	return nil // a wrong state ends the flow: mcpSignInDoneMsg tells
}

func (m *Model) onMCPSignInDone(msg mcpSignInDoneMsg) tea.Cmd {
	s := m.aiS()
	if s.mcpSignIn != nil && s.mcpSignIn.flow == msg.flow {
		s.mcpSignIn = nil
		if m.promptOpen && m.promptKind == promptKindMCPSignIn {
			m.promptOpen = false
		}
	}
	if msg.err != nil {
		if strings.Contains(msg.err.Error(), "the sign-in was closed") {
			return m.aiInfo("MCP " + msg.name + ": sign-in cancelled")
		}
		return m.aiToastErr("MCP "+msg.name+": sign-in failed", msg.err)
	}
	for _, st := range m.aiMCP().Servers() {
		if st.Name == msg.name {
			return m.aiInfo("MCP " + msg.name + ": signed in · " + mcpStatusHint(st))
		}
	}
	return m.aiInfo("MCP " + msg.name + ": signed in")
}

// ── Presets ("+ Add a server…") ──────────────────────────────────────────

// mcpAddID is the servers picker's add row (not a valid server name).
const mcpAddID = "+add"

const pickerKindMCPPresets pickerKindEnum = 955

// openMCPPresets lists the well-known remote servers.
func (m *Model) openMCPPresets() tea.Cmd {
	var items []picker.Item
	for _, p := range mcp.Presets() {
		items = append(items, picker.Item{ID: p.Name, Title: p.Title, Hint: p.URL})
	}
	items = append(items, picker.Item{ID: "+json", Title: "Other (edit mcp.json)…", Hint: "paste a Claude / Cursor mcpServers block"})
	m.picker = picker.NewItems(" Add an MCP server ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindMCPPresets
	return nil
}

// onMCPPresetPicked writes the preset into the user mcp.json and opens
// its actions (Sign in, Set secret …).
func (m *Model) onMCPPresetPicked(id string) tea.Cmd {
	if id == "+json" {
		return m.editMCPConfig()
	}
	for _, p := range mcp.Presets() {
		if p.Name != id {
			continue
		}
		if err := mcp.AddServer(mcpUserPath(), p.Name, p.Entry()); err != nil {
			return m.aiToastErr("MCP "+p.Title, err)
		}
		m.aiMCP().Reload()
		info := m.aiInfo("MCP " + p.Name + " added: " + p.Note)
		return tea.Batch(info, m.openMCPServerActions(p.Name))
	}
	return nil
}

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
	m.aiS().mcpSecret = name
	m.prompt = prompt.New("MCP secret "+name, "Value of ${secret:"+name+"}:", "")
	m.prompt.SetMasked(true)
	m.prompt.SetNote("Saved in ~/.config/termocode/ai/credentials.json (only you can read it), never in mcp.json.")
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
