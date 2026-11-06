package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// GITX-39: the small git leftovers (docs-local/plans/git-gap.md). This
// file has the commit extras: an empty commit (B10), a commit made as
// another author (B19) and a commit of the marked files only (B8).

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-commit-empty", Title: "Git: Commit Empty (No Changes)…", Hint: "a commit without changes, e.g. to run CI again"},
			{ID: "git-commit-author", Title: "Git: Commit Staged as Another Author…", Hint: "git commit --author; you stay the committer"},
			{ID: "git-commit-marked", Title: "Git: Commit Marked Files Only…", Hint: "the files marked in Source Control (Space); c there"},
		},
		policies: map[string]aiActionPolicy{
			"git-commit-empty":  aiNever("makes a commit in the user's name; only the user does that"),
			"git-commit-author": aiNever("makes a commit under another person's name; only the user does that"),
			"git-commit-marked": aiNever("makes a commit in the user's name; only the user does that"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-commit-empty":
				return m.gitxCommitEmpty(), true
			case "git-commit-author":
				return m.gitxCommitAsAuthor(), true
			case "git-commit-marked":
				return m.gitxCommitMarked(), true
			}
			return nil, false
		},
		key: func(m *Model, k tea.KeyMsg) (tea.Cmd, bool) {
			if k.String() == "c" && m.gitSelCount() > 0 {
				return m.gitxCommitMarked(), true
			}
			return nil, false
		},
	})
}

// gitxCommitExtra commits with o and the typed message; a message from
// the commit box empties the box.
func (m *Model) gitxCommitExtra(cwd, msg string, o git.CommitOptions) tea.Cmd {
	return m.gitxCommitPaths(cwd, msg, nil, o)
}

// gitxCommitPaths is gitxCommitExtra for paths only (nil: the index).
func (m *Model) gitxCommitPaths(cwd, msg string, paths []string, o git.CommitOptions) tea.Cmd {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return m.gitxInfo("Enter a commit message")
	}
	o.NoVerify = m.gitx.skipHooks
	var err error
	if paths != nil {
		err = git.CommitPaths(cwd, msg, paths, o)
	} else {
		err = git.CommitOpts(cwd, msg, o)
	}
	if err != nil {
		return tea.Batch(fetchGitCmd(), m.gitxFail("Commit failed", err))
	}
	if strings.TrimSpace(m.gitCommitMsg) == msg {
		m.clearCommitBox()
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, "Committed", commitToastLabel(msg, false))
	return tea.Batch(fetchGitCmd(), c)
}

// gitxCommitEmpty asks the message (the commit box text first) and
// makes a commit without changes.
func (m *Model) gitxCommitEmpty() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	m.gitxPrompt("Commit Empty", "Commit message (no file changes)", strings.TrimSpace(m.gitCommitMsg), func(m *Model, msg string) tea.Cmd {
		return m.gitxCommitExtra(cwd, msg, git.CommitOptions{AllowEmpty: true})
	})
	return nil
}

// gitxCommitAsAuthor asks the author (the own identity first), then the
// message, and commits what is staged with that author.
func (m *Model) gitxCommitAsAuthor() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	if staged, _ := m.gitFileCounts(); staged == 0 {
		return m.gitxInfo("Nothing is staged: stage the changes for this commit first")
	}
	def := ""
	if n, e := git.Identity(cwd); n != "" && e != "" {
		def = n + " <" + e + ">"
	}
	m.gitxPrompt("Commit as Another Author", "Author (Name <e-mail>)", def, func(m *Model, s string) tea.Cmd {
		author, err := git.ParseAuthor(s)
		if err != nil {
			return m.gitxInfo("Type the author as Name <e-mail>")
		}
		m.gitxPrompt("Commit as "+author, "Commit message", strings.TrimSpace(m.gitCommitMsg), func(m *Model, msg string) tea.Cmd {
			return m.gitxCommitExtra(cwd, msg, git.CommitOptions{Author: author})
		})
		return nil
	})
	return nil
}

// gitxCommitMarked asks the message (the commit box text first) and
// commits only the files marked in Source Control.
func (m *Model) gitxCommitMarked() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	var paths []string
	seen := map[string]bool{}
	for _, k := range m.gitSelRows() {
		if !seen[k.path] {
			seen[k.path] = true
			paths = append(paths, k.path)
		}
	}
	if len(paths) == 0 {
		return m.gitxInfo("Mark the files to commit first: Space on their rows in Source Control")
	}
	m.gitxPrompt("Commit "+gitxPlural(len(paths), "Marked File"), "Commit message (only the marked files)", strings.TrimSpace(m.gitCommitMsg), func(m *Model, msg string) tea.Cmd {
		c := m.gitxCommitPaths(cwd, msg, paths, git.CommitOptions{})
		if strings.TrimSpace(msg) != "" {
			m.gitSelClear()
		}
		return c
	})
	return nil
}
