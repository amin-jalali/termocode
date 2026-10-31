package app

import (
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"context"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/addai"
	"github.com/amin-jalali/termocode/internal/anim"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/debounce"
	"github.com/amin-jalali/termocode/internal/editor"
	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/findbar"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/keyhint"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/preview"
	"github.com/amin-jalali/termocode/internal/profileform"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/recents"
	"github.com/amin-jalali/termocode/internal/replacebar"
	"github.com/amin-jalali/termocode/internal/search"

	"github.com/amin-jalali/termocode/internal/clipring"
	"github.com/amin-jalali/termocode/internal/statusbar"
	"github.com/amin-jalali/termocode/internal/tabbar"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
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
	// agentRollSig is the agent roll-up this window wrote last (OMX-35,
	// ai_agent_rollup.go).
	agentRollSig string

	crash *crashUI // recovered panics → toasts (crash_ui.go)
	// ws is the open workspace (ADR 0018), set by New from the process
	// workspace. Read it through m.workspace() / m.rootDir().
	ws                      workspace.Workspace
	activity                activity.Model
	explorer                explorer.Model
	bulkRename              bulkRenameState
	explorerPreviewWidth    int
	editor                  editor.Model
	tabs                    tabbar.Model
	status                  statusbar.Model
	picker                  picker.Model
	menu                    menu.Model
	find                    findbar.Model
	confirm                 confirm.Model
	pendingContainerID      string
	pendingContainerStop    bool
	pendingContainerProject string
	pendingContainerDir     string
	pendingContainerFiles   []string
	pendingHostArgs         []string
	pendingContextName      string
	pendingHostOperation    string
	pendingHostTarget       string
	pendingImageRef         string
	pendingRunName          string
	pendingRunPorts         []string
	pendingRunEnv           []string
	preview                 preview.Model
	prompt                  prompt.Model
	recents                 recents.Model
	replace                 replacebar.Model
	search                  search.Model
	toast                   toast.Model
	themeID                 string // active theme id (drives the nvim colourscheme)
	// Color Theme picker live preview: the theme to restore on Esc.
	themePreviewFrom string
	themePreviewing  bool
	// Settings rows with fixed choices open a picker; this is the row.
	settingChoiceRow string
	// Image preview (image_view.go) and kitty images in use.
	imagePrev *imagePreview
	imageOpen bool
	kittyUsed bool
	// Animations (anim.go): overlay open progress 0→1, and whether a
	// tick is already scheduled.
	overlayAnim anim.Value
	// resizeT coalesces nvim resizes during splitter drags and window
	// resizes; lastWinResize is when the last WindowSizeMsg came.
	resizeT       resizeThrottle
	lastWinResize time.Time
	animTicking   bool
	keys          keymap.KeyMap
	nvim          *nvim.Client
	bufs          []nvim.BufferInfo
	activeBuf     int

	// keymapProblems are keymap.json entries dropped at startup (typos);
	// shown once in a toast on nvim.ReadyMsg.
	keymapProblems []string

	// closedRing remembers the paths of recently-closed editors so the
	// "Reopen Closed Editor" action can pop the most recent one. Capacity is
	// fixed (closedRingCap); when full, the oldest entry is dropped.
	closedRing []string

	gitIsRepo     bool
	gitLoaded     bool   // a GitMsg was applied (gitEnsureLoaded)
	gitRemoteBusy string // running push/pull/fetch/sync note, "" when idle
	gitBranch     git.Branch
	gitRebase     git.RebaseState // a stopped interactive rebase (git_rebase.go)
	gitFiles      []git.FileStatus
	gitCursor     int // index into gitVisibleRows() for the ViewGit sidebar

	// gitViewTree selects the Source-Control file list layout: true renders a
	// collapsible directory tree (the default, like the file explorer), false
	// a flat one-row-per-file list. Persisted in the session file.
	gitViewTree bool
	// gitCollapsed holds the repo-relative paths of directories the user has
	// collapsed in tree view. Absent = expanded, so a fresh tree starts fully
	// open. In-memory only (not persisted across runs).
	gitCollapsed map[string]bool

	// Source Control accordion section fold state (in-memory; all expanded by
	// default). gitCommits holds the current branch's history for the GRAPH
	// section, refreshed alongside status.
	gitStagedCollapsed  bool
	gitChangesCollapsed bool
	gitGraphCollapsed   bool
	// gitConflictsCollapsed folds the CONFLICTS section (git_conflicts.go).
	gitConflictsCollapsed bool
	// clone is the in-flight "Clone Repository" request (git_clone.go).
	clone      cloneState
	gitCommits []git.GraphCommit
	// gitCommitGroups[i] is the GRAPH group (Today, Yesterday, …) of
	// gitCommits[i], worked out when the commits arrive.
	gitCommitGroups []string
	// gitRowMemo keeps drawn GRAPH rows of gitCommits (git_panel.go).
	gitRowMemo *gitCommitRowMemo
	// gitGraphMore: older commits exist past the loaded ones.
	gitGraphMore bool
	// gitGraphShown is how many commits GRAPH shows (0 = the default);
	// "Show 15 more" raises it.
	gitGraphShown int
	// gitGroupFolded holds the GRAPH groups the user folded.
	gitGroupFolded map[string]bool
	// gitGraphAt is when gitCommits was last read (pollGitCmd reads it
	// again when it is older than gitGraphMaxAge).
	gitGraphAt time.Time
	// termBlurred: the terminal said its window lost focus (BlurMsg);
	// gitBlurredSkips counts git ticks skipped meanwhile.
	termBlurred     bool
	gitBlurredSkips int
	// gitGate lets the git poll sleep while nothing happens
	// (git_poll_idle.go).
	gitGate *gitPollGate
	// gitSeq: the Seq of the git read applied last (git.go).
	gitSeq uint64

	// Always-visible commit box (VSCode-style). gitCommitMsg is the single-line
	// inline message buffer; gitCommitFocused routes keystrokes to it (vs the
	// accordion navigation); gitCommitCaret is the rune-index caret position.
	// The multi-line prompt overlay (openCommitPrompt) remains for commit bodies.
	gitCommitMsg     string
	gitCommitFocused bool
	gitCommitCaret   int

	// VSCode-style mouse hover. hoverX/hoverY are the absolute screen cell
	// under the pointer (or -1 when unknown). mouseAllMotion mirrors the live
	// xterm tracking mode so the Update loop only emits a switch when it
	// changes — all-motion for hover, downgraded to button-only while a text
	// input is focused (avoids the SGR-fragmentation leak documented in
	// cmd/termocode/main.go).
	hoverX, hoverY int
	mouseAllMotion bool
	// tip is the hover label over icon buttons (tooltip.go).
	tip tooltipState
	// hints tracks a leader sequence for the key hint popup (key_hints.go).
	hints *keyhint.Tracker
	// hintSrc feeds it (nil = nvim); hintsOff / hintsInit cache key_hints.
	hintSrc             hintSource
	hintsOff, hintsInit bool
	// mouseProbe is set only on the copy a test probes (probeClick): the
	// mouse code then records which target a click resolves to and stops
	// before it acts (hit_targets.go).
	mouseProbe *string

	// termGen is the persistent-session attach whose terminal caps we
	// draw for (TerminalChangedMsg); View marks every frame with it so
	// the session server knows where output for that terminal starts.
	termGen int

	// Source Control commit hover card: the SHA the pointer is over and a
	// cache of fetched commit metadata (author/date/body/stat) keyed by SHA.
	hoverCommitHash string
	commitDetails   map[string]git.CommitDetail

	// Side-by-side diff: while one is showing, the pointer drives scrolling.
	// gitDiffHoverPane is the diff pane last under the pointer (0=left,
	// 1=right, -1=unknown) so we only re-focus when it crosses the split.
	gitDiffActive    bool
	gitDiffHoverPane int

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
	// replaceRepl / replaceTargets hold the replacement text and the
	// per-file match counts while the Replace-in-Workspace confirm dialog
	// is open (see replace_workspace.go).
	replaceRepl    string
	replaceTargets []search.FileCount

	// findRun tracks the streamed search feeding the Find Results buffer
	// (see find_results.go).
	findRun findResultsRun

	// pinnedBufs maps buffer-id → pin order (1, 2, 3 …). Pinned buffers
	// move to the front of the tab bar, keeping their relative order. The
	// in-memory state is rebuilt every StateMsg via reorderPinned, so we
	// don't need to persist it across sessions.
	pinnedBufs map[int]int
	pinOrder   int

	// closeQueue holds the tabs Close Others / Close All to the Right
	// still have to close while a "Save changes?" dialog is open
	// (drainCloseQueue).
	closeQueue []int

	// snippetPickerIndex maps picker IDs (e.g. "snip-iferr") to the snippet
	// body so insertSnippetBody knows what to expand. Populated each time
	// the snippet picker opens.
	snippetPickerIndex map[string]string

	// extras holds transient state for Group H flows (keymap conflict,
	// settings export/import, user snippets manager). See settings_sync.go.
	extras extrasState

	// xfer is the running download (transfer.go), nil when none.
	xfer *xferRun

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
	// to ~10 Hz instead. stateFetchPending is set while a trailing fetch
	// is scheduled, so the last redraw of a burst is never left unread.
	stateLastFetch    time.Time
	stateFetchPending bool

	// sess keeps the editor part of the session (open files, cursors,
	// expanded folders) in memory and writes it coalesced (N1).
	sess *sessionWriter

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
	// cursorCol is the matching 1-based virtual column (status bar).
	cursorCol int

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
	// terminalBarHeld / terminalBarHoldRows: while the panel bar is
	// dragged, the bar stays at the panel height nvim has drawn until
	// nvim's redraw for the new height arrives, so the bar and the split
	// under it move together (terminalBarIndex).
	terminalBarHeld     bool
	terminalBarHoldRows int

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
	// inPaneWin: nvim's current window is a shell pane of the active terminal
	// tab, in any mode (terminal_split.go). inTerminal is the typing subset.
	inPaneWin bool
	// skipShell holds the actions whose keys reach termocode even while
	// inTerminal (config.json "terminal_skip_shell"); nil = the defaults.
	skipShell map[keymap.Action]bool
	// termScrolled is true while the mouse wheel scrolls the terminal's
	// scrollback (terminal_scroll.go): nvim stays in Terminal-Normal mode
	// until the next key or click.
	termScrolled bool
	// indentLabel is the status bar's indentation chip, from StateMsg.
	indentLabel string
	// hiddenTerminalTabs are the tabs of a panel hidden with Ctrl+T; their
	// shells keep running until the panel shows again (terminal_hide.go).
	hiddenTerminalTabs   []terminalTab
	hiddenTerminalActive int
	// termRestorePending are saved terminals of a panel that was hidden at
	// the last quit: they come back on the next Ctrl+T (terminal_restore.go).
	termRestorePending *savedTerminals
	// termRestoreReady: the saved terminals were opened at start, so the
	// live ones may now be written over them. termRestoreOff and
	// termRestoreCmds are the settings, read at start.
	termRestoreReady bool
	termRestoreOff   bool
	termRestoreCmds  bool
	// termHints are the one-line notes of restored panes, by terminal
	// buffer, shown in the pane's title bar until the user types there.
	termHints map[int]termHint

	// ── Bottom panel framework (panel.go) ──────────────────────────────
	// panelTabs lists the open NON-terminal tabs (Output, Problems, …) in
	// bar order; terminal tabs stay in terminalTabs. panelActive is the
	// kind of the visible tab — panelKindTerminal (the zero value) means
	// "terminalTabs[terminalActiveTab]". panelFocused routes keys to the
	// active non-terminal tab (set by a click inside its content area).
	// panelHostBuf is the placeholder scratch buffer that holds the split
	// open while no terminal tab exists.
	panelTabs    []panelKind
	panelActive  panelKind
	panelFocused bool
	panelHostBuf int
	// output holds the named Output channels (output.go). Pointer so any
	// copy of Model — and background goroutines — append to the same store.
	output *outputStore
	// debounce coalesces noisy events (nvim notifications, keystrokes)
	// into one message per quiet period. Shared by pointer.
	debounce *debounce.Debouncer
	// Group C — Problems panel (problems_panel.go) and task runner
	// (tasks_run.go). Pointers so copies of Model share the state.
	problems *problemsPanel
	tasks    *taskRunner
	// testView is the Testing view + test runner state (Group E,
	// test_runner.go / test_explorer.go). Pointer, shared by copies.
	testView *testsState
	// services is the Services view state (services_state.go). Pointer,
	// shared by copies; idle (no process, no goroutine) until shown.
	services *servicesState
	// kube is the Kubernetes view state (kube_state.go); idle until shown.
	kube *kubeState
	// databases is lazy and holds idle sessions only while the view is open.
	databases *dbState
	// itermoc is the Itermoc view (profiles, models, services, outside
	// agents): lazy, a pointer so a render can refresh its snapshot.
	itermoc *itermocState
	// itermocBack is the sidebar view (and whether the sidebar was shown)
	// before Alt+Shift+I / Alt+O opened Itermoc; the same key goes back.
	itermocBack     activity.View
	itermocBackShow bool
	// runs is the Runs view and its page (runs_state.go); nil until it opens.
	runs *runsState
	// itermocPage is the detail page of the object picked in the list: one
	// editor tab, reused (itermoc_page.go). nil until the first one opens.
	itermocPage *itermocPageState
	// ext is the extension host state (Group I, ext.go). Pointer so
	// Model copies share it; nil in bare test Models.
	ext *extState

	// layoutHint is the name of the layout opened a moment ago; the status
	// bar shows it for layoutHintFor (layout_apply.go). layoutHintGen tells
	// the clear message of an older hint from the current one.
	layoutHint     string
	layoutSaveSrc  layout.Source // where Layout: Save As… writes
	layoutCardName string        // the layout a preview card is about
	layoutHintGen  int

	focus       Focus
	gate        *frameGate // draw at most once per frame (frame_gate.go)
	showExp     bool
	pickerOpen  bool
	pickerKind  pickerKindEnum
	menuOpen    bool
	menuKind    menuKindEnum
	menuPath    string
	menuTabID   int
	findOpen    bool
	replaceOpen bool
	confirmOpen bool
	confirmKind confirmKindEnum
	// reviewOpen shows the AI review card (ai_review.go; its state is in
	// m.ai.review).
	reviewOpen bool
	// connectOpen shows the Connect-a-service card (ai_mcp_connect.go).
	connectOpen bool
	connect     connectcard.Model
	// addAIOpen shows the Add AI card (ai_add.go).
	addAIOpen bool
	addAI     addai.Model
	// profileFormOpen shows the profile step form (ai_profile_form.go).
	profileFormOpen bool
	profileForm     profileform.Model
	profileFormStep int // the step the Remove question is about
	// profileFormBack is the step the Add AI card was opened for from the
	// form (nil: the card is not the form's); the card's end goes back to it.
	profileFormBack *profileform.AddAIMsg
	// commitView is the Source Control commit view (git_commit_view.go;
	// nil until a commit is opened).
	commitView *commitViewState
	rebase     *gitRebaseState // the interactive rebase card (git_rebase.go)
	gitUndo    *gitUndoState   // undo with the reflog (git_undo.go)
	merge      *mergeViewState // the three-way merge view (git_merge_view.go)
	// fixupTarget is the commit the fixup confirm is about (git_fixup.go).
	fixupTarget string
	// Hot exit (hot_exit.go): this workspace's backup dir, this run's
	// live dir, the backups waiting for Restore / Discard, and the ones
	// found while another dialog was open (asked once it closes).
	hotExitRoot       string
	hotExitLive       string
	hotExitPending    []hotBackup
	hotDiskChanged    []hotBackup // restored files that changed on disk (hot_exit_disk.go)
	workspaceGone     string      // the recent workspace folder that is gone (workspaces.go)
	hotExitWaiting    []hotBackup
	saveAfter         saveContinuation
	confirmTargetID   int
	keyCardGuide      help.Loc             // the key card's guide page (keycard.go)
	keyCardBack       func(*Model) tea.Cmd // runs after the key card closes
	confirmTargetPath string
	// confirmTargetPaths is the absolute-path list for a bulk-delete
	// confirm flow (confirmKindDeletePath with len > 1). When the user
	// confirms, every entry is os.RemoveAll'd and any matching open
	// buffer is :bdelete!'d. Empty means single-path delete via
	// confirmTargetPath; populated means bulk delete.
	confirmTargetPaths []string
	confirmLines       [2]int // line range of the "discard lines" card (git_lines.go)
	previewOpen        bool
	help               helpState // the guide viewer's place and history (help.go)
	promptOpen         bool
	promptKind         promptKindEnum
	promptPath         string
	recentsOpen        bool
	searchOpen         bool
	termOpen           bool
	w, h               int
	err                string
	nvimAttached       bool
	// nvimDead: the nvim process stopped on its own; the "Editor process
	// stopped" question is up (nvim_exit.go).
	nvimDead bool

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
	// actionsWidthSet: the user dragged the edge (or a saved width was
	// restored); until then the width follows the window (autoActionsWidth).
	actionsWidthSet bool

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
	// debug is the Run & Debug state (Group D, debug_state.go).
	debug *debugState

	// lspMgr: LSP/DAP installer state (manager picker, background
	// installs, one-time "no language server" toast, status-bar chip).
	lspMgr *lspInstallState

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

	// importOpen shows the review panel of Preferences: Import from
	// Another Editor… (import_editor.go); imp holds its state.
	importOpen bool
	imp        *importState

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

	// ai is the Group A AI assistant state (ai_state.go). Pointer so value
	// copies and the streaming goroutines share one instance.
	ai *aiState
	// ci watches the GitHub CI of the branch (ci_watch.go, G-CI).
	ci *ciWatch
	// upd is the termocode update state (update_check.go, G-UPD).
	upd *updateWatch
	// pr is the pull request UI state (pr_state.go, G-PR).
	pr *prState
	// mcpServe is termocode as an MCP server: the socket listener and the
	// confirm queue of outside agents (mcp_serve*.go); nil when not serving.
	mcpServe *mcpServeState
	// mcpServeForAgent: an agent tab asked for the MCP server (mcp_server
	// auto keeps it running then).
	mcpServeForAgent bool
	// agentMCPFiles are the temp MCP config files of running agents,
	// by agent id (agent_mcp.go).
	agentMCPFiles map[string]string
	// agentMCPPending waits for the OpenCode project-file card;
	// agentMCPDeclined remembers "Not now"; agentMCPTipped the hand-tip
	// toasts already shown (agent_mcp_card.go).
	agentMCPPending  *agentMCPCard
	agentMCPDeclined map[string]bool
	agentMCPTipped   map[string]bool
	// mcpServeErr: why the listener did not start ("" when it did or was
	// not asked to); the status screen shows it (mcp_serve_status.go).
	mcpServeErr string
	// mcpServeStopped: the user chose "Stop listening" on the status
	// screen (this session only; the setting is untouched).
	mcpServeStopped bool
	// web is the web hub (web_hub.go): the web gateway's way into this
	// UI; nil unless the web side is on.
	web *webHub
	// webPanel is the web gateway the user started (web_panel.go).
	webPanel *webPanelState
	// webStartCall is the `termocode web` request the start card answers.
	webStartCall *webCall
	// webAcc is pairing and the device list (web_access.go).
	webAcc *webAccessState
	// HTTP client for .http files (http_state.go); made on first use.
	http *httpState
	// secrets is the secret store's unlock flow (secrets_ui.go); the store
	// itself is m.secretStore().
	secrets *secretsState
	// setup is the first-run setup card on the Welcome page
	// (setup_card*.go); shared by Model copies.
	setup *setupState

	// lastLineCopy is the text of the last whole-line Ctrl+C / Ctrl+X (no
	// selection). While the OS clipboard still holds it, Ctrl+V pastes it
	// as a new line above the cursor, like VSCode.
	lastLineCopy string
}

// welcomeShowing reports whether the start page is currently rendered in
// the editor pane (mirrors the same condition view.go uses to choose
// renderWelcome over m.editor.View).
func (m Model) welcomeShowing() bool {
	return m.editorEmpty()
}

// editorEmpty reports whether the editor has nothing to show, so the start
// page takes its place: no file, no terminal, and the current buffer is not
// one of termocode's nameless views — the side-by-side diff's HEAD pane
// (gitDiffActive) or a commit diff / other scratch buffer with a filetype.
func (m Model) editorEmpty() bool {
	return m.editor.Path() == "" && len(m.bufs) <= 1 && !m.termOpen && !m.gitDiffActive && m.editor.Lang() == ""
}

func New() Model {
	// Load the persisted theme (if any) before constructing components, so
	// the first frame already renders in the user's chosen palette. Missing
	// or corrupt config falls back to VSCode Dark+ silently — see
	// theme.LoadAndApply for the precise rules.
	named := theme.LoadAndApply()

	// Layer the user's keymap overrides on top of the defaults. Missing /
	// corrupt files yield an empty map so startup is uninterruptible by a
	// hand-edited config; dropped entries are toasted once nvim is ready.
	overrides, keymapProblems := keymap.LoadOverridesReport("")
	keys := keymap.Default().MergeInto(overrides)

	// TERMOCODE_NO_NVIM=1 runs without the embedded editor (tests and
	// internal/testkit use it for fast, nvim-free UI runs).
	var client *nvim.Client
	var err error
	noNvim := os.Getenv("TERMOCODE_NO_NVIM") == "1"
	if !noNvim {
		client, err = nvim.New()
	}
	// Terminal panel height: prefer the value persisted from the previous
	// session so the user's drag-set preference survives a relaunch. Missing
	// / corrupt / out-of-range values fall back to the default.
	sess := loadSession()
	explorerW := defaultExplorerWidth
	if w := sess.ExplorerWidth; w >= explorerMinWidth && w <= explorerMaxWidth {
		explorerW = w // the sidebar width dragged last time
	}
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
	actionsWidthSet := false
	if sess.ActionsWidth >= actionsMinWidth {
		actionsWidth = sess.ActionsWidth
		actionsWidthSet = true
	}

	// Source Control file list defaults to the collapsible tree view. A
	// persisted preference (pointer, so an explicit prior choice wins over
	// the default) overrides it.
	gitViewTree := true
	if sess.GitViewTree != nil {
		gitViewTree = *sess.GitViewTree
	}

	ws := procWorkspace()
	m := Model{
		ws:               ws,
		sess:             newSessionWriter(),
		activity:         activity.New(),
		explorer:         newExplorerFor(ws),
		tabs:             tabbar.New(),
		toast:            toast.New(),
		status:           statusbar.New(),
		lspMgr:           newLSPInstallState(),
		themeID:          named.ID,
		keys:             keys,
		keymapProblems:   keymapProblems,
		focus:            FocusEditor,
		gate:             &frameGate{on: throttleFrames},
		showExp:          true,
		inlayHintsOn:     true,
		lineNumbersOn:    true,
		gitViewTree:      gitViewTree,
		gitCollapsed:     map[string]bool{},
		hoverX:           -1,
		hoverY:           -1,
		mouseAllMotion:   mouseHover,
		commitDetails:    map[string]git.CommitDetail{},
		gitDiffHoverPane: -1,
		explorerWidth:    explorerW,
		terminalRows:     termRows,
		actionsOpen:      actionsOpen,
		actionsPinned:    actionsPinned,
		actionsWidth:     actionsWidth,
		actionsWidthSet:  actionsWidthSet,
		output:           newOutputStore(),
		debounce:         debounce.New(),
		problems:         newProblemsPanel(), // Group C
		tasks:            newTaskRunner(),    // Group C
		testView:         newTestsState(),    // Group E
		services:         newServicesState(),
		kube:             newKubeState(),
		databases:        newDBState(),
		ext:              newExtState(),    // Group I
		ai:               newAIState(),     // Group A
		secrets:          &secretsState{},  // U19 SS-3
		setup:            newSetupState(),  // setup card (setup_card_ui.go)
		debug:            loadDebugState(), // Group D (shared by Model copies)
		crash:            newCrashUI(),
	}
	if noNvim {
		m.editor = editor.New(nil)
	} else if err != nil {
		m.editor = editor.New(nil)
		m.err = "nvim: " + err.Error()
	} else {
		m.editor = editor.New(client)
		m.nvim = client
		// nvim → Go event channel: subscribe every method registered via
		// registerNotifyHandler (notify.go) before Lua can send any.
		m.subscribeNotify()
	}
	m.clipRing = clipring.New()
	m.gitGate = &gitPollGate{}
	m.primeLiveSettings()
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.HideCursor, fetchGitCmd(), gitAutoRefreshTick(), m.secretsWarmReadyCmd()}
	if cmd := m.initClipboardCapture(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := fontTipCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.setupInitCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.extProcInitCmd(); cmd != nil { // N6b: process extensions
		cmds = append(cmds, cmd)
	}
	if cmd := m.aiProfileErrorsCmd(); cmd != nil { // broken ai/profiles.json
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, aiRunsStartupCmd())         // prune old agent run snapshots
	cmds = append(cmds, m.secretsMigrateStartCmd()) // plain credentials.json → safer store (SS-4)
	cmds = append(cmds, m.updateInitCmd())          // G-UPD: a newer release? (update_check.go)
	cmds = append(cmds, m.crash.waitCmd())          // recovered panics (crash_ui.go)
	cmds = append(cmds, agentCtlInitCmd())          // OMX-33: the agent control socket (agent_ctl.go)
	cmds = append(cmds, m.explorer.ScheduleLoads()) // FIX-2: a container root's first listing
	// The card about a crash of the last run; with nvim it comes once
	// the unsaved work is back (nvim ready).
	if m.nvim == nil {
		cmds = append(cmds, m.crashCardCmd())
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
		// First: vim.notify → toasts. With cmdheight=0 a notify would
		// open a hit-enter prompt and block every later RPC call.
		_ = m.nvim.ExecLua(notifyLua)
		// A container workspace: its files open as remote buffers. Before
		// any file is opened (session restore, hot exit).
		if err := m.enableRemoteBuffers(m.nvim); err != nil {
			recordError("remote buffers: " + err.Error())
		}
		for _, cmd := range []string{
			"set termguicolors",
			"set number",
			"set mouse=a",
			"set tabstop=4 shiftwidth=4 expandtab",
			"set scrolloff=4",
			// Scroll the view several lines per wheel notch (not 1–3) so the
			// cursor jumps in bigger steps and there are far fewer redraws per
			// scroll — keeps the side-by-side diff snappy.
			"set mousescroll=ver:6,hor:6",
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
			// selection=exclusive: the character under the cursor is not
			// part of the selection, as in VSCode. With the default
			// (inclusive) Shift+Right ×3 selected 4 characters, and a
			// mouse drag one more than the pointer covered.
			"set selection=exclusive",
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
			// The last cell is the git change mark (git_signs_lua.go): this
			// statuscolumn has no sign column, so signs alone never show.
			`lua _G.TcGitMark = _G.TcGitMark or function() return ' ' end; vim.opt.statuscolumn = '%=%l %{%v:lua.TcGitMark()%}'`,
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
		// VSCode-feel: stay in insert mode whenever a normal buffer is active.
		// Guard on &modifiable && !&readonly so we don't trigger E5 on help
		// buffers, find-results scratch (modifiable=false), or `:view`-opened
		// readonly files. With cmdheight=0 an E5 becomes a hit-enter prompt
		// that blocks ALL input until <Enter> — that's what locked the
		// integrated terminal after the user clicked into a readonly buffer.
		// Defined first, before the slow part of the setup (LSP, theme,
		// plugins): the UI is usable before ReadyMsg, and a file opened in
		// that time must start in Insert mode like any later one. A file
		// opened even earlier (before this line) gets the startinsert below.
		_ = m.nvim.Command("autocmd BufEnter,BufNewFile,WinEnter * if &buftype == '' && &modifiable && !&readonly | startinsert | endif")
		_ = m.nvim.Command("if &buftype == '' && &modifiable && !&readonly && expand('%') != '' | startinsert | endif")
		runNvimSetupGate()
		// LSP setup must register the FileType autocmd before we apply the
		// theme, so opening files later triggers Tree-sitter + LSP under the
		// final palette.
		_ = m.nvim.ExecLua(lspSetupLua)
		// Snippets: must come AFTER lspSetupLua so its <Tab>/<S-Tab> remaps
		// override the popup-completion handlers defined there. (Trying to
		// load both via the same chunk would force a single keymap, losing
		// either snippet expansion or popup acceptance.)
		_ = m.nvim.ExecLua(snippetsLua)
		// Group A: AI ghost text (_G._termocode_ai). After snippetsLua so
		// its Tab handler can call _termocode_ai.accept() first.
		_ = m.nvim.ExecLua(aiLua)
		// Group H: user snippets.json (layered over the bundled table) and
		// Neovim's built-in EditorConfig support.
		m.setupUserSnippets()
		_ = m.nvim.ExecLua(editorconfigLua)
		// Find Results scratch buffer: syntax + jump-to-file keymaps +
		// global F4/Shift+F4 navigation. Filetype-driven so a fresh
		// search buffer just sets filetype=findresults to wire up.
		_ = m.nvim.ExecLua(findResultsLua)
		// HTTP client: .http filetype + lens, the HTTP Response page.
		_ = m.nvim.ExecLua(httpBufferLua)
		_ = m.nvim.ExecLua(httpPageLua)
		_ = m.nvim.ExecLua(dbConsoleLua)
		_ = m.nvim.ExecLua(dbPageLua)
		// Inline git diff gutter signs: green +, blue ~, red −. Refreshes
		// on save / buffer enter / 1-second idle. Failure is non-fatal —
		// non-git buffers just stay sign-less.
		_ = m.nvim.ExecLua(gitSignsLua)
		// Merge-conflict highlights + ]x / [x (git_conflicts.go).
		_ = m.nvim.ExecLua(conflictLua())
		// Bundled plugins (plugins.go): set up the ones already downloaded.
		// Missing ones are cloned in the background after ReadyMsg and set
		// up when they arrive; never wait for the network here.
		// Multi-cursor (vim-visual-multi).
		m.setupPlugin(pluginMultiCursor, pluginMultiCursor.installedDir())
		// DAP (nvim-dap): register adapters for whichever debuggers are on
		// PATH (delve, debugpy, node). Runs without the plugin too: it then
		// registers stub helpers so the Go-side palette commands don't crash.
		m.setupPlugin(pluginDap, pluginDap.installedDir())
		// Apply the active theme's colourscheme via Lua. Done after `syntax
		// on` so the highlight groups override the default scheme.
		m.applyNvimThemeLua()
		// Bracket-pair colorization (HiPhish/rainbow-delimiters.nvim).
		// Runs after the theme so its RainbowDelimiter* overrides are the
		// final word.
		m.setupPlugin(pluginBracketPair, pluginBracketPair.installedDir())
		// Group A: ghost-text colors (after the theme's `hi clear`) + the
		// inline-completion toggle.
		m.aiAfterAttach()
		// Same idea for the integrated terminal: every time focus lands on a
		// terminal buffer, drop into terminal-insert mode automatically. Without
		// this, leaving and returning to the terminal puts the user in normal
		// mode where typing fails with "E21 modifiable is off". Two trigger
		// points: TermOpen (immediately when termopen() spawns the job) and
		// BufEnter/WinEnter (any subsequent re-focus, e.g. via :wincmd or
		// our own toggle).
		// Persistent undo (undo_lua.go).
		_ = m.nvim.ExecLua(persistentUndoLua)
		// Highlight trailing whitespace so it's visible. The match group is
		// a stable ID; reusing it across :match calls would just replace
		// the previous one (no leak).
		_ = m.nvim.Command(`autocmd BufEnter,BufNewFile * silent! match TrailingWhitespace /\s\+$/`)
		_ = m.nvim.Command(`highlight TrailingWhitespace guibg=#3c1f1f`)
		_ = m.nvim.Command("autocmd TermOpen * startinsert")
		_ = m.nvim.Command("autocmd BufEnter,WinEnter * if &buftype == 'terminal' | startinsert | endif")
		// Restore editor chrome (line-number gutter, signcolumn, cursorline,
		// custom statuscolumn) whenever a normal file buffer enters a window.
		// openTerminalPanel strips all of these WINDOW-LOCAL from the terminal
		// split; without this, a file buffer that ever lands in that window —
		// e.g. the shell exited and nvim reused the de-chromed window — renders
		// with no line numbers, shifted hard left, and no indent guides. The
		// number column honours the global `set number` toggle (vim.o.number),
		// so toggling line numbers off still works; statuscolumn mirrors it so
		// `%=%l` doesn't keep drawing numbers after a toggle-off. Idempotent on
		// already-correct windows, so firing on every BufWinEnter is cheap.
		_ = m.nvim.ExecLua(`
			vim.api.nvim_create_autocmd({'BufWinEnter', 'WinEnter'}, {
				callback = function(ev)
					if vim.bo[ev.buf].buftype ~= '' then return end
					local w = vim.api.nvim_get_current_win()
					pcall(function()
						vim.wo[w].number       = vim.o.number
						vim.wo[w].signcolumn   = 'auto'
						vim.wo[w].cursorline   = true
						vim.wo[w].foldcolumn   = '0'
						vim.wo[w].statuscolumn = vim.o.number and '%=%l %{%v:lua.TcGitMark()%}' or ''
					end)
				end,
			})
		`)
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
					-- The wheel scrolls the scrollback in nt mode (terminal_scroll.go).
					if vim.g.termocode_term_scroll then return end
					local b = vim.api.nvim_get_current_buf()
					if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == 'terminal' then
						pcall(vim.cmd, 'startinsert')
					end
				end,
			})
		`)
		// No save on buffer leave / focus loss here: files are written only
		// by the user (Ctrl+S, Save All, the quit / close questions) or by
		// the auto_save setting when it is on (toggleAutoSave).
		// Auto-reload buffers when their on-disk file changes (e.g. `git pull`,
		// edits from another editor). Combined with `set autoread` above, the
		// reload is silent; buffers with unsaved local changes keep them
		// (fileReloadLua answers the W12 prompt that would block nvim).
		_ = m.nvim.Command("autocmd FocusGained,BufEnter,CursorHold,CursorHoldI * silent! checktime")
		// File-watcher toast: when a buffer is reloaded from disk (autoread
		// kicks in after checktime detects a change), record the path in a
		// global so the next FetchState round-trip can surface a "Reloaded
		// <basename>" toast. Lua side keeps it idempotent — the same path is
		// not re-recorded back-to-back by tracking a monotonically-increasing
		// counter the Go side compares against.
		_ = m.nvim.ExecLua(fileReloadLua)
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
	m.sess.flush() // the last cursor positions, not yet written
	m.crash.close()
	if m.ext != nil && m.ext.proc != nil {
		m.ext.proc.Close() // process extensions and their children
	}
	m.services.shutdown()   // the docker events watcher (services_state.go)
	m.kube.shutdown()       // kubectl loads and port-forwards (kube_state.go)
	m.databasesClose(false) // database sessions are idle unless the view is visible
	m.dropImageTemp()       // an HTTP image preview's temp copy (U16)
	m.stopMCPServe()        // the MCP socket and its clients (mcp_serve_run.go)
	m.stopWebHub()          // the web hub socket and its connections (web_run.go)
	if m.nvim == nil {
		return nil
	}
	return m.nvim.Close()
}
