package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Agents without a per-launch flag (M1 MS-4) ──────────────────────────
//
// OpenCode reads MCP servers from opencode.json only. Adding termocode
// is a write into the user's project, so a card shows the file and the
// entry first; nothing is written without a yes. "Not now" starts the
// agent plain and the card does not return in this run. Gemini CLI and
// Aider have no flag at all: one toast per run tells the user what to do
// by hand (F1 → MCP: Copy termocode Server Config).

const confirmKindAgentMCP confirmKindEnum = 1960

// agentMCPCard is the agent waiting for the card's answer.
type agentMCPCard struct {
	agent agents.Agent
	path  string // the project file
	out   []byte // what the file will hold
}

// opencodeMCPMerge returns the opencode.json that adds termocode to old
// (nil or empty for no file). changed=false: it holds the entry already.
// A file that is not plain JSON (OpenCode allows comments) is an error:
// it is never rewritten behind the user's back.
func opencodeMCPMerge(old []byte, exe, root string) (out []byte, changed bool, err error) {
	entry := map[string]any{
		"type":    "local",
		"command": []string{exe, "mcp", "--dir", root},
		"enabled": true,
	}
	doc := map[string]any{}
	if len(bytes.TrimSpace(old)) > 0 {
		if err := json.Unmarshal(old, &doc); err != nil {
			return nil, false, errors.New("opencode.json is not plain JSON (comments?); add termocode by hand")
		}
	} else {
		doc["$schema"] = "https://opencode.ai/config.json"
	}
	servers, ok := doc["mcp"].(map[string]any)
	if doc["mcp"] != nil && !ok {
		return nil, false, errors.New("opencode.json has an unexpected \"mcp\" value; add termocode by hand")
	}
	if servers == nil {
		servers = map[string]any{}
	}
	// Compare as decoded JSON, so key order and spacing do not matter.
	var entryJSON any
	b, _ := json.Marshal(entry)
	_ = json.Unmarshal(b, &entryJSON)
	if reflect.DeepEqual(servers["termocode"], entryJSON) {
		return old, false, nil
	}
	servers["termocode"] = entry
	doc["mcp"] = servers
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), true, nil
}

// agentMCPCardFor decides whether launching a needs the project-file
// card. A plain launch (nothing to ask) returns nil.
func (m *Model) agentMCPCardFor(a agents.Agent) (*agentMCPCard, tea.Cmd) {
	if mcpServeMode() == "off" || a.MCP.Kind != agents.MCPProjectFile || a.MCP.ProjectFile == "" || m.confirmOpen {
		return nil, nil
	}
	exe, err := mcpServeExecutable()
	if err != nil || exe == "" {
		return nil, nil
	}
	root := aiWorkspaceRoot()
	if m.agentMCPDeclined[root+"|"+a.ID] {
		return nil, nil
	}
	path := filepath.Join(root, a.MCP.ProjectFile)
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, m.agentMCPToast(a.Title+" starts without termocode's tools", err.Error())
	}
	out, changed, err := opencodeMCPMerge(old, exe, root)
	if err != nil {
		return nil, m.agentMCPToast(a.Title+" starts without termocode's tools", err.Error())
	}
	if !changed {
		return nil, nil
	}
	return &agentMCPCard{agent: a, path: path, out: out}, nil
}

func (m *Model) agentMCPToast(title, detail string) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, title, detail)
	return c
}

// openAgentMCPCard shows the card: the file, the entry, three words of
// why.
func (m *Model) openAgentMCPCard(c *agentMCPCard) {
	m.agentMCPPending = c
	var b strings.Builder
	b.WriteString("To give " + c.agent.Title + " the editor's tools, termocode adds itself to this file in your project:\n\n")
	b.WriteString("  " + c.path + "\n\n")
	b.WriteString("The entry (other settings stay as they are):\n")
	b.WriteString(`  "termocode": local server, runs `)
	b.WriteString("`termocode mcp`\n\n")
	b.WriteString("The file will hold absolute paths of this machine: the termocode program and this folder.\n")
	b.WriteString("Check them before you commit it; they are not right on another computer.\n\n")
	b.WriteString("Not now: " + c.agent.Title + " starts without them. Esc is the same.")
	m.confirm = confirm.New("✦ Add termocode to "+c.agent.MCP.ProjectFile+"?", b.String(), []confirm.Button{
		{ID: "add", Title: "Add", Style: confirm.StylePrimary},
		{ID: "skip", Title: "Not now"},
	}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindAgentMCP
}

// answerAgentMCPCard handles a button (and Esc as "skip"): "add" writes
// the file; either way the agent starts.
func (m *Model) answerAgentMCPCard(id string) tea.Cmd {
	c := m.agentMCPPending
	m.agentMCPPending = nil
	if c == nil {
		return nil
	}
	var cmds []tea.Cmd
	if id == "add" {
		if err := writeProjectFile(c.path, c.out); err != nil {
			cmds = append(cmds, m.agentMCPToast("Could not write "+c.agent.MCP.ProjectFile, err.Error()))
		} else {
			cmds = append(cmds, m.agentMCPToast("Added termocode to "+c.agent.MCP.ProjectFile, "Restart "+c.agent.Title+" if it was open."))
		}
	} else {
		if m.agentMCPDeclined == nil {
			m.agentMCPDeclined = map[string]bool{}
		}
		m.agentMCPDeclined[aiWorkspaceRoot()+"|"+c.agent.ID] = true
	}
	cmds = append(cmds, m.launchAgent(c.agent))
	return tea.Batch(cmds...)
}

// writeProjectFile replaces path through a temp file in the same folder,
// keeping the file's mode (0644 for a new one).
func writeProjectFile(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".termocode-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(name, mode)
	}
	if werr == nil {
		werr = os.Rename(name, path)
	}
	if werr != nil {
		_ = os.Remove(name)
	}
	return werr
}

// agentMCPHandTip says once per run how to add termocode by hand to an
// agent with no per-launch way (Gemini CLI, Aider).
func (m *Model) agentMCPHandTip(a agents.Agent) tea.Cmd {
	if mcpServeMode() == "off" || a.MCP.Kind != agents.MCPNone || a.MCP.Note == "" || m.agentMCPTipped[a.ID] {
		return nil
	}
	if m.agentMCPTipped == nil {
		m.agentMCPTipped = map[string]bool{}
	}
	m.agentMCPTipped[a.ID] = true
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, a.Title+": no termocode tools yet", a.MCP.Note)
	return c
}
