package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Push and pull options (GITX-12): Push… shows what goes out first,
// Push to… another remote or branch, Pull… with merge, rebase or from
// another branch, Fetch All Remotes, the incoming and outgoing lists and
// an auto fetch timer (off by default; the git_auto_fetch setting keeps it).

const (
	gitxPreviewRows   = 10 // commits listed on the Push card
	gitxAutoFetchStep = 3 * time.Minute
)

// gitxAuto is the auto fetch timer. gen grows on each toggle, so a tick
// of an old timer stops.
type gitxAuto struct {
	on  bool
	gen int
}

var gitxAutoFetch gitxAuto // one UI per process

func (m *Model) gitxAutoFetchOn() bool { return gitxAutoFetch.on }
func (m *Model) gitxAutoFetchGen() int { return gitxAutoFetch.gen }
func gitxPlural(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %ss", n, one)
}

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-push-preview", Title: "Git: Push… (Show Outgoing Commits First)"},
			{ID: "git-push-to", Title: "Git: Push to…"},
			{ID: "git-pull-options", Title: "Git: Pull…"},
			{ID: "git-pull-rebase", Title: "Git: Pull (Rebase)"},
			{ID: "git-fetch-all", Title: "Git: Fetch All Remotes (Prune)"},
			{ID: "git-incoming", Title: "Git: Show Incoming Commits…"},
			{ID: "git-outgoing", Title: "Git: Show Outgoing Commits…"},
			{ID: "git-auto-fetch", Title: "Git: Toggle Auto Fetch"},
		},
		policies: map[string]aiActionPolicy{
			"git-push-preview": aiNever(aiNoPicker),
			"git-push-to":      aiNever(aiNoPicker),
			"git-pull-options": aiNever(aiNoPicker),
			"git-pull-rebase":  aiConfirm(ai.ClassGit),
			"git-fetch-all":    aiConfirm(ai.ClassGit),
			"git-incoming":     aiNever(aiNoPicker),
			"git-outgoing":     aiNever(aiNoPicker),
			"git-auto-fetch":   aiNever("starts a timer that talks to the remote; only the user turns it on"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-push-preview":
				return m.openPushPreview("", ""), true
			case "git-push-to":
				return m.openRemotePickerFor(" Push to which remote? ", func(m *Model, remote string) tea.Cmd {
					return m.askBranchThen("Push to "+remote, "Branch name on "+remote, pushBranch(m.gitBranch), func(m *Model, b string) tea.Cmd {
						return m.openPushPreview(remote, b)
					})
				}), true
			case "git-pull-options":
				return m.openPullOptions(), true
			case "git-pull-rebase":
				return m.gitxPull("", "", true), true
			case "git-fetch-all":
				return m.gitRemoteOp("Fetching all remotes…", func(cwd string) gitRemoteDoneMsg {
					return gitRemoteDoneMsg{failTitle: "Fetch failed", okText: "Fetched all remotes", err: git.FetchAll(cwd)}
				}), true
			case "git-incoming":
				return m.openCommitList(true), true
			case "git-outgoing":
				return m.openCommitList(false), true
			case "git-auto-fetch":
				return m.toggleAutoFetch(), true
			}
			return nil, false
		},
	})
}

// openRemotePickerFor picks a remote ("remote-<name>"); one remote is
// still shown, so the user sees where the action goes.
func (m *Model) openRemotePickerFor(title string, then func(m *Model, remote string) tea.Cmd) tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	names, err := git.RemoteNames(cwd)
	if err != nil {
		return m.gitxFail("Remotes failed", err)
	}
	if len(names) == 0 {
		return m.gitxInfo("This repository has no remote")
	}
	items := make([]picker.Item, 0, len(names))
	for _, n := range names {
		items = append(items, picker.Item{ID: "remote-" + n, Title: n})
	}
	m.gitxPick(title, items, func(m *Model, id string) tea.Cmd {
		if r, ok := strings.CutPrefix(id, "remote-"); ok {
			return then(m, r)
		}
		return nil
	})
	return nil
}

func (m *Model) askBranchThen(title, label, def string, then func(m *Model, branch string) tea.Cmd) tea.Cmd {
	m.gitxPrompt(title, label, def, func(m *Model, v string) tea.Cmd {
		if v = strings.TrimSpace(v); v != "" {
			return then(m, v)
		}
		return nil
	})
	return nil
}

// openPushPreview lists the commits a push sends on a Cancel-first card.
// remote and branch "" push the current branch the usual way.
func (m *Model) openPushPreview(remote, branch string) tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	cur := pushBranch(m.gitBranch)
	target, base, note := "", "", ""
	if remote != "" {
		target = remote + "/" + branch
		base = target
	} else if up, err := git.Upstream(cwd); err == nil {
		target = up
	} else if cur != "" {
		target, note = "new origin/"+cur, cur+" has no upstream yet; this push makes origin/"+cur+" and tracks it.\n\n"
	} else {
		return m.gitxInfo("Not on a branch: use Push to… to name one")
	}
	out, err := git.Outgoing(cwd, base, gitxPreviewRows+1)
	if err != nil {
		return m.gitxFail("Push failed", err)
	}
	if len(out) == 0 {
		return m.gitxInfo("Nothing to push to " + target)
	}
	files, _ := git.OutgoingFiles(cwd, base)
	var b strings.Builder
	b.WriteString(note)
	for i, e := range out {
		if i == gitxPreviewRows {
			b.WriteString("… and more\n")
			break
		}
		fmt.Fprintf(&b, "%s  %s\n", e.Hash, e.Subject)
	}
	n := gitxPlural(len(out), "commit")
	if len(out) > gitxPreviewRows {
		n = fmt.Sprintf("over %d commits", gitxPreviewRows)
	}
	fmt.Fprintf(&b, "\n%s changed. Cancel pushes nothing.", gitxPlural(files, "file"))
	m.gitxCard("Push "+n+" to "+target+"?", b.String(), func(m *Model, id string) tea.Cmd {
		if id != "push" {
			return nil
		}
		if remote == "" {
			return m.gitPush()
		}
		return m.gitRemoteOp("Pushing to "+target+"…", func(cwd string) gitRemoteDoneMsg {
			return gitRemoteDoneMsg{failTitle: "Push failed", okText: "Pushed to " + target, err: git.PushTo(cwd, remote, branch)}
		})
	}, confirm.Button{ID: "push", Title: "Push"})
	m.confirm = m.confirm.WithWideText()
	return nil
}

func (m *Model) openPullOptions() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	m.gitxPick(" Pull ", []picker.Item{
		{ID: "pull-merge", Title: "Pull (merge)", Hint: "git pull --no-rebase"},
		{ID: "pull-rebase", Title: "Pull (rebase)", Hint: "git pull --rebase: your commits go on top"},
		{ID: "pull-from", Title: "Pull from…", Hint: "another remote or branch"},
	}, func(m *Model, id string) tea.Cmd {
		switch id {
		case "pull-merge":
			return m.gitxPull("", "", false)
		case "pull-rebase":
			return m.gitxPull("", "", true)
		case "pull-from":
			return m.openRemotePickerFor(" Pull from which remote? ", func(m *Model, remote string) tea.Cmd {
				return m.askBranchThen("Pull from "+remote, "Branch on "+remote+" to merge in", "", func(m *Model, b string) tea.Cmd {
					return m.gitxPull(remote, b, false)
				})
			})
		}
		return nil
	})
	return nil
}

func (m *Model) gitxPull(remote, branch string, rebase bool) tea.Cmd {
	what := "origin"
	if remote != "" {
		what = remote + "/" + branch
	}
	how := ""
	if rebase {
		how = " (rebase)"
	}
	return m.gitRemoteOp("Pulling"+how+"…", func(cwd string) gitRemoteDoneMsg {
		return gitRemoteDoneMsg{failTitle: "Pull failed", okText: "Pulled from " + what + how, err: git.PullFrom(cwd, remote, branch, rebase), reload: true}
	})
}

// openCommitList lists the incoming (as of the last fetch) or outgoing
// commits; Enter opens one in the commit view.
func (m *Model) openCommitList(incoming bool) tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	var es []git.LogEntry
	title := " Outgoing commits (a push sends them) "
	if incoming {
		es, err = git.Incoming(cwd, gitxLogLimit)
		title = " Incoming commits (as of the last fetch) "
	} else {
		es, err = git.Outgoing(cwd, "", gitxLogLimit)
	}
	switch {
	case err == git.ErrNoUpstream:
		return m.gitxInfo("This branch has no upstream branch")
	case err != nil:
		return m.gitxFail("Log failed", err)
	case len(es) == 0 && incoming:
		return m.gitxInfo("Nothing to pull (fetch to check again)")
	case len(es) == 0:
		return m.gitxInfo("Nothing to push")
	}
	items := make([]picker.Item, 0, len(es))
	for _, e := range es {
		items = append(items, picker.Item{ID: "commit-" + e.Hash, Title: e.Hash + "  " + e.Subject, Hint: e.Author + ", " + e.When})
	}
	m.gitxPick(title, items, func(m *Model, id string) tea.Cmd {
		return m.gitOpenCommitViewCmd(strings.TrimPrefix(id, "commit-"))
	})
	return nil
}

// toggleAutoFetch turns the auto fetch timer on or off.
func (m *Model) toggleAutoFetch() tea.Cmd {
	gitxAutoFetch.on = !gitxAutoFetch.on
	gitxAutoFetch.gen++
	if err := saveAutoFetch(gitxAutoFetch.on); err != nil { // GITX-12: the git_auto_fetch setting
		return m.gitxFail("Settings error", err)
	}
	if !gitxAutoFetch.on {
		return m.gitxInfo("Auto fetch off")
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, "Auto fetch on", "Every 3 minutes, also after a restart (setting git_auto_fetch)")
	return tea.Batch(c, gitxAutoFetchAfter(gitxAutoFetch.gen))
}

func gitxAutoFetchAfter(gen int) tea.Cmd {
	return tea.Tick(gitxAutoFetchStep, func(time.Time) tea.Msg {
		return gitxDoneMsg{apply: func(m *Model) tea.Cmd { return m.gitxAutoFetchTick(gen) }}
	})
}

// gitxAutoFetchTick fetches in the background (quietly; a failure shows
// once) and plans the next tick. A tick of an older timer stops.
func (m *Model) gitxAutoFetchTick(gen int) tea.Cmd {
	if !gitxAutoFetch.on || gen != gitxAutoFetch.gen {
		return nil
	}
	next := gitxAutoFetchAfter(gen)
	cwd, err := m.rootDir()
	if err != nil || m.gitRemoteBusy != "" || !m.gitIsRepo {
		return next
	}
	return tea.Batch(next, func() tea.Msg {
		err := git.Fetch(cwd)
		return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
			if err != nil {
				gitxAutoFetch.on = false
				gitxAutoFetch.gen++
				return m.gitxFail("Auto fetch stopped", err)
			}
			return fetchGitCmd()
		}}
	})
}
