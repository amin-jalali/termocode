package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
)

// Result page conveniences (DBX-26): page size changed live, go to row N,
// load all rows, a new tab per run (db_result_new_tab) and one-row
// results shown transposed (db_auto_transpose).

const pickerKindDBPageSize pickerKindEnum = 4310
const promptKindDBGotoRow promptKindEnum = 4310

// dbLoadAllRows is the row limit of Load All Rows (db_max_rows' ceiling).
const dbLoadAllRows = 100000

func (s dbConsoleSettings) resultNewTab() bool {
	return s.ResultNewTab != nil && *s.ResultNewTab
}

var dbPageSizes = []int{25, 50, 100, 250, 500, 1000}

// databaseResultReady is the shown console result, or a toast why not.
func (m *Model) databaseResultReady() (*dbConsoleState, tea.Cmd, bool) {
	s := m.dbConsole()
	switch {
	case s.shown == nil:
		return s, m.pushToastDetail(toast.Info, "No result yet", "Run a statement first."), false
	case s.running != nil:
		return s, m.pushToastDetail(toast.Warn, "A query is running", "Esc cancels it."), false
	case s.shown.result.Err != nil || len(s.shown.result.Columns) == 0 || s.shown.plan != nil || s.shown.redis != nil:
		return s, m.pushToastDetail(toast.Info, "No rows on the page", "Run a query that returns rows."), false
	}
	return s, nil, true
}

func (m *Model) databasePageSizePicker() tea.Cmd {
	s, cmd, ok := m.databaseResultReady()
	if !ok {
		return cmd
	}
	items := make([]picker.Item, 0, len(dbPageSizes))
	for _, n := range dbPageSizes {
		it := picker.Item{ID: strconv.Itoa(n), Title: fmt.Sprintf("%d rows", n)}
		if n == s.pageSize() {
			it.Hint = "current"
		}
		items = append(items, it)
	}
	m.picker = picker.NewItems("Rows per page", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBPageSize
	m.pickerOpen = true
	return nil
}

func (m *Model) databaseSetPageSize(id string) tea.Cmd {
	n, err := strconv.Atoi(id)
	if err != nil || n < 1 {
		return nil
	}
	s, cmd, ok := m.databaseResultReady()
	if !ok {
		return cmd
	}
	if s.shown.tv != nil {
		return m.pushToastDetail(toast.Info, "Table data pages on the server", "Set db_page_rows in config.json for table data.")
	}
	first := s.page * s.pageSize() // keep the first row of the page in view
	s.size = min(n, 1000)
	s.page = first / s.size
	s.saveTab()
	return m.databaseShowPage()
}

func (m *Model) databaseGotoRowPrompt() tea.Cmd {
	s, cmd, ok := m.databaseResultReady()
	if !ok {
		return cmd
	}
	if s.shown.tv != nil {
		return m.pushToastDetail(toast.Info, "Not for table data", "Table data pages on the server; use ] and [.")
	}
	return m.openHTTPPrompt(promptKindDBGotoRow, "Go to row", fmt.Sprintf("Row number (1–%d):", len(s.displayedResult().Rows)), "")
}

func (m *Model) databaseGotoRow(value string) tea.Cmd {
	s, cmd, ok := m.databaseResultReady()
	if !ok {
		return cmd
	}
	total := len(s.displayedResult().Rows)
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 || n > total {
		return m.pushToastDetail(toast.Warn, "No such row", fmt.Sprintf("Type a number from 1 to %d.", total))
	}
	size := s.pageSize()
	s.trans = false
	s.page = (n - 1) / size
	s.gotoRow = (n-1)%size + 1
	return m.databaseShowPage()
}

// databaseLoadAll runs the shown console result again without the
// db_max_rows cut (up to dbLoadAllRows).
func (m *Model) databaseLoadAll() tea.Cmd {
	s, cmd, ok := m.databaseResultReady()
	if !ok {
		return cmd
	}
	old := s.shown
	if old.tv != nil {
		return m.pushToastDetail(toast.Info, "Table data pages on the server", "Use ] and [ to read more pages.")
	}
	if !old.result.Truncated && len(old.result.Rows) < loadDBConsoleSettings().maxRows() {
		return m.pushToastDetail(toast.Info, "All rows are loaded", dbPlural(len(old.result.Rows), "row")+".")
	}
	return m.databaseUnlessDirty(func() tea.Cmd {
		r := &dbRun{conn: old.conn, file: old.file, line: old.line, sql: old.sql, by: "user", statements: []string{old.sql}, sessKey: old.sessKey, tab: s.curTab(), maxRows: dbLoadAllRows}
		return m.databaseReviewRun(r)
	})
}
