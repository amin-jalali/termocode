package app

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The filter builder of a table's data page (DBX-9). b opens a menu next
// to the raw WHERE text (f): add a condition from a column, an operator
// and a value (AND or OR with the filter already set), sort by a column,
// edit the text, or clear it. Names are quoted for the connection and
// values are SQL string literals, so the built text passes the same
// dbWhereCheck as typed text and is sent the same way.

const (
	pickerKindDBWhereMenu pickerKindEnum = 5139 + iota
	pickerKindDBWhereCol
	pickerKindDBWhereOp
	pickerKindDBWhereSortCol
	pickerKindDBWhereSortDir
)

const promptKindDBWhereValue promptKindEnum = 5139

// dbWhereOps are the operators the builder offers, in menu order.
var dbWhereOps = []struct{ op, hint string }{
	{"=", "equals"},
	{"<>", "is not"},
	{"<", "less than"},
	{"<=", "at most"},
	{">", "greater than"},
	{">=", "at least"},
	{"LIKE", "matches a pattern (% any text, _ one character)"},
	{"NOT LIKE", "does not match a pattern"},
	{"IN", "one of a comma separated list"},
	{"IS NULL", "has no value"},
	{"IS NOT NULL", "has a value"},
}

// dbWhereDraft is the condition being built.
type dbWhereDraft struct {
	tv     *dbTableView
	or     bool
	col    string
	op     string
	sortBy string
}

// dbWhereLiteral is v as a string literal for driver d.
func dbWhereLiteral(d dbclient.Dialect, v string) string {
	if _, ok := d.(dbclient.MySQLDialect); ok {
		v = strings.ReplaceAll(v, `\`, `\\`)
	}
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

// dbBuildCondition is "col op value" for driver, combined with old (AND,
// or OR when or is set). The value is ignored for IS NULL / IS NOT NULL;
// IN takes a comma separated list.
func dbBuildCondition(driver, col, op, value, old string, or bool) (string, error) {
	d := dbclient.DialectFor(driver)
	if d == nil {
		return "", fmt.Errorf("%s has no SQL filter", driver)
	}
	if strings.TrimSpace(col) == "" {
		return "", errors.New("pick a column")
	}
	if err := dbWhereCheck(driver, old); err != nil {
		return "", err
	}
	known := false
	for _, o := range dbWhereOps {
		known = known || o.op == op
	}
	if !known {
		return "", fmt.Errorf("unknown operator %s", op)
	}
	cond := d.QuoteIdent(col) + " " + op
	switch op {
	case "IS NULL", "IS NOT NULL":
	case "IN":
		var items []string
		for _, p := range strings.Split(value, ",") {
			if p = strings.TrimSpace(p); p != "" {
				items = append(items, dbWhereLiteral(d, p))
			}
		}
		if len(items) == 0 {
			return "", errors.New("IN needs at least one value")
		}
		cond += " (" + strings.Join(items, ", ") + ")"
	default:
		cond += " " + dbWhereLiteral(d, value)
	}
	if old = strings.TrimSpace(old); old != "" {
		join := " AND "
		if or {
			join = " OR "
		}
		cond = "(" + old + ")" + join + cond
	}
	if err := dbWhereCheck(driver, cond); err != nil {
		return "", err
	}
	return cond, nil
}

// databaseWhereBuilder is b on the result page.
func (m *Model) databaseWhereBuilder() tea.Cmd {
	s := m.dbConsole()
	r := s.shown
	if r == nil || r.tv == nil || r.plan != nil {
		return m.pushToastDetail(toast.Info, "The filter builder works in table data", "Open a table with o; f filters any result by text.")
	}
	tv := r.tv
	s.whereDraft = &dbWhereDraft{tv: tv}
	title := "Filter and sort"
	if tv.where != "" {
		title += ": WHERE " + dbOneLine(tv.where)
	}
	items := []picker.Item{{ID: "and", Title: "Add a condition", Hint: "AND"}}
	if tv.where != "" {
		items = append(items, picker.Item{ID: "or", Title: "Add an alternative condition", Hint: "OR"})
	}
	items = append(items, picker.Item{ID: "sort", Title: "Sort by a column"}, picker.Item{ID: "edit", Title: "Edit the WHERE text", Hint: "f"})
	if tv.where != "" {
		items = append(items, picker.Item{ID: "clear", Title: "Clear the filter"})
	}
	m.dbOpenPicker(pickerKindDBWhereMenu, title, items)
	return nil
}

// dbWhereColumns is the columns the builder offers: the table's, else the
// result's (the hidden rowid left out).
func dbWhereColumns(r *dbRun) []string {
	if r.tv != nil && len(r.tv.cols) > 0 {
		return r.tv.cols
	}
	var out []string
	for _, c := range r.result.Columns {
		if c.Name != dbRowidAlias {
			out = append(out, c.Name)
		}
	}
	return out
}

func (m *Model) dbWhereColumnPicker(kind pickerKindEnum, title string) {
	s := m.dbConsole()
	var items []picker.Item
	for i, c := range dbWhereColumns(s.shown) {
		hint := ""
		if tv := s.whereDraft.tv; kind == pickerKindDBWhereSortCol && c == tv.orderCol {
			hint = "ascending"
			if tv.desc {
				hint = "descending"
			}
		}
		items = append(items, picker.Item{ID: fmt.Sprint(i), Title: dbOneLine(c), Hint: hint})
	}
	m.dbOpenPicker(kind, title, items)
}

// databaseWherePicker handles the builder's pickers.
func (m *Model) databaseWherePicker(id string) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindDBWhereMenu, pickerKindDBWhereCol, pickerKindDBWhereOp, pickerKindDBWhereSortCol, pickerKindDBWhereSortDir:
	default:
		return nil, false
	}
	s := m.dbConsole()
	dr := s.whereDraft
	if dr == nil || s.shown == nil || s.shown.tv != dr.tv {
		s.whereDraft = nil
		return nil, true
	}
	tv := dr.tv
	col := func() (string, bool) {
		var i int
		cols := dbWhereColumns(s.shown)
		if _, err := fmt.Sscan(id, &i); err != nil || i < 0 || i >= len(cols) {
			return "", false
		}
		return cols[i], true
	}
	switch m.pickerKind {
	case pickerKindDBWhereMenu:
		switch id {
		case "and", "or":
			dr.or = id == "or"
			m.dbWhereColumnPicker(pickerKindDBWhereCol, "Condition: column")
		case "sort":
			m.dbWhereColumnPicker(pickerKindDBWhereSortCol, "Sort by")
		case "edit":
			s.whereDraft = nil
			cmd := m.openHTTPPrompt(promptKindDBResultFilter, "Filter rows", "WHERE condition (empty: all rows):", tv.where)
			m.prompt.SetAllowEmpty(true)
			return cmd, true
		case "clear":
			s.whereDraft = nil
			return m.databaseUnlessDirty(func() tea.Cmd { return m.databaseTableWhere(tv, "") }), true
		}
		return nil, true
	case pickerKindDBWhereCol:
		c, ok := col()
		if !ok {
			return nil, true
		}
		dr.col = c
		items := make([]picker.Item, len(dbWhereOps))
		for i, o := range dbWhereOps {
			items[i] = picker.Item{ID: o.op, Title: dbOneLine(c) + " " + o.op, Hint: o.hint}
		}
		m.dbOpenPicker(pickerKindDBWhereOp, "Condition: operator", items)
		return nil, true
	case pickerKindDBWhereOp:
		dr.op = id
		if id == "IS NULL" || id == "IS NOT NULL" {
			return m.databaseWhereApply(""), true
		}
		label := "Value:"
		switch id {
		case "IN":
			label = "Values, comma separated:"
		case "LIKE", "NOT LIKE":
			label = "Pattern (% any text, _ one character):"
		}
		return m.openHTTPPrompt(promptKindDBWhereValue, dbOneLine(dr.col)+" "+id, label, ""), true
	case pickerKindDBWhereSortCol:
		c, ok := col()
		if !ok {
			return nil, true
		}
		dr.sortBy = c
		m.dbOpenPicker(pickerKindDBWhereSortDir, "Sort by "+dbOneLine(c), []picker.Item{{ID: "asc", Title: "Ascending", Hint: "A → Z, 0 → 9"}, {ID: "desc", Title: "Descending", Hint: "Z → A, 9 → 0"}})
		return nil, true
	default: // pickerKindDBWhereSortDir
		s.whereDraft = nil
		by, desc := dr.sortBy, id == "desc"
		return m.databaseUnlessDirty(func() tea.Cmd {
			tv.orderCol, tv.desc, tv.offset = by, desc, 0
			return m.databaseRunTable(tv)
		}), true
	}
}

// databaseWhereApply finishes the condition with value and runs the view.
func (m *Model) databaseWhereApply(value string) tea.Cmd {
	s := m.dbConsole()
	dr := s.whereDraft
	s.whereDraft = nil
	if dr == nil || s.shown == nil || s.shown.tv != dr.tv {
		return nil
	}
	tv := dr.tv
	where, err := dbBuildCondition(tv.driver, dr.col, dr.op, value, tv.where, dr.or)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Filter refused", sanitizeDBText(err.Error()))
	}
	return m.databaseUnlessDirty(func() tea.Cmd { return m.databaseTableWhere(tv, where) })
}
