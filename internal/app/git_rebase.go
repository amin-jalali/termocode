package app

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/rebaseview"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Interactive rebase (G-RBS): R on a GRAPH commit (or "Rebase from
// Here…") opens the rebase card (internal/rebaseview) on that commit and
// every newer one; the user sets an action per row and the order, then
// starts it. git runs it with termocode as its editor
// (internal/git/rebase_helper.go).

// gitRebaseState is the open rebase card.
type gitRebaseState struct {
	open bool
	card rebaseview.Model
}

// rebaseTodoMsg carries a loaded todo for the card.
type rebaseTodoMsg struct {
	title string
	todo  git.RebaseTodo
	err   error
}

// rebaseCardOpen reports whether the rebase card is on screen.
func (m Model) rebaseCardOpen() bool { return m.rebase != nil && m.rebase.open }

// gitRebaseFromCmd loads the todo from commit full (included) to HEAD.
func (m *Model) gitRebaseFromCmd(full string) tea.Cmd {
	if full == "" {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	branch := m.gitBranch.Name
	return func() tea.Msg {
		base := ""
		if p, err := git.ParentOf(cwd, full); err == nil {
			base = p
		}
		todo, err := git.LoadRebaseTodo(cwd, base)
		return rebaseTodoMsg{title: rebaseTitle(branch, base), todo: todo, err: err}
	}
}

// rebaseTitle is the card's title ("Interactive Rebase · main onto a1b2c3d").
func rebaseTitle(branch, base string) string {
	onto := "the root"
	if len(base) >= 7 {
		onto = base[:7]
	}
	if branch == "" {
		return "Interactive Rebase · onto " + onto
	}
	return "Interactive Rebase · " + branch + " onto " + onto
}

// onRebaseTodo opens the card (or says why not).
func (m *Model) onRebaseTodo(msg rebaseTodoMsg) tea.Cmd {
	if msg.err != nil {
		var cmd tea.Cmd
		switch {
		case errors.Is(msg.err, git.ErrRebaseMerge):
			m.toast, cmd = m.toast.PushDetail(toast.Warn, "Can't rebase over a merge commit", msg.err.Error())
		case errors.Is(msg.err, git.ErrRebaseEmpty):
			m.toast, cmd = m.toast.Push(toast.Info, "No commits to rebase")
		default:
			m.toast, cmd = m.toast.PushDetail(toast.Errr, "Rebase failed", msg.err.Error())
		}
		return cmd
	}
	m.closeCommitView()
	card := rebaseview.New(msg.title, msg.todo)
	card.SetSize(m.w, m.h)
	m.rebase = &gitRebaseState{open: true, card: card}
	return nil
}

// closeRebaseCard hides the card.
func (m *Model) closeRebaseCard() {
	if m.rebase != nil {
		m.rebase.open = false
	}
}

// rebaseLayer draws the card over base.
func (m Model) rebaseLayer(base string) string {
	if !m.rebaseCardOpen() {
		return base
	}
	c := m.rebase.card
	c.SetSize(m.w, m.h)
	box := c.Box()
	if box == "" {
		return base
	}
	o := ModalOpts{At: true}
	o.X, o.Y = c.Origin()
	return modalOverlay(base, box, m.w, m.h, &o)
}

// routeToRebaseCard gives the card every key and mouse event.
func (m Model) routeToRebaseCard(msg tea.Msg) (Model, tea.Cmd) {
	rs := m.rebase
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		rs.card.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		if m.mouseProbe != nil {
			m.probed("rebase")
			return m, nil
		}
		rs.card.SetSize(m.w, m.h)
		rs.card = rs.card.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
		return m, nil
	case tea.KeyMsg:
		rs.card.SetSize(m.w, m.h)
		var cmd tea.Cmd
		rs.card, cmd = rs.card.Update(msg)
		if cmd == nil {
			return m, nil
		}
		return m, m.onRebaseCardMsg(cmd())
	}
	return m, nil
}

// onRebaseMsg handles the rebase messages that come back from git.
func (m *Model) onRebaseMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case rebaseTodoMsg:
		return m.onRebaseTodo(msg)
	}
	return nil
}

// onRebaseCardMsg runs what the card asked for.
func (m *Model) onRebaseCardMsg(msg tea.Msg) tea.Cmd {
	switch msg.(type) {
	case rebaseview.CloseMsg:
		m.closeRebaseCard()
	}
	return nil
}
