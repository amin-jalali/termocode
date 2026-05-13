package app

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"termocode/internal/keymap"
	"termocode/internal/theme"
)

// Overflow (⋮) menu — a small dropdown of secondary toggles anchored to a
// 1-cell vertical-three-dots glyph at the right edge of the tab bar's body
// row. Lives in the negative space at the right of the tab bar — does not
// push tabs left.
//
// As of the context-aware refactor the menu has TWO sections:
//
//	┌───────────────────────────────────┐
//	│ Go to Definition           F12    │   ← context section
//	│ Find References        Shift+F12  │
//	│ Outline             Ctrl+Shift+O  │
//	│ ───────────────────────────────── │   ← divider
//	│ Word Wrap                  Alt+W  │   ← general section
//	│ Line Numbers                 ✓    │
//	│ Outline             Ctrl+Shift+O  │
//	│ Settings                          │
//	└───────────────────────────────────┘
//
// The TOP section varies by what the user is currently doing (active file's
// language, terminal focus, …). The BOTTOM section is a fixed set of
// always-available toggles. A 1-row dim divider sits between them.

// overflowMenuGlyph is the chevron rendered at the right edge of the tab
// bar's body row. U+22EE (vertical ellipsis) is the closest single-cell
// glyph to the "⋮" the spec requested.
const overflowMenuGlyph = "⋮"

// overflowMenuRightInset is how many cells from the right edge of the
// editor pane the glyph sits at. 1 leaves a 1-cell breather on the right
// so the glyph isn't flush against the splice/scrollbar boundary.
const overflowMenuRightInset = 1

// overflowMenuReservedCells is the slice of the editor pane's right edge
// reserved for the ⋮ glyph and its breather. The tab bar is sized to
// `edPaneW - overflowMenuReservedCells` so its strip never has to share
// cells with the glyph: when many tabs overflow, the right-edge ▶ scroll
// indicator and the ⋮ glyph live in disjoint columns and the tab strip
// keeps rendering its content. The render path then pads these reserved
// cells with the title-bar bg and overpaints the ⋮ glyph on top.
//
// 1 cell for the glyph + overflowMenuRightInset cells of breather.
const overflowMenuReservedCells = overflowMenuRightInset + 1

// Width clamps for the dropdown's INNER content area (label + shortcut
// hint + checkmark column). The dropdown's outer width is inner+2 (border).
const (
	overflowMenuInnerMin = 22
	overflowMenuInnerMax = 38
)

// menuGroup tags a MenuItem so the renderer knows where to splice
// dividers in. "context" sits at the top; "general" below the divider;
// "settings" stays last (no divider before it — it's part of "general").
type menuGroup int

const (
	groupContext menuGroup = iota
	groupGeneral
)

// overflowMenuContext snapshots the slice of model state the items care
// about so item Visible/Checked closures don't have to capture *Model.
// Computed once per build (cheap — ~5 fields plus one nvim eval for word
// wrap which already happens in the legacy code path).
type overflowMenuContext struct {
	InTerminal      bool
	EditorPath      string
	Lang            string // canonicalized: "go", "markdown", "json", "yaml", ""
	WordWrapOn      bool
	LineNumOn       bool
	SidebarOn       bool
	ZenModeOn       bool
	AutoSaveOn      bool
	TerminalTabsLen int // # of integrated-terminal tabs (>1 → cycling makes sense)
}

// overflowMenuItem is one row of the dropdown. Visible/Checked may be nil
// (always-visible / no checkmark). Action runs when the user activates
// the row; closing the menu happens in activateOverflowMenuItem.
type overflowMenuItem struct {
	label    string
	visible  func(overflowMenuContext) bool   // nil → always visible
	checked  func(overflowMenuContext) bool   // nil → no checkmark
	action   func(m *Model) tea.Cmd
	shortcut string                            // pre-resolved at build time
	group    menuGroup
}

// overflowMenuRow is a renderer-side row — either a real item OR a
// divider line. The keyboard router uses isDivider to skip non-selectable
// rows when the user presses up/down.
type overflowMenuRow struct {
	item       overflowMenuItem
	isDivider  bool
	itemIndex  int // -1 for divider; otherwise index into the visible-items slice
}

// overflowMenuContextSnapshot computes the live context the menu builder
// reads from. Kept as a method on Model so the closures the items return
// stay simple (no capture of *Model needed for state lookups).
func (m Model) overflowMenuContextSnapshot() overflowMenuContext {
	path := m.editor.Path()
	return overflowMenuContext{
		InTerminal:      m.inTerminal,
		EditorPath:      path,
		Lang:            overflowMenuDetectLang(path, m.editor.Lang()),
		WordWrapOn:      m.queryWordWrapOn(),
		LineNumOn:       m.lineNumbersOn,
		SidebarOn:       m.showExp,
		ZenModeOn:       m.zenMode,
		AutoSaveOn:      m.autoSaveOn,
		TerminalTabsLen: len(m.terminalTabs),
	}
}

// overflowMenuDetectLang canonicalizes a file path / nvim filetype hint
// into one of the menu's known buckets. Empty string means "no specific
// context applies" — the caller should fall back to the generic top item
// (Outline) or skip the context section entirely.
//
// Order: prefer extension over nvim's filetype because nvim sometimes
// fails to set filetype on transient buffers (welcome screen, scratch).
// On welcome / empty buffer both come back empty → returns "".
func overflowMenuDetectLang(path, nvimFiletype string) string {
	if path != "" {
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".go":
			return "go"
		case ".md", ".markdown":
			return "markdown"
		case ".json", ".jsonc":
			return "json"
		case ".yaml", ".yml":
			return "yaml"
		}
	}
	switch strings.ToLower(strings.TrimSpace(nvimFiletype)) {
	case "go":
		return "go"
	case "markdown":
		return "markdown"
	case "json", "jsonc":
		return "json"
	case "yaml":
		return "yaml"
	}
	return ""
}

// overflowMenuShortcuts walks the live keymap once and returns a map
// from Action to its first-bound human-friendly hint string ("F12",
// "Ctrl+Shift+P", …). Items reference an Action; the builder pulls the
// hint from this map. Cheap — runs once per menu open.
func overflowMenuShortcuts(km keymap.KeyMap) map[keymap.Action]string {
	out := make(map[keymap.Action]string)
	// Bindings() returns a fresh map[string]Action — we walk it once and
	// keep the first key found per action, preferring shorter keys when
	// there's a tie (so "f12" wins over the longer alternative).
	for keyStr, act := range km.Bindings() {
		if act == keymap.ActionNone {
			continue
		}
		pretty := overflowMenuPrettyKey(keyStr)
		if existing, ok := out[act]; ok {
			// Keep whichever pretty-printed form is shorter / cleaner.
			// Prefer a single-token form ("F12") over compound ones
			// ("Ctrl+Shift+F12") so primary single-key bindings surface
			// in the menu — this is the form users recognize.
			if len(pretty) < len(existing) {
				out[act] = pretty
			}
			continue
		}
		out[act] = pretty
	}
	return out
}

// overflowMenuPrettyKey converts a Bubble Tea key string ("ctrl+shift+p",
// "f12", "alt+enter") into a presentation-friendly form ("Ctrl+Shift+P",
// "F12", "Alt+Enter"). Mirrors the convention used in palette Hint strings.
func overflowMenuPrettyKey(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "+")
	for i, p := range parts {
		switch p {
		case "ctrl":
			parts[i] = "Ctrl"
		case "shift":
			parts[i] = "Shift"
		case "alt":
			parts[i] = "Alt"
		case "cmd", "meta", "super":
			parts[i] = "Cmd"
		case "enter":
			parts[i] = "Enter"
		case "tab":
			parts[i] = "Tab"
		case "esc":
			parts[i] = "Esc"
		case "space":
			parts[i] = "Space"
		case "pgup":
			parts[i] = "PgUp"
		case "pgdown":
			parts[i] = "PgDn"
		default:
			// Function keys: "f12" → "F12". Otherwise upper-case the
			// first rune (so single-letter keys render as "P", "T",
			// matching conventional shortcut typography).
			if len(p) >= 2 && (p[0] == 'f' || p[0] == 'F') && isAllDigits(p[1:]) {
				parts[i] = "F" + p[1:]
				continue
			}
			if len(p) == 1 {
				parts[i] = strings.ToUpper(p)
				continue
			}
			// Multi-char unknown token: title-case ("backspace" → "Backspace").
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "+")
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// overflowMenuMinWidth is the legacy minimum dropdown OUTER width (border
// included). Kept for the rect math that already works.
const overflowMenuMinWidth = overflowMenuInnerMin + 2

// overflowMenuItems builds the live items list for the current model
// state. Group ordering: context → general (with a divider spliced in at
// render time). Items whose Visible() returns false are dropped here so
// the cursor index always matches what the user sees.
func (m Model) overflowMenuItems() []overflowMenuItem {
	ctx := m.overflowMenuContextSnapshot()
	hints := overflowMenuShortcuts(m.keys)
	all := overflowMenuAllItems(hints)
	out := make([]overflowMenuItem, 0, len(all))
	for _, it := range all {
		if it.visible != nil && !it.visible(ctx) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// overflowMenuAllItems is the master list — every possible row, with its
// Visible predicate. The builder filters by predicate to produce the
// per-context view. Pre-resolves shortcut hints at build time so the
// renderer doesn't have to look them up per row.
func overflowMenuAllItems(hints map[keymap.Action]string) []overflowMenuItem {
	hint := func(a keymap.Action) string { return hints[a] }

	return []overflowMenuItem{
		// ─── Context section ───────────────────────────────────────────
		// Go file: Go to Definition / Find References / Outline. Go to
		// Definition has no Action constant — it's bound directly in nvim
		// via lsp_lua.go's `<F12>` keymap on LspAttach. We invoke it
		// through our shared gotoDefinitionLua helper (same path the
		// right-click menu uses).
		{
			label:    "Go to Definition",
			group:    groupContext,
			shortcut: "F12", // bound nvim-side; not in keymap.KeyMap
			visible:  func(c overflowMenuContext) bool { return !c.InTerminal && c.Lang == "go" },
			action: func(m *Model) tea.Cmd {
				if m.nvim != nil {
					_ = m.nvim.ExecLua(gotoDefinitionLua)
				}
				return nil
			},
		},
		{
			label:    "Find References",
			group:    groupContext,
			shortcut: hint(keymap.ActionFindReferences),
			visible:  func(c overflowMenuContext) bool { return !c.InTerminal && c.Lang == "go" },
			action:   func(m *Model) tea.Cmd { return m.fetchReferencesCmd() },
		},
		{
			label:    "Outline",
			group:    groupContext,
			shortcut: hint(keymap.ActionGotoSymbolInFile),
			visible:  func(c overflowMenuContext) bool { return !c.InTerminal && c.Lang == "go" },
			action:   openSymbolPickerAction,
		},
		// Hover / Rename / Format / Find Implementations / Run Tests round
		// out the Go context menu with the LSP actions a Go user reaches
		// for daily. Rename Symbol has no Action constant — F2 is bound
		// nvim-side in lsp_lua.go to vim.lsp.buf.rename, same pattern as
		// "Go to Definition" above.
		{
			label:    "Hover",
			group:    groupContext,
			shortcut: hint(keymap.ActionHover),
			visible:  func(c overflowMenuContext) bool { return !c.InTerminal && c.Lang == "go" },
			action:   func(m *Model) tea.Cmd { return m.hoverCmd() },
		},
		{
			label:    "Rename Symbol",
			group:    groupContext,
			shortcut: "F2", // bound nvim-side in lsp_lua.go; not in keymap.KeyMap
			visible:  func(c overflowMenuContext) bool { return !c.InTerminal && c.Lang == "go" },
			action: func(m *Model) tea.Cmd {
				if m.nvim != nil {
					_ = m.nvim.ExecLua(`pcall(vim.lsp.buf.rename)`)
				}
				return nil
			},
		},
		{
			label:    "Format Document",
			group:    groupContext,
			shortcut: hint(keymap.ActionFormatDocument),
			visible:  func(c overflowMenuContext) bool { return !c.InTerminal && c.Lang == "go" },
			action:   func(m *Model) tea.Cmd { return m.formatDocumentCmd() },
		},
		{
			label:    "Find Implementations",
			group:    groupContext,
			shortcut: hint(keymap.ActionGotoImplementation),
			visible:  func(c overflowMenuContext) bool { return !c.InTerminal && c.Lang == "go" },
			action:   func(m *Model) tea.Cmd { return m.gotoImplementationCmd() },
		},
		{
			label:    "Run Tests",
			group:    groupContext,
			shortcut: hint(keymap.ActionRunTests),
			visible:  func(c overflowMenuContext) bool { return !c.InTerminal && c.Lang == "go" },
			action:   func(m *Model) tea.Cmd { return m.runTestsCmd() },
		},

		// Markdown: Preview. Split-view is not implemented as a separate
		// action — the F7 markdown preview opens an overlay rather than a
		// split, so we expose only the Preview item here. (Documented in
		// the agent-summary as a gap.)
		{
			label:    "Preview Markdown",
			group:    groupContext,
			shortcut: hint(keymap.ActionMarkdownPreview),
			visible:  func(c overflowMenuContext) bool { return !c.InTerminal && c.Lang == "markdown" },
			action:   func(m *Model) tea.Cmd { return m.openPreviewCmd() },
		},

		// JSON / YAML: Format / Collapse / Expand.
		{
			label:    "Format Document",
			group:    groupContext,
			shortcut: hint(keymap.ActionFormatDocument),
			visible: func(c overflowMenuContext) bool {
				return !c.InTerminal && (c.Lang == "json" || c.Lang == "yaml")
			},
			action: func(m *Model) tea.Cmd { return m.formatDocumentCmd() },
		},
		{
			label: "Collapse All",
			group: groupContext,
			visible: func(c overflowMenuContext) bool {
				return !c.InTerminal && (c.Lang == "json" || c.Lang == "yaml")
			},
			action: func(m *Model) tea.Cmd {
				if m.nvim != nil {
					// `zM` is normal-mode "close all folds". Wrapping in
					// nvim_feedkeys with the "n" mode and remap=false so
					// it runs even when the user is currently in insert
					// or terminal mode (the menu's keyboard router
					// always closes the menu first, but the active mode
					// at item-activation time can still vary).
					_ = m.nvim.Command("normal! zM")
				}
				return nil
			},
		},
		{
			label: "Expand All",
			group: groupContext,
			visible: func(c overflowMenuContext) bool {
				return !c.InTerminal && (c.Lang == "json" || c.Lang == "yaml")
			},
			action: func(m *Model) tea.Cmd {
				if m.nvim != nil {
					_ = m.nvim.Command("normal! zR")
				}
				return nil
			},
		},

		// Terminal context: New Tab / Clear / Scrollback. Split-terminal
		// has no current Action, so we omit it (documented as a gap).
		{
			label:    "New Terminal Tab",
			group:    groupContext,
			shortcut: hint(keymap.ActionTerminalNewTab),
			visible:  func(c overflowMenuContext) bool { return c.InTerminal },
			action: func(m *Model) tea.Cmd {
				m.newTerminalTab()
				return nil
			},
		},
		{
			label:   "Clear Terminal",
			group:   groupContext,
			visible: func(c overflowMenuContext) bool { return c.InTerminal },
			action: func(m *Model) tea.Cmd {
				m.clearActiveTerminalTab()
				return nil
			},
		},
		{
			label:   "Show Scrollback",
			group:   groupContext,
			visible: func(c overflowMenuContext) bool { return c.InTerminal },
			action: func(m *Model) tea.Cmd {
				// Open the buffer-info preview pointed at the active
				// terminal — the closest existing surface for "show me
				// what's in this terminal's scrollback". Better than
				// nothing while a dedicated scrollback overlay is TBD.
				return m.openTerminalScrollbackOverlayCmd()
			},
		},
		// Tab management for the integrated terminal. Close Tab is always
		// available when in terminal context; Next/Prev only when there's
		// more than one tab to cycle to.
		{
			label:    "Close Terminal Tab",
			group:    groupContext,
			shortcut: hint(keymap.ActionTerminalCloseTab),
			visible:  func(c overflowMenuContext) bool { return c.InTerminal },
			action: func(m *Model) tea.Cmd {
				if m.termOpen && len(m.terminalTabs) > 0 {
					m.closeTerminalTab(m.terminalActiveTab)
					m.applyLayout()
					m.resizeTerminalSplit()
				}
				return nil
			},
		},
		{
			label:    "Next Terminal Tab",
			group:    groupContext,
			shortcut: hint(keymap.ActionTerminalNextTab),
			visible:  func(c overflowMenuContext) bool { return c.InTerminal && c.TerminalTabsLen > 1 },
			action: func(m *Model) tea.Cmd {
				m.cycleTerminalTab(1)
				return nil
			},
		},
		{
			label:    "Previous Terminal Tab",
			group:    groupContext,
			shortcut: hint(keymap.ActionTerminalPrevTab),
			visible:  func(c overflowMenuContext) bool { return c.InTerminal && c.TerminalTabsLen > 1 },
			action: func(m *Model) tea.Cmd {
				m.cycleTerminalTab(-1)
				return nil
			},
		},
		{
			label:    "Open External Shell",
			group:    groupContext,
			shortcut: hint(keymap.ActionOpenShell),
			visible:  func(c overflowMenuContext) bool { return c.InTerminal },
			action:   func(m *Model) tea.Cmd { return openShellCmd() },
		},

		// Non-terminal file context: "Open Terminal Here" cd's the
		// integrated terminal into the active file's directory. Only
		// makes sense when there's an active file (welcome screen has
		// no path → hidden).
		{
			label:   "Open Terminal Here",
			group:   groupContext,
			visible: func(c overflowMenuContext) bool { return !c.InTerminal && c.EditorPath != "" },
			action: func(m *Model) tea.Cmd {
				m.openIntegratedTerminalAt(filepath.Dir(m.editor.Path()))
				return nil
			},
		},

		// Generic-context fallback: a sensible top item when nothing
		// else fits. Only shows on non-terminal, non-empty buffers
		// whose language doesn't match any of the buckets above.
		{
			label:    "Outline",
			group:    groupContext,
			shortcut: hint(keymap.ActionGotoSymbolInFile),
			visible: func(c overflowMenuContext) bool {
				if c.InTerminal {
					return false
				}
				if c.EditorPath == "" {
					return false // welcome screen / empty: no context items
				}
				switch c.Lang {
				case "go", "markdown", "json", "yaml":
					return false // already covered by their own buckets
				}
				return true
			},
			action: openSymbolPickerAction,
		},

		// ─── General section ───────────────────────────────────────────
		// Always present, regardless of context. Order: chrome toggles
		// first (Sidebar, Word Wrap, Line Numbers), then mode toggles
		// (Zen, Auto Save), then jumps (Outline, Pick Theme), then
		// Settings last.
		{
			label:    "Toggle Sidebar",
			group:    groupGeneral,
			shortcut: hint(keymap.ActionToggleExplorer),
			checked:  func(c overflowMenuContext) bool { return c.SidebarOn },
			action: func(m *Model) tea.Cmd {
				m.showExp = !m.showExp
				m.applyLayout()
				return nil
			},
		},
		{
			label:    "Word Wrap",
			group:    groupGeneral,
			shortcut: hint(keymap.ActionToggleWordWrap),
			checked:  func(c overflowMenuContext) bool { return c.WordWrapOn },
			action:   toggleWordWrapAction,
		},
		{
			label:   "Line Numbers",
			group:   groupGeneral,
			checked: func(c overflowMenuContext) bool { return c.LineNumOn },
			action:  toggleLineNumbersAction,
		},
		{
			label:    "Zen Mode",
			group:    groupGeneral,
			shortcut: hint(keymap.ActionToggleZenMode),
			checked:  func(c overflowMenuContext) bool { return c.ZenModeOn },
			action:   func(m *Model) tea.Cmd { return m.toggleZenMode() },
		},
		{
			label:    "Auto Save",
			group:    groupGeneral,
			shortcut: hint(keymap.ActionToggleAutoSave),
			checked:  func(c overflowMenuContext) bool { return c.AutoSaveOn },
			action:   func(m *Model) tea.Cmd { return m.toggleAutoSave() },
		},
		{
			label:    "Outline",
			group:    groupGeneral,
			shortcut: hint(keymap.ActionGotoSymbolInFile),
			action:   openSymbolPickerAction,
		},
		{
			label:    "Pick Theme",
			group:    groupGeneral,
			shortcut: hint(keymap.ActionPickTheme),
			action:   func(m *Model) tea.Cmd { return m.openThemePicker() },
		},
		{
			label:  "Settings",
			group:  groupGeneral,
			action: openSettingsAction,
		},
	}
}

// overflowMenuRows builds the renderer-side row sequence: context items,
// then a divider (only if BOTH groups are non-empty), then general items.
// The divider gets isDivider=true and itemIndex=-1; real items carry
// their index into the items slice this function takes as input.
func overflowMenuRows(items []overflowMenuItem) []overflowMenuRow {
	rows := make([]overflowMenuRow, 0, len(items)+1)
	hasContext := false
	for _, it := range items {
		if it.group == groupContext {
			hasContext = true
			break
		}
	}
	hasGeneral := false
	for _, it := range items {
		if it.group == groupGeneral {
			hasGeneral = true
			break
		}
	}
	dividerInserted := false
	for i, it := range items {
		if it.group == groupGeneral && hasContext && hasGeneral && !dividerInserted {
			rows = append(rows, overflowMenuRow{isDivider: true, itemIndex: -1})
			dividerInserted = true
		}
		rows = append(rows, overflowMenuRow{item: it, itemIndex: i})
	}
	return rows
}

// queryWordWrapOn checks nvim's window-local `wrap` option so the
// checkmark always reflects the live state.
func (m Model) queryWordWrapOn() bool {
	if m.nvim == nil {
		return false
	}
	out, err := m.nvim.EvalLuaString(`return vim.wo.wrap and 'on' or 'off'`)
	if err != nil {
		return false
	}
	return out == "on"
}

// openSymbolPickerAction opens the document-symbol picker (the glass-
// overlay floating modal that lists the active file's symbols). Replaces
// the legacy sidebar-based outline panel as the only way to reach symbols.
func openSymbolPickerAction(m *Model) tea.Cmd {
	return m.fetchSymbolsCmd()
}

// toggleWordWrapAction is a thin wrapper around (*Model).toggleWordWrap
// (defined in quick_actions.go) so the menu action signature matches.
func toggleWordWrapAction(m *Model) tea.Cmd {
	return m.toggleWordWrap()
}

// toggleLineNumbersAction flips nvim's `set number` and updates the UI
// mirror so the next render's checkmark is correct.
func toggleLineNumbersAction(m *Model) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	if m.lineNumbersOn {
		_ = m.nvim.Command("set nonumber")
		m.lineNumbersOn = false
	} else {
		_ = m.nvim.Command("set number")
		m.lineNumbersOn = true
	}
	return nil
}

// openSettingsAction opens the dedicated settings modal.
func openSettingsAction(m *Model) tea.Cmd {
	return m.openSettingsPicker()
}

// clearActiveTerminalTab sends Ctrl+L (form-feed, 0x0C) to the active
// terminal tab's PTY. Most shells (bash, zsh, fish) interpret this as
// "clear screen" — same as the user pressing Ctrl+L themselves.
func (m *Model) clearActiveTerminalTab() {
	if m.nvim == nil {
		return
	}
	tab := m.activeTerminalTab()
	if tab == nil {
		return
	}
	_ = m.nvim.ExecLua(`
		local buf = ` + intToLua(tab.BufID) + `
		if vim.api.nvim_buf_is_loaded(buf) and vim.bo[buf].buftype == 'terminal' then
			local ok, chan = pcall(function() return vim.bo[buf].channel end)
			if ok and chan and chan > 0 then
				pcall(vim.fn.chansend, chan, '\12')
			end
		end
	`)
}

// openTerminalScrollbackOverlayCmd opens a preview overlay containing the
// active terminal tab's recent scrollback. Best-effort — when no terminal
// is active we push an info toast instead.
func (m *Model) openTerminalScrollbackOverlayCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	tab := m.activeTerminalTab()
	if tab == nil {
		return nil
	}
	// Pull the buffer's lines via nvim and stuff them into the existing
	// preview overlay. Reuses the same path the cheatsheet / error log
	// use so we don't carry a parallel implementation.
	body, err := m.nvim.EvalLuaString(`
		local buf = ` + intToLua(tab.BufID) + `
		if not vim.api.nvim_buf_is_loaded(buf) then return '' end
		local lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
		return table.concat(lines, '\n')
	`)
	if err != nil || strings.TrimSpace(body) == "" {
		return nil
	}
	return func() tea.Msg {
		return PreviewMsg{Title: " Terminal Scrollback ", Body: body}
	}
}

// intToLua emits a Go int as a Lua-compatible numeric literal. Tiny
// helper used by the heredoc-Lua chunks above so we don't have to drag
// in fmt for a single Sprintf.
func intToLua(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

// overflowGlyphX returns the screen column where the ⋮ glyph sits, given
// the current model geometry. Returns -1 when the editor pane is too
// narrow to host the glyph.
func (m Model) overflowGlyphX() int {
	edPaneW := m.editorPaneWidth()
	if edPaneW < 4 {
		return -1
	}
	editorStart := m.w - edPaneW - m.actionsColumnWidth()
	return editorStart + edPaneW - overflowMenuRightInset - 1
}

// overflowGlyphY returns the screen row where the ⋮ glyph sits — the
// last row of the tab bar (its "body" row, after the optional accent
// strip).
func (m Model) overflowGlyphY() int {
	rowsAboveTabs := 0
	if m.replaceOpen {
		rowsAboveTabs += 2
	}
	tabsRow := rowsAboveTabs
	tabsBodyRow := tabsRow + m.tabs.Height() - 1
	return tabsBodyRow
}

// hitOverflowGlyph reports whether (screenX, screenY) lands on the ⋮
// glyph cell.
func (m Model) hitOverflowGlyph(screenX, screenY int) bool {
	gx := m.overflowGlyphX()
	if gx < 0 {
		return false
	}
	if screenY != m.overflowGlyphY() {
		return false
	}
	return screenX == gx
}

// overflowMenuMeasureInner returns the inner content width to use for
// the dropdown given the current items. Computed as:
//
//	max( 2 (checkmark column) + label + 2 (gap) + shortcut ) over visible items
//
// then clamped to [overflowMenuInnerMin, overflowMenuInnerMax].
func overflowMenuMeasureInner(items []overflowMenuItem) int {
	maxW := 0
	for _, it := range items {
		labelW := runewidth.StringWidth(it.label)
		shortcutW := runewidth.StringWidth(it.shortcut)
		// 2 (checkmark column) + 1 (left pad) + label + at-least-2 gap +
		// shortcut + 1 (right pad). For items without a shortcut we
		// drop the gap+shortcut cost (the renderer fills with spaces).
		w := 1 + 2 + labelW + 1
		if shortcutW > 0 {
			w += 2 + shortcutW
		}
		if w > maxW {
			maxW = w
		}
	}
	if maxW < overflowMenuInnerMin {
		maxW = overflowMenuInnerMin
	}
	if maxW > overflowMenuInnerMax {
		maxW = overflowMenuInnerMax
	}
	return maxW
}

// overflowMenuRect returns the screen rectangle of the dropdown box
// (including its border) when the menu is open.
func (m Model) overflowMenuRect() (x, y, w, h int, ok bool) {
	if !m.overflowMenuOpen {
		return 0, 0, 0, 0, false
	}
	items := m.overflowMenuItems()
	if len(items) == 0 {
		return 0, 0, 0, 0, false
	}
	gx := m.overflowGlyphX()
	if gx < 0 {
		return 0, 0, 0, 0, false
	}
	inner := overflowMenuMeasureInner(items)
	w = inner + 2
	rows := overflowMenuRows(items)
	h = len(rows) + 2 // top border + rows (incl. divider) + bottom border
	x = gx + 1 - w
	if x < 0 {
		x = 0
	}
	if x+w > m.w {
		x = m.w - w
	}
	y = m.overflowGlyphY() + 1
	if y+h > m.h {
		y = m.h - h
		if y < 0 {
			y = 0
		}
	}
	return x, y, w, h, true
}

// renderOverflowMenu returns the dropdown's rendered box (h rows of w
// styled cells).
func (m Model) renderOverflowMenu(w, h int) string {
	items := m.overflowMenuItems()
	if len(items) == 0 || w < 4 || h < 3 {
		return ""
	}
	rows := overflowMenuRows(items)
	if len(rows)+2 > h {
		// Defensive: should never happen because rect math sizes h to
		// match. Truncate so the splice doesn't run past the box.
		rows = rows[:h-2]
	}

	bg := theme.Bg(theme.BgPanel)
	border := theme.FgBg(theme.BorderDefault, theme.BgPanel)
	innerW := w - 2

	top := border.Render("┌" + strings.Repeat("─", innerW) + "┐")
	bot := border.Render("└" + strings.Repeat("─", innerW) + "┘")

	out := make([]string, 0, h)
	out = append(out, top)

	// Snapshot context once — Checked() is called per row and one of
	// the fields (WordWrapOn) does an nvim RPC. We don't want that
	// running per-row every render frame.
	ctx := m.overflowMenuContextSnapshot()

	cursor := m.overflowMenuCursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	// The cursor index addresses ROWS (including dividers). The mouse
	// hit-tester and keyboard router skip dividers, so an open menu
	// should never land its cursor on one — but clamp defensively in
	// case the items list shrinks between activations.
	if cursor >= 0 && cursor < len(rows) && rows[cursor].isDivider {
		// Move cursor to the next selectable row.
		for i := cursor + 1; i < len(rows); i++ {
			if !rows[i].isDivider {
				cursor = i
				break
			}
		}
	}

	for i, r := range rows {
		if r.isDivider {
			line := border.Render("│") +
				bg.Render(" ") +
				border.Render(strings.Repeat("─", innerW-2)) +
				bg.Render(" ") +
				border.Render("│")
			out = append(out, line)
			continue
		}
		active := i == cursor
		var rowBg lipgloss.Style
		var labelStyle, shortcutStyle lipgloss.Style
		if active {
			rowBg = theme.Bg(theme.BgHover)
			labelStyle = theme.FgBg(theme.TextPrimary, theme.BgHover)
			shortcutStyle = theme.FgBg(theme.TextMuted, theme.BgHover)
		} else {
			rowBg = bg
			labelStyle = theme.FgBg(theme.TextSecondary, theme.BgPanel)
			shortcutStyle = theme.FgBg(theme.TextMuted, theme.BgPanel)
		}

		// Checkmark column: 2 cells, left-side. Always 2 cells wide so
		// labels stay aligned regardless of state.
		var checkCell string
		if r.item.checked != nil && r.item.checked(ctx) {
			var checkStyle lipgloss.Style
			if active {
				checkStyle = theme.FgBg(theme.AccentLavender, theme.BgHover).Bold(true)
			} else {
				checkStyle = theme.FgBg(theme.AccentLavender, theme.BgPanel).Bold(true)
			}
			checkCell = checkStyle.Render("✓") + rowBg.Render(" ")
		} else {
			checkCell = rowBg.Render("  ")
		}

		// Inner row layout:
		//   "<check 2><label> ... <gap> <shortcut> "
		//
		// labelArea = innerW - 2 (check) - 1 (right pad)
		// We render: check + label + filler + shortcut + " "
		labelW := runewidth.StringWidth(r.item.label)
		shortcutW := runewidth.StringWidth(r.item.shortcut)
		labelArea := innerW - 2 - 1
		if labelArea < 0 {
			labelArea = 0
		}
		// Reserve room for the shortcut on the right.
		labelMax := labelArea - shortcutW
		if shortcutW > 0 {
			labelMax-- // 1-cell gap between label and shortcut
		}
		if labelMax < 1 {
			labelMax = 1
		}
		if labelW > labelMax {
			r.item.label = runewidth.Truncate(r.item.label, labelMax, "…")
			labelW = runewidth.StringWidth(r.item.label)
		}
		filler := labelArea - labelW - shortcutW
		if filler < 0 {
			filler = 0
		}
		body := checkCell + labelStyle.Render(r.item.label)
		body += rowBg.Render(strings.Repeat(" ", filler))
		if shortcutW > 0 {
			body += shortcutStyle.Render(r.item.shortcut)
		}
		body += rowBg.Render(" ")
		// Defensive width pin so the splice math stays exact.
		bodyW := lipgloss.Width(body)
		if bodyW < innerW {
			body += rowBg.Render(strings.Repeat(" ", innerW-bodyW))
		}
		line := border.Render("│") + body + border.Render("│")
		out = append(out, line)
	}
	out = append(out, bot)
	return strings.Join(out, "\n")
}

// overlayOverflowMenuGlyph paints the ⋮ chevron on the tab bar's body row.
func (m Model) overlayOverflowMenuGlyph(base string) string {
	gx := m.overflowGlyphX()
	if gx < 0 {
		return base
	}
	row := m.overflowGlyphY()
	lines := strings.Split(base, "\n")
	if row < 0 || row >= len(lines) {
		return base
	}
	fg := theme.TextMuted
	if m.overflowMenuOpen {
		fg = theme.TextPrimary
	}
	style := theme.FgBg(fg, theme.BgTitleBar).Bold(true)
	cell := style.Render(overflowMenuGlyph)
	lines[row] = spliceAt(lines[row], cell, gx)
	return strings.Join(lines, "\n")
}

// overlayOverflowMenuDropdown paints the dropdown box at its anchored
// position.
func (m Model) overlayOverflowMenuDropdown(base string) string {
	x, y, w, h, ok := m.overflowMenuRect()
	if !ok {
		return base
	}
	box := m.renderOverflowMenu(w, h)
	if box == "" {
		return base
	}
	lines := strings.Split(base, "\n")
	boxLines := strings.Split(box, "\n")
	for i, bl := range boxLines {
		row := y + i
		if row < 0 || row >= len(lines) {
			continue
		}
		lines[row] = spliceAt(lines[row], bl, x)
	}
	return strings.Join(lines, "\n")
}

// hitOverflowMenuRect reports whether (screenX, screenY) lands inside
// the dropdown's bounding box (including the border).
func (m Model) hitOverflowMenuRect(screenX, screenY int) bool {
	x, y, w, h, ok := m.overflowMenuRect()
	if !ok {
		return false
	}
	if screenY < y || screenY >= y+h {
		return false
	}
	if screenX < x || screenX >= x+w {
		return false
	}
	return true
}

// overflowMenuItemRow returns the index into the renderer rows for a
// click at screenY (or -1 if the row isn't on a clickable item — i.e.
// the top/bottom border row, the divider row, or outside the box).
//
// Returns the row index (which is what cursor addresses), NOT the index
// into the items slice — the caller maps row→item via overflowMenuRows.
func (m Model) overflowMenuItemRow(screenY int) int {
	x, y, _, h, ok := m.overflowMenuRect()
	if !ok {
		return -1
	}
	_ = x
	if screenY <= y || screenY >= y+h-1 {
		return -1
	}
	rowIdx := screenY - y - 1
	items := m.overflowMenuItems()
	rows := overflowMenuRows(items)
	if rowIdx < 0 || rowIdx >= len(rows) {
		return -1
	}
	if rows[rowIdx].isDivider {
		return -1
	}
	return rowIdx
}

// activateOverflowMenuItem invokes the cursor-pointed item's action and
// closes the menu. The cursor addresses ROWS (including dividers); we
// re-resolve the row→item mapping here before firing the action.
func (m *Model) activateOverflowMenuItem() tea.Cmd {
	items := m.overflowMenuItems()
	if len(items) == 0 {
		m.overflowMenuOpen = false
		return nil
	}
	rows := overflowMenuRows(items)
	idx := m.overflowMenuCursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	if rows[idx].isDivider || rows[idx].itemIndex < 0 {
		// Can't fire on a divider — close and bail.
		m.overflowMenuOpen = false
		return nil
	}
	cmd := items[rows[idx].itemIndex].action(m)
	m.overflowMenuOpen = false
	return cmd
}

// routeToOverflowMenu handles keyboard input while the dropdown is open.
// Up/Down navigate, skipping dividers; Enter selects; Esc closes.
func (m Model) routeToOverflowMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	items := m.overflowMenuItems()
	rows := overflowMenuRows(items)
	switch key.String() {
	case "esc":
		m.overflowMenuOpen = false
		return m, nil
	case "up", "k":
		if len(rows) == 0 {
			return m, nil
		}
		m.overflowMenuCursor = overflowMenuPrevSelectable(rows, m.overflowMenuCursor)
		return m, nil
	case "down", "j":
		if len(rows) == 0 {
			return m, nil
		}
		m.overflowMenuCursor = overflowMenuNextSelectable(rows, m.overflowMenuCursor)
		return m, nil
	case "enter":
		cmd := m.activateOverflowMenuItem()
		return m, cmd
	}
	m.overflowMenuOpen = false
	return m.handleGlobalKey(key)
}

// overflowMenuNextSelectable advances `cursor` to the next non-divider
// row, wrapping around. If no row is selectable (all dividers — should
// never happen) returns the input unchanged.
func overflowMenuNextSelectable(rows []overflowMenuRow, cursor int) int {
	n := len(rows)
	if n == 0 {
		return 0
	}
	for step := 1; step <= n; step++ {
		i := ((cursor+step)%n + n) % n
		if !rows[i].isDivider {
			return i
		}
	}
	return cursor
}

// overflowMenuPrevSelectable mirrors Next but walks backward.
func overflowMenuPrevSelectable(rows []overflowMenuRow, cursor int) int {
	n := len(rows)
	if n == 0 {
		return 0
	}
	for step := 1; step <= n; step++ {
		i := ((cursor-step)%n + n) % n
		if !rows[i].isDivider {
			return i
		}
	}
	return cursor
}

// openOverflowMenu flips the dropdown open and resets the cursor to the
// first SELECTABLE row so a freshly-opened menu always lands on a real
// item (never on a divider, even though dividers don't appear at index 0
// in any current configuration).
func (m *Model) openOverflowMenu() {
	m.overflowMenuOpen = true
	items := m.overflowMenuItems()
	rows := overflowMenuRows(items)
	m.overflowMenuCursor = 0
	for i, r := range rows {
		if !r.isDivider {
			m.overflowMenuCursor = i
			break
		}
	}
}
