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
	return []picker.Item{
		{ID: "git-pr-list", Title: "Git: Pull Requests…"},
	}
}

// dispatchPRPalette runs the PR palette ids.
func (m *Model) dispatchPRPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "git-pr-list":
		return m.openPRList(), true
	}
	return nil, false
}

// prListMsg is a finished `gh pr list`.
type prListMsg struct {
	items []ghci.PRItem
	err   error
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
	if len(msg.items) == 0 {
		return m.aiInfo("No open pull requests")
	}
	items := make([]picker.Item, 0, len(msg.items))
	for _, p := range msg.items {
		items = append(items, picker.Item{ID: prRef(p.Number), Title: prRowTitle(p), Hint: prRowHint(p)})
	}
	m.picker = picker.NewItems(" Pull Requests ", items)
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
	}
	return nil, false
}

// onPRListPicked opens the view of the chosen pull request (pr_view.go).
func (m *Model) onPRListPicked(id string) tea.Cmd {
	n := prNumber(id)
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
