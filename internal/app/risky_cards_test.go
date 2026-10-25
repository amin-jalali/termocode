package app

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// AH-1: one rule for every card that grants trust or access, or lets the
// AI or an outside agent do something: the safe answer is the first button,
// plain and focused; Enter on a fresh card picks it; Esc is the safe answer
// too. A stray Enter (a card can open while the user types) never grants.

// riskyCard is one card of the table. open opens it on a fresh model and
// returns granted, which reports (after the answer) whether the card gave
// what it asks for. granted is nil when the card has no state to look at
// beyond the button it picked.
type riskyCard struct {
	name   string
	safe   string
	helper bool // built with confirm.Risky (cards of files this rule owns)
	open   func(t *testing.T) (m *Model, granted func() bool)
}

func riskyCards() []riskyCard {
	chatTool := func(tool ai.Tool) func(t *testing.T) (*Model, func() bool) {
		return func(t *testing.T) (*Model, func() bool) {
			m := aiTestModel(t)
			reply := make(chan bool, 1)
			m.onAIChatConfirm(aiChatConfirmMsg{tool: tool, human: "do it", reply: reply, ch: make(chan tea.Msg)})
			return &m, func() bool {
				select {
				case ok := <-reply:
					return ok
				default:
					return false // no answer: nothing ran
				}
			}
		}
	}
	return []riskyCard{
		{name: "project profile trust", safe: "deny", helper: true, open: func(t *testing.T) (*Model, func() bool) {
			m, _ := aiProfileTestModel(t)
			m.ai.profile = "house"
			m.sendAIChat("check my code")
			return m, func() bool {
				return loadSession().AIProfilesTrusted["house"] != "" || len(m.ai.conv.Messages) != 0
			}
		}},
		{name: "outside agent call", safe: "deny", helper: true, open: func(t *testing.T) (*Model, func() bool) {
			m, src, hub := mcpServeTestModel(t)
			call := mcpServeStartCall(src, "claude-code", "tasks_run", map[string]any{"label": "say hi"})
			mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "claude-code") })
			return m, func() bool {
				got := mcpServeAwait(t, m, hub, call)
				var ce *mcp.CallError
				return !errors.As(got.err, &ce) || ce.Code != mcp.CodeDeclined
			}
		}},
		{name: "add termocode to the project file", safe: "skip", helper: true, open: func(t *testing.T) (*Model, func() bool) {
			m := aiTestModel(t)
			a, ok := agents.ByID("opencode")
			if !ok {
				t.Fatal("no opencode agent")
			}
			m.openAgentMCPCard(&agentMCPCard{agent: a, path: "/w/opencode.json"})
			return &m, nil
		}},
		{name: "stop listening", safe: "cancel", helper: true, open: func(t *testing.T) (*Model, func() bool) {
			m := mcpServeStatusModel(t)
			m.onMCPServeStatusPicked("serve-stop")
			return m, nil
		}},
		{name: "disconnect an outside agent", safe: "cancel", helper: true, open: func(t *testing.T) (*Model, func() bool) {
			m := mcpServeStatusModel(t)
			m.onMCPServeStatusPicked("agent:1")
			return m, nil
		}},
		// The chat's own cards (ai_chat.go, ai_mcp.go) already follow the
		// rule with their own buttons; they are in the table so a change
		// there can not break it.
		{name: "AI database write", safe: "deny", open: chatTool(ai.Tool{Name: "db_query", Mutating: true})},
		{name: "AI containers", safe: "deny", open: chatTool(ai.Tool{Name: "containers_control", Class: ai.ClassContainers, Mutating: true})},
		{name: "AI services", safe: "deny", open: chatTool(ai.Tool{Name: "services_start", Class: ai.ClassServices, Mutating: true})},
		{name: "AI MCP tool", safe: "deny", open: chatTool(ai.Tool{Name: mcp.WirePrefix + "github__create_issue", Mutating: true})},
		{name: "start a project MCP server", safe: "deny", open: func(t *testing.T) (*Model, func() bool) {
			m := aiTestModel(t)
			m.mcpTrustDialog(mcp.Server{Name: "gh", Command: "gh-mcp", Source: mcp.SourceProject})
			return &m, nil
		}},
		{name: "MCP server tools changed", safe: "deny", open: func(t *testing.T) (*Model, func() bool) {
			m := aiTestModel(t)
			m.mcpTrustDialog(mcp.Server{Name: "gh", Command: "gh-mcp", TrustReason: mcp.ReasonToolsChanged})
			return &m, nil
		}},
	}
}

func TestRiskyCardsSafeFirstFocusedEnterAndEsc(t *testing.T) {
	for _, c := range riskyCards() {
		t.Run(c.name, func(t *testing.T) {
			m, _ := c.open(t)
			if !m.confirmOpen {
				t.Fatal("no card is open")
			}
			b := m.confirm.Buttons()
			if len(b) < 2 || b[0].ID != c.safe {
				t.Fatalf("first button %+v, want %q first", b, c.safe)
			}
			if b[0].Style == confirm.StyleDestructive {
				t.Errorf("the safe button %q is red", c.safe)
			}
			if c.helper && m.confirm.SafeID() != c.safe {
				t.Errorf("card not built with confirm.Risky (SafeID %q)", m.confirm.SafeID())
			}

			// Enter on the fresh card: the safe answer, and nothing granted.
			m, granted := c.open(t)
			if got := focusedCardID(t, *m); got != c.safe {
				t.Fatalf("Enter picks %q, want %q", got, c.safe)
			}
			m.confirmOpen = false
			m.handleConfirmSelect(c.safe)
			if granted != nil && granted() {
				t.Error("Enter on a fresh card granted it")
			}

			// Esc: the card closes, and nothing is granted.
			m, granted = c.open(t)
			_, cmd := m.confirm.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if cmd == nil {
				t.Fatal("Esc does nothing")
			}
			if _, ok := cmd().(confirm.CloseMsg); !ok {
				t.Fatalf("Esc gives %T, want CloseMsg", cmd())
			}
			m.confirmOpen = false
			m.onAIConfirmClosed()
			if granted != nil && granted() {
				t.Error("Esc granted it")
			}
		})
	}
}
