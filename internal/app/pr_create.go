package app

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// Git: Create Pull Request…: from the current branch. The title is the
// first commit subject and the body lists all subjects; the base is the
// default branch. The user edits the title, then a card shows the whole
// request with Cancel first and a Create / Create as Draft choice. A
// branch that is not pushed is refused with an offer to push it (a plain
// push, never forced).

const (
	promptKindPRCreateTitle promptKindEnum  = 3201
	confirmKindPRCreate     confirmKindEnum = 3202
	confirmKindPRPush       confirmKindEnum = 3203
	promptKindPRReviewers   promptKindEnum  = 3214 // GITX-23
	promptKindPRLabels      promptKindEnum  = 3215
)

func prCreatePaletteItems() []picker.Item {
	return []picker.Item{{ID: "git-pr-create", Title: "Git: Create Pull Request…"}}
}

// prCreatePrepMsg is what the branch looks like.
type prCreatePrepMsg struct {
	branch, base string
	subjects     []string
	unpushed     int
	pushed       bool   // the branch has an upstream
	template     string // the repository's pull request template ("" none)
	templateName string
	err          error
}

// prCreateCmd reads the default branch (gh) and the branch's commits (git).
func (m *Model) prCreateCmd() tea.Cmd {
	m.gitEnsureLoaded()
	if !m.gitIsRepo || m.gitBranch.Name == "" {
		return m.aiInfo("Create Pull Request needs a git branch: open a folder with a git repository")
	}
	branch := m.gitBranch.Name
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		base, err := ghci.DefaultBranch(ctx, run, dir)
		if err != nil {
			return prCreatePrepMsg{branch: branch, err: err}
		}
		n, pushed := git.Unpushed(dir)
		subjects, err := git.BranchSubjects(dir, base)
		tpl, tplName, _ := ghci.PRTemplate(dir)
		return prCreatePrepMsg{branch: branch, base: base, subjects: subjects, unpushed: n, pushed: pushed, err: err,
			template: tpl, templateName: tplName}
	})
}

// onPRCreatePrep decides what the branch needs before a request can be made.
func (m *Model) onPRCreatePrep(msg prCreatePrepMsg) tea.Cmd {
	if msg.err != nil {
		return m.prFailToast("Could not prepare the pull request", msg.err)
	}
	switch {
	case msg.branch == msg.base:
		return m.aiInfo("You are on " + msg.base + ", the default branch: switch to a feature branch first")
	case !msg.pushed || msg.unpushed > 0:
		return m.prAskPush(msg)
	case len(msg.subjects) == 0:
		return m.aiInfo("No commits on " + msg.branch + " that " + msg.base + " does not have: nothing to propose")
	}
	title, body := ghci.Compose(msg.subjects)
	m.prS().create = &ghci.NewPR{Title: title, Body: ghci.WithTemplate(msg.template, body), Base: msg.base, Branch: msg.branch}
	m.prS().createTemplate = msg.templateName
	m.prompt = prompt.New("Create Pull Request", "Title:", title)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindPRCreateTitle
	return nil
}

// prAskPush offers the existing push: the branch must be on GitHub first.
func (m *Model) prAskPush(msg prCreatePrepMsg) tea.Cmd {
	text := "Branch " + msg.branch + " is not on GitHub yet."
	if msg.pushed {
		text = "Branch " + msg.branch + " has " + strconv.Itoa(msg.unpushed) + " commit" + plural(msg.unpushed) + " that GitHub does not have."
	}
	text += "\nPush it, then choose Git: Create Pull Request… again."
	m.confirm = confirm.Risky("Push the branch first", text,
		confirm.Button{ID: "cancel", Title: "Not now"},
		confirm.Button{ID: "push", Title: "Push", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindPRPush
	return nil
}

// handlePRCreatePromptSubmit takes the title and shows the card.
func (m *Model) handlePRCreatePromptSubmit(value string) (tea.Cmd, bool) {
	if cmd, ok := m.handlePRNamesSubmit(value); ok {
		return cmd, true
	}
	if m.promptKind != promptKindPRCreateTitle {
		return nil, false
	}
	p := m.prS().create
	if p == nil {
		return nil, true
	}
	p.Title = strings.TrimSpace(value)
	m.showPRCreateCard()
	return nil, true
}

// showPRCreateCard (re)draws the card of the pull request being created.
func (m *Model) showPRCreateCard() {
	p := m.prS().create
	if p == nil {
		return
	}
	text := "Title: " + p.Title + "\n" + p.Branch + " → " + p.Base + "\n"
	text += "Reviewers: " + orNone(strings.Join(p.Reviewers, ", ")) + "\n"
	text += "Labels: " + orNone(strings.Join(p.Labels, ", ")) + "\n"
	if t := m.prS().createTemplate; t != "" {
		text += "Template: " + t + "\n"
	}
	if p.Body != "" {
		text += "\nDescription\n" + p.Body + "\n"
	}
	text += "\nCreate as Draft if it is not ready for review."
	m.confirm = confirm.Risky("Create Pull Request", text,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "create", Title: "Create", Style: confirm.StylePrimary},
		confirm.Button{ID: "draft", Title: "Create as Draft"},
		confirm.Button{ID: "reviewers", Title: "Reviewers…"},
		confirm.Button{ID: "labels", Title: "Labels…"}).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindPRCreate
}

// orNone is s, or "none" when empty.
func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// prAskNames opens the reviewers or labels prompt of the create card.
func (m *Model) prAskNames(labels bool) {
	p := m.prS().create
	if p == nil {
		return
	}
	if labels {
		m.prompt = prompt.New("Labels", "Labels, comma separated (empty: none):", strings.Join(p.Labels, ", "))
		m.promptKind = promptKindPRLabels
	} else {
		m.prompt = prompt.New("Reviewers", "GitHub logins or org/team, comma separated (empty: none):", strings.Join(p.Reviewers, ", "))
		m.promptKind = promptKindPRReviewers
	}
	m.prompt.SetAllowEmpty(true)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
}

// handlePRNamesSubmit takes reviewers or labels and shows the card again.
// ok=false: not ours.
func (m *Model) handlePRNamesSubmit(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindPRReviewers && m.promptKind != promptKindPRLabels {
		return nil, false
	}
	p := m.prS().create
	if p == nil {
		return nil, true
	}
	if m.promptKind == promptKindPRLabels {
		p.Labels = ghci.ParseNames(value, false)
	} else {
		p.Reviewers = ghci.ParseNames(value, true)
	}
	m.showPRCreateCard()
	return nil, true
}

// handlePRCreateConfirm answers the create and push cards. ok=false: not ours.
func (m *Model) handlePRCreateConfirm(id string) (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindPRPush:
		if id == "push" {
			return m.gitPush(), true
		}
		return nil, true
	case confirmKindPRCreate:
		if id == "reviewers" || id == "labels" {
			m.confirmOpen = false
			m.prAskNames(id == "labels")
			return nil, true
		}
		p := m.prS().create
		m.prS().create = nil
		if p == nil || (id != "create" && id != "draft") {
			return nil, true
		}
		p.Draft = id == "draft"
		req := *p
		return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
			url, err := ghci.Create(ctx, run, dir, req)
			return prDoneMsg{ok: "Created the pull request", detail: url, failed: "Could not create the pull request", err: err, ci: true}
		}), true
	}
	return nil, false
}
