package app

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// aiSkillsDemo sets up the skills shots (dev action
// "skillsdemo:chat|picker|trust"): a turn that loaded a skill, AI:
// Skills… with sample project and user skills (never the real files),
// and the project skills trust card.
func (m *Model) aiSkillsDemo(arg string) tea.Cmd {
	s := m.aiS()
	root := aiWorkspaceRoot()
	pdir := filepath.Join(root, filepath.FromSlash(ai.SkillsDir))
	udir := aiSkillsUserDir()
	fake := ai.Skills{
		List: []ai.Skill{
			{Name: "commit-style", Description: "How to write commit messages in this repo", Source: ai.SkillSourceUser, Path: filepath.Join(udir, "commit-style.md"), Body: "-"},
			{Name: "go-tests", Description: "Run and fix the Go tests; golden frames and -race", Source: ai.SkillSourceProject, Path: filepath.Join(pdir, "go-tests.md"), Body: "-"},
			{Name: "release", Description: "Steps to cut a release and update the changelog", Source: ai.SkillSourceProject, Path: filepath.Join(pdir, "release.md"), Body: "-"},
			{Name: "toast-ui", Description: "How toasts slide in and out; the frozen clock in tests", Source: ai.SkillSourceProject, Path: filepath.Join(pdir, "toast-ui.md"), Body: "-"},
		},
		Notes:       []string{ai.SkillsDir + "/draft.md: no description in the front matter"},
		ProjectHash: "demo",
	}
	s.skillsFake = &fake
	switch arg {
	case "picker":
		return m.openAISkills()
	case "trust":
		return m.askAISkillsTrust(fake, false, "")
	}
	s.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "Why does the toast slide out twice?"},
		{Role: ai.StoredTool, Text: aiSkillChip + "toast-ui"},
		{Role: ai.StoredTool, Text: "⚙ Search \"leaving\" in internal/toast → 3 matches"},
		{Role: ai.StoredAssistant, Text: aiDemoAnswer},
	}
	s.scroll = 0
	return nil
}
