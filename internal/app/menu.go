package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/activity"
	"termocode/internal/menu"
	"termocode/internal/tabbar"
	"termocode/internal/toast"
)

type menuKindEnum int

const (
	menuKindExplorerFile menuKindEnum = iota
	menuKindExplorerDir
	menuKindTab
	menuKindEditor
	menuKindGitFile
	menuKindGitCommit
	menuKindGitActions
)

// Source Control context-menu action IDs.
const (
	gitMenuDiff     = "git-diff"
	gitMenuStage    = "git-stage"
	gitMenuDiscard  = "git-discard"
	gitMenuCommit   = "git-commit"
	gitMenuCopyHash = "git-copy-hash"
	gitMenuRefresh  = "git-refresh"

	// Action-bar overflow (⋯) menu.
	gitMenuAmend     = "git-amend"
	gitMenuSignoff   = "git-signoff"
	gitMenuCommitMsg = "git-commit-msg"
	gitMenuPush      = "git-push"
	gitMenuPull      = "git-pull"
	gitMenuFetch     = "git-fetch"
	gitMenuSync      = "git-sync"
)

// gitFileMenuItems builds the right-click menu for a changed file. willStage
// reflects what the Stage toggle would do next (stage vs unstage), so the label
// matches the action.
func gitFileMenuItems(willStage bool) []menu.Item {
	stage := menu.Item{ID: gitMenuStage, Title: "Unstage Changes", Icon: "−"}
	if willStage {
		stage = menu.Item{ID: gitMenuStage, Title: "Stage Changes", Icon: "+"}
	}
	return []menu.Item{
		{ID: gitMenuDiff, Title: "Open Changes", Icon: "≢"},
		stage,
		{ID: gitMenuDiscard, Title: "Discard Changes", Icon: "↩"},
		{Sep: true},
		{ID: gitMenuCommit, Title: "Commit...", Icon: "✓"},
		{ID: gitMenuRefresh, Title: "Refresh", Icon: "⟳"},
	}
}

// gitCommitMenuItems builds the right-click menu for a graph commit.
func gitCommitMenuItems() []menu.Item {
	return []menu.Item{
		{ID: gitMenuDiff, Title: "Open Changes", Icon: "≢"},
		{ID: gitMenuCopyHash, Title: "Copy Commit Hash", Icon: "⎘"},
		{Sep: true},
		{ID: gitMenuRefresh, Title: "Refresh", Icon: "⟳"},
	}
}

// openGitMenu shows the Source Control context menu for the row under the
// pointer. anchorX/anchorY are screen coordinates; anchorY also locates the
// clicked row (same mapping the renderer and left-click hit-test use). The
// clicked row becomes the cursor so the existing cursor-based git actions
// target it.
func (m *Model) openGitMenu(anchorX, anchorY int) {
	if !m.gitIsRepo {
		return
	}
	top, bodyH, _ := m.gitPanelLayout(m.h)
	bodyRow := anchorY - m.gitPanelTopOffset()
	if bodyRow < 0 || bodyRow >= bodyH {
		return
	}
	idx := top + bodyRow
	rows := m.gitPanelRows()
	if idx < 0 || idx >= len(rows) {
		return
	}
	r := rows[idx]
	var items []menu.Item
	switch r.kind {
	case gitRowFile:
		f := m.gitFiles[r.fileIndex]
		items = gitFileMenuItems(f.Untracked() || f.Unstaged())
		m.menuKind = menuKindGitFile
	case gitRowCommit:
		items = gitCommitMenuItems()
		m.menuKind = menuKindGitCommit
	default:
		return
	}
	m.gitCursor = idx
	m.focus = FocusExplorer
	m.menu = menu.New(items, anchorX, anchorY)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen = true
}

// handleGitMenuSelect dispatches a Source Control context-menu action. The
// target row is whatever openGitMenu left under the cursor.
func (m *Model) handleGitMenuSelect(id string) tea.Cmd {
	switch id {
	case gitMenuDiff:
		if cur, ok := m.gitCurrentRow(); ok && cur.kind == gitRowCommit {
			return m.gitShowCommitCmd(cur.hash)
		}
		return m.gitDiffCmd()
	case gitMenuStage:
		return m.gitStageToggle()
	case gitMenuDiscard:
		m.openGitDiscardConfirm()
	case gitMenuCommit:
		m.openCommitPrompt()
	case gitMenuCopyHash:
		if cur, ok := m.gitCurrentRow(); ok && cur.hash != "" {
			m.copyToClipboard(cur.hash)
			m.err = "Copied: " + cur.hash
		}
	case gitMenuRefresh:
		return fetchGitCmd()
	}
	return nil
}

// gitActionsMenuItems builds the ⋯ overflow menu for the commit action bar.
func gitActionsMenuItems() []menu.Item {
	return []menu.Item{
		{ID: gitMenuCommit, Title: "Commit", Icon: "✓"},
		{ID: gitMenuAmend, Title: "Commit (Amend)", Icon: "✎"},
		{ID: gitMenuSignoff, Title: "Commit (Sign-off)", Icon: "✍"},
		{ID: gitMenuCommitMsg, Title: "Commit Message…", Icon: "≡"},
		{Sep: true},
		{ID: gitMenuSync, Title: "Sync", Icon: "⟳"},
		{ID: gitMenuPush, Title: "Push", Icon: "↑"},
		{ID: gitMenuPull, Title: "Pull", Icon: "↓"},
		{ID: gitMenuFetch, Title: "Fetch", Icon: "⇊"},
		{Sep: true},
		{ID: gitMenuRefresh, Title: "Refresh", Icon: "⟳"},
	}
}

// openGitActionsMenu pops the ⋯ overflow under the action-bar chip.
func (m *Model) openGitActionsMenu() {
	contentW := m.explorerWidth - 1
	anchorX := activity.Width
	for _, b := range m.gitActionButtons(contentW) {
		if b.id == gitBtnMore {
			anchorX += b.x0
			break
		}
	}
	m.menu = menu.New(gitActionsMenuItems(), anchorX, gitActionBarRow+1)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen = true
	m.menuKind = menuKindGitActions
	m.focus = FocusExplorer
}

// handleGitActionsMenuSelect dispatches a ⋯ overflow action.
func (m *Model) handleGitActionsMenuSelect(id string) tea.Cmd {
	switch id {
	case gitMenuCommit:
		return m.gitInlineCommit(false, false)
	case gitMenuAmend:
		return m.gitInlineCommit(true, false)
	case gitMenuSignoff:
		return m.gitInlineCommit(false, true)
	case gitMenuCommitMsg:
		m.openCommitPrompt()
	case gitMenuSync:
		return m.gitSync()
	case gitMenuPush:
		return m.gitPush()
	case gitMenuPull:
		return m.gitPull()
	case gitMenuFetch:
		return m.gitFetch()
	case gitMenuRefresh:
		return fetchGitCmd()
	}
	return nil
}

// Tab context-menu action IDs.
const (
	tabMenuClose       = "tab-close"
	tabMenuCloseOthers = "tab-close-others"
	tabMenuCloseRight  = "tab-close-right"
	tabMenuCopyPath    = "tab-copy-path"
)

func tabMenuItems() []menu.Item {
	return []menu.Item{
		{ID: tabMenuClose, Title: "Close", Icon: "×"},
		{ID: tabMenuCloseOthers, Title: "Close Others", Icon: "⊘"},
		{ID: tabMenuCloseRight, Title: "Close All to the Right", Icon: "→"},
		{Sep: true},
		{ID: tabMenuCopyPath, Title: "Copy Path", Icon: "⎘"},
	}
}

// openTabMenu builds and shows the right-click context menu for a tab. anchorX
// and anchorY are screen coordinates. id is the buffer id of the clicked tab.
func (m *Model) openTabMenu(id, anchorX, anchorY int) {
	m.menu = menu.New(tabMenuItems(), anchorX, anchorY)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen = true
	m.menuKind = menuKindTab
	m.menuTabID = id
}

func explorerFileMenuItems() []menu.Item {
	return []menu.Item{
		{ID: "open", Title: "Open", Icon: "▸"},
		{ID: "open-side", Title: "Open to the Side", Icon: "▷"},
		{Sep: true},
		{ID: "rename", Title: "Rename...", Icon: "✎"},
		{ID: "delete", Title: "Delete", Icon: "×"},
		{Sep: true},
		{ID: "copy-path", Title: "Copy Path", Icon: "⎘"},
		{ID: "reveal-terminal", Title: "Reveal in Terminal", Icon: "❯"},
	}
}

func explorerDirMenuItems() []menu.Item {
	return []menu.Item{
		{ID: "new-file", Title: "New File...", Icon: "+"},
		{ID: "new-folder", Title: "New Folder...", Icon: "+"},
		{Sep: true},
		{ID: "rename", Title: "Rename...", Icon: "✎"},
		{ID: "delete", Title: "Delete", Icon: "×"},
		{Sep: true},
		{ID: "open-terminal", Title: "Open in Terminal", Icon: "❯"},
		{ID: "copy-path", Title: "Copy Path", Icon: "⎘"},
	}
}

// openExplorerMenu builds and shows the right-click menu. anchorX/anchorY are
// screen coordinates (used to position the popup); explorerY is the row inside
// the explorer's own content area (used to look up the clicked node).
func (m *Model) openExplorerMenu(anchorX, anchorY, explorerY int) {
	node, ok := m.explorer.VisibleAt(explorerY)
	if !ok {
		return
	}
	var items []menu.Item
	if node.IsDir {
		items = explorerDirMenuItems()
		m.menuKind = menuKindExplorerDir
	} else {
		items = explorerFileMenuItems()
		m.menuKind = menuKindExplorerFile
	}
	m.menu = menu.New(items, anchorX, anchorY)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen = true
	m.menuPath = node.Path
}

func (m *Model) handleMenuSelect(id string) tea.Cmd {
	if m.menuKind == menuKindTab {
		return m.handleTabMenuSelect(id)
	}
	if m.menuKind == menuKindEditor {
		return m.handleEditorMenuSelect(id)
	}
	if m.menuKind == menuKindGitFile || m.menuKind == menuKindGitCommit {
		return m.handleGitMenuSelect(id)
	}
	if m.menuKind == menuKindGitActions {
		return m.handleGitActionsMenuSelect(id)
	}
	path := m.menuPath
	switch id {
	case "open":
		if !isSupportedFile(path) {
			var c tea.Cmd
			t, d := unsupportedFileMessage(path)
			m.toast, c = m.toast.PushDetail(toast.Warn, t, d)
			return c
		}
		m.ensureEditorWindowCurrent()
		if err := m.editor.Open(path); err != nil {
			m.err = err.Error()
		} else {
			m.err = ""
		}
		m.focus = FocusEditor
	case "open-side":
		// Open in a vertical split next to the current buffer (VSCode's
		// "Open to the Side"). nvim's `:vsplit <path>` creates the split
		// inside the editor pane's grid, so termocode's tab bar still
		// reflects every open buffer; the user just sees them side-by-
		// side instead of stacked tabs.
		if !isSupportedFile(path) {
			var c tea.Cmd
			t, d := unsupportedFileMessage(path)
			m.toast, c = m.toast.PushDetail(toast.Warn, t, d)
			return c
		}
		if m.nvim != nil {
			_ = m.nvim.Command("vsplit " + path)
		}
		m.focus = FocusEditor
	case "copy-path":
		m.copyToClipboard(path)
		m.err = "Copied: " + path
	case "reveal-terminal":
		// Open the integrated terminal at the file's parent dir, NOT a
		// new external sub-shell. The previous behaviour spawned a
		// detached shell that confused the user about cwd.
		m.openIntegratedTerminalAt(filepath.Dir(path))
		return nil
	case "open-terminal":
		m.openIntegratedTerminalAt(path)
		return nil
	case "rename":
		m.openRenamePrompt(path)
	case "delete":
		// If the user has a multi-selection AND the right-clicked row is
		// part of it, treat the menu's Delete as a bulk delete of every
		// selected entry. A right-click on a row OUTSIDE the selection
		// falls through to single-row delete (matches VSCode's "right-
		// click acts on the row you clicked, not the selection" rule
		// when the click missed the selection set).
		if sel := m.explorer.Selection(); len(sel) > 0 {
			inSel := false
			for _, s := range sel {
				if s == path {
					inSel = true
					break
				}
			}
			if inSel {
				m.openBulkDeleteConfirm(sel)
				return nil
			}
		}
		m.openDeleteConfirm(path)
	case "new-file":
		m.openNewFilePrompt(path)
	case "new-folder":
		m.openNewFolderPrompt(path)
	}
	return nil
}

// handleTabMenuSelect dispatches the action selected from the tab right-click
// menu against m.menuTabID.
//
// TODO: "Close Others" / "Close All to the Right" fan out via tea.Batch, so if
// any of the targeted buffers is dirty the first CloseMsg opens the unsaved-
// changes confirm dialog and the remaining CloseMsgs are routed to the dialog
// (and dropped). A proper fix needs to queue the pending closes and resume
// after the dialog resolves.
func (m *Model) handleTabMenuSelect(id string) tea.Cmd {
	target := m.menuTabID
	switch id {
	case tabMenuClose:
		return closeBufferCmd(target)
	case tabMenuCloseOthers:
		var cmds []tea.Cmd
		for _, b := range m.bufs {
			if b.ID == target {
				continue
			}
			cmds = append(cmds, closeBufferCmd(b.ID))
		}
		return tea.Batch(cmds...)
	case tabMenuCloseRight:
		idx := -1
		for i, b := range m.bufs {
			if b.ID == target {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil
		}
		var cmds []tea.Cmd
		for i := idx + 1; i < len(m.bufs); i++ {
			cmds = append(cmds, closeBufferCmd(m.bufs[i].ID))
		}
		return tea.Batch(cmds...)
	case tabMenuCopyPath:
		buf, ok := m.findBuffer(target)
		if !ok || buf.Path == "" {
			return nil
		}
		m.copyToClipboard(buf.Path)
		m.err = "Copied: " + buf.Path
	}
	return nil
}

// closeBufferCmd returns a tea.Cmd that emits a tabbar.CloseMsg for id, so the
// existing CloseMsg handler (with its dirty-buffer prompt) processes it.
func closeBufferCmd(id int) tea.Cmd {
	return func() tea.Msg { return tabbar.CloseMsg{ID: id} }
}

// Editor right-click context-menu action IDs.
const (
	editorMenuCut          = "editor-cut"
	editorMenuCopy         = "editor-copy"
	editorMenuPaste        = "editor-paste"
	editorMenuFormat       = "editor-format"
	editorMenuToggleComm   = "editor-toggle-comment"
	editorMenuGoToDef      = "editor-go-to-def"
)

func editorMenuItems() []menu.Item {
	return []menu.Item{
		{ID: editorMenuCut, Title: "Cut", Icon: "✂", Hint: "Ctrl+X"},
		{ID: editorMenuCopy, Title: "Copy", Icon: "⎘", Hint: "Ctrl+C"},
		{ID: editorMenuPaste, Title: "Paste", Icon: "▤", Hint: "Ctrl+V"},
		{Sep: true},
		{ID: editorMenuFormat, Title: "Format Document", Icon: "⚙", Hint: "Shift+Alt+F"},
		{ID: editorMenuToggleComm, Title: "Toggle Line Comment", Icon: "/", Hint: "Ctrl+/"},
		{Sep: true},
		{ID: editorMenuGoToDef, Title: "Go to Definition", Icon: "▸", Hint: "F12 / Ctrl+Click"},
	}
}

// openEditorMenu opens the right-click context menu inside the editor pane at
// the given screen-anchor coordinates.
func (m *Model) openEditorMenu(anchorX, anchorY int) {
	m.menu = menu.New(editorMenuItems(), anchorX, anchorY)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen = true
	m.menuKind = menuKindEditor
}

// handleEditorMenuSelect runs the action chosen from the editor context menu.
// All actions go through nvim's Ex command interface.
func (m *Model) handleEditorMenuSelect(id string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	switch id {
	case editorMenuCut:
		_ = m.nvim.Command(`normal! "+d`)
	case editorMenuCopy:
		_ = m.nvim.Command(`normal! "+y`)
	case editorMenuPaste:
		_ = m.nvim.Command(`normal! "+p`)
	case editorMenuFormat:
		_ = m.nvim.Command(`lua vim.lsp.buf.format({ async = false })`)
	case editorMenuToggleComm:
		_ = m.nvim.Command(`normal gcc`)
	case editorMenuGoToDef:
		_ = m.nvim.ExecLua(gotoDefinitionLua)
	}
	return nil
}

// gotoDefinitionLua runs vim.lsp.buf.definition with a custom on_list
// handler so multi-result LSP responses jump to the first location IN
// the current window instead of dumping them into a quickfix split at
// the bottom of the screen — that quickfix UX clashes with the rest
// of termocode's modal/picker flow. Used by both the editor right-click
// menu's "Go to Definition" item and Ctrl+click in the editor pane.
const gotoDefinitionLua = `
vim.lsp.buf.definition({
  on_list = function(opts)
    if not opts.items or #opts.items == 0 then return end
    local first = opts.items[1]
    if first.filename and first.filename ~= '' then
      vim.cmd('edit ' .. vim.fn.fnameescape(first.filename))
    end
    if first.lnum and first.lnum > 0 then
      pcall(vim.api.nvim_win_set_cursor, 0, { first.lnum, math.max((first.col or 1) - 1, 0) })
    end
  end,
})
`

func (m *Model) copyToClipboard(s string) {
	if m.nvim == nil {
		return
	}
	escaped := strings.ReplaceAll(s, "'", "''")
	_ = m.nvim.Command(fmt.Sprintf("let @+ = '%s' | let @\" = @+", escaped))
	// nvim's `+` register only reaches the *server's* clipboard
	// (and only if xclip/xsel/wl-copy is installed). OSC 52 makes
	// the menu "Copy Path" actions also land in the user's local
	// clipboard when termocode runs over SSH.
	emitOSC52(s)
}

func openShellInDirCmd(dir string) tea.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	// Spawn an interactive shell with a hint banner so the user knows
	// they're in a sub-shell, not termocode itself. Without this people
	// run commands here thinking termocode quit and end up confused
	// about cwd (basename collisions like /root/termocode vs
	// /root/termocode/cmd/termocode look identical in most prompts).
	banner := fmt.Sprintf(
		"echo '\\033[2m── termocode shell at: %s';"+
			"echo '   type \\033[1mexit\\033[0;2m to return to the editor ──\\033[0m';"+
			"exec %s",
		dir, shell)
	c := exec.Command("/bin/sh", "-c", banner)
	c.Env = os.Environ()
	c.Dir = dir
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return ErrMsg{Err: err}
		}
		return nil
	})
}
