package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtStopSeam records the agent tabs sleep stops instead of closing them.
func wtStopSeam(t *testing.T) *[]int {
	t.Helper()
	var stopped []int
	old := wtStopTab
	wtStopTab = func(m *Model, buf int) {
		stopped = append(stopped, buf)
		for i, tb := range m.terminalTabs {
			if tb.BufID == buf {
				m.terminalTabs = append(m.terminalTabs[:i], m.terminalTabs[i+1:]...)
				return
			}
		}
	}
	t.Cleanup(func() { wtStopTab = old })
	return &stopped
}

const (
	wtSide  = "/src/app.worktrees/wt-sidebar"
	wtToast = "/src/app.worktrees/toast"
)

// wtSleepModel: wt-sidebar is the parent of toast; a Claude Code tab
// works in wt-sidebar, a Codex tab in toast (and a Codex of another
// window too), a plain shell in the main checkout.
func wtSleepModel(t *testing.T) *Model {
	t.Helper()
	st, _ := wtFixture(t)
	st.common = t.TempDir()
	for i := range st.entries {
		if st.entries[i].Path == wtToast {
			st.entries[i].Meta.Parent = wtSide
		}
	}
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 40, w: 150, h: 42}
	m.terminalTabs = []terminalTab{
		{BufID: 11, Agent: "claude", Cwd: wtSide, AgentState: agents.Waiting},
		{BufID: 12, Agent: "codex", Cwd: wtToast + "/sub", AgentState: agents.Working},
		{BufID: 13, Name: "shell", Cwd: "/src/app"},
	}
	return m
}

// WTX-13: s on a parent asks first (Keep running first and focused):
// the card names the agent tabs that end, its children and the agent of
// another window that keeps running. Esc and Keep running change
// nothing; "Sleep with its child" stops both tabs and puts both to sleep
// (under ASLEEP); the files are never touched. s again wakes both.
func TestWorktreeSleepParentCoversChildren(t *testing.T) {
	stopped := wtStopSeam(t)
	m := wtSleepModel(t)
	m.wt.cursorPath = wtSide
	wtKey(t, m, "s")
	if !m.confirmOpen || m.confirmKind != confirmKindWTSleep || m.confirm.SafeID() != "keep" {
		t.Fatalf("no safe-first card: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"Claude Code", "Codex", "toast", "1 child", "another window keeps running", "files are not touched", "Keep running", "Sleep with its child", "Sleep only this one"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc
	m.handleAIConfirm("keep")
	if len(*stopped) != 0 || wtMetaState(t, m, wtSide) != "" {
		t.Fatalf("Esc / Keep running slept: %v", *stopped)
	}

	wtKey(t, m, "s")
	m.confirmOpen = false
	m.handleAIConfirm("all")
	if fmt.Sprint(*stopped) != "[11 12]" {
		t.Errorf("stopped %v", *stopped)
	}
	for _, p := range []string{wtSide, wtToast} {
		if wtMetaState(t, m, p) != worktrees.StateAsleep {
			t.Errorf("%s not asleep", p)
		}
	}
	if len(m.terminalTabs) != 1 || m.terminalTabs[0].BufID != 13 {
		t.Errorf("the plain shell was stopped: %+v", m.terminalTabs)
	}
	items, rows := m.wtList()
	for _, r := range rows {
		if r.item >= 0 && items[r.item].e.Path == wtToast && items[r.item].group != "ASLEEP" {
			t.Errorf("toast in %s", items[r.item].group)
		}
	}

	// s on the asleep parent wakes it and its child; no card.
	m.wt.cursorPath = wtSide
	wtKey(t, m, "s")
	if m.confirmOpen {
		t.Fatal("wake asked")
	}
	for _, p := range []string{wtSide, wtToast} {
		if wtMetaState(t, m, p) != "" {
			t.Errorf("%s still asleep", p)
		}
	}
	if !strings.Contains(wtLastToast(m), "Woke") {
		t.Errorf("toast %q", wtLastToast(m))
	}
}

// "Sleep only this one" leaves the child and its agent alone; a worktree
// with no agent tab and no child sleeps without a card; the main
// checkout and a gone worktree never sleep.
func TestWorktreeSleepOnlyThisAndNoCard(t *testing.T) {
	stopped := wtStopSeam(t)
	m := wtSleepModel(t)
	m.wt.cursorPath = wtSide
	wtKey(t, m, "s")
	m.confirmOpen = false
	m.handleAIConfirm("one")
	if len(*stopped) != 1 || (*stopped)[0] != 11 {
		t.Fatalf("stopped %v", *stopped)
	}
	if wtMetaState(t, m, wtSide) != worktrees.StateAsleep || wtMetaState(t, m, wtToast) != "" {
		t.Fatal("only the parent should sleep")
	}

	m.wt.cursorPath = "/elsewhere/api-v2" // no tab, no child
	wtKey(t, m, "s")
	if m.confirmOpen || wtMetaState(t, m, "/elsewhere/api-v2") != worktrees.StateAsleep {
		t.Fatalf("no-card sleep: open %v", m.confirmOpen)
	}
	m.wt.cursorPath = "/src/app"
	wtKey(t, m, "s")
	if wtMetaState(t, m, "/src/app") != "" || !strings.Contains(wtLastToast(m), "main checkout") {
		t.Errorf("main slept; toast %q", wtLastToast(m))
	}
	if _, ok := m.dispatchWorktreePalette(wtSleepPaletteID); !ok {
		t.Error("sleep not in the palette")
	}
	if p, ok := aiActionPolicyFor(wtSleepPaletteID); !ok || p.Gate == aiGateAuto {
		t.Errorf("sleep policy %+v", p)
	}
}

func wtMetaState(t *testing.T, m *Model, path string) string {
	t.Helper()
	md, err := worktrees.Load(m.wt.common)
	if err != nil {
		t.Fatal(err)
	}
	return md[path].State
}
