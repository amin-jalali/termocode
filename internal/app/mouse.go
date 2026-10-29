package app

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/lspinstall"
)

func (m Model) handleMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	if m.h <= 0 || m.w <= 0 {
		return m, nil
	}

	// Track the pointer cell for hover highlighting. Updated for every mouse
	// event (motion included); the renderers decide what to highlight. The
	// event still flows through to the click/drag handlers below.
	m.hoverX, m.hoverY = msg.X, msg.Y

	// A clickable toast ("Saved to …: Show in folder") sits on top of
	// everything below it.
	if c, ok := m.clickToast(msg); ok {
		return m, c
	}

	// Right-side Actions panel: click on the launcher chip re-opens (and
	// re-pins) the panel. The chip lives one row above the status bar
	// (msg.Y == m.h-2), so we have to handle it BEFORE the m.h-1 status-bar
	// guard below — same row priority wise as the editor body.
	if msg.Type == tea.MouseLeft && m.hitActionsLauncher(msg.X, msg.Y) {
		if m.probed("actions:launcher") {
			return m, nil
		}
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
		if m.probed("overflow:glyph") {
			return m, nil
		}
		if m.overflowMenuOpen {
			m.overflowMenuOpen = false
		} else {
			m.openOverflowMenu()
		}
		return m, nil
	}
	// Hover: move the overflow menu's highlight onto the row under the pointer.
	if m.overflowMenuOpen && msg.Type == tea.MouseMotion {
		if _, idx := m.overflowMenuHitAt(msg.X, msg.Y); idx >= 0 {
			m.overflowMenuCursor = idx
		}
		return m, nil
	}
	if m.overflowMenuOpen && msg.Type == tea.MouseLeft {
		if inBox, idx := m.overflowMenuHitAt(msg.X, msg.Y); inBox {
			if idx >= 0 {
				if m.probed("overflow:item:" + itoa(idx)) {
					return m, nil
				}
				m.overflowMenuCursor = idx
				cmd := m.activateOverflowMenuItem()
				return m, cmd
			}
			// Click landed on the border row — keep the menu open but
			// absorb the click so it doesn't fall through.
			m.probed("overflow:menu")
			return m, nil
		}
		// Click-outside-close: drop the menu, then fall through so the
		// underlying widget still receives the click (better UX than
		// requiring a second click to interact with anything).
		m.overflowMenuOpen = false
	}

	// Group A: AI chat panel body (wheel, clicks, input focus) — before the
	// status-bar guard because the panel spans the full height.
	if m.mouseProbe != nil {
		if id := m.aiPanelTargetAt(msg.X, msg.Y); id != "" {
			m.probed(id)
			return m, nil
		}
	} else if cmd, handled := m.handleAIPanelMouse(msg); handled {
		return m, cmd
	}

	// The status bar is the last row under the editor column only; the
	// activity bar and the sidebar run down to the bottom edge, so their
	// last row is theirs (e.g. the lower half of the Settings gear).
	if m.onStatusBar(msg.X, msg.Y) {
		if msg.Type == tea.MouseLeft && m.probed(m.statusHitAt(msg.X, msg.Y)) {
			return m, nil
		}
		// Group A: status-bar "✦ model" badge → configure / toggle chat.
		if msg.Type == tea.MouseLeft && m.hitStatusAIBadge(msg.X, msg.Y) {
			return m, m.onAIBadgeClick()
		}
		// Status-bar LSP chip → language-server manager.
		if msg.Type == tea.MouseLeft && m.hitStatusLSPChip(msg.X, msg.Y) {
			return m, m.openToolManager(lspinstall.CategoryLSP)
		}
		// Group C: error / warning counters → Problems panel.
		if msg.Type == tea.MouseLeft && m.hitStatusDiagCounts(msg.X, msg.Y) {
			return m, m.showProblemsPanel()
		}
		// Group E: status-bar test chip → Testing view.
		if msg.Type == tea.MouseLeft && m.hitStatusTestsChip(msg.X, msg.Y) {
			m.revealTestsView(true)
			return m, m.ensureTestsDiscovered()
		}
		if msg.Type == tea.MouseLeft && m.statusHitAt(msg.X, msg.Y) == "status:agents" {
			return m, m.revealItermocView(true, imSecAgents) // outside agents (Itermoc); its Status… row is the old screen
		}
		if msg.Type == tea.MouseLeft && m.statusHitAt(msg.X, msg.Y) == "status:services" {
			return m, m.revealServicesView(true)
		}
		if msg.Type == tea.MouseLeft && m.statusHitAt(msg.X, msg.Y) == "status:ci" { // G-CI
			return m, m.openCICard()
		}
		if msg.Type == tea.MouseLeft && m.statusHitAt(msg.X, msg.Y) == "status:update" { // G-UPD
			return m, m.openUpdateCard()
		}
		// Group I: extension status items.
		if msg.Type == tea.MouseLeft {
			if cmd, ok := m.hitExtStatusItem(msg.X, msg.Y); ok {
				return m, cmd
			}
		}
		if msg.Type == tea.MouseLeft && m.hitStatusDebugBadge(msg.X, msg.Y) { // Group D
			m.openRunView(runSecStack, true)
			return m, nil
		}
		return m, nil // statusbar
	}

	// Right-side Actions panel header: hit-test pin/close glyphs FIRST so
	// the editor click handler doesn't swallow them. Drag-resize splitter
	// on the panel's LEFT edge starts a dragActionsLeft drag (mirrors the
	// explorer's right-edge splitter).
	if m.actionsPanelVisible() {
		if msg.Type == tea.MouseLeft {
			if id := m.hitActionsButton(msg.X, msg.Y); id != "" && m.probed("actions:"+id) {
				return m, nil
			}
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
			if newW != m.actionsWidth || !m.actionsWidthSet {
				m.actionsWidth = newW
				m.actionsWidthSet = true
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
		if m.probed("actions:splitter") {
			return m, nil
		}
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
			pane := m.editorPaneLayout()
			newRows := pane.Top + pane.H - 1 - msg.Y
			// Snap-to-minimize: dragging the tab bar down past the min
			// height collapses the panel to just the bar (no shell content
			// visible). Spec calls for terminalRowsMin = 3, so anything
			// smaller flips to minimized.
			if newRows < terminalRowsMin {
				if !m.terminalMinimized {
					m.holdTerminalBar()
					m.terminalMinimized = true
					m.applyLayout()
					m.resizeTerminalSplit()
				}
				return m, nil
			}
			// Coming back up: un-minimize.
			wasMinimized := m.terminalMinimized
			if wasMinimized {
				m.holdTerminalBar()
				m.terminalMinimized = false
			}
			// Leave at least 8 rows above the panel for editor + tabs +
			// status bar + breadcrumbs so the user can't drag the editor
			// out of existence.
			if max := m.h - 8; max > 0 && newRows > max {
				newRows = max
			}
			if newRows != m.terminalRows || wasMinimized {
				m.holdTerminalBar()
				m.terminalRows = newRows
				m.applyLayout()
				m.resizeTerminalSplit()
			}
			return m, nil
		default:
			// Any other event (release, wheel, right-click) ends the drag.
			// Persist once on release so the new height survives a relaunch.
			m.dragKind = dragNone
			m.terminalBarHeld = false
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
		if id := panelBarTargetID(hit, idx); id != "" && m.probed(id) {
			return m, nil
		}
		switch hit {
		case terminalHitTabClose:
			m.closeTerminalTab(idx)
			m.applyLayout()
			m.resizeTerminalSplit()
			return m, nil
		case terminalHitTabActivate:
			m.switchTerminalTab(idx)
			return m, nil
		case terminalHitFixChip: // U9b
			return m, m.termFixWithAI(idx)
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
		// Non-terminal tabs (Output, Problems, …) — panel.go.
		case panelHitTabActivate:
			if idx >= 0 && idx < len(m.panelTabs) {
				m.activatePanelEntry(panelBarEntry{Kind: m.panelTabs[idx], Index: idx})
			}
			return m, nil
		case panelHitTabClose:
			if idx >= 0 && idx < len(m.panelTabs) {
				m.closePanelTab(m.panelTabs[idx])
				m.applyLayout()
				m.resizeTerminalSplit()
			}
			return m, nil
		}
	}
	// Content area of a non-terminal panel tab: Go owns it, so wheel and
	// clicks must never reach the hidden nvim split underneath (panel.go).
	if m.mouseProbe != nil && m.panelContentAt(msg.X, msg.Y) {
		m.probed("panel:content")
		return m, nil
	}
	if cmd, handled := m.routePanelContentMouse(msg); handled {
		return m, cmd
	}
	// Group C: file:line links in terminal tabs (tasks_run.go).
	if m.mouseProbe == nil && m.routeTerminalLinkClick(msg) {
		return m, nil
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
			if m.probed(m.findTargetAt(msg.X, msg.Y)) {
				return m, nil
			}
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
			// Persist once, so the width survives a relaunch (like the
			// terminal height and the AI panel width).
			m.dragKind = dragNone
			m.persistExplorerWidth()
			return m, nil
		}
	}
	if m.showExp && !m.zenMode && msg.Type == tea.MouseLeft && msg.X == splitterX {
		if m.probed("sidebar:splitter") {
			return m, nil
		}
		m.dragKind = dragExplorerRight
		m.dragStartX = msg.X
		return m, nil
	}

	// Activity bar (leftmost activity.Width cols; none in zen mode)
	if msg.X < activity.Width && !m.zenMode {
		if m.mouseProbe != nil {
			if it, ok := m.activity.ItemAt(msg.Y); ok {
				m.probed("activity:" + it.Label)
			} else {
				m.probed("activity")
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.activity, cmd = m.activity.HandleMouse(msg.X, msg.Y, msg.Type)
		return m, cmd
	}

	// Coordinates from here on are relative to area right of activity bar
	x := msg.X - activity.Width

	// Sidebar pane (right of activity bar, left of editor)
	if m.showExp && !m.zenMode && x < m.explorerWidth {
		if m.mouseProbe != nil {
			m.probed(m.sidebarTargetAt(x, msg.Y))
			return m, nil
		}
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
			if msg.Type == tea.MouseMotion {
				// Hover only: the tree does nothing with it, and the
				// persist below reads and writes the session file.
				return m, nil
			}
			var cmd tea.Cmd
			m.explorer, cmd = m.explorer.HandleMouse(x, localY, msg.Type, msg.Ctrl, msg.Shift)
			// A click may have toggled a directory — persist new state.
			if msg.Type == tea.MouseLeft {
				m.persistExplorerState()
			}
			return m, cmd
		case activity.ViewGit:
			if msg.Type == tea.MouseRight {
				m.openGitMenu(msg.X, msg.Y)
				return m, nil
			}
			return m.handleGitSidebarMouse(x, msg.Y, msg.Type)
		case activity.ViewSearch:
			// Header + placeholder; nothing interactive yet.
			return m, nil
		case activity.ViewRun: // Group D (debug_view.go)
			return m, m.handleRunSidebarMouse(x, msg.Y, msg.Type)
		case activity.ViewTests: // Group E (test_explorer.go)
			return m.handleTestsSidebarMouse(x, msg.Y, msg.Type)
		case activity.ViewServices: // services_actions.go
			return m.handleServicesSidebarMouse(x, msg.Y, msg.Type)
		case activity.ViewKubernetes: // kube_mouse.go
			return m.handleKubeSidebarMouse(x, msg.Y, msg.Type)
		case activity.ViewDatabases:
			return m.handleDatabasesSidebarMouse(x, msg.Y, msg.Type)
		case activity.ViewItermoc: // itermoc_actions.go
			return m.handleItermocSidebarMouse(x, msg.Y, msg.Type)
		case activity.ViewRuns: // runs_page.go
			return m.handleRunsSidebarMouse(x, msg.Y, msg.Type)
		}
		if m.isExtSidebar() { // Group I: extension panels (ext.go)
			return m, m.handleExtSidebarMouse(msg.Y, msg.Type)
		}
		return m, nil
	}
	// Right-side Actions panel body: when the click lands inside the
	// pinned panel column but not on a button (already handled above),
	// absorb it so it doesn't fall through to the editor.
	if m.actionsPanelVisible() && msg.X >= m.w-m.actionsColumnWidth() {
		m.probed("actions:body")
		return m, nil
	}

	// Editor region: the same pane geometry renderBase draws (replace
	// bar → tab bar → breadcrumbs → content; zen mode has no chrome).
	// The find bar is a floating overlay, not in the chrome stack.
	pane := m.editorPaneLayout()
	localX := msg.X - pane.X
	tabsRow := m.tabBarRow()
	tabsBodyRow := tabsRow + m.tabs.Height() - 1 // last row of the tab bar
	editorTopRow := pane.Top

	if m.replaceOpen && msg.Y < 2 {
		// click landed inside the replace bar — no mouse handling yet
		return m, nil
	}
	if tabsRow >= 0 && msg.Y >= tabsRow && msg.Y <= tabsBodyRow {
		if m.mouseProbe != nil {
			if tg, ok := m.tabs.TargetAt(localX); ok {
				m.probed(tabTargetID(tg))
			} else {
				m.probed("tabs")
			}
			return m, nil
		}
		// any tab-bar row (accent strip OR body) routes to tabbar
		var cmd tea.Cmd
		m.tabs, cmd = m.tabs.HandleMouse(localX, msg.X, msg.Y, msg.Type)
		return m, cmd
	}
	if msg.Y < editorTopRow {
		m.probed("breadcrumbs")
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
	if (msg.Type == tea.MouseLeft || msg.Type == tea.MouseMotion) && m.editorEmpty() {
		// Same pane size renderBase draws the welcome screen at (the AI
		// panel, when open, takes its width), so the hit map matches
		// what's actually on screen.
		pane := m.editorPaneLayout()
		edPaneW, editorH := pane.W, pane.H
		m.publishSetupCard()
		for i, hit := range welcomeRows(edPaneW, editorH) {
			if localY < hit.Row || localY >= hit.Row+hit.height() || localX < hit.ColStart || localX >= hit.ColEnd {
				continue
			}
			// Hover and click move the one cursor to the row under the
			// pointer; a click also runs it.
			m.welcomeFocus = i
			if msg.Type == tea.MouseMotion {
				return m, nil
			}
			if m.probed("welcome:" + hit.ID) {
				return m, nil
			}
			m.focus = FocusEditor
			return m.activateWelcomeRow(hit.ID)
		}
		if msg.Type == tea.MouseMotion {
			return m, nil
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
		barLocalY := m.terminalBarIndex(pane.H)
		if localY == barLocalY {
			m.probed("panel:bar")
			// Tab-bar row — terminalTabBarHitTest already handled every
			// button at the top of this function, so any click here is
			// on the non-interactive part. Drop it.
			return m, nil
		}
		if localY > barLocalY && m.panelActive == panelKindTerminal &&
			(msg.Type == tea.MouseWheelUp || msg.Type == tea.MouseWheelDown) {
			return m.terminalWheel(localX, localY, msg.Type)
		}
	}
	if m.termScrolled && msg.Type == tea.MouseLeft && m.mouseProbe == nil {
		m.endTerminalScroll()
	}

	// Side-by-side diff scrolling. A mouse wheel is forwarded to BOTH panes at
	// once (left col + right col), so each scrolls the same number of display
	// lines natively and they stay aligned — no 'scrollbind', whose per-tick
	// realignment across diff filler was the jank. Async dispatch keeps it
	// smooth. Plain motion still does focus-follows so the cursor sits on the
	// pane under the pointer.
	if m.gitDiffActive && m.nvim != nil {
		edPaneW := m.editorPaneLayout().W // drawn width (AI panel excluded)
		switch msg.Type {
		case tea.MouseLeft:
			// Change-nav buttons live on the winbar (localY == 0) at the very
			// start of the left pane: " ‹ " spans cols 0-2, " › " cols 3-5.
			// Clicking jumps to the prev/next change in BOTH panes (go-to-line).
			if m.mouseProbe == nil {
				recordError(fmt.Sprintf("[diffclick] localX=%d localY=%d msgX=%d msgY=%d edTop=%d", localX, localY, msg.X, msg.Y, editorTopRow))
			}
			if localY == 0 && diffNavShown(edPaneW) {
				switch {
				case localX >= 0 && localX <= 2:
					if m.probed("diff:prev") {
						return m, nil
					}
					recordError("[diffclick] -> PREV")
					_ = m.nvim.Command("lua _G.TcDiffPrev()")
					return m, nil
				case localX >= 3 && localX <= 5:
					if m.probed("diff:next") {
						return m, nil
					}
					recordError("[diffclick] -> NEXT")
					_ = m.nvim.Command("lua _G.TcDiffNext()")
					return m, nil
				}
			}
			// Not a nav button — fall through to the normal click forwarding.
		case tea.MouseWheelUp, tea.MouseWheelDown:
			dir := "up"
			if msg.Type == tea.MouseWheelDown {
				dir = "down"
			}
			right := edPaneW - 2
			if right < 2 {
				right = edPaneW - 1
			}
			m.nvim.MouseAsync("wheel", dir, "", localY, 1)
			m.nvim.MouseAsync("wheel", dir, "", localY, right)
			return m, nil
		case tea.MouseMotion:
			pane := 0
			if edPaneW > 0 && localX >= edPaneW/2 {
				pane = 1
			}
			if pane != m.gitDiffHoverPane {
				m.gitDiffHoverPane = pane
				dir := "h"
				if pane == 1 {
					dir = "l"
				}
				// stopinsert undoes the global "enter modifiable buffer →
				// startinsert" autocmd that fires on the working pane.
				_ = m.nvim.Command("wincmd " + dir + " | stopinsert")
			}
		}
	}

	if m.probed("editor") {
		return m, nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.HandleMouse(localX, localY, msg.Type)
	return m, cmd
}

// handleWelcomeKey moves the start page cursor over every interactive row
// (START, setup, recent files, workspaces): ↓ / ↑ / Tab / Shift+Tab (and
// → / ←) step and wrap, Home / End jump, Enter runs the row. Returns
// handled=true when the key was consumed so handleGlobalKey skips its
// normal switch; every other key (Ctrl+P, F1, …) falls through.
func (m Model) handleWelcomeKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	m.publishSetupCard()
	pane := m.editorPaneLayout()
	rows := welcomeRows(pane.W, pane.H)
	n := len(rows)
	if n == 0 {
		return m, nil, false
	}
	if m.welcomeFocus < 0 || m.welcomeFocus >= n {
		m.welcomeFocus = 0
	}
	switch msg.String() {
	case "up", "down":
		m.welcomeFocus = welcomeStep(m.welcomeFocus, n, msg.String() == "down", welcomeTileCount(rows), welcomeLayoutHits(pane.W, pane.H).tileCols)
		return m, nil, true
	case "left", "shift+tab":
		m.welcomeFocus = (m.welcomeFocus - 1 + n) % n
		return m, nil, true
	case "right", "tab":
		m.welcomeFocus = (m.welcomeFocus + 1) % n
		return m, nil, true
	case "home":
		m.welcomeFocus = 0
		return m, nil, true
	case "end":
		m.welcomeFocus = n - 1
		return m, nil, true
	case "enter":
		newM, cmd := m.activateWelcomeRow(rows[m.welcomeFocus].ID)
		return newM, cmd, true
	}
	return m, nil, false
}

// welcomeTileCount is how many leading rows are START tiles.
func welcomeTileCount(rows []welcomeRowHit) int {
	n := 0
	for _, r := range rows {
		if r.H == 0 {
			break
		}
		n++
	}
	return n
}

// welcomeStep moves the cursor one row up or down among n rows. The first
// tiles rows are a grid cols wide: up / down move a whole tile row (and
// keep the column); leaving the grid goes to the neighbouring list row.
func welcomeStep(cur, n int, down bool, tiles, cols int) int {
	if tiles == 0 || cols < 2 {
		if down {
			return (cur + 1) % n
		}
		return (cur - 1 + n) % n
	}
	switch {
	case cur >= tiles && down:
		return (cur + 1) % n
	case cur >= tiles:
		if cur == tiles {
			return tiles - cols // the last tile row, first column
		}
		return cur - 1
	case down:
		if cur+cols < tiles {
			return cur + cols
		}
		return tiles % n // the first list row (the top when there is none)
	default:
		if cur-cols >= 0 {
			return cur - cols
		}
		return n - 1
	}
}

// activateWelcomeRow runs start page row id (welcomeRowHit.ID).
func (m Model) activateWelcomeRow(id string) (Model, tea.Cmd) {
	kind, arg, _ := strings.Cut(id, ":")
	switch kind {
	case "card":
		i, _ := strconv.Atoi(arg)
		return m.runWelcomeCard(i)
	case "setup":
		return m, m.pressSetupRow(arg)
	case "recent":
		if arg == welcomeSentinelViewAll {
			return m, m.openRecentFilePicker()
		}
		if m.nvim != nil {
			m.ensureEditorWindowCurrent()
			_ = m.nvim.OpenFile("edit", arg)
		}
		return m, nil
	case "workspace":
		if arg == welcomeSentinelBrowseAll {
			return m, m.openWorkspacePicker()
		}
		// Re-execs termocode in that folder; a toast on failure.
		return m, m.openWorkspace(arg)
	}
	return m, nil
}

// dispatchWelcomeAction routes a Quick-Action card click to the same
// keymap.Action that the keyboard binding triggers. Synthesizing the
// matching tea.KeyMsg and feeding it through handleGlobalKey would risk
// drift if a binding ever changes, so we go straight to a synthetic
// KeyMsg that matches what handleGlobalKey expects via keys.Match.
func (m Model) dispatchWelcomeAction(action keymap.Action) (Model, tea.Cmd) {
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
