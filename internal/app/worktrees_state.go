package app

import (
	"errors"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/clock"
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
	setupAsk *wtSetupPending        // the open setup trust card (worktrees_setup.go)
	setups   map[string]*wtSetupRun // setups run in this window, by worktree path

	link        *wtLinkForm   // the open link card (worktrees_link.go)
	linkReading bool          // due links are being read again
	ctlLink     *wtCtlPending // the open card of a CLI link (worktrees_notes.go)

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
	if st == nil || (!msg.jump && (msg.gen != st.gen || !st.polling)) {
		return nil
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
		cmds = append(cmds, st.refreshStatus(), st.wtLinksRefresh(clock.Now()))
	}
	st.touch()
	if msg.jump {
		cmds = append(cmds, m.openWorktreeJump()) // the poll chain (if any) keeps its own tick
	} else {
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
	if c, ok := m.handleWTLinkMsg(msg); ok { // the link card (worktrees_link.go)
		return c, true
	}
	if c, ok := m.handleWTFormMsg(msg); ok { // the dialog's buttons (worktrees_new.go)
		return c, true
	}
	switch msg := msg.(type) {
	case wtNewPrepMsg:
		return m.onWTNewPrep(msg), true
	case wtCreatedMsg:
		return m.onWTCreated(msg), true
	case wtForgotMsg:
		return m.onWTForgot(msg), true
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
