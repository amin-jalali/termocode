package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// GITX-39 (O26): share the active file as a GitHub gist (gh gist
// create). It asks secret or public, then on a Cancel-first card; a file
// that may hold secrets, by its name or by a secret-looking line, is
// never offered.

const gitxGistMax = 1 << 20

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-gist", Title: "Git: Create Gist from File…", Hint: "share the active file as a GitHub gist (gh)"},
		},
		policies: map[string]aiActionPolicy{
			"git-gist": aiNever("publishes a file of the user on GitHub; only the user does that"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			if id != "git-gist" {
				return nil, false
			}
			return m.gitxOpenGist(), true
		},
	})
}

func (m *Model) gitxOpenGist() tea.Cmd {
	abs := m.editor.Path()
	if abs == "" {
		return m.gitxInfo("Open the file to share as a gist first")
	}
	if ai.IsSecretFileName(abs) {
		return m.gitxInfo("This file may hold secrets: it is not shared as a gist")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return m.gitxFail("Could not read the file", err)
	}
	if len(data) > gitxGistMax {
		return m.gitxInfo("The file is bigger than 1 MB: too big for a gist")
	}
	if strings.IndexByte(string(data), 0) >= 0 {
		return m.gitxInfo("This is a binary file: a gist holds text")
	}
	for _, l := range strings.Split(string(data), "\n") {
		if gitxLooksSecret(l) {
			return m.gitxInfo("This file may hold secrets (a key or token): it is not shared as a gist")
		}
	}
	cwd, _ := m.rootDir()
	arg := abs
	if rel, err := filepath.Rel(cwd, abs); err == nil && cwd != "" && !strings.HasPrefix(rel, "..") {
		arg = filepath.ToSlash(rel)
	}
	name := filepath.Base(abs)
	m.gitxPick(" Who can see the gist of "+name+"? ", []picker.Item{
		{ID: "gist-secret", Title: "Secret", Hint: "only people with the link"},
		{ID: "gist-public", Title: "Public", Hint: "listed on your GitHub profile"},
	}, func(m *Model, vis string) tea.Cmd {
		if vis != "gist-secret" && vis != "gist-public" {
			return nil
		}
		public := vis == "gist-public"
		what := "a secret gist (anyone with the link can read it)"
		if public {
			what = "a public gist (listed on your GitHub profile)"
		}
		m.gitxCard("Share "+name+" as a gist?", "gh uploads "+arg+" as it is saved on disk to GitHub as "+what+".",
			func(m *Model, id string) tea.Cmd {
				if id != "gist" {
					return nil
				}
				args := []string{"gist", "create", "--desc", name}
				if public {
					args = append(args, "--public")
				}
				args = append(args, "--", arg)
				return tea.Batch(m.gitxInfo("Creating the gist…"), m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
					out, stderr, err := run(ctx, dir, args...)
					if err != nil && strings.TrimSpace(stderr) != "" && err != ghci.ErrNoGH {
						err = errors.New(strings.TrimSpace(stderr))
					}
					return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
						if err != nil {
							return m.prFailToast("Gist not created", err)
						}
						var c tea.Cmd
						m.toast, c = m.toast.PushDetail(toast.Info, "Gist created", strings.TrimSpace(out))
						return c
					}}
				}))
			}, confirm.Button{ID: "gist", Title: "Create Gist"})
		return nil
	})
	return nil
}
