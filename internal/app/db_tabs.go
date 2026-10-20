package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// Result tabs (plan F5, §11.1 #8): every result lives in a tab; a new run
// replaces the tabs that are neither pinned (p) nor holding pending edits;
// a multi-statement run gets one tab per statement that returns rows. The
// keys 1…9 switch tabs. Paging, sorting, a rerun or the refresh after a
// submit replace the result in its own tab.

// dbMaxTabs is the most tabs kept (the keys reach 1…9).
const dbMaxTabs = 9

// dbTab is one result tab and the page state it had when it was left.
type dbTab struct {
	run     *dbRun
	pinned  bool
	view    int
	page    int
	size    int
	filter  string
	sortCol int
	desc    bool
	trans   bool
	trow    int
	edits   *dbEdits      // pending changes (nil: none)
	target  *dbEditTarget // where changes go, once known
	notes   []string      // what the last submit did (Messages)
}

func (t *dbTab) dirty() bool { return t != nil && t.edits.count() > 0 }

// curTab is the tab on the page (nil before the first result).
func (s *dbConsoleState) curTab() *dbTab {
	if s.tab >= 0 && s.tab < len(s.tabs) {
		return s.tabs[s.tab]
	}
	return nil
}

// saveTab keeps the page state in the current tab.
func (s *dbConsoleState) saveTab() {
	t := s.curTab()
	if t == nil {
		return
	}
	t.run = s.shown
	t.view, t.page, t.size, t.filter = s.view, s.page, s.size, s.filter
	t.sortCol, t.desc, t.trans, t.trow = s.sortCol, s.desc, s.trans, s.trow
}

// useTab makes tab i the page's tab.
func (s *dbConsoleState) useTab(i int) {
	if i < 0 || i >= len(s.tabs) {
		return
	}
	s.saveTab()
	s.tab = i
	t := s.tabs[i]
	s.shown = t.run
	s.view, s.page, s.size, s.filter = t.view, t.page, t.size, t.filter
	s.sortCol, s.desc, s.trans, s.trow = t.sortCol, t.desc, t.trans, t.trow
	s.cache = nil
}

func (s *dbConsoleState) tabIndex(t *dbTab) int {
	for i, x := range s.tabs {
		if x == t {
			return i
		}
	}
	return -1
}

// placeRun puts r on the page: in its own tab when r.tab names one (a
// page, a sort, a rerun of that tab), after the previous statement of a
// multi-statement run, or in a new tab that replaces the tabs that are
// neither pinned nor edited. The caller sets the page state afterwards.
func (s *dbConsoleState) placeRun(r *dbRun) {
	s.saveTab()
	if i := s.tabIndex(r.tab); i >= 0 {
		t := s.tabs[i]
		t.run, t.edits = r, nil
		if t.target != nil && t.target.loading {
			t.target = nil
		}
		if t.target != nil && t.run.tv == nil {
			t.target = nil // a console result looks its key up again
		}
		s.tab = i
		s.shown = r
		s.cache = nil
		return
	}
	if r.index > 0 {
		// The previous statement of this run: a statement without rows
		// gives its tab to the next one.
		if t := s.curTab(); t != nil && t.run != nil && t.run.batch == r.batch && r.batch != 0 && !t.pinned && t.run.result.Err == nil && len(t.run.result.Columns) == 0 {
			t.run, t.edits, t.target = r, nil, nil
			r.tab = t
			s.shown = r
			s.cache = nil
			return
		}
	} else {
		kept := s.tabs[:0]
		for _, t := range s.tabs {
			if t.pinned || t.dirty() {
				kept = append(kept, t)
			}
		}
		s.tabs = kept
	}
	for len(s.tabs) >= dbMaxTabs {
		drop := -1
		for i, t := range s.tabs {
			if !t.dirty() {
				drop = i
				break
			}
		}
		if drop < 0 {
			break
		}
		s.tabs = append(s.tabs[:drop], s.tabs[drop+1:]...)
	}
	t := &dbTab{run: r, sortCol: -1}
	r.tab = t
	s.tabs = append(s.tabs, t)
	s.tab = len(s.tabs) - 1
	s.shown = r
	s.cache = nil
}

// dbTabStrip is the tab list for row 1 ("1 [2] 3p"): the current tab in
// brackets, a pinned one with p, an edited one with *. Empty with one tab.
func (s *dbConsoleState) tabStrip() string {
	if len(s.tabs) < 2 {
		return ""
	}
	parts := make([]string, len(s.tabs))
	for i, t := range s.tabs {
		label := fmt.Sprint(i + 1)
		if t.pinned {
			label += "p"
		}
		if t.dirty() {
			label += "*"
		}
		if i == s.tab {
			label = "[" + label + "]"
		}
		parts[i] = label
	}
	return strings.Join(parts, " ")
}

// databaseSwitchTab shows tab n (1-based).
func (m *Model) databaseSwitchTab(n int) tea.Cmd {
	s := m.dbConsole()
	if n < 1 || n > len(s.tabs) || n-1 == s.tab {
		return nil
	}
	s.useTab(n - 1)
	return m.databaseShowPage()
}

// databasePinTab pins the current tab (again: unpins it).
func (m *Model) databasePinTab() tea.Cmd {
	s := m.dbConsole()
	t := s.curTab()
	if t == nil {
		return nil
	}
	t.pinned = !t.pinned
	cmd := m.databaseShowPage()
	if t.pinned {
		return tea.Batch(cmd, m.pushToastDetail(toast.Info, "Result pinned", "The next run opens a new tab. p again unpins it."))
	}
	return tea.Batch(cmd, m.pushToast(toast.Info, "Result unpinned"))
}
