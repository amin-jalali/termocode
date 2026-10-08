package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/debounce"
	"github.com/amin-jalali/termocode/internal/statusbar"
)

// aiTestModel builds a Model with an isolated config dir and no AI keys in
// the environment (so the provider is unconfigured unless a test sets one).
func aiTestModel(t *testing.T) Model {
	t.Helper()
	withConfigDir(t)
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY", "TERMOCODE_AI_PROVIDER", "TERMOCODE_AI_API_KEY", "TERMOCODE_AI_MODEL", "TERMOCODE_AI_BASE_URL", "OLLAMA_HOST"} {
		t.Setenv(k, "")
	}
	return Model{w: 120, h: 40, debounce: debounce.New(), ai: newAIState(), actionsWidth: defaultActionsWidth}
}

// fakeAIProvider streams canned text without any network.
type fakeAIProvider struct{ reply string }

func (fakeAIProvider) Name() string        { return "fake" }
func (fakeAIProvider) Configured() bool    { return true }
func (fakeAIProvider) SupportsTools() bool { return true }
func (f fakeAIProvider) Stream(_ context.Context, _ ai.Request) <-chan ai.Chunk {
	ch := make(chan ai.Chunk, 4)
	half := len(f.reply) / 2
	ch <- ai.Chunk{Delta: f.reply[:half]}
	ch <- ai.Chunk{Delta: f.reply[half:]}
	ch <- ai.Chunk{Done: true}
	close(ch)
	return ch
}

func TestAIUnconfiguredEntriesOnlyToast(t *testing.T) {
	m := aiTestModel(t)
	if m.aiConfigured() {
		t.Fatal("test env must be unconfigured")
	}
	for _, e := range aiPaletteItems() {
		switch e.id {
		case "ai-configure", "ai-sign-in-claude", "ai-sign-out-claude", "ai-credentials", "ai-cancel",
			"ai-discard-proposal", "ai-history", "ai-pick-model", "ai-auto-approve":
			continue
		}
		mm := m
		mm.toast = m.toast
		cmd, ok := mm.dispatchAIPalette(e.id)
		if !ok || cmd == nil {
			t.Errorf("%s: want a toast cmd", e.id)
		}
		if mm.toast.Empty() {
			t.Errorf("%s: no toast", e.id)
		}
		if mm.pickerOpen || mm.promptOpen || mm.confirmOpen || mm.actionsOpen || mm.ai.streaming {
			t.Errorf("%s: changed more than a toast", e.id)
		}
	}
	label, st := m.aiStatus()
	if st != statusbar.AIUnconfigured || label == "" {
		t.Errorf("badge %q %v", label, st)
	}
}

func TestAIChatStreamsAndSaves(t *testing.T) {
	m := aiTestModel(t)
	s := m.ai
	s.provider = fakeAIProvider{reply: "Hello from the fake model"}
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	s.attachments = []aiAttachment{{Label: "a.go:1-2", Text: "package a"}}
	cmd := m.sendAIChat("hi there")
	if !s.streaming || cmd == nil {
		t.Fatal("chat did not start")
	}
	for i := 0; cmd != nil && i < 50; i++ {
		msg := cmd()
		if msg == nil {
			break
		}
		next, ok := m.handleAIMsg(msg)
		if !ok {
			t.Fatalf("unrouted msg %T", msg)
		}
		if _, done := msg.(aiChatDoneMsg); done {
			break
		}
		cmd = next
	}
	if s.streaming {
		t.Fatal("still streaming")
	}
	msgs := s.conv.Messages
	if len(msgs) != 2 || msgs[1].Text != "Hello from the fake model" || !strings.Contains(msgs[0].Context, "package a") {
		t.Fatalf("transcript %+v", msgs)
	}
	got, err := s.store.Load(s.conv.ID)
	if err != nil || len(got.Messages) != 2 {
		t.Fatalf("chat not saved: %v", err)
	}
	if loadSession().AILastChat != s.conv.ID {
		t.Error("session does not remember the chat")
	}
	// A fresh state restores it.
	if again := newAIState(); again.conv.ID != s.conv.ID {
		t.Error("last chat not restored")
	}
}

func TestAIPanelRendersExactWidth(t *testing.T) {
	m := aiTestModel(t)
	m.actionsOpen, m.actionsPinned = true, true
	s := m.ai
	s.provider = fakeAIProvider{}
	s.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "Make it faster please, this is a long line that must wrap inside the panel", Context: "Attached x.go:1-3:\n..."},
		{Role: ai.StoredAssistant, Text: "Sure:\n```go\n\tfor i := 0; i < len(xs); i++ { total += xs[i] * weightForIndexThatIsLong(i) }\n```\nDone ✦"},
		{Role: ai.StoredTool, Text: "⚙ Read x.go → ok"},
		{Role: ai.StoredError, Text: "HTTP 500"},
	}
	s.input = []rune("multi\nline input with 日本語 text that wraps around")
	s.caret = 8
	s.focused = true
	s.attachments = []aiAttachment{{Label: "b.go"}}
	for _, w := range []int{24, 32, 50} {
		out := m.renderActionsPanel(w, m.h)
		rows := strings.Split(out, "\n")
		if len(rows) != m.h {
			t.Fatalf("w=%d: %d rows", w, len(rows))
		}
		for i, r := range rows {
			if lipgloss.Width(r) != w {
				t.Fatalf("w=%d row %d width %d: %q", w, i, lipgloss.Width(r), r)
			}
		}
	}
	// A code block gets Copy / Apply buttons.
	found := false
	for _, l := range m.aiTranscript(40) {
		for _, b := range l.buttons {
			if strings.HasPrefix(b.id, "apply:") {
				found = true
			}
		}
	}
	if !found {
		t.Error("no Apply button for the code block")
	}
}

func TestAIInputWrappedCaret(t *testing.T) {
	rows, r, c := aiInputWrapped([]rune("abcdef"), 6, 3)
	if len(rows) != 3 || r != 2 || c != 0 {
		t.Errorf("full-row caret: %q r=%d c=%d", rows, r, c)
	}
	rows, r, c = aiInputWrapped([]rune("ab\ncd"), 4, 10)
	if len(rows) != 2 || r != 1 || c != 1 {
		t.Errorf("newline caret: %q r=%d c=%d", rows, r, c)
	}
	if s, e := aiVisibleRange(100, 10, 5); s != 85 || e != 95 {
		t.Errorf("visible %d %d", s, e)
	}
	if s, e := aiVisibleRange(4, 10, 3); s != 0 || e != 4 {
		t.Errorf("short visible %d %d", s, e)
	}
}

func TestAIPanelKeysAndMouse(t *testing.T) {
	m := aiTestModel(t)
	m.actionsOpen, m.actionsPinned = true, true
	s := m.ai
	if _, ok := m.routeAIPanelKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}); ok {
		t.Fatal("unfocused panel must not take keys")
	}
	// Click the input row focuses the panel.
	g := aiGeom(m.h)
	x := m.w - m.actionsColumnWidth() + 3
	if _, ok := m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: g.inputTop, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}); !ok || !s.focused {
		t.Fatal("click on input should focus")
	}
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("hé")},
		{Type: tea.KeySpace},
		{Type: tea.KeyRunes, Runes: []rune("yo")},
		{Type: tea.KeyLeft},
		{Type: tea.KeyBackspace},
	} {
		if _, ok := m.routeAIPanelKey(k); !ok {
			t.Fatalf("key %v not handled", k)
		}
	}
	if string(s.input) != "hé o" || s.caret != 3 {
		t.Fatalf("input %q caret %d", string(s.input), s.caret)
	}
	if _, ok := m.routeAIPanelKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a"), Alt: true}); ok {
		t.Error("alt keys must fall through to the keymap")
	}
	// Enter without a provider keeps the draft and toasts.
	if cmd, ok := m.routeAIPanelKey(tea.KeyMsg{Type: tea.KeyEnter}); !ok || cmd == nil || string(s.input) != "hé o" {
		t.Error("enter unconfigured should toast and keep the draft")
	}
	// Click outside blurs.
	m.handleAIPanelMouse(tea.MouseMsg{X: 5, Y: 5, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if s.focused {
		t.Error("click outside should blur")
	}
	// F6 cycle: editor → AI → explorer.
	m.focus = FocusEditor
	m.showExp = true
	if !m.aiFocusSwap() || !s.focused {
		t.Fatal("F6 from editor should focus AI panel")
	}
	if !m.aiFocusSwap() || s.focused || m.focus != FocusExplorer {
		t.Fatal("F6 from AI panel should go to explorer")
	}
}

func TestAISettingsRows(t *testing.T) {
	m := aiTestModel(t)
	cmd, ok := m.applyAISetting("setting-ai-provider", "ollama")
	if !ok || cmd == nil {
		t.Fatal("provider row")
	}
	if !m.aiConfigured() || m.ai.cfg.Kind != ai.KindOllama {
		t.Fatalf("ollama should configure AI: %+v", m.ai.cfg)
	}
	if v, _ := aiSettingValue("setting-ai-provider"); v != "ollama" {
		t.Errorf("value %q", v)
	}
	if _, ok := m.applyAISetting("setting-ai-provider", "bogus"); !ok || m.ai.cfg.Kind != ai.KindOllama {
		t.Error("bogus provider must be rejected")
	}
	if _, ok := m.applyAISetting("setting-theme", "x"); ok {
		t.Error("non-AI rows are not ours")
	}
	cats := settingsCategories()
	if cats["setting-ai-model"] != "AI" {
		t.Error("AI settings category")
	}
	label, st := m.aiStatus()
	if st != statusbar.AIIdle || label == "" {
		t.Errorf("badge %q %v", label, st)
	}
}

func TestAIToolConfirmAlwaysPersists(t *testing.T) {
	m := aiTestModel(t)
	reply := make(chan bool, 1)
	m.ai.chatGen = 7
	m.onAIChatConfirm(aiChatConfirmMsg{gen: 7, tool: ai.Tool{Name: "run_command", Class: ai.ClassRun}, human: "Run: ls", reply: reply, ch: make(chan tea.Msg)})
	if !m.confirmOpen || m.confirmKind != confirmKindAITool {
		t.Fatal("confirm not shown")
	}
	m.confirmOpen = false
	m.handleConfirmSelect("always")
	if ok := <-reply; !ok {
		t.Fatal("always must allow")
	}
	path, _ := ai.ConfigPath()
	if s := ai.LoadSettings(path); len(s.AutoApprove) != 1 || s.AutoApprove[0] != ai.ClassRun {
		t.Fatalf("auto-approve not saved: %+v", s)
	}
	// Esc on a tool confirm denies.
	reply2 := make(chan bool, 1)
	m.onAIChatConfirm(aiChatConfirmMsg{gen: 7, tool: ai.Tool{Name: "delete"}, reply: reply2, ch: make(chan tea.Msg)})
	m.onAIConfirmClosed()
	if ok := <-reply2; ok {
		t.Fatal("esc must deny")
	}
}

func TestErrorLogRedactsSecrets(t *testing.T) {
	dir := withConfigDir(t)
	recordError("request failed with key sk-ant-api03-SECRETSECRETSECRET")
	b, err := os.ReadFile(filepath.Join(dir, "termocode", "errors.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SECRETSECRET") {
		t.Fatalf("secret leaked to errors.log: %s", b)
	}
}

func TestAIProposalNoChangeAndLines(t *testing.T) {
	m := aiTestModel(t)
	tgt := aiRangeTarget{full: []string{"a", "b", "c"}, start: 1, end: 2, path: "/x/f.go"}
	m.openAIProposal(aiProposal{target: tgt, repl: []string{"b"}})
	if m.ai.proposal != nil || m.confirmOpen {
		t.Fatal("identical proposal must not open")
	}
	m.openAIProposal(aiProposal{target: tgt, repl: []string{"B", "B2"}, title: "t"})
	if m.ai.proposal == nil || !m.confirmOpen || m.confirmKind != confirmKindAIProposal {
		t.Fatal("proposal dialog not opened")
	}
	if !strings.Contains(aiEditPrompt(tgt, "do it"), "Range to replace (lines 2-2)") {
		t.Error("edit prompt range")
	}
	m.discardAIProposal(false)
	if m.ai.proposal != nil {
		t.Error("discard")
	}
}
