package app

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Commit hooks (GITX-27; B23, P5): when a git hook stops a commit, a
// Cancel-first card shows the start of its output, Show Hook Output
// opens all of it and Commit Without Hooks commits with --no-verify.
// Git: Toggle Skip Hooks on Commit skips the hooks for this session
// only; it is never saved.

const gitxHookCardLines = 8

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-skip-hooks", Title: "Git: Toggle Skip Hooks on Commit (--no-verify)", Hint: "this session only"},
			{ID: "git-hook-output", Title: "Git: Show Last Hook Output"},
		},
		policies: map[string]aiActionPolicy{
			"git-skip-hooks":  aiNever("turns off the checks the repository runs on each commit; only the user does that"),
			"git-hook-output": aiAuto(),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-skip-hooks":
				m.gitx.skipHooks = !m.gitx.skipHooks
				if m.gitx.skipHooks {
					return m.pushToastDetail(toast.Warn, "Commit hooks skipped", "Commits skip the pre-commit and commit-msg hooks (--no-verify) until you turn this off or quit."), true
				}
				return m.pushToastDetail(toast.Info, "Commit hooks run again", ""), true
			case "git-hook-output":
				if m.gitx.hookOutput == "" {
					return m.gitxInfo("No hook has stopped a commit yet"), true
				}
				return m.gitxShowHookOutput(), true
			}
			return nil, false
		},
	})
}

// gitxHookCard asks what to do when a hook stopped the commit.
func (m *Model) gitxHookCard(err error, cwd, msg string, amend, signoff bool, after string) tea.Cmd {
	out := err.Error()
	var ce *git.CmdError
	if errors.As(err, &ce) && ce.Output != "" {
		out = ce.Output
	}
	m.gitx.hookOutput = ai.Redact(out)
	lines := strings.Split(strings.TrimSpace(m.gitx.hookOutput), "\n")
	if len(lines) > gitxHookCardLines {
		lines = append(lines[:gitxHookCardLines-1], "… (Show Hook Output has all of it)")
	}
	title := "A git hook stopped the commit"
	text := strings.Join(lines, "\n") + "\n\nCommit Without Hooks skips them (--no-verify) for this commit only."
	m.gitx.checks.card = title + "\n" + text
	m.gitxCard(title, text, func(m *Model, id string) tea.Cmd {
		switch id {
		case "output":
			return m.gitxShowHookOutput()
		case "noverify":
			m.gitx.noVerifyOnce = true
			m.gitx.checks.passed = true // the checks ran for this commit already
			return m.gitCommitStaged(cwd, msg, amend, signoff, after)
		}
		return nil
	}, confirm.Button{ID: "output", Title: "Show Hook Output"}, confirm.Button{ID: "noverify", Title: "Commit Without Hooks", Style: confirm.StyleDestructive})
	return nil
}

// gitxShowHookOutput opens the last hook output as a read-only page.
func (m *Model) gitxShowHookOutput() tea.Cmd {
	m.ensureEditorWindowCurrent()
	if err := m.openReadOnlyPage("Git Hook Output", m.gitx.hookOutput, "", "log"); err != nil {
		return m.gitxFail("Could not open the hook output", err)
	}
	return nil
}
