package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Commit signing (GITX-38): the commit view shows a signed commit's badge
// (git_commit_view.go), this switch turns signing on or off for the
// repository, and a commit that cannot be signed says how to fix it
// (internal/git errors.go).

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{{ID: "git-signing-toggle", Title: "Git: Toggle Commit Signing (This Repository)"}},
		policies: map[string]aiActionPolicy{
			"git-signing-toggle": aiNever("changes how the user's commits are signed; only the user does that"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			if id != "git-signing-toggle" {
				return nil, false
			}
			return m.toggleSigning(), true
		},
	})
}

func (m *Model) toggleSigning() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	on := !git.Signing(cwd)
	if err := git.SetSigning(cwd, on); err != nil {
		return m.gitxFail("Signing not changed", err)
	}
	var c tea.Cmd
	if on {
		m.toast, c = m.toast.PushDetail(toast.Info, "Commit signing on", "This repository signs new commits (commit.gpgsign). git uses user.signingkey and gpg.format (openpgp or ssh).")
	} else {
		m.toast, c = m.toast.PushDetail(toast.Info, "Commit signing off", "This repository does not sign new commits.")
	}
	return c
}
