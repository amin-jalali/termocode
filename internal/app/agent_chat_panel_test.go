package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agents"
)

const chatPanelQuestionLog = `{"type":"user","sessionId":"s2","cwd":"/w","timestamp":"2026-11-05T10:00:00Z","message":{"role":"user","content":"Add integration tests."}}
{"type":"assistant","sessionId":"s2","timestamp":"2026-11-05T10:00:05Z","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"q1","name":"AskUserQuestion","input":{"questions":[{"header":"Database","question":"Which database do you want for the integration tests?","options":[{"label":"SQLite","description":"Fast, no server needed"},{"label":"Postgres","description":"Same as production"}]}]}}]}}
`

const chatPanelPlanLog = `{"type":"user","sessionId":"s3","cwd":"/w","timestamp":"2026-11-05T10:00:00Z","message":{"role":"user","content":"Plan the cache."}}
{"type":"assistant","sessionId":"s3","timestamp":"2026-11-05T10:00:05Z","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"p1","name":"ExitPlanMode","input":{"plan":"# Cache plan\n\n1. Add an LRU in cache.go\n2. Test it"}}]}}
`

var chatPanelQuestionScreen = []string{
	" Which database do you want for the integration tests?",
	"",
	" ❯ 1. SQLite",
	"      Fast, no server needed",
	"   2. Postgres",
	"      Same as production",
	"   3. Type something.",
	"",
	" Enter to select · ↑/↓ to navigate · Esc to cancel",
}

var chatPanelPlanScreen = []string{
	" Would you like to proceed?",
	" ❯ 1. Yes, and auto-accept edits",
	"   2. Yes, and manually approve edits",
	"   3. No, keep planning",
}

// chatPanelModel is a chat view on a waiting Claude Code tab (buf 7)
// with the session log and the screen given; sent collects what goes
// to the terminal.
func chatPanelModel(t *testing.T, log string, screen *[]string) (*Model, *[]string) {
	t.Helper()
	m, _ := chatTestModel(t)
	m.terminalTabs[0].AgentState = agents.Waiting
	s, err := agentlog.Parse(agentlog.Claude, strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	agentChatGen++
	m.agentChat = &agentChatState{gen: agentChatGen, buf: 7, agent: "claude", cli: agentlog.Claude,
		title: "Claude Code", loaded: true, sess: s}
	m.showAgentChatPanel()
	oldRead := agentReadTail
	agentReadTail = func(_ *Model, buf int) (string, bool) { return strings.Join(*screen, "\n"), buf == 7 }
	var sent []string
	oldSend := agentChatSend
	agentChatSend = func(_ *Model, buf int, text string) { sent = append(sent, itoa(buf)+":"+text) }
	t.Cleanup(func() { agentReadTail, agentChatSend = oldRead, oldSend })
	m.agentChatSyncChoices(m.agentChat)
	return m, &sent
}

// drainApply runs cmd and the commands its results return, in order.
func drainApply(m *Model, cmd tea.Cmd) {
	for i := 0; cmd != nil && i < 20; i++ {
		a, ok := cmd().(applyMsg)
		if !ok {
			return
		}
		cmd = a(m)
	}
}

func chatKey(m *Model, s string) tea.Cmd {
	cmd, _ := agentChatKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return cmd
}

// Spec (CMP-14): while a Claude Code tab asks a question (its screen
// shows a numbered menu and the session's newest call is
// AskUserQuestion), the chat view shows a native QUESTION panel: the
// question, its options with their descriptions and the menu's numbers.
// A number key picks that answer at once (the user chose it): the
// cursor is moved there with arrow keys, then Enter, one key at a time.
func TestAgentChatQuestionPanel(t *testing.T) {
	screen := chatPanelQuestionScreen
	m, sent := chatPanelModel(t, chatPanelQuestionLog, &screen)
	rows := chatPlain(m.agentChatRender(110, 30))
	for _, want := range []string{"QUESTION", "Which database do you want for the integration tests?",
		"1  SQLite", "2  Postgres", "3  Type something.", "Fast, no server needed"} {
		if !strings.Contains(rows, want) {
			t.Errorf("no %q in:\n%s", want, rows)
		}
	}
	for _, r := range m.agentChatRender(60, 24) {
		if w := ansi.StringWidth(r); w > 60 {
			t.Errorf("row %q is %d cells wide", ansi.Strip(r), w)
		}
	}
	drainApply(m, chatKey(m, "2"))
	if got := strings.Join(*sent, "|"); got != "7:\x1b[B|7:\r" {
		t.Errorf("sent %q", got)
	}
	if m.confirmOpen {
		t.Error("a card for a question answer")
	}
	// 9 is not on the menu: nothing is sent, the box stays empty.
	*sent = nil
	m.agentChatSyncChoices(m.agentChat) // the next poll reads the screen again
	drainApply(m, chatKey(m, "9"))
	if len(*sent) != 0 || m.agentChat.input != "" {
		t.Errorf("9 sent %q, input %q", *sent, m.agentChat.input)
	}
}

// Spec (CMP-14): a plan waiting for approval (ExitPlanMode) shows a PLAN
// panel with the plan's first lines; o opens the whole plan read only in
// an editor tab. A number answers only through a card with Cancel first:
// Cancel sends nothing; Send checks the screen again and sends the keys
// only while the same answer is on show under the same number.
func TestAgentChatPlanPanelAsksFirst(t *testing.T) {
	screen := chatPanelPlanScreen
	m, sent := chatPanelModel(t, chatPanelPlanLog, &screen)
	rows := chatPlain(m.agentChatRender(110, 30))
	for _, want := range []string{"PLAN", "# Cache plan", "1  Yes, and auto-accept edits", "o full plan"} {
		if !strings.Contains(rows, want) {
			t.Errorf("no %q in:\n%s", want, rows)
		}
	}
	var page, pageText string
	oldOpen := agentChatOpenPage
	agentChatOpenPage = func(_ *Model, name, text string) error { page, pageText = name, text; return nil }
	t.Cleanup(func() { agentChatOpenPage = oldOpen })
	chatKey(m, "o")
	if page != "Plan · Claude Code" || !strings.Contains(pageText, "2. Test it") {
		t.Errorf("o opened %q %q", page, pageText)
	}

	drainApply(m, chatKey(m, "2"))
	if !m.confirmOpen || m.confirmKind != confirmKindAgentChatAnswer || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	if len(*sent) != 0 {
		t.Fatalf("sent before the card: %q", *sent)
	}
	m.confirmOpen = false
	if _, ok := m.handleAIConfirm("cancel"); !ok || len(*sent) != 0 {
		t.Fatalf("Cancel sent %q (handled %v)", *sent, ok)
	}
	drainApply(m, chatKey(m, "2"))
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("send")
	drainApply(m, cmd)
	if got := strings.Join(*sent, "|"); got != "7:\x1b[B|7:\r" {
		t.Errorf("Send sent %q", got)
	}
	// The screen moved on before Send: nothing is sent.
	*sent = nil
	m.agentChatSyncChoices(m.agentChat)
	drainApply(m, chatKey(m, "3"))
	screen = []string{" ❯ 1. Yes", "   2. No", " Enter to confirm · Esc to cancel"}
	m.confirmOpen = false
	cmd, _ = m.handleAIConfirm("send")
	drainApply(m, cmd)
	if len(*sent) != 0 {
		t.Errorf("sent to a changed screen: %q", *sent)
	}
}

// Spec (CMP-14): a permission prompt (the newest call is a plain tool
// such as Bash) shows a NEEDS YOUR OK panel with the call; its answers go
// through the same Cancel-first card. A working tab shows no panel and
// number keys are typed into the box as before.
func TestAgentChatPermissionPanelAndNoPanel(t *testing.T) {
	log := `{"type":"user","sessionId":"s4","cwd":"/w","timestamp":"2026-11-05T10:00:00Z","message":{"role":"user","content":"Clean the build."}}
{"type":"assistant","sessionId":"s4","timestamp":"2026-11-05T10:00:05Z","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"b1","name":"Bash","input":{"command":"rm -rf build"}}]}}
`
	screen := []string{" Bash command", "   rm -rf build", " Do you want to proceed?", " ❯ 1. Yes", "   2. No, and tell Claude what to do differently (esc)"}
	m, sent := chatPanelModel(t, log, &screen)
	rows := chatPlain(m.agentChatRender(110, 30))
	for _, want := range []string{"NEEDS YOUR OK", "Bash  rm -rf build", "1  Yes", "2  No, and tell Claude what to do differently"} {
		if !strings.Contains(rows, want) {
			t.Errorf("no %q in:\n%s", want, rows)
		}
	}
	drainApply(m, chatKey(m, "1"))
	if !m.confirmOpen || m.confirmKind != confirmKindAgentChatAnswer || len(*sent) != 0 {
		t.Fatalf("permission: card %v kind %v sent %q", m.confirmOpen, m.confirmKind, *sent)
	}
	m.confirmOpen = false
	m.handleAIConfirm("cancel")

	m.terminalTabs[0].AgentState = agents.Working
	m.agentChatSyncChoices(m.agentChat)
	if rows := chatPlain(m.agentChatRender(110, 30)); strings.Contains(rows, "NEEDS YOUR OK") {
		t.Errorf("panel while working:\n%s", rows)
	}
	chatKey(m, "1")
	if m.agentChat.input != "1" || m.confirmOpen {
		t.Errorf("working: input %q card %v", m.agentChat.input, m.confirmOpen)
	}
}
