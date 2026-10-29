package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/mergeview"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The three-way merge view (G-MRG): base, ours and theirs of one
// conflicted file, read-only, in rows right above the editor; the editor
// below holds the real file, which is the result. Each Take changes the
// result in one step, so one undo takes it back. Opened from a CONFLICTS
// row or "Git: Open Merge View".

// mergeNarrowW is the window width under which the panes show one at a
// time (1 · 2 · 3 switch them).
const mergeNarrowW = 120

// mergeViewState is the open merge view.
type mergeViewState struct {
	open bool
	view mergeview.Model
	rel  string // path from the repo root ("internal/x.go")
	abs  string // the result's path, as the editor has it
	// whole is the binary / deleted conflict the keep-or-delete card is about.
	whole *git.ConflictStages
}

// mergeViewShown reports whether the merge view is on screen.
func (m Model) mergeViewShown() bool { return m.merge != nil && m.merge.open }

// mergeRows is how many rows the merge view takes from an editor content
// area contentH rows tall: a bit under half, leaving the result at least
// 4 rows. Zen mode and tiny windows show no merge view.
func (m Model) mergeRows(contentH int) int {
	if !m.mergeViewShown() || m.zenMode || contentH < 10 {
		return 0
	}
	return min(max(contentH*45/100, 5), contentH-4)
}

// mergeNarrow reports whether the panes show one at a time.
func (m Model) mergeNarrow(paneW int) bool { return m.w < mergeNarrowW || paneW < 90 }

// renderMergeRows draws the merge view h rows tall and w wide.
func (m Model) renderMergeRows(w, h int) []string {
	v := m.merge.view
	v.SetSize(w, h, m.mergeNarrow(w))
	v.SetFocused(m.mergeFocused())
	return strings.Split(v.View(), "\n")
}

// mergeEditor reports whether the editor is up (the result lives there).
func (m Model) mergeEditor() bool { return m.nvim != nil && m.nvimAttached }

// mergeFocused reports whether the merge view has the keys: it was given
// them and the editor column has the focus.
func (m Model) mergeFocused() bool {
	return m.mergeViewShown() && m.merge.view.Focused() && m.focus == FocusEditor
}

// mergeStagesMsg carries a conflicted file's index stages.
type mergeStagesMsg struct {
	rel string
	st  git.ConflictStages
	err error
}

// mergeResolvedMsg is the end of Mark as Resolved.
type mergeResolvedMsg struct {
	rel string
	err error
}

// mergeWholeDoneMsg is the end of keep ours / keep theirs / delete.
type mergeWholeDoneMsg struct {
	rel, verb string
	err       error
}

// mergeSyncMsg asks to read the result again (it changed or the cursor
// moved; debounced).
type mergeSyncMsg struct{}

func init() {
	registerNotifyHandler("termocode_merge_sync", func(m *Model, _ []any) tea.Cmd {
		if !m.mergeViewShown() {
			return nil
		}
		return m.debounce.Do("merge-sync", 60*time.Millisecond, func() tea.Msg { return mergeSyncMsg{} })
	})
}

// onMergeMsg handles the merge view's messages.
func (m *Model) onMergeMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case mergeStagesMsg:
		return m.onMergeStages(msg)
	case mergeSyncMsg:
		m.mergeSync()
	case mergeResolvedMsg:
		return m.onMergeResolved(msg)
	case mergeWholeDoneMsg:
		return m.onMergeWholeDone(msg)
	}
	return nil
}

// gitOpenMergeView reads rel's stages off the loop; the view opens when
// they come back (onMergeStages).
func (m *Model) gitOpenMergeView(rel string) tea.Cmd {
	cwd, err := m.rootDir()
	if err != nil || rel == "" {
		return nil
	}
	return func() tea.Msg {
		st, err := git.ReadConflictStages(cwd, rel)
		return mergeStagesMsg{rel: rel, st: st, err: err}
	}
}

// gitOpenMergeViewForFile opens the merge view for a CONFLICTS row.
func (m *Model) gitOpenMergeViewForFile(fileIndex int) tea.Cmd {
	if fileIndex < 0 || fileIndex >= len(m.gitFiles) {
		return nil
	}
	return m.gitOpenMergeView(m.gitFiles[fileIndex].Path)
}

// mergePaletteTarget is the file "Git: Open Merge View" opens: the file in
// the editor when it has a conflict, else the CONFLICTS row under the
// cursor, else the first conflicted file.
func (m *Model) mergePaletteTarget() string {
	var conflicted []string
	for _, f := range m.gitFiles {
		if f.Conflicted() {
			conflicted = append(conflicted, f.Path)
		}
	}
	if root, err := m.rootDir(); err == nil && m.editor.Path() != "" {
		if rel, err := filepath.Rel(root, m.editor.Path()); err == nil && slices.Contains(conflicted, filepath.ToSlash(rel)) {
			return filepath.ToSlash(rel)
		}
	}
	if fi, ok := m.gitCurrentFileIndex(); ok && m.gitFiles[fi].Conflicted() {
		return m.gitFiles[fi].Path
	}
	if len(conflicted) > 0 {
		return conflicted[0]
	}
	return ""
}

// onMergeStages opens the view (a text conflict) or the keep-or-delete
// card (binary, or deleted by one side).
func (m *Model) onMergeStages(msg mergeStagesMsg) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case errors.Is(msg.err, git.ErrNotConflicted):
		m.toast, cmd = m.toast.Push(toast.Info, "No merge conflict in "+msg.rel)
		return cmd
	case msg.err != nil:
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Can't open the merge view", msg.err.Error())
		return cmd
	}
	if !msg.st.TextMerge() {
		m.openMergeWholeCard(msg.st)
		return nil
	}
	root, err := m.rootDir()
	if err != nil {
		return nil
	}
	m.closeCommitView()
	m.closeMergeView()
	v := mergeview.New(msg.rel, msg.st)
	v.SetFocused(true)
	m.merge = &mergeViewState{open: true, view: v, rel: msg.rel, abs: filepath.Join(root, filepath.FromSlash(msg.rel))}
	m.mergeShowResult()
	m.mergeSync()
	m.focus = FocusEditor
	m.applyLayout()
	return nil
}

// closeMergeView hides the view and drops its editor hooks.
func (m *Model) closeMergeView() {
	if !m.mergeViewShown() {
		return
	}
	m.merge.open = false
	if m.mergeEditor() {
		_ = m.nvim.ExecLua(`pcall(vim.api.nvim_del_augroup_by_name, 'TermocodeMerge')`)
	}
	m.applyLayout()
}

// mergeResultLua opens the result and finds its buffer later; edits and
// cursor moves in it sync the view.
const mergeResultLua = `
_G.termocode_merge_buf = function(p)
  local full = vim.fn.fnamemodify(p, ':p')
  for _, b in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_loaded(b) and vim.api.nvim_buf_get_name(b) == full then return b end
  end
  return -1
end
local p = ...
vim.cmd('edit ' .. vim.fn.fnameescape(p))
local buf = vim.api.nvim_get_current_buf()
local g = vim.api.nvim_create_augroup('TermocodeMerge', { clear = true })
vim.api.nvim_create_autocmd({ 'TextChanged', 'TextChangedI', 'CursorMoved', 'CursorMovedI' }, {
  group = g, buffer = buf,
  callback = function() _G.termocode_notify('termocode_merge_sync') end,
})
vim.api.nvim_win_set_cursor(0, { 1, 0 })
vim.fn.search([[^<<<<<<<\%( \|\t\|$\)]], 'cW')
pcall(_G.termocode_conflict_paint)
`

// mergeShowResult opens the result in the editor with the view's hooks.
func (m *Model) mergeShowResult() {
	if !m.mergeEditor() {
		return
	}
	m.ensureEditorWindowCurrent()
	if err := m.nvim.ExecLuaArgs(mergeResultLua, m.merge.abs); err != nil {
		recordError("[merge] open result: " + err.Error())
	}
}

// mergeResult is the result buffer as nvim has it.
type mergeResult struct {
	Lines []string `msgpack:"lines"`
	Cur   int      `msgpack:"cur"` // 0-based cursor line, -1: not the current buffer
	Gone  bool     `msgpack:"gone"`
}

// mergeReadResult reads the result buffer (ok=false without an editor).
func (m *Model) mergeReadResult() (mergeResult, bool) {
	if !m.mergeEditor() || !m.mergeViewShown() {
		return mergeResult{}, false
	}
	var r mergeResult
	err := m.nvim.ExecLuaResult(`
		local p = ...
		local b = _G.termocode_merge_buf and _G.termocode_merge_buf(p) or -1
		if b < 0 then return { gone = true, cur = -1, lines = {} } end
		local cur = -1
		if vim.api.nvim_get_current_buf() == b then cur = vim.api.nvim_win_get_cursor(0)[1] - 1 end
		return { lines = vim.api.nvim_buf_get_lines(b, 0, -1, false), cur = cur, gone = false }
	`, &r, m.merge.abs)
	if err != nil {
		recordError("[merge] read result: " + err.Error())
		return mergeResult{}, false
	}
	return r, true
}

// mergeSync gives the view the result's lines and the cursor's block.
// A closed result buffer closes the view.
func (m *Model) mergeSync() {
	r, ok := m.mergeReadResult()
	if !ok {
		return
	}
	if r.Gone {
		m.closeMergeView()
		return
	}
	m.merge.view.SetResult(r.Lines)
	if r.Cur >= 0 && !m.mergeFocused() {
		m.merge.view.SetCursorLine(r.Cur)
	}
}

// mergeFocusPanes gives the view the keys.
func (m *Model) mergeFocusPanes() {
	if !m.mergeViewShown() {
		return
	}
	m.merge.view.SetFocused(true)
	m.focus = FocusEditor
}

// mergeFocusResult gives the editor the keys, the cursor on the current
// block.
func (m *Model) mergeFocusResult() {
	if !m.mergeViewShown() {
		return
	}
	m.merge.view.SetFocused(false)
	m.focus = FocusEditor
	b, ok := m.merge.view.CurrentBlock()
	if !m.mergeEditor() || !ok {
		return
	}
	_ = m.nvim.ExecLuaArgs(`
		local p, line = ...
		local b = _G.termocode_merge_buf and _G.termocode_merge_buf(p) or -1
		if b < 0 then return end
		local w = vim.fn.win_findbuf(b)[1]
		if not w then return end
		vim.api.nvim_set_current_win(w)
		pcall(vim.api.nvim_win_set_cursor, w, { line + 1, 0 })
	`, m.merge.abs, b.Conflict.Start)
}

// routeToMergeView gives the view its keys (while it has the focus) and
// the clicks and wheel on its rows. ok=false sends msg on.
func (m Model) routeToMergeView(msg tea.Msg) (Model, tea.Cmd, bool) {
	pane := m.editorPaneLayout()
	switch msg := msg.(type) {
	case tea.MouseMsg:
		top := pane.Top - pane.Merge
		inside := pane.Merge > 0 && msg.X >= pane.X && msg.X < pane.X+pane.W && msg.Y >= top && msg.Y < pane.Top
		if !inside {
			if msg.Action == tea.MouseActionPress && m.mouseProbe == nil {
				m.merge.view.SetFocused(false)
			}
			return m, nil, false
		}
		if m.mouseProbe != nil {
			m.probed("merge")
			return m, nil, true
		}
		switch {
		case msg.Button == tea.MouseButtonWheelUp:
			m.merge.view.Scroll(-3)
		case msg.Button == tea.MouseButtonWheelDown:
			m.merge.view.Scroll(3)
		case msg.Action == tea.MouseActionPress:
			m.mergeFocusPanes()
			v := m.merge.view
			v.SetSize(pane.W, pane.Merge, m.mergeNarrow(pane.W))
			v.Click(msg.X-pane.X, msg.Y-top)
			m.merge.view = v
		}
		return m, nil, true
	case tea.KeyMsg:
		// F6 (focus swap) stops at the view between the editor and
		// the sidebar.
		if m.keys.Match(msg) == keymap.ActionFocusSwap && m.focus == FocusEditor && !m.inTerminal {
			if m.merge.view.Focused() {
				m.merge.view.SetFocused(false)
				return m, nil, false
			}
			m.mergeFocusPanes()
			return m, nil, true
		}
		if !m.mergeFocused() {
			return m, nil, false
		}
		return m.mergeKey(msg)
	}
	return m, nil, false
}

// mergeKey runs one merge key. Keys the view does not know go on, so
// global shortcuts (the palette, save) still work.
func (m Model) mergeKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	v := &m.merge.view
	switch msg.String() {
	case "esc":
		// Esc goes on too: it leaves Insert mode in the editor.
		m.closeMergeView()
		return m, nil, false
	case "q":
		m.closeMergeView()
	case "tab", "e":
		m.mergeFocusResult()
	case "n", "]", "down":
		v.Next()
	case "p", "[", "up":
		v.Prev()
	case "j":
		v.Scroll(1)
	case "k":
		v.Scroll(-1)
	case "o":
		return m, m.mergeTake(git.AcceptOurs), true
	case "t":
		return m, m.mergeTake(git.AcceptTheirs), true
	case "b":
		return m, m.mergeTake(git.AcceptBoth), true
	case "B":
		return m, m.mergeTake(git.AcceptBothTheirsFirst), true
	case "a":
		return m, m.mergeTake(git.AcceptBase), true
	case "u":
		return m, m.mergeUndo(), true
	case "r":
		return m, m.mergeMarkResolved(), true
	case "1", "2", "3":
		v.SetPane(mergeview.Pane(msg.Runes[0] - '1'))
	case "v":
		v.CyclePane()
	default:
		return m, nil, false
	}
	return m, nil, true
}

// dispatchMergePalette handles the merge view's palette entries.
func (m *Model) dispatchMergePalette(id string) (tea.Cmd, bool) {
	if id == "git-merge-open" {
		rel := m.mergePaletteTarget()
		if rel == "" {
			var cmd tea.Cmd
			m.toast, cmd = m.toast.Push(toast.Info, "No merge conflicts")
			return cmd, true
		}
		return m.gitOpenMergeView(rel), true
	}
	if !strings.HasPrefix(id, "git-merge-") {
		return nil, false
	}
	if !m.mergeViewShown() {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Info, "The merge view is not open", "Open it first: Git: Open Merge View")
		return cmd, true
	}
	v := &m.merge.view
	switch id {
	case "git-merge-next":
		v.Next()
	case "git-merge-prev":
		v.Prev()
	case "git-merge-pane":
		v.CyclePane()
	case "git-merge-focus":
		m.mergeFocusPanes()
	case "git-merge-close":
		m.closeMergeView()
	case "git-merge-take-ours":
		return m.mergeTake(git.AcceptOurs), true
	case "git-merge-take-theirs":
		return m.mergeTake(git.AcceptTheirs), true
	case "git-merge-take-both":
		return m.mergeTake(git.AcceptBoth), true
	case "git-merge-take-both-theirs-first":
		return m.mergeTake(git.AcceptBothTheirsFirst), true
	case "git-merge-take-base":
		return m.mergeTake(git.AcceptBase), true
	case "git-merge-undo":
		return m.mergeUndo(), true
	case "git-merge-resolve":
		return m.mergeMarkResolved(), true
	default:
		return nil, false
	}
	return nil, true
}

// mergeTakeLua replaces lines [s, e) of the result with lines as one
// undo step (the undo sequence is broken first, so two Takes never merge
// into one undo), and puts the cursor there.
const mergeTakeLua = `
local p, s, e, lines = ...
local b = _G.termocode_merge_buf and _G.termocode_merge_buf(p) or -1
if b < 0 then return 'gone' end
vim.api.nvim_buf_call(b, function() vim.cmd('let &undolevels = &undolevels') end)
vim.api.nvim_buf_set_lines(b, s, e, false, lines)
local n = vim.api.nvim_buf_line_count(b)
for _, w in ipairs(vim.fn.win_findbuf(b)) do
  pcall(vim.api.nvim_win_set_cursor, w, { math.min(s + 1, n), 0 })
end
pcall(_G.termocode_conflict_paint, b)
return 'ok'
`

// mergeTake resolves the current block in the result. The result is
// read again first: the user may have edited it since the last sync.
func (m *Model) mergeTake(r git.Resolution) tea.Cmd {
	if !m.mergeViewShown() || !m.mergeEditor() {
		return nil
	}
	m.mergeSync()
	tk, ok := m.merge.view.Take(r)
	if !ok {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Info, "No conflicts left", "Mark as Resolved (r) stages the file.")
		return cmd
	}
	lines := tk.Lines
	if lines == nil {
		lines = []string{}
	}
	var out string
	if err := m.nvim.ExecLuaResult(mergeTakeLua, &out, m.merge.abs, tk.Start, tk.End, lines); err != nil {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Merge", err.Error())
		return cmd
	}
	m.mergeSync()
	return nil
}

// mergeUndo undoes the last change in the result (one Take at a time).
func (m *Model) mergeUndo() tea.Cmd {
	if !m.mergeViewShown() || !m.mergeEditor() {
		return nil
	}
	_ = m.nvim.ExecLuaArgs(`
		local p = ...
		local b = _G.termocode_merge_buf and _G.termocode_merge_buf(p) or -1
		if b < 0 then return end
		vim.api.nvim_buf_call(b, function() pcall(vim.cmd, 'silent undo') end)
		pcall(_G.termocode_conflict_paint, b)
	`, m.merge.abs)
	m.mergeSync()
	return nil
}

// mergeMarkResolved saves the result and stages it, but only when no
// conflict markers are left in it.
func (m *Model) mergeMarkResolved() tea.Cmd {
	if !m.mergeViewShown() {
		return nil
	}
	var cmd tea.Cmd
	if r, ok := m.mergeReadResult(); ok && !r.Gone && git.HasConflicts(strings.Join(r.Lines, "\n")) {
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "Can't mark as resolved",
			conflictWord(max(len(git.ParseConflictLines(r.Lines)), 1))+" left. Take a side or edit the file first.")
		return cmd
	}
	if m.mergeEditor() {
		_ = m.nvim.ExecLuaArgs(`
			local p = ...
			local b = _G.termocode_merge_buf and _G.termocode_merge_buf(p) or -1
			if b >= 0 then vim.api.nvim_buf_call(b, function() vim.cmd('silent update') end) end
		`, m.merge.abs)
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	rel := m.merge.rel
	return func() tea.Msg { return mergeResolvedMsg{rel: rel, err: git.MarkResolved(cwd, rel)} }
}

// onMergeResolved closes the view once the file is staged, and says what
// comes next.
func (m *Model) onMergeResolved(msg mergeResolvedMsg) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case errors.Is(msg.err, git.ErrMarkersLeft):
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "Can't mark as resolved", "Conflict markers are still in the saved file.")
		return cmd
	case msg.err != nil:
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Can't mark as resolved", msg.err.Error())
		return cmd
	}
	if m.mergeViewShown() && m.merge.rel == msg.rel {
		m.closeMergeView()
	}
	m.toast, cmd = m.toast.PushDetail(toast.Info, "Marked as resolved: "+msg.rel, m.mergeNextStep(msg.rel))
	return tea.Batch(cmd, fetchGitCmd())
}

// mergeNextStep is the hint after a file is resolved: the other
// conflicted files, else how to finish.
func (m Model) mergeNextStep(done string) string {
	left := 0
	for _, f := range m.gitFiles {
		if f.Conflicted() && f.Path != done {
			left++
		}
	}
	switch {
	case left == 1:
		return "1 more file in CONFLICTS."
	case left > 1:
		return fmt.Sprintf("%d more files in CONFLICTS.", left)
	case m.gitRebase.InProgress:
		return "Continue the rebase from the banner in Source Control."
	}
	return "Commit to finish."
}

// mergeSideWord says what one side did to a whole-file conflict.
func mergeSideWord(s git.ConflictSide) string {
	switch {
	case !s.Present:
		return "deleted it"
	case s.Binary:
		return "changed it (binary)"
	}
	return "changed it"
}

// openMergeWholeCard asks how to settle a conflict that can not be merged
// line by line: a binary file, or one side deleted the file. Cancel is
// first and has the focus (Enter and Esc change nothing).
func (m *Model) openMergeWholeCard(st git.ConflictStages) {
	why := "This file is binary, so it can't be merged line by line."
	if st.DeletedBy() != "" {
		why = "One side deleted this file and the other changed it."
	}
	msg := strings.Join([]string{
		why, "",
		"Ours " + mergeSideWord(st.Ours) + ". Theirs " + mergeSideWord(st.Theirs) + ".",
		"Keep ours or Keep theirs uses that side's file (a side that deleted it deletes it). Delete removes the file.",
	}, "\n")
	m.closeMergeView()
	m.merge = &mergeViewState{rel: st.Path, whole: &st}
	m.confirm = confirm.Risky("Merge conflict: "+st.Path, msg,
		confirm.Button{ID: "merge-cancel", Title: "Cancel"},
		confirm.Button{ID: "merge-keep-ours", Title: "Keep ours"},
		confirm.Button{ID: "merge-keep-theirs", Title: "Keep theirs"},
		confirm.Button{ID: "merge-delete", Title: "Delete", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindMergeWholeFile
}

// onMergeWholeCard runs the chosen answer off the loop.
func (m *Model) onMergeWholeCard(id string) tea.Cmd {
	if m.merge == nil || m.merge.whole == nil {
		return nil
	}
	rel := m.merge.whole.Path
	m.merge.whole = nil
	verb := strings.TrimPrefix(id, "merge-")
	if verb != "keep-ours" && verb != "keep-theirs" && verb != "delete" {
		return nil // Cancel, Esc
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		var err error
		switch verb {
		case "keep-ours":
			err = git.ResolveWithSide(cwd, rel, "ours")
		case "keep-theirs":
			err = git.ResolveWithSide(cwd, rel, "theirs")
		default:
			err = git.ResolveDelete(cwd, rel)
		}
		return mergeWholeDoneMsg{rel: rel, verb: verb, err: err}
	}
}

// onMergeWholeDone says how it went and refreshes Source Control.
func (m *Model) onMergeWholeDone(msg mergeWholeDoneMsg) tea.Cmd {
	var cmd tea.Cmd
	if msg.err != nil {
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Can't resolve "+msg.rel, msg.err.Error())
		return cmd
	}
	title := map[string]string{"keep-ours": "Kept ours: ", "keep-theirs": "Kept theirs: ", "delete": "Deleted: "}[msg.verb] + msg.rel
	m.toast, cmd = m.toast.PushDetail(toast.Info, title, m.mergeNextStep(msg.rel))
	return tea.Batch(cmd, fetchGitCmd())
}
