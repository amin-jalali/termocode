package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Clone repository (dashboard card + palette "Git: Clone Repository…").
//
// Flow: URL prompt → target-folder prompt (pre-filled) → background
// `git clone --progress` with a live, in-place progress toast → re-exec into
// the new folder (same path as Open Recent Workspace). When an HTTPS clone
// fails for lack of credentials, a masked token prompt appears and the clone
// is retried once with the token handed to git through GIT_ASKPASS (see
// git.Clone). The token lives only in that retry's child environment.

// cloneToastKey identifies the single in-place progress toast.
const cloneToastKey = "git-clone"

// cloneProgressMsg is one progress update from the clone goroutine.
type cloneProgressMsg struct {
	p  git.CloneProgress
	ch <-chan tea.Msg
}

// cloneDoneMsg ends a clone attempt.
type cloneDoneMsg struct {
	err       error
	url, dest string
	withToken bool
}

// cloneState is the in-flight clone request (between prompts and attempts).
type cloneState struct {
	url     string
	dest    string
	running bool
}

// openCloneURLPrompt starts the flow.
func (m *Model) openCloneURLPrompt() tea.Cmd {
	if m.clone.running {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.Push(toast.Info, "A clone is already running")
		return cmd
	}
	m.prompt = prompt.New("Clone Repository", "Repository URL (https:// or git@…):", m.clone.url)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindCloneURL
	return nil
}

// handleClonePromptSubmit advances the flow for the three clone prompts.
func (m *Model) handleClonePromptSubmit(value string) tea.Cmd {
	switch m.promptKind {
	case promptKindCloneURL:
		name := git.RepoNameFromURL(value)
		if name == "" {
			var cmd tea.Cmd
			m.toast, cmd = m.toast.Push(toast.Warn, "That does not look like a repository URL")
			return cmd
		}
		m.clone.url = value
		cwd, _ := os.Getwd()
		def := git.DefaultCloneDest(cwd, m.gitIsRepo, name)
		m.prompt = prompt.New("Clone Repository", "Clone into folder:", def)
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindCloneDest
		return nil
	case promptKindCloneDest:
		dest := git.ExpandHome(value)
		if !filepath.IsAbs(dest) {
			if cwd, err := os.Getwd(); err == nil {
				dest = filepath.Join(cwd, dest)
			}
		}
		if err := git.CheckCloneDest(dest); err != nil {
			var cmd tea.Cmd
			m.toast, cmd = m.toast.PushDetail(toast.Errr, "Cannot clone there", err.Error())
			return cmd
		}
		m.clone.dest = dest
		return m.startClone("", "")
	case promptKindCloneToken:
		user, token := git.SplitUserToken(value)
		return m.startClone(user, token)
	}
	return nil
}

// handleClonePromptClose reacts to Esc on a clone prompt.
func (m *Model) handleClonePromptClose() tea.Cmd {
	switch m.promptKind {
	case promptKindCloneURL, promptKindCloneDest, promptKindCloneToken:
		var cmd tea.Cmd
		m.toast, cmd = m.toast.Push(toast.Info, "Clone cancelled")
		return cmd
	}
	return nil
}

// openCloneTokenPrompt asks for an access token after an auth failure.
func (m *Model) openCloneTokenPrompt() {
	host := git.HostFromURL(m.clone.url)
	if host == "" {
		host = "this host"
	}
	m.prompt = prompt.New("Sign in to "+host, "Access token (or user:token):", "")
	m.prompt.SetMasked(true)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindCloneToken
}

// startClone launches the background clone and returns the Cmd that waits
// for its first message.
func (m *Model) startClone(user, token string) tea.Cmd {
	m.clone.running = true
	url, dest := m.clone.url, m.clone.dest
	ch := make(chan tea.Msg, 8)
	go func() {
		var last time.Time
		err := git.Clone(git.CloneOptions{
			URL: url, Dest: dest, Username: user, Token: token,
			OnProgress: func(p git.CloneProgress) {
				// Throttle: the toast doesn't need more than ~5 updates/s.
				if now := time.Now(); now.Sub(last) >= 200*time.Millisecond || p.Percent == 100 {
					last = now
					select {
					case ch <- cloneProgressMsg{p: p, ch: ch}:
					default: // UI busy — drop this frame
					}
				}
			},
		})
		ch <- cloneDoneMsg{err: err, url: url, dest: dest, withToken: token != ""}
		close(ch)
	}()
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.PushKeyed(cloneToastKey, toast.Info, "Cloning "+git.RepoNameFromURL(url)+"…", dest)
	return tea.Batch(toastCmd, waitCloneMsg(ch))
}

// waitCloneMsg blocks (off the UI thread) for the next clone message.
func waitCloneMsg(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// cloneProgressText renders a progress update for the toast detail.
func cloneProgressText(p git.CloneProgress) string {
	if p.Percent < 0 {
		return p.Phase + "…"
	}
	return fmt.Sprintf("%s %d%%", p.Phase, p.Percent)
}

func (m *Model) handleCloneProgress(msg cloneProgressMsg) tea.Cmd {
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.PushKeyed(cloneToastKey, toast.Info,
		"Cloning "+git.RepoNameFromURL(m.clone.url)+"…", cloneProgressText(msg.p))
	return tea.Batch(toastCmd, waitCloneMsg(msg.ch))
}

func (m *Model) handleCloneDone(msg cloneDoneMsg) tea.Cmd {
	m.clone.running = false
	m.toast = m.toast.Dismiss(cloneToastKey)
	var toastCmd tea.Cmd
	if msg.err != nil {
		if errors.Is(msg.err, git.ErrCloneAuth) && git.IsHTTPURL(msg.url) && !msg.withToken {
			m.openCloneTokenPrompt()
			return nil
		}
		title := "Clone failed"
		if errors.Is(msg.err, git.ErrCloneAuth) {
			title = "Clone failed: authentication"
		}
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, title, msg.err.Error())
		return toastCmd
	}
	m.clone = cloneState{}
	// Same re-exec path as Open Recent Workspace.
	return m.openWorkspace(msg.dest)
}

// welcomeCardClone is the dashboard card id for "Clone repository".
const welcomeCardClone = "clone"

// runWelcomeCard runs dashboard card i: cards with an id are handled here,
// the rest go through their keymap action.
func (m Model) runWelcomeCard(i int) (tea.Model, tea.Cmd) {
	actions := welcomeQuickActions()
	if i < 0 || i >= len(actions) {
		return m, nil
	}
	if actions[i].id == welcomeCardClone {
		cmd := m.openCloneURLPrompt()
		return m, cmd
	}
	return m.dispatchWelcomeAction(actions[i].action)
}
