package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
)

// The jump list has every worktree and every agent in one: agents that
// wait for you first, then working ones, then the worktrees by last
// visit; gone worktrees are left out.
func TestWorktreeJumpRows(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	st, tabs := wtFixture(t)
	tabs = append(tabs, wtTabAgent{name: "Aider", cwd: "/src/app", state: agents.Working})
	st.others = append(st.others, agentroll.Rollup{Workspace: "/x", PID: 9, Items: []agentroll.Item{{Name: "Gemini", State: "waiting", Worktree: "/elsewhere/api-v2"}}})
	items := wtItems(st.entries, tabs, st.others, st.root)
	rows := wtJumpRows(items, tabs, st.others)
	var got []string
	for _, r := range rows {
		got = append(got, r.ID+"="+r.Title)
	}
	want := []string{
		"tab:0=Claude Code · feat/wt-sidebar",
		"ag:1:/elsewhere/api-v2=Gemini · api-v2",
		"tab:2=Aider · main",
		"ag:0:/src/app.worktrees/toast=Codex · fix/toast-race",
		"wt:/src/app.worktrees/wt-sidebar=feat/wt-sidebar",
		"wt:/elsewhere/api-v2=api-v2",
		"wt:/src/app=main",
		"wt:/src/app.worktrees/toast=fix/toast-race",
		"wt:/src/app.worktrees/grpc=spike/grpc",
		"wt:/src/app/.git/termocode/worktrees/r9-a=agent r9-a",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("rows\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
	}
	for _, r := range rows {
		if r.ID == "wt:/src/app" && !strings.Contains(r.Hint, "you are here") {
			t.Errorf("main hint %q", r.Hint)
		}
		if strings.HasPrefix(r.ID, "ag:1:") && !strings.Contains(r.Hint, "waits for you") {
			t.Errorf("other window hint %q", r.Hint)
		}
	}
}

// Worktree: Jump… opens the picker (reading the list first when the view
// never showed); picking a worktree opens it, picking an agent tab of
// this window shows that tab. Ctrl+Shift+J runs it.
func TestWorktreeJumpPicks(t *testing.T) {
	opened, _ := wtSeams(t)
	root := wtRepo(t)
	m := wtTestModel(t, root)
	cmd, ok := m.dispatchWorktreePalette("worktree-jump")
	if !ok || cmd == nil {
		t.Fatal("no load")
	}
	msg := cmd().(wtLoadMsg)
	m.handleWTMsg(msg)
	if !m.pickerOpen || m.pickerKind != pickerKindWorktreeJump {
		t.Fatalf("picker open %v kind %v", m.pickerOpen, m.pickerKind)
	}
	var feat string
	for _, e := range m.wt.entries {
		if e.Branch == "feat/x" {
			feat = e.Path
		}
	}
	m.onWorktreeJumpPicked("wt:" + feat)
	m.onWorktreeJumpPicked("ag:3:" + feat)
	if strings.Join(*opened, ",") != feat+","+feat {
		t.Errorf("opened %v", *opened)
	}
	m.terminalTabs = []terminalTab{{Agent: "claude"}, {Agent: "codex"}}
	m.onWorktreeJumpPicked("tab:1")
	if m.terminalActiveTab != 1 {
		t.Errorf("tab %d", m.terminalActiveTab)
	}
	if keys := strings.Join(keymap.Default().KeysFor(keymap.ActionWorktreeJump), " "); keys != "ctrl+shift+j" {
		t.Errorf("jump keys %q", keys)
	}
	if !keymap.KittyOnlyKeys["ctrl+shift+j"] {
		t.Error("ctrl+shift+j is not marked kitty-only")
	}
	if p, ok := aiActionPolicyFor("worktree-jump"); !ok || p.Gate != aiGateNever {
		t.Errorf("worktree-jump policy %+v", p)
	}
	_ = picker.Item{}
	_ = tea.KeyMsg{}
}
