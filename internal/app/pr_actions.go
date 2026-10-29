package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Actions… in the PR view: a short list of what the user can do with the
// pull request. Nothing here is reachable by the AI (ai_actions_policy.go
// only allows the read-only list); every change asks the user.

const pickerKindPRActions pickerKindEnum = 3201

// prActionItems are the rows for the pull request in the view.
func prActionItems(d ghci.PRDetail) []picker.Item {
	items := []picker.Item{
		{ID: "browser", Title: "Open in Browser", Hint: "or copy the link over SSH"},
		{ID: "description", Title: "Read Description", Hint: "the whole text, in a read-only page"},
	}
	if d.State != "OPEN" {
		return items
	}
	items = append(items,
		picker.Item{ID: "checkout", Title: "Check Out Branch", Hint: d.Branch},
		picker.Item{ID: "approve", Title: "Approve…"},
		picker.Item{ID: "request-changes", Title: "Request Changes…"},
		picker.Item{ID: "comment", Title: "Comment…"},
	)
	if d.Draft {
		items = append(items, picker.Item{ID: "ready", Title: "Mark Ready for Review"})
	} else {
		items = append(items, picker.Item{ID: "draft", Title: "Convert to Draft"})
	}
	items = append(items, picker.Item{ID: "merge", Title: "Merge…", Hint: "squash, merge or rebase"})
	return items
}

// prOpenActions opens the actions picker for the pull request in the view.
func (m *Model) prOpenActions() tea.Cmd {
	d := m.prS().detail
	if d == nil {
		return nil
	}
	m.confirmOpen = false
	m.picker = picker.NewItems(" "+prRef(d.Number)+" Actions ", prActionItems(*d))
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindPRActions
	return nil
}

// onPRActionPicked runs a row of the actions picker.
func (m *Model) onPRActionPicked(id string) tea.Cmd {
	d := m.prS().detail
	if d == nil {
		return nil
	}
	switch id {
	case "browser":
		return m.openLink(d.URL)
	case "description":
		return m.prReadDescription()
	case "checkout":
		return m.prCheckout()
	}
	return nil
}

// prReadDescription opens the whole description as a read-only page.
func (m *Model) prReadDescription() tea.Cmd {
	d := m.prS().detail
	body := strings.TrimSpace(d.Body)
	if body == "" {
		return m.aiInfo(prRef(d.Number) + " has no description")
	}
	m.focus = FocusEditor
	m.ensureEditorWindowCurrent()
	page := "# " + d.Title + "\n\n" + body + "\n"
	if err := m.openReadOnlyPage("PR "+prRef(d.Number)+".md", page, "", "markdown"); err != nil {
		m.err = "Pull request: " + err.Error()
	}
	return nil
}

// prCheckout switches to the branch of the pull request. It refuses when
// tracked files have changes: gh would fail or take them along.
func (m *Model) prCheckout() tea.Cmd {
	d := m.prS().detail
	dir, err := m.rootDir()
	if d == nil || err != nil {
		return nil
	}
	dirty, err := git.Dirty(dir)
	if err != nil {
		return m.prFailToast("Could not read the git state", err)
	}
	if dirty {
		return m.prInfo("Not checked out: you have changes", "Commit, stash or discard them first, then check out "+prRef(d.Number)+" again.")
	}
	n, branch := d.Number, d.Branch
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		err := ghci.Checkout(ctx, run, dir, n)
		return prDoneMsg{number: n, ok: "Checked out " + prRef(n), detail: branch, failed: "Could not check out " + prRef(n), err: err, refresh: true}
	})
}
