package app

// Docs — the user documentation site and the pure, side-effect-free
// catalogs that cmd/docgen turns into docs/reference/*.md. Nothing here
// starts nvim or a UI. When you add a key, palette command, setting or
// config file, update the matching list and run `go run ./cmd/docgen`.

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/toast"
)

// DocsURL is the user documentation site.
const DocsURL = "https://amin-jalali.github.io/termocode/"

// docsHost is DocsURL without scheme and trailing slash, for short hints.
const docsHost = "amin-jalali.github.io/termocode"

// openDocs opens DocsURL in the system browser ("Help: Open
// Documentation"). If no browser can be started, the URL is shown in a
// toast so the user can copy it.
func (m *Model) openDocs() tea.Cmd {
	var c tea.Cmd
	if err := osOpen(DocsURL); err != nil {
		m.toast, c = m.toast.PushDetail(toast.Warn, "Open the docs in your browser", DocsURL)
		return c
	}
	m.toast, c = m.toast.PushDetail(toast.Info, "Opened", DocsURL)
	return c
}

// ── Palette ─────────────────────────────────────────────────────────────

// PaletteEntry is one static command palette row.
type PaletteEntry struct {
	ID    string
	Title string
	// Hint is the text shown on the right of the palette row.
	Hint string
	// Keys are the default keymap keys (Bubble Tea strings) of the
	// command's action, sorted. Empty when the command has no key.
	Keys []string
}

// paletteActions maps a palette ID to the keymap Action that does the same
// thing, so the reference can show the real default key next to the
// command (the palette Hint is free text and can drift).
var paletteActions = map[string]keymap.Action{
	"save":                    keymap.ActionSave,
	"save-all":                keymap.ActionSaveAll,
	"close-buffer":            keymap.ActionCloseBuffer,
	"reopen-closed":           keymap.ActionReopenClosed,
	"clipboard-history":       keymap.ActionClipboardHistory,
	"next-buffer":             keymap.ActionNextBuffer,
	"prev-buffer":             keymap.ActionPrevBuffer,
	"toggle-explorer":         keymap.ActionToggleExplorer,
	"toggle-actions-panel":    keymap.ActionToggleActionsPanel,
	"focus-explorer":          keymap.ActionFocusExplorer,
	"focus-editor":            keymap.ActionFocusEditor,
	"reveal-file":             keymap.ActionRevealFile,
	"toggle-terminal":         keymap.ActionToggleTerminal,
	"open-shell":              keymap.ActionOpenShell,
	"terminal-new-tab":        keymap.ActionTerminalNewTab,
	"terminal-close-tab":      keymap.ActionTerminalCloseTab,
	"terminal-next-tab":       keymap.ActionTerminalNextTab,
	"terminal-prev-tab":       keymap.ActionTerminalPrevTab,
	"quick-open":              keymap.ActionQuickOpen,
	"duplicate-line":          keymap.ActionDuplicateLine,
	"toggle-comment":          keymap.ActionToggleComment,
	"preview-md":              keymap.ActionMarkdownPreview,
	"find-files":              keymap.ActionWorkspaceSearch,
	"replace-in-file":         keymap.ActionReplaceInFile,
	"toggle-inlay-hints":      keymap.ActionToggleInlayHints,
	"pick-theme":              keymap.ActionPickTheme,
	"git-stage-hunk":          keymap.ActionStageHunk,
	"git-unstage-hunk":        keymap.ActionUnstageHunk,
	"git-discard-hunk":        keymap.ActionDiscardHunk,
	"code-actions":            keymap.ActionCodeActions,
	"goto-symbol-file":        keymap.ActionGotoSymbolInFile,
	"bookmark-toggle":         keymap.ActionToggleBookmark,
	"bookmark-list":           keymap.ActionShowBookmarks,
	"goto-line":               keymap.ActionGotoLine,
	"split-vertical":          keymap.ActionSplitVertical,
	"split-horizontal":        keymap.ActionSplitHorizontal,
	"close-split":             keymap.ActionCloseSplit,
	"format-document":         keymap.ActionFormatDocument,
	"open-recent":             keymap.ActionOpenRecent,
	"toggle-wrap":             keymap.ActionToggleWordWrap,
	"alternate-file":          keymap.ActionAlternateFile,
	"next-diagnostic":         keymap.ActionNextDiagnostic,
	"prev-diagnostic":         keymap.ActionPrevDiagnostic,
	"toggle-hidden":           keymap.ActionToggleHidden,
	"cheat-sheet":             keymap.ActionShowCheatSheet,
	"buffer-info":             keymap.ActionShowBufferInfo,
	"hover":                   keymap.ActionHover,
	"find-references":         keymap.ActionFindReferences,
	"replace-workspace":       keymap.ActionReplaceInWorkspace,
	"goto-type-def":           keymap.ActionGotoTypeDef,
	"goto-impl":               keymap.ActionGotoImplementation,
	"run-tests":               keymap.ActionRunTests,
	"testing-run-all":         keymap.ActionRunTests,
	"git-blame":               keymap.ActionGitBlameLine,
	"git-history":             keymap.ActionFileHistory,
	"zen-mode":                keymap.ActionToggleZenMode,
	"reload-window":           keymap.ActionReloadWindow,
	"toggle-auto-save":        keymap.ActionToggleAutoSave,
	"next-hunk":               keymap.ActionNextHunk,
	"prev-hunk":               keymap.ActionPrevHunk,
	"reload-buffer":           keymap.ActionReloadBuffer,
	"open-url":                keymap.ActionOpenURL,
	"pin-tab":                 keymap.ActionPinTab,
	"copy-file-ref":           keymap.ActionCopyFileRef,
	"snippet-picker":          keymap.ActionShowSnippetPicker,
	"debug-start":             keymap.ActionDebugStartContinue,
	"debug-stop":              keymap.ActionDebugStop,
	"debug-toggle-breakpoint": keymap.ActionDebugToggleBreakpoint,
	"debug-step-over":         keymap.ActionDebugStepOver,
	"debug-step-into":         keymap.ActionDebugStepInto,
	"debug-step-out":          keymap.ActionDebugStepOut,
	"tasks-run":               keymap.ActionRunTask,
	"tasks-rerun":             keymap.ActionRerunTask,
	"problems-panel":          keymap.ActionShowProblems,
	"ai-open-chat":            keymap.ActionToggleActionsPanel,
	"ai-trigger-inline":       keymap.ActionAITriggerInline,
	"ai-edit":                 keymap.ActionAIEditSelection,
	"ai-toggle-inline":        keymap.ActionAIToggleInline,
	"quit":                    keymap.ActionQuit,
}

// PaletteCatalog returns every static command palette row (the same rows
// the palette shows, minus user commands from commands.json and "Ext:"
// commands from extensions, which only exist at run time), with the
// default keys of each row's action.
func PaletteCatalog() []PaletteEntry {
	km := keymap.Default()
	items := append(paletteItems(), tasksPaletteItems()...)
	out := make([]PaletteEntry, 0, len(items))
	for _, it := range items {
		e := PaletteEntry{ID: it.ID, Title: it.Title, Hint: it.Hint}
		if a, ok := paletteActions[it.ID]; ok {
			e.Keys = km.KeysFor(a)
		}
		out = append(out, e)
	}
	return out
}

// PaletteActionFor returns the keymap Action a palette ID runs, if any.
func PaletteActionFor(id string) (keymap.Action, bool) {
	a, ok := paletteActions[id]
	return a, ok
}

// ── Settings ────────────────────────────────────────────────────────────

// SettingEntry is one config.json key.
type SettingEntry struct {
	Key         string
	Type        string
	Default     string
	Description string
	// Category is the Settings modal group ("Appearance", "Editor", …).
	// Keys that only exist in the file use "<group> (file only)".
	Category string
}

// fileOnlySettings are config.json keys with no row in the Settings modal.
// They are set by other UI (named in the description) or by hand.
var fileOnlySettings = []SettingEntry{
	{Key: "ai_inline_model", Type: "string", Default: "(provider default)", Category: "AI (file only)",
		Description: "Model for inline ghost-text completions. Empty uses a fast model of the provider (for example Claude Haiku or gpt-4.1-mini)."},
	{Key: "ai_auto_approve", Type: "string[]", Default: "[]", Category: "AI (file only)",
		Description: "Agent tool classes that run without asking: " + strings.Join(ai.ToolClasses, ", ") + ". Set with AI: Agent Auto-Approve…."},
}

// SettingsCatalog returns every config.json setting in Settings modal
// order (grouped by category), then the file-only keys.
func SettingsCatalog() []SettingEntry {
	cats := settingsCategories()
	var order []string
	byCat := map[string][]SettingEntry{}
	for _, r := range settingsRows {
		c, ok := cats[r.ID]
		if !ok {
			c = "Editor" // same fallback as newSettingsModal
		}
		if _, seen := byCat[c]; !seen {
			order = append(order, c)
		}
		byCat[c] = append(byCat[c], SettingEntry{
			Key: r.Label, Type: r.Kind, Default: r.Default, Description: r.Desc, Category: c,
		})
	}
	var out []SettingEntry
	for _, c := range order {
		out = append(out, byCat[c]...)
	}
	return append(out, fileOnlySettings...)
}

// EnvVar is one environment variable termocode reads.
type EnvVar struct{ Name, Description string }

// EnvCatalog lists the environment variables users can set, sorted.
func EnvCatalog() []EnvVar {
	out := []EnvVar{
		{"XDG_CONFIG_HOME", "Moves the config folder to `$XDG_CONFIG_HOME/termocode`."},
		{"XDG_DATA_HOME", "Moves the data folder to `$XDG_DATA_HOME/termocode`."},
		{"TERMOCODE_FONT_DELTA", "Font size change asked of the terminal at launch (default -2, 0 turns it off)."},
		{"TERMOCODE_ICON_MODE", "Icon set: `nerd_font`, `unicode` or `ascii`."},
		{"TERMOCODE_NERD_FONT", "Legacy: `1` is the same as `TERMOCODE_ICON_MODE=nerd_font`."},
		{"TERMOCODE_PLUGINS_DIR", "Folder for the Neovim plugins termocode clones on first launch."},
		{"TERMOCODE_TOOLS_DIR", "Folder for language servers and debug adapters installed by termocode."},
		{"TERMOCODE_AI_PROVIDER", "AI provider; wins over `ai_provider` in config.json."},
		{"TERMOCODE_AI_MODEL", "AI chat model; wins over `ai_model`."},
		{"TERMOCODE_AI_BASE_URL", "AI API base URL; wins over `ai_base_url`."},
		{"TERMOCODE_AI_API_KEY", "AI API key for the selected provider; wins over saved keys."},
		{"ANTHROPIC_API_KEY", "Anthropic API key. Also selects Anthropic when no provider is set."},
		{"OPENAI_API_KEY", "OpenAI API key. Also selects OpenAI when no provider is set."},
		{"OPENROUTER_API_KEY", "OpenRouter API key."},
		{"OLLAMA_HOST", "Ollama host, used for the base URL of the ollama provider."},
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ── Config files ────────────────────────────────────────────────────────

// ConfigFile is one file or folder termocode reads or writes.
type ConfigFile struct {
	// Location is "config", "data", "workspace" or "other".
	Location string
	// Path is relative to the location's folder (or absolute-ish for
	// "other", e.g. "~/termocode-settings.json").
	Path    string
	Purpose string
	Format  string
	// Edit says whether hand edits are safe, and how else to change it.
	Edit string
}

// ConfigFileCatalog lists every file termocode reads or writes, in a
// fixed order (location, then path).
func ConfigFileCatalog() []ConfigFile {
	out := []ConfigFile{
		{"config", "config.json", "Settings (theme, editor, search, AI). See the settings reference.",
			"JSON object. Unknown keys are kept on save.", "Yes. Also Preferences: Open Settings (UI)."},
		{"config", "keymap.json", "Key binding overrides. See the keys reference.",
			"JSON object: key string → action name, or `\"ext:<command id>\"` for an extension command.",
			"Yes. Also Preferences: Customize Keybindings."},
		{"config", "commands.json", "User commands shown in the palette as `User: <title>`.",
			"JSON array of `{\"id\", \"title\", \"cmd\", \"cwd\"}`.", "Yes. Open with Open Config: User Commands."},
		{"config", userSnippetsFile, "User snippets, layered over the built-in ones.",
			"JSON object: scope (filetype or `*`) → trigger → body (string or array of lines).",
			"Yes. Also Snippets: Manage…."},
		{"config", "user_theme.json", "Custom color theme saved by the theme editor.",
			"JSON object: palette key (for example `BgEditor`) → hex color. Used by the `custom` theme.",
			"Yes, but Preferences: Edit Custom Theme is easier."},
		{"config", "breakpoints.json", "Saved debugger breakpoints per file (line and line text).",
			"JSON `{\"version\", \"breakpoints\": {file: [...]}}`.", "Not needed. termocode rewrites it when breakpoints change."},
		{"config", "session.json", "Open files, tab order, cursor positions, expanded folders, workspace roots.",
			"JSON, written by termocode.", "No (state). Developer: Reset Session clears it and keeps `session.json.bak`."},
		{"config", "recents.json", "Recent files for the welcome screen and File: Open Recent….",
			"JSON array.", "No (state). Safe to delete."},
		{"config", "workspaces.json", "Recent workspace folders for File: Open Recent Workspace….",
			"JSON array.", "No (state). Safe to delete."},
		{"config", "palette_recents.json", "Recently used palette commands (shown first).",
			"JSON array of command IDs.", "No (state). Safe to delete."},
		{"config", "errors.log", "Log of error and warning toasts. `tail -f` it while debugging.",
			"Plain text, one timestamped line per entry; secrets are redacted.", "Safe to delete."},
		{"config", "settings-backup-<time>.json", "Backup written before Preferences: Import Settings….",
			"Settings bundle (same shape as an export).", "Safe to delete."},
		{"config", "extensions/<name>/", "Lua extensions: `extension.json` manifest and `init.lua`.",
			"Folder per extension.", "Yes. Extensions: New Extension… creates one."},
		{"config", "ai/credentials.json", "AI API keys and Claude OAuth tokens. File mode 0600.",
			"JSON `{\"keys\": {provider: key}, \"claude_oauth\": {...}}`.",
			"With care. Prefer AI: Configure Provider…. Never exported by Preferences: Export Settings…."},
		{"config", "ai/chats/*.json", "Saved AI chats (newest 50 kept).",
			"JSON, one file per chat.", "Safe to delete."},
		{"data", "plugins/", "Neovim plugins cloned on first launch (nvim-dap, vim-visual-multi).",
			"Git checkouts. `$TERMOCODE_PLUGINS_DIR` moves it.", "Delete to re-clone on next launch."},
		{"data", "tools/", "Language servers and debug adapters installed by LSP: Manage Language Servers… and DAP: Install Adapter….",
			"One folder per tool; `tools/bin` holds shims on Neovim's PATH. `$TERMOCODE_TOOLS_DIR` moves it.",
			"Use the palette commands. Delete a tool folder to remove it."},
		{"data", "tools/asked.json", "Filetypes already asked about in the \"no language server\" toast.",
			"JSON `{\"filetypes\": [...]}`.", "Safe to delete (the toasts come back)."},
		{"workspace", ".termocode/tasks.json", "Workspace tasks for Tasks: Run Task….",
			"VS Code style tasks: `label`, `command`, `args`, `cwd`, `env`, `group`, `problemMatcher`.",
			"Yes. Tasks: Configure Tasks creates it."},
		{"workspace", launchJSONRel, "Debug and run configurations.",
			"VS Code `launch.json` shape; comments allowed.", "Yes. Debug: Open launch.json creates it."},
		{"workspace", ".editorconfig", "Indent and whitespace rules, read by Neovim's EditorConfig support.",
			"EditorConfig INI.", "Yes (read only for termocode)."},
		{"other", "~/termocode-settings.json", "Default target of Preferences: Export Settings….",
			"Settings bundle: `{\"termocode_settings\": 1, \"files\": {...}}`. State files and secrets are left out.",
			"Yes, but re-export is easier."},
		{"other", "~/.local/share/fonts/JetBrainsMonoNF/", "Nerd Font installed by `termocode setup` (Linux).",
			"Font files.", "Safe to delete."},
	}
	return out
}

// ── Neovim-side keys ────────────────────────────────────────────────────

// EditorKey is a key handled inside Neovim (lsp_lua.go, multi_cursor.go,
// git_conflicts.go, …) rather than by the keymap package. These can not
// be changed in keymap.json.
type EditorKey struct {
	Key, Mode, Description string
}

// EditorKeyCatalog lists the Neovim-side keys termocode sets up.
func EditorKeyCatalog() []EditorKey {
	return []EditorKey{
		{"F12 / gd", "Normal", "Go to definition (LSP)."},
		{"gD", "Normal", "Go to declaration (LSP)."},
		{"gi", "Normal", "Go to implementation (LSP)."},
		{"gr / Shift+F12", "Normal", "List references (LSP)."},
		{"gt", "Normal", "Go to type definition (LSP)."},
		{"K", "Normal", "Hover documentation (LSP)."},
		{"F2", "Normal", "Rename symbol (LSP)."},
		{"Ctrl+Space", "Normal", "Signature help (LSP)."},
		{"Ctrl+Space", "Insert", "Open completion."},
		{"<leader>f", "Normal", "Format the buffer (LSP)."},
		{"Tab / Enter", "Insert", "Accept the completion item. Tab also accepts AI ghost text and jumps to the next snippet field."},
		{"Shift+Tab", "Insert", "Previous completion item, or previous snippet field."},
		{"Ctrl+Z / Ctrl+Y", "Insert", "Undo / redo."},
		{"Alt+Up / Alt+Down", "All", "Move the line or selection up / down."},
		{"Shift+Alt+Up / Shift+Alt+Down", "All", "Duplicate the line or selection up / down."},
		{"Ctrl+D", "Normal", "Multi-cursor: select the next match of the word."},
		{"Ctrl+Alt+Up / Ctrl+Alt+Down", "Normal, Insert", "Multi-cursor: add a cursor above / below."},
		{"]x / [x", "Normal", "Next / previous merge conflict."},
		{"F4 / Shift+F4", "Normal, Insert", "Next / previous Find in Files result. Note: today the app keys F4 (ToggleInlayHints) and Shift+F4 (ReopenClosed) catch these keys first."},
		{"Esc", "Insert", "Back to Normal mode — the editor is real Neovim."},
	}
}
