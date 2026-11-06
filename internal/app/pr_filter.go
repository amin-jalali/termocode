package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// GITX-23: Git: Pull Requests (Filter)… (the common lists and a GitHub
// search) and Git: My Pull Requests… (a launchpad: what asks for my
// review, then my own). Both only read; the rows open the PR view.

const (
	pickerKindPRFilter pickerKindEnum = 3213
	promptKindPRSearch promptKindEnum = 3213
)

// prFilterChoice is one row of the filter picker.
type prFilterChoice struct {
	id, title, hint string
	filter          ghci.PRFilter
}

var prFilterChoices = []prFilterChoice{
	{"open", "Open", "every open pull request", ghci.PRFilter{}},
	{"review", "Review Requested from Me", "open, waiting for your review", ghci.PRFilter{Search: "review-requested:@me"}},
	{"mine", "Created by Me", "open", ghci.PRFilter{Author: "@me"}},
	{"assigned", "Assigned to Me", "open", ghci.PRFilter{Assignee: "@me"}},
	{"drafts", "Drafts", "open drafts", ghci.PRFilter{Draft: true}},
	{"closed", "Closed", "closed without a merge", ghci.PRFilter{State: "closed"}},
	{"merged", "Merged", "", ghci.PRFilter{State: "merged"}},
	{"all", "All", "open, closed and merged", ghci.PRFilter{State: "all"}},
}

// openPRFilter shows the filter picker.
func (m *Model) openPRFilter() tea.Cmd {
	m.gitEnsureLoaded()
	if !m.gitIsRepo {
		return m.aiInfo("Pull requests need a folder with a git repository")
	}
	items := make([]picker.Item, 0, len(prFilterChoices)+1)
	for _, c := range prFilterChoices {
		items = append(items, picker.Item{ID: c.id, Title: c.title, Hint: c.hint})
	}
	items = append(items, picker.Item{ID: "search", Title: "Search…", Hint: "GitHub search words, for example label:bug"})
	m.picker = picker.NewItems(" Pull Requests: Filter ", items)
	m.picker.SetWidth(min(m.w-4, 80))
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindPRFilter
	return nil
}

// onPRFilterPicked reads the chosen list.
func (m *Model) onPRFilterPicked(id string) tea.Cmd {
	if id == "search" {
		m.prompt = prompt.New("Search Pull Requests", "Search (open, closed and merged):", "")
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindPRSearch
		return nil
	}
	for _, c := range prFilterChoices {
		if c.id == id {
			return m.prFilterCmd(c.filter, " "+c.title+" Pull Requests ")
		}
	}
	return nil
}

// handlePRSearchSubmit runs a search. ok=false: not ours.
func (m *Model) handlePRSearchSubmit(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindPRSearch {
		return nil, false
	}
	q := strings.TrimSpace(value)
	if q == "" {
		return nil, true
	}
	return m.prFilterCmd(ghci.PRFilter{State: "all", Search: q}, " Pull Requests: "+truncRunes(q, 40)+" "), true
}

// prFilterCmd reads one filtered list.
func (m *Model) prFilterCmd(f ghci.PRFilter, title string) tea.Cmd {
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		items, err := ghci.ListPRsFilter(ctx, run, dir, f)
		return prListMsg{items: items, err: err, title: title, empty: "No pull requests match"}
	})
}

// openPRMine reads the launchpad; the picker opens when gh answers.
func (m *Model) openPRMine() tea.Cmd {
	fetch := m.prMineCmd()
	if fetch == nil {
		return m.aiInfo("Pull requests need a folder with a git repository")
	}
	return tea.Batch(m.aiInfo("Reading your pull requests…"), fetch)
}

// prMineCmd runs both lists of the launchpad off the main loop.
func (m *Model) prMineCmd() tea.Cmd {
	m.gitEnsureLoaded()
	if !m.gitIsRepo {
		return nil
	}
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		review, mine, err := ghci.MyPRs(ctx, run, dir)
		return prListMsg{err: err, mine: true, review: review, items: mine}
	})
}

// prMineItems are the launchpad rows: review requests first, then mine
// (a PR in both shows once, as a review request).
func prMineItems(review, mine []ghci.PRItem) []picker.Item {
	var items []picker.Item
	seen := map[int]bool{}
	add := func(p ghci.PRItem, group string) {
		if seen[p.Number] {
			return
		}
		seen[p.Number] = true
		hint := group
		if h := prRowHint(p); h != "" {
			hint += " · " + h
		}
		items = append(items, picker.Item{ID: "mine-" + prRef(p.Number), Title: prRowTitle(p), Hint: hint})
	}
	for _, p := range review {
		add(p, "Needs your review")
	}
	for _, p := range mine {
		add(p, "Yours")
	}
	return items
}
