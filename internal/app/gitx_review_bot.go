package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prreview"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Git: Set Up PR Review Bot (CMP-36) adds a GitHub workflow that runs
// `termocode review` on every pull request: a short AI review with only
// the high-priority findings, posted as one comment. A Cancel-first card
// shows the file before it is written; an existing file is never
// replaced.

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-pr-review-bot", Title: "Git: Set Up PR Review Bot (GitHub)…"},
		},
		policies: map[string]aiActionPolicy{
			"git-pr-review-bot": aiNever("writes a CI workflow into the user's repository; the user confirms it on a card"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			if id != "git-pr-review-bot" {
				return nil, false
			}
			return m.gitxReviewBot(), true
		},
	})
}

// gitxReviewBot shows the workflow on a Cancel-first card.
func (m *Model) gitxReviewBot() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	root, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	path := filepath.Join(root, filepath.FromSlash(prreview.WorkflowPath))
	if _, err := os.Stat(path); err == nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "The PR review bot is already set up", prreview.WorkflowPath)
		return c
	}
	preview := strings.Split(strings.TrimRight(prreview.Workflow, "\n"), "\n")
	if len(preview) > 14 {
		preview = append(preview[:14:14], "…")
	}
	text := "Adds " + prreview.WorkflowPath + ". On every pull request it runs `termocode review`:\n" +
		"the AI posts one short comment with only P0/P1 findings. Comment /termocode review to run it again.\n" +
		"It needs the repository secret ANTHROPIC_API_KEY (or OPENAI_API_KEY). Nothing is committed or pushed.\n\n" +
		strings.Join(preview, "\n")
	m.gitxCard("Set up the PR review bot?", text, func(m *Model, id string) tea.Cmd {
		if id != "add" {
			return nil
		}
		return m.gitxWriteReviewBot(path)
	}, confirm.Button{ID: "add", Title: "Add Workflow"})
	m.confirm = m.confirm.WithWideText()
	return nil
}

// gitxWriteReviewBot writes the workflow file; a file made in the
// meantime stays as it is.
func (m *Model) gitxWriteReviewBot(path string) tea.Cmd {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return m.gitxFail("Could not add the workflow", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return m.gitxInfo("The PR review bot is already set up")
	}
	if err != nil {
		return m.gitxFail("Could not add the workflow", err)
	}
	_, werr := f.WriteString(prreview.Workflow)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
		return m.gitxFail("Could not add the workflow", werr)
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, "Added "+prreview.WorkflowPath,
		"Commit and push it, then add the ANTHROPIC_API_KEY secret in the repository settings")
	return tea.Batch(c, m.pollGitCmd())
}
