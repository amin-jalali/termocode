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
	log   string // the log file
}

// wtSetupPending is the open trust card.
type wtSetupPending struct {
	folder, hash, common string
	setup                []string
}

// wtSetupLogPath is the log file of the worktree folder.
func wtSetupLogPath(common, folder string) string {
	return filepath.Join(common, "termocode", "setup", filepath.Base(folder)+".log")
}

// wtSetupAsk runs the setup when this file is trusted, else opens the
// trust card.
func (m *Model) wtSetupAsk(f *wtForm, folder string) tea.Cmd {
	p := &wtSetupPending{folder: folder, hash: f.hash, common: f.common, setup: append([]string(nil), f.cfg.Setup...)}
	if worktreeform.SetupTrusted(f.hash, loadSession().WorktreeSetupTrusted) {
		return m.wtSetupStart(p)
	}
	m.wt.setupAsk = p
	var b strings.Builder
	b.WriteString(worktreeform.ConfigFile + " in this repository asks to run, in " + wtShortPath(folder) + ":\n")
	for _, l := range p.setup {
		b.WriteString("\n    " + aiCleanText(l))
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
	failed string // the line that failed
}

// wtSetupExec runs the lines in dir, output to logPath (a var: tests).
var wtSetupExec = func(dir, logPath string, lines []string) (failed string, err error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return "", err
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer logf.Close()
	env := clistep.CleanEnv(os.Environ())
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
	logPath := wtSetupLogPath(p.common, p.folder)
	st.setups[p.folder] = &wtSetupRun{state: wtSetupRunning, log: logPath}
	st.touch()
	ch := make(chan tea.Msg, 1)
	crash.Go("worktree setup", func() {
		failed, err := wtSetupExec(p.folder, logPath, p.setup)
		ch <- wtSetupDoneMsg{folder: p.folder, ok: err == nil, failed: failed}
	})
	return func() tea.Msg { return <-ch }
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
		return m.pushToastDetail(toast.Info, "Setup done", wtShortPath(msg.folder))
	}
	r.state = wtSetupFailed
	why := "It could not start; see the log (L in WORKTREES)."
	if msg.failed != "" {
		why = "`" + aiCleanText(msg.failed) + "` failed; see the log (L in WORKTREES)."
	}
	return m.pushToastDetail(toast.Warn, "Setup failed", why)
}

// worktreeSetupLog opens the setup log of the cursor's worktree.
func (m *Model) worktreeSetupLog() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	r := m.wt.setups[it.e.Path]
	if r == nil {
		return m.pushToastDetail(toast.Info, "No setup ran in this worktree", "Setup runs after Worktree: New… when .termocode/worktrees.json lists setup commands.")
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
