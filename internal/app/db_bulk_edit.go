package app

import (
	"errors"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Bulk edit on the result page (DBX-11). V selects rows; then e sets one
// value in the column under the cursor for every selected row, + adds a
// copy of each and - marks them for deletion. P pastes tab-separated
// rows from the clipboard at the cursor cell. All of it stays a pending
// change like a single edit: u undoes, w shows the SQL card.

// dbPasteMaxRows is the most clipboard lines P takes at once.
const dbPasteMaxRows = 5000

// dbClipboardText reads the clipboard for P (tests swap it).
var dbClipboardText = readClipboardText

func init() {
	registerNotifyHandler("termocode_db_range", func(m *Model, args []any) tea.Cmd {
		return m.databaseRangeKey(notifyArgString(args, 0), notifyArgInt(args, 1), notifyArgInt(args, 2), notifyArgInt(args, 3))
	})
}

// dbSetCellValue keeps value as the pending value of the cell at pos (\N
// is NULL; on a new row an empty value from the prompt means the
// default). false: pos is not a row of t.
func dbSetCellValue(t *dbTab, pos dbEditPos, value string, fromBuffer bool) bool {
	if t == nil || t.run == nil {
		return false
	}
	if t.edits == nil {
		t.edits = newDBEdits()
	}
	t.edits.note = ""
	cell := dbclient.Cell{Text: value}
	if value == `\N` {
		cell = dbclient.Cell{Null: true}
	}
	if pos.added >= 0 {
		if pos.added >= len(t.edits.added) {
			return false
		}
		if value == "" && !fromBuffer {
			delete(t.edits.added[pos.added].set, pos.col)
		} else {
			t.edits.added[pos.added].set[pos.col] = cell
		}
		return true
	}
	if pos.orig < 0 || pos.orig >= len(t.run.result.Rows) {
		return false
	}
	ref := dbCellRef{pos.orig, pos.col}
	old := dbclient.Cell{Null: true}
	if row := t.run.result.Rows[pos.orig]; pos.col < len(row) {
		old = row[pos.col]
	}
	cell.Kind = old.Kind
	if old.Null == cell.Null && old.Text == cell.Text {
		delete(t.edits.cells, ref) // back to the loaded value
	} else {
		t.edits.cells[ref] = cell
	}
	return true
}

// dbToggleDeleteRows is - on a range: new rows are removed; loaded rows
// are all marked for deletion, or, when every one already is, all kept.
// n is the number of rows it touched.
func dbToggleDeleteRows(t *dbTab, poses []dbEditPos) (deleted bool, n int) {
	if t.edits == nil {
		t.edits = newDBEdits()
	}
	t.edits.note = ""
	var added []int
	all := true
	for _, p := range poses {
		if p.added >= 0 {
			added = append(added, p.added)
		} else if !t.edits.deleted[p.orig] {
			all = false
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(added)))
	for i, a := range added {
		if a < len(t.edits.added) && (i == 0 || a != added[i-1]) {
			t.edits.added = append(t.edits.added[:a], t.edits.added[a+1:]...)
			n++
		}
	}
	for _, p := range poses {
		if p.added >= 0 {
			continue
		}
		if all {
			delete(t.edits.deleted, p.orig)
		} else {
			t.edits.deleted[p.orig] = true
		}
		n++
	}
	return !all, n
}

// dbCloneRows is + on a range: a new row for each, with the values it
// shows now. Key columns get their default (a copy of a key would clash)
// and binary values are left out (they cannot be edited here).
func dbCloneRows(t *dbTab, tg *dbEditTarget, poses []dbEditPos) int {
	if t.edits == nil {
		t.edits = newDBEdits()
	}
	t.edits.note = ""
	isKey := map[int]bool{}
	for _, k := range tg.key {
		isKey[k] = true
	}
	var rows []*dbNewRow
	for _, p := range poses {
		if p.added >= len(t.edits.added) || p.added < 0 && (p.orig < 0 || p.orig >= len(t.run.result.Rows)) {
			continue
		}
		nr := &dbNewRow{set: map[int]dbclient.Cell{}}
		for col := range t.run.result.Columns {
			if isKey[col] || !tg.editable[col] {
				continue
			}
			c, ok := dbCellNow(t, dbEditPos{orig: p.orig, added: p.added, col: col})
			if !ok || c.Kind == "binary" {
				continue
			}
			nr.set[col] = dbclient.Cell{Text: c.Text, Null: c.Null}
		}
		rows = append(rows, nr)
	}
	t.edits.added = append(t.edits.added, rows...)
	return len(rows)
}

// dbParseTSV splits clipboard text into rows of tab-separated values
// (CRLF or LF; blank lines are left out).
func dbParseTSV(text string) ([][]string, error) {
	var rows [][]string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		rows = append(rows, strings.Split(line, "\t"))
		if len(rows) > dbPasteMaxRows {
			return nil, errors.New("more than 5000 rows")
		}
	}
	if len(rows) == 0 {
		return nil, errors.New("the clipboard holds no rows")
	}
	return rows, nil
}

// dbColumnBinary reports whether column col holds binary values.
func dbColumnBinary(t *dbTab, col int) bool {
	for _, row := range t.run.result.Rows {
		if col < len(row) && row[col].Kind == "binary" {
			return true
		}
	}
	return false
}

// dbPasteTSV pastes lines from startCol to the right: line i goes into
// targets[i], the lines after the last target become new rows. Values
// past the last column, in a column that cannot be edited, in a binary
// column or on a row marked for deletion are skipped. cells counts the
// values set on existing rows.
func dbPasteTSV(t *dbTab, tg *dbEditTarget, targets []dbEditPos, startCol int, lines [][]string) (cells, added, skipped int) {
	if t.edits == nil {
		t.edits = newDBEdits()
	}
	ncols := len(t.run.result.Columns)
	for i, fields := range lines {
		var pos dbEditPos
		if i < len(targets) {
			pos = targets[i]
			if pos.added < 0 && t.edits.deleted[pos.orig] {
				skipped += len(fields)
				continue
			}
		} else {
			t.edits.added = append(t.edits.added, &dbNewRow{set: map[int]dbclient.Cell{}})
			pos = dbEditPos{orig: -1, added: len(t.edits.added) - 1}
			added++
		}
		for j, v := range fields {
			col := startCol + j
			if col >= ncols || !tg.editable[col] || dbColumnBinary(t, col) {
				skipped++
				continue
			}
			pos.col = col
			if dbSetCellValue(t, pos, v, false) && i < len(targets) {
				cells++
			}
		}
	}
	return cells, added, skipped
}

// databaseRangeKey runs V then e, + or - on rows r1…r2 of the page (col:
// the column under the cursor).
func (m *Model) databaseRangeKey(key string, r1, r2, col int) tea.Cmd {
	s := m.dbConsole()
	if s.shown == nil || s.running != nil || s.shown.redis != nil {
		return nil
	}
	if s.submitting != nil {
		return m.pushToastDetail(toast.Info, "Saving the changes…", "One moment.")
	}
	if s.shown.plan != nil {
		return m.pushToastDetail(toast.Info, "A plan cannot be edited", "x explains again, r runs the plan again.")
	}
	t := s.curTab()
	if t == nil || t.run == nil {
		return nil
	}
	if s.trans || s.view != dbViewGrid {
		return m.pushToastDetail(toast.Info, "Select rows in the grid", "Tab or t goes back to the grid.")
	}
	tg, cmd := m.databaseEditTarget(t)
	if tg.loading {
		return tea.Batch(cmd, m.pushToastDetail(toast.Info, "Looking up the table's key…", "Select the rows again in a moment."))
	}
	if tg.why != "" {
		return m.pushToastDetail(toast.Info, "This result cannot be edited", tg.detail)
	}
	if r2 < r1 {
		r1, r2 = r2, r1
	}
	pc := col
	if pc < 0 {
		pc = 0
	}
	var poses []dbEditPos
	for r := max(r1, 0); r <= r2; r++ {
		if p, ok := m.databaseEditPos(t, r, pc); ok {
			poses = append(poses, p)
		}
	}
	if len(poses) == 0 {
		return m.pushToastDetail(toast.Info, "Select rows first", "V selects rows of the grid.")
	}
	s.keepCursor = true
	switch key {
	case "-":
		deleted, n := dbToggleDeleteRows(t, poses)
		what := "kept"
		if deleted {
			what = "marked for deletion"
		}
		return tea.Batch(m.databaseShowPage(), m.pushToastDetail(toast.Info, dbPlural(n, "row")+" "+what, "u undoes a change; w shows the SQL."))
	case "+":
		n := dbCloneRows(t, tg, poses)
		s.cursorNewRow = true
		return tea.Batch(m.databaseShowPage(), m.pushToastDetail(toast.Info, "Copied "+dbPlural(n, "row"), "Key columns get their default; e sets a value."))
	case "e":
		if col < 0 || !tg.editable[col] {
			return m.pushToastDetail(toast.Info, "This column cannot be edited", "Put the cursor in a column of "+sanitizeDBText(tg.table)+".")
		}
		if dbColumnBinary(t, col) {
			return m.pushToastDetail(toast.Info, "Binary values cannot be edited here", "Use an UPDATE in the console.")
		}
		var live []dbEditPos
		for _, p := range poses {
			if p.added >= 0 || !t.edits.deletedRow(p.orig) {
				live = append(live, p)
			}
		}
		if len(live) == 0 {
			return m.pushToastDetail(toast.Info, "These rows will be deleted", "- again keeps them; then e edits them.")
		}
		s.editAt, s.editTab, s.editMore = live[0], t, live[1:]
		name := dbOneLine(t.run.result.Columns[col].Name)
		cmd := m.openHTTPPrompt(promptKindDBEditCell, "Edit "+dbPlural(len(live), "row"), name+` for every row (\N for NULL):`, "")
		m.prompt.SetAllowEmpty(true)
		return cmd
	}
	return nil
}

// deletedRow reports whether loaded row orig is marked for deletion.
func (e *dbEdits) deletedRow(orig int) bool { return e != nil && e.deleted[orig] }

// databasePasteRows is P: the clipboard's tab-separated rows go into
// the page from the cursor cell (row, col) to the right and down.
func (m *Model) databasePasteRows(t *dbTab, tg *dbEditTarget, row, col int) tea.Cmd {
	lines, err := dbParseTSV(dbClipboardText())
	if err != nil {
		return m.pushToastDetail(toast.Info, "Nothing to paste", "Copy rows with tabs between the values; "+err.Error()+".")
	}
	var targets []dbEditPos
	for i := range lines {
		p, ok := m.databaseEditPos(t, row+i, col)
		if !ok {
			break
		}
		targets = append(targets, p)
	}
	cells, added, skipped := dbPasteTSV(t, tg, targets, col, lines)
	detail := "u undoes a change; w shows the SQL."
	if skipped > 0 {
		detail = dbPlural(skipped, "value") + " skipped (past the last column, binary, not editable or on a deleted row). " + detail
	}
	s := m.dbConsole()
	s.keepCursor = true
	return tea.Batch(m.databaseShowPage(), m.pushToastDetail(toast.Info, "Pasted "+dbPlural(cells, "value")+", "+dbPlural(added, "new row"), detail))
}
