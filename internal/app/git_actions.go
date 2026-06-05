package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/confirm"
	"termocode/internal/git"
	"termocode/internal/picker"
	"termocode/internal/prompt"
	"termocode/internal/toast"
)

// handleGitSidebarKey routes key presses when the Git sidebar has focus.
// Returns (model, cmd, true) if the key was consumed; (model, nil, false)
// otherwise so the caller can fall through to other handlers.
//
// Bindings (mirroring VSCode-ish habits while staying single-keystroke
// friendly for the terminal):
//
//	j / down, k / up   move cursor
//	g / G              jump top / bottom
//	enter              open the file in the editor
//	s                  stage / unstage (toggle, picks the right one based on
//	                   whether the file already has staged changes)
//	d                  diff vs HEAD (open in preview overlay)
//	c                  commit (opens a text-input prompt)
//	x                  discard worktree changes (with confirm)
//	r                  refresh
func (m Model) handleGitSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !m.gitIsRepo {
		return m, nil, false
	}
	// When the always-visible message box has focus, keystrokes edit it (Enter
	// commits, Esc/Tab return to the accordion).
	if m.gitCommitFocused {
		return m.handleGitCommitKey(msg)
	}
	// Navigation and selection operate over the unified accordion rows
	// (section headers + change files/dirs + graph commits) so every section
	// shares one cursor model. Connector lines are skipped by gitMoveCursor.
	cur, hasCur := m.gitCurrentRow()

	switch msg.Type {
	case tea.KeyDown:
		m.gitMoveCursor(1)
		return m, nil, true
	case tea.KeyUp:
		m.gitMoveCursor(-1)
		return m, nil, true
	case tea.KeyLeft:
		// Collapse the section/dir under the cursor.
		if hasCur {
			switch {
			case cur.kind == gitRowSection:
				m.gitToggleSection(cur.section)
			case cur.kind == gitRowDir && !m.gitCollapsed[cur.dirPath]:
				m.gitToggleCollapse(cur.dirPath)
			}
		}
		return m, nil, true
	case tea.KeyRight:
		if hasCur {
			switch {
			case cur.kind == gitRowSection:
				m.gitToggleSection(cur.section)
			case cur.kind == gitRowDir && m.gitCollapsed[cur.dirPath]:
				m.gitToggleCollapse(cur.dirPath)
			}
		}
		return m, nil, true
	case tea.KeyEnter:
		if !hasCur {
			return m, nil, true
		}
		switch cur.kind {
		case gitRowSection:
			m.gitToggleSection(cur.section)
		case gitRowDir:
			m.gitToggleCollapse(cur.dirPath)
		case gitRowCommit:
			return m, m.gitShowCommitCmd(cur.hash), true
		case gitRowFile:
			path := gitFileAbsPath(m.gitFiles[cur.fileIndex].Path)
			if m.nvim != nil {
				m.ensureEditorWindowCurrent()
				_ = m.nvim.Command("edit " + path)
			}
			m.focus = FocusEditor
		}
		return m, nil, true
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return m, nil, false
		}
		switch msg.Runes[0] {
		case 'j':
			m.gitMoveCursor(1)
			return m, nil, true
		case 'k':
			m.gitMoveCursor(-1)
			return m, nil, true
		case 'g':
			m.gitCursor = 0
			m.gitClampCursor()
			return m, nil, true
		case 'G':
			m.gitCursor = len(m.gitPanelRows()) - 1
			m.gitClampCursor()
			return m, nil, true
		case 's':
			return m, m.gitStageToggle(), true
		case 'd':
			// On a graph commit, diff that commit; otherwise the file's diff.
			if hasCur && cur.kind == gitRowCommit {
				return m, m.gitShowCommitCmd(cur.hash), true
			}
			return m, m.gitDiffCmd(), true
		case 'c':
			// Focus the always-visible message box (the multi-line prompt
			// remains reachable from the ⋯ menu / right-click "Commit...").
			m.gitFocusCommitBox()
			return m, nil, true
		case 'x':
			m.openGitDiscardConfirm()
			return m, nil, true
		case 'r':
			return m, fetchGitCmd(), true
		case 't':
			m.toggleGitViewMode()
			return m, nil, true
		}
	}
	return m, nil, false
}

// gitStageToggle stages an unstaged change (or untracked file) and unstages
// any change that's only present in the index. Files that have *both* staged
// and unstaged changes get the unstaged ones promoted to the index — that
// matches VSCode's "Stage" affordance and is the more useful default.
type gitHunkKind int

const (
	gitHunkStage gitHunkKind = iota
	gitHunkUnstage
	gitHunkDiscard
)

// gitStageHunk / gitUnstageHunk / gitDiscardHunk apply git's per-hunk staging
// to the hunk under the cursor in the active editor buffer — the `git add -p`
// workflow, without dropping to the terminal.
func (m *Model) gitStageHunk() tea.Cmd   { return m.gitHunkOp(gitHunkStage) }
func (m *Model) gitUnstageHunk() tea.Cmd { return m.gitHunkOp(gitHunkUnstage) }
func (m *Model) gitDiscardHunk() tea.Cmd { return m.gitHunkOp(gitHunkDiscard) }

func (m *Model) gitHunkOp(kind gitHunkKind) tea.Cmd {
	path := m.editor.Path()
	if path == "" || m.cursorLine <= 0 {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	rel := path
	if r, e := filepath.Rel(cwd, path); e == nil && !strings.HasPrefix(r, "..") {
		rel = r
	}
	var opErr error
	var verb string
	switch kind {
	case gitHunkStage:
		opErr, verb = git.StageHunk(cwd, rel, m.cursorLine), "Staged hunk"
	case gitHunkUnstage:
		opErr, verb = git.UnstageHunk(cwd, rel, m.cursorLine), "Unstaged hunk"
	case gitHunkDiscard:
		opErr, verb = git.DiscardHunk(cwd, rel, m.cursorLine), "Discarded hunk"
	}
	var toastCmd tea.Cmd
	if opErr != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Git", opErr.Error())
		return toastCmd
	}
	// Discard rewrote the working file on disk — reload it (autoread) so the
	// buffer + signs reflect the revert. Stage/unstage only touch the index,
	// which the HEAD-relative editor signs don't show, so no reload is needed.
	if kind == gitHunkDiscard && m.nvim != nil {
		_ = m.nvim.Command("checktime")
	}
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, verb, filepath.Base(rel))
	return tea.Batch(fetchGitCmd(), toastCmd)
}

func (m *Model) gitStageToggle() tea.Cmd {
	fi, ok := m.gitCurrentFileIndex()
	if !ok {
		return nil
	}
	f := m.gitFiles[fi]
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var opErr error
	var verb string
	if f.Untracked() || f.Unstaged() {
		opErr = git.Stage(cwd, f.Path)
		verb = "Staged"
	} else {
		opErr = git.Unstage(cwd, f.Path)
		verb = "Unstaged"
	}
	var toastCmd tea.Cmd
	if opErr != nil {
		m.err = "git: " + opErr.Error()
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Git error", opErr.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, verb, f.Path)
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitDiffReadyMsg carries the HEAD version of a file (written to a temp file)
// so the main loop can set up the Neovim side-by-side diff.
type gitDiffReadyMsg struct {
	workPath     string // absolute path of the working file (right pane)
	headPath     string // temp file holding the HEAD version (left pane)
	name         string // display name for the left (HEAD) buffer
	hunkStarts   []int  // working-file start line of each diff hunk (for the X/N counter)
	changedLines []int  // every changed working-file line (for the overview ruler)
	totalLines   int    // working-file line count (overview scale denominator)
}

// diffHunkStarts returns the new-file (working) start line of every hunk in a
// unified diff — the "+c" of each "@@ -a,b +c,d @@" header.
func diffHunkStarts(diff string) []int {
	var starts []int
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "@@") {
			continue
		}
		plus := strings.IndexByte(line, '+')
		if plus < 0 {
			continue
		}
		n := 0
		for _, ch := range line[plus+1:] {
			if ch < '0' || ch > '9' {
				break
			}
			n = n*10 + int(ch-'0')
		}
		if n > 0 {
			starts = append(starts, n)
		}
	}
	return starts
}

// diffSignLines walks a unified diff and returns the new-file line numbers of
// added/changed ('+') lines and the old-file line numbers of removed ('-')
// lines — used to paint the change ruler in each pane's sign column. Computed
// in Go so it never depends on nvim having finished its async diff.
func diffSignLines(diff string) (add, del []int) {
	newLn, oldLn := 0, 0
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			if i := strings.IndexByte(line, '-'); i >= 0 {
				oldLn = leadingInt(line[i+1:])
			}
			if j := strings.IndexByte(line, '+'); j >= 0 {
				newLn = leadingInt(line[j+1:])
			}
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			// file headers — skip
		case strings.HasPrefix(line, "+"):
			add = append(add, newLn)
			newLn++
		case strings.HasPrefix(line, "-"):
			del = append(del, oldLn)
			oldLn++
		default:
			newLn++
			oldLn++
		}
	}
	return add, del
}

func leadingInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// gitDiffCmd opens the current file's changes as a Neovim-native side-by-side
// diff (VSCode style): the HEAD revision on the left, the working tree on the
// right, with nvim's own diff highlighting. The HEAD blob is fetched off the
// main loop and handed back via gitDiffReadyMsg.
func (m Model) gitDiffCmd() tea.Cmd {
	fi, ok := m.gitCurrentFileIndex()
	if !ok {
		return nil
	}
	rel := m.gitFiles[fi].Path
	abs := gitFileAbsPath(rel)
	name := filepath.Base(rel) + " (HEAD)"
	return func() tea.Msg {
		cwd, err := os.Getwd()
		if err != nil {
			return ErrMsg{Err: err}
		}
		head, _ := git.ShowFileAtRevision(cwd, "HEAD", rel)
		diff, _ := git.Diff(cwd, rel)
		changed, _ := diffSignLines(diff)
		total := 1
		if b, e := os.ReadFile(abs); e == nil {
			total = 1 + strings.Count(string(b), "\n")
		}
		tmp, err := os.CreateTemp("", "termocode-diff-*")
		if err != nil {
			return ErrMsg{Err: err}
		}
		_, _ = tmp.WriteString(head)
		_ = tmp.Close()
		return gitDiffReadyMsg{
			workPath: abs, headPath: tmp.Name(), name: name,
			hunkStarts: diffHunkStarts(diff), changedLines: changed, totalLines: total,
		}
	}
}

// openSideBySideDiff sets up the two-pane nvim diff for a gitDiffReadyMsg:
// the working file on the right, a read-only HEAD scratch buffer on the left,
// both in diff mode. Runs on the main loop (ExecLua is synchronous, so the
// temp file is safe to remove afterwards).
func (m *Model) openSideBySideDiff(msg gitDiffReadyMsg) {
	if m.nvim == nil {
		return
	}
	m.ensureEditorWindowCurrent()
	if err := m.nvim.ExecLua(buildDiffLua(msg.workPath, msg.headPath, msg.name, msg.hunkStarts, msg.changedLines, msg.totalLines)); err != nil {
		m.err = "diff: " + err.Error()
	}
	_ = os.Remove(msg.headPath)
	m.focus = FocusEditor
	// The pointer now drives synced scrolling between the two panes.
	m.gitDiffActive = true
	m.gitDiffHoverPane = -1
}

// buildDiffLua returns the Lua that opens the working file, splits a read-only
// HEAD scratch buffer to its left, and puts both into diff mode.
func buildDiffLua(work, head, name string, hunkStarts, changedLines []int, totalLines int) string {
	esc := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		return strings.ReplaceAll(s, `"`, `\"`)
	}
	luaArr := func(xs []int) string {
		ss := make([]string, len(xs))
		for i, x := range xs {
			ss[i] = strconv.Itoa(x)
		}
		return "{" + strings.Join(ss, ",") + "}"
	}
	startsLua := luaArr(hunkStarts)
	changedLua := luaArr(changedLines)
	// Values are concatenated (not %-formatted) so the Lua body can use literal
	// '%' freely — winbar highlight syntax (%#Grp#) and gsub patterns need it.
	return `local work, head, name = "` + esc(work) + `", "` + esc(head) + `", "` + esc(name) + `"
vim.g.tc_diff_changed = ` + changedLua + `
vim.g.tc_diff_total = ` + strconv.Itoa(totalLines) + `
-- X/N change counter: re-evaluated live in the working pane's winbar.
vim.g.tc_diff_starts = ` + startsLua + `
vim.api.nvim_set_hl(0, 'TcDiffCount', { fg = '#9aa3ad', bg = '#26292e', bold = true })
vim.api.nvim_set_hl(0, 'TcDiffOk',    { fg = '#73c991', bg = '#26292e' })
function _G.TcDiffCounter()
  local s = vim.g.tc_diff_starts or {}
  local n = #s
  if n == 0 then return '' end
  local l = vim.fn.line('.')
  local cur = 1
  for i, v in ipairs(s) do if l >= v then cur = i end end
  return cur .. '/' .. n
end
local base = (vim.fn.fnamemodify(work, ':t') or ''):gsub('%%', '%%%%')
-- Tiny dots (not dashes) fill the deleted-line gaps.
pcall(function() vim.opt.fillchars:append('diff:·') end)
-- Shared diff colours; per-pane overrides applied further down.
vim.api.nvim_set_hl(0, 'DiffAdd',    { bg = '#16361f' })
vim.api.nvim_set_hl(0, 'DiffChange', { bg = '#2a2a18' })
vim.api.nvim_set_hl(0, 'DiffText',   { bg = '#4a4a1f', bold = true })
vim.api.nvim_set_hl(0, 'DiffDelete', { bg = '#241317', fg = '#4a2630' })
-- Per-pane header bar: "<file> · HEAD" / "<file> · working".
vim.api.nvim_set_hl(0, 'WinBar',       { fg = '#cfd8e3', bg = '#26292e' })
vim.api.nvim_set_hl(0, 'WinBarNC',     { fg = '#9aa3ad', bg = '#26292e' })
vim.api.nvim_set_hl(0, 'TcDiffHdr',    { fg = '#e6edf3', bg = '#26292e', bold = true })
vim.api.nvim_set_hl(0, 'TcDiffHdrDim', { fg = '#7d868f', bg = '#26292e' })
vim.api.nvim_set_hl(0, 'TcDiffCount',  { fg = '#9aa3ad', bg = '#26292e', bold = true })
-- Close any prior diff panes so re-diffing replaces instead of stacking.
for _, w in ipairs(vim.api.nvim_list_wins()) do
  local b = vim.api.nvim_win_get_buf(w)
  local ok2, v = pcall(vim.api.nvim_buf_get_var, b, 'termocode_diff')
  if ok2 and v then pcall(vim.api.nvim_win_close, w, true) end
end
pcall(vim.cmd, 'diffoff!')
-- Working tree on the right.
vim.cmd('edit ' .. vim.fn.fnameescape(work))
local ft = vim.bo.filetype
local rwin = vim.api.nvim_get_current_win()
local wlines = vim.api.nvim_buf_get_lines(0, 0, -1, false)
vim.cmd('diffthis')
vim.wo.foldenable = false
vim.wo.number = true
vim.wo.winbar = '%#TcDiffCount#%{v:lua.TcDiffCounter()}  %#TcDiffHdr#' .. base .. '  %#TcDiffHdrDim#·  working'
-- HEAD revision on the left, read-only scratch. 'syntax' (not 'filetype')
-- highlights it without firing the FileType→LSP autocmd (which hangs).
vim.cmd('leftabove vnew')
local lwin = vim.api.nvim_get_current_win()
local buf = vim.api.nvim_get_current_buf()
vim.bo[buf].buftype = 'nofile'
vim.bo[buf].swapfile = false
vim.bo[buf].buflisted = false
vim.bo[buf].bufhidden = 'wipe'
vim.api.nvim_buf_set_var(buf, 'termocode_diff', true)
local ok, lines = pcall(vim.fn.readfile, head)
if ok then vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines) end
vim.bo[buf].modifiable = false
-- Highlight the HEAD pane with TREE-SITTER (incremental, no FileType→LSP
-- autocmd, no hang) — regex 'syntax' re-syncs from above on every scroll-up,
-- which made scrolling up far slower than scrolling down. Fall back to regex
-- syntax only when no parser is installed for the language.
if ft ~= '' then
  local lang = ft
  if vim.treesitter.language and vim.treesitter.language.get_lang then
    lang = vim.treesitter.language.get_lang(ft) or ft
  end
  if not pcall(vim.treesitter.start, buf, lang) then
    pcall(function() vim.bo[buf].syntax = ft end)
  end
end
-- Intentionally NOT named: a path-like buffer name ("<file> (HEAD)") can get
-- written to disk as a phantom file. The winbar below carries the label.
vim.cmd('diffthis')
vim.wo.foldenable = false
vim.wo.number = true
vim.wo.winbar = '%#TcDiffHdr# ' .. base .. '  %#TcDiffHdrDim#·  HEAD'
-- Per-side colours: red on the left (HEAD), green on the right (working).
local nsL = vim.api.nvim_create_namespace('tcDiffL')
vim.api.nvim_set_hl(nsL, 'DiffChange', { bg = '#3a1f28' })
vim.api.nvim_set_hl(nsL, 'DiffText',   { bg = '#5e2b3a', bold = true })
vim.api.nvim_set_hl(nsL, 'DiffAdd',    { bg = '#3a1f28' })
vim.api.nvim_set_hl(nsL, 'DiffDelete', { bg = '#241317', fg = '#4a2630' })
pcall(vim.api.nvim_win_set_hl_ns, lwin, nsL)
local nsR = vim.api.nvim_create_namespace('tcDiffR')
vim.api.nvim_set_hl(nsR, 'DiffAdd',    { bg = '#16361f' })
vim.api.nvim_set_hl(nsR, 'DiffChange', { bg = '#16361f' })
vim.api.nvim_set_hl(nsR, 'DiffText',   { bg = '#1f5e34', bold = true })
vim.api.nvim_set_hl(nsR, 'DiffDelete', { bg = '#241317', fg = '#4a2630' })
pcall(vim.api.nvim_win_set_hl_ns, rwin, nsR)
-- Sync and land on the working pane in normal mode.
vim.cmd('syncbind')
pcall(vim.api.nvim_set_current_win, rwin)
vim.cmd('stopinsert')
-- Wipe any stray empty unnamed buffer (e.g. the start-up [No Name]) that the
-- diff's :edit left behind as a phantom extra tab/window.
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buflisted
     and vim.api.nvim_buf_get_name(b) == '' and vim.fn.bufwinid(b) == -1 then
    local lc = vim.api.nvim_buf_line_count(b)
    local first = vim.api.nvim_buf_get_lines(b, 0, 1, false)[1] or ''
    if lc <= 1 and first == '' then
      pcall(vim.api.nvim_buf_delete, b, { force = true })
    end
  end
end
`
}

// openCommitPrompt asks for a commit message via the standard prompt overlay.
// Submission is handled in handlePromptSubmit (case promptKindCommit).
func (m *Model) openCommitPrompt() {
	if !m.gitIsRepo {
		return
	}
	// We surface staged-only as the line under the title so the user knows
	// what the commit will include. If nothing is staged, warn — git would
	// normally just refuse, and "Commit (0 staged)" is clearer.
	staged := 0
	for _, f := range m.gitFiles {
		if f.Staged() {
			staged++
		}
	}
	subtitle := fmt.Sprintf("Message (%d staged file%s):", staged, plural(staged))
	m.prompt = prompt.New("Commit", subtitle, "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindCommit
}

// openGitDiscardConfirm wires up the standard confirm dialog for the destructive
// "Discard Changes" action.
func (m *Model) openGitDiscardConfirm() {
	fi, ok := m.gitCurrentFileIndex()
	if !ok {
		return
	}
	f := m.gitFiles[fi]
	m.confirm = confirm.New(
		"Discard changes?",
		fmt.Sprintf("This will revert \"%s\" to its last committed state. This cannot be undone.", f.Path),
		[]confirm.Button{
			{ID: "discard", Title: "Discard", Style: confirm.StyleDestructive},
			{ID: "cancel", Title: "Cancel"},
		},
	)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindGitDiscard
	m.confirmTargetPath = f.Path
}

// colorizeDiff prefixes each unified-diff line with ANSI SGR colours so the
// preview overlay (which renders pre-styled content as-is) shows additions
// in green, removals in red, and hunk headers in cyan.
func colorizeDiff(s string) string {
	const (
		reset = "\x1b[0m"
		green = "\x1b[38;2;115;201;145m"
		red   = "\x1b[38;2;199;78;57m"
		cyan  = "\x1b[38;2;86;156;214m"
		dim   = "\x1b[38;2;138;138;138m"
	)
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			b.WriteString(dim)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index "):
			b.WriteString(dim)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(line, "@@"):
			b.WriteString(cyan)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(line, "+"):
			b.WriteString(green)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(line, "-"):
			b.WriteString(red)
			b.WriteString(line)
			b.WriteString(reset)
		default:
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// gitDiscardConfirmed runs the actual discard once the user confirms. Called
// from handleConfirmSelect (confirm.go) for the confirmKindGitDiscard branch.
func (m *Model) gitDiscardConfirmed() tea.Cmd {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	path := m.confirmTargetPath
	var toastCmd tea.Cmd
	if err := git.Discard(cwd, path); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Discard failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Discarded changes", path)
	}
	// Buffer that file may now be stale on disk — nudge nvim to checktime so
	// the editor picks up the reverted contents (autoread is on).
	if m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitCommitFromPrompt runs `git commit -m <msg>` after a successful prompt
// submission. Called from handlePromptSubmit (file_ops.go) for the
// promptKindCommit branch.
func (m *Model) gitCommitFromPrompt(message string) tea.Cmd {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Commit(cwd, message); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Commit failed", err.Error())
	} else {
		// Trim the message for the toast: keep first line, cap at 50 chars.
		first := message
		if i := strings.Index(first, "\n"); i >= 0 {
			first = first[:i]
		}
		if len(first) > 50 {
			first = first[:50] + "…"
		}
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Committed", first)
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitStageActiveFile stages the file currently shown in the editor. Used
// from the command palette so the user doesn't have to navigate the git
// sidebar for the common single-file case.
func (m *Model) gitStageActiveFile() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	rel, ok := m.activeGitRel()
	if !ok {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Stage(cwd, rel); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Stage failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Staged", rel)
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitStageAll runs `git add -A`.
func (m *Model) gitStageAll() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Stage(cwd, "."); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Stage all failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Staged all changes")
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitDiffActiveFile opens the diff for the currently active editor buffer.
func (m *Model) gitDiffActiveFile() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	rel, ok := m.activeGitRel()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		cwd, err := os.Getwd()
		if err != nil {
			return ErrMsg{Err: err}
		}
		raw, err := git.Diff(cwd, rel)
		if err != nil {
			return ErrMsg{Err: err}
		}
		body := colorizeDiff(raw)
		return PreviewMsg{Title: "diff · " + rel, Body: body}
	}
}

// openGitDiscardForActiveFile opens the discard-confirm dialog for whichever
// file is currently in the editor. Same UX as 'x' on the git sidebar but
// reachable from the palette.
func (m *Model) openGitDiscardForActiveFile() {
	rel, ok := m.activeGitRel()
	if !ok {
		return
	}
	m.confirm = confirm.New(
		"Discard changes?",
		fmt.Sprintf("This will revert \"%s\" to its last committed state. This cannot be undone.", rel),
		[]confirm.Button{
			{ID: "discard", Title: "Discard", Style: confirm.StyleDestructive},
			{ID: "cancel", Title: "Cancel"},
		},
	)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindGitDiscard
	m.confirmTargetPath = rel
}

// gitPush runs `git push`. If the current branch has no upstream we add the
// `-u origin <branch>` form so the user doesn't have to drop to the
// integrated terminal for the first push.
func (m *Model) gitPush() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var opErr error
	if git.HasUpstream(cwd) {
		opErr = git.Push(cwd)
	} else if m.gitBranch.Name != "" {
		opErr = git.PushSetUpstream(cwd, m.gitBranch.Name)
	} else {
		opErr = fmt.Errorf("no current branch")
	}
	var toastCmd tea.Cmd
	if opErr != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Push failed", opErr.Error())
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Pushed to origin")
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitPull runs `git pull`.
func (m *Model) gitPull() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Pull(cwd); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Pull failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Pulled from origin")
		// Buffers may be stale on disk after a pull — nudge nvim to reload.
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitFetch runs `git fetch --prune` — updates remote-tracking refs (so the
// ahead/behind counters refresh) without touching the working tree.
func (m *Model) gitFetch() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Fetch(cwd); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Fetch failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Fetched from origin")
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitSync mirrors VSCode's "Sync Changes": pull, then push. With no upstream
// configured it falls back to a first push (-u origin <branch>).
func (m *Model) gitSync() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	if !git.HasUpstream(cwd) {
		return m.gitPush()
	}
	var toastCmd tea.Cmd
	if err := git.Pull(cwd); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Sync failed (pull)", err.Error())
		return tea.Batch(fetchGitCmd(), toastCmd)
	}
	if err := git.Push(cwd); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Sync failed (push)", err.Error())
		return tea.Batch(fetchGitCmd(), toastCmd)
	}
	if m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	m.toast, toastCmd = m.toast.Push(toast.Info, "Synced with origin")
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitInlineCommit commits the always-visible message box. amend rewords HEAD;
// signoff adds the Signed-off-by trailer. When nothing is staged it warns (or,
// if there are unstaged changes, offers to stage-all-and-commit) rather than
// letting git fail. On success the box is cleared and blurred.
func (m *Model) gitInlineCommit(amend, signoff bool) tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	msg := strings.TrimSpace(m.gitCommitMsg)
	if msg == "" && !amend {
		// Nothing typed yet — focus the field instead of erroring out.
		m.gitFocusCommitBox()
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Warn, "Enter a commit message")
		return c
	}
	staged, changed := m.gitFileCounts()
	if staged == 0 && !amend {
		if changed == 0 {
			var c tea.Cmd
			m.toast, c = m.toast.Push(toast.Info, "Nothing to commit")
			return c
		}
		m.openStageAllCommitConfirm()
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.CommitWith(cwd, msg, amend, signoff); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Commit failed", err.Error())
		return tea.Batch(fetchGitCmd(), toastCmd)
	}
	m.clearCommitBox()
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Committed", commitToastLabel(msg, amend))
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitStageAllAndCommit is the confirmed branch of the "nothing staged" prompt:
// stage every change, then commit the stored message.
func (m *Model) gitStageAllAndCommit() tea.Cmd {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	if err := git.Stage(cwd, "."); err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Errr, "Stage all failed", err.Error())
		return c
	}
	msg := strings.TrimSpace(m.gitCommitMsg)
	var toastCmd tea.Cmd
	if err := git.Commit(cwd, msg); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Commit failed", err.Error())
		return tea.Batch(fetchGitCmd(), toastCmd)
	}
	m.clearCommitBox()
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Committed", commitToastLabel(msg, false))
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// clearCommitBox resets the inline message field after a successful commit.
func (m *Model) clearCommitBox() {
	m.gitCommitMsg = ""
	m.gitCommitCaret = 0
	m.gitCommitFocused = false
}

// openStageAllCommitConfirm asks before staging every change for a commit when
// the index is empty (VSCode's "no staged changes" prompt).
func (m *Model) openStageAllCommitConfirm() {
	m.confirm = confirm.New(
		"Stage all changes?",
		"There are no staged changes. Stage all changes and commit?",
		[]confirm.Button{
			{ID: "stage", Title: "Stage All & Commit", Style: confirm.StylePrimary},
			{ID: "cancel", Title: "Cancel"},
		},
	)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindGitStageCommit
}

// commitToastLabel trims a commit message to a single ≤50-char line for toasts.
func commitToastLabel(msg string, amend bool) string {
	first := msg
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	if first == "" && amend {
		return "amended HEAD"
	}
	if len(first) > 50 {
		first = first[:50] + "…"
	}
	if amend {
		return "amended: " + first
	}
	return first
}

// openBranchPicker shows a fuzzy picker of local branches; selecting one
// runs `git checkout <name>`. The first item is always "Create new branch
// …" (id "new") so users can stay in the picker flow for the new-branch
// case too.
func (m *Model) openBranchPicker() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	branches, err := git.Branches(cwd)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Branches failed", err.Error())
		return toastCmd
	}
	items := make([]picker.Item, 0, len(branches)+1)
	items = append(items, picker.Item{ID: "new", Title: "+  Create new branch…", Hint: ""})
	for _, b := range branches {
		hint := ""
		if b == m.gitBranch.Name {
			hint = "current"
		}
		items = append(items, picker.Item{ID: "co-" + b, Title: b, Hint: hint})
	}
	m.picker = picker.NewItems(" Switch Branch ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindBranches
	return nil
}

// gitCheckoutSelected runs `git checkout <branch>` for the picked entry.
// Called from handlePickerSelect (case pickerKindBranches).
func (m *Model) gitCheckoutSelected(id string) tea.Cmd {
	if id == "new" {
		// Open a prompt for the new branch name; commit on submit goes
		// through handlePromptSubmit.
		m.prompt = prompt.New("New Branch", "Branch name:", "")
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindGitNewBranch
		return nil
	}
	branch := id[len("co-"):]
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Checkout(cwd, branch); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Checkout failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Switched to", branch)
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitCreateBranchFromPrompt runs `git checkout -b <name>` after a successful
// prompt submission for promptKindGitNewBranch.
func (m *Model) gitCreateBranchFromPrompt(name string) tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.CreateBranch(cwd, name); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Branch failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Created branch", name)
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitStash runs `git stash push` to save uncommitted changes. We don't
// prompt for a message — the timestamped default git produces is fine for
// the day-to-day "I need to switch context briefly" use case. Users who
// want a labelled stash can run `git stash push -m "label"` from the
// integrated terminal.
func (m *Model) gitStash() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Stash(cwd, ""); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Warn, "Stash failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Stashed changes")
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitStashPop runs `git stash pop` (applies stash@{0} and removes it).
func (m *Model) gitStashPop() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.StashPop(cwd); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Stash pop failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Popped latest stash")
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// openStashPicker shows a list of all stashes; selection applies (without
// dropping). Drop has to go through the terminal for safety — accidental
// drops are unrecoverable.
func (m *Model) openStashPicker() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	entries, err := git.StashList(cwd)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Stash list failed", err.Error())
		return toastCmd
	}
	if len(entries) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No stashes")
		return toastCmd
	}
	items := make([]picker.Item, 0, len(entries))
	for _, e := range entries {
		items = append(items, picker.Item{
			ID:    e.Ref,
			Title: e.Ref + "   " + e.Message,
			Hint:  "apply",
		})
	}
	m.picker = picker.NewItems(" Stash List (Enter applies) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindStash
	return nil
}

// applyStashSelected runs `git stash apply <ref>` for the picked stash.
func (m *Model) applyStashSelected(ref string) tea.Cmd {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.StashApply(cwd, ref); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Stash apply failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Applied", ref)
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitBlameCurrentLine fetches blame info for the cursor's line and shows
// it as a toast — quick "who wrote this" without taking the user out of
// flow. (The full file-history picker is gitFileHistoryPicker, below.)
func (m *Model) gitBlameCurrentLine() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	path := m.editor.Path()
	if path == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No file to blame")
		return toastCmd
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	line := m.cursorLine
	if line <= 0 {
		line = 1
	}
	bl, err := git.BlameLineAt(cwd, path, line)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Blame failed", err.Error())
		return toastCmd
	}
	msg := fmt.Sprintf("%s · %s · %s · %s", bl.Hash, bl.Author, bl.When, bl.Subject)
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, msg)
	return toastCmd
}

// gitFileHistoryPicker opens a picker showing every commit that touched
// the active file. Selecting a commit shows its full diff, same as the
// general "Show Log" picker.
func (m *Model) gitFileHistoryPicker() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	path := m.editor.Path()
	if path == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No file selected")
		return toastCmd
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	entries, err := git.FileHistory(cwd, path, 200)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "History failed", err.Error())
		return toastCmd
	}
	if len(entries) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No history (file untracked or new)")
		return toastCmd
	}
	items := make([]picker.Item, 0, len(entries))
	for _, e := range entries {
		title := e.Hash + "  " + e.Subject
		hint := e.Author + ", " + e.When
		items = append(items, picker.Item{
			ID:    "commit-" + e.Hash,
			Title: title,
			Hint:  hint,
		})
	}
	rel := path
	if r, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(r, "..") {
		rel = r
	}
	m.picker = picker.NewItems(" History · "+rel+" ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindGitLog // reuse: same Enter-shows-diff flow
	return nil
}

// gitTagPicker shows every tag; selection puts the tag name on the
// clipboard as a small "did something" affordance.
func (m *Model) gitTagPicker() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	tags, err := git.Tags(cwd)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Tags failed", err.Error())
		return toastCmd
	}
	if len(tags) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No tags in this repo")
		return toastCmd
	}
	items := make([]picker.Item, 0, len(tags))
	for _, t := range tags {
		items = append(items, picker.Item{ID: "tag-" + t, Title: t})
	}
	m.picker = picker.NewItems(" Tags (Enter checks out) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindTags
	return nil
}

// checkoutTagSelected does `git checkout <tag>` (detached HEAD).
func (m *Model) checkoutTagSelected(id string) tea.Cmd {
	tag := strings.TrimPrefix(id, "tag-")
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Checkout(cwd, tag); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Checkout failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Checked out tag (detached HEAD)", tag)
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitLogPicker shows the recent commit history. Selecting a commit opens
// its full diff in the preview overlay.
func (m *Model) gitLogPicker() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	entries, err := git.Log(cwd, 200)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Log failed", err.Error())
		return toastCmd
	}
	if len(entries) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No commits yet")
		return toastCmd
	}
	items := make([]picker.Item, 0, len(entries))
	for _, e := range entries {
		title := e.Hash + "  " + e.Subject
		if e.Refs != "" {
			title = title + "   " + e.Refs
		}
		hint := e.Author + ", " + e.When
		items = append(items, picker.Item{
			ID:    "commit-" + e.Hash,
			Title: title,
			Hint:  hint,
		})
	}
	m.picker = picker.NewItems(" Git Log (Enter shows diff) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindGitLog
	return nil
}

// showCommitFromPicker handles the "user picked a commit from the log"
// case: load the commit's diff via `git show` and stuff it into the
// preview overlay.
func (m *Model) showCommitFromPicker(id string) tea.Cmd {
	return m.gitShowCommitCmd(strings.TrimPrefix(id, "commit-"))
}

// gitCommitDiffReadyMsg carries a commit's full diff (written to a temp file)
// so the main loop can open it in a Neovim diff buffer.
type gitCommitDiffReadyMsg struct {
	path string // temp file holding `git show <hash>`
	name string // display name for the buffer
}

// gitShowCommitCmd loads a commit's full diff (`git show <hash>`) and opens it,
// GitLab-style, as a single scrollable Neovim buffer (filetype=diff) with every
// changed file stacked and syntax-highlighted. Shared by the log picker and the
// Source Control graph's click-to-diff.
func (m *Model) gitShowCommitCmd(hash string) tea.Cmd {
	if hash == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		body, err := git.ShowCommit(cwd, hash)
		if err != nil {
			return ErrMsg{Err: err}
		}
		tmp, err := os.CreateTemp("", "termocode-commit-*.diff")
		if err != nil {
			return ErrMsg{Err: err}
		}
		_, _ = tmp.WriteString(body)
		_ = tmp.Close()
		return gitCommitDiffReadyMsg{path: tmp.Name(), name: hash + " · commit"}
	}
}

// openCommitDiffBuffer renders a commit's diff into a read-only, syntax-
// highlighted Neovim buffer in the editor window (GitLab-style single page).
func (m *Model) openCommitDiffBuffer(msg gitCommitDiffReadyMsg) {
	if m.nvim == nil {
		return
	}
	m.ensureEditorWindowCurrent()
	if err := m.nvim.ExecLua(buildCommitDiffLua(msg.path, msg.name)); err != nil {
		m.err = "diff: " + err.Error()
	}
	_ = os.Remove(msg.path)
	m.focus = FocusEditor
	m.gitDiffActive = false // single scrollable buffer — no pane sync needed
}

// buildCommitDiffLua loads the diff temp file into a read-only nofile buffer in
// the current (editor) window, sets filetype=diff for highlighting, and tunes
// the diff syntax colours so adds/removes/headers stand out.
func buildCommitDiffLua(path, name string) string {
	esc := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		return strings.ReplaceAll(s, `"`, `\"`)
	}
	return fmt.Sprintf(`
local path, name = "%s", "%s"
vim.api.nvim_set_hl(0, 'diffAdded',   { fg = '#73c991' })
vim.api.nvim_set_hl(0, 'diffRemoved', { fg = '#e2756a' })
vim.api.nvim_set_hl(0, 'diffLine',    { fg = '#569cd6' })
vim.api.nvim_set_hl(0, 'diffFile',    { fg = '#dcdcaa', bold = true })
vim.api.nvim_set_hl(0, 'diffIndexLine', { fg = '#808080' })
-- Close any prior diff panes (side-by-side or commit) before reusing the window.
for _, w in ipairs(vim.api.nvim_list_wins()) do
  local b = vim.api.nvim_win_get_buf(w)
  local ok2, v = pcall(vim.api.nvim_buf_get_var, b, 'termocode_diff')
  if ok2 and v then pcall(vim.api.nvim_win_close, w, true) end
end
pcall(vim.cmd, 'diffoff!')
vim.cmd('enew')
local buf = vim.api.nvim_get_current_buf()
vim.bo[buf].buftype = 'nofile'
vim.bo[buf].swapfile = false
vim.bo[buf].buflisted = false
vim.bo[buf].bufhidden = 'wipe'
vim.api.nvim_buf_set_var(buf, 'termocode_diff', true)
local ok, lines = pcall(vim.fn.readfile, path)
if ok then vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines) end
vim.bo[buf].modifiable = false
vim.bo[buf].filetype = 'diff'
-- Label via the winbar, NOT a buffer name: a path-like name gets written to
-- disk as a phantom file.
vim.api.nvim_set_hl(0, 'WinBar',    { fg = '#cfd8e3', bg = '#26292e' })
vim.api.nvim_set_hl(0, 'TcDiffHdr', { fg = '#e6edf3', bg = '#26292e', bold = true })
vim.wo.winbar = '%%#TcDiffHdr# ' .. name
pcall(vim.api.nvim_win_set_cursor, 0, { 1, 0 })
vim.cmd('stopinsert')
`, esc(path), esc(name))
}

// openCompareWithRevisionPrompt asks the user for a revision/branch to
// diff against (e.g. "main", "HEAD~3", "origin/main"). The submission goes
// through promptKindCompareRevision in handlePromptSubmit.
func (m *Model) openCompareWithRevisionPrompt() {
	m.prompt = prompt.New("Compare with Revision", "Revision (branch / HEAD~N / SHA):", "HEAD")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindCompareRevision
}

// runCompareWithRevision fetches `git diff <ref>` and renders it.
func (m *Model) runCompareWithRevision(ref string) tea.Cmd {
	if ref == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		body, err := git.DiffAgainst(cwd, ref)
		if err != nil {
			return ErrMsg{Err: err}
		}
		if strings.TrimSpace(body) == "" {
			body = "(no differences)"
		}
		return PreviewMsg{Title: "diff · vs " + ref, Body: colorizeDiff(body)}
	}
}

// activeGitRel returns the active editor file's path expressed as a
// repo-relative path (suitable for git CLI args). Returns ("", false) when
// there's no active file or it's outside the working directory.
func (m Model) activeGitRel() (string, bool) {
	abs := m.editor.Path()
	if abs == "" {
		return "", false
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	rel := abs
	if strings.HasPrefix(abs, cwd+"/") {
		rel = abs[len(cwd)+1:]
	} else if abs == cwd {
		return "", false
	}
	return rel, true
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
