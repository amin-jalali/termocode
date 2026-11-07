//go:build unix

package app

import (
	"os"
	"os/exec"
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
	wtSetupExec = func(dir, _ string, lines []string, _ map[string]string) (string, error) {
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
	failed, err := wtSetupExec(dir, logPath, []string{`pwd > where.txt`, `env > env.txt`, `false`, `touch never.txt`}, map[string]string{"PORT": "3001"})
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
	if !strings.Contains(string(env), "PORT=3001") {
		t.Error("the repo's env did not reach the setup")
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

// W2 leftover (plan WTX-7): with an editor the setup runs in a terminal
// tab you can watch: in the new worktree, through /bin/sh whatever your
// shell is, with a clean environment (no termocode secrets); its exit
// puts ✓ or ✗ on the card, and L shows the tab.
func TestWTSetupInTab(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-"+"test-not-real")
	var jobs []terminalJob
	old := wtSetupSpawn
	wtSetupSpawn = func(m *Model, j terminalJob) (int, error) {
		jobs = append(jobs, j)
		m.terminalTabs = append(m.terminalTabs, terminalTab{BufID: 40 + len(jobs), Name: j.Label, Cwd: j.Dir, Running: true})
		return 40 + len(jobs), nil
	}
	defer func() { wtSetupSpawn = old }()
	m := wtFormModel(t)
	folder := "/src/app.worktrees/feat-x"
	p := &wtSetupPending{folder: folder, common: m.wt.common, setup: []string{"pnpm install", "echo 'it''s done'"}}
	m.wtSetupStart(p)
	if len(jobs) != 1 {
		t.Fatalf("jobs %v", jobs)
	}
	j := jobs[0]
	if j.Dir != folder || !strings.Contains(j.Label, "feat-x") || !j.ClearEnv || !strings.HasPrefix(j.Cmdline, "/bin/sh -c ") {
		t.Errorf("job %+v", j)
	}
	if !strings.Contains(j.Cmdline, "pnpm install") {
		t.Errorf("cmdline %q", j.Cmdline)
	}
	for k, v := range j.Env {
		if k == "ANTHROPIC_API_KEY" || strings.Contains(v.(string), "sk-"+"test") {
			t.Errorf("a secret in the tab's env: %s", k)
		}
	}
	if r := m.wt.setups[folder]; r == nil || r.state != wtSetupRunning || r.buf != 41 {
		t.Fatalf("setup %+v", r)
	}
	// The script really runs each line and stops at the first failure.
	dir := t.TempDir()
	script := wtSetupScript([]string{"echo one > a.txt", "false", "echo never > b.txt"})
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "$ false") {
		t.Errorf("script: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err == nil {
		t.Error("a line after the failure ran")
	}
	m.terminalActiveTab = -1
	wtSetupL := m.worktreeSetupLogFor(folder)
	_ = wtSetupL
	if m.terminalActiveTab != 0 {
		t.Errorf("L did not show the tab: %d", m.terminalActiveTab)
	}
	m.onTaskExit(41, 1)
	if r := m.wt.setups[folder]; r.state != wtSetupFailed {
		t.Errorf("after exit 1: %+v", r)
	}
	m.wtSetupStart(p)
	m.onTaskExit(42, 0)
	if r := m.wt.setups[folder]; r.state != wtSetupOK || !strings.Contains(wtLastToast(m), "Setup done") {
		t.Errorf("after exit 0: %+v %q", r, wtLastToast(m))
	}
}
