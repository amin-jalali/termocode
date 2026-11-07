package app

import (
	"errors"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/bgtask"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── WORKTREES view: state and loading (WTX-2) ────────────────────────────
//
// Every worktree of the repository in one sidebar list: the main
// checkout, the user's worktrees (<repo>.worktrees/<name>), agent lanes
// and worktrees made outside termocode. It lives under the Source
// Control icon (activity.ViewWorktrees lights it) and opens with
// Alt+Shift+W or Worktree: Show.
//
// Loading: the list (git worktree list + worktrees.json + the other
// windows' agent roll-ups) is read off the UI thread when the view shows,
// then every wtInterval while it is on screen and the window has the
// focus. Statuses (dirty files, ahead / behind) come from a cache filled
// by 4 workers (worktrees.StatusCache); the view draws from the cache and
// never runs git per frame.

const wtInterval = worktrees.StatusTTL

type wtState struct {
	visible  bool
	off      bool         // a demo: never call git
	demoTabs []wtTabAgent // a demo's terminal tabs
	// sessionsPath is the worktree the sessions list shows; sessionDir
	// is where a new session starts ("" = the workspace; CMP-28).
	sessionsPath, sessionDir string

	loaded  bool
	err     string // why the list could not be read
	root    string // this window's folder ("you are here")
	common  string // the git common dir
	entries []worktrees.Entry
	others  []agentroll.Rollup // the other windows' agents
	cache   *worktrees.StatusCache
	badMeta bool // worktrees.json could not be read (said once)

	cursorPath   string // the card the cursor is on (by path: it stays on it across reloads)
	filter       string
	filterTyping bool
	kind         wtKind // the f filter
	sessLoaded   bool   // the filters were read from the session
	detail       bool   // the compact layout shows the cursor's card
	lastClick    time.Time

	gen     uint64
	polling bool

	form     *wtForm                // Worktree: New… (worktrees_new.go)
	taskAsk  bool                   // a task-start reads the repository for its card (agent_ctl_task.go)
	multi    *wtMultiForm           // Worktree: New in Each Repository… (worktrees_multi.go)
	setupAsk *wtSetupPending        // the open setup trust card (worktrees_setup.go)
	setups   map[string]*wtSetupRun // setups run in this window, by worktree path

	runAsk *wtRunPending     // the open dev server trust card (worktrees_run.go)
	runs   map[string]*wtRun // dev servers run in this window, by worktree path

	link        *wtLinkForm   // the open link card (worktrees_link.go)
	linkReading bool          // due links are being read again
	ctlLink     *wtCtlPending // the open card of a CLI link (worktrees_notes.go)

	sleepAsk   *wtSleepPending   // the open sleep card (worktrees_sleep.go)
	archiveAsk *wtArchivePending // the open archive card (worktrees_archive.go)
	move       *wtMovePending    // the open move, unpack or pull card (worktrees_move.go)
	restackAsk *wtRestackPending // the open restack card (worktrees_stack.go)
	del        *wtDeletePending  // a delete in its steps (worktrees_delete.go)
	cleanup    *wtCleanup        // the open cleanup list (worktrees_cleanup.go)
	preview    *wtPreviewState   // the open agent preview (worktrees_preview.go)

	feedbackAsk     *wtFeedbackPending   // the open PR feedback card (worktrees_followup.go)
	feedbackReading map[string]bool      // PR feedback reads in flight, by path
	followAt        map[string]time.Time // the follow loop's last read, by path
	bgAsk           *wtBgPending         // a background task between prompt and card (worktrees_bgtask.go)
	bgAct           *wtBgActPending      // the open card of one background task
	bgTasks         []bgtask.Task        // the last listed background tasks
	bgEngine        containers.Engine    // the engine they were listed on
	handoff         *wtHandoffForm       // the open handoff card (worktrees_handoff.go)

	autoAt   time.Time // the last auto clean-up check (worktrees_autoclean.go)
	autoBusy bool      // an auto clean-up runs

	seq    uint64
	render struct {
		sync.Mutex
		key, out string
	}
}

func newWTState() *wtState {
	return &wtState{cache: worktrees.NewStatusCache(git.GetWorktreeStatus)}
}

func (st *wtState) touch() { st.seq++ }

// worktreesVisible: the WORKTREES view is on screen.
func (m *Model) worktreesVisible() bool {
	return m.showExp && !m.zenMode && m.activity.Active() == activity.ViewWorktrees
}

// revealWorktrees shows the view (Worktree: Show, Alt+Shift+W) and
// focuses it.
func (m *Model) revealWorktrees() tea.Cmd {
	m.activity.SetActive(activity.ViewWorktrees)
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	m.focus = FocusExplorer
	if m.inTerminal || m.panelFocused { // run from the terminal: the keys come to the list
		m.inTerminal, m.panelFocused = false, false
		m.ensureEditorWindowCurrent()
	}
	return m.worktreesSync()
}

// worktreesSync follows the view's visibility and the window focus:
// shown → load now and every wtInterval; hidden → stop. Called after
// every update; cheap when nothing changed.
func (m *Model) worktreesSync() tea.Cmd {
	st := m.wt
	if st == nil {
		return nil
	}
	if vis := m.worktreesVisible(); vis != st.visible {
		st.visible = vis
		st.touch()
		if vis {
			m.worktreesLoadSession()
		}
	}
	want := st.visible && !m.termBlurred && !st.off
	switch {
	case want && !st.polling:
		st.polling = true
		st.gen++
		return m.worktreesLoad()
	case !want && st.polling:
		st.polling = false
		st.gen++
	}
	return nil
}

type wtLoadMsg struct {
	gen     uint64
	jump    bool // Worktree: Jump… asked: apply it and open the picker
	web     bool // the page asked (web_worktrees.go): apply it, no poll tick
	board   bool // AI: Agent Board… asked (agent_board.go): apply it and open the board
	root    string
	common  string
	list    []git.Worktree
	metas   map[string]worktrees.Meta
	metaErr error
	others  []agentroll.Rollup
	err     error
}

type wtStatusMsg struct{ gen uint64 }

type wtTickMsg struct{ gen uint64 }

// worktreesLoad reads the list in the background.
func (m *Model) worktreesLoad() tea.Cmd { return m.worktreesLoadFor(false) }

func (m *Model) worktreesLoadFor(jump bool) tea.Cmd {
	gen := m.wt.gen
	root, _ := m.rootDir()
	return func() tea.Msg {
		msg := wtLoadMsg{gen: gen, root: root, jump: jump}
		list, common, err := git.ListWorktrees(root)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.list, msg.common = list, common
		msg.metas, msg.metaErr = worktrees.Load(common)
		msg.others = agentRollupOthers()
		return msg
	}
}

func (m *Model) onWTLoad(msg wtLoadMsg) tea.Cmd {
	st := m.wt
	if st == nil || (!msg.jump && !msg.web && !msg.board && (msg.gen != st.gen || !st.polling)) {
		return nil
	}
	if msg.web && m.web != nil {
		m.web.wt.loading = false
	}
	st.loaded = true
	st.root = msg.root
	if real, err := filepath.EvalSymlinks(msg.root); err == nil {
		st.root = real
	}
	var cmds []tea.Cmd
	if msg.err != nil {
		st.err = msg.err.Error()
		st.entries = nil
	} else {
		st.err = ""
		st.common = msg.common
		st.entries = worktrees.Build(msg.list, msg.metas)
		st.others = msg.others
		if errors.Is(msg.metaErr, worktrees.ErrBadMeta) && !st.badMeta {
			st.badMeta = true
			cmds = append(cmds, m.pushToastDetail(toast.Warn, "Worktree notes could not be read", "termocode/worktrees.json in the git folder is kept as it is; pins and names are off until it is fixed."))
		}
		cmds = append(cmds, st.refreshStatus(), st.wtLinksRefresh(clock.Now()), m.wtFollowRefresh(clock.Now()))
		cmds = append(cmds, m.wtAutoClean()) // CMP-16: when it is on and due
	}
	st.touch()
	switch {
	case msg.jump:
		cmds = append(cmds, m.openWorktreeJump()) // the poll chain (if any) keeps its own tick
	case msg.board:
		cmds = append(cmds, m.openAgentBoard()) // the poll chain (if any) keeps its own tick
	case msg.web: // the page's read: the poll chain (if any) keeps its own tick
	default:
		cmds = append(cmds, wtTick(st.gen))
	}
	return tea.Batch(cmds...)
}

// refreshStatus starts the status reads that are due; the returned cmd
// waits for them and asks for a redraw.
func (st *wtState) refreshStatus() tea.Cmd {
	var paths []string
	for _, e := range st.entries {
		if !e.Gone {
			paths = append(paths, e.Path)
		}
	}
	done := make(chan struct{})
	if !st.cache.Refresh(paths, func() { close(done) }) {
		return nil
	}
	gen := st.gen
	return func() tea.Msg {
		<-done
		return wtStatusMsg{gen: gen}
	}
}

func wtTick(gen uint64) tea.Cmd {
	return tea.Tick(wtInterval, func(time.Time) tea.Msg { return wtTickMsg{gen: gen} })
}

// handleWTMsg routes the WORKTREES messages. ok=false: not ours.
func (m *Model) handleWTMsg(msg tea.Msg) (tea.Cmd, bool) {
	st := m.wt
	if c, ok := m.handleWTCtlMsg(msg); ok { // the control socket (worktrees_notes.go)
		return c, true
	}
	if c, ok := m.handleWTDiffMsg(msg); ok { // the diff vs base (worktrees_diff.go)
		return c, true
	}
	if c, ok := m.handleWTHandoffMsg(msg); ok { // the handoff card (worktrees_handoff.go)
		return c, true
	}
	if c, ok := m.handleWTDeleteMsg(msg); ok { // the type-the-name card (worktrees_delete.go)
		return c, true
	}
	if c, ok := m.handleWTLinkMsg(msg); ok { // the link card (worktrees_link.go)
		return c, true
	}
	if c, ok := m.handleWTMultiMsg(msg); ok { // the multi-repo card (worktrees_multi.go)
		return c, true
	}
	if c, ok := m.handleWTFormMsg(msg); ok { // the dialog's buttons (worktrees_new.go)
		return c, true
	}
	switch msg := msg.(type) {
	case wtFeedbackMsg: // worktrees_followup.go
		return m.onWTFeedback(msg), true
	case wtNewPrepMsg:
		return m.onWTNewPrep(msg), true
	case wtCreatedMsg:
		return m.onWTCreated(msg), true
	case wtWebTaskMsg: // a new task from the page (web_worktrees_task.go)
		return m.onWTWebTask(msg), true
	case wtForgotMsg:
		return m.onWTForgot(msg), true
	case wtArchivePrepMsg:
		return m.onWTArchivePrep(msg), true
	case wtArchivedMsg:
		return m.onWTArchived(msg), true
	case wtRestackedMsg:
		return m.onWTRestacked(msg), true
	case wtCleanupPrepMsg:
		return m.onWTCleanupPrep(msg), true
	case wtCleanedMsg:
		return m.onWTCleaned(msg), true
	case wtAutoCleanedMsg:
		return m.onWTAutoCleaned(msg), true
	case wtDeletePrepMsg:
		return m.onWTDeletePrep(msg), true
	case wtDeletedMsg:
		return m.onWTDeleted(msg), true
	case wtSetupDoneMsg:
		return m.onWTSetupDone(msg), true
	case wtLoadMsg:
		return m.onWTLoad(msg), true
	case wtStatusMsg:
		if st != nil && msg.gen == st.gen {
			st.touch()
		}
		return nil, true
	case wtTickMsg:
		if st == nil || msg.gen != st.gen || !st.polling {
			return nil, true
		}
		return m.worktreesLoad(), true
	}
	return nil, false
}

// worktreesReload is R: read the list and every status now.
func (m *Model) worktreesReload() tea.Cmd {
	st := m.wt
	if st == nil || st.off {
		return nil
	}
	for _, e := range st.entries {
		st.cache.Invalidate(e.Path)
	}
	st.polling = false
	return m.worktreesSync()
}
