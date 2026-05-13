package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/activity"
	"termocode/internal/findbar"
	"termocode/internal/picker"
	"termocode/internal/search"
	"termocode/internal/theme"
)

type pickerKindEnum int

const (
	pickerKindFiles pickerKindEnum = iota
	pickerKindPalette
	pickerKindTheme
	pickerKindClipboard
	pickerKindProblems
	pickerKindCodeAction
	pickerKindSymbols
	pickerKindWorkspaceSymbols
	pickerKindBranches
	pickerKindRecentFiles
	pickerKindWorkspaces
	pickerKindStash
	pickerKindDiffLeft
	pickerKindDiffRight
	pickerKindGitLog
	pickerKindTags
	pickerKindSnippet
	pickerKindRemoveRoot
	pickerKindSettings
	pickerKindThemeEditor
	pickerKindKeybinding
)

// loadFiles walks the cwd and returns one picker.Item per file (relative path).
//
// In a multi-root workspace it also walks every additional root persisted
// in the session file. Files in the cwd root keep their cwd-relative paths
// (Item.ID + Title) so existing call sites that `nvim.Command("edit "+id)`
// don't change behaviour. Files in extra roots use their absolute path as
// the ID (so :edit can find them) and a title prefixed with the root's
// basename so the picker reads as "myproj/foo.go".
//
// Dedup is by absolute path: a file already surfaced under the cwd root is
// not re-emitted if an extra root happens to overlap.
func loadFiles() []picker.Item {
	return loadFilesIn(workspaceRoots())
}

// loadFilesIn is the testable core of loadFiles: walks each root in `roots`
// (in order) and emits one picker.Item per file. Duplicates (by absolute
// path) are skipped.
func loadFilesIn(roots []string) []picker.Item {
	skipDirs := map[string]bool{
		".git":         true,
		"node_modules": true,
		"vendor":       true,
		"dist":         true,
		"build":        true,
		"target":       true,
		"__pycache__":  true,
		".idea":        true,
		".vscode":      true,
		".cache":       true,
	}
	seen := make(map[string]bool, 256)
	var items []picker.Item
	for i, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		_ = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				name := info.Name()
				if path != abs && (skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			absPath, errAbs := filepath.Abs(path)
			if errAbs != nil {
				absPath = path
			}
			if seen[absPath] {
				return nil
			}
			seen[absPath] = true
			rel, errRel := filepath.Rel(abs, path)
			if errRel != nil {
				return nil
			}
			// Primary root (i==0): keep ID/Title cwd-relative so existing
			// behaviour (e.g. :edit <relpath>) is preserved. Extra roots:
			// use absolute path for ID, prefix the visible title with the
			// root basename so users can tell which project a file lives in.
			if i == 0 {
				items = append(items, picker.Item{ID: rel, Title: rel})
			} else {
				title := filepath.Join(filepath.Base(abs), rel)
				items = append(items, picker.Item{ID: absPath, Title: title})
			}
			return nil
		})
	}
	return items
}

// workspaceRoots returns the live list of workspace roots: cwd first, then
// any additional roots persisted via Add Folder. The explorer is the
// runtime source of truth, but it isn't reachable from a free function;
// instead we read the saved-roots list from the session file (kept in sync
// by the explorer's add/remove handlers). cwd is always first.
func workspaceRoots() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	out := []string{cwd}
	for _, p := range loadSession().Roots {
		if p == "" || p == cwd {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

// paletteItems returns the static list of commands shown in the command palette.
// Each item's ID is dispatched in dispatchPaletteAction.
func paletteItems() []picker.Item {
	return []picker.Item{
		{ID: "save", Title: "File: Save", Hint: "Ctrl+S"},
		{ID: "save-all", Title: "File: Save All Files"},
		{ID: "close-buffer", Title: "View: Close Editor", Hint: "Ctrl+W"},
		{ID: "reopen-closed", Title: "View: Reopen Closed Editor", Hint: "Shift+F4 / Alt+Shift+T"},
		{ID: "clipboard-history", Title: "Edit: Clipboard History", Hint: "Ctrl+Shift+V"},
		{ID: "next-buffer", Title: "View: Next Editor", Hint: "Ctrl+PgDn"},
		{ID: "prev-buffer", Title: "View: Previous Editor", Hint: "Ctrl+PgUp"},
		{ID: "toggle-explorer", Title: "View: Toggle Explorer", Hint: "Ctrl+B"},
		{ID: "toggle-actions-panel", Title: "View: Toggle Actions Panel", Hint: "Alt+A"},
		{ID: "focus-explorer", Title: "View: Focus Explorer", Hint: "Ctrl+0"},
		{ID: "focus-editor", Title: "View: Focus Editor", Hint: "Ctrl+1"},
		{ID: "reveal-file", Title: "Reveal File in Explorer", Hint: "Ctrl+Shift+E"},
		{ID: "toggle-terminal", Title: "Terminal: Toggle Integrated Panel", Hint: "Ctrl+T"},
		{ID: "open-shell", Title: "Terminal: Open External Shell"},
		{ID: "terminal-new-tab", Title: "Terminal: New Tab", Hint: "Ctrl+Shift+`"},
		{ID: "terminal-close-tab", Title: "Terminal: Close Active Tab", Hint: "Ctrl+Shift+W"},
		{ID: "terminal-next-tab", Title: "Terminal: Next Tab", Hint: "Ctrl+Shift+PgDown"},
		{ID: "terminal-prev-tab", Title: "Terminal: Previous Tab", Hint: "Ctrl+Shift+PgUp"},
		{ID: "terminal-maximize", Title: "Terminal: Maximize Panel"},
		{ID: "terminal-minimize", Title: "Terminal: Minimize Panel"},
		{ID: "quick-open", Title: "Go to File...", Hint: "Ctrl+P"},
		{ID: "duplicate-line", Title: "Edit: Duplicate Line", Hint: "Shift+Alt+Down"},
		{ID: "toggle-comment", Title: "Edit: Toggle Line Comment", Hint: "Ctrl+/"},
		{ID: "fold-all", Title: "View: Fold All", Hint: "zM"},
		{ID: "unfold-all", Title: "View: Unfold All", Hint: "zR"},
		{ID: "toggle-fold", Title: "View: Toggle Fold Under Cursor", Hint: "za"},
		{ID: "preview-md", Title: "Markdown: Open Preview", Hint: "F7"},
		{ID: "find-files", Title: "Search: Find in Files", Hint: "F8 / Alt+F"},
		{ID: "find-files-overlay", Title: "Search: Find in Files (Live Overlay)"},
		{ID: "replace-in-file", Title: "Edit: Replace in File", Hint: "Ctrl+H"},
		{ID: "toggle-inlay-hints", Title: "LSP: Toggle Inlay Hints", Hint: "F4"},
		{ID: "pick-theme", Title: "Preferences: Color Theme"},
		{ID: "git-stage", Title: "Git: Stage Current File"},
		{ID: "git-stage-all", Title: "Git: Stage All Changes"},
		{ID: "git-commit", Title: "Git: Commit..."},
		{ID: "git-diff", Title: "Git: Show Diff for Current File"},
		{ID: "git-discard", Title: "Git: Discard Changes in Current File"},
		{ID: "git-refresh", Title: "Git: Refresh Status"},
		{ID: "git-focus", Title: "Git: Focus Source Control"},
		{ID: "git-push", Title: "Git: Push"},
		{ID: "git-pull", Title: "Git: Pull"},
		{ID: "git-branch", Title: "Git: Switch Branch..."},
		{ID: "problems", Title: "View: Problems"},
		{ID: "code-actions", Title: "Edit: Quick Fix...", Hint: "Ctrl+. / Alt+Enter"},
		{ID: "goto-symbol-file", Title: "Go to Symbol in File...", Hint: "Ctrl+Shift+O"},
		{ID: "goto-symbol-workspace", Title: "Go to Symbol in Workspace..."},
		{ID: "bookmark-toggle", Title: "Bookmarks: Toggle on Current Line", Hint: "F3"},
		{ID: "bookmark-list", Title: "Bookmarks: Show List", Hint: "Shift+F3"},
		{ID: "bookmark-clear", Title: "Bookmarks: Clear All"},
		{ID: "goto-line", Title: "Go to Line...", Hint: "Ctrl+G"},
		{ID: "split-vertical", Title: "Editor: Split Right", Hint: "Ctrl+\\"},
		{ID: "split-horizontal", Title: "Editor: Split Down"},
		{ID: "close-split", Title: "Editor: Close Split"},
		{ID: "format-document", Title: "Edit: Format Document", Hint: "Shift+Alt+F"},
		{ID: "open-recent", Title: "File: Open Recent...", Hint: "Ctrl+R"},
		{ID: "open-workspace", Title: "File: Open Recent Workspace..."},
		{ID: "workspace-add-folder", Title: "Workspace: Add Folder..."},
		{ID: "workspace-remove-folder", Title: "Workspace: Remove Folder..."},
		{ID: "toggle-wrap", Title: "View: Toggle Word Wrap", Hint: "Alt+W"},
		{ID: "trim-whitespace", Title: "Edit: Trim Trailing Whitespace"},
		{ID: "reveal-fs", Title: "File: Reveal in File System"},
		{ID: "diff-files", Title: "File: Compare Two Files..."},
		{ID: "git-stash", Title: "Git: Stash Changes"},
		{ID: "git-stash-pop", Title: "Git: Pop Latest Stash"},
		{ID: "git-stash-list", Title: "Git: Show Stash List..."},
		{ID: "sort-asc", Title: "Edit: Sort Lines Ascending"},
		{ID: "sort-desc", Title: "Edit: Sort Lines Descending"},
		{ID: "reverse-lines", Title: "Edit: Reverse Lines"},
		{ID: "uppercase", Title: "Edit: Convert to UPPERCASE"},
		{ID: "lowercase", Title: "Edit: Convert to lowercase"},
		{ID: "join-lines", Title: "Edit: Join Lines"},
		{ID: "alternate-file", Title: "View: Switch to Last File", Hint: "Ctrl+Tab"},
		{ID: "next-diagnostic", Title: "Go to Next Problem", Hint: "F5"},
		{ID: "prev-diagnostic", Title: "Go to Previous Problem", Hint: "Shift+F5"},
		{ID: "toggle-hidden", Title: "Files: Toggle Hidden Files"},
		{ID: "cheat-sheet", Title: "Help: Show Shortcuts"},
		{ID: "buffer-info", Title: "View: Show Buffer Info", Hint: "Ctrl+Alt+I"},
		{ID: "error-log", Title: "Help: Show Error Log"},
		{ID: "hover", Title: "LSP: Show Hover Documentation"},
		{ID: "find-references", Title: "LSP: Find All References", Hint: "Shift+F12"},
		{ID: "replace-workspace", Title: "Search: Replace in Workspace...", Hint: "Ctrl+Shift+H"},
		{ID: "goto-type-def", Title: "LSP: Go to Type Definition", Hint: "Ctrl+F12"},
		{ID: "goto-impl", Title: "LSP: Go to Implementation"},
		{ID: "run-tests", Title: "Run: Tests", Hint: "Alt+T"},
		{ID: "git-log", Title: "Git: Show Log..."},
		{ID: "git-compare-rev", Title: "Git: Compare with Revision..."},
		{ID: "git-blame", Title: "Git: Blame Current Line", Hint: "Alt+B"},
		{ID: "git-history", Title: "Git: Show File History..."},
		{ID: "git-tags", Title: "Git: Show Tags..."},
		{ID: "zen-mode", Title: "View: Toggle Zen Mode", Hint: "Alt+Z"},
		{ID: "reload-window", Title: "Developer: Reload Window"},
		{ID: "reset-session", Title: "Developer: Reset Session (clears tabs / cursor history)"},
		{ID: "toggle-auto-save", Title: "File: Toggle Auto Save"},
		{ID: "next-hunk", Title: "Git: Go to Next Change", Hint: "Alt+]"},
		{ID: "prev-hunk", Title: "Git: Go to Previous Change", Hint: "Alt+["},
		{ID: "reload-buffer", Title: "File: Revert / Reload from Disk"},
		{ID: "open-url", Title: "View: Open URL on Current Line"},
		{ID: "pin-tab", Title: "Tab: Toggle Pinned", Hint: "Alt+P"},
		{ID: "copy-file-ref", Title: "Copy: File:Line Reference"},
		{ID: "snippet-picker", Title: "Snippets: Browse..."},
		{ID: "tabs-to-spaces", Title: "Edit: Convert Tabs to Spaces"},
		{ID: "spaces-to-tabs", Title: "Edit: Convert Spaces to Tabs"},
		{ID: "trim-blanks", Title: "Edit: Collapse Blank Lines"},
		{ID: "config-commands", Title: "Open Config: User Commands"},
		{ID: "config-theme", Title: "Open Config: Theme"},
		// Debug Adapter Protocol. F-key shortcuts (F9 / F10 / F11 / Shift+F11)
		// are exposed via the Hint column so the palette doubles as a
		// discoverability surface. Continue / Start / Stop have no shortcut.
		{ID: "debug-start", Title: "Debug: Start"},
		{ID: "debug-stop", Title: "Debug: Stop"},
		{ID: "debug-toggle-breakpoint", Title: "Debug: Toggle Breakpoint", Hint: "F9"},
		{ID: "debug-step-over", Title: "Debug: Step Over", Hint: "F10"},
		{ID: "debug-step-into", Title: "Debug: Step Into", Hint: "F11"},
		{ID: "debug-step-out", Title: "Debug: Step Out", Hint: "Shift+F11"},
		{ID: "debug-continue", Title: "Debug: Continue"},
		{ID: "debug-show-stack", Title: "Debug: Show Call Stack"},
		{ID: "debug-show-vars", Title: "Debug: Show Variables"},
		{ID: "open-settings-ui", Title: "Preferences: Open Settings (UI)"},
		{ID: "edit-custom-theme", Title: "Preferences: Edit Custom Theme"},
		{ID: "customize-keybindings", Title: "Preferences: Customize Keybindings"},
		{ID: "quit", Title: "File: Quit", Hint: "Ctrl+Q"},
	}
}

// dispatchPaletteAction performs the action selected in the palette.
// Returns an optional tea.Cmd.
func (m *Model) dispatchPaletteAction(id string) tea.Cmd {
	// Record this action in the recent-palette list so next time it floats
	// to the top. Best-effort; failures don't block the dispatch.
	pushPaletteRecent(id)
	switch id {
	case "save":
		if err := m.editor.Save(); err != nil {
			m.err = err.Error()
		}
	case "save-all":
		if m.nvim != nil {
			_ = m.nvim.Command(":wall")
		}
	case "close-buffer":
		if m.activeBuf != 0 && m.promptCloseBufferIfDirty(m.activeBuf) {
			return nil
		}
		if buf, ok := m.findBuffer(m.activeBuf); ok {
			m.pushClosed(buf.Path)
		}
		if m.nvim != nil {
			_ = m.nvim.Command("bdelete")
		}
	case "reopen-closed":
		return m.reopenClosed()
	case "clipboard-history":
		return m.openClipboardPicker()
	case "next-buffer":
		if m.nvim != nil {
			_ = m.nvim.Command("bnext")
		}
	case "prev-buffer":
		if m.nvim != nil {
			_ = m.nvim.Command("bprevious")
		}
	case "toggle-explorer":
		m.showExp = !m.showExp
		m.applyLayout()
	case "toggle-actions-panel":
		m.toggleActionsPanel()
		m.applyLayout()
	case "focus-explorer":
		m.focus = FocusExplorer
	case "focus-editor":
		m.focus = FocusEditor
	case "reveal-file":
		m.revealActiveFileInExplorer()
	case "open-shell":
		return openShellCmd()
	case "toggle-terminal":
		m.toggleTerminalPanel()
		m.applyLayout()
	case "terminal-new-tab":
		m.newTerminalTab()
		m.applyLayout()
		m.resizeTerminalSplit()
	case "terminal-close-tab":
		if m.termOpen && len(m.terminalTabs) > 0 {
			m.closeTerminalTab(m.terminalActiveTab)
			m.applyLayout()
			m.resizeTerminalSplit()
		}
	case "terminal-next-tab":
		if m.termOpen {
			m.cycleTerminalTab(1)
		}
	case "terminal-prev-tab":
		if m.termOpen {
			m.cycleTerminalTab(-1)
		}
	case "terminal-maximize":
		if m.termOpen {
			m.maximizeTerminalPanel()
		}
	case "terminal-minimize":
		if m.termOpen {
			m.minimizeTerminalPanel()
		}
	case "quick-open":
		m.picker = picker.NewWith(" Go to file ", func() []picker.Item { return loadFiles() })
		m.picker.SetSize(m.w, m.h)
		m.pickerOpen = true
		m.pickerKind = pickerKindFiles
		return m.picker.Init()
	case "duplicate-line":
		// Forward to nvim's <M-S-Down> binding (defined in lsp_lua.go), which
		// duplicates the current line in normal/insert/visual modes.
		if m.nvim != nil {
			_ = m.nvim.Input("<M-S-Down>")
		}
	case "toggle-comment":
		m.toggleLineComment()
	case "fold-all":
		if m.nvim != nil {
			_ = m.nvim.Command("set foldlevel=0")
		}
	case "unfold-all":
		if m.nvim != nil {
			_ = m.nvim.Command("set foldlevel=99")
		}
	case "toggle-fold":
		// `za` is the vim-native "toggle fold under cursor" normal-mode action.
		// Use Input with a normal-mode escape so it works regardless of which
		// mode the user is currently in (insert vs normal).
		if m.nvim != nil {
			_ = m.nvim.Command("normal! za")
		}
	case "preview-md":
		return m.openPreviewCmd()
	case "find-files":
		m.openFindInFilesPrompt()
		return nil
	case "find-files-overlay":
		m.search = search.New()
		m.search.SetSize(m.w, m.h)
		m.search.SetRoots(m.explorer.Roots())
		m.searchOpen = true
	case "replace-in-file":
		// Unified findbar in expanded (Replace) mode — mirrors Ctrl+H.
		if !m.findOpen {
			m.find = findbar.New()
			m.findOpen = true
		}
		m.find.EnableReplace(true)
		m.applyLayout()
	case "toggle-inlay-hints":
		m.toggleInlayHints()
	case "pick-theme":
		return m.openThemePicker()
	case "git-stage":
		return m.gitStageActiveFile()
	case "git-stage-all":
		return m.gitStageAll()
	case "git-commit":
		m.openCommitPrompt()
	case "git-diff":
		return m.gitDiffActiveFile()
	case "git-discard":
		m.openGitDiscardForActiveFile()
	case "git-refresh":
		return fetchGitCmd()
	case "git-focus":
		m.activity.SetActive(activity.ViewGit)
		m.showExp = true
		m.focus = FocusExplorer
		m.applyLayout()
	case "git-push":
		return m.gitPush()
	case "git-pull":
		return m.gitPull()
	case "git-branch":
		return m.openBranchPicker()
	case "problems":
		return m.openProblemsPicker()
	case "code-actions":
		return m.fetchCodeActionsCmd()
	case "goto-symbol-file":
		return m.fetchSymbolsCmd()
	case "goto-symbol-workspace":
		return m.fetchWorkspaceSymbolsCmd()
	case "bookmark-toggle":
		return m.toggleBookmark()
	case "bookmark-list":
		return m.fetchBookmarksCmd()
	case "bookmark-clear":
		return m.clearAllBookmarks()
	case "goto-line":
		m.openGotoLinePrompt()
	case "split-vertical":
		if m.nvim != nil {
			_ = m.nvim.Command("vsplit")
		}
	case "split-horizontal":
		if m.nvim != nil {
			_ = m.nvim.Command("split")
		}
	case "close-split":
		if m.nvim != nil {
			_ = m.nvim.Command("close")
		}
	case "format-document":
		return m.formatDocumentCmd()
	case "open-recent":
		return m.openRecentFilePicker()
	case "open-workspace":
		return m.openWorkspacePicker()
	case "workspace-add-folder":
		m.openAddRootPrompt()
	case "workspace-remove-folder":
		return m.openRemoveRootPicker()
	case "toggle-wrap":
		return m.toggleWordWrap()
	case "trim-whitespace":
		return m.trimTrailingWhitespace()
	case "reveal-fs":
		return m.revealInFileSystem()
	case "diff-files":
		return m.openDiffFilesFlow()
	case "git-stash":
		return m.gitStash()
	case "git-stash-pop":
		return m.gitStashPop()
	case "git-stash-list":
		return m.openStashPicker()
	case "sort-asc":
		return m.sortLinesAsc()
	case "sort-desc":
		return m.sortLinesDesc()
	case "reverse-lines":
		return m.reverseLines()
	case "uppercase":
		return m.uppercaseSelection()
	case "lowercase":
		return m.lowercaseSelection()
	case "join-lines":
		return m.joinLines()
	case "alternate-file":
		m.alternateFile()
	case "next-diagnostic":
		m.gotoNextDiagnostic()
	case "prev-diagnostic":
		m.gotoPrevDiagnostic()
	case "toggle-hidden":
		return m.toggleHiddenFiles()
	case "cheat-sheet":
		m.openCheatSheet()
	case "buffer-info":
		return m.openBufferInfo()
	case "error-log":
		return m.openErrorLog()
	case "hover":
		return m.hoverCmd()
	case "find-references":
		return m.fetchReferencesCmd()
	case "replace-workspace":
		m.openReplaceInWorkspacePrompt()
	case "goto-type-def":
		return m.gotoTypeDefCmd()
	case "goto-impl":
		return m.gotoImplementationCmd()
	case "run-tests":
		return m.runTestsCmd()
	case "git-log":
		return m.gitLogPicker()
	case "git-compare-rev":
		m.openCompareWithRevisionPrompt()
	case "git-blame":
		return m.gitBlameCurrentLine()
	case "git-history":
		return m.gitFileHistoryPicker()
	case "git-tags":
		return m.gitTagPicker()
	case "zen-mode":
		return m.toggleZenMode()
	case "reload-window":
		return m.reloadWindow()
	case "reset-session":
		return m.resetSession()
	case "toggle-auto-save":
		return m.toggleAutoSave()
	case "next-hunk":
		return m.gotoNextHunk()
	case "prev-hunk":
		return m.gotoPrevHunk()
	case "reload-buffer":
		return m.reloadBuffer()
	case "open-url":
		return m.openURLUnderCursor()
	case "pin-tab":
		return m.togglePinActiveTab()
	case "copy-file-ref":
		return m.copyFileLineRef()
	case "snippet-picker":
		return m.openSnippetPicker()
	case "tabs-to-spaces":
		return m.tabsToSpaces()
	case "spaces-to-tabs":
		return m.spacesToTabs()
	case "trim-blanks":
		return m.trimBlankLines()
	case "config-commands":
		return m.editConfigFile("commands.json", defaultCommandsJSON)
	case "config-theme":
		return m.editConfigFile("config.json", defaultThemeConfigJSON)
	case "debug-start":
		return m.dapStart()
	case "debug-stop":
		return m.dapStop()
	case "debug-toggle-breakpoint":
		return m.dapToggleBreakpoint()
	case "debug-step-over":
		return m.dapStepOver()
	case "debug-step-into":
		return m.dapStepInto()
	case "debug-step-out":
		return m.dapStepOut()
	case "debug-continue":
		return m.dapContinue()
	case "debug-show-stack":
		return m.dapShowStack()
	case "debug-show-vars":
		return m.dapShowVars()
	case "open-settings-ui":
		return m.openSettingsPicker()
	case "edit-custom-theme":
		return m.openThemeEditor()
	case "customize-keybindings":
		return m.openKeybindingPicker()
	default:
		// User commands have IDs prefixed with "user-".
		if strings.HasPrefix(id, "user-") {
			return m.runUserCommand(id)
		}
	case "quit":
		if m.nvim != nil {
			_ = m.nvim.Close()
		}
		return tea.Quit
	}
	return nil
}

// applyTheme switches the runtime palette and composite styles to the given
// theme ID and persists the choice. Persistence is best-effort: a write
// failure surfaces in m.err but does not block the swap (the user still sees
// the new colors for the rest of the session). Components that read directly
// from the package-level palette vars (statusbar, explorer, tabbar, …) pick
// up the new colors on their next render automatically.
func (m *Model) applyTheme(id string) {
	t, _ := theme.ApplyTheme(id)
	m.theme = t.Styles
	if err := theme.SaveThemeID(t.ID); err != nil {
		m.err = "theme: " + err.Error()
	}
}

// openClipboardPicker pops a sub-picker listing entries in the in-memory
// clipboard ring. Selection pastes the chosen entry at the editor cursor.
func (m *Model) openClipboardPicker() tea.Cmd {
	entries := m.clipboardPickerItems()
	if len(entries) == 0 {
		return nil
	}
	items := make([]picker.Item, 0, len(entries))
	for _, e := range entries {
		items = append(items, picker.Item{ID: fmt.Sprintf("clip-%d", e.Index), Title: e.Preview})
	}
	m.picker = picker.NewItems(" Clipboard History ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindClipboard
	return m.picker.Init()
}

// openThemePicker pops a sub-picker listing every built-in theme. Selection is
// dispatched in handlePickerSelect via pickerKindTheme. Each Item.ID is the
// theme's stable persistent identifier; Item.Hint shows " ✓" on the active
// theme so the user can see what's currently applied without reading the
// config file.
func (m *Model) openThemePicker() tea.Cmd {
	themes := theme.AvailableThemes()
	active := theme.LoadThemeID()
	if active == "" {
		active = "vscode-dark-plus"
	}
	items := make([]picker.Item, 0, len(themes))
	for _, t := range themes {
		hint := ""
		if t.ID == active {
			hint = "active"
		}
		items = append(items, picker.Item{ID: t.ID, Title: t.Name, Hint: hint})
	}
	m.picker = picker.NewItems(" Color Theme ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindTheme
	return m.picker.Init()
}
