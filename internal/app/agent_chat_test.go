package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agentlog/agentlogtest"
	"github.com/amin-jalali/termocode/internal/agents"
)

func chatFixture(t *testing.T, cli agentlog.CLI, rel string) agentlog.Session {
	t.Helper()
	s, err := agentlog.ReadFile(cli, filepath.Join("..", "agentlog", rel))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func chatPlain(rows []string) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(strings.TrimRight(ansi.Strip(r), " ") + "\n")
	}
	return b.String()
}

// Spec (AGX-8, plan 2.6): the chat view draws the session as a chat:
// "you" rows for the prompts, ✦ rows for the answers, and one row per
// tool call with its summary (paths relative to the session's folder)
// and the result's mark (✓, ✘ on an error, … while it runs). Results are
// collapsed: their text is not drawn. Narrow: the file's base name only.
func TestAgentChatBodyRows(t *testing.T) {
	s := chatFixture(t, agentlog.Claude, "testdata/claude/-work-demo/3f9a2c1e-0000-4000-8000-000000000001.jsonl")
	got := chatPlain(agentChatBody(s, 110))
	for _, want := range []string{
		"you   Add the pinned row as in the issue. Run go test.\n",
		aiSpark() + "     I will add a PINNED group to the worktree list.\n",
		"      ▸ Read  internal/app/worktrees_view.go  ✓\n",
		"      ▸ Edit  internal/app/worktrees_view.go +4 -1  ✓\n",
		"      ▸ Bash  go test ./internal/app/ -run Worktree  ✓\n",
		aiSpark() + "     Done. The pinned rows sit first; 2 tests added.\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no row %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "func worktreesView") {
		t.Errorf("a tool result is drawn open:\n%s", got)
	}
	narrow := chatPlain(agentChatBody(s, 60))
	if !strings.Contains(narrow, "▸ Edit worktrees_view.go +4 -1 ✓\n") {
		t.Errorf("narrow rows:\n%s", narrow)
	}
	for _, r := range agentChatBody(s, 60) {
		if w := ansi.StringWidth(r); w > 60 {
			t.Errorf("row %q is %d cells wide", ansi.Strip(r), w)
		}
	}
	c := chatFixture(t, agentlog.Codex, "testdata/codex/sessions/2026/11/05/rollout-2026-11-05T11-00-00-0199aaaa-bbbb-7ccc-8ddd-eeeeeeee0001.jsonl")
	got = chatPlain(agentChatBody(c, 110))
	if !strings.Contains(got, "▸ shell  go test ./...  ✘\n") || !strings.Contains(got, "▸ apply_patch  queue.go +2 -1  ✓\n") {
		t.Errorf("codex rows:\n%s", got)
	}
	// A call without its result yet is running.
	s.Turns = s.Turns[:len(s.Turns)-2]
	if got := chatPlain(agentChatBody(s, 110)); !strings.Contains(got, "▸ Bash  go test ./internal/app/ -run Worktree  …\n") {
		t.Errorf("running call:\n%s", got)
	}
}

// Spec (AGX-8): the line under the chat says what the agent does (from
// its screen: working, waiting for you, done) and the session's numbers:
// the context (of the window when known), tokens in and out, and the plan
// limit with its reset time when the CLI records it.
func TestAgentChatStatusLine(t *testing.T) {
	s := chatFixture(t, agentlog.Claude, "testdata/claude/-work-demo/3f9a2c1e-0000-4000-8000-000000000001.jsonl")
	if got := agentChatStatus(s, agents.Waiting, "Claude Code"); got != "◐ Claude Code is waiting for you · answer in the terminal (v) · context 82 k · 102 k in · 440 out" {
		t.Errorf("claude waiting: %q", got)
	}
	if got := agentChatStatus(s, agents.Working, "Claude Code"); !strings.HasPrefix(got, "● working · context 82 k") {
		t.Errorf("claude working: %q", got)
	}
	c := chatFixture(t, agentlog.Codex, "testdata/codex/sessions/2026/11/05/rollout-2026-11-05T11-00-00-0199aaaa-bbbb-7ccc-8ddd-eeeeeeee0001.jsonl")
	reset := c.Usage.LimitReset.Local().Format("15:04")
	if got := agentChatStatus(c, agents.Done, "Codex"); got != "✓ done · context 9 k of 272 k · 18 k in · 2 k out · limit 62% ↻ "+reset {
		t.Errorf("codex done: %q", got)
	}
}

// chatTestModel is a model with one Claude Code tab in the fixture folder
// and the fixture sessions in a fake HOME.
func chatTestModel(t *testing.T) (*Model, string) {
	t.Helper()
	home, _ := transcriptHome(t)
	m := aiTestModel(t)
	m.terminalTabs = []terminalTab{{BufID: 7, Name: "claude", Cwd: agentlogtest.Folder, Agent: "claude", AgentState: agents.Working}}
	m.terminalActiveTab = 0
	m.termOpen = true
	return &m, home
}

// runApply runs cmd and applies the background result it returns.
func runApply(m *Model, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	if a, ok := cmd().(applyMsg); ok {
		return a(m)
	}
	return nil
}

// Spec (AGX-8, D1): Agent: Chat View on a Claude Code / Codex tab. While
// agent_transcripts is off it asks first (Not now first; Not now and Esc
// leave it off and open nothing); Turn on writes the setting. On another
// tab it says it needs an agent tab. The AI cannot open it.
func TestAgentChatOpenAsksFirst(t *testing.T) {
	m, _ := chatTestModel(t)
	if p, ok := aiActionPolicyFor(agentChatPaletteID); !ok || p.Gate != aiGateNever {
		t.Errorf("%s policy %+v %v", agentChatPaletteID, p, ok)
	}
	found := false
	for _, it := range tasksPaletteItems() {
		if it.ID == agentChatPaletteID && it.Title == "Agent: Chat View" {
			found = true
		}
	}
	if !found {
		t.Error("no Agent: Chat View palette row")
	}
	m.dispatchTasksPalette(agentChatPaletteID)
	if !m.confirmOpen || m.confirmKind != confirmKindAgentTranscripts || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	m.confirmOpen = false
	if _, ok := m.handleAIConfirm("cancel"); !ok || agentTranscriptsOn() || m.agentChat != nil {
		t.Fatalf("Not now: on %v chat %v", agentTranscriptsOn(), m.agentChat)
	}
	m.dispatchTasksPalette(agentChatPaletteID)
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("on")
	if !agentTranscriptsOn() || m.agentChat == nil || m.agentChat.buf != 7 {
		t.Fatalf("Turn on: on %v chat %+v", agentTranscriptsOn(), m.agentChat)
	}
	if cmd == nil {
		t.Fatal("no load after Turn on")
	}
	// Another kind of tab.
	m.agentChat = nil
	m.terminalTabs[0].Agent = "aider"
	m.dispatchTasksPalette(agentChatPaletteID)
	if m.agentChat != nil || m.confirmOpen {
		t.Error("chat view on an Aider tab")
	}
}

// Spec (AGX-8): the view loads the tab's session file off the UI thread
// and follows it (a 1 s poll while the view is open): a line the CLI adds
// shows on the next poll. Closed, the poll stops.
func TestAgentChatLoadsAndFollows(t *testing.T) {
	m, home := chatTestModel(t)
	setAgentTranscripts(t, "read")
	cmd := m.openAgentChatView()
	if m.agentChat == nil {
		t.Fatal("no chat state")
	}
	runApply(m, cmd)
	c := m.agentChat
	if c.err != nil || c.sess.Prompts() != 1 || c.sess.ID != "3f9a2c1e-0000-4000-8000-000000000001" {
		t.Fatalf("loaded: err %v prompts %d id %q", c.err, c.sess.Prompts(), c.sess.ID)
	}
	file := filepath.Join(home, ".claude", "projects", "-work-demo", "3f9a2c1e-0000-4000-8000-000000000001.jsonl")
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"type":"user","sessionId":"3f9a2c1e-0000-4000-8000-000000000001","cwd":"/work/demo","timestamp":"2026-11-05T10:02:00Z","message":{"role":"user","content":"Now add a test."}}` + "\n")
	f.Close()
	runApply(m, m.agentChatPollCmd(c.gen))
	if c.sess.Prompts() != 2 {
		t.Errorf("after the append: %d prompts", c.sess.Prompts())
	}
	m.closeAgentChat()
	if m.agentChat != nil || m.agentChatPollCmd(c.gen) != nil {
		t.Error("the poll goes on after close")
	}
}

// Spec (AGX-8): a shell without shell integration (sh, dash) never
// reports its folder, so the tab's folder is nvim's buffer name
// term://DIR//PID:CMD (from b:term_title). The chat view still finds the
// session of DIR, never "No session file yet".
func TestAgentChatFindsSessionOfANvimTermName(t *testing.T) {
	m, _ := chatTestModel(t)
	setAgentTranscripts(t, "read")
	m.terminalTabs[0].Cwd = "term://" + agentlogtest.Folder + "//4242:/bin/sh"
	runApply(m, m.openAgentChatView())
	c := m.agentChat
	if c == nil {
		t.Fatal("no chat state")
	}
	if c.missing || c.err != nil || c.sess.Prompts() != 1 {
		t.Fatalf("cwd %q: missing %v err %v prompts %d", c.cwd, c.missing, c.err, c.sess.Prompts())
	}
}

// Spec (AGX-8): a file the reader does not understand shows "cannot
// read this session" (the terminal is one key away), never a wrong chat.
func TestAgentChatUnknownFormat(t *testing.T) {
	m, home := chatTestModel(t)
	setAgentTranscripts(t, "read")
	file := filepath.Join(home, ".claude", "projects", "-work-demo", "3f9a2c1e-0000-4000-8000-000000000001.jsonl")
	if err := os.WriteFile(file, []byte("{\"format\":\"new\"}\n{\"format\":\"new\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runApply(m, m.openAgentChatView())
	rows := chatPlain(m.agentChatRender(100, 12))
	if !strings.Contains(rows, "cannot read this session") || !strings.Contains(rows, "v terminal") {
		t.Errorf("rows:\n%s", rows)
	}
	if strings.Contains(rows, "you ") {
		t.Errorf("a chat is drawn from an unknown file:\n%s", rows)
	}
}

// Spec (AGX-8): typed text goes to the tab when Enter is pressed (the
// user typed it: no card), then Enter for the CLI; v with an empty box
// shows the terminal again; a read-only view sends nothing.
func TestAgentChatKeys(t *testing.T) {
	m, _ := chatTestModel(t)
	setAgentTranscripts(t, "read")
	runApply(m, m.openAgentChatView())
	var sent []string
	old := agentChatSend
	agentChatSend = func(_ *Model, buf int, text string) { sent = append(sent, itoa(buf)+":"+text) }
	t.Cleanup(func() { agentChatSend = old })
	key := func(s string) tea.Cmd {
		var k tea.KeyMsg
		switch s {
		case "enter":
			k = tea.KeyMsg{Type: tea.KeyEnter}
		case "backspace":
			k = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
		}
		cmd, _ := agentChatKey(m, k)
		return cmd
	}
	for _, s := range []string{"r", "u", "n", "x", "backspace", " ", "v", "1"} {
		key(s)
	}
	if m.agentChat.input != "run v1" {
		t.Fatalf("input %q", m.agentChat.input)
	}
	cmd := key("enter")
	if strings.Join(sent, "|") != "7:run v1" || m.agentChat.input != "" {
		t.Errorf("sent %v, input %q", sent, m.agentChat.input)
	}
	if cmd == nil {
		t.Fatal("no Enter for the CLI after the text")
	}
	if a, ok := cmd().(applyMsg); ok {
		a(m)
	}
	if strings.Join(sent, "|") != "7:run v1|7:\r" {
		t.Errorf("sent %q", sent)
	}
	key("enter") // empty box: nothing
	if len(sent) != 2 {
		t.Errorf("an empty Enter sent %q", sent)
	}
	m.panelActive = panelKindAgentChat
	key("v")
	if m.panelActive != panelKindTerminal || m.terminalActiveTab != 0 {
		t.Errorf("v: panel %v tab %d", m.panelActive, m.terminalActiveTab)
	}
	m.agentChat.readOnly = true
	key("h")
	key("enter")
	if len(sent) != 2 {
		t.Errorf("read-only sent %q", sent)
	}
}
