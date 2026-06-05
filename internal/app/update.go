package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.design/x/clipboard"

	"termocode/internal/activity"
	"termocode/internal/confirm"
	"termocode/internal/explorer"
	"termocode/internal/findbar"
	"termocode/internal/git"
	"termocode/internal/keymap"
	"termocode/internal/toast"
	"termocode/internal/menu"
	"termocode/internal/nvim"
	"termocode/internal/picker"
	"termocode/internal/preview"
	"termocode/internal/prompt"
	"termocode/internal/recents"
	"termocode/internal/replacebar"
	"termocode/internal/search"
	"termocode/internal/tabbar"
)

// Update wraps updateInner and keeps the xterm mouse-tracking mode in sync:
// all-motion (1003) for hover, downgraded to button-only (1002) while a text
// input is focused so the SGR-fragmentation leak can't corrupt typed input.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.updateInner(msg)
	nm, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	// Hover card: fetch the commit metadata when the pointer moves onto a new
	// commit row in the Source Control panel.
	if hoverCmd := nm.updateHoverCommit(); hoverCmd != nil {
		cmd = tea.Batch(cmd, hoverCmd)
	}
	want := !nm.anyTextInputOpen()
	if want != nm.mouseAllMotion {
		nm.mouseAllMotion = want
		mode := tea.EnableMouseCellMotion
		if want {
			mode = tea.EnableMouseAllMotion
		}
		return nm, tea.Batch(cmd, mode)
	}
	return nm, cmd
}

// anyTextInputOpen reports whether a focused text field is on screen — the
// only context where all-motion mouse events risk leaking into typed input.
func (m Model) anyTextInputOpen() bool {
	return m.pickerOpen || m.promptOpen || m.searchOpen || m.replaceOpen ||
		m.settingsModalOpen || m.recentsOpen || m.findOpen
}

func (m Model) updateInner(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Overlay control messages always handled regardless of state.
	switch msg := msg.(type) {
	case commitDetailMsg:
		if m.commitDetails == nil {
			m.commitDetails = map[string]git.CommitDetail{}
		}
		m.commitDetails[msg.hash] = msg.detail
		return m, nil
	case picker.SelectMsg:
		m.pickerOpen = false
		return m, m.handlePickerSelect(msg)
	case picker.CloseMsg:
		m.pickerOpen = false
		return m, nil
	case recents.SelectMsg:
		// Recents modal selection: close the overlay, open the file via
		// the editor (which also pushes the path back onto the recents
		// list), reveal it in the tree, and refocus the editor.
		m.recentsOpen = false
		if msg.Path != "" {
			m.ensureEditorWindowCurrent()
			if err := m.editor.Open(msg.Path); err != nil {
				m.err = err.Error()
			} else {
				pushRecent(msg.Path)
				m.explorer.RevealPath(msg.Path)
			}
		}
		m.focus = FocusEditor
		return m, nil
	case recents.CloseMsg:
		m.recentsOpen = false
		return m, nil
	case SettingsSelectMsg:
		// Close the settings modal then route the row ID through the
		// existing prompt-driven row handler. The prompt opens on top of
		// the dimmed editor, identical to the legacy flow.
		m.settingsModalOpen = false
		return m, m.onSettingsRowSelected(msg.ID)
	case SettingsCloseMsg:
		m.settingsModalOpen = false
		return m, nil
	case menu.SelectMsg:
		m.menuOpen = false
		return m, m.handleMenuSelect(msg.ID)
	case menu.CloseMsg:
		m.menuOpen = false
		return m, nil
	case findbar.SearchMsg:
		m.applySearch(msg.Query)
		return m, nil
	case findbar.FindNextMsg:
		if m.nvim != nil {
			_ = m.nvim.Command("silent! normal! n")
			// Cursor moved → searchcount().current changes; refresh the
			// indicator so "3/12" advances to "4/12" etc.
			m.find.SetMatchCount(m.fetchMatchCount())
		}
		return m, nil
	case findbar.FindPrevMsg:
		if m.nvim != nil {
			_ = m.nvim.Command("silent! normal! N")
			m.find.SetMatchCount(m.fetchMatchCount())
		}
		return m, nil
	case findbar.CloseMsg:
		m.findOpen = false
		if m.nvim != nil {
			_ = m.nvim.Command("nohlsearch")
		}
		m.applyLayout()
		return m, nil
	case findbar.ReplaceMsg:
		m.applyReplaceNext(msg.Find, msg.Replace)
		m.find.SetMatchCount(m.fetchMatchCount())
		return m, nil
	case findbar.ReplaceAllMsg:
		m.applyReplaceAll(msg.Find, msg.Replace)
		m.find.SetMatchCount(m.fetchMatchCount())
		return m, nil
	case replacebar.ReplaceNextMsg:
		m.applyReplaceNext(msg.Find, msg.Replace)
		return m, nil
	case replacebar.ReplaceAllMsg:
		m.applyReplaceAll(msg.Find, msg.Replace)
		return m, nil
	case replacebar.CloseMsg:
		m.replaceOpen = false
		if m.nvim != nil {
			_ = m.nvim.Command("nohlsearch")
		}
		m.applyLayout()
		return m, nil
	case confirm.SelectMsg:
		m.confirmOpen = false
		return m, m.handleConfirmSelect(msg.ID)
	case confirm.CloseMsg:
		m.confirmOpen = false
		return m, nil
	case activity.SwitchMsg:
		// Settings is a one-shot action (open the modal Settings UI), not
		// a sidebar view — clicking it should pop the picker instead of
		// switching the sidebar to an empty placeholder pane.
		if msg.View == activity.ViewSettings {
			return m, m.openSettingsPicker()
		}
		m.activity.SetActive(msg.View)
		if !m.showExp {
			m.showExp = true
			m.applyLayout()
		}
		return m, nil
	case activity.ToggleSidebarMsg:
		m.showExp = !m.showExp
		m.applyLayout()
		return m, nil
	case PreviewMsg:
		m.preview = preview.New(msg.Title, msg.Body)
		m.preview.SetSize(m.w, m.h)
		m.previewOpen = true
		return m, nil
	case gitDiffReadyMsg:
		m.openSideBySideDiff(msg)
		return m, nil
	case gitCommitDiffReadyMsg:
		m.openCommitDiffBuffer(msg)
		return m, nil
	case preview.CloseMsg:
		m.previewOpen = false
		return m, nil
	case prompt.SubmitMsg:
		m.promptOpen = false
		return m, m.handlePromptSubmit(msg.Value)
	case prompt.CloseMsg:
		m.promptOpen = false
		return m, nil
	case search.SelectMsg:
		m.searchOpen = false
		if m.nvim != nil {
			abs := msg.Path
			if !filepath.IsAbs(abs) {
				if cwd, err := os.Getwd(); err == nil {
					abs = filepath.Join(cwd, abs)
				}
			}
			m.ensureEditorWindowCurrent()
			_ = m.nvim.Command(fmt.Sprintf("edit +%d %s", msg.Line, abs))
		}
		m.focus = FocusEditor
		return m, nil
	case search.CloseMsg:
		m.searchOpen = false
		return m, nil
	case stickyContextMsg:
		needsLayout := (m.stickyContext == "") != (msg.Signature == "")
		m.stickyContext = msg.Signature
		if needsLayout {
			m.applyLayout()
		}
		return m, nil
	case toast.TickMsg:
		var cmd tea.Cmd
		m.toast, cmd = m.toast.Tick(time.Time(msg))
		return m, cmd
	case scrollbarMarkersMsg:
		m.scrollbarMarks = msg.Marks
		m.scrollbarTotalLines = msg.TotalLines
		return m, nil
	case ProblemsMsg:
		return m, m.applyProblemsMsg(msg)
	case CodeActionsMsg:
		return m, m.applyCodeActionsMsg(msg)
	case SymbolPickerMsg:
		return m, m.applySymbolPickerMsg(msg)
	case WorkspaceSymbolPickerMsg:
		return m, m.applyWorkspaceSymbolPickerMsg(msg)
	case BookmarksMsg:
		return m, m.applyBookmarksMsg(msg)
	case ReferencesPickerMsg:
		return m, m.applyReferencesPickerMsg(msg)
	case hoverEmptyMsg:
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No hover info")
		return m, toastCmd
	}

	// nvim event stream stays alive even with overlays open.
	if newM, cmd, handled := m.handleNvimMsg(msg); handled {
		return newM, cmd
	}

	if m.previewOpen {
		return m.routeToPreview(msg)
	}
	if m.searchOpen {
		return m.routeToSearch(msg)
	}
	if m.promptOpen {
		return m.routeToPrompt(msg)
	}
	if m.confirmOpen {
		return m.routeToConfirm(msg)
	}
	if m.settingsModalOpen {
		return m.routeToSettingsModal(msg)
	}
	if m.pickerOpen {
		return m.routeToPicker(msg)
	}
	if m.recentsOpen {
		return m.routeToRecents(msg)
	}
	if m.menuOpen {
		return m.routeToMenu(msg)
	}
	if m.findOpen {
		return m.routeToFind(msg)
	}
	if m.replaceOpen {
		return m.routeToReplace(msg)
	}
	// Overflow (⋮) menu: keyboard input goes to the menu's own router so
	// arrow keys / Enter / Esc are absorbed; mouse events fall through to
	// handleMouse where click-outside-close runs against the menu rect.
	if m.overflowMenuOpen {
		if _, ok := msg.(tea.KeyMsg); ok {
			return m.routeToOverflowMenu(msg)
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		return m, nil

	case tea.KeyMsg:
		return m.handleGlobalKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case explorer.OpenFileMsg:
		if !isSupportedFile(msg.Path) {
			var c tea.Cmd
			t, d := unsupportedFileMessage(msg.Path)
			m.toast, c = m.toast.PushDetail(toast.Warn, t, d)
			return m, c
		}
		m.ensureEditorWindowCurrent()
		if err := m.editor.Open(msg.Path); err != nil {
			m.err = err.Error()
		} else {
			m.err = ""
		}
		// Mouse-clicks from the explorer set KeepFocus=true so the user
		// can keep arrow-navigating the tree while files preview in the
		// editor. Enter / palette open paths default to KeepFocus=false
		// and move focus to the editor as before.
		if !msg.KeepFocus {
			m.focus = FocusEditor
		}
		return m, nil

	case tabbar.SwitchMsg:
		if m.nvim != nil {
			// `:buffer N` loads the buffer into the CURRENT window. If the
			// user clicked an editor tab while focus was inside the
			// integrated terminal, the terminal window IS current — and the
			// editor buffer would land in it, displacing the running shell
			// (and leaving the tab-bar splice rendering across editor cells).
			// Hop to any non-terminal window first.
			m.ensureEditorWindowCurrent()
			_ = m.nvim.Command(fmt.Sprintf("buffer %d", msg.ID))
		}
		// Also reveal the file in the explorer tree — expand any
		// collapsed ancestor directories so the user can see where the
		// active tab lives.
		for _, b := range m.bufs {
			if b.ID == msg.ID && b.Path != "" {
				m.explorer.RevealPath(b.Path)
				break
			}
		}
		return m, nil

	case tabbar.CloseMsg:
		if m.promptCloseBufferIfDirty(msg.ID) {
			// Dirty path: pushClosed runs from the confirm handler if the
			// user picks Save or Don't Save (not on Cancel).
			return m, nil
		}
		if buf, ok := m.findBuffer(msg.ID); ok {
			m.pushClosed(buf.Path)
		}
		if m.nvim != nil {
			_ = m.nvim.Command(fmt.Sprintf("bdelete %d", msg.ID))
		}
		return m, nil

	case tabbar.TabContextMsg:
		m.openTabMenu(msg.ID, msg.X, msg.Y)
		return m, nil

	case ErrMsg:
		m.err = msg.Err.Error()
		return m, nil

	case ToastMsg:
		// Generic toast plumbing for background commands (tea.ExecProcess
		// callbacks and similar) that can't directly mutate the model.
		var c tea.Cmd
		if msg.Body != "" {
			m.toast, c = m.toast.PushDetail(msg.Level, msg.Title, msg.Body)
		} else {
			m.toast, c = m.toast.Push(msg.Level, msg.Title)
		}
		return m, c
	}

	return m, nil
}

func (m Model) handleNvimMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case nvim.ReadyMsg:
		m.nvimAttached = true
		m.applyLayout()
		// Restore previous session: re-open every file that was open last
		// time and re-expand the explorer tree to its previous state.
		// Multi-root: load any persisted extra roots BEFORE applying the
		// expanded-dirs set, so an expansion path that lives under an extra
		// root is found and reopened too.
		m.loadAndApplyWorkspaceRoots()
		sess := loadSession()
		if len(sess.ExpandedDirs) > 0 {
			m.explorer.ExpandPaths(sess.ExpandedDirs)
		}
		if len(sess.OpenFiles) > 0 && m.nvim != nil {
			// Restore every file in ONE Lua call instead of N synchronous
			// :edit commands. The previous loop fired one msgpack-RPC per
			// file, and each `:edit` triggered BufEnter / BufRead / syntax
			// / LSP attach + a redraw — which produced visible flicker on
			// session restore. The Lua version:
			//   - opens all inactive buffers under `noautocmd` so the
			//     autocommand chain doesn't fire per file,
			//   - sets each buffer's cursor in-place,
			//   - then does a single normal `:edit <active>` at the end so
			//     LSP / syntax attach fire for the active buffer only.
			payload := struct {
				Open    []string          `json:"open"`
				Active  string            `json:"active"`
				Cursors map[string][2]int `json:"cursors"`
			}{
				Open:    sess.OpenFiles,
				Active:  sess.ActivePath,
				Cursors: sess.Cursors,
			}
			b, _ := json.Marshal(payload)
			lua := `local p = vim.json.decode([==[` + string(b) + `]==])
if type(p) ~= 'table' then return end
local active = p.active or ''
local cursors = p.cursors or {}
for _, path in ipairs(p.open or {}) do
  if path ~= active and path ~= '' then
    pcall(vim.cmd, 'silent! noautocmd edit ' .. vim.fn.fnameescape(path))
  end
end
for path, pos in pairs(cursors) do
  if path ~= '' and type(pos) == 'table' and (pos[1] or 0) > 0 then
    local col = math.max(pos[2] or 1, 1)
    pcall(vim.cmd, string.format('silent! noautocmd buffer %s | call cursor(%d, %d)',
      vim.fn.fnameescape(path), pos[1], col))
  end
end
if active ~= '' then
  pcall(vim.cmd, 'silent! edit ' .. vim.fn.fnameescape(active))
  local pos = cursors[active]
  if type(pos) == 'table' and (pos[1] or 0) > 0 then
    local col = math.max(pos[2] or 1, 1)
    pcall(vim.fn.cursor, pos[1], col)
  end
end
`
			_ = m.nvim.ExecLua(lua)
		}
		// Also reveal every restored file in the tree so the explorer
		// expands to show all of them — even ones the user originally
		// opened via Ctrl+P (which doesn't trigger a tree expansion).
		// The active file is revealed last so its row ends up as cursor.
		for _, p := range sess.OpenFiles {
			if p != sess.ActivePath {
				m.explorer.RevealPath(p)
			}
		}
		if sess.ActivePath != "" {
			m.explorer.RevealPath(sess.ActivePath)
		}
		// Persist the freshly-expanded set so a subsequent restart sees
		// the same tree shape even if no further user interaction happens.
		m.persistExplorerState()
		return m, nil, true
	case nvim.RedrawMsg:
		// Watch for mode_change events so we can update the hardware
		// cursor shape (DECSCUSR) in View(). Events arrive as
		// ["mode_change", [name, idx], ...].
		for _, ev := range msg.Events {
			if len(ev) < 2 {
				continue
			}
			if name, _ := ev[0].(string); name == "mode_change" {
				if last, ok := ev[len(ev)-1].([]any); ok && len(last) >= 1 {
					if mode, ok := last[0].(string); ok {
						m.editorMode = mode
					}
				}
			}
		}
		m.editor.ApplyRedraw(msg.Events)
		if m.nvim != nil {
			cmds := []tea.Cmd{m.nvim.Next()}
			// Throttle FetchState. RedrawMsg fires per nvim event batch
			// (every grid_line / grid_scroll / etc.) — at ~60fps during
			// scroll that's a Lua roundtrip every 16ms. Coalesce to 100ms.
			const stateThrottle = 100 * time.Millisecond
			if time.Since(m.stateLastFetch) >= stateThrottle {
				m.stateLastFetch = time.Now()
				cmds = append(cmds, m.fetchStateCmd())
			}
			return m, tea.Batch(cmds...), true
		}
		return m, nil, true
	case StateMsg:
		// Recents: push to disk only when the active path actually changes.
		if msg.Path != "" && msg.Path != m.lastRecordedPath {
			m.lastRecordedPath = msg.Path
			pushRecent(msg.Path)
		}
		// Clear the cached preferred-action hint when the cursor moves
		// off the line it was pinned to — the diagnostic that drove the
		// hint almost certainly doesn't apply at the new line.
		if m.preferredCodeAction != nil && msg.CursorLine != m.preferredCodeActionLine {
			m.preferredCodeAction = nil
		}
		m.cursorLine = msg.CursorLine
		// File-watcher toast: surface "Reloaded <basename>" the first time
		// we see a new reload-seq value so each external mutation fires
		// exactly one toast (subsequent StateMsgs with the same seq pass
		// through silently).
		var reloadToastCmd tea.Cmd
		if msg.LastReloadSeq > m.lastReloadSeq && msg.LastReloadPath != "" {
			m.lastReloadSeq = msg.LastReloadSeq
			m.toast, reloadToastCmd = m.toast.PushDetail(toast.Info, "Reloaded", filepath.Base(msg.LastReloadPath))
		} else if msg.LastReloadSeq > m.lastReloadSeq {
			// Counter bumped but path was empty — still advance our cursor
			// so we don't repeatedly try to surface the missing event.
			m.lastReloadSeq = msg.LastReloadSeq
		}
		// Persist current session. Files only update on a non-empty
		// snapshot (so closing the last buffer doesn't clobber a good
		// list); explorer expansion ALWAYS updates so tree state is
		// preserved even when no files are currently open.
		paths := make([]string, 0, len(msg.Bufs))
		for _, b := range msg.Bufs {
			if b.Path != "" {
				paths = append(paths, b.Path)
			}
		}
		existing := loadSession()
		if len(paths) > 0 {
			existing.OpenFiles = paths
			existing.ActivePath = msg.Path
		}
		existing.ExpandedDirs = m.explorer.ExpandedPaths()
		// Merge (don't replace): a buffer that was just closed externally
		// drops out of msg.Cursors but its last-known position is still
		// useful if the user re-opens that file in the same session, so we
		// keep stale entries until they're explicitly overwritten.
		if existing.Cursors == nil {
			existing.Cursors = map[string][2]int{}
		}
		for path, pos := range msg.Cursors {
			existing.Cursors[path] = pos
		}
		saveSession(existing)
		m.editor.SetMeta(msg.Path, msg.Dirty, msg.Lang)
		m.editor.SetDiagnostics(msg.Errors, msg.Warnings)
		m.bufs = toNvimBufs(msg.Bufs)
		m.activeBuf = msg.Active
		// Move pinned buffers to the front before handing the list to the
		// tab bar so the rendering order matches the pinned-set state.
		m.tabs.SetBuffers(m.reorderPinned(m.bufs), msg.Active)
		// Track the side-by-side diff view straight from nvim's window state
		// so it self-corrects when the user opens/closes the diff.
		m.gitDiffActive = msg.Diff
		// Refresh layout-extension state: breadcrumbs from path, sticky
		// context from the enclosing function/class signature. Only the
		// breadcrumbs change on every state msg; sticky context is fetched
		// lazily and populated by maybeRefreshStickyContext below.
		m.refreshBreadcrumbs(msg.Path)
		// Reconcile integrated-terminal state with nvim: detects the
		// case where the shell exited on its own (Ctrl+D / `exit`) and
		// nvim's on_exit closed the buffer — we need to clear m.termOpen
		// so the chrome (header / status bar / palette wording) reflects
		// reality without waiting for the user to toggle. Refresh the
		// header's cwd echo from b:term_title (set by OSC 7-emitting
		// shells) on the same cadence — the throttle inside the helper
		// makes the per-frame cost negligible.
		if m.syncTerminalState() {
			m.applyLayout()
		}
		m.refreshTerminalCwd()
		// Recompute the "user is actively typing into the integrated
		// terminal" flag. handleGlobalKey reads this to decide whether
		// to pass through terminal-meaningful shortcuts (Ctrl+C as
		// SIGINT, Tab for completion, Ctrl+R for history, …) to the
		// PTY instead of running termocode's keymap. The check requires
		// BOTH the current window being our terminal split AND mode == 't'
		// (terminal-insert) — Terminal-Normal mode (`nt`, e.g. after a
		// click that drops out of insert) should still let termocode
		// handle keys until the ModeChanged *:nt autocmd in model.go
		// re-enters terminal-insert. Cleared whenever the panel closes
		// or the user moves focus to an editor window.
		m.inTerminal = m.termOpen && m.terminalWinID > 0 &&
			msg.CurrentWin == m.terminalWinID && msg.Mode == "t"
		return m, tea.Batch(m.maybeRefreshStickyContext(), m.maybeRefreshScrollbarMarkers(), reloadToastCmd), true
	case GitMsg:
		m.gitIsRepo = msg.IsRepo
		m.gitBranch = msg.Branch
		m.gitFiles = msg.Files
		m.gitGraph = msg.Graph
		// Clamp against the visible-row count: the accordion interleaves
		// section headers, dir headers, and graph rows, so the row count
		// differs from len(gitFiles).
		m.gitClampCursor()
		return m, nil, true
	case nvim.ErrMsg:
		m.err = msg.Err.Error()
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) routeToPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		m.picker.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.picker, cmd = m.picker.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
		return m, cmd
	}
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

// routeToRecents is the recents-modal twin of routeToPicker. Mouse
// events are routed to the modal's HandleMouse for click-to-select and
// wheel-to-scroll; keyboard events go through Update() like any other
// component.
func (m Model) routeToRecents(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		m.recents.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.recents, cmd = m.recents.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
		return m, cmd
	}
	var cmd tea.Cmd
	m.recents, cmd = m.recents.Update(msg)
	return m, cmd
}

// routeToSettingsModal feeds events to the dedicated Settings overlay. The
// modal only consumes keys (no fuzzy search, no mouse navigation yet); window
// resize updates its remembered geometry so Box() lays out correctly.
func (m Model) routeToSettingsModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		m.settingsModal.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.settingsModal, cmd = m.settingsModal.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
		return m, cmd
	}
	var cmd tea.Cmd
	m.settingsModal, cmd = m.settingsModal.Update(msg)
	return m, cmd
}

func (m Model) routeToMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		m.menu.SetScreenSize(m.w, m.h)
		return m, nil
	}
	var cmd tea.Cmd
	m.menu, cmd = m.menu.Update(msg)
	return m, cmd
}

func (m Model) routeToSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		m.search.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		_ = msg
		return m, nil
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	return m, cmd
}

func (m Model) routeToPrompt(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		m.prompt.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		_ = msg
		return m, nil
	}
	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(msg)
	return m, cmd
}

func (m Model) routeToPreview(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		m.preview.SetSize(m.w, m.h)
		return m, nil
	}
	var cmd tea.Cmd
	m.preview, cmd = m.preview.Update(msg)
	return m, cmd
}

func (m Model) routeToConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		m.confirm.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		// Click on a Yes / No / Cancel button activates it. Clicks
		// elsewhere (inside or outside the modal) are silently
		// swallowed — destructive prompts must not be dismissed by a
		// stray click; the user has to use Esc or pick a button.
		var cmd tea.Cmd
		m.confirm, cmd, _ = m.confirm.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
		return m, cmd
	}
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(msg)
	return m, cmd
}

func (m Model) routeToFind(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		return m, nil
	case tea.MouseMsg:
		// Forward through the global mouse dispatcher: it owns the find-bar
		// hit-test (the ↑ / ↓ / × glyph buttons) and falls through to the
		// rest of the UI for clicks outside the panel, so the user can
		// still hit tabs / explorer without dismissing the bar first.
		return m.handleMouse(msg)
	}
	var cmd tea.Cmd
	m.find, cmd = m.find.Update(msg)
	return m, cmd
}

func (m Model) routeToReplace(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		return m, nil
	case tea.MouseMsg:
		_ = msg
		return m, nil
	}
	var cmd tea.Cmd
	m.replace, cmd = m.replace.Update(msg)
	return m, cmd
}

// vimRegexEscape escapes characters that are special inside an nvim "magic"
// regex pattern. Used when interpolating raw user text into a `:s` source.
func vimRegexEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '/', '.', '*', '[', ']', '^', '$', '~':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString("\\n")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// vimReplaceEscape escapes characters that are special inside the replacement
// half of a `:s` command (the right side of the second slash).
func vimReplaceEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '/', '&', '~':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString("\\r")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// findbarPattern builds an nvim search/substitute LHS pattern from `find`,
// honoring the findbar's case / word / regex toggles. Mirrors applySearch's
// recipe so :s and the @/ search register agree.
func (m *Model) findbarPattern(find string) string {
	pat := find
	if !m.find.UseRegex() {
		pat = `\V` + strings.ReplaceAll(pat, `\`, `\\`)
		// Inside \V, slashes still need escaping for the :s delimiter.
		pat = strings.ReplaceAll(pat, `/`, `\/`)
	} else {
		// In magic-regex mode escape only the :s delimiter.
		pat = strings.ReplaceAll(pat, `/`, `\/`)
	}
	if m.find.WholeWord() {
		if m.find.UseRegex() {
			pat = `\<` + pat + `\>`
		} else {
			body := strings.TrimPrefix(pat, `\V`)
			pat = `\<\V` + body + `\m\>`
		}
	}
	if m.find.CaseSensitive() {
		pat = `\C` + pat
	} else {
		pat = `\c` + pat
	}
	return pat
}

// applyReplaceNext finds the next occurrence of `find` in the current buffer
// (wrapping from the cursor) and replaces just that one with `replace`. After
// the substitution the cursor is left on the replaced span so a follow-up
// Enter advances to the next match. Honors the findbar toggles (case /
// whole-word / regex) when constructing the pattern.
func (m *Model) applyReplaceNext(find, replace string) {
	if m.nvim == nil || find == "" {
		return
	}
	// Regex-mode off: the user typed a literal — escape it through \V (handled
	// by findbarPattern). Regex-mode on: caller-supplied pattern stays raw,
	// with only the :s delimiter (`/`) escaped.
	pat := m.findbarPattern(find)
	rep := vimReplaceEscape(replace)
	cmds := []string{
		fmt.Sprintf("let @/ = '%s'", strings.ReplaceAll(pat, "'", "''")),
		"set hlsearch",
		"silent! normal! n",
		fmt.Sprintf("silent! s/\\%%#%s/%s/", pat, rep),
	}
	for _, c := range cmds {
		_ = m.nvim.Command(c)
	}
}

// applyReplaceAll runs `:%s/pat/repl/g` on the current buffer. Empty `find`
// is a no-op so we don't accidentally drop the buffer's contents. Honors
// the findbar toggles when constructing the pattern.
func (m *Model) applyReplaceAll(find, replace string) {
	if m.nvim == nil || find == "" {
		return
	}
	pat := m.findbarPattern(find)
	rep := vimReplaceEscape(replace)
	_ = m.nvim.Command(fmt.Sprintf("silent! %%s/%s/%s/g", pat, rep))
}

func (m *Model) applySearch(q string) {
	if m.nvim == nil {
		return
	}
	if q == "" {
		_ = m.nvim.Command("nohlsearch")
		// Empty input means "no active search" — clear the indicator so
		// the slot renders blank rather than continuing to show a stale
		// "3/12" from the previous query.
		m.find.SetMatchCount("")
		return
	}
	pattern := q
	// Regex toggle off → treat the pattern as a literal substring. \V is
	// nvim's "very nomagic" mode where every metachar except \ becomes
	// literal; we still escape \ → \\ so users can search for a backslash.
	if !m.find.UseRegex() {
		pattern = `\V` + strings.ReplaceAll(pattern, `\`, `\\`)
	}
	// Whole-word toggle wraps the body in word-boundary anchors. \< and \>
	// work in both magic and \V "very nomagic" mode, so we add them after
	// the \V prefix is in place but before the case flag (which must come
	// at the very front).
	if m.find.WholeWord() {
		if m.find.UseRegex() {
			pattern = `\<` + pattern + `\>`
		} else {
			// In \V mode the whole literal sits between \V and the trailing
			// chars; wrap with \<…\> outside the literal block by exiting
			// \V via \m, then re-enter with a fresh \V on the body.
			body := strings.TrimPrefix(pattern, `\V`)
			pattern = `\<\V` + body + `\m\>`
		}
	}
	// Case toggle inline — \C forces sensitive, \c forces insensitive,
	// regardless of the user's 'ignorecase' / 'smartcase' settings.
	if m.find.CaseSensitive() {
		pattern = `\C` + pattern
	} else {
		pattern = `\c` + pattern
	}
	escaped := strings.ReplaceAll(pattern, "'", "''")
	_ = m.nvim.Command(fmt.Sprintf("silent! let @/ = '%s' | set hlsearch", escaped))
	m.find.SetMatchCount(m.fetchMatchCount())
}

// getNvimSelection returns the current visual selection (text only) when
// the buffer is in v / V / Ctrl-V mode, with newlines collapsed to spaces
// so the find input stays single-line. Returns "" when there's no
// selection or nvim isn't attached. The unnamed register is restored so
// this snoop doesn't clobber the user's last yank.
func (m *Model) getNvimSelection() string {
	if m.nvim == nil {
		return ""
	}
	s, err := m.nvim.EvalLuaString(`
local mode = vim.fn.mode()
if mode ~= 'v' and mode ~= 'V' and mode ~= '\22' then
  return ''
end
local saved = vim.fn.getreg('"')
local savedType = vim.fn.getregtype('"')
vim.cmd('noautocmd silent normal! y')
local sel = vim.fn.getreg('"')
vim.fn.setreg('"', saved, savedType)
sel = sel:gsub('\n', ' ')
sel = sel:gsub('^%s+', ''):gsub('%s+$', '')
return sel
`)
	if err != nil {
		return ""
	}
	return s
}

// fetchMatchCount runs vim.fn.searchcount() against the active search
// pattern and returns either "N/M" (current of total), "0" (no matches),
// or "" (nvim unavailable / Lua error). Synchronous: capped at 50 ms in
// Lua so a pathological regex on a huge buffer can't lag the UI loop.
func (m *Model) fetchMatchCount() string {
	if m.nvim == nil {
		return ""
	}
	out, err := m.nvim.EvalLuaString(`
		local ok, sc = pcall(vim.fn.searchcount, { maxcount = 9999, timeout = 50 })
		if not ok or type(sc) ~= 'table' then return '' end
		if (sc.total or 0) == 0 then return '0' end
		return tostring(sc.current or 0) .. '/' .. tostring(sc.total)
	`)
	if err != nil {
		return ""
	}
	return out
}

func toNvimBufs(in []nvimBuffer) []nvim.BufferInfo {
	out := make([]nvim.BufferInfo, len(in))
	for i, b := range in {
		out[i] = nvim.BufferInfo{
			ID:       b.ID,
			Path:     b.Path,
			Modified: b.Modified,
			Filetype: b.Filetype,
		}
	}
	return out
}

func (m Model) fetchStateCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	return func() tea.Msg {
		s, err := c.FetchState()
		if err != nil {
			return nil
		}
		bufs := make([]nvimBuffer, len(s.Buffers))
		for i, b := range s.Buffers {
			bufs[i] = nvimBuffer{ID: b.ID, Path: b.Path, Modified: b.Modified, Filetype: b.Filetype}
		}
		return StateMsg{
			Path:           s.Path,
			Dirty:          s.Dirty,
			Lang:           s.Lang,
			Bufs:           bufs,
			Active:         s.Active,
			Errors:         s.Errors,
			Warnings:       s.Warnings,
			CursorLine:     s.CursorLine,
			Cursors:        s.Cursors,
			LastReloadPath: s.LastReloadPath,
			LastReloadSeq:  s.LastReloadSeq,
			CurrentWin:     s.CurrentWin,
			Mode:           s.Mode,
			Diff:           s.Diff,
		}
	}
}

func (m Model) handleGlobalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Debug: log every key the app actually receives so when a binding
	// "doesn't work" we can tell from the log whether (a) the host
	// terminal swallowed the key (no entry), (b) Bubble Tea reported it
	// under a different name than we bound (entry shows the surprising
	// name), or (c) the action ran but failed (entry but no follow-up
	// toast). Cheap: just appends to errors.log via recordError.
	recordError("[key] " + msg.String())

	// Terminal pass-through: when the user is actively typing into the
	// integrated terminal (current nvim window IS our terminal split AND
	// nvim is in terminal-insert mode), shell-meaningful shortcuts MUST
	// reach the PTY instead of being claimed by termocode's keymap.
	// Without this, Ctrl+C inside the terminal triggers ActionCopy
	// instead of sending SIGINT to the running command, which breaks
	// every shell convention.
	//
	// We don't call nvim_input ourselves here — instead we route the
	// key through the editor model. The editor's translateKey pipeline
	// (internal/editor/keys.go) already maps Ctrl+letter, Tab,
	// Backspace, arrows, etc. to the right nvim_input keycodes, and
	// nvim's terminal-insert mode forwards them to the PTY. This keeps
	// us out of the business of duplicating that translation.
	//
	// We bypass dispatchToFocus on purpose: when m.inTerminal == true
	// the active nvim window IS the terminal split (regardless of what
	// m.focus, the termocode-side editor-vs-explorer flag, says), so
	// any nvim_input call lands in the PTY. Routing through the editor
	// model directly avoids an edge case where m.focus == FocusExplorer
	// would otherwise route the key into the explorer's keymap.
	//
	// shouldPassToTerminal explicitly excludes panel-management chords
	// (Ctrl+T, Ctrl+`, Ctrl+Shift+`, …) so the user can still toggle /
	// switch / close the panel from inside it. See shell.go for the
	// full pass-through set.

	// Terminal-emulator-style clipboard chords. Most host terminals bind
	// Ctrl+Shift+C to copy and Ctrl+Shift+V to paste; users expect the
	// SAME chords to behave that way inside termocode's integrated
	// terminal too. The shell itself ignores these (Ctrl+Shift+* are
	// emulator-level keystrokes), so we implement them at the termocode
	// layer when the terminal pane is the focus.
	//
	// This gate runs BEFORE the shouldPassToTerminal block on purpose:
	//   - Ctrl+Shift+V/C are intentionally NOT in the pass-through set
	//     (they wouldn't reach the shell as raw keystrokes anyway).
	//   - When inTerminal == false, this falls through to the normal
	//     keymap dispatch, where Ctrl+Shift+V is bound to the clipboard-
	//     history picker (ActionClipboardHistory) and Ctrl+Shift+C is
	//     unbound — the editor's regular Copy stays on plain Ctrl+C.
	if m.inTerminal {
		switch msg.String() {
		case "ctrl+shift+v":
			m.terminalPasteFromClipboard()
			return m, nil
		case "ctrl+shift+c":
			lines := m.terminalCopyToClipboard()
			if lines <= 0 {
				return m, nil
			}
			label := fmt.Sprintf("Copied %d lines from terminal", lines)
			if lines == 1 {
				label = "Copied 1 line from terminal"
			}
			var toastCmd tea.Cmd
			m.toast, toastCmd = m.toast.Push(toast.Info, label)
			return m, toastCmd
		}
	}

	if m.inTerminal && shouldPassToTerminal(msg) {
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		return m, cmd
	}

	// Welcome-screen keyboard navigation: when the start page is
	// rendered (no buffer, no terminal panel) we let arrow keys / Tab
	// move the Quick Actions cursor and Enter dispatch the focused
	// card. Falls through to normal handling otherwise.
	if m.welcomeShowing() {
		if newM, cmd, handled := m.handleWelcomeKey(msg); handled {
			return newM, cmd
		}
	}
	// Inline preferred-action hint dismissal: any keystroke (other than
	// the one that applies the preferred action) clears the cached hint,
	// so it doesn't linger as the user moves around or types. The apply
	// branch reads m.preferredCodeAction BEFORE this clear runs (we
	// detect that case below via matchedAction == ActionApplyPreferred...).
	matchedAction := m.keys.Match(msg)
	if m.preferredCodeAction != nil && matchedAction != keymap.ActionApplyPreferredCodeAction {
		m.preferredCodeAction = nil
	}
	switch matchedAction {
	case keymap.ActionQuit:
		if m.nvim != nil {
			_ = m.nvim.Close()
		}
		return m, tea.Quit
	case keymap.ActionSave:
		var toastCmd tea.Cmd
		if err := m.editor.Save(); err != nil {
			m.err = err.Error()
			m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Save failed", err.Error())
		} else if p := m.editor.Path(); p != "" {
			m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Saved", saveDetail(p))
		}
		return m, tea.Batch(fetchGitCmd(), toastCmd)
	case keymap.ActionSaveAll:
		var toastCmd tea.Cmd
		if m.nvim != nil {
			_ = m.nvim.Command(":wall")
			m.toast, toastCmd = m.toast.Push(toast.Info, "Saved all open files")
		}
		return m, tea.Batch(fetchGitCmd(), toastCmd)
	case keymap.ActionToggleExplorer:
		m.showExp = !m.showExp
		m.applyLayout()
		return m, nil
	case keymap.ActionFocusSwap:
		if m.focus == FocusExplorer {
			m.focus = FocusEditor
		} else {
			m.focus = FocusExplorer
		}
		return m, nil
	case keymap.ActionFocusExplorer:
		m.focus = FocusExplorer
		return m, nil
	case keymap.ActionFocusEditor:
		m.focus = FocusEditor
		return m, nil
	case keymap.ActionOpenShell:
		return m, openShellCmd()
	case keymap.ActionToggleTerminal:
		m.toggleTerminalPanel()
		m.applyLayout()
		// terminal-insert mode is started by the Lua chunk's
		// vim.schedule(startinsert). Don't double-fire from Go — the
		// extra 'i' lands as literal `i` in the shell once the first
		// startinsert succeeds (visible as `#ii` at the prompt).
		return m, nil
	case keymap.ActionTerminalNewTab:
		m.newTerminalTab()
		m.applyLayout()
		m.resizeTerminalSplit()
		return m, nil
	case keymap.ActionTerminalCloseTab:
		// Only act when the panel is open — fall through to the default
		// binding's no-op otherwise so other handlers can claim the chord
		// (e.g. nothing else binds Ctrl+Shift+W today, but if a future
		// binding does, this guard keeps the action panel-scoped).
		if m.termOpen && len(m.terminalTabs) > 0 {
			m.closeTerminalTab(m.terminalActiveTab)
			m.applyLayout()
			m.resizeTerminalSplit()
		}
		return m, nil
	case keymap.ActionTerminalNextTab:
		if m.termOpen {
			m.cycleTerminalTab(1)
			return m, nil
		}
		// Panel closed — fall through to ActionNextBuffer behaviour so
		// Ctrl+Shift+PgDown still cycles editor buffers when there's no
		// terminal context.
		if m.nvim != nil {
			_ = m.nvim.Command("bnext")
		}
		return m, nil
	case keymap.ActionTerminalPrevTab:
		if m.termOpen {
			m.cycleTerminalTab(-1)
			return m, nil
		}
		if m.nvim != nil {
			_ = m.nvim.Command("bprevious")
		}
		return m, nil
	case keymap.ActionCloseBuffer:
		if m.activeBuf != 0 && m.promptCloseBufferIfDirty(m.activeBuf) {
			return m, nil
		}
		if buf, ok := m.findBuffer(m.activeBuf); ok {
			m.pushClosed(buf.Path)
		}
		if m.nvim != nil {
			_ = m.nvim.Command("bdelete")
		}
		return m, nil
	case keymap.ActionNextBuffer:
		if m.nvim != nil {
			_ = m.nvim.Command("bnext")
		}
		return m, nil
	case keymap.ActionPrevBuffer:
		if m.nvim != nil {
			_ = m.nvim.Command("bprevious")
		}
		return m, nil
	case keymap.ActionQuickOpen:
		m.picker = picker.NewWith(" Go to file ", func() []picker.Item {
			return loadFiles()
		})
		m.picker.SetSize(m.w, m.h)
		m.pickerOpen = true
		m.pickerKind = pickerKindFiles
		return m, m.picker.Init()
	case keymap.ActionCommandPalette:
		// Splice user-defined commands at the bottom so they're visible but
		// don't push built-ins down the fuzzy-match list.
		items := paletteItems()
		items = append(items, userCommandsItems()...)
		// Promote frequently-used commands to the front so muscle memory
		// works: type a few letters and the right command is already
		// near the cursor.
		items = promoteRecents(items, loadPaletteRecents())
		m.picker = picker.NewItems(" Command Palette ", items)
		m.picker.SetSize(m.w, m.h)
		m.pickerOpen = true
		m.pickerKind = pickerKindPalette
		return m, m.picker.Init()
	case keymap.ActionFind:
		// Re-open: keep an existing findbar's toggle state if it's already
		// open (just collapse the replace row); otherwise start fresh and
		// prefill from the current visual selection (VS-Code-style — if
		// the user has text selected, it lands in the search field).
		var cmd tea.Cmd
		if !m.findOpen {
			m.find = findbar.New()
			if sel := m.getNvimSelection(); sel != "" {
				m.find.SetQuery(sel)
				m.applySearch(sel)
			}
			m.findOpen = true
			cmd = m.find.Init()
		}
		m.find.EnableReplace(false)
		m.applyLayout()
		return m, cmd
	case keymap.ActionReplaceInFile:
		// Ctrl+H opens the unified findbar in expanded (Replace) mode and,
		// like Ctrl+F, seeds the find input with whatever's currently
		// selected so the user can jump straight to typing the
		// replacement. The legacy `internal/replacebar` package is still
		// intact but no longer instantiated from here.
		var cmd tea.Cmd
		if !m.findOpen {
			m.find = findbar.New()
			if sel := m.getNvimSelection(); sel != "" {
				m.find.SetQuery(sel)
				m.applySearch(sel)
			}
			m.findOpen = true
			cmd = m.find.Init()
		}
		m.find.EnableReplace(true)
		m.applyLayout()
		return m, cmd
	case keymap.ActionDuplicateLine:
		if m.nvim != nil {
			_ = m.nvim.Input("<M-S-Down>")
		}
		return m, nil
	case keymap.ActionToggleComment:
		m.toggleLineComment()
		return m, nil
	case keymap.ActionCopy:
		m.editorCopy()
		return m, nil
	case keymap.ActionPaste:
		m.editorPaste()
		return m, nil
	case keymap.ActionCut:
		m.editorCut()
		return m, nil
	case keymap.ActionCodeActions:
		return m, m.fetchCodeActionsCmd()
	case keymap.ActionApplyPreferredCodeAction:
		// If we have a cached preferred action from a recent fetch, fire
		// it directly. Otherwise fall back to opening the picker (same
		// flow as Ctrl+.) so the user still gets to act.
		if m.preferredCodeAction != nil {
			return m, m.applyPreferredCodeActionFromCache()
		}
		return m, m.fetchCodeActionsCmd()
	case keymap.ActionGotoSymbolInFile:
		return m, m.fetchSymbolsCmd()
	case keymap.ActionToggleBookmark:
		return m, m.toggleBookmark()
	case keymap.ActionShowBookmarks:
		return m, m.fetchBookmarksCmd()
	case keymap.ActionGotoLine:
		m.openGotoLinePrompt()
		return m, nil
	case keymap.ActionSplitVertical:
		if m.nvim != nil {
			_ = m.nvim.Command("vsplit")
		}
		return m, nil
	case keymap.ActionSplitHorizontal:
		if m.nvim != nil {
			_ = m.nvim.Command("split")
		}
		return m, nil
	case keymap.ActionCloseSplit:
		if m.nvim != nil {
			// `:close` shuts the current window without touching the buffer
			// (so the file stays open in the buffer list). `:quit` would
			// also work but :close is mode-safer.
			_ = m.nvim.Command("close")
		}
		return m, nil
	case keymap.ActionFormatDocument:
		return m, m.formatDocumentCmd()
	case keymap.ActionOpenRecent:
		return m, m.openRecentFilePicker()
	case keymap.ActionToggleWordWrap:
		return m, m.toggleWordWrap()
	case keymap.ActionAlternateFile:
		m.alternateFile()
		return m, nil
	case keymap.ActionNextDiagnostic:
		m.gotoNextDiagnostic()
		return m, nil
	case keymap.ActionPrevDiagnostic:
		m.gotoPrevDiagnostic()
		return m, nil
	case keymap.ActionToggleHidden:
		return m, m.toggleHiddenFiles()
	case keymap.ActionShowCheatSheet:
		m.openCheatSheet()
		return m, nil
	case keymap.ActionHover:
		return m, m.hoverCmd()
	case keymap.ActionFindReferences:
		return m, m.fetchReferencesCmd()
	case keymap.ActionReplaceInWorkspace:
		m.openReplaceInWorkspacePrompt()
		return m, nil
	case keymap.ActionGotoTypeDef:
		return m, m.gotoTypeDefCmd()
	case keymap.ActionGotoImplementation:
		return m, m.gotoImplementationCmd()
	case keymap.ActionRunTests:
		return m, m.runTestsCmd()
	case keymap.ActionGitBlameLine:
		return m, m.gitBlameCurrentLine()
	case keymap.ActionFileHistory:
		return m, m.gitFileHistoryPicker()
	case keymap.ActionToggleZenMode:
		return m, m.toggleZenMode()
	case keymap.ActionReloadWindow:
		return m, m.reloadWindow()
	case keymap.ActionToggleAutoSave:
		return m, m.toggleAutoSave()
	case keymap.ActionNextHunk:
		return m, m.gotoNextHunk()
	case keymap.ActionPrevHunk:
		return m, m.gotoPrevHunk()
	case keymap.ActionReloadBuffer:
		return m, m.reloadBuffer()
	case keymap.ActionOpenURL:
		return m, m.openURLUnderCursor()
	case keymap.ActionPinTab:
		return m, m.togglePinActiveTab()
	case keymap.ActionCopyFileRef:
		return m, m.copyFileLineRef()
	case keymap.ActionShowSnippetPicker:
		return m, m.openSnippetPicker()
	case keymap.ActionMarkdownPreview:
		if cmd := m.openPreviewCmd(); cmd != nil {
			return m, cmd
		}
		return m, nil
	case keymap.ActionWorkspaceSearch:
		m.openFindInFilesPrompt()
		return m, nil
	case keymap.ActionRevealFile:
		m.revealActiveFileInExplorer()
		return m, nil
	case keymap.ActionReopenClosed:
		return m, m.reopenClosed()
	case keymap.ActionClipboardHistory:
		return m, m.openClipboardPicker()
	case keymap.ActionToggleInlayHints:
		m.toggleInlayHints()
		return m, nil
	case keymap.ActionPickTheme:
		return m, m.openThemePicker()
	case keymap.ActionDebugToggleBreakpoint:
		return m, m.dapToggleBreakpoint()
	case keymap.ActionDebugStepOver:
		return m, m.dapStepOver()
	case keymap.ActionDebugStepInto:
		return m, m.dapStepInto()
	case keymap.ActionDebugStepOut:
		return m, m.dapStepOut()
	case keymap.ActionShowBufferInfo:
		return m, m.openBufferInfo()
	case keymap.ActionToggleActionsPanel:
		m.toggleActionsPanel()
		m.applyLayout()
		return m, nil
	}
	return m.dispatchToFocus(msg)
}

// toggleInlayHints flips the LSP inlay-hint display via the appropriate API
// for the running Neovim version. Authoritative state lives in nvim
// (vim.lsp.inlay_hint); m.inlayHintsOn is a UI mirror so callers can show
// the current state if needed. No-op on Neovim < 0.10 (pcall swallows the
// missing API call) and when nvim isn't attached.
func (m *Model) toggleInlayHints() {
	if m.nvim == nil {
		return
	}
	m.inlayHintsOn = !m.inlayHintsOn
	// The Lua snippet is self-contained: it asks vim.lsp.inlay_hint whether
	// it's currently on, then flips it. Keeping the toggle decision inside
	// Lua avoids races with the actual nvim state if our mirror drifts.
	const toggleLua = `
local ih = vim.lsp.inlay_hint
if ih == nil then
  if vim.lsp.buf and vim.lsp.buf.inlay_hint then
    pcall(vim.lsp.buf.inlay_hint, 0, nil)
  end
  return
end
local on = false
if ih.is_enabled then
  local ok, val = pcall(ih.is_enabled)
  if ok then on = val and true or false end
end
if ih.enable then
  pcall(ih.enable, not on)
end
`
	_ = m.nvim.ExecLua(toggleLua)
}

// editorCopy yanks the active selection (any visual flavour) or the current
// line (otherwise) into nvim's `+` register, then ALSO pushes the
// resulting text into the host OS clipboard via golang.design/x/clipboard.
// The Go-side push is what makes the copy visible to other apps — nvim's
// `+` register relies on a clipboard provider (xclip / xsel / wl-copy)
// being installed, which isn't guaranteed on every box. Writing from Go
// uses the OS APIs directly and works without external binaries.
func (m *Model) editorCopy() {
	if m.nvim == nil {
		return
	}
	out, err := m.nvim.EvalLuaString(`
local mode = vim.api.nvim_get_mode().mode
local m1 = mode:sub(1,1)
local visual = m1 == 'v' or m1 == 'V' or mode:byte(1) == 22
if visual then
  vim.cmd('normal! "+y')
else
  vim.cmd('normal! "+yy')
end
vim.cmd('startinsert')
return vim.fn.getreg('+')
`)
	if err == nil && out != "" {
		safeClipboardWrite(out)
		// Also emit OSC 52 so the user's *local* clipboard (their
		// laptop) gets the text when termocode is running over SSH.
		// safeClipboardWrite alone only reaches the server clipboard.
		emitOSC52(out)
	}
}

// editorCut deletes the selection / current line into the `+` register and
// returns the user to Insert mode. Also pushes the cut text to the host
// OS clipboard from Go so the copy is visible outside nvim even when
// xclip / xsel / wl-copy aren't installed.
func (m *Model) editorCut() {
	if m.nvim == nil {
		return
	}
	out, err := m.nvim.EvalLuaString(`
local mode = vim.api.nvim_get_mode().mode
local m1 = mode:sub(1,1)
local visual = m1 == 'v' or m1 == 'V' or mode:byte(1) == 22
if visual then
  vim.cmd('normal! "+d')
else
  vim.cmd('normal! "+dd')
end
vim.cmd('startinsert')
return vim.fn.getreg('+')
`)
	if err == nil && out != "" {
		safeClipboardWrite(out)
		// See editorCopy: OSC 52 is what reaches the local clipboard
		// over SSH; safeClipboardWrite is the local-build fallback.
		emitOSC52(out)
	}
}

// safeClipboardWrite pushes `s` to the OS clipboard, swallowing panics
// from golang.design/x/clipboard — the library hard-panics when built
// with CGO_ENABLED=0 (its native backend is unavailable). In that case
// we fall back to nvim's own `+` register clipboard provider, which the
// caller already populated via `"+y` / `"+d` before calling here.
func safeClipboardWrite(s string) {
	defer func() {
		_ = recover()
	}()
	clipboard.Write(clipboard.FmtText, []byte(s))
}

// editorPaste replaces the selection (or inserts at cursor) with the
// system-clipboard contents. Uses nvim_paste which is the API designed
// for arbitrary-mode text injection — works correctly from Insert and
// from Visual without needing the <C-O> trampoline that caused the
// `"+p` literal-text bug.
func (m *Model) editorPaste() {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.ExecLua(`
local mode = vim.api.nvim_get_mode().mode
local m1 = mode:sub(1,1)
local visual = m1 == 'v' or m1 == 'V' or mode:byte(1) == 22
if visual then
  -- delete the selection into the black-hole register so the paste
  -- below uses the clipboard contents, not the just-deleted text.
  vim.cmd('normal! "_d')
end
local clip = vim.fn.getreg('+')
if clip ~= '' then
  vim.api.nvim_paste(clip, true, -1)
end
vim.cmd('startinsert')
`)
}

// toggleLineComment toggles the comment on the current line / selection.
// nvim 0.10+ ships `gc` / `gcc` built-in; nvim 0.9 doesn't have them, so
// we fall back to our own Lua helpers (_toggle_comment_line and
// _toggle_comment_visual) defined in lsp_lua.go. The Lua helpers read
// commentstring per filetype so Go / JS / Python / etc. all work
// without any per-language plumbing on the Go side.
func (m *Model) toggleLineComment() {
	if m.nvim == nil {
		return
	}
	switch m.editorMode {
	case "v", "V", "visual", "vs", "Vs":
		// Use the Lua-defined helper which exits visual mode, applies
		// comments to the saved range, and restores the selection.
		_ = m.nvim.ExecLua(`
			if _G._toggle_comment_visual then _G._toggle_comment_visual()
			else pcall(vim.cmd, 'normal gc') end
		`)
	default:
		_ = m.nvim.ExecLua(`
			if _G._toggle_comment_line then _G._toggle_comment_line()
			else pcall(vim.cmd, 'normal gcc') end
		`)
	}
}

// reopenClosed pops the most recently closed editor's path off the ring and
// re-opens it via nvim. Toast feedback in every branch so the user knows
// what happened — silent returns made it look like the action hung.
func (m *Model) reopenClosed() tea.Cmd {
	path, ok := m.popClosed()
	if !ok {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No recently closed editors")
		return toastCmd
	}
	if m.nvim == nil {
		return nil
	}
	// Pass the path through nvim's own fnameescape so spaces / special
	// chars survive the :edit parser without relying on shell-style
	// quoting (which nvim's :edit would treat as literal characters in
	// the filename, opening a file whose name actually contains the
	// quotes — that's the "wrong content" the user saw).
	m.ensureEditorWindowCurrent()
	_ = m.nvim.Command(fmt.Sprintf(`execute 'edit ' . fnameescape(%q)`, path))
	m.focus = FocusEditor
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Reopened", filepath.Base(path))
	return toastCmd
}

// revealActiveFileInExplorer expands the explorer's tree as needed to show the
// currently-active editor's file, focuses the explorer pane, and ensures the
// sidebar is visible. No-ops silently if the editor has no active path.
func (m *Model) revealActiveFileInExplorer() {
	path := m.editor.Path()
	if path == "" {
		return
	}
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	m.explorer.RevealPath(path)
	m.focus = FocusExplorer
}

func (m *Model) handlePickerSelect(msg picker.SelectMsg) tea.Cmd {
	switch m.pickerKind {
	case pickerKindFiles:
		if !isSupportedFile(msg.ID) {
			var c tea.Cmd
			t, d := unsupportedFileMessage(msg.ID)
			m.toast, c = m.toast.PushDetail(toast.Warn, t, d)
			return c
		}
		if m.nvim != nil {
			m.ensureEditorWindowCurrent()
			_ = m.nvim.Command("edit " + msg.ID)
		}
		m.explorer.RevealPath(msg.ID)
		m.focus = FocusEditor
	case pickerKindPalette:
		// dispatch action by ID
		return m.dispatchPaletteAction(msg.ID)
	case pickerKindTheme:
		m.applyTheme(msg.ID)
	case pickerKindClipboard:
		// msg.ID looks like "clip-N" where N is the index into the ring.
		var idx int
		if _, err := fmt.Sscanf(msg.ID, "clip-%d", &idx); err == nil {
			items := m.clipboardPickerItems()
			if idx >= 0 && idx < len(items) {
				return m.pasteClipboardEntry(items[idx].Content)
			}
		}
	case pickerKindProblems:
		m.jumpToProblem(msg.ID)
	case pickerKindCodeAction:
		return m.applySelectedCodeAction(msg.ID)
	case pickerKindSymbols:
		m.jumpToSymbol(msg.ID)
	case pickerKindWorkspaceSymbols:
		m.jumpToWorkspaceSymbol(msg.ID)
	case pickerKindBranches:
		return m.gitCheckoutSelected(msg.ID)
	case pickerKindRecentFiles:
		if !isSupportedFile(msg.ID) {
			var c tea.Cmd
			t, d := unsupportedFileMessage(msg.ID)
			m.toast, c = m.toast.PushDetail(toast.Warn, t, d)
			return c
		}
		if m.nvim != nil {
			m.ensureEditorWindowCurrent()
			_ = m.nvim.Command("edit " + msg.ID)
		}
		m.explorer.RevealPath(msg.ID)
		m.focus = FocusEditor
	case pickerKindWorkspaces:
		return m.openWorkspace(msg.ID)
	case pickerKindStash:
		return m.applyStashSelected(msg.ID)
	case pickerKindDiffLeft:
		return m.onDiffLeftPicked(msg.ID)
	case pickerKindDiffRight:
		return m.onDiffRightPicked(msg.ID)
	case pickerKindGitLog:
		return m.showCommitFromPicker(msg.ID)
	case pickerKindTags:
		return m.checkoutTagSelected(msg.ID)
	case pickerKindSnippet:
		return m.insertSnippetBody(msg.ID)
	case pickerKindRemoveRoot:
		return m.removeRootFromPicker(msg.ID)
	case pickerKindSettings:
		return m.onSettingsRowSelected(msg.ID)
	case pickerKindThemeEditor:
		return m.onThemeEditorRowSelected(msg.ID)
	case pickerKindKeybinding:
		return m.onKeybindingRowSelected(msg.ID)
	}
	return nil
}

func (m Model) dispatchToFocus(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focus {
	case FocusExplorer:
		// The "explorer" focus actually means "the sidebar panel": route
		// keys to whichever pane the activity bar currently shows.
		switch m.activity.Active() {
		case activity.ViewGit:
			// Git sidebar has its own keymap (s/d/c/x/r + j/k/g/G/Enter).
			// Route key events through it; non-key events fall through to
			// the explorer Update so mouse / size events still apply.
			if k, ok := msg.(tea.KeyMsg); ok {
				newM, gcmd, _ := m.handleGitSidebarKey(k)
				return newM, gcmd
			}
			m.explorer, cmd = m.explorer.Update(msg)
		default:
			// Files pane: intercept the Delete / `d` chord here so the
			// confirm modal opens with the multi-selection (or the
			// cursor row as a fallback). Anything else flows into the
			// explorer's own Update for cursor motion, expand/collapse,
			// shift+arrow range select, ctrl+space toggle, etc.
			if k, ok := msg.(tea.KeyMsg); ok {
				switch k.String() {
				case "delete", "d":
					targets := m.explorer.Selection()
					if len(targets) == 0 {
						if p := m.explorer.CursorPath(); p != "" {
							targets = []string{p}
						}
					}
					if len(targets) > 0 {
						m.openBulkDeleteConfirm(targets)
					}
					return m, nil
				}
			}
			m.explorer, cmd = m.explorer.Update(msg)
			// Tree expansion may have changed (Enter/click toggles a dir);
			// persist so a relaunch can restore the same shape.
			m.persistExplorerState()
		}
	case FocusEditor:
		m.editor, cmd = m.editor.Update(msg)
	}
	return m, cmd
}

// persistExplorerState writes a fresh snapshot of the explorer tree's
// expanded directories to the session file. Called whenever the explorer
// might have changed shape (key toggle, click toggle).
func (m *Model) persistExplorerState() {
	existing := loadSession()
	existing.ExpandedDirs = m.explorer.ExpandedPaths()
	saveSession(existing)
}

func (m *Model) applyLayout() {
	// Zen mode collapses the layout to just the editor — full width, full
	// height, no chrome. Skip the rest of the math entirely.
	if m.zenMode {
		m.editor.SetSize(m.w, m.h)
		if m.nvim != nil {
			_ = m.nvim.Resize(m.w, m.h)
		}
		return
	}
	bodyH := m.h - 1
	if bodyH < 1 {
		bodyH = 1
	}

	// Activity bar now spans the full screen height — the status bar
	// has been confined to the editor column, so the activity column's
	// background runs all the way to the bottom edge instead of stopping
	// one row short of it.
	m.activity.SetHeight(m.h)

	// Migrate uninitialized models (created via struct literal in tests, etc.)
	// to the default explorer width so layout math doesn't divide by zero.
	if m.explorerWidth <= 0 {
		m.explorerWidth = defaultExplorerWidth
	}

	// Editor pane width = total - activity bar - sidebar (if shown) -
	// scrollbar - right-side actions column (if pinned & open).
	actionsColW := m.actionsColumnWidth()
	edPaneW := m.w - activity.Width - editorScrollbarWidth - actionsColW
	if m.showExp {
		edPaneW -= m.explorerWidth
	}
	if edPaneW < 1 {
		edPaneW = 1
	}

	chromeRows := m.tabs.Height() // tab bar (2 rows)
	if m.replaceOpen {
		chromeRows += 2 // replacebar is two rows tall
	}
	// Find bar is now a floating overlay (see overlayFindBarInEditor) — it
	// no longer takes a chrome row.
	// Breadcrumbs + signature share one row (signature is appended inline).
	if len(m.breadcrumbs) > 0 || m.stickyContext != "" {
		chromeRows++
	}
	editorH := bodyH - chromeRows
	if editorH < 1 {
		editorH = 1
	}
	// Integrated terminal tab bar: the bar lives INSIDE the terminal panel
	// (it's the topmost row of the panel and doubles as the drag-resize
	// handle — see spliceTerminalTabBar). nvim renders the full editorH
	// rows; the splice overpaints the topmost row of nvim's terminal split
	// with the bar. So no extra row is stolen from nvim's grid.
	//
	// Minimized mode: terminal split is squashed to 1 row, which the splice
	// also overpaints with the bar; the whole panel collapses to a single
	// row that's still grabbable for un-minimize.
	nvimEditorH := editorH
	// Tab bar shares the editor pane's right edge with the ⋮ overflow
	// glyph. Reserve those cells BEFORE handing the width to the tab bar
	// so its clamp / scroll math (and its right-edge ▶ overflow indicator)
	// don't collide with the glyph the renderer overpaints later. Without
	// this reservation the glyph stomps the rightmost tab cell and — when
	// many tabs overflow — the indicator math fights with the overpaint
	// and the strip ends up looking blank.
	tabsW := edPaneW - overflowMenuReservedCells
	if tabsW < 1 {
		tabsW = 1
	}
	m.tabs.SetWidth(tabsW)
	m.find.SetWidth(edPaneW)
	m.replace.SetWidth(edPaneW)

	// Mirror overlayFindBarInEditor's splice math so the find bar's stored
	// anchor matches where it actually renders. HandleMouse consumes this
	// via Bounds() to do screen-coord hit-testing on the ↑ / ↓ / × glyphs.
	{
		editorStart := m.w - edPaneW - actionsColW
		topRow := 0
		if m.replaceOpen {
			topRow += 2
		}
		topRow += m.tabs.Height()
		if len(m.breadcrumbs) > 0 || m.stickyContext != "" {
			topRow++
		}
		topRow++ // hover 1 row down from editor body top
		const rightInset = 2
		leftCol := editorStart + edPaneW - m.find.PanelWidth() - rightInset
		if leftCol < editorStart+1 {
			leftCol = editorStart + 1
		}
		m.find.SetAnchor(leftCol, topRow)
	}

	// Sidebar reserves 1 row for the header (only for non-Git views; Git's
	// sidebar already renders its own header row). The sidebar now runs
	// the full screen height (the status bar no longer slots under it),
	// so the body height is m.h - 1 instead of bodyH - 1.
	sidebarH := m.h - 1
	if sidebarH < 1 {
		sidebarH = 1
	}
	// reserve 1 col on the right for the sidebar's right-edge divider.
	// Explorer renders its own header+footer chrome and so takes the full
	// screen height (m.h).
	m.explorer.SetSize(m.explorerWidth-1, m.h)
	m.editor.SetSize(edPaneW, nvimEditorH)
	// Status bar is confined to the editor column now (renderBase sets
	// the width per-frame too, but seeding it here keeps any synchronous
	// View() callers — e.g. tests — from rendering an over-wide bar).
	m.status.SetWidth(edPaneW)
	m.picker.SetSize(m.w, m.h)
	m.recents.SetSize(m.w, m.h)

	if m.nvim != nil && m.nvimAttached && edPaneW > 0 && nvimEditorH > 0 {
		_ = m.nvim.Resize(edPaneW, nvimEditorH)
	}
	// Push the panel height into the live nvim split so a stale split
	// shrinks/grows to match m.terminalRows after a window resize, theme
	// switch, etc. No-op when the panel isn't open or the value matches
	// the last sent (resizeTerminalSplit caches).
	m.resizeTerminalSplit()
}

// refreshBreadcrumbs derives the breadcrumb segments from the editor's path.
// We strip the project root prefix when possible so the breadcrumbs read like
// "internal › app › view.go" rather than "/home/.../termocode/internal/app/...".
// Symbol-level segments (e.g. the enclosing function name) are deliberately
// NOT appended here in v1 — that would require a synchronous LSP request on
// every cursor-move StateMsg. The sticky-scroll strip carries the same
// information for now, and a follow-up can append the symbol once we have
// it cached on the model side.
//
// An empty path produces an empty slice, which suppresses the strip entirely.
func (m *Model) refreshBreadcrumbs(path string) {
	prev := m.breadcrumbs
	m.breadcrumbs = m.breadcrumbs[:0]
	// In the side-by-side diff each pane's winbar already labels its file, so
	// the breadcrumb/sticky strip is just a redundant third header row — hide
	// it (and the sticky context) for the whole diff view.
	if m.gitDiffActive {
		hadSticky := m.stickyContext != ""
		m.stickyContext = ""
		if len(prev) > 0 || hadSticky {
			m.applyLayout()
		}
		return
	}
	if path == "" {
		if len(prev) > 0 {
			m.applyLayout()
		}
		return
	}
	// Make the path repository-relative when the file lives under cwd.
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = rel
		}
	}
	for _, seg := range strings.Split(path, string(filepath.Separator)) {
		if seg == "" || seg == "." {
			continue
		}
		m.breadcrumbs = append(m.breadcrumbs, seg)
	}
	// applyLayout is needed when we toggle the strip's visibility so the
	// editor pane height stays consistent.
	if (len(prev) == 0) != (len(m.breadcrumbs) == 0) {
		m.applyLayout()
	}
}

// scrollbarDebounce throttles diagnostic+linecount Lua queries fed into
// the overview ruler. 800ms is fast enough that markers feel live but
// won't compete with the editor's typing input.
const scrollbarDebounce = 800 * time.Millisecond

// maybeRefreshScrollbarMarkers fires off a single Lua call to gather
// per-line diagnostic positions plus total line count for the active
// buffer. Returns nil when the throttle window hasn't elapsed.
func (m *Model) maybeRefreshScrollbarMarkers() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	now := time.Now()
	if now.Sub(m.scrollbarLastFetch) < scrollbarDebounce {
		return nil
	}
	m.scrollbarLastFetch = now
	c := m.nvim
	return func() tea.Msg {
		marks, total, err := c.BufferDiagnosticsAndLines()
		if err != nil {
			return nil
		}
		return scrollbarMarkersMsg{Marks: marks, TotalLines: total}
	}
}

// stickyDebounce throttles sticky-context Lua queries. StateMsg fires on
// every cursor move / keystroke — without a throttle we'd hammer nvim with
// a synchronous Lua roundtrip per keystroke and the editor feels laggy.
const stickyDebounce = 1500 * time.Millisecond

// maybeRefreshStickyContext fires off a Lua call to find the enclosing
// `func` or `class` line above the cursor. Returns nil when nvim isn't
// attached OR when the last refresh was within stickyDebounce. The reply
// is delivered as stickyContextMsg.
//
// Implementation note: vim.fn.search('^\\s*\\(func\\|class\\|def\\)\\>',
// 'bWnc') from the cursor position returns the line number of the most
// recent matching line ABOVE the cursor (W=no wrap, n=no move, c=accept
// match at cursor). Empty result → no enclosing symbol → strip hidden.
func (m *Model) maybeRefreshStickyContext() tea.Cmd {
	if m.nvim == nil || m.gitDiffActive {
		return nil
	}
	now := time.Now()
	if now.Sub(m.stickyLastFetch) < stickyDebounce {
		return nil
	}
	m.stickyLastFetch = now
	c := m.nvim
	return func() tea.Msg {
		s, err := c.EvalLuaString(`
			local pat = [[^\s*\(func\|class\|def\|interface\|type\|impl\|struct\)\>]]
			local lnum = vim.fn.search(pat, 'bWnc')
			if lnum == 0 then return '' end
			local line = vim.api.nvim_buf_get_lines(0, lnum-1, lnum, false)[1] or ''
			return line
		`)
		if err != nil {
			return stickyContextMsg{Signature: ""}
		}
		return stickyContextMsg{Signature: strings.TrimSpace(s)}
	}
}

// saveDetail returns the secondary line shown in the post-save toast:
// "<basename> — <N>L, <B>B written". Falls back to just the basename on
// stat / read error.
func saveDetail(path string) string {
	base := filepath.Base(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return base
	}
	bytes := len(data)
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if bytes > 0 && (bytes == 0 || data[bytes-1] != '\n') {
		lines++
	}
	return fmt.Sprintf("%s - %dL, %dB written", base, lines, bytes)
}
