package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// GITX-39 (J13): move the last commit to another branch (a commit made
// on the wrong branch). It picks a new or an existing branch and asks on a
// Cancel-first card. A commit that is pushed already is only copied, so
// the history that others have stays as it is.

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-move-commit", Title: "Git: Move Last Commit to Another Branch…", Hint: "a commit made on the wrong branch; a pushed one is copied"},
		},
		policies: map[string]aiActionPolicy{
			"git-move-commit": aiNever("moves the user's commits between branches; only the user does that"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			if id != "git-move-commit" {
				return nil, false
			}
			return m.gitxMoveCommit(), true
		},
	})
}

const gitxMoveToPrefix = "move-to:"

// gitxMoveCommit opens the branch picker: New Branch… first, then the
// other local branches.
func (m *Model) gitxMoveCommit() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	in, err := git.LastCommitForMove(cwd)
	if err != nil {
		return m.gitxInfo(gitxSentence(err.Error()))
	}
	items := []picker.Item{{ID: "move-new", Title: "New Branch…", Hint: "a new branch that starts at this commit"}}
	branches, _ := git.Branches(cwd)
	for _, b := range branches {
		if b != in.Branch {
			items = append(items, picker.Item{ID: gitxMoveToPrefix + b, Title: b})
		}
	}
	verb := "Move"
	if in.Pushed {
		verb = "Copy"
	}
	m.gitxPick(" "+verb+" "+in.Short+" "+in.Subject+" to… ", items, func(m *Model, id string) tea.Cmd {
		if id == "move-new" {
			m.gitxPrompt(verb+" Commit to a New Branch", "New branch name", "", func(m *Model, name string) tea.Cmd {
				name = strings.TrimSpace(name)
				if name == "" {
					return nil
				}
				m.gitxMoveCard(cwd, in, name, true)
				return nil
			})
			return nil
		}
		if b, ok := strings.CutPrefix(id, gitxMoveToPrefix); ok {
			m.gitxMoveCard(cwd, in, b, false)
		}
		return nil
	})
	return nil
}

// gitxMoveCard asks before the move; a pushed commit gets a Copy card.
func (m *Model) gitxMoveCard(cwd string, in git.MoveCommitInfo, target string, create bool) {
	where := "on top of " + target
	if create {
		where = "to the new branch " + target
	}
	title := "Move commit " + in.Short + " to " + target + "?"
	text := in.Subject + "\n\nThe commit goes " + where + " and leaves " + in.Branch + ". Your uncommitted changes stay. Nothing is checked out."
	btn := confirm.Button{ID: "move", Title: "Move Commit"}
	if in.Pushed {
		title = "Copy commit " + in.Short + " to " + target + "?"
		text = in.Subject + "\n\nThis commit is pushed already, so it stays on " + in.Branch + ": taking it away would rewrite history that others may have. A copy goes " + where + ". To undo it on " + in.Branch + ", open the commit and use Revert."
		btn = confirm.Button{ID: "copy", Title: "Copy Commit"}
	}
	m.gitxCard(title, text, func(m *Model, id string) tea.Cmd {
		if id != btn.ID {
			return nil
		}
		if err := git.MoveLastCommit(cwd, in.Hash, target, create, in.Pushed); err != nil {
			return tea.Batch(fetchGitCmd(), m.gitxFail("Commit not moved", err))
		}
		done, detail := "Moved "+in.Short+" to "+target, in.Branch+" no longer has it"
		if in.Pushed {
			done, detail = "Copied "+in.Short+" to "+target, "it stays on "+in.Branch+" too"
		}
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, done, detail)
		return tea.Batch(fetchGitCmd(), c)
	}, btn)
}

// gitxSentence starts an error text with a capital letter for a note.
func gitxSentence(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
