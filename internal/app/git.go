package app

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/git"
)

// gitAutoRefreshMsg fires on a timer so git state (the Source Control list,
// explorer decorations, status-bar branch, and editor diff signs) stays live
// when files change OUTSIDE termocode's own actions — `:w` typed directly in
// nvim, edits/commits run from the integrated terminal, or changes made by
// external tools. A lightweight poll catches every change source uniformly;
// while idle it waits for a change (git_poll_idle.go, git_tree_watch.go). fetchGitCmd
// short-circuits on a cheap git.IsRepo() check when the cwd isn't a repo, so
// polling stays inexpensive.
type gitAutoRefreshMsg struct{}

// gitPollInterval is how often git state is re-polled. 2s balances "feels live"
// against the cost of spawning git status/branch/graph in the background. Users
// who want an instant update can still trigger a manual refresh.
const gitPollInterval = 2 * time.Second

// gitBlurredEvery: while the terminal window is in the background
// (focus events), only every gitBlurredEvery-th tick polls (10 s).
const gitBlurredEvery = 5

// onTerminalFocus handles the terminal's focus events (xterm 1004). In
// the background termocode polls git less often and stops reading the
// clipboard; back in front it reads both at once.
func (m *Model) onTerminalFocus(msg tea.Msg) tea.Cmd {
	clipFocus(msg)
	_, blurred := msg.(tea.BlurMsg)
	m.termBlurred = blurred
	m.gitBlurredSkips = 0
	if blurred || !m.gitLoaded {
		return nil
	}
	return m.pollGitCmd()
}

// gitAutoRefreshTick schedules the next git poll.
func gitAutoRefreshTick() tea.Cmd {
	return tea.Tick(gitPollInterval, func(time.Time) tea.Msg { return gitAutoRefreshMsg{} })
}

// GitMsg carries the latest git snapshot.
type GitMsg struct {
	IsRepo bool
	Branch git.Branch
	Files  []git.FileStatus
	Graph  []git.GraphCommit
	// More: older commits exist past Graph.
	More bool
	// KeepGraph: the poll found HEAD where it was, so Graph is empty and
	// the graph already shown stays.
	KeepGraph bool
	// Rebase: an interactive rebase stopped here (git_rebase.go banner).
	Rebase git.RebaseState
	// Seq: when the read started (gitReadSeq). Refreshes run at the same
	// time and can end out of order; one that started before the last
	// applied one is dropped. 0: always applied.
	Seq uint64
}

// gitReadSeq numbers git reads in the order they start.
var gitReadSeq atomic.Uint64

// gitGraphMaxAge is how long the poll keeps a graph while HEAD stays put.
// Refs that do not move HEAD (a new tag, a fetched branch) show up after
// at most this long.
const gitGraphMaxAge = 30 * time.Second

// gitGraphLimit caps how many commits the Source Control GRAPH section loads.
const gitGraphLimit = 200

// fetchGitCmd asynchronously runs git status + branch + graph and returns a
// GitMsg. One `git status --porcelain=v2 --branch` gives the branch and
// the files (it fails outside a work tree, like rev-parse
// --is-inside-work-tree), and the graph is read at the same time: on a
// big repo the six git processes one after another took twice as long.
func fetchGitCmd() tea.Cmd {
	return func() tea.Msg {
		seq := gitReadSeq.Add(1)
		cwd, err := procRootDir()
		if err != nil {
			return GitMsg{}
		}
		var (
			graph []git.GraphCommit
			more  bool
		)
		done := make(chan struct{})
		crash.Go("reading the git graph", func() {
			defer close(done) // also after a panic, so the refresh goes on
			graph, more, _ = git.Commits(cwd, 0, gitGraphLimit)
		})
		st, err := git.StatusBranch(cwd)
		<-done
		if err != nil {
			return GitMsg{IsRepo: false, Seq: seq}
		}
		return GitMsg{IsRepo: true, Branch: st.Branch, Files: st.Files, Graph: graph, More: more, Rebase: git.RebaseStatus(cwd), Seq: seq}
	}
}

// pollGitCmd is the background poll: one `git status --porcelain=v2
// --branch` (instead of rev-parse, branch, rev-list, status and log). The
// graph is read again only when HEAD, the branch or ahead/behind moved,
// or the graph is older than gitGraphMaxAge.
func (m *Model) pollGitCmd() tea.Cmd {
	graphHead := gitGraphHead(m.gitCommits)
	// Keep the pages "Load older commits" brought in.
	limit := max(gitGraphLimit, len(m.gitCommits))
	branch := m.gitBranch
	stale := clock.Now().Sub(m.gitGraphAt) > gitGraphMaxAge
	cwd, cwdErr := m.rootDir()
	gate := m.gitGate
	return func() tea.Msg {
		if cwdErr != nil {
			return GitMsg{}
		}
		seq := gitReadSeq.Add(1)
		if gate != nil {
			gate.beforeRead()
		}
		st, err := git.StatusBranch(cwd)
		if gate != nil {
			gate.afterRead(cwd, err == nil, st.Files) // after status: it may refresh the index
		}
		if err != nil {
			return GitMsg{IsRepo: false, Seq: seq}
		}
		msg := GitMsg{IsRepo: true, Branch: st.Branch, Files: st.Files, Rebase: git.RebaseStatus(cwd), Seq: seq}
		if !stale && graphHead != "" && strings.HasPrefix(st.OID, graphHead) && st.Branch == branch {
			msg.KeepGraph = true
			return msg
		}
		msg.Graph, msg.More, _ = git.Commits(cwd, 0, limit)
		return msg
	}
}

// applyGitSnapshot stores a git snapshot in the model.
func (m *Model) applyGitSnapshot(msg GitMsg) {
	if msg.Seq != 0 {
		if msg.Seq < m.gitSeq {
			return // an older read that ended after a newer one
		}
		m.gitSeq = msg.Seq
	}
	if msg.KeepGraph {
		msg.Graph, msg.More = m.gitCommits, m.gitGraphMore
	} else {
		m.gitGraphAt = clock.Now()
	}
	if slices.Equal(msg.Files, m.gitFiles) {
		// Same list: keep the old slice so caches keyed on it stay warm.
		msg.Files = m.gitFiles
	}
	// Editor gutter signs compare buffers with HEAD; when HEAD or the
	// changed-file list moved (commit, checkout, pull, discard …),
	// repaint them now instead of on the next buffer event.
	if m.nvim != nil && gitSnapshotChanged(m.gitCommits, m.gitFiles, msg.Graph, msg.Files) {
		_ = m.nvim.ExecLua(`if _G.termocode_git_signs_refresh then _G.termocode_git_signs_refresh() end`)
	}
	m.gitIsRepo = msg.IsRepo
	m.gitLoaded = true
	m.gitBranch = msg.Branch
	m.gitRebase = msg.Rebase
	m.gitFiles = msg.Files
	m.setGitCommits(msg.Graph, msg.More)
	// Clamp against the visible-row count: the accordion interleaves
	// section headers, dir headers, and graph rows, so the row count
	// differs from len(gitFiles).
	m.gitClampCursor()
}

// gitEnsureLoaded reports whether the cwd is a git repo. Before the first
// background refresh has come back it reads the git state at once, so a
// git action the user starts right after launch runs instead of being
// dropped.
func (m *Model) gitEnsureLoaded() bool {
	if !m.gitLoaded {
		if msg, ok := fetchGitCmd()().(GitMsg); ok {
			m.applyGitSnapshot(msg)
		}
	}
	return m.gitIsRepo
}

// gitSnapshotChanged reports whether HEAD (the first commit of the graph)
// or the changed-file list differs between two git snapshots.
func gitSnapshotChanged(oldGraph []git.GraphCommit, oldFiles []git.FileStatus, newGraph []git.GraphCommit, newFiles []git.FileStatus) bool {
	return gitGraphHead(oldGraph) != gitGraphHead(newGraph) || !slices.Equal(oldFiles, newFiles)
}

// gitGraphHead returns the short hash of the first commit in the graph:
// HEAD, since the graph is `git log` of HEAD.
func gitGraphHead(g []git.GraphCommit) string {
	if len(g) == 0 {
		return ""
	}
	return g[0].Hash
}

// gitFileAbsPath returns the absolute path of a git status entry.
func gitFileAbsPath(rel string) string {
	cwd, err := procRootDir()
	if err != nil {
		return rel
	}
	return filepath.Join(cwd, rel)
}

// gitStatusMapCache keeps the last gitStatusMap: it is asked for on
// every frame, and m.gitFiles is only ever replaced as a whole (git.go),
// so the same slice and folder give the same map. The cache holds the
// slice, so its address cannot be reused by a new one.
var gitStatusMapCache struct {
	sync.Mutex
	files []git.FileStatus
	cwd   string
	out   map[string]string
}

// gitStatusMap builds an absolute-path → status-letter map for the
// explorer. Callers must not modify the result (it is cached).
func (m Model) gitStatusMap() map[string]string {
	if !m.gitIsRepo || len(m.gitFiles) == 0 {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	c := &gitStatusMapCache
	c.Lock()
	defer c.Unlock()
	if c.out != nil && c.cwd == cwd && len(c.files) == len(m.gitFiles) && unsafe.SliceData(c.files) == unsafe.SliceData(m.gitFiles) {
		return c.out
	}
	out := buildGitStatusMap(m.gitFiles, cwd)
	c.files, c.cwd, c.out = m.gitFiles, cwd, out
	return out
}

func buildGitStatusMap(files []git.FileStatus, cwd string) map[string]string {
	out := make(map[string]string, len(files))
	for _, f := range files {
		abs := filepath.Join(cwd, f.Path)
		// Pick the more meaningful column: index col first, then worktree col.
		var letter string
		if f.Untracked() {
			letter = "U"
		} else if len(f.Code) >= 1 && f.Code[0] != ' ' {
			letter = string(f.Code[0])
		} else if len(f.Code) >= 2 && f.Code[1] != ' ' {
			letter = string(f.Code[1])
		}
		if letter != "" {
			out[abs] = letter
		}
	}
	return out
}

// gitSidebarHit is a click target of the Source Control view: sidebar
// cells [x0, x1) of one row. row is the accordion row index (-1 for the
// fixed chrome above it).
type gitSidebarHit struct {
	id     string // "scm:toggle-view", "scm:commit-input", "scm:btn:<id>", "scm:row:<i>"
	x0, x1 int
	row    int
}

// gitSidebarHits lists the click targets on sidebar row y (the title is
// row 0) for content width w. The renderer draws with the same helpers
// (gitPanelLayout, gitActionButtons), so a click hits what is drawn.
func (m Model) gitSidebarHits(y, w int) []gitSidebarHit {
	switch {
	case y == gitSubheaderRow:
		// The view-mode toggle glyph sits on the right edge of the sub-header.
		return []gitSidebarHit{{id: "scm:toggle-view", x0: w - gitViewToggleWidth(m.gitViewTree), x1: w, row: -1}}
	case y == gitCommitInputRow:
		return []gitSidebarHit{{id: "scm:commit-input", x0: 0, x1: w, row: -1}}
	case y == gitActionBarRow:
		var out []gitSidebarHit
		for _, b := range m.gitActionButtons(w) {
			if b.x1 >= b.x0 {
				out = append(out, gitSidebarHit{id: "scm:btn:" + b.id, x0: b.x0, x1: b.x1 + 1, row: -1})
			}
		}
		return out
	}
	// Map the row to a body row through the same scroll layout the
	// renderer used.
	top, bodyH, _ := m.gitPanelLayout(m.h)
	bodyRow := y - m.gitPanelTopOffset()
	if bodyRow < 0 || bodyRow >= bodyH {
		return nil
	}
	idx := top + bodyRow
	rows := m.gitPanelRows()
	if idx < 0 || idx >= len(rows) || !rows[idx].selectable() {
		return nil
	}
	return []gitSidebarHit{{id: "scm:row:" + itoa(idx), x0: 0, x1: w, row: idx}}
}

// gitSidebarHitAt returns the target at sidebar cell (x, y).
func (m Model) gitSidebarHitAt(x, y int) (gitSidebarHit, bool) {
	for _, h := range m.gitSidebarHits(y, m.explorerWidth-1) {
		if x >= h.x0 && x < h.x1 {
			return h, true
		}
	}
	return gitSidebarHit{}, false
}

// handleGitSidebarMouse processes a click inside the Git sidebar. x is
// sidebar-relative (0 = first column), y is absolute with the title at row 0.
//
//   - The tree/flat glyph on the title row toggles the view mode.
//   - An accordion section header (CHANGES / GRAPH) folds/unfolds it.
//   - A directory header folds/unfolds it.
//   - A changed file shows its diff (VSCode-style single click).
//   - A graph commit shows that commit's diff.
func (m Model) handleGitSidebarMouse(x, y int, t tea.MouseEventType) (Model, tea.Cmd) {
	if t != tea.MouseLeft {
		return m, nil
	}
	m.focus = FocusExplorer
	if !m.gitEnsureLoaded() {
		return m, nil
	}
	contentW := m.explorerWidth - 1
	// A click anywhere but the message field blurs it (re-focused below if the
	// field itself was clicked).
	if y != gitCommitInputRow {
		m.gitCommitFocused = false
	}
	h, ok := m.gitSidebarHitAt(x, y)
	if !ok {
		return m, nil
	}
	if h.id == "scm:toggle-view" {
		m.toggleGitViewMode()
		return m, nil
	}
	if h.row < 0 {
		// Commit message box + action bar (fixed chrome between the
		// sub-header and the accordion).
		if newM, cmd, ok := m.gitCommitAreaClick(x, y, contentW); ok {
			return newM, cmd
		}
		return m, nil
	}
	idx := h.row
	rows := m.gitPanelRows()
	m.gitCursor = idx
	r := rows[idx]
	switch r.kind {
	case gitRowSection:
		m.gitToggleSection(r.section)
	case gitRowDir:
		m.gitToggleCollapse(r.dirPath)
	case gitRowFile:
		if r.section == gitSecConflicts {
			return m, m.gitOpenMergeViewForFile(r.fileIndex)
		}
		return m, m.gitDiffCmd()
	case gitRowCommit:
		return m, m.gitOpenCommitViewCmd(r.hash)
	case gitRowGroup:
		m.gitToggleGroup(r.group)
	case gitRowMore:
		return m, m.gitShowMore()
	case gitRowRebase:
		m.openRebaseControl()
	}
	return m, nil
}
