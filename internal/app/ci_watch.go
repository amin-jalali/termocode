package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/statusbar"
	"github.com/amin-jalali/termocode/internal/toast"
)

// G-CI: termocode watches the GitHub CI of the current branch through
// the gh CLI (internal/ghci) and shows it as a status-bar chip. It asks
// gh right away when the branch changes or a push is seen (ahead drops
// to 0), every ghci.FastPoll while a run is pending or just after a
// push, and every ghci.SlowPoll otherwise. While the terminal window is
// in the background no gh runs.

// ciGHTimeout caps one gh call.
const ciGHTimeout = 15 * time.Second

// ciBlurredRetry is when a tick that came while the window was in the
// background tries again.
const ciBlurredRetry = 30 * time.Second

// ciWatch is the CI state of the current branch.
type ciWatch struct {
	run      ghci.Runner // nil: gh on PATH (never in headless runs)
	branch   string      // the branch snap is for
	snap     ghci.Snapshot
	err      error // the last fetch error (ghci.Quiet: no gh / login / GitHub)
	loaded   bool  // snap / err hold an answer for branch
	gen      int   // tick generation: a newer trigger ends older chains
	busy     bool  // a fetch runs
	again    bool  // a trigger came while busy: fetch once more after it
	pushAt   time.Time
	notified map[string]bool // branch@sha that already had the red toast
}

// ciResultMsg is a finished fetch.
type ciResultMsg struct {
	gen    int
	branch string
	snap   ghci.Snapshot
	err    error
}

// ciFixClickMsg is a click on the red-CI toast: Fix with AI.
type ciFixClickMsg struct{}

// ciTickMsg is a poll timer.
type ciTickMsg struct{ gen int }

// ciW returns the watcher, creating it on first use.
func (m *Model) ciW() *ciWatch {
	if m.ci == nil {
		m.ci = &ciWatch{}
	}
	return m.ci
}

// ciNoGH is the runner of headless runs (tests): gh is never started.
func ciNoGH(context.Context, string, ...string) (string, string, error) {
	return "", "", ghci.ErrNoGH
}

func (w *ciWatch) runner() ghci.Runner {
	switch {
	case w.run != nil:
		return w.run
	case headless():
		return ciNoGH
	}
	return ghci.DefaultRunner(ciGHTimeout)
}

// ciOnGitSnapshot follows the git poll: a new branch or a push (ahead
// went to 0 on the same branch) asks gh now.
func (m *Model) ciOnGitSnapshot(prev git.Branch) tea.Cmd {
	w := m.ciW()
	cur := m.gitBranch
	if !m.gitIsRepo || cur.Name == "" {
		if w.branch != "" {
			*w = ciWatch{run: w.run, notified: w.notified, gen: w.gen + 1}
		}
		return nil
	}
	switch {
	case cur.Name != w.branch:
		w.branch, w.snap, w.err, w.loaded = cur.Name, ghci.Snapshot{}, nil, false
		w.pushAt = time.Time{}
		return m.ciTrigger()
	case prev.Name == cur.Name && prev.Ahead > 0 && cur.Ahead == 0:
		w.pushAt = clock.Now()
		return m.ciTrigger()
	}
	return nil
}

// ciTrigger fetches now and starts a new tick chain.
func (m *Model) ciTrigger() tea.Cmd {
	w := m.ciW()
	w.gen++
	if w.busy {
		w.again = true
		return nil
	}
	return m.ciFetchCmd()
}

// ciFetchCmd runs one fetch off the main loop.
func (m *Model) ciFetchCmd() tea.Cmd {
	w := m.ciW()
	dir, err := m.rootDir()
	if err != nil || w.branch == "" {
		return nil
	}
	w.busy = true
	gen, branch, run := w.gen, w.branch, w.runner()
	return func() tea.Msg {
		snap, err := ghci.Fetch(context.Background(), run, dir, branch)
		return ciResultMsg{gen: gen, branch: branch, snap: snap, err: err}
	}
}

// handleCIMsg handles the watcher's messages (any overlay may be open).
func (m *Model) handleCIMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case ciResultMsg:
		return m.onCIResult(msg), true
	case ciFixClickMsg:
		return m.ciFixWithAI(), true
	case ciLogMsg: // ci_card.go
		return m.onCILog(msg), true
	case ciTickMsg:
		w := m.ciW()
		if msg.gen != w.gen || w.busy {
			return nil, true
		}
		if m.termBlurred {
			return ciTick(w.gen, ciBlurredRetry), true
		}
		return m.ciFetchCmd(), true
	}
	return nil, false
}

func ciTick(gen int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return ciTickMsg{gen: gen} })
}

// onCIResult stores a fetch and schedules the next one.
func (m *Model) onCIResult(msg ciResultMsg) tea.Cmd {
	w := m.ciW()
	w.busy = false
	if w.again {
		w.again = false
		return m.ciFetchCmd()
	}
	if msg.branch != w.branch {
		return m.ciFetchCmd() // the branch changed while gh ran
	}
	var toastCmd tea.Cmd
	if msg.err == nil {
		toastCmd = m.ciRedToast(msg.snap)
		w.snap = msg.snap
	} else if ghci.Quiet(msg.err) {
		w.snap = ghci.Snapshot{Branch: msg.branch} // keep nothing: CI is not available
	}
	w.err, w.loaded = msg.err, true
	if m.ciCardOpen() {
		m.showCICard() // the card shows the fresh answer
	}
	if msg.gen != w.gen {
		return toastCmd // a newer chain runs
	}
	since := time.Duration(-1)
	if !w.pushAt.IsZero() {
		since = clock.Now().Sub(w.pushAt)
	}
	return tea.Batch(toastCmd, ciTick(w.gen, ghci.NextPoll(w.snap, msg.err, since)))
}

// ciRedToast says once per branch and commit that CI turned red, with
// the key that sends the log to the AI. A branch that is already red
// when termocode first looks gets only the chip.
func (m *Model) ciRedToast(next ghci.Snapshot) tea.Cmd {
	w := m.ciW()
	if next.State() != ghci.StateFail {
		return nil
	}
	if w.notified == nil {
		w.notified = map[string]bool{}
	}
	key := next.Branch + "@" + next.HeadSHA
	if w.notified[key] {
		return nil
	}
	w.notified[key] = true
	if !w.loaded || (w.snap.State() == ghci.StateFail && w.snap.HeadSHA == next.HeadSHA) {
		return nil
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushClick(toast.Errr, "CI failed on "+next.Branch, m.ciFixKeyLabel()+" Fix with AI", ciFixClickMsg{})
	return c
}

// ciFixKeyLabel is the key of Fix with AI ("Alt+C" unless rebound).
func (m Model) ciFixKeyLabel() string {
	if k := overflowMenuShortcuts(m.keys)[keymap.ActionGitCIFix]; k != "" {
		return k
	}
	return "Alt+C"
}

// fillCIStatus sets the status-bar CI chip.
func (m Model) fillCIStatus(st *statusbar.State) {
	w := m.ci
	if w == nil || w.branch == "" || w.branch != m.gitBranch.Name {
		return
	}
	switch w.snap.State() {
	case ghci.StatePending:
		st.CI = statusbar.CIPending
	case ghci.StatePass:
		st.CI = statusbar.CIPass
	case ghci.StateFail:
		st.CI, st.CIFailed = statusbar.CIFail, len(w.snap.FailedRuns())
	}
}
