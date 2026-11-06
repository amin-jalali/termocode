package app

import (
	"path"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// .gitignore actions and the git identity (GITX-30).

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-ignore-file", Title: "Git: Add to .gitignore…"},
			{ID: "git-identity", Title: "Git: Set Identity (Name and E-mail)…"},
		},
		policies: map[string]aiActionPolicy{
			"git-ignore-file": aiNever(aiNoPicker),
			"git-identity":    aiNever("sets the name and e-mail on the user's commits; only the user does that"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-ignore-file":
				return m.openIgnorePicker(), true
			case "git-identity":
				return m.openIdentityPrompts(), true
			}
			return nil, false
		},
	})
}

// openIgnorePicker offers the active file, all files with its extension
// and its folder.
func (m *Model) openIgnorePicker() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	rel, ok := m.activeGitRel()
	if !ok {
		return m.gitxInfo("Open a file of this repository first")
	}
	rel = filepath.ToSlash(rel)
	items := []picker.Item{{ID: "ignore-file", Title: "This file", Hint: "/" + rel}}
	if ext := path.Ext(rel); ext != "" {
		items = append(items, picker.Item{ID: "ignore-ext", Title: "All " + ext + " files", Hint: "*" + ext})
	}
	if d := path.Dir(rel); d != "." {
		items = append(items, picker.Item{ID: "ignore-dir", Title: "Its folder", Hint: "/" + d + "/"})
	}
	m.gitxPick(" Add to .gitignore ", items, func(m *Model, id string) tea.Cmd {
		for _, it := range items {
			if it.ID == id {
				return m.gitxIgnore(rel, it.Hint, id == "ignore-file")
			}
		}
		return nil
	})
	return nil
}

// gitxIgnore adds pattern. Ignoring a tracked file changes nothing until
// it stops being tracked, so a card asks (Cancel first).
func (m *Model) gitxIgnore(rel, pattern string, oneFile bool) tea.Cmd {
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	add := func(m *Model, untrack bool) tea.Cmd {
		if err := git.AddIgnore(cwd, pattern); err != nil {
			return m.gitxFail("Ignore failed", err)
		}
		if untrack {
			if err := git.Untrack(cwd, rel); err != nil {
				return m.gitxFail("Stop tracking failed", err)
			}
		}
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "Added to .gitignore", pattern)
		return tea.Batch(fetchGitCmd(), c)
	}
	if !oneFile || !git.IsTracked(cwd, rel) {
		return add(m, false)
	}
	m.gitxCard("Ignore "+rel+"?", rel+" is tracked: git keeps showing its changes until it stops being tracked. "+
		"Stop Tracking keeps the file on disk and stages its removal from the repository.",
		func(m *Model, id string) tea.Cmd {
			switch id {
			case "ignore":
				return add(m, false)
			case "untrack":
				return add(m, true)
			}
			return nil
		}, confirm.Button{ID: "ignore", Title: "Ignore Only"}, confirm.Button{ID: "untrack", Title: "Ignore and Stop Tracking"})
	return nil
}

// openIdentityPrompts asks the name, the e-mail, then this repository or
// all of them.
func (m *Model) openIdentityPrompts() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	name, email := git.Identity(cwd)
	m.gitxPrompt("Git Identity", "Name on your commits", name, func(m *Model, n string) tea.Cmd {
		m.gitxPrompt("Git Identity", "E-mail on your commits", email, func(m *Model, e string) tea.Cmd {
			m.gitxPick(" Use this identity where? ", []picker.Item{
				{ID: "id-repo", Title: "This repository only", Hint: "git config --local"},
				{ID: "id-global", Title: "All repositories", Hint: "git config --global"},
			}, func(m *Model, id string) tea.Cmd {
				if id != "id-repo" && id != "id-global" {
					return nil
				}
				if err := git.SetIdentity(cwd, n, e, id == "id-global"); err != nil {
					return m.gitxFail("Identity not set", err)
				}
				var c tea.Cmd
				m.toast, c = m.toast.PushDetail(toast.Info, "Identity set", strings.TrimSpace(n)+" <"+strings.TrimSpace(e)+">")
				return c
			})
			return nil
		})
		return nil
	})
	return nil
}
