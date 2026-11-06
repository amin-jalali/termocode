package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agentlog/agentlogtest"
)

// Spec (AGX-9): the resume flag is checked against the CLI's own --help
// text: claude --resume <id>, codex resume <id>; an id that is not a
// plain session id is never typed into a shell.
func TestAgentResumeLine(t *testing.T) {
	if !agentResumeSupported(agentlog.Claude, "Options:\n  -r, --resume [sessionId]  Resume a conversation\n") ||
		agentResumeSupported(agentlog.Claude, "Options:\n  -c, --continue\n") {
		t.Error("claude --help check")
	}
	if !agentResumeSupported(agentlog.Codex, "Commands:\n  exec    Run non-interactively\n  resume  Resume a previous session\n") ||
		agentResumeSupported(agentlog.Codex, "Commands:\n  exec    Run\n") {
		t.Error("codex --help check")
	}
	for _, c := range []struct {
		cli  agentlog.CLI
		id   string
		want string
	}{
		{agentlog.Claude, "3f9a2c1e-0000-4000-8000-000000000001", "claude --resume 3f9a2c1e-0000-4000-8000-000000000001"},
		{agentlog.Codex, "0199aaaa-bbbb-7ccc-8ddd-eeeeeeee0001", "codex resume 0199aaaa-bbbb-7ccc-8ddd-eeeeeeee0001"},
		{agentlog.Claude, "x; rm -rf ~", ""},
		{agentlog.Codex, "$(id)", ""},
		{agentlog.Claude, "", ""},
	} {
		if got, _ := agentResumeLine(c.cli, c.id); got != c.want {
			t.Errorf("resume line %s %q = %q, want %q", c.cli, c.id, got, c.want)
		}
	}
}

// sessionsTestModel opens Agent: Sessions… over the fixtures, with both
// CLIs installed and resume supported.
func sessionsTestModel(t *testing.T) *Model {
	t.Helper()
	m, _ := chatTestModel(t)
	oldRoot := agentSessionsRoot
	agentSessionsRoot = func() string { return agentlogtest.Folder }
	t.Cleanup(func() { agentSessionsRoot = oldRoot })
	fakeAgentLookPath(t, "claude", "codex")
	old := agentResumeCheck
	agentResumeCheck = func(agentlog.CLI) bool { return true }
	t.Cleanup(func() { agentResumeCheck = old })
	return m
}

// Spec (AGX-9, D1): Agent: Sessions… asks first while agent_transcripts
// is off (nothing is read), the AI cannot open it. On, it lists this
// project's sessions of both CLIs, newest first: time and first prompt,
// then the CLI, the prompts and the tokens. The index is built off the UI
// thread.
func TestAgentSessionsPicker(t *testing.T) {
	m := sessionsTestModel(t)
	if p, ok := aiActionPolicyFor(agentSessionsPaletteID); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
	found := false
	for _, it := range tasksPaletteItems() {
		if it.ID == agentSessionsPaletteID && it.Title == "Agent: Sessions…" {
			found = true
		}
	}
	if !found {
		t.Error("no Agent: Sessions… palette row")
	}
	m.dispatchTasksPalette(agentSessionsPaletteID)
	if !m.confirmOpen || m.confirmKind != confirmKindAgentTranscripts || m.pickerOpen {
		t.Fatalf("off: card %v kind %v picker %v", m.confirmOpen, m.confirmKind, m.pickerOpen)
	}
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("on")
	if !m.pickerOpen || m.pickerKind != pickerKindAgentSessions {
		t.Fatalf("Turn on: picker %v kind %v", m.pickerOpen, m.pickerKind)
	}
	runApply(m, cmd)
	rows := agentPickRowsText(m.agentSessionItems())
	if !strings.Contains(rows, "| Add the pinned row as in the issue. Run go test. | ") || !strings.Contains(rows, " · Claude Code · 1 prompt · 103 k tok\n") ||
		!strings.Contains(rows, "| Fix the failing test in queue.go | ") || !strings.Contains(rows, " · Codex · 1 prompt · 20 k tok\n") {
		t.Errorf("rows:\n%s", rows)
	}
}

// Spec (AGX-9): Enter resumes the session in a new terminal tab in its
// folder with the CLI's resume command; → opens it read only in the chat
// view; a CLI whose --help has no resume says "resume not supported" on
// the row and Enter only tells so.
func TestAgentSessionsResumeAndRead(t *testing.T) {
	m := sessionsTestModel(t)
	setAgentTranscripts(t, "read")
	runApply(m, m.openAgentSessions())
	var ran []string
	old := agentSessionsRun
	agentSessionsRun = func(_ *Model, dir, line, agentID string) { ran = append(ran, dir+" | "+line+" | "+agentID) }
	t.Cleanup(func() { agentSessionsRun = old })
	id := ""
	for _, it := range m.agentSessionItems() {
		if strings.Contains(it.Hint, "Claude Code") {
			id = it.ID
		}
	}
	if id == "" {
		t.Fatal("no Claude Code row")
	}
	m.handleAIPickerSelect(id)
	if strings.Join(ran, "\n") != "/work/demo | claude --resume 3f9a2c1e-0000-4000-8000-000000000001 | claude" {
		t.Errorf("resume ran %q", ran)
	}
	m.pickerOpen, m.pickerKind = true, pickerKindAgentSessions
	m.onAgentSessionsAction(id)
	if c := m.agentChat; c == nil || !c.readOnly || !strings.HasSuffix(c.path, "3f9a2c1e-0000-4000-8000-000000000001.jsonl") {
		t.Fatalf("read only chat %+v", m.agentChat)
	}
	agentResumeCheck = func(agentlog.CLI) bool { return false }
	m.agentSessionsResume = map[agentlog.CLI]bool{}
	runApply(m, m.openAgentSessions())
	ran = nil
	for _, it := range m.agentSessionItems() {
		if it.ID == id && !strings.Contains(it.Hint, "resume not supported") {
			t.Errorf("row %q", it.Hint)
		}
	}
	m.handleAIPickerSelect(id)
	if len(ran) != 0 {
		t.Errorf("ran %q without resume support", ran)
	}
}
