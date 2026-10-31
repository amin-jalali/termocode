package app

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/rebaseview"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Interactive rebase (G-RBS): R on a GRAPH commit (or "Rebase from
// Here…") opens the rebase card (internal/rebaseview) on that commit and
// every newer one; the user sets an action per row and the order, then
// starts it. git runs it with termocode as its editor
// (internal/git/rebase_helper.go).

// gitRebaseState is the open rebase card.
type gitRebaseState struct {
	open   bool
	card   rebaseview.Model
	reword rebaseview.RewordMsg // the row the reword prompt is for
	start  *rebaseCheckMsg      // the run the confirm card asks about
}

// rebaseCheckMsg is what the confirm card says before a run.
type rebaseCheckMsg struct {
	todo     git.RebaseTodo
	upstream string // the branch's upstream ("origin/main")
	pushed   int    // commits of the range on it already
	dirty    bool   // uncommitted changes: the run must stash them
}

// rebaseDoneMsg is the end of a run (or of its first stretch, when git
// stopped).
type rebaseDoneMsg struct {
	verb    string // "" (a new run), "continue", "skip", "abort"
	summary string
	out     git.RebaseOutcome
	err     error
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
	if m.gitRebase.InProgress {
		return m.rebaseBusyToast()
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
	case rebaseCheckMsg:
		m.openRebaseConfirm(msg)
	case rebaseDoneMsg:
		return m.onRebaseDone(msg)
	case fixupCheckMsg:
		return m.openFixupConfirm(msg)
	}
	return nil
}

// onRebaseCardMsg runs what the card asked for.
func (m *Model) onRebaseCardMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case rebaseview.CloseMsg:
		m.closeRebaseCard()
	case rebaseview.RewordMsg:
		m.openRebaseRewordPrompt(msg)
	case rebaseview.StartMsg:
		return m.rebaseCheckCmd(msg.Todo)
	}
	return nil
}

// rebaseCheckCmd reads what the confirm card must say.
func (m *Model) rebaseCheckCmd(todo git.RebaseTodo) tea.Cmd {
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		msg := rebaseCheckMsg{todo: todo}
		msg.upstream, msg.pushed = git.RebasePushed(cwd, todo.Base)
		msg.dirty = git.WorkTreeDirty(cwd)
		return msg
	}
}

// openRebaseConfirm asks before the run: Cancel first and focused (the
// rebase card stays open behind it).
func (m *Model) openRebaseConfirm(msg rebaseCheckMsg) {
	if !m.rebaseCardOpen() {
		return
	}
	m.rebase.start = &msg
	body := rebaseConfirmText(msg)
	start := confirm.Button{ID: "rebase-start", Title: "Start Rebase", Style: confirm.StyleDestructive}
	if msg.dirty {
		start = confirm.Button{ID: "rebase-stash", Title: "Stash and Continue", Style: confirm.StyleDestructive}
	}
	m.confirm = confirm.Risky("Start the rebase?", body, confirm.Button{ID: "cancel", Title: "Cancel"}, start)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindRebaseStart
}

// rebaseConfirmText lists what will happen.
func rebaseConfirmText(msg rebaseCheckMsg) string {
	todo := msg.todo
	onto := "the root commit"
	if len(todo.Base) >= 7 {
		onto = todo.Base[:7]
	}
	lines := []string{fmt.Sprintf("Replay %s onto %s: %s.", rebaseCount(len(todo.Items)), onto, todo.Summary())}
	counts := map[git.RebaseAction]int{}
	for _, it := range todo.Items {
		counts[it.Action]++
	}
	var parts []string
	for _, a := range []git.RebaseAction{git.RebaseReword, git.RebaseEdit, git.RebaseSquash, git.RebaseFixup, git.RebaseDrop} {
		if n := counts[a]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", a, n))
		}
	}
	if len(parts) > 0 {
		lines = append(lines, strings.Join(parts, " · "))
	}
	if counts[git.RebaseEdit] > 0 {
		lines = append(lines, "It stops at each edit row so you can change the commit.")
	}
	if msg.pushed > 0 {
		these := "these commits are"
		if msg.pushed < len(todo.Items) {
			these = fmt.Sprintf("%d of these commits are", msg.pushed)
		}
		lines = append(lines, "", fmt.Sprintf("⚠ %s on %s already; you will need to force-push.", these, msg.upstream),
			"termocode never force-pushes for you.")
	}
	if msg.dirty {
		lines = append(lines, "", "You have uncommitted changes. Stash and Continue puts them aside",
			"and back after the rebase.")
	}
	lines = append(lines, "", "Undo afterwards: git reset --keep ORIG_HEAD")
	return strings.Join(lines, "\n")
}

func rebaseCount(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

// onRebaseConfirm runs the rebase (or goes back to the card).
func (m *Model) onRebaseConfirm(id string) tea.Cmd {
	if m.rebase == nil || m.rebase.start == nil {
		return nil
	}
	check := *m.rebase.start
	m.rebase.start = nil
	if id != "rebase-start" && id != "rebase-stash" {
		return nil // Cancel: the card is still open
	}
	opt := git.RebaseOptions{Autostash: id == "rebase-stash"}
	m.closeRebaseCard()
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		out, err := git.RunRebase(cwd, check.todo, opt)
		return rebaseDoneMsg{summary: check.todo.Summary(), out: out, err: err}
	}
}

// onRebaseDone says how the run went and refreshes Source Control.
func (m *Model) onRebaseDone(msg rebaseDoneMsg) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case msg.verb == "abort" && msg.err == nil:
		m.toast, cmd = m.toast.Push(toast.Info, "Rebase aborted: the branch is back as it was")
	case msg.verb == "continue" && msg.err != nil:
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "Can't continue yet", msg.err.Error())
	case msg.err != nil:
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Rebase failed", msg.err.Error())
	case len(msg.out.Conflicts) > 0:
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "Rebase stopped: "+conflictWord(len(msg.out.Conflicts)),
			"Fix them in CONFLICTS, then Continue in Source Control. Abort puts everything back.")
	case msg.out.Stopped:
		m.toast, cmd = m.toast.PushDetail(toast.Info, "Rebase stopped to edit a commit",
			"Change the commit (Commit (Amend)), then Continue in Source Control.")
	case msg.summary == "":
		m.toast, cmd = m.toast.PushDetail(toast.Info, "Rebase done", "Undo: u / Git: Undo Last Git Action")
	default:
		m.toast, cmd = m.toast.PushDetail(toast.Info, "Rebase done: "+msg.summary,
			"Undo: u / Git: Undo Last Git Action")
	}
	return tea.Batch(cmd, fetchGitCmd())
}

// conflictWord is "1 conflict" / "3 conflicts".
func conflictWord(n int) string {
	if n == 1 {
		return "1 conflict"
	}
	return fmt.Sprintf("%d conflicts", n)
}

// openRebaseRewordPrompt asks for a reword row's new subject; the body
// of the message stays.
func (m *Model) openRebaseRewordPrompt(msg rebaseview.RewordMsg) {
	m.rebase.reword = msg
	subject := msg.Item.Subject
	if msg.Item.Message != "" {
		subject, _, _ = strings.Cut(msg.Item.Message, "\n")
	}
	m.prompt = prompt.New("Reword "+msg.Item.Hash, "New subject (the body stays):", subject)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindRebaseReword
}

// onRebaseReword gives the reword row its new message: the typed subject
// and the commit's body. An empty subject keeps the old message.
func (m *Model) onRebaseReword(subject string) {
	if !m.rebaseCardOpen() {
		return
	}
	rw := m.rebase.reword
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return
	}
	body := rw.Item.Body
	if rw.Item.Message != "" {
		_, body, _ = strings.Cut(rw.Item.Message, "\n")
	}
	msg := subject
	if body = strings.TrimSpace(body); body != "" {
		msg += "\n\n" + body
	}
	m.rebase.card.SetMessage(rw.Index, msg)
}

// rebaseBusyToast says a rebase is stopped already.
func (m *Model) rebaseBusyToast() tea.Cmd {
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Warn, "A rebase is in progress",
		"Finish it first: Continue, Skip or Abort from the banner in Source Control.")
	return cmd
}

// renderGitRebaseRow draws the Source Control banner of a stopped rebase.
func (m Model) renderGitRebaseRow(active, focused bool, w int) string {
	st := m.gitRebase
	bg := gitSelBg(active, focused)
	label := fmt.Sprintf("⇅ Rebasing %d/%d", st.Step, st.Total)
	switch {
	case len(st.Conflicts) > 0:
		label += " · " + conflictWord(len(st.Conflicts))
	case st.Action == "edit":
		label += " · edit " + st.Hash
	default:
		label += " · stopped"
	}
	label += " ›"
	fg := theme.LG(theme.AccentAmber)
	var sb strings.Builder
	sb.WriteString(gitLead(active, focused, bg))
	sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(true).Render("  " + label))
	return clampSidebarRow(sb.String(), 1+2+runewidth.StringWidth(label), w, lipgloss.NewStyle().Background(bg))
}

// openRebaseControl asks what to do with the stopped rebase. Close is
// first and has the focus, so Enter or Esc never throw conflict fixes
// away; Abort puts the branch back as it was.
func (m *Model) openRebaseControl() {
	st := m.gitRebase
	if !st.InProgress {
		return
	}
	var lines []string
	where := fmt.Sprintf("Step %d of %d", st.Step, st.Total)
	if st.Hash != "" {
		where = fmt.Sprintf("Stopped at %s %q (%s %d of %d)", st.Hash, st.Subject, st.Action, st.Step, st.Total)
	}
	if st.Branch != "" {
		where += " on " + st.Branch
	}
	lines = append(lines, where+".")
	switch {
	case len(st.Conflicts) > 0:
		lines = append(lines, "", conflictWord(len(st.Conflicts))+": "+strings.Join(st.Conflicts, ", "),
			"Fix them in CONFLICTS, then Continue (it stages the fixed files).")
	case st.Action == "edit":
		lines = append(lines, "", "Change the commit (Commit (Amend)), then Continue.")
	}
	lines = append(lines, "", "Continue goes on · Skip drops this commit · Abort puts the branch back as it was.")
	m.confirm = confirm.Risky("Rebase in progress", strings.Join(lines, "\n"),
		confirm.Button{ID: "rebase-close", Title: "Close"},
		confirm.Button{ID: "rebase-abort", Title: "Abort", Style: confirm.StyleDestructive},
		confirm.Button{ID: "rebase-skip", Title: "Skip"},
		confirm.Button{ID: "rebase-continue", Title: "Continue", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindRebaseControl
}

// onRebaseControl runs Continue, Skip or Abort off the loop.
func (m *Model) onRebaseControl(id string) tea.Cmd {
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	verb := strings.TrimPrefix(id, "rebase-")
	if verb != "continue" && verb != "skip" && verb != "abort" {
		return nil // Close, Esc
	}
	return func() tea.Msg {
		msg := rebaseDoneMsg{verb: verb}
		switch verb {
		case "continue":
			msg.out, msg.err = git.RebaseContinue(cwd, git.RebaseOptions{})
		case "skip":
			msg.out, msg.err = git.RebaseSkip(cwd, git.RebaseOptions{})
		default: // "abort"
			msg.err = git.RebaseAbort(cwd)
		}
		return msg
	}
}

// dispatchRebasePalette handles the rebase palette entries.
func (m *Model) dispatchRebasePalette(id string) (tea.Cmd, bool) {
	switch id {
	case "git-rebase":
		return m.openCommitTargetPicker(pickerKindRebaseFrom, " Rebase from which commit? (it and every newer one) "), true
	case "git-fixup":
		if !m.gitEnsureLoaded() {
			return nil, true
		}
		if staged, _ := m.gitFileCounts(); staged == 0 {
			return m.nothingStagedToast(), true
		}
		return m.openCommitTargetPicker(pickerKindFixupInto, " Fixup the staged changes into which commit? "), true
	case "git-rebase-status":
		if !m.gitEnsureLoaded() {
			return nil, true
		}
		if !m.gitRebase.InProgress {
			var cmd tea.Cmd
			m.toast, cmd = m.toast.Push(toast.Info, "No rebase in progress")
			return cmd, true
		}
		m.openRebaseControl()
		return nil, true
	}
	return nil, false
}

// openCommitTargetPicker lists HEAD's commits down to the first merge
// (newest first) for the rebase and fixup palette entries.
func (m *Model) openCommitTargetPicker(kind pickerKindEnum, title string) tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	if m.gitRebase.InProgress {
		return m.rebaseBusyToast()
	}
	var items []picker.Item
	for _, c := range m.gitCommits {
		if c.IsMerge {
			break
		}
		items = append(items, picker.Item{ID: c.FullHash, Title: c.Hash + "  " + c.Subject, Hint: git.CompactAge(c.Committed)})
	}
	if len(items) == 0 {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.Push(toast.Info, "No commits to rebase")
		return cmd
	}
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = kind
	return nil
}
