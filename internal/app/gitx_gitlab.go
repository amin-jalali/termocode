package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/gitlab"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// GITX-39: GitLab merge requests, like the GitHub pull request commands:
// list the open ones of the GitLab remote (pick one to open it in the
// browser) and create one from the current branch. The token is kept
// only in the secret store (internal/secrets), one per host; a reference
// (${env:NAME}, ${cmd:…}) may stand in for it and is resolved on use.

// gitlabAPI is the API address of a host (tests point it at a fake).
var gitlabAPI = func(host string) string { return "https://" + host }

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-gitlab-mrs", Title: "Git: GitLab Merge Requests…", Hint: "the open merge requests of the GitLab remote; Enter opens one"},
			{ID: "git-gitlab-mr-create", Title: "Git: Create GitLab Merge Request…", Hint: "from the current branch into the default branch"},
			{ID: "git-gitlab-token", Title: "Git: Set GitLab Token…", Hint: "saved in the secret store for this GitLab host"},
		},
		policies: map[string]aiActionPolicy{
			"git-gitlab-mrs":       aiAuto(), // read only: a list of open merge requests
			"git-gitlab-mr-create": aiNever("publishes a merge request in the user's name; only the user creates one"),
			"git-gitlab-token":     aiNever("a token is typed by the user only"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-gitlab-mrs":
				return m.gitlabListMRs(), true
			case "git-gitlab-mr-create":
				return m.gitlabCreateMR(), true
			case "git-gitlab-token":
				return m.gitlabSetToken(), true
			}
			return nil, false
		},
	})
}

// gitlabSecretName is the store name of a host's token:
// gitlab.token.<host>, lower case, [a-z0-9_.-] only.
func gitlabSecretName(host string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(host) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		case r == '.':
			if s := b.String(); s != "" && !strings.HasSuffix(s, ".") {
				b.WriteByte('.')
			}
		default:
			b.WriteByte('-')
		}
	}
	return "gitlab.token." + strings.Trim(b.String(), ".-")
}

// gitlabProject is the GitLab project of origin (or the only remote).
func (m *Model) gitlabProject() (gitlab.Project, string, tea.Cmd, bool) {
	cwd, _ := m.rootDir()
	base, err := git.RemoteWeb(cwd)
	p, ok := gitlab.FromWebBase(base)
	if err != nil || !ok {
		return gitlab.Project{}, "", m.gitxInfo("This repository has no GitLab remote (origin on a gitlab host)"), false
	}
	return p, cwd, nil, true
}

// gitlabWithToken runs then with the saved token (or its reference). No
// token yet: it is asked first, saved, and then runs.
func (m *Model) gitlabWithToken(p gitlab.Project, then func(m *Model, stored string) tea.Cmd) tea.Cmd {
	return m.withSecretsUnlocked(func(m *Model) tea.Cmd {
		s, err := m.secretStore()
		if err != nil || s == nil {
			return m.gitxInfo("The secret store is not available: the GitLab token cannot be kept")
		}
		v, ok, err := s.Get(gitlabSecretName(p.Host))
		if err != nil {
			return m.gitxFail("Could not read the GitLab token", errors.New(secretsRedact(err)))
		}
		if ok && v != "" {
			return then(m, v)
		}
		return m.gitlabAskToken(p, then)
	})
}

// gitlabAskToken opens the masked token prompt; Enter saves it in the
// secret store and goes on with then (nil: only save).
func (m *Model) gitlabAskToken(p gitlab.Project, then func(m *Model, stored string) tea.Cmd) tea.Cmd {
	host := sanitizeOutputLine(p.Host)
	m.gitxPrompt(" GitLab token for "+host+" ", "Token (api scope) or ${env:NAME}", "", func(m *Model, value string) tea.Cmd {
		m.clearPrompt()
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		if _, err := secrets.ParseRef(value); err != nil && !errors.Is(err, secrets.ErrNotRef) {
			return m.gitxInfo("Not a valid reference: use ${env:NAME}, ${dotenv:NAME} or ${cmd:…}")
		}
		if !secrets.IsRef(value) {
			ai.RegisterSecret(value)
		}
		return m.withSecretsUnlocked(func(m *Model) tea.Cmd {
			s, err := m.secretStore()
			if err == nil && s != nil {
				err = s.Set(gitlabSecretName(p.Host), value)
			}
			if err != nil || s == nil {
				return m.gitxInfo("The GitLab token was not saved: the secret store is not available")
			}
			if then == nil {
				return m.gitxInfo("GitLab token saved for " + host)
			}
			return then(m, value)
		})
	})
	m.prompt.SetMasked(true)
	return nil
}

// gitlabRun resolves the token and runs work off the UI loop; work's
// answer is applied on the loop. A refused token says how to set it
// again; no error holds the token.
func (m *Model) gitlabRun(p gitlab.Project, stored string, work func(ctx context.Context, c *gitlab.Client) func(m *Model) tea.Cmd) tea.Cmd {
	root, _ := m.rootDir()
	return func() tea.Msg {
		token := stored
		if ref, err := secrets.ParseRef(stored); err == nil {
			v, rerr := newRefResolver(root).Resolve(ref)
			if rerr != nil {
				return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
					return m.gitxFail("Could not read the GitLab token", errors.New(sanitizeOutputLine(errLine(ai.Redact(rerr.Error())))))
				}}
			}
			token = v
		}
		ai.RegisterSecret(token)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return gitxDoneMsg{apply: work(ctx, gitlab.New(gitlabAPI(p.Host), token))}
	}
}

// gitlabFail shows a GitLab error in one clean line.
func (m *Model) gitlabFail(title string, err error) tea.Cmd {
	text := sanitizeOutputLine(errLine(ai.Redact(err.Error())))
	if errors.Is(err, gitlab.ErrUnauthorized) {
		text += ". Run Git: Set GitLab Token… to change it."
	}
	return m.gitxFail(title, errors.New(text))
}

func (m *Model) gitlabListMRs() tea.Cmd {
	p, _, note, ok := m.gitlabProject()
	if !ok {
		return note
	}
	return m.gitlabWithToken(p, func(m *Model, stored string) tea.Cmd {
		return tea.Batch(m.gitxInfo("Reading the merge requests…"), m.gitlabRun(p, stored, func(ctx context.Context, c *gitlab.Client) func(m *Model) tea.Cmd {
			mrs, err := c.ListMRs(ctx, p.Path)
			return func(m *Model) tea.Cmd {
				if err != nil {
					return m.gitlabFail("Could not read the merge requests", err)
				}
				if len(mrs) == 0 {
					return m.gitxInfo("No open merge requests in " + sanitizeOutputLine(p.Path))
				}
				items := make([]picker.Item, 0, len(mrs))
				urls := map[string]string{}
				for _, mr := range mrs {
					id := "mr-" + strconv.Itoa(mr.IID)
					hint := sanitizeOutputLine(mr.Source) + " → " + sanitizeOutputLine(mr.Target)
					if mr.Author != "" {
						hint += " · " + sanitizeOutputLine(mr.Author)
					}
					if mr.Draft {
						hint += " · draft"
					}
					items = append(items, picker.Item{ID: id, Title: "!" + strconv.Itoa(mr.IID) + " " + sanitizeOutputLine(mr.Title), Hint: hint})
					urls[id] = mr.URL
				}
				m.gitxPick(" Merge requests of "+sanitizeOutputLine(p.Path)+" ", items, func(m *Model, id string) tea.Cmd {
					u := urls[id]
					if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
						return nil
					}
					if err := osOpen(u); err != nil {
						return m.gitxFail("Open failed", err)
					}
					var c tea.Cmd
					m.toast, c = m.toast.PushDetail(toast.Info, "Opened", u)
					return c
				})
				return nil
			}
		}))
	})
}

func (m *Model) gitlabCreateMR() tea.Cmd {
	p, cwd, note, ok := m.gitlabProject()
	if !ok {
		return note
	}
	branch := m.gitBranch.Name
	target := git.DefaultBranch(cwd)
	if target == "" {
		return m.gitxInfo("No default branch (main or master) to merge into")
	}
	if branch == "" || branch == target {
		return m.gitxInfo("Switch to the branch to merge first (now on " + sanitizeOutputLine(target) + ")")
	}
	subject := ""
	if c := m.gitxHeadCommit(); c != nil {
		subject = c.Subject
	}
	m.gitxPrompt(" Merge request title ", "Title", subject, func(m *Model, title string) tea.Cmd {
		title = strings.TrimSpace(title)
		if title == "" {
			return nil
		}
		m.gitxCard("Create a merge request on "+sanitizeOutputLine(p.Host)+"?",
			"Merges "+sanitizeOutputLine(branch)+" into "+sanitizeOutputLine(target)+" of "+sanitizeOutputLine(p.Path)+": "+sanitizeOutputLine(title)+
				". The branch must be pushed first.",
			func(m *Model, id string) tea.Cmd {
				if id != "create" && id != "draft" {
					return nil
				}
				in := gitlab.NewMR{Source: branch, Target: target, Title: title, Draft: id == "draft"}
				return m.gitlabWithToken(p, func(m *Model, stored string) tea.Cmd {
					return tea.Batch(m.gitxInfo("Creating the merge request…"), m.gitlabRun(p, stored, func(ctx context.Context, c *gitlab.Client) func(m *Model) tea.Cmd {
						mr, err := c.CreateMR(ctx, p.Path, in)
						return func(m *Model) tea.Cmd {
							if err != nil {
								return m.gitlabFail("Merge request not created", err)
							}
							var c tea.Cmd
							m.toast, c = m.toast.PushDetail(toast.Info, "Merge request !"+strconv.Itoa(mr.IID)+" created", sanitizeOutputLine(mr.URL))
							return c
						}
					}))
				})
			},
			confirm.Button{ID: "create", Title: "Create"}, confirm.Button{ID: "draft", Title: "Create as Draft"})
		return nil
	})
	return nil
}

func (m *Model) gitlabSetToken() tea.Cmd {
	p, _, note, ok := m.gitlabProject()
	if !ok {
		return note
	}
	return m.withSecretsUnlocked(func(m *Model) tea.Cmd { return m.gitlabAskToken(p, nil) })
}
