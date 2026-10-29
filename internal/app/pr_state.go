package app

import (
	"context"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/toast"
)

// G-PR: pull requests inside termocode through the gh CLI (no MCP):
// a list of open PRs (pr_list.go), a PR view with its actions
// (pr_view.go), review, merge, create. gh runs through the same runner
// as the CI watcher (ci_watch.go), so a headless run never starts it.

// prState is the PR UI state.
type prState struct {
	items  []ghci.PRItem   // the last list
	detail *ghci.PRDetail  // the PR on screen in the view
	diff   []ghci.FileDiff // its diff, once read
	busy   bool            // an action runs: a second click waits
	review ghci.ReviewKind // the review the open prompt sends
	create *ghci.NewPR     // the pull request being created
}

// prS returns the state, creating it on first use.
func (m *Model) prS() *prState {
	if m.pr == nil {
		m.pr = &prState{}
	}
	return m.pr
}

// prRun runs fn off the main loop with the shared gh runner and the
// workspace folder. A missing folder gives a toast, not a command.
func (m *Model) prRun(fn func(ctx context.Context, run ghci.Runner, dir string) tea.Msg) tea.Cmd {
	dir, err := m.rootDir()
	if err != nil {
		return m.aiInfo("Pull requests need a folder with a git repository")
	}
	run := m.ciW().runner()
	return func() tea.Msg { return fn(context.Background(), run, dir) }
}

// prFailToast says why a gh call failed: the quiet hint when gh is not
// usable, else the error text.
func (m *Model) prFailToast(title string, err error) tea.Cmd {
	var c tea.Cmd
	switch {
	case err == ghci.ErrNoGitHub:
		m.toast, c = m.toast.Push(toast.Info, "This repository has no GitHub remote.")
	case ghci.Quiet(err):
		m.toast, c = m.toast.Push(toast.Info, ciGHHint)
	default:
		m.toast, c = m.toast.PushDetail(toast.Errr, title, err.Error())
	}
	return c
}

// prInfo pushes an info toast with a detail line.
func (m *Model) prInfo(text, detail string) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, text, detail)
	return c
}

// prRef is "#12".
func prRef(n int) string { return "#" + strconv.Itoa(n) }

// handlePRMsg handles the PR messages (any overlay may be open).
func (m *Model) handlePRMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case prCreatePrepMsg: // pr_create.go
		return m.onPRCreatePrep(msg), true
	case prListMsg: // pr_list.go
		return m.onPRList(msg), true
	case prViewMsg: // pr_view.go
		return m.onPRView(msg), true
	case prDoneMsg: // pr_state.go
		return m.onPRDone(msg), true
	case prDiffMsg: // pr_files.go
		return m.onPRDiff(msg), true
	}
	return nil, false
}

// prDoneMsg is a finished change through gh (check out, review, merge…).
type prDoneMsg struct {
	number  int
	ok      string // the success toast
	failed  string // the failure toast title
	err     error
	refresh bool // the branch or files changed: read git again
	reopen  bool // read the pull request again and show the card
	ci      bool // ask CI again (a new pull request)
	detail  string
}

// onPRDone says how it went. A change of the work tree refreshes git (and
// with it the CI chip); a change of the pull request reopens its card.
func (m *Model) onPRDone(msg prDoneMsg) tea.Cmd {
	s := m.prS()
	s.busy = false
	if msg.err != nil {
		return m.prFailToast(msg.failed, msg.err)
	}
	cmds := []tea.Cmd{m.prInfo(msg.ok, msg.detail)}
	if msg.refresh {
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
		cmds = append(cmds, fetchGitCmd())
	}
	if msg.ci {
		if c := m.ciTrigger(); c != nil {
			cmds = append(cmds, c)
		}
	}
	if msg.reopen && msg.number > 0 {
		cmds = append(cmds, m.prViewCmd(msg.number))
	}
	return tea.Batch(cmds...)
}
