package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Web links (GITX-29): copy the remote's link to the cursor line or to a
// commit, to share code with the team.

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-copy-line-link", Title: "Git: Copy Remote Link to Line"},
			{ID: "git-copy-commit-link", Title: "Git: Copy Remote Link to Commit"},
		},
		policies: map[string]aiActionPolicy{
			"git-copy-line-link":   aiConfirm(ai.ClassIDE), // overwrites the system clipboard
			"git-copy-commit-link": aiConfirm(ai.ClassIDE),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-copy-line-link":
				return m.copyLineLink(), true
			case "git-copy-commit-link":
				return m.copyCommitLink(), true
			}
			return nil, false
		},
	})
}

// copyLineLink copies the link of the cursor line of the active file at
// HEAD. A HEAD that is not pushed yet gives a link that does not open,
// so the toast says so.
func (m *Model) copyLineLink() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	rel, ok := m.activeGitRel()
	if !ok {
		return m.gitxInfo("Open a file of this repository first")
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	head, err := git.HeadSHA(cwd)
	if err != nil {
		return m.gitxFail("No commit yet", err)
	}
	line := max(m.cursorLine, 1)
	return m.copyWebLink(cwd, head, func(base string) string { return git.LineLink(base, head, rel, line, line) })
}

// copyCommitLink copies the link of the commit on show in the commit view
// (or HEAD).
func (m *Model) copyCommitLink() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	sha := ""
	if cv := m.commitView; cv != nil && cv.open && cv.compare == nil {
		sha = cv.info.FullHash
	} else if sha, err = git.HeadSHA(cwd); err != nil {
		return m.gitxFail("No commit yet", err)
	}
	return m.copyWebLink(cwd, sha, func(base string) string { return git.CommitLink(base, sha) })
}

func (m *Model) copyWebLink(cwd, sha string, link func(base string) string) tea.Cmd {
	base, err := git.RemoteWeb(cwd)
	if err != nil {
		return m.gitxFail("No remote web page", err)
	}
	l := link(base)
	m.copyToClipboard(l)
	var c tea.Cmd
	if !git.OnRemote(cwd, sha) {
		m.toast, c = m.toast.PushDetail(toast.Warn, "Copied link: the commit is not on the remote yet", l+"\nPush first, or the link does not open.")
		return c
	}
	m.toast, c = m.toast.PushDetail(toast.Info, "Copied link", l)
	return c
}
