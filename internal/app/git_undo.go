package app

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Undo with the reflog (G-UND): "Git: Undo Last Git Action…" reads HEAD's
// reflog, says what the last action was in plain words and what the undo
// will do, and asks (Cancel first). The undo is `git reset --keep` (it
// never throws local changes away) or a switch back for a checkout.

// gitUndoState is the undo the confirm card asks about.
type gitUndoState struct {
	plan *git.UndoPlan // the plan on the card (nil when no card is open)
	move bool          // the card moves HEAD to a reflog line (Git: Reflog…)
	redo *undoRedo     // the last undo, until it is redone or replaced
}

// undoRedo is what Redo needs: the undo's plan and HEAD right after it.
type undoRedo struct {
	plan  git.UndoPlan
	after string
	move  bool
}

// redoClickMsg is a click on the toast after an undo (or Git: Redo).
type redoClickMsg struct{}

// redoDoneMsg is the end of a redo.
type redoDoneMsg struct {
	plan git.UndoPlan
	move bool
	err  error
}

// undoPlanMsg carries a plan (or why there is none) for the card.
type undoPlanMsg struct {
	plan git.UndoPlan
	move bool
	err  error
}

// undoDoneMsg is the end of an undo.
type undoDoneMsg struct {
	plan git.UndoPlan
	move bool
	res  git.UndoResult
	err  error
}

// gitUndoCmd plans the undo of the last git action off the loop.
func (m *Model) gitUndoCmd() tea.Cmd {
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		p, err := git.PlanUndo(cwd)
		return undoPlanMsg{plan: p, err: err}
	}
}

// onUndoMsg handles the undo messages that come back from git.
func (m *Model) onUndoMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case undoPlanMsg:
		return m.onUndoPlan(msg)
	case undoDoneMsg:
		return m.onUndoDone(msg)
	case redoClickMsg:
		return m.gitRedoCmd()
	case redoDoneMsg:
		return m.onRedoDone(msg)
	}
	return nil
}

// onUndoPlan opens the confirm card (or says why there is nothing to do).
func (m *Model) onUndoPlan(msg undoPlanMsg) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case msg.err != nil:
		return m.undoErrorToast(msg.err)
	case msg.plan.Noop():
		m.toast, cmd = m.toast.PushDetail(toast.Info, "Nothing to undo", "HEAD is there already.")
		return cmd
	}
	m.gitUndo = &gitUndoState{plan: &msg.plan, move: msg.move}
	title, verb := "Undo the last git action?", "Undo"
	if msg.move {
		title, verb = "Move HEAD back to here?", "Move HEAD"
	}
	act := confirm.Button{ID: "undo", Title: verb, Style: confirm.StyleDestructive}
	if len(msg.plan.Blocked) > 0 { // a plain undo would stop
		act = confirm.Button{ID: "undo-stash", Title: "Stash and " + verb, Style: confirm.StyleDestructive}
	}
	m.confirm = confirm.Risky(title, undoConfirmText(msg.plan, msg.move),
		confirm.Button{ID: "cancel", Title: "Cancel"}, act)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindGitUndo
	return nil
}

// undoConfirmText says what the action was and what the undo will do.
func undoConfirmText(p git.UndoPlan, move bool) string {
	e := p.Entry
	what := e.Summary()
	if ago := e.Ago(); ago != "" {
		what += " (" + ago + ")"
	}
	lead := "Last action: "
	if move {
		lead = "Reflog line: "
	}
	lines := []string{lead + what + "."}
	target := fmt.Sprintf("%s '%s'", p.TargetShort(), p.TargetSubject)
	switch {
	case p.Switch != "":
		lines = append(lines, "", "Switch back to "+p.Switch+".")
	case p.Detach:
		lines = append(lines, "", "Go back to "+target+" (detached HEAD).")
	default:
		head := "HEAD"
		if p.Branch != "" {
			head = "HEAD (" + p.Branch + ")"
		}
		lines = append(lines, "", head+" goes back to "+target+".",
			"What it leaves stays in the reflog for 90 days (Git: Reflog…).")
		if p.Pushed > 0 {
			are := "are"
			if p.Pushed == 1 {
				are = "is"
			}
			lines = append(lines, "", fmt.Sprintf("⚠ %s leaving %s %s on %s already:", rebaseCount(p.Pushed), p.Branch, are, p.Upstream),
				"you would need to force-push. termocode never force-pushes.")
		}
	}
	switch {
	case len(p.Blocked) > 0:
		stash := "Stash and Undo"
		if move {
			stash = "Stash and Move HEAD"
		}
		lines = append(lines, "", "Your local changes in "+fileList(p.Blocked)+" are in the way.",
			stash+" puts them aside and back after.")
	case p.Staged && p.Switch == "":
		lines = append(lines, "", "Staged changes stay, but become unstaged.")
	}
	return strings.Join(lines, "\n")
}

// fileList names up to three files: "a.txt, b.txt and 4 more".
func fileList(files []string) string {
	if len(files) <= 3 {
		return strings.Join(files, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(files[:3], ", "), len(files)-3)
}

// onUndoConfirm runs the undo the card asked about (Cancel and Esc: none).
func (m *Model) onUndoConfirm(id string) tea.Cmd {
	if m.gitUndo == nil || m.gitUndo.plan == nil {
		return nil
	}
	p, move := *m.gitUndo.plan, m.gitUndo.move
	m.gitUndo.plan = nil
	if id != "undo" && id != "undo-stash" {
		return nil
	}
	opt := git.UndoOptions{Stash: id == "undo-stash"}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		res, err := git.RunUndo(cwd, p, opt)
		return undoDoneMsg{plan: p, move: move, res: res, err: err}
	}
}

// onUndoDone says how the undo went and refreshes Source Control.
func (m *Model) onUndoDone(msg undoDoneMsg) tea.Cmd {
	if msg.err != nil {
		return m.undoErrorToast(msg.err)
	}
	p := msg.plan
	title := "Undone: " + p.Entry.Summary()
	if msg.move {
		title = "HEAD moved back to " + p.TargetShort()
	}
	detail := fmt.Sprintf("HEAD is at %s '%s'.", p.TargetShort(), p.TargetSubject)
	if p.Switch != "" {
		detail = "Back on " + p.Switch + "."
	}
	var cmd tea.Cmd
	if msg.res.StashKept {
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "Undone, but your changes did not go back cleanly",
			"They are kept in "+msg.res.StashRef+". Fix the conflicts, then drop the stash.")
		return tea.Batch(cmd, fetchGitCmd())
	}
	if m.gitUndo == nil {
		m.gitUndo = &gitUndoState{}
	}
	m.gitUndo.redo = &undoRedo{plan: p, after: msg.res.After, move: msg.move}
	m.toast, cmd = m.toast.PushClick(toast.Info, title, detail+" Click to Redo.", redoClickMsg{})
	return tea.Batch(cmd, fetchGitCmd())
}

// gitRedoCmd takes back the last undo (once), off the loop.
func (m *Model) gitRedoCmd() tea.Cmd {
	if m.gitUndo == nil || m.gitUndo.redo == nil {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Info, "Nothing to redo", "Redo works right after an undo.")
		return cmd
	}
	r := *m.gitUndo.redo
	m.gitUndo.redo = nil
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		return redoDoneMsg{plan: r.plan, move: r.move, err: git.RunRedo(cwd, r.plan, r.after)}
	}
}

// onRedoDone says how the redo went.
func (m *Model) onRedoDone(msg redoDoneMsg) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case errors.Is(msg.err, git.ErrUndoStale):
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "Can't redo: HEAD moved since the undo",
			"Nothing moved. Git: Reflog… still lists where it was.")
		return cmd
	case errors.Is(msg.err, git.ErrUndoLocalChanges):
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "Can't redo: your local changes are in the way",
			"Nothing moved. Commit or stash them first.")
		return cmd
	case msg.err != nil:
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Redo failed", msg.err.Error())
		return cmd
	}
	title := "Redone: " + msg.plan.Entry.Summary()
	if msg.move {
		title = "Redone"
	}
	m.toast, cmd = m.toast.PushDetail(toast.Info, title, "HEAD is back at "+shortHashOf(msg.plan.From)+".")
	return tea.Batch(cmd, fetchGitCmd())
}

// shortHashOf is the first 7 letters of a hash.
func shortHashOf(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// undoErrorToast says why an undo did not run; nothing moved.
func (m *Model) undoErrorToast(err error) tea.Cmd {
	var cmd tea.Cmd
	var busy *git.BusyError
	switch {
	case errors.As(err, &busy) && busy.Op == "rebase":
		return m.rebaseBusyToast() // it points to the banner in Source Control
	case errors.As(err, &busy):
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "A "+busy.Op+" is in progress",
			"Finish it first (fix the conflicts and commit), or stop it: git "+busy.Op+" --abort")
	case errors.Is(err, git.ErrNothingToUndo), errors.Is(err, git.ErrUndoFirstCommit):
		m.toast, cmd = m.toast.PushDetail(toast.Info, "Nothing to undo", err.Error())
	case errors.Is(err, git.ErrUndoLocalChanges):
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "Undo stopped: your local changes are in the way",
			"Nothing moved. Commit or stash them first.")
	case errors.Is(err, git.ErrUndoStale):
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "HEAD moved since the card opened",
			"Nothing moved. Run the undo again.")
	default:
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Undo failed", err.Error())
	}
	return cmd
}

// dispatchUndoPalette handles the undo palette entries.
func (m *Model) dispatchUndoPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "git-undo":
		if !m.gitEnsureLoaded() {
			return nil, true
		}
		return m.gitUndoCmd(), true
	case "git-redo":
		return m.gitRedoCmd(), true
	}
	return nil, false
}

// aiNoGitUndo is why the AI can't run the undo entries (ai_actions_policy.go).
const aiNoGitUndo = "moves HEAD back (undo, redo, reflog); only the user rewinds git history"
