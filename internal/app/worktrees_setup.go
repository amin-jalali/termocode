package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktreeform"
)

// ── Setup commands of a new worktree (WTX-7) ─────────────────────────────
//
// .termocode/worktrees.json "setup" lines come from the repository: they
// are untrusted input. They run only after the trust card (Not now
// first and focused; Enter and Esc never run them), and the answer is
// kept by the file's hash in this workspace's session, so a changed file
// asks again. They run through internal/spawn in the new worktree with
// the user's environment minus termocode's secrets (clistep.CleanEnv),
// one after the other; the output goes to a log file in the git dir
// (Worktree: Show Setup Log, L in the view). The worktree card shows
// setup ● running / ✓ / ✗.
//
// CMP-96: "env" adds plain variables to the setup (in the tab and in the
// background). With "warm", a setup that worked leaves a copy of its
// paths in the git dir (worktreeform.SaveWarm); the next new worktree
// with the same key gets the copy back and skips the setup. The trust
// card names both, and a warm copy is used only after that trust.

const confirmKindWTSetupTrust confirmKindEnum = 3602

// Setup states on the card.
const (
	wtSetupRunning = "running"
	wtSetupOK      = "ok"
	wtSetupFailed  = "failed"
)

// wtSetupRun is the setup of one worktree.
type wtSetupRun struct {
	state string
	log   string  // the log file (a setup run without an editor)
	buf   int     // the terminal tab's buffer (a setup run in a tab)
	warm  *wtWarm // keep a warm copy when the tab ends well (nil: no warm)
}

// wtWarm is where the warm copy of one setup goes.
type wtWarm struct {
	common, snap string
	paths        []string
}

// wtSetupSpawn opens the setup's terminal tab (a var: tests; nil = the
// panel's spawnTerminalJob when the editor runs).
var wtSetupSpawn func(m *Model, j terminalJob) (int, error)

// wtShellQuote is s in single quotes for /bin/sh.
func wtShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// wtSetupScript is the /bin/sh script of a setup: each line shown, then
// run; the first failure stops it.
func wtSetupScript(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		q := wtShellQuote("$ " + l)
		fmt.Fprintf(&b, "printf '%%s\\n' %s; ( %s ) || { printf '\\n✗ failed: %%s\\n' %s; exit 1; }\n", q, l, wtShellQuote(l))
	}
	b.WriteString("printf '\\n✓ setup done\\n'\n")
	return b.String()
}

// wtSetupPending is the open trust card.
type wtSetupPending struct {
	folder, hash, common string
	setup                []string
	cfg                  worktreeform.Config // env and warm (the setup lines are in setup)
}

// warm is the warm copy of the pending setup (nil: none asked, or its
// key could not be made).
func (p *wtSetupPending) warm() *wtWarm {
	if !p.cfg.WarmOn() {
		return nil
	}
	key, err := worktreeform.WarmKey(p.folder, p.hash, p.cfg)
	if err != nil {
		recordError("[worktrees] warm key: " + err.Error())
		return nil
	}
	return &wtWarm{common: p.common, snap: worktreeform.WarmDir(p.common, key), paths: p.cfg.Warm.Paths}
}

// keep saves the warm copy of folder and drops old ones; errors only go
// to the log (the setup itself worked).
func (w *wtWarm) keep(folder string) {
	if _, err := worktreeform.SaveWarm(folder, w.snap, w.paths); err != nil {
		recordError("[worktrees] warm copy: " + err.Error())
		return
	}
	worktreeform.PruneWarm(w.common, worktreeform.WarmKeep)
}

// wtSetupLogPath is the log file of the worktree folder.
func wtSetupLogPath(common, folder string) string {
	return filepath.Join(common, "termocode", "setup", filepath.Base(folder)+".log")
}

// wtSetupAsk runs the setup when this file is trusted, else opens the
// trust card.
func (m *Model) wtSetupAsk(f *wtForm, folder string) tea.Cmd {
	p := &wtSetupPending{folder: folder, hash: f.hash, common: f.common, setup: append([]string(nil), f.cfg.Setup...), cfg: f.cfg}
	if worktreeform.SetupTrusted(f.hash, loadSession().WorktreeSetupTrusted) {
		return m.wtSetupStart(p)
	}
	m.wt.setupAsk = p
	var b strings.Builder
	b.WriteString(worktreeform.ConfigFile + " in this repository asks to run, in " + wtShortPath(folder) + ":\n")
	for _, l := range p.setup {
		b.WriteString("\n    " + aiCleanText(l))
	}
	if names := worktreeform.EnvNames(p.cfg.Env); len(names) > 0 {
		b.WriteString("\n\nIt adds these variables: " + aiCleanText(strings.Join(names, ", ")) + ".")
	}
	if p.cfg.WarmOn() {
		b.WriteString("\n\nAfter it works, a copy of " + aiCleanText(strings.Join(p.cfg.Warm.Paths, ", ")) +
			" is kept in the git folder. A new worktree with the same lock files gets that copy and skips the setup.")
	}
	b.WriteString("\n\nIt runs with your environment minus termocode's secrets. Run it only if you trust this repository; a changed file asks again.")
	m.confirm = confirm.Risky("Run this repository's setup?", b.String(),
		confirm.Button{ID: "deny", Title: "Not now"},
		confirm.Button{ID: "run", Title: "Trust and run", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTSetupTrust
	return nil
}

// answerWTSetupTrust stores the answer of the card ("" = Esc: no answer).
func (m *Model) answerWTSetupTrust(id string) tea.Cmd {
	p := m.wt.setupAsk
	m.wt.setupAsk = nil
	if p == nil || id != "run" {
		if p != nil && id == "deny" {
			return m.pushToastDetail(toast.Info, "The setup did not run", "Run it yourself in "+wtShortPath(p.folder)+", or make a new worktree to be asked again.")
		}
		return nil
	}
	sess := loadSession()
	sess.WorktreeSetupTrusted = p.hash
	saveSession(sess)
	return m.wtSetupStart(p)
}

// wtSetupDoneMsg is the end of a setup.
type wtSetupDoneMsg struct {
	folder string
	ok     bool
	failed string   // the line that failed
	warm   []string // the warm paths that came back (the setup did not run)
	kept   bool     // a warm copy was kept for the next worktree
}

// wtSetupExec runs the lines in dir, output to logPath (a var: tests).
var wtSetupExec = func(dir, logPath string, lines []string, extra map[string]string) (failed string, err error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return "", err
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer logf.Close()
	env := worktreeform.SetupEnv(clistep.CleanEnv(os.Environ()), extra)
	for _, l := range lines {
		fmt.Fprintf(logf, "$ %s\n", l)
		cmd := spawn.Command("/bin/sh", "-c", l)
		cmd.Dir = dir
		cmd.Env = env
		cmd.Stdout, cmd.Stderr = logf, logf
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(logf, "\n✗ %v\n", err)
			return l, err
		}
	}
	fmt.Fprintln(logf, "\n✓ setup done")
	return "", nil
}

// wtSetupStart runs the setup in the background.
func (m *Model) wtSetupStart(p *wtSetupPending) tea.Cmd {
	st := m.wt
	if st.setups == nil {
		st.setups = map[string]*wtSetupRun{}
	}
	warm := p.warm()
	if warm != nil && worktreeform.HasWarm(warm.snap) {
		return m.wtSetupWarmStart(p, warm)
	}
	spawnTab := wtSetupSpawn
	if spawnTab == nil && m.nvim != nil {
		spawnTab = (*Model).spawnTerminalJob
	}
	if spawnTab != nil {
		env := map[string]any{}
		for _, kv := range worktreeform.SetupEnv(clistep.CleanEnv(os.Environ()), p.cfg.Env) {
			if k, v, ok := strings.Cut(kv, "="); ok {
				env[k] = v
			}
		}
		j := terminalJob{Label: "setup: " + filepath.Base(p.folder), Dir: p.folder, Env: env, ClearEnv: true,
			Cmdline: "/bin/sh -c " + wtShellQuote(wtSetupScript(p.setup))}
		buf, err := spawnTab(m, j)
		if err == nil {
			st.setups[p.folder] = &wtSetupRun{state: wtSetupRunning, buf: buf, warm: warm}
			st.touch()
			return nil
		}
		recordError("[worktrees] setup tab: " + err.Error()) // run it in the background instead
	}
	logPath := wtSetupLogPath(p.common, p.folder)
	st.setups[p.folder] = &wtSetupRun{state: wtSetupRunning, log: logPath}
	st.touch()
	ch := make(chan tea.Msg, 1)
	crash.Go("worktree setup", func() {
		failed, err := wtSetupExec(p.folder, logPath, p.setup, p.cfg.Env)
		kept := false
		if err == nil && warm != nil {
			warm.keep(p.folder)
			kept = worktreeform.HasWarm(warm.snap)
		}
		ch <- wtSetupDoneMsg{folder: p.folder, ok: err == nil, failed: failed, kept: kept}
	})
	return func() tea.Msg { return <-ch }
}

// wtSetupWarmStart puts the warm copy into the new worktree in the
// background; when that fails the setup runs after all.
func (m *Model) wtSetupWarmStart(p *wtSetupPending, warm *wtWarm) tea.Cmd {
	st := m.wt
	logPath := wtSetupLogPath(p.common, p.folder)
	st.setups[p.folder] = &wtSetupRun{state: wtSetupRunning, log: logPath}
	st.touch()
	ch := make(chan tea.Msg, 1)
	crash.Go("worktree warm copy", func() {
		got, err := worktreeform.RestoreWarm(warm.snap, p.folder, warm.paths)
		if err == nil && len(got) > 0 {
			wtSetupNote(logPath, "✓ warm copy of "+strings.Join(got, ", ")+" restored; the setup did not run\n")
			ch <- wtSetupDoneMsg{folder: p.folder, ok: true, warm: got}
			return
		}
		why := "nothing to restore"
		if err != nil {
			why = err.Error()
		}
		recordError("[worktrees] warm restore: " + why)
		failed, err := wtSetupExec(p.folder, logPath, p.setup, p.cfg.Env)
		ch <- wtSetupDoneMsg{folder: p.folder, ok: err == nil, failed: failed}
	})
	return func() tea.Msg { return <-ch }
}

// wtSetupNote writes text as the setup log of a warm start.
func wtSetupNote(logPath, text string) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(logPath, []byte(text), 0o600)
}

// onWTSetupDone puts the result on the card and in a toast.
func (m *Model) onWTSetupDone(msg wtSetupDoneMsg) tea.Cmd {
	st := m.wt
	r := st.setups[msg.folder]
	if r == nil {
		return nil
	}
	st.touch()
	if msg.ok {
		r.state = wtSetupOK
		if len(msg.warm) > 0 {
			return m.pushToastDetail(toast.Info, "Setup skipped: warm copy restored",
				strings.Join(msg.warm, ", ")+" came from an earlier setup with the same lock files ("+wtShortPath(msg.folder)+").")
		}
		detail := wtShortPath(msg.folder)
		if msg.kept {
			detail += " · a warm copy is kept for the next worktree"
		}
		return m.pushToastDetail(toast.Info, "Setup done", detail)
	}
	r.state = wtSetupFailed
	why := "See its output (L in WORKTREES)."
	if msg.failed != "" {
		why = "`" + aiCleanText(msg.failed) + "` failed; see the log (L in WORKTREES)."
	}
	return m.pushToastDetail(toast.Warn, "Setup failed", why)
}

// wtSetupExit is the end of a setup tab (ok=false: not a setup's).
func (m *Model) wtSetupExit(buf, code int) (tea.Cmd, bool) {
	if m.wt == nil || buf <= 0 {
		return nil, false
	}
	for folder, r := range m.wt.setups {
		if r.buf == buf && r.state == wtSetupRunning {
			if code == 0 && r.warm != nil {
				w, f := r.warm, folder
				crash.Go("worktree warm save", func() { w.keep(f) })
			}
			return m.onWTSetupDone(wtSetupDoneMsg{folder: folder, ok: code == 0}), true
		}
	}
	return nil, false
}

// worktreeSetupLog shows the setup of the cursor's worktree: its tab, or
// its log file.
func (m *Model) worktreeSetupLog() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	return m.worktreeSetupLogFor(it.e.Path)
}

func (m *Model) worktreeSetupLogFor(folder string) tea.Cmd {
	r := m.wt.setups[folder]
	if r == nil {
		return m.pushToastDetail(toast.Info, "No setup ran in this worktree", "Setup runs after Worktree: New… when .termocode/worktrees.json lists setup commands.")
	}
	if r.buf > 0 {
		for i := range m.terminalTabs {
			if m.terminalTabs[i].BufID == r.buf {
				m.terminalActiveTab = i
				m.panelActive = panelKindTerminal
				if m.nvim != nil {
					m.applyLayout()
					m.resizeTerminalSplit()
				}
				return nil
			}
		}
		return m.pushToastDetail(toast.Info, "The setup tab is closed", "Its output went with it; run the setup again in a terminal if you need it.")
	}
	m.openFileAtLine(r.log, 1)
	return nil
}

// wtSetupLine is the setup piece of a card ("" = none).
func wtSetupLine(state string) string {
	switch state {
	case wtSetupRunning:
		return "setup ● running"
	case wtSetupOK:
		return "setup ✓"
	case wtSetupFailed:
		return "setup ✗ (L log)"
	}
	return ""
}
