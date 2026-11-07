package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/theme"
)

// CMP-28: several agent sessions on one worktree.

// The sessions list of a worktree has every agent tab working in it
// (also in a folder under it): the ones that wait for you first, then
// the working ones, then the rest; the last row starts one more.
func TestWorktreeSessionRows(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	st, _ := wtFixture(t)
	tabs := []wtTabAgent{
		{name: "Claude Code", cwd: "/src/app.worktrees/wt-sidebar", state: agents.Working},
		{name: "Claude Code 2", cwd: "/src/app.worktrees/wt-sidebar/sub", state: agents.Waiting},
		{name: "Aider", cwd: "/src/app", state: agents.Done},
		{name: "", cwd: "/src/app.worktrees/wt-sidebar"}, // a plain shell
		{name: "Codex", cwd: "/src/app.worktrees/wt-sidebar", state: agents.Done},
	}
	rows := wtSessionRows(st.entries, tabs, "/src/app.worktrees/wt-sidebar")
	var got []string
	for _, r := range rows {
		got = append(got, r.ID+"="+r.Title+"|"+r.Hint)
	}
	want := []string{
		"tab:1=Claude Code 2|waits for you",
		"tab:0=Claude Code|working",
		"tab:4=Codex|done",
		"new=+ New agent session…|starts one more agent in this worktree",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("rows\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
	}
	if rows := wtSessionRows(st.entries, tabs, "/src/app.worktrees/toast"); len(rows) != 1 || rows[0].ID != "new" {
		t.Errorf("a worktree without agents: %+v", rows)
	}
}

// A second session of the same agent in the same folder (and account)
// gets the next number; the first one has none.
func TestAgentSessionNo(t *testing.T) {
	tabs := []terminalTab{
		{Agent: "claude", Worktree: "/w"},
		{Agent: "claude", Worktree: "/w/", SessionNo: 3},
		{Agent: "codex", Worktree: "/w"},
		{Agent: "claude", Worktree: "/v"},
		{Agent: "claude", Worktree: "/w", Account: "work"},
	}
	for _, c := range []struct {
		id, dir, label string
		want           int
	}{
		{"claude", "/w", "", 4},
		{"codex", "/w", "", 2},
		{"claude", "/x", "", 0},
		{"claude", "/w", "work", 2},
		{"gemini", "/w", "", 0},
	} {
		if got := agentSessionNo(tabs, c.id, c.dir, c.label); got != c.want {
			t.Errorf("agentSessionNo(%s, %s, %q) = %d, want %d", c.id, c.dir, c.label, got, c.want)
		}
	}
	if got := (terminalTab{Agent: "claude", SessionNo: 2}).agentTabName(); got != "Claude Code 2" {
		t.Errorf("tab name %q", got)
	}
	if got := (terminalTab{Agent: "claude", SessionNo: 1}).agentTabName(); got != "Claude Code" {
		t.Errorf("tab name of session 1 %q", got)
	}
	if got := (terminalTab{Agent: "claude", SessionNo: 2, Account: "work"}).agentTabName(); got != "Claude Code 2 work" {
		t.Errorf("tab name with account %q", got)
	}
}

// Agent: Start… shows the running tab instead of a second copy; a new
// session always opens a new tab.
func TestAgentRunningTab(t *testing.T) {
	tabs := []terminalTab{{Agent: "codex", Worktree: "/w"}, {Agent: "claude", Worktree: "/w"}}
	if i := agentRunningTab(tabs, "claude", "/w", "", false); i != 1 {
		t.Errorf("running tab %d", i)
	}
	if i := agentRunningTab(tabs, "claude", "/w", "", true); i != -1 {
		t.Errorf("a new session found tab %d", i)
	}
	if i := agentRunningTab(tabs, "claude", "/v", "", false); i != -1 {
		t.Errorf("other folder found tab %d", i)
	}
}

// T lists the cursor's sessions; O opens the list of installed agents
// for one more session there (the worktree you are in starts in the
// workspace); a gone worktree only says why not. The palette rows do
// the same and the AI never runs them.
func TestWorktreeSessionKeys(t *testing.T) {
	defer func(f func(string) (string, error)) { agentLookPath = f }(agentLookPath)
	agentLookPath = func(bin string) (string, error) {
		if bin == "claude" || bin == "codex" {
			return "/bin/" + bin, nil
		}
		return "", errors.New("not found")
	}
	st, _ := wtFixture(t)
	st.off = true
	st.demoTabs = []wtTabAgent{{name: "Claude Code", cwd: "/src/app.worktrees/toast", state: agents.Working}}
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 30, h: 42, w: 150}
	st.cursorPath = "/src/app.worktrees/toast"
	wtKey(t, m, "T")
	if !m.pickerOpen || m.pickerKind != pickerKindWTSessions || st.sessionsPath != "/src/app.worktrees/toast" {
		t.Fatalf("T: open %v kind %v path %q", m.pickerOpen, m.pickerKind, st.sessionsPath)
	}
	m.pickerOpen = false
	wtKey(t, m, "O")
	if !m.pickerOpen || m.pickerKind != pickerKindWTSessionAgent || st.sessionDir != "/src/app.worktrees/toast" {
		t.Fatalf("S: open %v kind %v dir %q", m.pickerOpen, m.pickerKind, st.sessionDir)
	}
	var ids []string
	for _, it := range wtSessionAgentRows(m.terminalTabs, st.sessionDir) {
		ids = append(ids, it.ID)
	}
	if strings.Join(ids, ",") != "agent:claude,agent:codex" {
		t.Errorf("agent rows %v", ids)
	}
	m.pickerOpen = false
	st.cursorPath = "/src/app"
	wtKey(t, m, "O")
	if !m.pickerOpen || st.sessionDir != "" {
		t.Errorf("here: open %v dir %q", m.pickerOpen, st.sessionDir)
	}
	m.pickerOpen = false
	st.cursorPath = "/tmp/deleted"
	wtKey(t, m, "O")
	if m.pickerOpen || !strings.Contains(wtLastToast(m), "gone") {
		t.Errorf("gone: open %v toast %q", m.pickerOpen, wtLastToast(m))
	}
	st.cursorPath = "/src/app.worktrees/toast"
	for _, id := range []string{"worktree-new-session", "worktree-sessions"} {
		m.pickerOpen = false
		if _, ok := m.dispatchWorktreePalette(id); !ok || !m.pickerOpen {
			t.Errorf("%s: handled %v open %v", id, ok, m.pickerOpen)
		}
		if p, ok := aiActionPolicyFor(id); !ok || p.Gate != aiGateNever {
			t.Errorf("%s policy %+v", id, p)
		}
		found := false
		for _, it := range worktreePaletteItems() {
			found = found || it.ID == id
		}
		if !found {
			t.Errorf("%s not in the palette", id)
		}
	}
	agentLookPath = func(string) (string, error) { return "", errors.New("not found") }
	m.pickerOpen = false
	wtKey(t, m, "O")
	if m.pickerOpen || !strings.Contains(wtLastToast(m), "No agent is installed") {
		t.Errorf("none installed: open %v toast %q", m.pickerOpen, wtLastToast(m))
	}
}
