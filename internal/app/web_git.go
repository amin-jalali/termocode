package app

import (
	"reflect"
	"time"
	"unsafe"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The git topic (WEB-23, web design 2.6): the laptop's Source Control
// state for the page's Git tab. It is built from what the laptop's own
// git refresh stored on the Model (the 2 s poll, and the refresh after
// every git action), so it never runs git and never polls on its own: an
// Update without a new git state costs one comparison. The delta is the
// whole snapshot, at most one per webGitEvery; a queued one is replaced
// by the next.

const webGitEvery = time.Second

func init() {
	webTopics["git"] = webTopicDef{snapshot: webGitSnapshot, apply: webApplyReplace[webproto.GitSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishGit)
}

// webGitDelta is a git delta: the whole snapshot; a newer one replaces a
// queued one.
type webGitDelta webproto.GitSnap

func (webGitDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webGitDelta)
	return n, ok
}

// webGitPub is what the git topic sent last (Update only).
type webGitPub struct {
	key  webGitKey
	last *webproto.GitSnap // what the browsers have; nil: nobody listens
	next *webproto.GitSnap // a newer state, not sent yet
	at   time.Time         // when the last delta went out
	tick bool              // a one-shot tick is on its way
}

// webGitKey identifies a git state cheaply: the laptop replaces its file
// and commit slices as a whole when they change (and keeps the old slice
// when a poll found the same list), so their addresses say "new".
type webGitKey struct {
	loaded, repo     bool
	files, commits   unsafe.Pointer
	nfiles, ncommits int
	branch           git.Branch
	rebase           bool
}

// webGitTickMsg: the rate limit's wait is over.
type webGitTickMsg struct{}

func (m *Model) webGitKey() webGitKey {
	return webGitKey{loaded: m.gitLoaded, repo: m.gitIsRepo,
		files: unsafe.Pointer(unsafe.SliceData(m.gitFiles)), nfiles: len(m.gitFiles),
		commits: unsafe.Pointer(unsafe.SliceData(m.gitCommits)), ncommits: len(m.gitCommits),
		branch: m.gitBranch, rebase: m.gitRebase.InProgress}
}

func webGitSnapshot(m *Model, _ string) (any, error) {
	snap := m.webGitBuild()
	if m.web != nil {
		p := &m.web.git
		p.key, p.last, p.next = m.webGitKey(), &snap, nil // the deltas continue from this snap
	}
	return snap, nil
}

// webPublishGit sends the snapshot when the laptop's git state changed,
// at most once per webGitEvery.
func (m *Model) webPublishGit(msg tea.Msg) tea.Cmd {
	h := m.web
	p := &h.git
	if _, ok := msg.(webGitTickMsg); ok {
		p.tick = false
	}
	if !h.webListens("git") {
		p.last, p.next = nil, nil
		return nil
	}
	if key := m.webGitKey(); p.last == nil || key != p.key {
		// New slices from a refresh: build, and keep it only if the
		// content moved (a re-read graph is often the same).
		p.key = key
		cur := m.webGitBuild()
		p.next = &cur
		if p.last != nil && reflect.DeepEqual(*p.last, cur) {
			p.next = nil
		}
	}
	if p.next == nil {
		return nil
	}
	now := webNow()
	if wait := webGitEvery - now.Sub(p.at); wait > 0 {
		if p.tick {
			return nil
		}
		p.tick = true
		return tea.Tick(wait, func(time.Time) tea.Msg { return webGitTickMsg{} })
	}
	p.at, p.last, p.next = now, p.next, nil
	h.publish("git", webGitDelta(*p.last))
	return nil
}

// webGitBuild makes the snapshot from the Model's git state.
func (m *Model) webGitBuild() webproto.GitSnap {
	s := webproto.GitSnap{Loading: !m.gitLoaded, Repo: m.gitLoaded && m.gitIsRepo,
		Files: []webproto.GitFile{}, Log: []webproto.GitCommit{}}
	if !s.Repo {
		return s
	}
	s.Branch = webproto.GitBranch{Name: webName(m.gitBranch.Name), Ahead: m.gitBranch.Ahead, Behind: m.gitBranch.Behind}
	conflict := false
	for i, f := range m.gitFiles {
		if i == webproto.MaxGitFiles {
			s.More = len(m.gitFiles) - i
			break
		}
		wf := webGitFile(f)
		conflict = conflict || wf.Conflict
		s.Files = append(s.Files, wf)
	}
	switch {
	case m.gitRebase.InProgress:
		s.Op = "rebase"
	case conflict:
		s.Op = "merge"
	}
	for i, c := range m.gitCommits {
		if i == webproto.MaxGitLog {
			break
		}
		s.Log = append(s.Log, webproto.GitCommit{Hash: c.Hash, Subject: webName(c.Subject), Author: webName(c.Author),
			When: c.Committed * 1000, Unpushed: c.Unpushed})
	}
	return s
}

// webGitFile is one git status entry for the page. The path is sent as
// git has it (the ops take it back); the page shows it isolated.
func webGitFile(f git.FileStatus) webproto.GitFile {
	x, y := " ", " "
	if len(f.Code) == 2 {
		x, y = f.Code[:1], f.Code[1:]
	}
	return webproto.GitFile{Path: f.Path, X: x, Y: y, Staged: f.Staged(), Unstaged: f.Unstaged(),
		Untracked: f.Untracked(), Conflict: webGitConflict(f.Code)}
}

// webGitConflict: an unmerged entry (git status' DD, AU, UD, UA, DU, AA, UU).
func webGitConflict(code string) bool {
	switch code {
	case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
		return true
	}
	return false
}
