//go:build unix

package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtRunSeams fakes the repo file, the port check and the terminal tab.
func wtRunSeams(t *testing.T, cfg worktreeform.Config, hash string) (*[]terminalJob, *[]string) {
	t.Helper()
	var jobs []terminalJob
	var opened []string
	oldCfg, oldFree, oldSpawn, oldOpen := wtRunConfig, wtRunPortFree, wtRunSpawn, osOpen
	wtRunConfig = func(string) (worktreeform.Config, string, error) { return cfg, hash, nil }
	wtRunPortFree = func(int) bool { return true }
	wtRunSpawn = func(m *Model, j terminalJob) (int, error) {
		jobs = append(jobs, j)
		m.terminalTabs = append(m.terminalTabs, terminalTab{BufID: 70 + len(jobs), Name: j.Label, Cwd: j.Dir, Running: true})
		return 70 + len(jobs), nil
	}
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { wtRunConfig, wtRunPortFree, wtRunSpawn, osOpen = oldCfg, oldFree, oldSpawn, oldOpen })
	return &jobs, &opened
}

func wtRunModel(t *testing.T) *Model {
	t.Helper()
	withConfigDir(t)
	st, _ := wtFixture(t)
	st.common = t.TempDir()
	st.cursorPath = "/src/app.worktrees/toast"
	return &Model{w: 150, h: 42, wt: st, focus: FocusExplorer, explorerWidth: 40}
}

// CMP-5: without a run line, r says how to add one and runs nothing.
func TestWTRunNoCommand(t *testing.T) {
	jobs, _ := wtRunSeams(t, worktreeform.Config{}, "")
	m := wtRunModel(t)
	wtKey(t, m, "r")
	if len(*jobs) != 0 || m.confirmOpen {
		t.Fatalf("ran %v", *jobs)
	}
	if got := wtLastToast(m); !strings.Contains(got, "run") || !strings.Contains(got, "$PORT") {
		t.Errorf("toast %q", got)
	}
}

// CMP-5: the repo's run line is untrusted: a Not now first card, Esc and
// Not now run nothing; Trust and run starts it in a terminal tab of the
// worktree with its own port in $PORT and a clean environment; the port
// is kept; the card shows the address; r again shows the tab; o opens
// the address; another worktree gets the next port; the exit clears it.
func TestWTRunFlow(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-"+"test-not-real")
	cfg := worktreeform.Config{Run: "pnpm dev --port $PORT", Ports: "3000-3005"}
	jobs, opened := wtRunSeams(t, cfg, "h1")
	m := wtRunModel(t)
	folder := "/src/app.worktrees/toast"

	wtKey(t, m, "r")
	if !m.confirmOpen || m.confirmKind != confirmKindWTRunTrust || m.confirm.SafeID() != "deny" {
		t.Fatalf("no safe-first card: open %v kind %d safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"pnpm dev --port $PORT", "3000", "Not now", "Trust and run"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	m.confirmOpen = false
	m.onAIConfirmClosed()
	if len(*jobs) != 0 || loadSession().WorktreeSetupTrusted != "" {
		t.Fatalf("Esc ran %v", *jobs)
	}
	wtKey(t, m, "r")
	m.confirmOpen = false
	m.handleAIConfirm("deny")
	if len(*jobs) != 0 || loadSession().WorktreeSetupTrusted != "" {
		t.Fatalf("Not now ran %v", *jobs)
	}
	wtKey(t, m, "r")
	m.confirmOpen = false
	m.handleAIConfirm("run")
	if len(*jobs) != 1 || loadSession().WorktreeSetupTrusted != "h1" {
		t.Fatalf("jobs %v trusted %q", *jobs, loadSession().WorktreeSetupTrusted)
	}
	j := (*jobs)[0]
	if j.Dir != folder || !j.ClearEnv || j.Env["PORT"] != "3000" || !strings.HasPrefix(j.Cmdline, "/bin/sh -c ") || !strings.Contains(j.Cmdline, "pnpm dev") {
		t.Errorf("job %+v", j)
	}
	if !strings.Contains(j.Label, "toast") || !strings.Contains(j.Label, "3000") {
		t.Errorf("label %q", j.Label)
	}
	for k, v := range j.Env {
		if k == "ANTHROPIC_API_KEY" || strings.Contains(v.(string), "sk-"+"test") {
			t.Errorf("a secret in the env: %s", k)
		}
	}
	if metas, err := worktrees.Load(m.wt.common); err != nil || metas[folder].Port != 3000 {
		t.Errorf("port not kept: %+v %v", metas[folder], err)
	}
	if r := m.wt.runs[folder]; r == nil || r.port != 3000 || r.buf != 71 {
		t.Fatalf("run %+v", r)
	}
	out := ansi.Strip(m.renderWorktreesSidebar(40, 42))
	if !strings.Contains(out, "localhost:3000") {
		t.Errorf("card lacks the address:\n%s", out)
	}
	// r again: the tab, not a second server.
	m.terminalActiveTab = -1
	wtKey(t, m, "r")
	if len(*jobs) != 1 || m.terminalActiveTab != 0 {
		t.Errorf("second r: jobs %d tab %d", len(*jobs), m.terminalActiveTab)
	}
	// o opens the address.
	wtKey(t, m, "o")
	if strings.Join(*opened, ",") != "http://localhost:3000" {
		t.Errorf("opened %v", *opened)
	}
	// The trusted file starts at once in another worktree, on the next port.
	m.wt.cursorPath = "/src/app.worktrees/wt-sidebar"
	wtKey(t, m, "r")
	if m.confirmOpen || len(*jobs) != 2 || (*jobs)[1].Env["PORT"] != "3001" {
		t.Fatalf("second worktree: open %v jobs %v", m.confirmOpen, *jobs)
	}
	// The exit stops it; the port stays the worktree's.
	m.onTaskExit(71, 1)
	if r := m.wt.runs[folder]; r != nil {
		t.Errorf("still running: %+v", r)
	}
	if got := wtLastToast(m); !strings.Contains(got, "stopped") {
		t.Errorf("toast %q", got)
	}
	m.wt.cursorPath = folder
	wtKey(t, m, "r")
	if len(*jobs) != 3 || (*jobs)[2].Env["PORT"] != "3000" {
		t.Errorf("restart: %v", *jobs)
	}
	// A changed file asks again.
	wtRunConfig = func(string) (worktreeform.Config, string, error) { return cfg, "h2", nil }
	m.onTaskExit(73, 0)
	wtKey(t, m, "r")
	if !m.confirmOpen || len(*jobs) != 3 {
		t.Errorf("changed file ran without asking: %v", *jobs)
	}
}

// CMP-5: the card line says the port, and the address while it runs.
func TestWTRunLine(t *testing.T) {
	if got := wtRunLine(0, ""); got != "" {
		t.Errorf("no port: %q", got)
	}
	if got := wtRunLine(3004, ""); !strings.Contains(got, "3004") || !strings.Contains(got, "r runs") {
		t.Errorf("port: %q", got)
	}
	if got := wtRunLine(3004, "http://localhost:3004"); !strings.Contains(got, "localhost:3004") || strings.Contains(got, "r runs") {
		t.Errorf("running: %q", got)
	}
}

// CMP-5: a full range says so and runs nothing.
func TestWTRunNoFreePort(t *testing.T) {
	jobs, _ := wtRunSeams(t, worktreeform.Config{Run: "make dev", Ports: "3000-3000"}, "h1")
	wtRunPortFree = func(int) bool { return false }
	m := wtRunModel(t)
	sess := loadSession()
	sess.WorktreeSetupTrusted = "h1"
	saveSession(sess)
	wtKey(t, m, "r")
	if len(*jobs) != 0 || !strings.Contains(wtLastToast(m), "free port") {
		t.Errorf("jobs %v toast %q", *jobs, wtLastToast(m))
	}
}

// CMP-5: the web card gets the port and the address.
func TestWTRunWeb(t *testing.T) {
	_, _ = wtRunSeams(t, worktreeform.Config{Run: "make dev"}, "h1")
	m := wtRunModel(t)
	for i := range m.wt.entries {
		if m.wt.entries[i].Path == "/src/app.worktrees/toast" {
			m.wt.entries[i].Meta.Port = 3007
		}
	}
	m.wt.runs = map[string]*wtRun{"/src/app.worktrees/toast": {port: 3007, url: "http://localhost:3007", buf: 9}}
	s := m.webWorktreesBuild()
	found := false
	for _, w := range s.Worktrees {
		if w.Title != "" && w.Port == 3007 {
			found = true
			if !w.Running || w.URL != "http://localhost:3007" {
				t.Errorf("web item %+v", w)
			}
		}
	}
	if !found {
		t.Errorf("no web item with the port: %+v", s.Worktrees)
	}
}
