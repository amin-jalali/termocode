package app

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/picker"
)

const pickerKindPRList pickerKindEnum = 3200

func prPaletteItems() []picker.Item {
	return append([]picker.Item{
		{ID: "git-pr-list", Title: "Git: Pull Requests…"},
		{ID: "git-pr-filter", Title: "Git: Pull Requests (Filter)…"}, // pr_filter.go
		{ID: "git-pr-mine", Title: "Git: My Pull Requests…"},
	}, prCreatePaletteItems()...)
}

// dispatchPRPalette runs the PR palette ids.
func (m *Model) dispatchPRPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "git-pr-list":
		return m.openPRList(), true
	case "git-pr-create":
		return m.prCreateCmd(), true
	case "git-pr-filter":
		return m.openPRFilter(), true
	case "git-pr-mine":
		return m.openPRMine(), true
	}
	return nil, false
}

// prListMsg is a finished `gh pr list`.
type prListMsg struct {
	items []ghci.PRItem
	err   error
	title string // the picker title ("" : Pull Requests)
	empty string // the toast of an empty list ("": No open pull requests)
	// mine: the launchpad (pr_filter.go): review first, then items
	mine   bool
	review []ghci.PRItem
}

// openPRList asks gh for the open pull requests; the picker opens when
// the answer comes.
func (m *Model) openPRList() tea.Cmd {
	fetch := m.prListCmd()
	if fetch == nil {
		return nil
	}
	return tea.Batch(m.aiInfo("Reading pull requests…"), fetch)
}

// prListCmd runs `gh pr list` off the main loop; nil (with a toast
// pending in the caller's model) outside a git repository.
func (m *Model) prListCmd() tea.Cmd {
	m.gitEnsureLoaded()
	if !m.gitIsRepo {
		return nil
	}
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		items, err := ghci.ListPRs(ctx, run, dir)
		return prListMsg{items: items, err: err}
	})
}

// onPRList opens the picker with the answer.
func (m *Model) onPRList(msg prListMsg) tea.Cmd {
	if msg.err != nil {
		return m.prFailToast("Could not read pull requests", msg.err)
	}
	s := m.prS()
	s.items = msg.items
	title, empty := msg.title, msg.empty
	if title == "" {
		title = " Pull Requests "
	}
	if empty == "" {
		empty = "No open pull requests"
	}
	var items []picker.Item
	if msg.mine {
		title, empty = " My Pull Requests ", "Nothing waits for you: no pull requests of yours and none ask for your review"
		items = prMineItems(msg.review, msg.items)
	} else {
		for _, p := range msg.items {
			items = append(items, picker.Item{ID: prRef(p.Number), Title: prRowTitle(p), Hint: prRowHint(p)})
		}
	}
	if len(items) == 0 {
		return m.aiInfo(empty)
	}
	m.picker = picker.NewItems(title, items)
	m.picker.SetNote([]string{"Filter by number, title, author or branch · Enter opens"})
	m.picker.SetWidth(min(m.w-4, 110)) // the row shows title, author and branch
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindPRList
	return nil
}

// prRowTitle is what the filter searches: number, title, author, branch.
func prRowTitle(p ghci.PRItem) string {
	t := prRef(p.Number) + " " + p.Title
	if p.Author != "" {
		t += " · " + p.Author
	}
	if p.Branch != "" {
		t += " · " + p.Branch
	}
	return t
}

// prRowHint is the state: draft, checks and review.
func prRowHint(p ghci.PRItem) string {
	var parts []string
	switch p.State {
	case "MERGED":
		parts = append(parts, "Merged")
	case "CLOSED":
		parts = append(parts, "Closed")
	}
	if p.Draft {
		parts = append(parts, "Draft")
	}
	if len(p.Checks) > 0 {
		parts = append(parts, ciMark(p.ChecksState())+" checks")
	}
	if r := ciReviewText(p.Review); r != "" {
		parts = append(parts, r)
	}
	return strings.Join(parts, " · ")
}

// handlePRPickerSelect answers the PR pickers. ok=false: not ours.
func (m *Model) handlePRPickerSelect(id string) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindPRList:
		return m.onPRListPicked(id), true
	case pickerKindPRActions: // pr_actions.go
		return m.onPRActionPicked(id), true
	case pickerKindPRThreads: // pr_threads.go
		return m.onPRThreadPicked(id), true
	case pickerKindPRChecks: // worktrees_pr.go
		return m.onPRCheckPicked(id), true
	case pickerKindPRFilter: // pr_filter.go
		return m.onPRFilterPicked(id), true
	}
	return nil, false
}

// onPRListPicked opens the view of the chosen pull request (pr_view.go).
func (m *Model) onPRListPicked(id string) tea.Cmd {
	n := prNumber(strings.TrimPrefix(id, "mine-"))
	if n == 0 {
		return nil
	}
	return m.openPRView(n)
}

// prNumber reads "#12" (0 when it is not one).
func prNumber(id string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "#"))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
