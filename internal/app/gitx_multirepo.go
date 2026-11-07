package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// Several repositories in one workspace, and submodules (GITX-31; gap
// rows M6, M7, M8, M15, M16).
//
// The workspace folder can hold several repositories (a folder of
// projects, or nested repos inside a repo). Source Control works on one
// of them at a time: the active repository. Every git action of the
// panel, the graph and the editor runs there (gitRootDir). The
// REPOSITORIES section lists all repos the scan found (branch and
// change count) and Enter on one makes it active; Git: Switch
// Repository… does the same from the palette. When the workspace folder
// is no repository itself, the first repo found becomes active, so the
// panel shows something useful at once.
//
// The SUBMODULES section lists the active repo's submodules and their
// state. Enter on one offers Open (make it the active repo) and Update
// (a Cancel-first card, then `git submodule update --init --recursive`
// for it); Git: Update Submodules… updates all of them.

// gitActiveRepo is the active repository folder ("" or nil: the
// workspace folder). It is process-wide, like the workspace: commands
// built off the Model (procGitRootDir) read it too.
var gitActiveRepo atomic.Pointer[string]

// setGitActiveRepo makes dir the active repository ("": the workspace
// folder).
func setGitActiveRepo(dir string) { gitActiveRepo.Store(&dir) }

// gitScanDepth is how many folders down the repo scan looks.
const gitScanDepth = 3

// gitRootOf is the folder git runs in for ws: the active repository
// when one is set, still exists and lies inside a local workspace;
// otherwise the workspace folder.
func gitRootOf(ws workspace.Workspace) (string, error) {
	root, err := rootOf(ws)
	if err != nil || !isLocal(ws) {
		return root, err
	}
	p := gitActiveRepo.Load()
	if p == nil || *p == "" || *p == root {
		return root, nil
	}
	if rel, err := filepath.Rel(root, *p); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return root, nil
	}
	if st, err := os.Stat(*p); err != nil || !st.IsDir() {
		return root, nil
	}
	return *p, nil
}

// gitRootDir is the folder of the active repository of the Model's
// workspace.
func (m Model) gitRootDir() (string, error) { return gitRootOf(m.workspace()) }

// procGitRootDir is gitRootDir for code with no Model at hand.
func procGitRootDir() (string, error) { return gitRootOf(procWorkspace()) }

// gitRepoInfo is one repository the scan found.
type gitRepoInfo struct {
	dir     string
	branch  string
	changes int
}

// gitReposMsg carries a repo scan; pick opens the repo picker on it.
type gitReposMsg struct {
	root  string
	repos []gitRepoInfo
	pick  bool
}

// gitScanReposMax caps how many repos the scan reads the status of.
const gitScanReposMax = 20

// gitScanReposCmd finds the repositories of a local workspace and reads
// the branch and change count of each.
func gitScanReposCmd(ws workspace.Workspace, pick bool) tea.Cmd {
	root, err := rootOf(ws)
	if err != nil || !isLocal(ws) {
		return nil
	}
	return func() tea.Msg {
		dirs := git.FindRepos(root, gitScanDepth)
		if len(dirs) > gitScanReposMax {
			dirs = dirs[:gitScanReposMax]
		}
		repos := make([]gitRepoInfo, 0, len(dirs))
		for _, d := range dirs {
			r := gitRepoInfo{dir: d}
			if st, err := git.StatusBranch(d); err == nil {
				r.branch, r.changes = st.Branch.Name, len(st.Files)
			}
			repos = append(repos, r)
		}
		return gitReposMsg{root: root, repos: repos, pick: pick}
	}
}

// gitReposScanCmd runs the first repo scan of this Model, once.
func (m *Model) gitReposScanCmd() tea.Cmd {
	if m.gitReposScanned {
		return nil
	}
	m.gitReposScanned = true
	return gitScanReposCmd(m.workspace(), false)
}

// onGitRepos stores a scan. When no repo is active and the workspace
// folder is none, the first repo found becomes active.
func (m *Model) onGitRepos(msg gitReposMsg) tea.Cmd {
	if root, err := m.rootDir(); err != nil || root != msg.root {
		return nil // a scan of another workspace
	}
	m.gitRepos = msg.repos
	m.gitClampCursor()
	var cmd tea.Cmd
	if p := gitActiveRepo.Load(); (p == nil || *p == "") && !m.gitIsRepo && len(msg.repos) > 0 && msg.repos[0].dir != msg.root {
		cmd = m.gitSwitchRepo(msg.repos[0].dir)
	}
	if msg.pick {
		m.openGitRepoPicker()
	}
	return cmd
}

// gitRepoLabel is the repo's folder relative to the workspace ("." is
// the workspace folder's own name).
func (m Model) gitRepoLabel(dir string) string {
	root, err := m.rootDir()
	if err != nil {
		return filepath.Base(dir)
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return filepath.Base(dir)
	}
	return filepath.ToSlash(rel)
}

// openGitRepoPicker lists the scanned repos; the active one is marked.
func (m *Model) openGitRepoPicker() {
	if len(m.gitRepos) == 0 {
		m.gitxInfo("No git repository in this workspace")
		return
	}
	cur, _ := m.gitRootDir()
	items := make([]picker.Item, 0, len(m.gitRepos))
	for _, r := range m.gitRepos {
		hint := r.branch
		if r.changes > 0 {
			hint += " · " + itoa(r.changes) + " change" + plural(r.changes)
		}
		if r.dir == cur {
			hint = "active · " + hint
		}
		items = append(items, picker.Item{ID: r.dir, Title: m.gitRepoLabel(r.dir), Hint: strings.TrimSuffix(hint, " · ")})
	}
	m.gitxPick("Switch Repository", items, func(m *Model, id string) tea.Cmd {
		return m.gitSwitchRepo(id)
	})
}

// gitSwitchRepo makes dir the active repository and reads it. The view
// of the old repo (files, graph, marks) goes at once.
func (m *Model) gitSwitchRepo(dir string) tea.Cmd {
	if cur, _ := m.gitRootDir(); cur == dir {
		return nil
	}
	setGitActiveRepo(dir)
	m.gitFiles, m.gitCommits, m.gitStashes, m.gitSubmodules = nil, nil, nil, nil
	m.gitBranch = git.Branch{}
	m.gitCursor = 0
	m.gitClampCursor()
	return tea.Batch(fetchGitCmd(), gitScanReposCmd(m.workspace(), false))
}

// appendRepoRows adds the REPOSITORIES section while the scan found
// more than one repo.
func (m Model) appendRepoRows(rows []gitPanelRow) []gitPanelRow {
	if len(m.gitRepos) < 2 {
		return rows
	}
	rows = append(rows, gitPanelRow{kind: gitRowSpacer}, gitPanelRow{kind: gitRowSection, section: gitSecRepos})
	if !m.gitReposFolded {
		for i := range m.gitRepos {
			rows = append(rows, gitPanelRow{kind: gitRowRepo, repo: i})
		}
	}
	return rows
}

// appendSubmoduleRows adds the SUBMODULES section while the active repo
// has submodules.
func (m Model) appendSubmoduleRows(rows []gitPanelRow) []gitPanelRow {
	if len(m.gitSubmodules) == 0 {
		return rows
	}
	rows = append(rows, gitPanelRow{kind: gitRowSpacer}, gitPanelRow{kind: gitRowSection, section: gitSecSubmodules})
	if !m.gitSubsFolded {
		for i := range m.gitSubmodules {
			rows = append(rows, gitPanelRow{kind: gitRowSubmodule, sub: i})
		}
	}
	return rows
}

// onRepoRow makes repo i active.
func (m *Model) onRepoRow(i int) tea.Cmd {
	if i < 0 || i >= len(m.gitRepos) {
		return nil
	}
	return m.gitSwitchRepo(m.gitRepos[i].dir)
}

// onSubmoduleRow offers the actions of submodule i.
func (m *Model) onSubmoduleRow(i int) tea.Cmd {
	if i < 0 || i >= len(m.gitSubmodules) {
		return nil
	}
	s := m.gitSubmodules[i]
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(cwd, filepath.FromSlash(s.Path))
	var items []picker.Item
	if s.State != git.SubmoduleNotInit {
		items = append(items, picker.Item{ID: "open", Title: "Open as the active repository", Hint: s.Path})
	}
	title := "Update (check out the recorded commit)"
	if s.State == git.SubmoduleNotInit {
		title = "Init and update"
	}
	items = append(items, picker.Item{ID: "update", Title: title, Hint: s.Path})
	m.gitxPick("Submodule "+s.Path, items, func(m *Model, id string) tea.Cmd {
		switch id {
		case "open":
			return m.gitSwitchRepo(dir)
		case "update":
			m.gitSubmoduleUpdateCard(cwd, s.Path)
		}
		return nil
	})
	return nil
}

// gitSubmoduleUpdateCard asks before `git submodule update` (path "":
// all). It moves the submodule's HEAD (detached) to the recorded commit;
// git refuses when local changes there would be lost.
func (m *Model) gitSubmoduleUpdateCard(cwd, path string) {
	title, what := "Update Submodule?", "Check out the commit this repository records in "+path+"."
	if path == "" {
		title, what = "Update Submodules?", "Init the new submodules and check out the commit this repository records in each one."
	}
	m.gitxCard(title, what+"\nCommits made there on no branch can then only be found in its reflog. Git stops if local changes would be lost.",
		func(m *Model, id string) tea.Cmd {
			if id != "update" {
				return nil
			}
			return func() tea.Msg {
				err := git.SubmoduleUpdate(cwd, path)
				return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
					if err != nil {
						return m.gitxFail("Submodule update failed", err)
					}
					return tea.Batch(m.gitxInfo("Submodules updated"), fetchGitCmd())
				}}
			}
		}, confirm.Button{ID: "update", Title: "Update"})
}

// errNoSubmodules: Update Submodules… in a repo without any.
var errNoSubmodules = errors.New("this repository has no submodules")

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-repo-pick", Title: "Git: Switch Repository…"},
			{ID: "git-submodule-update", Title: "Git: Update Submodules…"},
		},
		policies: map[string]aiActionPolicy{
			"git-repo-pick":        aiAuto(),
			"git-submodule-update": aiConfirm(ai.ClassGit),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-repo-pick":
				c := gitScanReposCmd(m.workspace(), true)
				if c == nil {
					return m.gitxInfo("Repositories are listed for a folder on this machine only"), true
				}
				return c, true
			case "git-submodule-update":
				if !m.gitEnsureLoaded() {
					return nil, true
				}
				cwd, err := m.gitRootDir()
				if err != nil {
					return nil, true
				}
				if subs, _ := git.Submodules(cwd); len(subs) == 0 {
					return m.gitxInfo(errNoSubmodules.Error()), true
				}
				m.gitSubmoduleUpdateCard(cwd, "")
				return nil, true
			}
			return nil, false
		},
	})
}

// gitSubmoduleStateText is the dim text after a submodule's path.
func gitSubmoduleStateText(s git.Submodule) string {
	switch s.State {
	case git.SubmoduleNotInit:
		return "not initialized"
	case git.SubmoduleChanged:
		return "other commit"
	case git.SubmoduleConflict:
		return "conflict"
	}
	if s.Describe != "" {
		return s.Describe
	}
	return s.SHA[:min(7, len(s.SHA))]
}

// renderGitRepoRow is " ● <name>  <branch> <changes>": the active repo
// has the dot.
func (m Model) renderGitRepoRow(i int, active, focused bool, w int) string {
	if i < 0 || i >= len(m.gitRepos) {
		return sidebarFill.Render(strings.Repeat(" ", w))
	}
	r := m.gitRepos[i]
	cur, _ := m.gitRootDir()
	mark := "  "
	if r.dir == cur {
		mark = "● "
	}
	right := r.branch
	if r.changes > 0 {
		right += " " + itoa(r.changes)
	}
	return m.renderGitTwoPartRow(mark+m.gitRepoLabel(r.dir), right, active, focused, w)
}

// renderGitSubmoduleRow is "   <path>  <state>".
func (m Model) renderGitSubmoduleRow(i int, active, focused bool, w int) string {
	if i < 0 || i >= len(m.gitSubmodules) {
		return sidebarFill.Render(strings.Repeat(" ", w))
	}
	s := m.gitSubmodules[i]
	return m.renderGitTwoPartRow("  "+s.Path, gitSubmoduleStateText(s), active, focused, w)
}

// renderGitTwoPartRow draws a name, then dim text; the dim part goes
// first when the row is too narrow.
func (m Model) renderGitTwoPartRow(name, dim string, active, focused bool, w int) string {
	bg := gitSelBg(active, focused)
	fgName, fgDim := theme.LG(theme.TextPrimary), theme.LG(theme.TextMuted)
	if active && focused {
		fgName, fgDim = theme.LG(theme.TextWhite), theme.Mix(theme.TextSecondary, theme.BorderFocus, 0.3)
	}
	rowStyle := lipgloss.NewStyle().Background(bg)
	avail := w - 2
	if runewidth.StringWidth(name)+2+runewidth.StringWidth(dim) > avail {
		dim = ""
	}
	if runewidth.StringWidth(name) > avail {
		name = runewidth.Truncate(name, max(avail, 1), "…")
	}
	var sb strings.Builder
	sb.WriteString(gitLead(active, focused, bg))
	sb.WriteString(rowStyle.Render(" "))
	sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(fgName).Render(name))
	used := 1 + 1 + runewidth.StringWidth(name)
	if dim != "" {
		sb.WriteString(rowStyle.Render("  "))
		sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(fgDim).Render(dim))
		used += 2 + runewidth.StringWidth(dim)
	}
	return clampSidebarRow(sb.String(), used, w, rowStyle)
}
