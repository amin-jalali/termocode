package app

import (
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/multirepo"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Worktree: New in Each Repository… (CMP-22) ───────────────────────────
//
// A multi-repo workspace: the folders of this window hold several git
// repositories (a folder of services, frontend + backend added as two
// folders). One task often needs a change in more than one of them, so
// this card makes one worktree per repository, on the same branch, in
// one step. Each repository keeps its own git state; termocode only tags
// the worktrees with the branch (worktrees.json "group") so they read as
// one task.
//
// Cancel is first. Each repository has a yes / no row and shows its
// folder; nothing is made until Create. Create checks every repository
// first (branch name, branch or folder there already) and says which one
// is in the way; then git runs off the UI thread, all or nothing: when
// one repository fails, the worktrees made before go again. Create + Add
// Folders also adds the new worktrees as folders of this window.

const (
	wtMultiTitle     = "New worktree in each repository"
	wtMultiPaletteID = "worktree-new-multi"
	wtMultiReposMax  = 8 // repositories on the card
)

// wtMultiForm is the open card.
type wtMultiForm struct {
	repos   []multirepo.Repo
	setting string // config.json worktree_dir
	home    string
	errs    map[string]string // a repository's Check error, by name
	busy    bool
	fail    string
	gen     int
}

// wtMultiPrepMsg is the repositories read for the card.
type wtMultiPrepMsg struct{ f *wtMultiForm }

// wtMultiResolve reads a folder's main checkout and git common dir.
func wtMultiResolve(dir string) (string, string, error) {
	list, common, err := git.ListWorktrees(dir)
	if err != nil {
		return "", "", err
	}
	for _, w := range list {
		if w.Main && !w.Bare {
			return w.Path, common, nil
		}
	}
	return "", "", os.ErrNotExist // a bare repository: no checkout to branch from
}

// wtMultiDirs are the repository folders of this window: the repos found
// under each folder (the git panel's scan).
var wtMultiDirs = func(roots []string) []string {
	var out []string
	for _, r := range roots {
		out = append(out, git.FindRepos(r, gitScanDepth)...)
	}
	return out
}

// openWorktreeNewMulti reads the repositories off the UI thread, then
// opens the card.
func (m *Model) openWorktreeNewMulti() tea.Cmd {
	if m.wt == nil || m.wt.off || !isLocal(m.workspace()) {
		return m.pushToastDetail(toast.Info, "Only on this machine", "A worktree in each repository is made in local folders.")
	}
	roots := m.explorer.Roots()
	if len(roots) == 0 {
		if r, err := m.rootDir(); err == nil {
			roots = []string{r}
		}
	}
	setting := worktreeDirSetting()
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		repos := multirepo.Repos(wtMultiDirs(roots), wtMultiResolve)
		return wtMultiPrepMsg{f: &wtMultiForm{repos: repos, setting: setting, home: home}}
	}
}

// onWTMultiPrep opens the card (two repositories at least).
func (m *Model) onWTMultiPrep(msg wtMultiPrepMsg) tea.Cmd {
	f := msg.f
	if f == nil || len(f.repos) < 2 {
		return m.pushToastDetail(toast.Info, "One repository here", "Add the other repositories as folders of this window (File: Add Folder…), or use Worktree: New….")
	}
	if m.connectOpen {
		return nil
	}
	if len(f.repos) > wtMultiReposMax {
		f.repos = f.repos[:wtMultiReposMax]
	}
	m.wt.multi = f
	m.connect = connectcard.New(wtMultiCardFor(f, "", "", nil))
	m.connect.SetSize(m.w, m.h)
	m.connectOpen = true
	return nil
}

func wtMultiPickID(i int) string { return "repo-" + strconv.Itoa(i) }

// wtMultiCardFor builds the card (pure: tests). picks: a repository's
// choice (0 = yes, 1 = no) by row id.
func wtMultiCardFor(f *wtMultiForm, branch, from string, picks map[string]int) connectcard.Card {
	home := f.home
	b := strings.TrimSpace(branch)
	items := multirepo.Plan(f.repos, b, f.setting, home)
	rows := []connectcard.Row{
		{Kind: connectcard.Input, ID: "branch", Text: "Branch", Inline: true, Placeholder: "feat/my-change", Value: branch},
		{Kind: connectcard.Input, ID: "from", Text: "From", Inline: true, Placeholder: "each repository's current branch", Value: from},
		{Kind: connectcard.Blank},
		{Kind: connectcard.Dim, Text: "One worktree in each repository ticked yes, on the same branch:"},
	}
	for i, it := range items {
		rows = append(rows, connectcard.Row{Kind: connectcard.Choice, ID: wtMultiPickID(i), Text: it.Repo.Name, Options: []string{"yes", "no"}, Selected: picks[wtMultiPickID(i)]})
		where := "type a branch"
		if b != "" {
			where = homeShort(it.Folder, home)
		}
		rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "  " + theme.ASCIIText("→") + " " + where})
		if e := f.errs[it.Repo.Name]; e != "" {
			rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: "  " + it.Repo.Name + ": " + e})
		}
	}
	rows = append(rows, connectcard.Row{Kind: connectcard.Blank})
	switch {
	case f.busy:
		rows = append(rows, connectcard.Row{Kind: connectcard.Status, Text: theme.GlyphWait.String() + " Making the worktrees…"})
	case f.fail != "":
		rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: f.fail})
	default:
		rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "All or nothing: when one repository fails, the others are taken back."})
	}
	return connectcard.Card{Title: wtMultiTitle, Rows: rows, Form: true,
		Buttons: []connectcard.Button{
			{ID: "cancel", Title: "Cancel"},
			{ID: "create", Title: "Create"},
			{ID: "folders", Title: "Create + Add Folders"},
		}}
}

func (m Model) wtMultiOpen() bool {
	return m.wt != nil && m.wt.multi != nil && m.connectOpen && m.connect.Card().Title == wtMultiTitle
}

// wtMultiPicks reads the yes / no rows of the open card.
func (m *Model) wtMultiPicks() map[string]int {
	out := map[string]int{}
	for i := range m.wt.multi.repos {
		id := wtMultiPickID(i)
		out[id] = m.connect.Choice(id)
	}
	return out
}

// refreshWTMulti rebuilds the open card (the folders follow the branch).
func (m *Model) refreshWTMulti() {
	if !m.wtMultiOpen() {
		return
	}
	m.connect.SetCard(wtMultiCardFor(m.wt.multi, m.connect.InputValue("branch"), m.connect.InputValue("from"), m.wtMultiPicks()))
}

// routeToWTMulti sends a key to the card. Enter in a field goes to the
// next one: only a button makes anything.
func (m Model) routeToWTMulti(msg tea.Msg) (Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.Type == tea.KeyEnter && m.connect.FocusedID() != "" {
		msg = tea.KeyMsg{Type: tea.KeyTab}
	}
	var cmd tea.Cmd
	m.connect, cmd = m.connect.Update(msg)
	m.refreshWTMulti()
	return m, cmd
}

// wtMultiCreatedMsg is the end of a Create.
type wtMultiCreatedMsg struct {
	gen        int
	branch     string
	items      []multirepo.Item
	addFolders bool
	err        error
}

// wtMultiCreateRun makes the worktrees, all or nothing (a var: tests).
var wtMultiCreateRun = func(items []multirepo.Item, branch, ref string) error {
	return multirepo.Create(items, branch, ref, git.AddWorktree, func(dir, folder, branch string) error {
		if err := git.RemoveWorktree(dir, folder, true); err != nil {
			return err
		}
		return git.DeleteBranch(dir, branch, true)
	})
}

// wtMultiProbe is the checks before a Create (a var: tests).
var wtMultiProbe = func() multirepo.Probe {
	return multirepo.Probe{
		BranchName:   git.CheckBranchName,
		BranchExists: git.BranchExists,
		FolderExists: func(p string) bool { _, err := os.Lstat(p); return err == nil },
	}
}

// handleWTMultiMsg runs the card's buttons. ok=false: not ours.
func (m *Model) handleWTMultiMsg(msg tea.Msg) (tea.Cmd, bool) {
	if m.wt == nil {
		return nil, false
	}
	if p, ok := msg.(wtMultiPrepMsg); ok {
		return m.onWTMultiPrep(p), true
	}
	if c, ok := msg.(wtMultiCreatedMsg); ok {
		return m.onWTMultiCreated(c), true
	}
	if m.wt.multi == nil || !m.wtMultiOpen() {
		return nil, false
	}
	switch msg := msg.(type) {
	case connectcard.ButtonMsg:
		switch msg.ID {
		case "cancel":
			m.closeWTMulti()
		case "create", "folders":
			return m.wtMultiCreate(msg.ID == "folders"), true
		}
		return nil, true
	case connectcard.SubmitMsg, connectcard.KeyMsg:
		return nil, true
	case connectcard.CloseMsg:
		m.closeWTMulti()
		return nil, true
	}
	return nil, false
}

// closeWTMulti closes the card; nothing is made.
func (m *Model) closeWTMulti() {
	if m.wtMultiOpen() {
		m.connectOpen = false
	}
	m.wt.multi = nil
}

// wtMultiCreate checks the card and makes the worktrees in the
// background.
func (m *Model) wtMultiCreate(addFolders bool) tea.Cmd {
	f := m.wt.multi
	if f == nil || f.busy {
		return nil
	}
	branch := strings.TrimSpace(m.connect.InputValue("branch"))
	from := strings.TrimSpace(m.connect.InputValue("from"))
	picks := m.wtMultiPicks()
	f.fail, f.errs = "", nil
	var items []multirepo.Item
	for i, it := range multirepo.Plan(f.repos, branch, f.setting, f.home) {
		if picks[wtMultiPickID(i)] == 0 {
			items = append(items, it)
		}
	}
	switch {
	case branch == "":
		f.fail = "Type a branch."
	case len(items) == 0:
		f.fail = "Tick yes on one repository at least."
	case strings.HasPrefix(from, "-"):
		f.fail = "From: " + strconv.Quote(from) + " is not a ref."
	}
	if f.fail == "" && !multirepo.Check(items, branch, wtMultiProbe()) {
		f.errs = map[string]string{}
		for _, it := range items {
			if it.Err != "" {
				f.errs[it.Repo.Name] = it.Err
			}
		}
		f.fail = "Nothing was made: fix the repositories above."
	}
	if f.fail != "" {
		m.connect.SetCard(wtMultiCardFor(f, branch, from, picks))
		return nil
	}
	f.busy = true
	f.gen++
	gen := f.gen
	m.connect.SetCard(wtMultiCardFor(f, branch, from, picks))
	return func() tea.Msg {
		err := wtMultiCreateRun(items, branch, from)
		return wtMultiCreatedMsg{gen: gen, branch: branch, items: items, addFolders: addFolders, err: err}
	}
}

// onWTMultiCreated tags the new worktrees with their group, or keeps the
// card with git's words.
func (m *Model) onWTMultiCreated(msg wtMultiCreatedMsg) tea.Cmd {
	f := m.wt.multi
	if f == nil || msg.gen != f.gen {
		return nil
	}
	f.busy = false
	if msg.err != nil {
		f.fail = "Nothing was made. " + wtCreateError(msg.err)
		if m.wtMultiOpen() {
			m.connect.SetCard(wtMultiCardFor(f, m.connect.InputValue("branch"), m.connect.InputValue("from"), m.wtMultiPicks()))
		}
		return nil
	}
	m.closeWTMulti()
	now := clock.Now()
	var names []string
	for _, it := range msg.items {
		folder := it.Folder
		err := worktrees.Update(it.Repo.Common, func(all map[string]worktrees.Meta) {
			md := all[folder]
			md.Group = msg.branch
			md.Created, md.LastSeen = now, now
			all[folder] = md
		})
		if err != nil {
			recordError("[worktrees] " + it.Repo.Name + ": " + err.Error())
		}
		names = append(names, it.Repo.Name)
	}
	cmds := []tea.Cmd{
		m.pushToastDetail(toast.Info, "Worktree "+msg.branch+" is ready in "+wtReposWord(len(msg.items)), strings.Join(names, theme.ASCIIText(" · "))),
		m.worktreesReload(),
	}
	if msg.addFolders {
		for _, it := range msg.items {
			cmds = append(cmds, wtAddFolder(m, it.Folder))
		}
	}
	return tea.Batch(cmds...)
}

func init() {
	aiActionPolicies[wtMultiPaletteID] = aiNever(aiNoPicker + " (makes branches and folders in several repositories)")
}

// wtReposWord is "1 repository" or "N repositories".
func wtReposWord(n int) string {
	if n == 1 {
		return "1 repository"
	}
	return strconv.Itoa(n) + " repositories"
}
