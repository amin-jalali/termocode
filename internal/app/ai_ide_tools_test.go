package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/toast"
)

// scriptedAIProvider replays one canned reply per Stream call (native
// tool calls on the final chunk) and records what the agent sent.
type scriptedAIProvider struct {
	mu      sync.Mutex
	replies []ai.Chunk
	calls   int
	seen    [][]ai.Message
}

func (*scriptedAIProvider) Name() string        { return "fake" }
func (*scriptedAIProvider) Configured() bool    { return true }
func (*scriptedAIProvider) SupportsTools() bool { return true }
func (p *scriptedAIProvider) Stream(_ context.Context, req ai.Request) <-chan ai.Chunk {
	p.mu.Lock()
	p.seen = append(p.seen, append([]ai.Message(nil), req.Messages...))
	r := ai.Chunk{Delta: "done"}
	if p.calls < len(p.replies) {
		r = p.replies[p.calls]
	}
	p.calls++
	p.mu.Unlock()
	ch := make(chan ai.Chunk, 2)
	if r.Delta != "" {
		ch <- ai.Chunk{Delta: r.Delta}
	}
	ch <- ai.Chunk{Done: true, ToolCalls: r.ToolCalls}
	close(ch)
	return ch
}

// toolResults returns every tool result the agent fed back to the model.
func (p *scriptedAIProvider) toolResults() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	if n := len(p.seen); n > 0 {
		for _, m := range p.seen[n-1] {
			if m.Role == ai.RoleTool {
				out = append(out, m.Content)
			}
		}
	}
	return out
}

func toolCall(name string, args map[string]any) ai.Chunk {
	return ai.Chunk{ToolCalls: []ai.ToolCall{{ID: "c1", Name: name, Args: args}}}
}

// nextAIMsg runs a chat cmd with a timeout (it blocks on the channel).
func nextAIMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case msg := <-got:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("chat message did not arrive in 5s")
		return nil
	}
}

// driveAIChat routes chat messages through handleAIMsg until the turn is
// done. onConfirm answers tool confirms (nil: deny).
func driveAIChat(t *testing.T, m *Model, cmd tea.Cmd, onConfirm func(aiChatConfirmMsg) string) {
	t.Helper()
	for i := 0; cmd != nil && i < 200; i++ {
		msg := nextAIMsg(t, cmd)
		if msg == nil {
			return
		}
		if c, ok := msg.(aiChatConfirmMsg); ok {
			next, _ := m.handleAIMsg(msg)
			id := "deny"
			if onConfirm != nil {
				id = onConfirm(c)
			}
			m.answerAIToolConfirm(id)
			m.confirmOpen = false
			_ = next
			cmd = waitAIChat(m.ai.chatCh)
			continue
		}
		next, ok := m.handleAIMsg(msg)
		if !ok {
			t.Fatalf("unrouted msg %T", msg)
		}
		if _, done := msg.(aiChatDoneMsg); done {
			return
		}
		_ = next // may batch other cmds (toast timers); read the channel directly
		cmd = waitAIChat(m.ai.chatCh)
	}
}

// aiIDETestModel is aiTestModel in a temp workspace (the agent's root is
// the working directory).
func aiIDETestModel(t *testing.T, p ai.Provider) (*Model, string) {
	t.Helper()
	m := aiTestModel(t)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	t.Chdir(dir)
	m.ai.provider = p
	m.ai.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	return &m, dir
}

func aiTestNvim(t *testing.T, m *Model) *nvim.Client {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(100, 30); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	m.nvim = c
	return c
}

func TestAIOpenFileToolOpensAndFocusesEditor(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{
		toolCall("open_file", map[string]any{"path": "sub/x.md", "line": 2}),
		{Delta: "opened"},
	}}
	m, dir := aiIDETestModel(t, p)
	c := aiTestNvim(t, m)
	_ = os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "sub", "x.md"), []byte("one\ntwo\nthree\n"), 0o644)
	m.focus = FocusExplorer
	m.ai.focused = true

	confirms := 0
	driveAIChat(t, m, m.sendAIChat("open it"), func(aiChatConfirmMsg) string { confirms++; return "deny" })
	if confirms != 0 {
		t.Fatalf("open_file asked for confirmation %d times", confirms)
	}
	name, _ := c.EvalLuaString(`return vim.api.nvim_buf_get_name(0)`)
	if name != filepath.Join(dir, "sub", "x.md") {
		t.Fatalf("current buffer %q, results %v", name, p.toolResults())
	}
	line, _ := c.EvalLuaString(`return tostring(vim.api.nvim_win_get_cursor(0)[1])`)
	if line != "2" {
		t.Errorf("cursor line %s, want 2", line)
	}
	if m.focus != FocusEditor || m.ai.focused {
		t.Errorf("editor not focused: focus=%v chatFocused=%v", m.focus, m.ai.focused)
	}
	if r := p.toolResults(); len(r) != 1 || !strings.Contains(r[0], "sub/x.md") {
		t.Errorf("tool result %v", r)
	}
}

func TestAIOpenFileToolRefusesEscapeAndMissing(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"../outside.txt", "outside"},
		{"nope.txt", "does not exist"},
	} {
		p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("open_file", map[string]any{"path": tc.path})}}
		m, _ := aiIDETestModel(t, p)
		driveAIChat(t, m, m.sendAIChat("open it"), nil)
		r := p.toolResults()
		if len(r) != 1 || !strings.Contains(r[0], "error") || !strings.Contains(r[0], tc.want) {
			t.Errorf("%s: result %v, want an error with %q", tc.path, r, tc.want)
		}
	}
}

func TestAIGetEditorStateTool(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("get_editor_state", nil), {Delta: "ok"}}}
	m, dir := aiIDETestModel(t, p)
	c := aiTestNvim(t, m)
	path := filepath.Join(dir, "a.go")
	_ = os.WriteFile(path, []byte("package a\n\nfunc A() {\n\treturn\n}\n"), 0o644)
	m.openFileAtLine(path, 3)
	_ = c.Command("call cursor(3, 6)")
	_ = c.Command("normal! ix")
	driveAIChat(t, m, m.sendAIChat("where am I"), nil)
	r := p.toolResults()
	if len(r) != 1 {
		t.Fatalf("results %v", r)
	}
	var st struct {
		File     string   `json:"file"`
		Line     int      `json:"line"`
		Column   int      `json:"column"`
		Tabs     []string `json:"tabs"`
		Modified []string `json:"modified"`
		Focus    string   `json:"focus"`
	}
	if err := json.Unmarshal([]byte(r[0]), &st); err != nil {
		t.Fatalf("not json: %v\n%s", err, r[0])
	}
	if st.File != "a.go" || st.Line != 3 || st.Column != 6 {
		t.Errorf("state %+v", st)
	}
	if len(st.Tabs) != 1 || st.Tabs[0] != "a.go" || len(st.Modified) != 1 || st.Focus == "" {
		t.Errorf("tabs/modified/focus %+v", st)
	}

	// A visual selection is reported with its text.
	_ = c.Input("<Esc>gg0vj$")
	var out string
	for i := 0; i < 50 && !strings.Contains(out, "selection"); i++ {
		time.Sleep(10 * time.Millisecond)
		out, _ = m.aiEditorState(func(p string) string { r, _ := filepath.Rel(dir, p); return r })
	}
	if !strings.Contains(out, `"start_line":1`) || !strings.Contains(out, `"text":"package a\n"`) {
		t.Errorf("selection not reported: %s", out)
	}
}

// execute_action follows aiActionPolicies: never ids are refused without
// asking, auto ids run at once, confirm ids ask with their class (none
// for destructive ones, so no "Always allow").
func TestAIExecuteActionPolicy(t *testing.T) {
	run := func(id string) (asked []string, results []string) {
		t.Helper()
		p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("execute_action", map[string]any{"id": id})}}
		m, _ := aiIDETestModel(t, p)
		driveAIChat(t, m, m.sendAIChat(id), func(c aiChatConfirmMsg) string {
			asked = append(asked, c.tool.Class)
			return "deny"
		})
		return asked, p.toolResults()
	}
	// Never: refused, with the reason, without asking.
	if asked, r := run("quit"); len(asked) != 0 || len(r) != 1 || !strings.Contains(r[0], "not allowed") ||
		!strings.Contains(r[0], aiActionPolicies["quit"].Reason) {
		t.Fatalf("quit: asked=%v result %v", asked, r)
	}
	// Unknown: refused, points to list_actions.
	if asked, r := run("no-such"); len(asked) != 0 || len(r) != 1 || !strings.Contains(r[0], "list_actions") {
		t.Fatalf("unknown: asked=%v result %v", asked, r)
	}
	// Auto: runs without asking.
	if asked, r := run("git-refresh"); len(asked) != 0 || len(r) != 1 || r[0] != "ran git-refresh" {
		t.Fatalf("git-refresh: asked=%v result %v", asked, r)
	}
	// Confirm with a class.
	if asked, r := run("git-stage"); len(asked) != 1 || asked[0] != ai.ClassGit || !strings.Contains(r[0], "declined") {
		t.Fatalf("git-stage: asked=%v result %v", asked, r)
	}
	// Destructive: asks with no class.
	if asked, _ := run("git-push"); len(asked) != 1 || asked[0] != "" {
		t.Fatalf("git-push: asked=%v", asked)
	}
}

// list_actions runs without asking and lists live commands, never ids
// left out.
func TestAIListActionsTool(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("list_actions", map[string]any{"query": "fold"})}}
	m, _ := aiIDETestModel(t, p)
	asked := 0
	driveAIChat(t, m, m.sendAIChat("fold"), func(aiChatConfirmMsg) string { asked++; return "deny" })
	r := p.toolResults()
	if asked != 0 || len(r) != 1 || !strings.Contains(r[0], "fold-all: View: Fold All (View; auto)") {
		t.Fatalf("asked=%d result %v", asked, r)
	}
}

// Stop while a tool waits on the UI: the agent returns at once, and the
// late call is not run.
func TestAIIDECallCancelDoesNotHang(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("notify", map[string]any{"title": "hello"})}}
	m, _ := aiIDETestModel(t, p)
	cmd := m.sendAIChat("say hello")
	var call tea.Msg
	for i := 0; i < 20; i++ {
		msg := nextAIMsg(t, cmd)
		if _, ok := msg.(aiIDECallMsg); ok {
			call = msg
			break
		}
		cmd, _ = m.handleAIMsg(msg)
	}
	if call == nil {
		t.Fatal("no IDE call reached Update")
	}
	m.aiCancelAll()
	// The agent goroutine finishes: the channel ends with a done message.
	ch := call.(aiIDECallMsg).ch
	deadline := time.After(3 * time.Second)
	for done := false; !done; {
		select {
		case msg, ok := <-ch:
			if !ok {
				done = true
			}
			_ = msg
		case <-deadline:
			t.Fatal("agent goroutine still running after cancel")
		}
	}
	// The stale call is ignored.
	m.toast = toast.New() // drop the "AI stopped" toast
	m.handleAIMsg(call)
	if !m.toast.Empty() {
		t.Error("a cancelled IDE call still ran")
	}
	if r := p.toolResults(); len(r) != 0 {
		t.Errorf("model was called again after cancel: %v", r)
	}
}
