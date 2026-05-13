package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/activity"
	"termocode/internal/keymap"
)

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.h <= 0 || m.w <= 0 {
		return m, nil
	}

	// Right-side Actions panel: click on the launcher chip re-opens (and
	// re-pins) the panel. The chip lives one row above the status bar
	// (msg.Y == m.h-2), so we have to handle it BEFORE the m.h-1 status-bar
	// guard below — same row priority wise as the editor body.
	if msg.Type == tea.MouseLeft && m.hitActionsLauncher(msg.X, msg.Y) {
		m.actionsPinned = true
		m.actionsOpen = true
		m.persistActionsState()
		m.applyLayout()
		return m, nil
	}

	// Overflow (⋮) menu: a left click on the chevron toggles the dropdown;
	// a click inside the open dropdown activates the targeted item; a
	// click anywhere else with the dropdown open closes it before the
	// click is dispatched to the underlying widget. Right-clicks fall
	// through (so the user can still right-click the editor while the
	// dropdown happens to be open — though typically they wouldn't).
	if msg.Type == tea.MouseLeft && m.hitOverflowGlyph(msg.X, msg.Y) {
		if m.overflowMenuOpen {
			m.overflowMenuOpen = false
		} else {
			m.openOverflowMenu()
		}
		return m, nil
	}
	if m.overflowMenuOpen && msg.Type == tea.MouseLeft {
		if m.hitOverflowMenuRect(msg.X, msg.Y) {
			if idx := m.overflowMenuItemRow(msg.Y); idx >= 0 {
				m.overflowMenuCursor = idx
				cmd := m.activateOverflowMenuItem()
				return m, cmd
			}
			// Click landed on the border row — keep the menu open but
			// absorb the click so it doesn't fall through.
			return m, nil
		}
		// Click-outside-close: drop the menu, then fall through so the
		// underlying widget still receives the click (better UX than
		// requiring a second click to interact with anything).
		m.overflowMenuOpen = false
	}

	if msg.Y >= m.h-1 {
		return m, nil // statusbar
	}

	// Right-side Actions panel header: hit-test pin/close glyphs FIRST so
	// the editor click handler doesn't swallow them. Drag-resize splitter
	// on the panel's LEFT edge starts a dragActionsLeft drag (mirrors the
	// explorer's right-edge splitter).
	if m.actionsPanelVisible() {
		if msg.Type == tea.MouseLeft {
			switch m.hitActionsButton(msg.X, msg.Y) {
			case "pin":
				// Toggle pinned: collapse to launcher chip, panel stays
				// "open" so the chip is shown.
				m.setActionsPinned(false)
				m.applyLayout()
				return m, nil
			case "close":
				m.closeActionsPanel()
				m.applyLayout()
				return m, nil
			}
		}
	}

	// Drag-resize splitter on the LEFT edge of the actions column.
	actionsLeftEdge := m.w - m.actionsColumnWidth()
	if m.dragKind == dragActionsLeft {
		switch msg.Type {
		case tea.MouseMotion, tea.MouseLeft:
			newW := m.w - msg.X
			if newW < actionsMinWidth {
				newW = actionsMinWidth
			}
			if max := m.w / 2; newW > max {
				newW = max
			}
			// Don't let the editor pane vanish: keep at least 10 cols on
			// the left of the splitter for the editor itself.
			if max := m.w - activity.Width - editorScrollbarWidth - 10; m.showExp {
				if other := max - m.explorerWidth; other > 0 && newW > other {
					newW = other
				}
			} else if newW > max {
				newW = max
			}
			if newW != m.actionsWidth {
				m.actionsWidth = newW
				m.applyLayout()
			}
			return m, nil
		default:
			m.dragKind = dragNone
			m.persistActionsState()
			return m, nil
		}
	}
	if m.actionsPanelVisible() && msg.Type == tea.MouseLeft && msg.X == actionsLeftEdge {
		m.dragKind = dragActionsLeft
		m.dragStartX = msg.X
		return m, nil
	}

	// Integrated terminal: a vertical drag on the tab-bar row resizes the
	// panel. Active drag must be checked BEFORE the press hit-test below so
	// MouseMotion / MouseRelease events are routed to the drag handler even
	// after the cursor leaves the tab-bar row.
	if m.dragKind == dragTerminalTop {
		switch msg.Type {
		case tea.MouseMotion, tea.MouseLeft:
			// We want the tab bar to land EXACTLY where the cursor is so
			// the user feels they're grabbing the bar. The bar's screen row
			// is `terminalTabBarRowAbsolute() = m.h - 2 - terminalRows`
			// (terminalRows = visible shell rows; bar sits one row above
			// the shell as the separator overpaint; status bar at m.h-1).
			// So solving for terminalRows when the bar should be at msg.Y:
			//   terminalRows = m.h - 2 - msg.Y
			// Dragging UP (smaller msg.Y) grows the panel — VSCode-style.
			newRows := (m.h - 2) - msg.Y
			// Snap-to-minimize: dragging the tab bar down past the min
			// height collapses the panel to just the bar (no shell content
			// visible). Spec calls for terminalRowsMin = 3, so anything
			// smaller flips to minimized.
			if newRows < terminalRowsMin {
				if !m.terminalMinimized {
					m.terminalMinimized = true
					m.applyLayout()
					m.resizeTerminalSplit()
				}
				return m, nil
			}
			// Coming back up: un-minimize.
			if m.terminalMinimized {
				m.terminalMinimized = false
			}
			// Leave at least 8 rows above the panel for editor + tabs +
			// status bar + breadcrumbs so the user can't drag the editor
			// out of existence.
			if max := m.h - 8; max > 0 && newRows > max {
				newRows = max
			}
			if newRows != m.terminalRows {
				m.terminalRows = newRows
				m.applyLayout()
				m.resizeTerminalSplit()
			}
			return m, nil
		default:
			// Any other event (release, wheel, right-click) ends the drag.
			// Persist once on release so the new height survives a relaunch.
			m.dragKind = dragNone
			m.persistTerminalRows()
			return m, nil
		}
	}

	// Integrated terminal tab bar: hit-test all click targets on the tab-bar
	// row (tabs, close-x per tab, "+", maximize, minimize, close-panel).
	// A press on any non-button cell of the row starts a vertical drag —
	// the entire row is the resize handle.
	if m.termOpen && msg.Type == tea.MouseLeft {
		hit, idx := m.terminalTabBarHitTest(msg.X, msg.Y)
		switch hit {
		case terminalHitTabClose:
			m.closeTerminalTab(idx)
			m.applyLayout()
			m.resizeTerminalSplit()
			return m, nil
		case terminalHitTabActivate:
			m.switchTerminalTab(idx)
			return m, nil
		case terminalHitNewTab:
			m.newTerminalTab()
			m.applyLayout()
			m.resizeTerminalSplit()
			return m, nil
		case terminalHitMaximize:
			m.maximizeTerminalPanel()
			return m, nil
		case terminalHitMinimize:
			m.minimizeTerminalPanel()
			return m, nil
		case terminalHitClosePanel:
			m.closeTerminalPanel()
			m.applyLayout()
			return m, nil
		case terminalHitDrag:
			m.dragKind = dragTerminalTop
			m.dragStartX = msg.X
			return m, nil
		}
	}

	// Find bar takes priority when open: it floats above the editor and
	// owns clicks that fall inside its panel rectangle (the ↑ / ↓ / ×
	// glyph buttons live there). Clicks elsewhere fall through to the
	// regular dispatch below so the user can still interact with tabs,
	// the explorer, etc. without having to dismiss the bar first.
	if m.findOpen {
		bx, by, bw, bh := m.find.Bounds()
		if bw > 0 && bh > 0 &&
			msg.X >= bx && msg.X < bx+bw &&
			msg.Y >= by && msg.Y < by+bh {
			var cmd tea.Cmd
			m.find, cmd = m.find.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
			return m, cmd
		}
	}

	// ── Drag splitter routing ───────────────────────────────────────────
	// The boundary column between the sidebar and the editor pane lives at
	// screen X = activity.Width + m.explorerWidth - 1. A left-click on that
	// column (when the sidebar is visible) starts a drag; subsequent
	// MouseMotion events update m.explorerWidth; release ends the drag.
	splitterX := activity.Width + m.explorerWidth - 1
	if m.dragKind == dragExplorerRight {
		switch msg.Type {
		case tea.MouseMotion, tea.MouseLeft:
			newW := msg.X - activity.Width + 1
			if newW < explorerMinWidth {
				newW = explorerMinWidth
			}
			if newW > explorerMaxWidth {
				newW = explorerMaxWidth
			}
			// Don't let the editor pane vanish: keep at least 10 cols on the
			// right (after subtracting the scrollbar) for the editor itself.
			if max := m.w - activity.Width - editorScrollbarWidth - 10; max > 0 && newW > max {
				newW = max
			}
			if newW != m.explorerWidth {
				m.explorerWidth = newW
				m.applyLayout()
			}
			return m, nil
		default:
			// Any other event (release, wheel, right-click, etc.) ends the drag.
			m.dragKind = dragNone
			return m, nil
		}
	}
	if m.showExp && msg.Type == tea.MouseLeft && msg.X == splitterX {
		m.dragKind = dragExplorerRight
		m.dragStartX = msg.X
		return m, nil
	}

	// Activity bar (leftmost activity.Width cols)
	if msg.X < activity.Width {
		var cmd tea.Cmd
		m.activity, cmd = m.activity.HandleMouse(msg.X, msg.Y, msg.Type)
		return m, cmd
	}

	// Coordinates from here on are relative to area right of activity bar
	x := msg.X - activity.Width

	// Sidebar pane (right of activity bar, left of editor)
	if m.showExp && x < m.explorerWidth {
		if msg.Type == tea.MouseLeft {
			m.focus = FocusExplorer
		}
		// Files & Search panes have a 1-row "EXPLORER: PROJECT" / "SEARCH"
		// header rendered above their content. Git renders its own header
		// inline so its mouse handler reads msg.Y directly.
		switch m.activity.Active() {
		case activity.ViewFiles:
			// Explorer now renders its own header + footer chrome and
			// expects panel-local Y. It internally subtracts its
			// HeaderChromeRows() / FooterChromeRows() so clicks on
			// chrome rows are no-ops.
			localY := msg.Y
			if msg.Type == tea.MouseRight {
				m.openExplorerMenu(msg.X, msg.Y, localY)
				return m, nil
			}
			var cmd tea.Cmd
			m.explorer, cmd = m.explorer.HandleMouse(x, localY, msg.Type, msg.Ctrl, msg.Shift)
			// A click may have toggled a directory — persist new state.
			m.persistExplorerState()
			return m, cmd
		case activity.ViewGit:
			return m.handleGitSidebarMouse(x, msg.Y, msg.Type)
		case activity.ViewSearch:
			// Header + placeholder; nothing interactive yet.
			return m, nil
		}
		return m, nil
	}
	// Right-side Actions panel body: when the click lands inside the
	// pinned panel column but not on a button (already handled above),
	// absorb it so it doesn't fall through to the editor.
	if m.actionsPanelVisible() && msg.X >= m.w-m.actionsColumnWidth() {
		return m, nil
	}

	// Editor region (right of explorer or full width)
	localX := x
	if m.showExp {
		localX = x - m.explorerWidth
	}

	// Compute the chrome layout above editor content, matching the order
	// in view.go::renderBase exactly:
	//   replacebar (2) → tabs (2) → breadcrumbs (1) → editor
	// (find bar is a floating overlay — it's not in the chrome stack)
	rowsAboveTabs := 0
	if m.replaceOpen {
		rowsAboveTabs += 2
	}
	tabsRow := rowsAboveTabs                    // first row of tab bar (accent strip)
	tabsBodyRow := tabsRow + m.tabs.Height() - 1 // last row of tab bar (body)
	editorTopRow := tabsBodyRow + 1
	// Breadcrumbs and sticky-context now share ONE row (signature inline).
	if len(m.breadcrumbs) > 0 || m.stickyContext != "" {
		editorTopRow++
	}

	if msg.Y < tabsRow {
		// click landed inside findbar / replacebar — no mouse handling yet
		return m, nil
	}
	if msg.Y >= tabsRow && msg.Y <= tabsBodyRow {
		// any tab-bar row (accent strip OR body) routes to tabbar
		var cmd tea.Cmd
		m.tabs, cmd = m.tabs.HandleMouse(localX, msg.X, msg.Y, msg.Type)
		return m, cmd
	}
	if msg.Y < editorTopRow {
		// breadcrumb / sticky-scroll row — no clickable elements now that
		// the Outline chip has been removed (the symbol picker is opened
		// via Ctrl+Shift+O / palette / ⋮ menu).
		return m, nil
	}
	if msg.Type == tea.MouseRight {
		m.openEditorMenu(msg.X, msg.Y)
		return m, nil
	}
	if msg.Type == tea.MouseLeft {
		m.focus = FocusEditor
	}
	localY := msg.Y - editorTopRow

	// Welcome screen: when no editor file is open we show the welcome
	// content in the editor pane. A click on a recent-files row should
	// open that file rather than fall through to the editor.
	if msg.Type == tea.MouseLeft && m.editor.Path() == "" && len(m.bufs) <= 1 && !m.termOpen {
		// Reproduce edPaneW + editorH the same way renderBase does so
		// the hit map matches what's actually on screen.
		bodyH := m.h - 1
		if bodyH < 1 {
			bodyH = 1
		}
		chromeRows := m.tabs.Height()
		if m.replaceOpen {
			chromeRows += 2
		}
		if len(m.breadcrumbs) > 0 || m.stickyContext != "" {
			chromeRows++
		}
		editorH := bodyH - chromeRows
		if editorH < 1 {
			editorH = 1
		}
		edPaneW := m.w - activity.Width - editorScrollbarWidth
		if m.showExp {
			edPaneW -= m.explorerWidth
		}
		if edPaneW < 1 {
			edPaneW = 1
		}
		// Quick-action cards: dispatch the same keymap action the
		// keyboard binding would fire. This goes first so a card click
		// doesn't accidentally fall through to a recents row underneath.
		// Also moves the keyboard focus to the clicked card so a
		// follow-up Enter / arrow press stays in sync with what the
		// user just pointed at.
		for _, hit := range welcomeQuickActionHits(edPaneW, editorH) {
			if localY != hit.Row {
				continue
			}
			if localX < hit.ColStart || localX >= hit.ColEnd {
				continue
			}
			actions := welcomeQuickActions()
			for i, a := range actions {
				if a.action == hit.Action {
					m.welcomeFocus = i
					break
				}
			}
			m.focus = FocusEditor
			return m.dispatchWelcomeAction(hit.Action)
		}
		for _, hit := range welcomeRecentHits(edPaneW, editorH) {
			if localY != hit.Row {
				continue
			}
			// ColEnd == 0 means full-row hit (stacked layout); otherwise
			// only react when the click landed inside the recents column.
			if hit.ColEnd > 0 && (localX < hit.ColStart || localX >= hit.ColEnd) {
				continue
			}
			// Sentinel: "View all →" row opens the dedicated recents
			// modal (Ctrl+R) instead of `:edit`-ing a file.
			if hit.Path == welcomeSentinelViewAll {
				m.focus = FocusEditor
				return m, m.openRecentFilePicker()
			}
			if m.nvim != nil {
				_ = m.nvim.Command("edit " + hit.Path)
			}
			m.focus = FocusEditor
			return m, nil
		}
		for _, hit := range welcomeWorkspaceHits(edPaneW, editorH) {
			if localY != hit.Row {
				continue
			}
			if hit.ColEnd > 0 && (localX < hit.ColStart || localX >= hit.ColEnd) {
				continue
			}
			// "browse all → " sentinel opens the dedicated workspaces
			// picker (same modal as "Workspaces: Open Recent" in the
			// command palette) instead of re-execing.
			if hit.Path == welcomeSentinelBrowseAll {
				m.focus = FocusEditor
				return m, m.openWorkspacePicker()
			}
			// Workspace click re-execs termocode in that folder; openWorkspace
			// returns a toast cmd on failure (success replaces the process).
			return m, m.openWorkspace(hit.Path)
		}
	}

	// Ctrl+left-click → VSCode-style "Go to Definition" of the symbol
	// under the click point. We first position nvim's cursor at the click
	// cell (synchronously, so the LSP call observes the new cursor), then
	// run the same definition Lua the right-click menu uses.
	//
	// Plain (no-modifier) left-click and Ctrl+drag continue through the
	// normal HandleMouse path below — Ctrl+drag isn't a defined gesture
	// here, so it just behaves like a regular drag.
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.Ctrl {
		return m, m.gotoDefinitionAtCell(localX, localY)
	}

	// When the terminal panel is open, nvim's grid is sized to the FULL
	// editor pane height (applyLayout sets nvimEditorH = editorH). The
	// integrated tab bar overpaints nvim's split-separator row at
	// editor-pane-local Y = editorH - integratedTerminalRows() - 1 — same
	// formula spliceTerminalTabBar uses, so they coincide on the same
	// physical row that's both the tab bar AND the drag-resize handle.
	// Clicks on that row are owned by terminalTabBarHitTest above (handled
	// before this block runs); a click here means a non-interactive cell
	// of the bar — drop it. Clicks BELOW the bar fall through into nvim's
	// terminal split at their natural Y; nvim's grid IS the editor pane,
	// so screen-pane-local Y == nvim grid Y with no translation needed.
	if m.termOpen {
		bodyH := m.h - 1
		if bodyH < 1 {
			bodyH = 1
		}
		chromeRows := m.tabs.Height()
		if m.replaceOpen {
			chromeRows += 2
		}
		if len(m.breadcrumbs) > 0 || m.stickyContext != "" {
			chromeRows++
		}
		editorH := bodyH - chromeRows
		if editorH < 1 {
			editorH = 1
		}
		barLocalY := editorH - m.integratedTerminalRows() - 1
		if barLocalY < 0 {
			barLocalY = 0
		}
		if localY == barLocalY {
			// Tab-bar row — terminalTabBarHitTest already handled every
			// button at the top of this function, so any click here is
			// on the non-interactive part. Drop it.
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.editor, cmd = m.editor.HandleMouse(localX, localY, msg.Type)
	return m, cmd
}

// handleWelcomeKey lets the Quick Actions row act as a keyboard menu
// while the welcome screen is showing: ←/→/Tab cycle focus, ↑/↓ mirror
// (since the card row may wrap to 2×2 on narrow widths, but cycling
// through 0..3 still reaches every card), and Enter dispatches the
// focused card's action. Returns handled=true when the key was
// consumed so handleGlobalKey skips its normal switch.
func (m Model) handleWelcomeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	actions := welcomeQuickActions()
	if len(actions) == 0 {
		return m, nil, false
	}
	switch msg.String() {
	case "left":
		m.welcomeFocus = (m.welcomeFocus - 1 + len(actions)) % len(actions)
		return m, nil, true
	case "right", "tab":
		m.welcomeFocus = (m.welcomeFocus + 1) % len(actions)
		return m, nil, true
	case "shift+tab":
		m.welcomeFocus = (m.welcomeFocus - 1 + len(actions)) % len(actions)
		return m, nil, true
	case "up":
		// On a single-row layout, map up/down to left/right so users
		// who reach for the vertical arrows still navigate. Wraps.
		m.welcomeFocus = (m.welcomeFocus - 1 + len(actions)) % len(actions)
		return m, nil, true
	case "down":
		m.welcomeFocus = (m.welcomeFocus + 1) % len(actions)
		return m, nil, true
	case "enter":
		idx := m.welcomeFocus
		if idx < 0 || idx >= len(actions) {
			idx = 0
		}
		newM, cmd := m.dispatchWelcomeAction(actions[idx].action)
		return newM, cmd, true
	}
	return m, nil, false
}

// dispatchWelcomeAction routes a Quick-Action card click to the same
// keymap.Action that the keyboard binding triggers. Synthesizing the
// matching tea.KeyMsg and feeding it through handleGlobalKey would risk
// drift if a binding ever changes, so we go straight to a synthetic
// KeyMsg that matches what handleGlobalKey expects via keys.Match.
func (m Model) dispatchWelcomeAction(action keymap.Action) (tea.Model, tea.Cmd) {
	// Pick a representative key string for the action that is in the
	// default keymap. handleGlobalKey calls keys.Match(msg) which only
	// looks at msg.String(), so a tea.KeyMsg with the right Type/Runes
	// is enough.
	var keyStr string
	switch action {
	case keymap.ActionQuickOpen:
		keyStr = "ctrl+p"
	case keymap.ActionCommandPalette:
		keyStr = "f1"
	case keymap.ActionWorkspaceSearch:
		keyStr = "f8"
	case keymap.ActionToggleTerminal:
		keyStr = "ctrl+t"
	case keymap.ActionOpenShell:
		keyStr = "ctrl+t"
	default:
		return m, nil
	}
	return m.handleGlobalKey(synthKey(keyStr))
}

// synthKey produces a tea.KeyMsg whose String() returns `s`. We can't
// rely on a struct-literal trick because tea.KeyMsg.String() goes
// through internal logic; the most reliable path is to special-case the
// short list of bindings the welcome screen actually emits.
func synthKey(s string) tea.KeyMsg {
	switch s {
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "ctrl+t":
		return tea.KeyMsg{Type: tea.KeyCtrlT}
	case "f1":
		return tea.KeyMsg{Type: tea.KeyF1}
	case "f8":
		return tea.KeyMsg{Type: tea.KeyF8}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// gotoDefinitionAtCell positions nvim's cursor at the editor-local click
// cell (localX, localY) using a synchronous mouse press+release pair —
// bypassing the async mouse dispatcher — so the LSP request observes
// the updated cursor — then runs the shared gotoDefinitionLua snippet.
//
// Synchronous press is important: MouseAsync queues onto a goroutine,
// while ExecLua calls into the nvim RPC directly, so async-press +
// immediate-Lua races and the LSP call may run against the OLD cursor.
func (m *Model) gotoDefinitionAtCell(localX, localY int) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.focus = FocusEditor
	_ = m.nvim.Mouse("left", "press", "", localY, localX)
	_ = m.nvim.Mouse("left", "release", "", localY, localX)
	_ = m.nvim.ExecLua(gotoDefinitionLua)
	return nil
}
