package app

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"context"
	"termocode/internal/activity"
	"termocode/internal/confirm"
	"termocode/internal/editor"
	"termocode/internal/explorer"
	"termocode/internal/findbar"
	"termocode/internal/git"
	"termocode/internal/keymap"
	"termocode/internal/menu"
	"termocode/internal/nvim"
	"termocode/internal/picker"
	"termocode/internal/preview"
	"termocode/internal/prompt"
	"termocode/internal/recents"
	"termocode/internal/replacebar"
	"termocode/internal/search"

	"termocode/internal/clipring"
	"termocode/internal/statusbar"
	"termocode/internal/tabbar"
	"termocode/internal/theme"
	"termocode/internal/toast"
)

type Focus int

const (
	FocusExplorer Focus = iota
	FocusEditor
)

// Layout constants. The chrome composes left-to-right as:
//
//	[ activity.Width ][ explorerWidth ][ editor area ][ scrollbar (1) ]
//
// where the editor area takes the remaining columns. Each component renders
// only inside its own width — none reach into the next column.
//
// `defaultExplorerWidth` is the initial value for `Model.explorerWidth`; the
// drag-splitter changes the field, not the constant. The legacy name
// `explorerWidth` is retained as the default so older call sites that read it
// at package scope still compile.
const (
	defaultExplorerWidth = 30 // includes the 1-col right-edge divider
	explorerWidth        = defaultExplorerWidth
	sidebarHeaderHeight  = 1 // EXPLORER / SEARCH / SOURCE CONTROL row
	editorTabBarHeight   = 1 // tab strip above the editor content
	closedRingCap        = 10
	editorScrollbarWidth = 0 // overview ruler column on the right edge — disabled (scroll perf)
	explorerMinWidth     = 20
	explorerMaxWidth     = 60
)

// dragKind identifies what kind of layout drag is currently in progress.
// dragNone is the resting state; dragExplorerRight means the user is dragging
// the splitter between the sidebar and the editor pane (which resizes
// explorerWidth); dragTerminalTop means the user is dragging the integrated
// terminal panel's header row up/down to resize the panel height.
type dragKind int

const (
	dragNone dragKind = iota
	dragExplorerRight
	dragTerminalTop
	dragActionsLeft
)

// Right-side Actions panel layout constants. These mirror the explorer's
// drag-splitter constants (explorerMinWidth/Max) so resize feels consistent.
const (
	defaultActionsWidth = 32
	actionsMinWidth     = 24
)

// terminalRowsMin is the smallest height (in rows) the integrated terminal
// panel can shrink to via the drag splitter. Anything smaller and the panel
// stops being useful — there's no room to read shell output.
const terminalRowsMin = 5

// terminalRowsDefault is the initial panel height on first open. Mirrors
// the previous hardcoded constant so existing users see no behavioural
// change until they grab the drag handle.
const terminalRowsDefault = 10

type Model struct {
	activity  activity.Model
	explorer  explorer.Model
	editor    editor.Model
	tabs      tabbar.Model
	status    statusbar.Model
	picker    picker.Model
	menu      menu.Model
	find      findbar.Model
	confirm   confirm.Model
	preview   preview.Model
	prompt    prompt.Model
	recents   recents.Model
	replace   replacebar.Model
	search    search.Model
	toast     toast.Model
	theme     theme.Theme
	keys      keymap.KeyMap
	nvim      *nvim.Client
	bufs      []nvim.BufferInfo
	activeBuf int

	// closedRing remembers the paths of recently-closed editors so the
	// "Reopen Closed Editor" action can pop the most recent one. Capacity is
	// fixed (closedRingCap); when full, the oldest entry is dropped.
	closedRing []string

	gitIsRepo bool
	gitBranch git.Branch
	gitFiles  []git.FileStatus
	gitCursor int // index into gitFiles for ViewGit sidebar

	// problemsIndex maps picker IDs (e.g. "p-3") back to the underlying
	// nvim.Diagnostic so jumpToProblem can decode the user's selection. Only
	// populated while the Problems picker is open.
	problemsIndex map[string]nvim.Diagnostic

	// codeActionsIndex maps picker IDs (e.g. "ca-2") to the 1-based index
	// into nvim's parked _termocode_code_actions table, so applying a
	// selected action just needs the integer back. Populated when the Code
	// Actions picker is opened; stale between sessions but harmless.
	codeActionsIndex map[string]int

	// synthCodeActions holds the Phase-2 synthesized actions (Run Test,
	// Git: Show Blame, Generate: Doc Comment, …) keyed by the negative
	// index we encode in nvim.CodeAction.Index for round-tripping through
	// the picker. The apply path branches on this map: positive index →
	// dispatch to nvim.ApplyCodeAction; negative index → look up here and
	// run the Go-side handler.
	synthCodeActions map[int]synthCodeAction

	// preferredCodeAction is the LSP-flagged "preferred" action from the
	// most recent fetchCodeActionsCmd round-trip — typically the
	// canonical Quick Fix for a diagnostic. When non-nil, the editor
	// renders an inline 💡 hint at the cursor row advertising the
	// "Ctrl+Shift+. apply" shortcut, and the dispatcher routes
	// ActionApplyPreferredCodeAction straight to the apply path
	// (skipping the picker). Cleared when the cursor moves to a
	// different line, when any action is applied, or when the user
	// presses Esc / dismisses the hint.
	preferredCodeAction *nvim.CodeAction
	// preferredCodeActionLine is the 1-based cursor line the hint was
	// pinned to. The next StateMsg compares msg.CursorLine against this
	// to decide whether to clear m.preferredCodeAction (the user moved
	// off the diagnostic row).
	preferredCodeActionLine int

	// symbolsIndex maps picker IDs to {line, col} for the "Go to Symbol in
	// File" overlay. Populated each time the picker is opened.
	symbolsIndex map[string][2]int

	// workspaceSymIndex maps picker IDs to {file, line, col} for the "Go to
	// Symbol in Workspace" overlay. Same lifetime semantics as symbolsIndex.
	workspaceSymIndex map[string]workspaceSymbolTarget

	// diffLeftPath is set after the user picks the LEFT file in the
	// "Compare Two Files..." flow. The next picker selection is treated as
	// the RIGHT file and the comparison runs.
	diffLeftPath string

	// replaceFind holds the search pattern entered in the first step of
	// the Replace-in-Workspace flow. The second prompt's submit reads it.
	replaceFind string

	// pinnedBufs maps buffer-id → pin order (1, 2, 3 …). Pinned buffers
	// move to the front of the tab bar, keeping their relative order. The
	// in-memory state is rebuilt every StateMsg via reorderPinned, so we
	// don't need to persist it across sessions.
	pinnedBufs map[int]int
	pinOrder   int

	// snippetPickerIndex maps picker IDs (e.g. "snip-iferr") to the snippet
	// body so insertSnippetBody knows what to expand. Populated each time
	// the snippet picker opens.
	snippetPickerIndex map[string]string

	// Markdown live preview: when on, a scratch buffer named
	// "<basename> PREVIEW" carries the glamour-rendered output and lives
	// in its own tab/window alongside the source. Refreshes on save.
	mdPreviewOn         bool
	mdPreviewSourcePath string // absolute path of the markdown file being previewed

	// errorLog is an in-memory ring buffer of full error / warning
	// messages so the user can review them via "Help: Show Error Log"
	// when the toast / status bar truncated the headline. Capped at
	// errorLogCap; appended via logErr().
	errorLog []string

	// zenMode hides every chrome element (sidebar, tabs, status bar,
	// breadcrumbs) and lets the editor fill the entire window. Toggled
	// from the palette / shortcut.
	zenMode bool

	// autoSaveOn drives a 5-second autocmd that writes every modified
	// non-special buffer. Off by default. Persisted via palette only.
	autoSaveOn bool

	// stateLastFetch throttles fetchStateCmd. nvim emits a burst of
	// RedrawMsgs when scrolling — issuing a Lua FetchState per redraw
	// makes scrolling feel choppy on large files. We coalesce them down
	// to ~10 Hz instead.
	stateLastFetch time.Time

	// lastReloadSeq remembers the most recent FileChangedShellPost counter
	// surfaced via FetchState. A higher value in the next StateMsg means a
	// new external reload happened — we push a "Reloaded <basename>" toast
	// once and update the cursor so duplicates aren't shown.
	lastReloadSeq int

	// lastRecordedPath is the path most recently written into the recents
	// store. We only re-push when the active path actually changes so the
	// recents list isn't churned by every cursor move.
	lastRecordedPath string

	// Scrollbar overview ruler: per-line markers + total line count, used
	// by renderEditorScrollbar to project diagnostic locations onto the
	// scrollbar column. Refreshed lazily on save / cursor-move (debounced).
	scrollbarMarks      []nvim.DiagnosticMark
	scrollbarTotalLines int
	scrollbarLastFetch  time.Time

	// cursorLine mirrors the most recent StateMsg's 1-based cursor line.
	// Used by breadcrumbs to find the enclosing symbol.
	cursorLine int

	// editorMode is the nvim mode short name ("insert", "normal",
	// "replace", "visual", …) used to shape the terminal hardware cursor
	// via DECSCUSR escapes in View().
	editorMode string

	// Clipboard history: in-memory ring fed by TextYankPost (in-editor
	// yanks) and golang.design/x/clipboard.Watch (system-wide). Picker
	// reads via ring.Items(). cancel stops the watcher on shutdown.
	clipRing   *clipring.Ring
	clipCancel context.CancelFunc

	// Integrated terminal panel state ──────────────────────────────────
	// terminalCwd holds the directory the shell is currently in (for the
	// header echo). Seeded from openIntegratedTerminalAt(dir) when the
	// panel opens via the explorer's "Open Terminal Here" flow, then
	// refreshed at terminalCwdRefreshTTL from nvim's b:term_title (most
	// modern shells emit OSC 7 / OSC 0 to update it on every `cd`).
	// terminalCwdAt is the last-poll wall-clock used for throttling.
	terminalCwd   string
	terminalCwdAt time.Time

	// terminalRows is the user-controllable height (in rows) of the
	// integrated terminal panel BODY (i.e. the nvim terminal split, not
	// counting the 1-row chrome header termocode draws above it). The
	// drag-splitter on the header row mutates this; applyLayout feeds it
	// into the layout math and resizeTerminalSplit pushes the new height
	// into the live nvim split. Defaults to terminalRowsDefault on first
	// open and is persisted across sessions via session.json.
	terminalRows int

	// terminalRowsLastSent caches the most recent value pushed to nvim via
	// resizeTerminalSplit so we only fire the Lua RPC when the height
	// actually changes — without this, every MouseMotion during a drag
	// (>20/sec) would issue a redundant nvim_win_set_height call even when
	// the cell-aligned value didn't change.
	terminalRowsLastSent int

	// terminalTabs is the list of integrated-terminal tabs. Each tab owns a
	// distinct nvim terminal buffer; the active tab is rendered into the
	// single visible terminal window via nvim_win_set_buf. terminalActiveTab
	// is an index into this slice — clamped to [0, len-1] on every read.
	// Empty slice means "no tabs" (panel implicitly closed).
	terminalTabs      []terminalTab
	terminalActiveTab int

	// terminalWinID is the live nvim window-id of the integrated terminal
	// split. Captured on first open and reused across tab switches via
	// nvim_win_set_buf. Zero / -1 when the window doesn't exist (panel
	// closed). Recovered lazily by probeTerminalWindowID when stale.
	terminalWinID int

	// terminalMinimized collapses the panel to just the tab bar (no shell
	// content visible). Toggled by the "−" button on the tab bar and via
	// the palette. The drag-resize handler also flips this when the user
	// drags the panel down past the minimum height.
	terminalMinimized bool

	// inTerminal mirrors "nvim's current window is the integrated-terminal
	// window AND nvim is in terminal-insert mode (mode 't')". When true,
	// handleGlobalKey lets terminal-meaningful shortcuts (Ctrl+C, Tab,
	// Ctrl+D, Ctrl+R, …) bypass the termocode keymap and reach the shell
	// PTY directly — so e.g. Ctrl+C sends SIGINT to a long-running command
	// instead of triggering ActionCopy. Recomputed on every StateMsg from
	// (msg.CurrentWin == m.terminalWinID && msg.Mode == "t"); cleared the
	// moment the user clicks back into the editor or runs `exit` in the
	// shell. See shell.go::shouldPassToTerminal for the pass-through set.
	inTerminal bool

	focus             Focus
	showExp           bool
	pickerOpen        bool
	pickerKind        pickerKindEnum
	menuOpen          bool
	menuKind          menuKindEnum
	menuPath          string
	menuTabID         int
	findOpen          bool
	replaceOpen       bool
	confirmOpen       bool
	confirmKind       confirmKindEnum
	confirmTargetID   int
	confirmTargetPath string
	// confirmTargetPaths is the absolute-path list for a bulk-delete
	// confirm flow (confirmKindDeletePath with len > 1). When the user
	// confirms, every entry is os.RemoveAll'd and any matching open
	// buffer is :bdelete!'d. Empty means single-path delete via
	// confirmTargetPath; populated means bulk delete.
	confirmTargetPaths []string
	previewOpen       bool
	promptOpen        bool
	promptKind        promptKindEnum
	promptPath        string
	recentsOpen       bool
	searchOpen        bool
	termOpen          bool
	w, h              int
	err               string
	nvimAttached      bool

	// inlayHintsOn mirrors the LSP inlay-hint display toggle for the
	// "Toggle Inlay Hints" command. Hints start enabled on LspAttach
	// (see lsp_lua.go); flipping this is purely a UI mirror — the
	// authoritative state lives in nvim's vim.lsp.inlay_hint module.
	inlayHintsOn bool

	// Layout-extension state ────────────────────────────────────────────
	// explorerWidth is the live sidebar width in columns, mutated by the
	// drag-splitter handler in mouse.go. Initialized to defaultExplorerWidth
	// in New(). Clamped to [explorerMinWidth, explorerMaxWidth] on update.
	explorerWidth int

	// Right-side Actions panel state ─────────────────────────────────────
	// actionsOpen flips false when the user clicks the panel's × button,
	// hiding the panel AND its launcher chip; the only ways back in are the
	// keybinding (Alt+A) and the palette command. actionsPinned controls
	// whether the panel takes a real column on screen or collapses to the
	// bottom-right launcher chip — pin off but open=true means "show the
	// chip, not the column." actionsWidth is the live column width when
	// pinned, mutated by the drag-splitter and persisted across sessions.
	actionsOpen   bool
	actionsPinned bool
	actionsWidth  int

	// stickyContext is the 1-line "function/class signature" shown above the
	// editor content as the user scrolls past it. Empty string suppresses the
	// strip entirely (no row reserved). Populated on StateMsg via Lua.
	stickyContext   string
	stickyLastFetch time.Time // throttle: skip refresh if last fetch was recent

	// breadcrumbs is the path-and-symbols trail shown above the sticky-scroll
	// strip (e.g. ["internal","app","view.go","renderBase"]). Empty slice or
	// nil suppresses the strip. Populated on StateMsg from editor path + the
	// last symbol enclosing the cursor.
	breadcrumbs []string

	// dragKind / dragStartX track an in-progress mouse drag on a splitter
	// boundary. dragKind == dragNone means nothing is being dragged.
	dragKind   dragKind
	dragStartX int

	// dapSessionActive flips true when a Debug Adapter Protocol session is
	// running (driven by Debug: Start / Debug: Stop palette commands). It
	// gates step-* keybindings (so F10/F11 don't toast spam without a
	// session) and adds a "● DEBUG" badge to the status bar.
	dapSessionActive bool

	// Preferences UI state ───────────────────────────────────────────────
	// settingPromptID remembers which settings row the user just chose,
	// so handlePromptSubmit can dispatch the typed value to the right
	// column when promptKindSetting fires.
	settingPromptID string

	// settingsModal is the dedicated structured Settings overlay (separate
	// from the universal picker so it can have a header/grouped/aligned
	// layout). settingsModalOpen flips when the user invokes Settings via
	// the activity bar / palette; closes on Esc or after a row selection
	// hands off to the prompt.
	settingsModal     settingsModal
	settingsModalOpen bool

	// themePromptKey is the palette field the user is editing in the
	// custom-theme editor (BgEditor, SyntaxKeyword, etc.). Mirrors
	// settingPromptID's usage.
	themePromptKey string

	// keybindingPromptAction is the action name (e.g. "Save") whose
	// shortcut the user is rebinding. Read by applyKeybinding to know
	// which Action to write to keymap.json.
	keybindingPromptAction string

	// welcomeFocus is the index of the currently-focused Quick Action
	// card on the welcome screen (0..3). Sticky across welcome opens.
	// Updated by keyboard arrow/Tab navigation and by mouse clicks so
	// keyboard and mouse stay in sync.
	welcomeFocus int

	// Overflow (⋮) menu state ─────────────────────────────────────────
	// overflowMenuOpen is true while the dropdown anchored to the
	// vertical-three-dots glyph at the right edge of the tab bar's body
	// row is visible. The glyph itself is ALWAYS rendered (muted by
	// default, primary on hover/open); the dropdown is the panel below.
	// overflowMenuCursor is the index into overflowMenuItems(m) — clamped
	// every render so adding/removing items mid-flight stays safe.
	overflowMenuOpen   bool
	overflowMenuCursor int

	// lineNumbersOn mirrors nvim's `set number` toggle. Initialized true
	// to match the `set number` line in attachCmd. The overflow menu's
	// "Line Numbers" item flips this and pushes `set number` /
	// `set nonumber` to nvim. Authoritative state lives in nvim; this is
	// just a UI mirror so the checkmark renders correctly.
	lineNumbersOn bool
}

// welcomeShowing reports whether the start page is currently rendered in
// the editor pane (mirrors the same condition view.go uses to choose
// renderWelcome over m.editor.View).
func (m Model) welcomeShowing() bool {
	return m.editor.Path() == "" && len(m.bufs) <= 1 && !m.termOpen
}

func New() Model {
	// Load the persisted theme (if any) before constructing components, so
	// the first frame already renders in the user's chosen palette. Missing
	// or corrupt config falls back to VSCode Dark+ silently — see
	// theme.LoadAndApply for the precise rules.
	named := theme.LoadAndApply()
	t := named.Styles

	// Layer the user's keymap overrides on top of the defaults. Missing /
	// corrupt files yield an empty map (LoadOverrides never errors) so
	// startup is uninterruptible by a hand-edited config.
	keys := keymap.Default().MergeInto(keymap.LoadOverrides(""))

	client, err := nvim.New()
	// Terminal panel height: prefer the value persisted from the previous
	// session so the user's drag-set preference survives a relaunch. Missing
	// / corrupt / out-of-range values fall back to the default.
	sess := loadSession()
	termRows := terminalRowsDefault
	if r := sess.TerminalRows; r >= terminalRowsMin {
		termRows = r
	}

	// Actions panel: defaults are open=false, pinned=true, width=32 on
	// first run — the panel is hidden until the user explicitly opens
	// it (Alt+A or palette). Persisted booleans are pointers so an
	// explicit value from a prior session wins over the default.
	actionsOpen := false
	if sess.ActionsOpen != nil {
		actionsOpen = *sess.ActionsOpen
	}
	actionsPinned := true
	if sess.ActionsPinned != nil {
		actionsPinned = *sess.ActionsPinned
	}
	actionsWidth := defaultActionsWidth
	if sess.ActionsWidth >= actionsMinWidth {
		actionsWidth = sess.ActionsWidth
	}

	m := Model{
		activity:      activity.New(),
		explorer:      explorer.New(),
		tabs:          tabbar.New(),
		toast:         toast.New(),
		status:        statusbar.New(t),
		theme:         t,
		keys:          keys,
		focus:         FocusEditor,
		showExp:       true,
		inlayHintsOn:  true,
		lineNumbersOn: true,
		explorerWidth: defaultExplorerWidth,
		terminalRows:  termRows,
		actionsOpen:   actionsOpen,
		actionsPinned: actionsPinned,
		actionsWidth:  actionsWidth,
	}
	if err != nil {
		m.editor = editor.New(nil)
		m.err = "nvim: " + err.Error()
	} else {
		m.editor = editor.New(client)
		m.nvim = client
	}
	m.clipRing = clipring.New()
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.HideCursor, fetchGitCmd()}
	if cmd := m.initClipboardCapture(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if m.nvim != nil {
		cmds = append(cmds, m.attachCmd(), m.nvim.Next())
	}
	return tea.Batch(cmds...)
}

func (m Model) attachCmd() tea.Cmd {
	return func() tea.Msg {
		if err := m.nvim.Attach(80, 24); err != nil {
			return ErrMsg{Err: err}
		}
		for _, cmd := range []string{
			"set termguicolors",
			"set number",
			"set mouse=a",
			"set tabstop=4 shiftwidth=4 expandtab",
			"set scrolloff=4",
			"set signcolumn=auto",
			// External file change detection: autoread silently reloads
			// buffers whose mtime changed on disk (e.g., after `git pull`
			// or edits from another editor); updatetime=1000 makes the
			// CursorHold-driven checktime fire promptly during idle.
			"set autoread",
			"set updatetime=1000",
			// numberwidth=7: room for 5-digit line numbers + breathing
			// space on both sides (1+ left padding via right-align, 2
			// trailing via statuscolumn). For files >99999 lines nvim
			// auto-grows further.
			"set numberwidth=7",
			"set laststatus=0",
			// cmdheight=0 reclaims the bottom row of nvim's grid that would
			// otherwise be reserved for the cmdline. Without this, our
			// integrated terminal panel's splice math is off by one: nvim
			// places the upper window's statusline (the row we overpaint
			// with the tab bar) at editorH-terminalRows-2 because the
			// cmdline takes the bottom row, but the splice writes the bar
			// at editorH-terminalRows-1 — leaving the statusline visible
			// AS A SECOND chrome row above the tab bar (the user-reported
			// "two gray bars" bug). With cmdheight=0 the cmdline row is
			// gone, the statusline lands at editorH-terminalRows-1, and
			// the splice overpaints it cleanly. Available since Neovim
			// 0.8; stable in 0.9+ which is our floor (see CLAUDE.md).
			// Transient cmdline expansions (e.g. on `:` commands or
			// unsuppressed messages) are rare in termocode — almost every
			// nvim interaction goes through `:silent` or the Lua API.
			"set cmdheight=0",
			"set noruler",
			"set noshowmode",
			// fillchars: blank-out every chrome filler nvim might draw.
			//   eob (end-of-buffer): no `~` markers in empty buffer space.
			//   stl/stlnc:           no `^` characters in the status-line
			//                        separator that nvim draws between
			//                        horizontally-split windows even with
			//                        laststatus=0. The terminal panel splice
			//                        overpaints this row with the tab bar,
			//                        but during transient frames (e.g. a
			//                        resize before splice runs) we don't
			//                        want path text or `^` glyphs to peek out.
			"set fillchars=eob:\\ ,stl:\\ ,stlnc:\\ ",
			// Empty status line — content (default = filename) would otherwise
			// flash through the separator row in the moment between nvim
			// repaint and splice overpaint.
			"set statusline=\\ ",
			// We dropped `keymodel=startsel,stopsel` and `selectmode=mouse,key`
			// — they routed mouse drags into Select mode, where the highlight
			// is barely visible. With them off, mouse drag enters Visual mode
			// (highlighted cleanly) which is what users actually expect.
			//
			// keymodel=startsel: Shift+Arrow extends a Visual selection (VSCode
			// style). selectmode is left empty (the default) so we get Visual
			// mode highlighting, not Select mode.
			"set keymodel=startsel,stopsel",
			"set selectmode=",
			"set virtualedit=onemore",
			"set clipboard=unnamedplus",
			"set guicursor=n-v-c-sm:block,i-ci-ve:ver25,r-cr-o:hor20",
			"set cursorline",
			"syntax on",
			// Code folding: gutter disabled (foldcolumn=0) to keep the
			// left of the buffer compact — folding still works via `za`/
			// `zM`/`zR` keyboard shortcuts and palette items. Re-enable
			// `foldcolumn=1` if a visual gutter is needed back.
			"set foldlevelstart=99",
			"set foldlevel=99",
			"set fillchars+=foldopen:▾,foldclose:▸,fold:\\ ,foldsep:│",
			// Custom statuscolumn via Lua so we don't get tripped up by Ex
			// command escape rules. Pattern: right-align + line number +
			// 2 trailing spaces — gives clear breathing room on BOTH sides
			// of the digit (left padding from numberwidth=5, right from
			// the trailing spaces).
			`lua vim.opt.statuscolumn = '%=%l  '`,
			// Word jumps with Ctrl+Right / Ctrl+Left in both normal and insert
			// mode. Insert-mode bindings use <C-O> so we don't drop out of insert.
			"nnoremap <silent> <C-Right> w",
			"nnoremap <silent> <C-Left> b",
			"inoremap <silent> <C-Right> <C-O>w",
			"inoremap <silent> <C-Left> <C-O>b",
			// Ctrl+Shift+Right / Ctrl+Shift+Left: extend Visual selection by
			// a word. From normal mode it enters Visual; from insert it
			// exits insert first; from visual it just extends.
			// Note: needs a terminal that distinguishes <C-S-Right> from
			// <C-Right> (kitty/CSI-u protocol — iTerm2, kitty, WezTerm,
			// ghostty). Older terminals will fall through to plain
			// Ctrl+Right behaviour (word jump without selection).
			"nnoremap <silent> <C-S-Right> vw",
			"nnoremap <silent> <C-S-Left> vb",
			"xnoremap <silent> <C-S-Right> w",
			"xnoremap <silent> <C-S-Left> b",
			"inoremap <silent> <C-S-Right> <Esc>vw",
			"inoremap <silent> <C-S-Left> <Esc>vb",
			// VSCode-style line-comment toggle. Most terminals send Ctrl+/
			// as the ASCII US byte (0x1f) which nvim sees as <C-_>; modern
			// kitty/CSI-u terminals deliver <C-/> distinctly. Bind both so
			// the action fires regardless of terminal protocol. These are
			// fallbacks on top of the keymap-layer dispatch in update.go;
			// they fire even if our outer routing misses the key (e.g. when
			// focus is briefly out of the editor). gc/gcc are Neovim 0.10+
			// built-ins and use `commentstring` per filetype automatically.
			"nnoremap <silent> <C-_> gcc",
			"nnoremap <silent> <C-/> gcc",
			"xnoremap <silent> <C-_> gc",
			"xnoremap <silent> <C-/> gc",
			"inoremap <silent> <C-_> <C-O>gcc",
			"inoremap <silent> <C-/> <C-O>gcc",
		} {
			_ = m.nvim.Command(cmd)
		}
		// LSP setup must register the FileType autocmd before we apply the
		// theme, so opening files later triggers Tree-sitter + LSP under the
		// final palette.
		_ = m.nvim.ExecLua(lspSetupLua)
		// Snippets: must come AFTER lspSetupLua so its <Tab>/<S-Tab> remaps
		// override the popup-completion handlers defined there. (Trying to
		// load both via the same chunk would force a single keymap, losing
		// either snippet expansion or popup acceptance.)
		_ = m.nvim.ExecLua(snippetsLua)
		// Find Results scratch buffer: syntax + jump-to-file keymaps +
		// global F4/Shift+F4 navigation. Filetype-driven so a fresh
		// search buffer just sets filetype=findresults to wire up.
		_ = m.nvim.ExecLua(findResultsLua)
		// Inline git diff gutter signs: green +, blue ~, red −. Refreshes
		// on save / buffer enter / 1-second idle. Failure is non-fatal —
		// non-git buffers just stay sign-less.
		_ = m.nvim.ExecLua(gitSignsLua)
		// Multi-cursor: bootstrap-clone vim-visual-multi (silent, idempotent,
		// non-fatal on failure), then run its setup Lua. If the clone fails
		// we log a warning to stderr but the editor keeps running normally.
		if path, warn := ensureVimVisualMulti(); warn != "" {
			fmt.Fprintln(os.Stderr, warn)
		} else if path != "" {
			_ = m.nvim.ExecLua(multiCursorSetupLua(path))
		}
		// DAP: bootstrap-clone mfussenegger/nvim-dap, then register adapters
		// for whichever debuggers are on PATH (delve, debugpy, node). Same
		// idempotent / non-fatal contract as the multi-cursor clone — a clone
		// failure surfaces a stderr warning and the Lua chunk still runs (it
		// just registers stub helpers so the Go-side palette commands don't
		// crash when the user invokes them).
		dapPath, dapWarn := ensureNvimDap()
		if dapWarn != "" {
			fmt.Fprintln(os.Stderr, dapWarn)
		}
		_ = m.nvim.ExecLua(dapSetupLua(dapPath))
		// Apply the VSCode Dark+ palette via Lua. Done after `syntax on` so
		// the highlight groups override the default scheme.
		_ = m.nvim.ExecLua(vscodeDarkPlusLua)
		// Bracket-pair colorization (HiPhish/rainbow-delimiters.nvim).
		// Runs after the theme so its RainbowDelimiter* overrides are the
		// final word; failure (offline, no git) is non-fatal.
		_ = m.nvim.ExecLua(bracketPairLua)
		// VSCode-feel: stay in insert mode whenever a normal buffer is active.
		// Guard on &modifiable && !&readonly so we don't trigger E5 on help
		// buffers, find-results scratch (modifiable=false), or `:view`-opened
		// readonly files. With cmdheight=0 an E5 becomes a hit-enter prompt
		// that blocks ALL input until <Enter> — that's what locked the
		// integrated terminal after the user clicked into a readonly buffer.
		_ = m.nvim.Command("autocmd BufEnter,BufNewFile,WinEnter * if &buftype == '' && &modifiable && !&readonly | startinsert | endif")
		// Same idea for the integrated terminal: every time focus lands on a
		// terminal buffer, drop into terminal-insert mode automatically. Without
		// this, leaving and returning to the terminal puts the user in normal
		// mode where typing fails with "E21 modifiable is off". Two trigger
		// points: TermOpen (immediately when termopen() spawns the job) and
		// BufEnter/WinEnter (any subsequent re-focus, e.g. via :wincmd or
		// our own toggle).
		// Persistent undo: write per-file undo history into ~/.local/share/
		// nvim/undo so Ctrl+Z keeps working across sessions. Best-effort —
		// if the directory can't be created, undo just stays in-memory like
		// before.
		_ = m.nvim.ExecLua(`
			local undodir = vim.fn.stdpath('data') .. '/termocode-undo'
			pcall(vim.fn.mkdir, undodir, 'p')
			vim.opt.undodir = undodir
			vim.opt.undofile = true
		`)
		// Highlight trailing whitespace so it's visible. The match group is
		// a stable ID; reusing it across :match calls would just replace
		// the previous one (no leak).
		_ = m.nvim.Command(`autocmd BufEnter,BufNewFile * silent! match TrailingWhitespace /\s\+$/`)
		_ = m.nvim.Command(`highlight TrailingWhitespace guibg=#3c1f1f`)
		_ = m.nvim.Command("autocmd TermOpen * startinsert")
		_ = m.nvim.Command("autocmd BufEnter,WinEnter * if &buftype == 'terminal' | startinsert | endif")
		// A left-click anywhere INSIDE a terminal-buffer window drops nvim
		// out of terminal-insert into Terminal-Normal mode (`nt`) — by
		// design in nvim with `mouse=a`, but it makes the integrated
		// terminal feel broken: the user clicks anywhere in the panel and
		// suddenly their typing produces normal-mode commands. Neither
		// BufEnter/WinEnter nor TermOpen fire on that transition (same buf,
		// same window), so the autocmds above don't help.
		//
		// ModeChanged with pattern `*:nt` fires the moment the click enters
		// Terminal-Normal — we synchronously re-enter terminal-insert so the
		// click lands as a no-op from the user's perspective. Available since
		// nvim 0.8; verified on the 0.9.5 floor we test against.
		//
		// IMPORTANT: this MUST be a synchronous startinsert (no vim.schedule).
		// Wrapping it in vim.schedule defers the bounce-back to the next event
		// loop tick — but any keystrokes the user types in that interval (in
		// fast click-then-type flows) are processed in nt mode, where letters
		// like `e`, `c`, `h` trigger normal-mode operator/motion commands.
		// The keystrokes never reach the shell, manifesting as "I clicked the
		// terminal and now I can't type". Synchronous startinsert closes the
		// window: by the time control returns from nvim_input_mouse, mode is
		// back to `t` and subsequent input goes to the shell.
		//
		// Trade-off: clicks inside the terminal can no longer be used for
		// vim-style visual selection / copy-mode in the buffer. That's the
		// right call for a terminal pane that's meant to feel like a real
		// terminal — the shell's own selection (Shift-click in most
		// emulators) and the OSC52 path are still available for copying.
		_ = m.nvim.ExecLua(`
			vim.api.nvim_create_autocmd('ModeChanged', {
				pattern = '*:nt',
				callback = function()
					local b = vim.api.nvim_get_current_buf()
					if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == 'terminal' then
						pcall(vim.cmd, 'startinsert')
					end
				end,
			})
		`)
		// Auto-save normal-file buffers on focus loss / buffer leave.
		_ = m.nvim.Command("autocmd BufLeave,FocusLost * if &buftype == '' && !empty(expand('%')) | silent! write | endif")
		// Auto-reload buffers when their on-disk file changes (e.g. `git pull`,
		// edits from another editor). Combined with `set autoread` above, the
		// reload is silent; buffers with unsaved local changes are preserved
		// by the default autoread behaviour.
		_ = m.nvim.Command("autocmd FocusGained,BufEnter,CursorHold,CursorHoldI * silent! checktime")
		// File-watcher toast: when a buffer is reloaded from disk (autoread
		// kicks in after checktime detects a change), record the path in a
		// global so the next FetchState round-trip can surface a "Reloaded
		// <basename>" toast. Lua side keeps it idempotent — the same path is
		// not re-recorded back-to-back by tracking a monotonically-increasing
		// counter the Go side compares against.
		_ = m.nvim.ExecLua(`
			vim.g.termocode_last_reload = ''
			vim.g.termocode_reload_seq = 0
			vim.api.nvim_create_augroup('TermocodeFileReload', { clear = true })
			vim.api.nvim_create_autocmd('FileChangedShellPost', {
				group = 'TermocodeFileReload',
				callback = function(args)
					local name = args.file or vim.api.nvim_buf_get_name(args.buf or 0)
					if name == nil or name == '' then return end
					vim.g.termocode_last_reload = name
					vim.g.termocode_reload_seq = (vim.g.termocode_reload_seq or 0) + 1
				end,
			})
		`)
		_ = m.nvim.Command("startinsert")
		return nvim.ReadyMsg{}
	}
}

// pushClosed records the path of a buffer that was just closed, dropping the
// oldest entry once the ring exceeds closedRingCap. Empty paths and exact
// duplicates of the most recent entry are skipped (so closing the same buffer
// twice in a row doesn't waste a slot).
func (m *Model) pushClosed(path string) {
	if path == "" {
		return
	}
	if n := len(m.closedRing); n > 0 && m.closedRing[n-1] == path {
		return
	}
	m.closedRing = append(m.closedRing, path)
	if len(m.closedRing) > closedRingCap {
		// Drop oldest. Copy avoids holding a reference to the truncated head.
		m.closedRing = append([]string(nil), m.closedRing[len(m.closedRing)-closedRingCap:]...)
	}
}

// popClosed removes and returns the most recently closed path. The boolean
// reports whether the ring had any entry.
func (m *Model) popClosed() (string, bool) {
	n := len(m.closedRing)
	if n == 0 {
		return "", false
	}
	path := m.closedRing[n-1]
	m.closedRing = m.closedRing[:n-1]
	return path, true
}

// Close releases nvim resources. Call from main after tea.Program exits.
func (m *Model) Close() error {
	if m.nvim == nil {
		return nil
	}
	return m.nvim.Close()
}
