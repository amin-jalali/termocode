package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// Idle git poll. Each 2 s tick used to start `git status` and redraw,
// even while nobody used termocode for hours: that was most of its idle
// CPU. Now, when nothing happened since the last tick (no key, click,
// focus or screen change), the tick does not come back to the UI: it
// checks a few file times under .git and the work tree root (no process
// started) and waits 2 s more. It comes back, and git runs, when that
// check sees a change (a commit, checkout, stage, fetch or a new file at
// the root), after gitIdleEvery waits (an edit inside a file shows
// within 10 s), or on the first 2 s step after any activity.

// gitIdleEvery: while nobody uses termocode, git is read at least every
// gitIdleEvery-th poll step (10 s).
const gitIdleEvery = 5

// gitIndexRefreshEvery: how often the poll refreshes the index (FIX-40).
const gitIndexRefreshEvery = time.Minute

// gitPollGate is shared by the copies of the Model (a pointer) and the
// waiting tick: the fingerprint taken after the last full git read, and
// whether there was activity since the last tick.
type gitPollGate struct {
	mu     sync.Mutex
	fp     string
	active atomic.Bool
	// refreshedAt is when the poll last refreshed the index.
	refreshedAt time.Time
}

func (g *gitPollGate) get() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.fp
}

func (g *gitPollGate) set(fp string) {
	g.mu.Lock()
	g.fp = fp
	g.mu.Unlock()
}

// afterRead runs after a poll's status: about once a minute it refreshes
// the index (FIX-40: the status takes no lock, so a touched file would be
// read again by every status), then takes the fingerprint the idle wait
// compares with (after the refresh, which changes the index).
func (g *gitPollGate) afterRead(cwd string, ok bool) {
	if ok && g.refreshIndexDue() {
		if err := git.RefreshIndex(cwd); err != nil {
			recordError("git: refresh the index: " + err.Error())
		}
	}
	g.set(gitFingerprint(cwd))
}

// refreshIndexDue reports (and books) an index refresh when the last one
// is gitIndexRefreshEvery ago.
func (g *gitPollGate) refreshIndexDue() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := clock.Now()
	if !g.refreshedAt.IsZero() && now.Sub(g.refreshedAt) < gitIndexRefreshEvery {
		return false
	}
	g.refreshedAt = now
	return true
}

// noteGitActivity marks user activity and screen changes: the next poll
// step reads git.
func (m *Model) noteGitActivity(msg tea.Msg) {
	if m.gitGate == nil {
		return
	}
	switch msg.(type) {
	case tea.KeyMsg, tea.MouseMsg, tea.FocusMsg, tea.WindowSizeMsg, nvim.RedrawMsg:
		m.gitGate.active.Store(true)
	}
}

// gitIdleGated reports whether the next poll waits while idle: there is
// a gate, git was read once and the folder is a repository.
func (m *Model) gitIdleGated() bool {
	if m.gitGate == nil || !m.gitLoaded || !m.gitIsRepo {
		return false
	}
	_, err := m.rootDir()
	return err == nil
}

// gitAutoRefreshTick schedules the next git poll (see above); when not
// gated it is a plain 2 s tick.
func (m *Model) gitAutoRefreshTick() tea.Cmd {
	if !m.gitIdleGated() {
		return gitAutoRefreshTick()
	}
	gate := m.gitGate
	cwd, _ := m.rootDir()
	return func() tea.Msg { return gitIdleWait(gate, cwd, time.Sleep) }
}

// gitIdleWait waits in gitPollInterval steps until the next poll is due.
func gitIdleWait(gate *gitPollGate, cwd string, sleep func(time.Duration)) tea.Msg {
	for i := 1; ; i++ {
		sleep(gitPollInterval)
		if gate.active.Swap(false) || i >= gitIdleEvery {
			return gitAutoRefreshMsg{}
		}
		if fp := gitFingerprint(cwd); fp == "" || fp != gate.get() {
			return gitAutoRefreshMsg{}
		}
	}
}

// gitFingerprint is the size and time of the files a commit,
// checkout, stage, fetch, merge or rebase changes, and of the work tree
// root (a file made or removed there). "" when dir is in no repository.
func gitFingerprint(dir string) string {
	gitDir, top := findGitDir(dir)
	if gitDir == "" {
		return ""
	}
	common := gitDir
	if b, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		if c := strings.TrimSpace(string(b)); filepath.IsAbs(c) {
			common = c
		} else {
			common = filepath.Join(gitDir, c)
		}
	}
	var sb strings.Builder
	for _, p := range []string{
		gitDir, filepath.Join(gitDir, "HEAD"), filepath.Join(gitDir, "index"),
		common, filepath.Join(common, "packed-refs"), filepath.Join(common, "refs", "heads"),
		filepath.Join(common, "refs", "remotes"), filepath.Join(common, "refs", "tags"),
		top, dir,
	} {
		sb.WriteString(statStamp(p))
		sb.WriteByte('|')
	}
	return sb.String()
}

// statStamp is p's size and modification time ("-" when missing).
func statStamp(p string) string {
	fi, err := os.Stat(p)
	if err != nil {
		return "-"
	}
	return strconv.FormatInt(fi.Size(), 10) + ":" + strconv.FormatInt(fi.ModTime().UnixNano(), 10)
}

// findGitDir walks up from dir to the repository: its git directory
// (".git", or where a ".git" file points) and its top folder.
func findGitDir(dir string) (gitDir, top string) {
	for d := dir; ; {
		p := filepath.Join(d, ".git")
		if fi, err := os.Stat(p); err == nil {
			if fi.IsDir() {
				return p, d
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return "", ""
			}
			g, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
			if !ok {
				return "", ""
			}
			g = strings.TrimSpace(g)
			if !filepath.IsAbs(g) {
				g = filepath.Join(d, g)
			}
			return g, d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", ""
		}
		d = parent
	}
}
