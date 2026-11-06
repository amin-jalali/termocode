//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// wtSetupSeam records the setup runs instead of running them.
func wtSetupSeam(t *testing.T) *[]string {
	t.Helper()
	var runs []string
	old := wtSetupExec
	wtSetupExec = func(dir, _ string, lines []string) (string, error) {
		runs = append(runs, dir+": "+strings.Join(lines, " && "))
		return "", nil
	}
	t.Cleanup(func() { wtSetupExec = old })
	return &runs
}

// WTX-7: the repository's setup never runs before the trust card; Not
// now first and focused; Esc and Not now run nothing; Trust and run
// runs it and remembers this file; a changed file asks again.
func TestWTSetupNeedsTrust(t *testing.T) {
	runs := wtSetupSeam(t)
	m := wtFormModel(t)
	f := m.wt.form
	folder := "/src/app.worktrees/feat-x"

	m.wtSetupAsk(f, folder)
	if !m.confirmOpen || m.confirmKind != confirmKindWTSetupTrust || m.confirm.SafeID() != "deny" {
		t.Fatalf("no safe-first trust card: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"pnpm install", "feat-x", "Not now", "Trust and run"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	// Esc: no answer, nothing runs, nothing is stored.
	m.confirmOpen = false
	m.onAIConfirmClosed()
	if len(*runs) != 0 || loadSession().WorktreeSetupTrusted != "" {
		t.Fatalf("Esc ran %v", *runs)
	}
	// Not now.
	m.wtSetupAsk(f, folder)
	m.confirmOpen = false
	m.handleAIConfirm("deny")
	if len(*runs) != 0 || loadSession().WorktreeSetupTrusted != "" {
		t.Fatalf("Not now ran %v", *runs)
	}
	// Trust and run.
	m.wtSetupAsk(f, folder)
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm("run")
	wtDrive(t, m, cmd)
	if strings.Join(*runs, "|") != folder+": pnpm install" || loadSession().WorktreeSetupTrusted != f.hash {
		t.Fatalf("runs %v trusted %q", *runs, loadSession().WorktreeSetupTrusted)
	}
	if r := m.wt.setups[folder]; r == nil || r.state != wtSetupOK {
		t.Errorf("setup state %+v", r)
	}
	// The same file: no card, it runs.
	wtDrive(t, m, m.wtSetupAsk(f, folder))
	if m.confirmOpen || len(*runs) != 2 {
		t.Errorf("trusted file asked again: open %v runs %v", m.confirmOpen, *runs)
	}
	// A changed file asks again.
	f.hash = "changed"
	m.wtSetupAsk(f, folder)
	if !m.confirmOpen || len(*runs) != 2 {
		t.Errorf("changed file ran without asking: %v", *runs)
	}
}

// WTX-7: the setup runs in the new worktree through a shell, without
// termocode's secrets in its environment, and logs to the git dir; a
// failing line stops it.
func TestWTSetupExec(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "termocode", "setup", "x.log")
	t.Setenv("ANTHROPIC_API_KEY", "sk-"+"test-not-real")
	t.Setenv("TERMOCODE_AI_API_KEY", "k-"+"test")
	failed, err := wtSetupExec(dir, logPath, []string{`pwd > where.txt`, `env > env.txt`, `false`, `touch never.txt`})
	if err == nil || failed != "false" {
		t.Fatalf("failed %q err %v", failed, err)
	}
	where, _ := os.ReadFile(filepath.Join(dir, "where.txt"))
	if real, _ := filepath.EvalSymlinks(dir); strings.TrimSpace(string(where)) != real && strings.TrimSpace(string(where)) != dir {
		t.Errorf("ran in %q", where)
	}
	env, _ := os.ReadFile(filepath.Join(dir, "env.txt"))
	if strings.Contains(string(env), "ANTHROPIC_API_KEY") || strings.Contains(string(env), "TERMOCODE_AI_API_KEY") {
		t.Error("a secret reached the setup")
	}
	if _, err := os.Stat(filepath.Join(dir, "never.txt")); err == nil {
		t.Error("a line after the failure ran")
	}
	log, _ := os.ReadFile(logPath)
	if !strings.Contains(string(log), "$ pwd > where.txt") || !strings.Contains(string(log), "$ false") {
		t.Errorf("log:\n%s", log)
	}
	if fi, err := os.Stat(logPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v %v", fi, err)
	}
}

// The worktree card shows the setup state; L opens the log.
func TestWTSetupOnCard(t *testing.T) {
	for state, want := range map[string]string{wtSetupRunning: "running", wtSetupOK: "setup", wtSetupFailed: "L log"} {
		if got := wtSetupLine(state); !strings.Contains(got, want) {
			t.Errorf("%s: %q", state, got)
		}
	}
	if wtSetupLine("") != "" {
		t.Error("a card without setup shows one")
	}
	st, _ := wtFixture(t)
	st.setups = map[string]*wtSetupRun{"/src/app.worktrees/toast": {state: wtSetupFailed}}
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 40, h: 42}
	items, _ := m.wtList()
	for _, it := range items {
		if it.e.Path == "/src/app.worktrees/toast" && it.setup != wtSetupFailed {
			t.Errorf("item setup %q", it.setup)
		}
	}
}
