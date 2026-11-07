package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
)

// GITX-39: the AI git leftovers. Both only fill the chat box: the user
// reads the text and sends it with Enter, and the AI answers in the chat
// (it runs no git command from here).
//   - Write Changelog with AI… (Q4): the commit subjects since a tag.
//   - Plan Commits for My Changes with AI (Q8, J14): the uncommitted
//     changes, with a request to split them into small commits.

const gitxChangelogMax = 300

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-ai-changelog", Title: "Git: Write Changelog with AI…", Hint: "from the commits since a tag"},
			{ID: "git-ai-compose", Title: "Git: Plan Commits for My Changes with AI", Hint: "split the uncommitted work into small commits"},
		},
		policies: map[string]aiActionPolicy{
			"git-ai-changelog": aiNever(aiNoNested),
			"git-ai-compose":   aiNever(aiNoNested),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-ai-changelog":
				return m.gitxAIChangelog(), true
			case "git-ai-compose":
				return m.gitxAICompose(), true
			}
			return nil, false
		},
	})
}

// gitxAIChangelog asks where to start (the latest tag first; empty: the
// whole branch) and fills the chat with the subjects since then.
func (m *Model) gitxAIChangelog() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	m.gitxPrompt("Write Changelog with AI", "Changes since (tag or commit; empty: all)", git.LatestTag(cwd), func(m *Model, from string) tea.Cmd {
		from = strings.TrimSpace(from)
		subjects, err := git.SubjectsSince(cwd, from, gitxChangelogMax)
		if err != nil {
			return m.gitxFail("No commit "+from, err)
		}
		if len(subjects) == 0 {
			return m.aiInfo("No commits since " + from)
		}
		since := "the whole history of " + m.gitBranch.Name
		if from != "" {
			since = from
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Write a changelog in Markdown for the changes since %s. Group the entries (Added, Changed, Fixed, Removed), one short line each, in plain words for users; leave out changes that only touch tests, CI or chores.\n\nCommits (newest first):\n", since)
		for _, s := range subjects {
			b.WriteString("- " + s + "\n")
		}
		return m.gitAIPrefillChat(b.String())
	})
	return nil
}

// gitxAICompose fills the chat with the uncommitted changes and asks for
// a plan of small commits.
func (m *Model) gitxAICompose() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	diff, cut, err := git.ChangesDiff(cwd, false, gitAIChatMax)
	if err != nil || strings.TrimSpace(diff) == "" {
		return m.aiInfo("No changes to split into commits")
	}
	return m.gitAIPrefillChat("Split my uncommitted changes into small commits that each do one thing. For each commit list the files (or hunks) that belong to it and a commit message in the repository's style, in the order to make them. Do not run anything.\n\n" + gitAIFence(diff, cut))
}
