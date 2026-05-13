package keymap

import tea "github.com/charmbracelet/bubbletea"

type Action int

const (
	ActionNone Action = iota
	ActionQuit
	ActionSave
	ActionSaveAll
	ActionFocusExplorer
	ActionFocusEditor
	ActionFocusSwap
	ActionToggleExplorer
	ActionOpenShell
	ActionToggleTerminal
	ActionCloseBuffer
	ActionNextBuffer
	ActionPrevBuffer
	ActionQuickOpen
	ActionCommandPalette
	ActionFind
	ActionReplaceInFile
	ActionDuplicateLine
	ActionMarkdownPreview
	ActionWorkspaceSearch
	ActionRevealFile
	ActionReopenClosed
	ActionToggleInlayHints
	ActionPickTheme
	ActionClipboardHistory
	ActionToggleComment
	ActionCopy
	ActionPaste
	ActionCut
	ActionCodeActions
	ActionGotoSymbolInFile
	ActionToggleBookmark
	ActionShowBookmarks
	ActionGotoLine
	ActionSplitVertical
	ActionSplitHorizontal
	ActionCloseSplit
	ActionFormatDocument
	ActionOpenRecent
	ActionToggleWordWrap
	ActionAlternateFile
	ActionNextDiagnostic
	ActionPrevDiagnostic
	ActionToggleHidden
	ActionShowCheatSheet
	ActionHover
	ActionFindReferences
	ActionReplaceInWorkspace
	ActionGotoTypeDef
	ActionGotoImplementation
	ActionRunTests
	ActionGitBlameLine
	ActionFileHistory
	ActionToggleZenMode
	ActionReloadWindow
	ActionToggleAutoSave
	ActionNextHunk
	ActionPrevHunk
	ActionReloadBuffer
	ActionOpenURL
	ActionPinTab
	ActionCopyFileRef
	ActionShowSnippetPicker
	ActionMdLivePreview
	// Debug Adapter Protocol actions. ActionDebugToggleBreakpoint is bound to
	// F9 (terminal toggle moves to Ctrl+T only); ActionDebugStepOver / StepInto
	// / StepOut to F10 / F11 / Shift+F11. Continue / Start / Stop have no
	// universal hot-key — they live in the palette only.
	ActionDebugToggleBreakpoint
	ActionDebugStepOver
	ActionDebugStepInto
	ActionDebugStepOut
	// ActionShowBufferInfo opens a preview overlay listing the active
	// buffer's path, size, line count, filetype, encoding, indent, and
	// LSP attachment. Bound to Ctrl+Alt+I when the terminal forwards it
	// distinctly; always reachable via the palette ("View: Show Buffer
	// Info").
	ActionShowBufferInfo
	// ActionToggleActionsPanel flips the right-side Actions panel between
	// open (pinned column) and closed. Opening from a fully-closed state
	// also re-pins so the user lands in the full panel rather than just
	// the launcher chip. Default binding: Alt+A — Ctrl+Shift+A is taken
	// by terminals (select-all). Always reachable via the palette
	// ("View: Toggle Actions Panel").
	ActionToggleActionsPanel
	// Integrated-terminal multi-tab actions. ActionTerminalNewTab opens a
	// fresh tab in the panel (creating the panel if it isn't open yet);
	// ActionTerminalCloseTab closes the active tab — and the whole panel
	// when closing the last tab. ActionTerminalNextTab /
	// ActionTerminalPrevTab cycle the active tab (only when the panel is
	// open; they fall through otherwise so the existing
	// ActionNextBuffer/ActionPrevBuffer bindings still work for editor
	// buffers).
	ActionTerminalNewTab
	ActionTerminalCloseTab
	ActionTerminalNextTab
	ActionTerminalPrevTab
	// ActionApplyPreferredCodeAction applies the LSP-flagged "preferred"
	// code action at the cursor (when present) without opening the picker.
	// VSCode-style: the inline 💡 hint advertises this shortcut so users
	// can skip the picker for the common quickfix-driven flow. Default
	// binding: Ctrl+Shift+. (kitty/CSI-u terminals); palette is the
	// universal fallback ("Edit: Apply Preferred Quick Fix").
	ActionApplyPreferredCodeAction
)

type KeyMap struct {
	bindings map[string]Action
}

// Default returns the bindings that work reliably across virtually every
// terminal: plain Ctrl+letter keys plus a small set of function keys.
//
// Modifier keys that depend on the terminal's keyboard protocol (Ctrl+digit,
// Ctrl+Shift+letter, etc.) are intentionally excluded — they only work on a
// subset of terminals (kitty, wezterm, ghostty, modern alacritty). When the
// user has such a terminal, the corresponding nvim-side keymaps still fire
// through the editor's translateKey path; this map just covers the universal
// fallbacks.
func Default() KeyMap {
	return KeyMap{
		bindings: map[string]Action{
			// Quit / save. Ctrl+C is reserved for Copy (VSCode-style); quit
			// stays on Ctrl+Q. Bubble Tea catches Ctrl+C as a regular key
			// (no SIGINT) so we're free to repurpose it.
			"ctrl+q": ActionQuit,
			"ctrl+s": ActionSave,
			"ctrl+c": ActionCopy,
			"ctrl+v": ActionPaste,
			"ctrl+x": ActionCut,

			// Layout
			"ctrl+b": ActionToggleExplorer,
			"f6":     ActionFocusSwap,

			// Buffers
			"ctrl+w":      ActionCloseBuffer,
			"ctrl+pgdown": ActionNextBuffer,
			"ctrl+pgup":   ActionPrevBuffer,

			// Overlays
			"ctrl+p": ActionQuickOpen,
			"f1":     ActionCommandPalette,
			"ctrl+f": ActionFind,
			"ctrl+h": ActionReplaceInFile,

			// Editing
			//
			// Ctrl+D is intentionally NOT bound here: it is reserved for the
			// vim-visual-multi "Find Under" multi-cursor action (VSCode-style
			// "select next occurrence"). Leaving it unbound at this layer
			// lets the editor forward it to nvim where VM handles it.
			// Duplicate-line stays reachable via Shift+Alt+Down (see lsp_lua.go)
			// and via the command palette ("Edit: Duplicate Line").

			// Tools.
			//
			// Terminal toggle: Ctrl+T is the primary binding — every terminal
			// reports it distinctly and nothing in the host environment grabs
			// it. (Trade-off: nvim's insert-mode Ctrl+T = "increase indent" is
			// no longer reachable from the editor; users who need it can map
			// the same action to e.g. `>>` / Tab.) "ctrl+`" / "alt+`" remain
			// best-effort for terminals that forward them. F9 used to alias
			// the terminal toggle but now belongs to the debugger (toggle
			// breakpoint, VSCode-standard); Ctrl+T is the universal fallback.
			"ctrl+t": ActionToggleTerminal,
			"ctrl+`": ActionToggleTerminal,
			"alt+`":  ActionToggleTerminal,

			// Integrated-terminal multi-tab bindings. These route to the
			// terminal panel only when it's open AND the user's focus is
			// in the editor / terminal region — a no-op otherwise so the
			// keys don't shadow other bindings when the panel is closed.
			//
			// Ctrl+Shift+` is the VSCode default for "new terminal"; modern
			// terminals running the kitty/CSI-u protocol forward it as a
			// distinct chord. Legacy terminals fold it to plain Ctrl+`
			// (= ActionToggleTerminal) in which case the user still gets a
			// new tab via the palette command.
			"ctrl+shift+`": ActionTerminalNewTab,
			// Ctrl+Shift+W: close the active terminal tab. Best-effort
			// binding — many terminal emulators (gnome-terminal, kitty,
			// iTerm2) grab Ctrl+Shift+W for "close window" before it
			// reaches us. Palette ("Terminal: Close Tab") is the
			// universal fallback.
			"ctrl+shift+w": ActionTerminalCloseTab,
			// Ctrl+PageUp / Ctrl+PageDown are also bound to
			// ActionPrevBuffer / ActionNextBuffer; the dispatcher in
			// update.go routes to the terminal-tab cycle ONLY when the
			// terminal panel is open, otherwise falls through to the
			// editor-buffer cycle.
			"ctrl+shift+pgup":   ActionTerminalPrevTab,
			"ctrl+shift+pgdown": ActionTerminalNextTab,

			// Debug Adapter Protocol. F9 is the VSCode-standard breakpoint
			// toggle; F10 / F11 / Shift+F11 are the standard step-over /
			// step-into / step-out keys. F11 collides with most terminal
			// emulators' fullscreen toggle so F11 / Shift+F11 only reach the
			// app on terminals that don't intercept them — "Debug: Step Into"
			// and "Debug: Step Out" stay reachable via the palette regardless.
			// F5 is *intentionally* not bound here because it's already
			// ActionNextDiagnostic; "Debug: Continue" is palette-only.
			"f9":  ActionDebugToggleBreakpoint,
			"f10": ActionDebugStepOver,
			"f7":     ActionMarkdownPreview,
			"f8":    ActionWorkspaceSearch,
			// Ctrl+Shift+F (VSCode / Sublime default) is intercepted by
			// every common terminal emulator we've tested so it never
			// reaches the app. Alt+F is forwarded by iTerm2, wezterm,
			// kitty, ghostty — anywhere else it'll get eaten by the host
			// terminal's menu bar (gnome-terminal binds it to "File"),
			// in which case F8 / palette stay as fallbacks.
			"alt+f": ActionWorkspaceSearch,

			// LSP display toggles. F4 is free across all common terminals
			// (F2/F12 are taken by rename / goto-def in lsp_lua.go).
			"f4": ActionToggleInlayHints,

			// Navigation
			// ctrl+shift+e is best-effort: only terminals with the kitty/CSI-u
			// keyboard protocol report it distinctly. The same action remains
			// reachable via the command palette ("Reveal File in Explorer").
			"ctrl+shift+e": ActionRevealFile,

			// Buffer history
			// Reopen-closed-editor. Ctrl+Shift+T (the VSCode binding) gets
			// eaten by every terminal emulator for "new terminal tab", so we
			// can't use it. Bubble Tea reports Alt+Shift+T differently on
			// different terminals — sometimes "alt+shift+t", sometimes
			// "alt+T" (capital, shift implied), sometimes neither. We bind
			// every form *plus* a Shift+F4 fallback so at least one route
			// reaches the action. The palette ("View: Reopen Closed Editor")
			// always works.
			// Bubble Tea reports Reopen-closed-editor differently per
			// terminal — we bind every form we've seen so at least one
			// route works. Ctrl+Shift+T (VSCode default) and
			// Ctrl+Shift+W are deliberately NOT bound: every terminal
			// emulator we've tested grabs them for "new tab" / "close
			// tab" and they never reach us.
			//   modern terminals: alt+shift+t
			//   xterm folded:     alt+T   (capital T, shift implied)
			//   xterm Shift+F4:   f16     (xterm extra-function range)
			"alt+shift+t": ActionReopenClosed,
			"alt+T":       ActionReopenClosed,
			"shift+f4":    ActionReopenClosed,
			"f16":         ActionReopenClosed,
			// ctrl+shift+v: clipboard history picker (best-effort).
			"ctrl+shift+v": ActionClipboardHistory,

			// VSCode-style line-comment toggle. Most legacy terminals (xterm,
			// gnome-terminal, default macOS Terminal) report Ctrl+/ as the
			// ASCII control character US (0x1f), which Bubble Tea surfaces as
			// "ctrl+_". Modern terminals running the kitty/CSI-u keyboard
			// protocol (kitty, wezterm, ghostty) can report it as a distinct
			// "ctrl+/" — we bind both so the action fires either way.
			"ctrl+_": ActionToggleComment,
			"ctrl+/": ActionToggleComment,

			// Code Actions (Quick Fix). VSCode's primary binding is Ctrl+. but
			// most legacy terminals (xterm, gnome-terminal, default macOS
			// Terminal) don't report it distinctly — they fold Ctrl+. to a
			// plain ".". Modern terminals running the kitty/CSI-u keyboard
			// protocol forward it as "ctrl+.". Alt+Enter is the universal
			// VSCode-supported alternative and reaches us reliably across
			// every terminal we've tested. Both are bound; the action stays
			// reachable via the command palette ("Edit: Quick Fix").
			"ctrl+.":    ActionCodeActions,
			"alt+enter": ActionCodeActions,

			// Apply Preferred Quick Fix. When LSP marks an action with
			// `isPreferred=true` (typically the canonical quickfix for a
			// diagnostic), Ctrl+Shift+. applies it directly without the
			// picker. Most legacy terminals fold Ctrl+Shift+. to plain
			// "." or Ctrl+. — kitty/CSI-u terminals report it distinctly.
			// Falls back to the picker via Ctrl+. + Enter when the chord
			// doesn't reach us.
			"ctrl+shift+.": ActionApplyPreferredCodeAction,

			// Goto Symbol in File. VSCode binds this to Ctrl+Shift+O — which
			// only reaches us on terminals running the kitty/CSI-u keyboard
			// protocol (legacy terminals fold it to plain "ctrl+o"). We bind
			// "ctrl+shift+o" for capable terminals and don't bind plain
			// Ctrl+O because nvim uses it for the jumplist. Palette is the
			// universal fallback ("Go to Symbol in File...").
			"ctrl+shift+o": ActionGotoSymbolInFile,

			// Bookmarks. F3 toggles a bookmark on the current line; Shift+F3
			// (or palette) opens the picker. F-keys for these because every
			// terminal forwards them and they don't conflict with anything
			// nvim cares about by default. Underneath these ride on nvim's
			// global marks A-Z, so power users can also use the native
			// `mA` / `'A` syntax interchangeably.
			"f3":       ActionToggleBookmark,
			"shift+f3": ActionShowBookmarks,

			// Go to Line. Ctrl+G is the VSCode binding; terminals send it
			// as the BEL byte (0x07) which Bubble Tea reports as "ctrl+g".
			// nvim's normal-mode Ctrl+G shows file info, but at our global
			// keymap layer we intercept it before nvim sees it, so the only
			// effect of binding is the line-jump prompt.
			"ctrl+g": ActionGotoLine,

			// Split editor (VSCode bindings).
			//   Ctrl+\        : split right (vertical split)
			//   Ctrl+K Ctrl+\ : not supported (no chord support); use palette
			"ctrl+\\": ActionSplitVertical,

			// Format document on demand. VSCode's Shift+Alt+F. Most terminals
			// forward this distinctly.
			"shift+alt+f": ActionFormatDocument,

			// Open Recent File picker. VSCode binds this to Ctrl+R.
			"ctrl+r": ActionOpenRecent,

			// Quick wins:
			//   Ctrl+Tab        : alternate file (toggle between two recents)
			//   F5 / Shift+F5   : next / previous diagnostic in current buffer
			//   Ctrl+H          : was Replace-In-File; we keep that, so the
			//                     hidden-files toggle goes to the palette only.
			"ctrl+tab": ActionAlternateFile,
			"f5":       ActionNextDiagnostic,
			"shift+f5": ActionPrevDiagnostic,

			// LSP power moves:
			//   K               : hover documentation (vim's standard)
			//   shift+f12       : find references picker (VSCode standard)
			//   ctrl+shift+h    : replace in workspace
			"shift+f12":    ActionFindReferences,
			"ctrl+shift+h": ActionReplaceInWorkspace,

			// Type definition / implementation: VSCode binds Goto Type to
			// Ctrl+F12 — most terminals report it as "ctrl+f12" distinctly
			// (the F12 frame already has a unique escape sequence; Ctrl is
			// just an extra modifier byte). Goto Implementation has no
			// universal shortcut; we surface it via the palette only.
			"ctrl+f12": ActionGotoTypeDef,

			// Run tests. Ctrl+Shift+P is taken in some IDEs but we use F1
			// for the palette; Shift+F5 was just bound to "previous
			// problem". For runtest we use Alt+T as the keyboard shortcut
			// (palette also has "Run: Tests").
			"alt+t": ActionRunTests,

			// Zen mode toggle. VSCode uses Ctrl+K Z (chord) which we can't
			// represent; Alt+Z is the next-most-common alternative and is
			// not commonly grabbed by terminal emulators.
			"alt+z": ActionToggleZenMode,

			// Soft word wrap toggle. VSCode default is Alt+Z but we already
			// use that for Zen mode (their Ctrl+K Z chord doesn't survive
			// Bubble Tea's input layer). Alt+W is rarely grabbed by terminals
			// and reads as "Wrap" — the next-best mnemonic.
			"alt+w": ActionToggleWordWrap,

			// Git blame for the current line — pure feedback move (toast),
			// no shortcut conflict to worry about.
			"alt+b": ActionGitBlameLine,

			// Git hunk navigation: F6 is focus-swap, F5/Shift+F5 are
			// diagnostics. F-keys around there are crowded so we use Alt
			// combos. nvim power users can keep `]c` / `[c` from native
			// vim defaults too — these don't conflict.
			"alt+]": ActionNextHunk,
			"alt+[": ActionPrevHunk,

			// Pin / unpin the active tab — VSCode uses Ctrl+K Shift+Enter
			// (chord) which we can't represent. Alt+P is universally free
			// and easy to remember.
			"alt+p": ActionPinTab,

			// Buffer info popup. Ctrl+Alt+I is reachable on terminals that
			// forward distinct ctrl-alt sequences (kitty/CSI-u, wezterm,
			// ghostty); legacy terminals fall back to the palette
			// ("View: Show Buffer Info") which always works.
			"ctrl+alt+i": ActionShowBufferInfo,

			// Right-side Actions panel toggle. Alt+A is universally free
			// (Ctrl+Shift+A is grabbed by most terminals for "select all").
			"alt+a": ActionToggleActionsPanel,

			// Markdown preview: F7 (the canonical one-shot overlay) is the
			// only binding now. We had a tab-based "live preview" attempt
			// (Alt+M / Shift+F7) but the underlying nvim-terminal-buffer
			// approach broke down under nvim 0.9 without a markdown
			// treesitter parser, and embedding glamour-rendered ANSI in a
			// scratch buffer doesn't render either (regular buffers show
			// escape codes verbatim). The F7 overlay does the right thing
			// — render via glamour, show in a scrollable preview pane.

			// Cheat sheet overlay. `?` is the closest thing to a "help" key
			// that doesn't conflict with any common typing — but in Insert
			// mode it would type a literal `?`, so this only fires from the
			// global keymap layer (Normal mode passes through to nvim where
			// `?` is the search-up command). For everyday use the palette
			// command "Help: Show Shortcuts" is the more discoverable path.

			// Toggle soft word wrap (palette only — Alt+Z is the VSCode
			// binding but Alt-letter often gets eaten by terminal/window
			// managers, so we don't bind it here).
		},
	}
}

func (k KeyMap) Match(msg tea.KeyMsg) Action {
	if a, ok := k.bindings[msg.String()]; ok {
		return a
	}
	return ActionNone
}
