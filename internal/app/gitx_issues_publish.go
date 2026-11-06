package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Issues and "start work" (GITX-36), init a repository and publish it
// to GitHub (GITX-37). gh does the GitHub part.

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-issues", Title: "Git: Issues… (Start Work on One)"},
			{ID: "git-init", Title: "Git: Initialize Repository"},
			{ID: "git-publish", Title: "Git: Publish to GitHub…"},
		},
		policies: map[string]aiActionPolicy{
			"git-issues":  aiNever(aiNoPicker),
			"git-init":    aiNever("makes a new git repository in the user's folder; user only"),
			"git-publish": aiNever("creates a GitHub repository in the user's name and pushes to it; user only"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-issues":
				return m.openIssues(), true
			case "git-init":
				return m.gitxInit(), true
			case "git-publish":
				return m.openPublish(), true
			}
			return nil, false
		},
	})
}

type gitxIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// openIssues asks gh for the open issues (off the UI loop).
func (m *Model) openIssues() tea.Cmd {
	m.gitEnsureLoaded()
	if !m.gitIsRepo {
		return m.gitxInfo("Issues need a folder with a git repository")
	}
	return tea.Batch(m.gitxInfo("Reading issues…"), m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		out, stderr, err := run(ctx, dir, "issue", "list", "--state", "open", "--limit", "100", "--json", "number,title,labels")
		var issues []gitxIssue
		if err == nil {
			err = json.Unmarshal([]byte(out), &issues)
		} else if s := strings.TrimSpace(stderr); s != "" && err != ghci.ErrNoGH {
			err = errors.New(s)
		}
		return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
			if err != nil {
				return m.prFailToast("Could not read issues", err)
			}
			return m.onIssues(issues)
		}}
	}))
}

func (m *Model) onIssues(issues []gitxIssue) tea.Cmd {
	if len(issues) == 0 {
		return m.gitxInfo("No open issues")
	}
	items := make([]picker.Item, 0, len(issues))
	for _, is := range issues {
		var ls []string
		for _, l := range is.Labels {
			ls = append(ls, l.Name)
		}
		items = append(items, picker.Item{ID: "issue-" + strconv.Itoa(is.Number), Title: "#" + strconv.Itoa(is.Number) + " " + is.Title, Hint: strings.Join(ls, ", ")})
	}
	m.gitxPick(" Issues ", items, func(m *Model, id string) tea.Cmd {
		for _, is := range issues {
			if id == "issue-"+strconv.Itoa(is.Number) {
				return m.openIssueActions(is)
			}
		}
		return nil
	})
	return nil
}

func (m *Model) openIssueActions(is gitxIssue) tea.Cmd {
	ref := "#" + strconv.Itoa(is.Number)
	m.gitxPick(" "+ref+" "+is.Title+" ", []picker.Item{
		{ID: "start", Title: "Start Work: New Branch", Hint: issueBranch(is)},
		{ID: "copy", Title: "Copy " + ref},
	}, func(m *Model, id string) tea.Cmd {
		switch id {
		case "start":
			m.gitxPrompt("Start Work on "+ref, "New branch (from "+m.gitBranch.Name+")", issueBranch(is), func(m *Model, name string) tea.Cmd {
				name = strings.TrimSpace(name)
				cwd, err := m.rootDir()
				if err != nil || name == "" {
					return nil
				}
				if strings.HasPrefix(name, "-") {
					return m.gitxFail("Branch not made", errors.New("a branch name can not start with -"))
				}
				if err := git.CreateBranch(cwd, name); err != nil {
					return m.gitxFail("Branch not made", err)
				}
				var c tea.Cmd
				m.toast, c = m.toast.PushDetail(toast.Info, "On "+name, "Working on "+ref+" "+is.Title)
				return tea.Batch(c, fetchGitCmd())
			})
		case "copy":
			m.copyToClipboard(ref)
			return m.gitxInfo("Copied " + ref)
		}
		return nil
	})
	return nil
}

// issueBranch is "12-login-crashes-on-empty-password": the number and
// the title's words, lower case, at most 50 letters.
func issueBranch(is gitxIssue) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(is.Title) {
		if unicode.IsLetter(r) && r < 128 || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 50 {
		slug = strings.Trim(slug[:50], "-")
	}
	name := strconv.Itoa(is.Number)
	if slug != "" {
		name += "-" + slug
	}
	return name
}

// gitxInit makes a repository (branch main) in the workspace folder.
func (m *Model) gitxInit() tea.Cmd {
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	if git.IsRepo(cwd) {
		return m.gitxInfo("This folder is already a git repository")
	}
	cmd := spawn.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		return m.gitxFail("git init failed", errors.New(strings.TrimSpace(string(out))))
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, "Made a git repository", "Stage and commit in Source Control, then Git: Publish to GitHub…")
	return tea.Batch(c, fetchGitCmd())
}

// openPublish asks the name, private or public, then asks on a
// Cancel-first card; gh creates the repository, adds origin and pushes.
func (m *Model) openPublish() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	rc := spawn.Command("git", "remote")
	rc.Dir = cwd
	if out, err := rc.Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		return m.gitxInfo("This repository has a remote already (" + strings.Join(strings.Fields(string(out)), ", ") + ")")
	}
	m.gitxPrompt("Publish to GitHub", "Repository name", filepath.Base(cwd), func(m *Model, name string) tea.Cmd {
		name = strings.TrimSpace(name)
		if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, " \t\n") {
			return m.gitxInfo("Type a repository name without spaces")
		}
		m.gitxPick(" Who can see "+name+"? ", []picker.Item{
			{ID: "private", Title: "Private", Hint: "only you and people you add"},
			{ID: "public", Title: "Public", Hint: "everyone on the internet"},
		}, func(m *Model, vis string) tea.Cmd {
			if vis != "private" && vis != "public" {
				return nil
			}
			m.gitxCard("Publish "+name+" ("+vis+")?", "gh creates the GitHub repository "+name+" ("+vis+") in your account, adds it as origin and pushes "+m.gitBranch.Name+".",
				func(m *Model, id string) tea.Cmd {
					if id != "publish" {
						return nil
					}
					return tea.Batch(m.gitxInfo("Publishing "+name+"…"), m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
						out, stderr, err := run(ctx, dir, "repo", "create", name, "--"+vis, "--source", ".", "--remote", "origin", "--push")
						if err != nil && strings.TrimSpace(stderr) != "" && err != ghci.ErrNoGH {
							err = errors.New(strings.TrimSpace(stderr))
						}
						return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
							if err != nil {
								return tea.Batch(fetchGitCmd(), m.prFailToast("Publish failed", err))
							}
							var c tea.Cmd
							m.toast, c = m.toast.PushDetail(toast.Info, "Published "+name, strings.TrimSpace(out))
							return tea.Batch(c, fetchGitCmd())
						}}
					}))
				}, confirm.Button{ID: "publish", Title: "Publish"})
			return nil
		})
		return nil
	})
	return nil
}
