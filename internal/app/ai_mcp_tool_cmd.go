package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// Call a tool by name from the chat (UX-10): "/tool server.tool key=value …"
// (or "/tool server.tool {json}"). A value is read as JSON when it is
// valid JSON (3, true, [1,2]), else it is a string. A read-only tool runs
// at once; any other tool asks first, with Cancel first. The result is a
// note in the chat (shown to you, not sent to the model).

const (
	aiMCPToolPrefix                      = "/tool "
	aiMCPToolTimeout                     = 60 * time.Second
	aiMCPToolMaxNote                     = 6000
	confirmKindAIMCPTool confirmKindEnum = 30313
)

type aiMCPToolCmd struct {
	server, tool string
	args         map[string]any
}

// aiMCPToolParse reads "/tool server.tool k=v …". ok=false: not that form.
func aiMCPToolParse(text string) (aiMCPToolCmd, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), aiMCPToolPrefix)
	if !ok {
		return aiMCPToolCmd{}, false
	}
	head, tail, _ := strings.Cut(strings.TrimSpace(rest), " ")
	server, tool, found := strings.Cut(head, ".")
	if !found || server == "" || tool == "" || strings.ContainsAny(head, "/\n") {
		return aiMCPToolCmd{}, false
	}
	c := aiMCPToolCmd{server: server, tool: tool, args: map[string]any{}}
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return c, true
	}
	if strings.HasPrefix(tail, "{") {
		if err := json.Unmarshal([]byte(tail), &c.args); err != nil {
			return aiMCPToolCmd{}, false
		}
		return c, true
	}
	parts, err := aiShellSplit(tail)
	if err != nil {
		return aiMCPToolCmd{}, false
	}
	for _, p := range parts {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return aiMCPToolCmd{}, false
		}
		var j any
		if json.Unmarshal([]byte(v), &j) == nil && v != "" {
			c.args[k] = j
		} else {
			c.args[k] = v
		}
	}
	return c, true
}

// aiMCPToolMsg is the answer of a "/tool" command: the result text, an
// error, or needsYes (a write: ask first).
type aiMCPToolMsg struct {
	cmd      aiMCPToolCmd
	text     string
	err      error
	needsYes bool
}

// aiMCPToolSubmit takes "/tool server.tool …" when server is configured;
// ok=false lets the text go on as it is.
func (m *Model) aiMCPToolSubmit(text string) (tea.Cmd, bool) {
	c, ok := aiMCPToolParse(text)
	set := mcp.LoadSettings(mcpConfigPath())
	if !ok || !set.Enabled {
		return nil, false
	}
	mgr := m.aiMCP()
	if _, known := mgr.Caps(c.server); !known {
		return nil, false
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), aiMCPToolTimeout)
		defer cancel()
		var found *mcp.Tool
		var srv mcp.Server
		for _, st := range mgr.ToolsFor(ctx, nil, func(n string) bool { return n == c.server }) {
			for i := range st.Tools {
				if st.Tools[i].Name == c.tool {
					found, srv = &st.Tools[i], st.Server
				}
			}
		}
		if found == nil {
			return aiMCPToolMsg{cmd: c, err: fmt.Errorf("MCP %s has no tool %q", c.server, c.tool)}
		}
		exposed, mutating, _ := mcp.Gate(srv, *found, set.TrustAnnotations)
		if !exposed || set.ToolOff(c.server, c.tool) {
			return aiMCPToolMsg{cmd: c, err: fmt.Errorf("%s.%s is turned off for termocode", c.server, c.tool)}
		}
		if mutating {
			return aiMCPToolMsg{cmd: c, needsYes: true}
		}
		return aiMCPToolRun(ctx, mgr, c)
	}, true
}

// aiMCPToolRun calls the tool and returns its result as a message.
func aiMCPToolRun(ctx context.Context, mgr *mcp.Manager, c aiMCPToolCmd) aiMCPToolMsg {
	res, err := mgr.Call(ctx, nil, c.server, c.tool, c.args)
	if err != nil {
		return aiMCPToolMsg{cmd: c, err: err}
	}
	text := ai.RedactSecrets(aiCleanText(res.Text()))
	if r := []rune(text); len(r) > aiMCPToolMaxNote {
		text = string(r[:aiMCPToolMaxNote]) + "…"
	}
	if res.IsError {
		return aiMCPToolMsg{cmd: c, err: fmt.Errorf("%s", text)}
	}
	if strings.TrimSpace(text) == "" {
		text = "(no output)"
	}
	return aiMCPToolMsg{cmd: c, text: text}
}

// aiMCPToolLine is "fake.write_note title=T …" for notes and the card.
func aiMCPToolLine(c aiMCPToolCmd) string {
	b, _ := json.Marshal(c.args) // keys come out sorted
	return c.server + "." + c.tool + " " + string(b)
}

// onAIMCPTool shows the result, asks first for a write, or says why not.
func (m *Model) onAIMCPTool(msg aiMCPToolMsg) tea.Cmd {
	s := m.aiS()
	switch {
	case msg.err != nil:
		return m.aiMCPWarn("MCP tool not run", ai.Redact(msg.err.Error()))
	case msg.needsYes:
		s.mcpx.toolPending = &msg.cmd
		m.confirm = confirm.New("Run "+msg.cmd.server+"."+msg.cmd.tool+"?",
			"This tool can change things on the server.\n\nCall: "+ai.Redact(aiMCPToolLine(msg.cmd)),
			[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "run", Title: "Run", Style: confirm.StyleDestructive}})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen, m.confirmKind = true, confirmKindAIMCPTool
		return nil
	}
	m.aiNote("⌁ " + ai.Redact(aiMCPToolLine(msg.cmd)) + "\n" + msg.text)
	return nil
}

// runAIMCPToolConfirm answers the card: Run calls the tool, Cancel drops it.
func (m *Model) runAIMCPToolConfirm(id string) tea.Cmd {
	s := m.aiS()
	c := s.mcpx.toolPending
	s.mcpx.toolPending = nil
	if id != "run" || c == nil {
		return nil
	}
	mgr := m.aiMCP()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), aiMCPToolTimeout)
		defer cancel()
		return aiMCPToolRun(ctx, mgr, *c)
	}
}
