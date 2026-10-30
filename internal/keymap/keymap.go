package keymap

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

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
	ActionStageHunk
	ActionUnstageHunk
	ActionDiscardHunk
	ActionStageLines
	ActionUnstageLines
	ActionReloadBuffer
	ActionOpenURL
	ActionPinTab
	ActionCopyFileRef
	ActionShowSnippetPicker
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
	// Terminal splits (one terminal tab with several shells): split the
	// focused pane to the right / down, move focus between the panes.
	ActionTerminalSplitRight
	ActionTerminalSplitDown
	ActionTerminalFocusPrevPane
	ActionTerminalFocusNextPane
	// ActionApplyPreferredCodeAction applies the LSP-flagged "preferred"
	// code action at the cursor (when present) without opening the picker.
	// VSCode-style: the inline 💡 hint advertises this shortcut so users
	// can skip the picker for the common quickfix-driven flow. Default
	// binding: Ctrl+Shift+. (kitty/CSI-u terminals); palette is the
	// universal fallback ("Edit: Apply Preferred Quick Fix").
	ActionApplyPreferredCodeAction
	// Group C — tasks + Problems panel. Alt+R picks a task to run,
	// Alt+Shift+R reruns the last one, Ctrl+Shift+M / Alt+M shows the
	// Problems panel.
	ActionRunTask
	ActionRerunTask
	ActionShowProblems
	// Group A — AI. Alt+A (ActionToggleActionsPanel) opens the AI chat
	// panel; these cover the rest: Alt+\ asks for an inline completion
	// now, Alt+I edits the selection / line with an instruction, Alt+|
	// toggles automatic ghost text.
	ActionAITriggerInline
	ActionAIEditSelection
	ActionAIToggleInline
	// Group D — Run & Debug. ActionDebugStartContinue starts the selected
	// configuration (or continues a paused session): Alt+F5.
	// ActionDebugStop terminates the session: Shift+Alt+F5 (arrives as
	// alt+f17 on terminals that report Shift+F5 as F17; xterm's CSI 15;4~
	// is caught in app.routeDebugMsg).
	ActionDebugStartContinue
	ActionDebugStop
	// ActionNextSearchResult / ActionPrevSearchResult walk the Find Results
	// buffer (VSCode: F4 / Shift+F4). No-op with a toast when no search
	// has run yet.
	ActionNextSearchResult
	ActionPrevSearchResult
	// ActionDetachSession detaches this terminal from a persistent session
	// (the session keeps running; `termocode attach` comes back). No
	// default key; palette "Session: Detach".
	ActionDetachSession
	// ActionServicesFocus shows and focuses the Services view. No default
	// key (the activity bar and the palette reach it); keymap.json may
	// bind one.
	ActionServicesFocus
	// ActionDatabasesFocus shows and focuses the Databases view.
	ActionDatabasesFocus
	// ActionHTTPSend sends the request or SQL statement at the cursor,
	// or runs the selection in a SQL buffer.
	ActionHTTPSend
	// ActionAINextProfile switches the AI chat to the next agent profile
	// (U20 AP-5): Alt+Shift+P (Alt+P is Pin Tab).
	ActionAINextProfile
	// ActionAIToggleAgents shows the AGENTS view (the chat's agent runs)
	// in the chat slot, or switches the slot back to the chat (U20 AP-9):
	// Alt+Shift+A.
	ActionAIToggleAgents
	// ActionItermocToggle opens the Itermoc view, or goes back when it
	// is already shown: Alt+Shift+I (Alt+I is Edit with AI) or Alt+O.
	ActionItermocToggle
	// ActionGitCIFix sends the failed CI log of the branch to the AI chat
	// input (G-CI): Alt+C. Without red CI it opens the CI card.
	ActionGitCIFix
	// ActionContextHelp opens the built-in guide at the section for the
	// hover label on screen, else the focused item (app/help_contexts.go).
	// Shift+F1; xterm sends it as f13, which canonicalKey matches.
	ActionContextHelp
	// ActionLayoutOpen opens the Layout: Open… list (G-LAY): Alt+Y.
	ActionLayoutOpen
	// ActionWebStop stops the web panel (the kill switch, ADR 0020). No
	// default key (owner): the user binds one in keymap.json.
	ActionWebStop
)

// KittyOnlyKeys are default bindings a legacy terminal cannot send (or
// sends as another key: Ctrl+Alt+I arrives as Alt+Tab). They work on
// terminals with the kitty keyboard protocol; elsewhere their actions are
// reachable through another key or the command palette (app tests check
// this). Adding a binding that no legacy terminal can send means adding it
// here on purpose (reachable_test.go fails otherwise).
var KittyOnlyKeys = map[string]bool{
	"ctrl+enter": true,
	"ctrl+.":     true, "ctrl+shift+.": true, "ctrl+/": true, "ctrl+`": true, "ctrl+shift+`": true,
	"ctrl+tab": true, "ctrl+alt+i": true,
	"ctrl+shift+e": true, "ctrl+shift+h": true, "ctrl+shift+m": true, "ctrl+shift+o": true,
	"ctrl+shift+t": true, "ctrl+shift+v": true, "ctrl+shift+w": true,
}

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
			// A second palette key: VS Code's and JetBrains' terminals keep
			// F1 for their own palette (Emacs users know Alt+X as "run a
			// command").
			"alt+x":  ActionCommandPalette,
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
			// Terminal splits: "/" and "-" draw the divider's direction.
			// Alt+Left / Alt+Right move between the panes.
			"alt+/":     ActionTerminalSplitRight,
			"alt+-":     ActionTerminalSplitDown,
			"alt+left":  ActionTerminalFocusPrevPane,
			"alt+right": ActionTerminalFocusNextPane,

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

			// Group D: F11 step into, Shift+F11 step out (xterm reports
			// Shift+F11 as F23), Alt+F5 start/continue, Shift+Alt+F5 stop.
			"f11":     ActionDebugStepInto,
			"f23":     ActionDebugStepOut,
			"alt+f5":  ActionDebugStartContinue,
			"alt+f17": ActionDebugStop,

			"f7": ActionMarkdownPreview,
			"f8": ActionWorkspaceSearch,
			// Ctrl+Shift+F (VSCode / Sublime default) is intercepted by
			// every common terminal emulator we've tested so it never
			// reaches the app. Alt+F is forwarded by iTerm2, wezterm,
			// kitty, ghostty — anywhere else it'll get eaten by the host
			// terminal's menu bar (gnome-terminal binds it to "File"),
			// in which case F8 / palette stay as fallbacks.
			"alt+f": ActionWorkspaceSearch,

			// Find in Files results: F4 / Shift+F4 = next / previous result,
			// as in VSCode. xterm reports Shift+F4 as F16.
			"f4":       ActionNextSearchResult,
			"shift+f4": ActionPrevSearchResult,
			"f16":      ActionPrevSearchResult,

			// LSP inlay hints. VSCode has no default key; Alt+L ("inLay")
			// is free and every terminal forwards it.
			"alt+l": ActionToggleInlayHints,

			// Navigation
			// ctrl+shift+e is best-effort: only terminals with the kitty/CSI-u
			// keyboard protocol report it distinctly. The same action remains
			// reachable via the command palette ("Reveal File in Explorer").
			"ctrl+shift+e": ActionRevealFile,

			// Buffer history
			// Reopen-closed-editor. Ctrl+Shift+T is the VSCode binding; it
			// only reaches us on terminals that do not grab it for "new
			// tab" (kitty/CSI-u setups with the key unbound), so we bind it
			// best-effort. Alt+Shift+T is the universal route; Bubble Tea
			// reports it as "alt+shift+t" on modern terminals and "alt+T"
			// (capital T, shift implied) on xterm-style ones. The palette
			// ("View: Reopen Closed Editor") always works.
			"ctrl+shift+t": ActionReopenClosed,
			"alt+shift+t":  ActionReopenClosed,
			"alt+T":        ActionReopenClosed,
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

			// Group C — tasks + Problems panel. Shift+Alt+R arrives as
			// "alt+R" on legacy terminals and "alt+shift+r" on CSI-u ones.
			"alt+r":        ActionRunTask,
			"alt+R":        ActionRerunTask,
			"alt+shift+r":  ActionRerunTask,
			"ctrl+shift+m": ActionShowProblems,
			"alt+m":        ActionShowProblems,

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
			"alt+c": ActionGitCIFix, // G-CI: red CI → the AI chat

			// Git hunk navigation: F6 is focus-swap, F5/Shift+F5 are
			// diagnostics. F-keys around there are crowded so we use Alt
			// combos. nvim power users can keep `]c` / `[c` from native
			// vim defaults too — these don't conflict.
			"alt+]": ActionNextHunk,
			"alt+[": ActionPrevHunk,
			"alt+s": ActionStageHunk,
			"alt+u": ActionUnstageHunk,
			// Alt+Shift+S / U: the same for the cursor line or the selected
			// lines only (arrives as "alt+S" / "alt+U" on legacy terminals).
			"alt+S": ActionStageLines,
			"alt+U": ActionUnstageLines,

			// Pin / unpin the active tab — VSCode uses Ctrl+K Shift+Enter
			// (chord) which we can't represent. Alt+P is universally free
			// and easy to remember.
			"alt+p": ActionPinTab,

			// Buffer info popup. Ctrl+Alt+I is reachable on terminals that
			// forward distinct ctrl-alt sequences (kitty/CSI-u, wezterm,
			// ghostty); legacy terminals fall back to the palette
			// ("View: Show Buffer Info") which always works.
			"ctrl+alt+i": ActionShowBufferInfo,

			// HTTP client: send the request at the cursor (VS Code REST
			// Client's key). Legacy terminals send it as Esc + Ctrl+R,
			// which canonicalKey matches too; the palette always works.
			"ctrl+alt+r": ActionHTTPSend,
			"ctrl+enter": ActionHTTPSend,

			// Right-side Actions panel toggle. Alt+A is universally free
			// (Ctrl+Shift+A is grabbed by most terminals for "select all").
			"alt+a": ActionToggleActionsPanel,

			// Group A — AI.
			"alt+\\": ActionAITriggerInline,
			"alt+i":  ActionAIEditSelection,
			"alt+|":  ActionAIToggleInline,
			// Next agent profile. Alt+Shift+P arrives as "alt+P" on legacy
			// terminals and "alt+shift+p" on CSI-u ones.
			"alt+P":       ActionAINextProfile,
			"alt+shift+p": ActionAINextProfile,
			// The AGENTS view in the chat slot. Alt+Shift+A arrives as
			// "alt+A" on legacy terminals and "alt+shift+a" on CSI-u ones.
			"alt+A":       ActionAIToggleAgents,
			"alt+shift+a": ActionAIToggleAgents,
			// A second route for both keys above: some terminals and
			// window managers swallow Alt+Shift+letter. Alt+G (aGents) and
			// Alt+N (Next profile) carry no Shift.
			"alt+g": ActionAIToggleAgents,
			"alt+n": ActionAINextProfile,
			// The Itermoc view. Alt+Shift+I arrives as "alt+I" on legacy
			// terminals and "alt+shift+i" on CSI-u ones (Alt+I is Edit
			// with AI); Alt+O carries no Shift.
			"alt+I":       ActionItermocToggle,
			"alt+shift+i": ActionItermocToggle,
			"alt+o":       ActionItermocToggle,

			// Help for the focused item (or the hover label on screen).
			// F1 is the palette; Shift+F1 is free in every terminal we
			// test (xterm sends f13).
			"shift+f1": ActionContextHelp,

			// Layouts: the list of saved layouts (Alt+Y, no Shift needed).
			"alt+y": ActionLayoutOpen,

			// Markdown preview: F7 (the canonical one-shot overlay) is the
			// only binding now. We had a tab-based "live preview" attempt
			// (Alt+M / Shift+F7) but the underlying nvim-terminal-buffer
			// approach broke down under nvim 0.9 without a markdown
			// treesitter parser, and embedding glamour-rendered ANSI in a
			// scratch buffer doesn't render either (regular buffers show
			// escape codes verbatim). The F7 overlay does the right thing
			// — render via internal/mdview, show in a scrollable preview pane.

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
	s := msg.String()
	if a, ok := k.bindings[s]; ok {
		return a
	}
	// The same key spelled differently: "f15" from xterm is "shift+f3"
	// in the keymap, "alt+F" is "shift+alt+f". Only keys with a modifier
	// or a high F-key can differ, so plain typing never pays this loop.
	if strings.ContainsRune(s, '+') || (len(s) > 2 && s[0] == 'f') {
		c := canonicalKey(s)
		for key, a := range k.bindings {
			if canonicalKey(key) == c {
				return a
			}
		}
	}
	return ActionNone
}

// canonicalKey is the one name of a key: modifiers in the order ctrl, alt,
// shift; xterm's F13–F24 are Shift+F1–F12; an upper-case letter after Alt
// or Ctrl means Shift.
func canonicalKey(s string) string {
	parts := strings.Split(s, "+")
	if len(parts) > 1 && parts[len(parts)-1] == "" { // "ctrl++"
		parts = append(parts[:len(parts)-2], "+")
	}
	name := parts[len(parts)-1]
	var ctrl, alt, shift bool
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "ctrl":
			ctrl = true
		case "alt":
			alt = true
		case "shift":
			shift = true
		}
	}
	if len(name) > 1 && name[0] == 'f' {
		if n, err := strconv.Atoi(name[1:]); err == nil && n >= 13 && n <= 24 {
			name, shift = "f"+strconv.Itoa(n-12), true
		}
	}
	if (ctrl || alt) && len(name) == 1 && name[0] >= 'A' && name[0] <= 'Z' {
		name, shift = strings.ToLower(name), true
	}
	var b strings.Builder
	if ctrl {
		b.WriteString("ctrl+")
	}
	if alt {
		b.WriteString("alt+")
	}
	if shift {
		b.WriteString("shift+")
	}
	b.WriteString(name)
	return b.String()
}

// CanonicalKey returns the keymap package's canonical spelling for a key.
// Modifier order and terminal function-key aliases match the active keymap.
func CanonicalKey(s string) string { return canonicalKey(s) }
