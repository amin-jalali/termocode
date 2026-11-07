package app

import (
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
)

// Execution modes (DBX-25): what the Run key sends from a SQL buffer.
// db_exec_mode picks the default (largest: the statement at the caret);
// the palette has Run From Caret, Run Smallest Statement and Run… (ask),
// and Choose Execution Mode saves the default. db_selection_split says
// whether a selection is split into statements or sent as one text.

const (
	pickerKindDBExecMode pickerKindEnum = 4300 + iota
	pickerKindDBExecAsk
)

func (s dbConsoleSettings) execMode() string {
	if s.ExecMode != nil {
		if m, ok := dbclient.ParseExecMode(*s.ExecMode); ok && (m <= dbclient.ExecFromCaret || m == dbclient.ExecAsk) {
			return m.String()
		}
	}
	return dbclient.ExecLargest.String()
}

func (s dbConsoleSettings) selectionSplit() bool {
	return s.SelectionSplit == nil || *s.SelectionSplit
}

// dbCursorOffset is the byte offset of (line 1-based, col 0-based bytes).
func dbCursorOffset(lines []string, line, col int) int {
	off := 0
	for i := 0; i < line-1 && i < len(lines); i++ {
		off += len(lines[i]) + 1
	}
	if line-1 < len(lines) {
		col = min(max(col, 0), len(lines[line-1]))
	} else {
		col = 0
	}
	return off + col
}

func dbLineAt(text string, off int) int {
	off = min(max(off, 0), len(text))
	return strings.Count(text[:off], "\n") + 1
}

// dbPickByMode fills r.statements for the smallest and from-caret modes;
// false leaves the default (the whole statement at the caret).
func dbPickByMode(r *dbRun, driver string) bool {
	cursor := r.cursor - 1
	switch r.mode {
	case dbclient.ExecSmallest.String():
		c := dbclient.ExecCandidates(r.sql, driver, cursor)
		if len(c) == 0 {
			return false
		}
		r.statements = []string{c[0].SQL}
		r.line = dbLineAt(r.sql, c[0].Start)
		return true
	case dbclient.ExecFromCaret.String():
		spans := dbclient.StatementSpans(r.sql, driver)
		if len(spans) == 0 {
			return false
		}
		i := 0
		for k := range spans {
			if spans[k].Start <= cursor {
				i = k
			}
		}
		r.statements = nil
		for _, sp := range spans[i:] {
			r.statements = append(r.statements, sp.SQL)
		}
		r.line = dbLineAt(r.sql, spans[i].Start)
		return true
	}
	return false
}

// databaseExecAsk lists what can run at the caret: each subquery around
// it (innermost first), the whole statement, then it and all after it.
func (m *Model) databaseExecAsk(r *dbRun, driver string) tea.Cmd {
	cands := dbclient.ExecCandidates(r.sql, driver, r.cursor-1)
	if len(cands) == 0 {
		return m.pushToastDetail(toast.Info, "No SQL at the cursor", "")
	}
	s := m.dbConsole()
	s.pending = r
	s.askCands = cands
	var items []picker.Item
	for i, c := range cands {
		hint := "subquery"
		if i == len(cands)-1 {
			hint = "statement"
		}
		items = append(items, picker.Item{ID: fmt.Sprint(i), Title: sanitizeDBText(dbExecLabel(c.SQL, 70)), Hint: hint})
	}
	if spans := dbclient.StatementSpans(r.sql, driver); len(spans) > 1 {
		items = append(items, picker.Item{ID: "rest", Title: "This statement and every one after it", Hint: "from caret"})
	}
	m.picker = picker.NewItems("Run what?", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBExecAsk
	m.pickerOpen = true
	return nil
}

func (m *Model) databaseExecAskPick(id string) tea.Cmd {
	s := m.dbConsole()
	r, cands := s.pending, s.askCands
	s.pending, s.askCands = nil, nil
	if r == nil {
		return nil
	}
	c, ok := m.databases.connection(r.conn)
	if !ok {
		return nil
	}
	if id == "rest" {
		r.mode = dbclient.ExecFromCaret.String()
		if !dbPickByMode(r, c.Driver) {
			return nil
		}
		return m.databasePrepareTail(r)
	}
	var i int
	if _, err := fmt.Sscan(id, &i); err != nil || i < 0 || i >= len(cands) {
		return nil
	}
	r.statements = []string{cands[i].SQL}
	r.line = dbLineAt(r.sql, cands[i].Start)
	return m.databasePrepareTail(r)
}

// dbExecLabel folds sql to one line of at most n runes.
func dbExecLabel(sql string, n int) string {
	t := dbOneLine(sql)
	if rs := []rune(t); len(rs) > n {
		return string(rs[:n-1]) + "…"
	}
	return t
}

var dbExecModeItems = []picker.Item{
	{ID: "largest", Title: "Whole statement at the caret (largest)"},
	{ID: "smallest", Title: "Smallest statement at the caret, a subquery too (smallest)"},
	{ID: "from-caret", Title: "Statement at the caret and all after it (from caret)"},
	{ID: "ask", Title: "Ask what to run each time (ask)"},
	{ID: "selection-split", Title: "Selection: split into statements"},
	{ID: "selection-one", Title: "Selection: send as one text"},
}

// databaseExecModePicker is Databases: Choose Execution Mode….
func (m *Model) databaseExecModePicker() tea.Cmd {
	set := loadDBConsoleSettings()
	items := append([]picker.Item(nil), dbExecModeItems...)
	for i := range items {
		switch items[i].ID {
		case set.execMode():
			items[i].Hint = "current"
		case "selection-split", "selection-one":
			if (items[i].ID == "selection-split") == set.selectionSplit() {
				items[i].Hint = "current"
			}
		}
	}
	m.picker = picker.NewItems("Run key runs…", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBExecMode
	m.pickerOpen = true
	return nil
}

func (m *Model) databaseExecModeSet(id string) tea.Cmd {
	path, err := settingsConfigPath()
	if err != nil {
		return m.dbConsoleError(err)
	}
	var title string
	err = cfgfile.Update(path, func(c map[string]any) error {
		switch id {
		case "selection-split", "selection-one":
			c["db_selection_split"] = id == "selection-split"
		default:
			if _, ok := dbclient.ParseExecMode(id); !ok {
				return fmt.Errorf("unknown execution mode %q", id)
			}
			c["db_exec_mode"] = id
		}
		return nil
	})
	if err != nil {
		return m.dbConsoleError(err)
	}
	for _, it := range dbExecModeItems {
		if it.ID == id {
			title = it.Title
		}
	}
	return m.pushToastDetail(toast.Info, "Run key: "+title, "Saved in config.json.")
}
