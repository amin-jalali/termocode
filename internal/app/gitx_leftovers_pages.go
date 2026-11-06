package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
)

// GITX-39: the read-only git pages of the small leftovers: the
// contributors (E28).

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-contributors", Title: "Git: Show Contributors", Hint: "the authors of this branch and their commit counts"},
		},
		policies: map[string]aiActionPolicy{
			"git-contributors": aiAuto(),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-contributors":
				return m.gitxOpenContributors(), true
			}
			return nil, false
		},
	})
}

// gitxOpenPage opens a read-only text page.
func (m *Model) gitxOpenPage(name, text string) tea.Cmd {
	m.ensureEditorWindowCurrent()
	if err := m.openReadOnlyPage(name, text, "", "log"); err != nil {
		return m.gitxFail("Could not open the page", err)
	}
	return nil
}

func (m *Model) gitxOpenContributors() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	list, err := git.Contributors(cwd)
	if err != nil {
		return m.gitxFail("Could not read the contributors", err)
	}
	return m.gitxOpenPage("Contributors", gitxContributorsPage(list))
}

// gitxContributorsPage is the contributors as text: count, share, author.
func gitxContributorsPage(list []git.Contributor) string {
	total := 0
	for _, c := range list {
		total += c.Commits
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s, %s on this branch (merge commits not counted).\n\n", gitxPlural(len(list), "contributor"), gitxPlural(total, "commit"))
	w := len(fmt.Sprint(total))
	for _, c := range list {
		who := c.Name
		if c.Email != "" {
			who += " <" + c.Email + ">"
		}
		fmt.Fprintf(&b, "%*d  %d%%  %s\n", w, c.Commits, c.Commits*100/max(total, 1), who)
	}
	return b.String()
}
