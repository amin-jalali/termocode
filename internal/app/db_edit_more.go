package app

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Edit upgrades (DBX-55): D on a grid cell sets it back to the column's
// DEFAULT (the keyword goes into the SQL), U puts a new random UUID in
// it, and a typed value is checked against the column type before it
// becomes a pending change. Both keys work on new rows too.

// dbDefaultKind marks a pending cell that is the DEFAULT keyword.
const dbDefaultKind = "default"

func dbDefaultCell() dbclient.Cell { return dbclient.Cell{Text: "DEFAULT", Kind: dbDefaultKind} }

// dbEditValue is the edit of one column: the DEFAULT marker becomes the
// keyword, anything else a bound value.
func dbEditValue(col string, v, old dbclient.Cell, check bool) dbclient.EditValue {
	if v.Kind == dbDefaultKind && !v.Null {
		return dbclient.EditValue{Column: col, Default: true, Old: old, Check: check}
	}
	return dbclient.EditValue{Column: col, Value: v, Old: old, Check: check}
}

var (
	dbUUIDText = regexp.MustCompile(`^(?i)\{?[0-9a-f]{8}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{12}\}?$`)
	dbTypeArgs = regexp.MustCompile(`\(.*\)`)
)

// dbCheckCellType says why value does not fit a column of type typ (nil:
// it fits, or the type is not checked). The server stays the judge; this
// only catches typing slips before the card.
func dbCheckCellType(typ, value string) error {
	t := strings.ToLower(strings.TrimSpace(dbTypeArgs.ReplaceAllString(typ, "")))
	if strings.HasSuffix(t, "[]") || strings.HasPrefix(t, "_") {
		return nil // arrays: the server checks them
	}
	t = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(t, " unsigned"), " signed"))
	v := strings.TrimSpace(value)
	switch t {
	case "int", "integer", "int2", "int4", "int8", "smallint", "bigint", "tinyint", "mediumint", "serial", "bigserial", "smallserial":
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			if _, uerr := strconv.ParseUint(v, 10, 64); uerr != nil {
				return errors.New("a whole number, such as 42")
			}
		}
	case "numeric", "decimal", "real", "float", "float4", "float8", "double", "double precision", "money":
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return errors.New("a number, such as 1.5")
		}
	case "bool", "boolean":
		switch strings.ToLower(v) {
		case "true", "false", "t", "f", "1", "0", "yes", "no", "y", "n", "on", "off":
		default:
			return errors.New("true or false")
		}
	case "uuid", "uniqueidentifier":
		if !dbUUIDText.MatchString(v) {
			return errors.New("a UUID, such as 3f2b8c1e-9a4d-4e2f-8b1a-0c9d8e7f6a5b (U makes one)")
		}
	case "json", "jsonb":
		if !json.Valid([]byte(v)) {
			return errors.New("valid JSON")
		}
	case "date":
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return errors.New("a date as YYYY-MM-DD")
		}
	}
	return nil
}

// databaseCheckTyped checks the value typed for the cell being edited;
// false: it does not fit, and a toast says what does.
func (m *Model) databaseCheckTyped(t *dbTab, pos dbEditPos, value string) (tea.Cmd, bool) {
	if value == `\N` || (value == "" && pos.added >= 0) || pos.col < 0 || pos.col >= len(t.run.result.Columns) {
		return nil, true
	}
	col := t.run.result.Columns[pos.col]
	if err := dbCheckCellType(col.Type, value); err != nil {
		return m.pushToastDetail(toast.Warn, dbOneLine(col.Name)+" is "+dbOneLine(col.Type)+": type "+err.Error(), "The value was not kept. e edits it again."), false
	}
	return nil, true
}

// dbSetCellSpecial keeps cell (DEFAULT or a UUID) as the pending value at
// pos. false: pos is not a row of t.
func dbSetCellSpecial(t *dbTab, pos dbEditPos, cell dbclient.Cell) bool {
	if t.edits == nil {
		t.edits = newDBEdits()
	}
	t.edits.note = ""
	if pos.added >= 0 {
		if pos.added >= len(t.edits.added) {
			return false
		}
		if cell.Kind == dbDefaultKind {
			delete(t.edits.added[pos.added].set, pos.col) // an unset cell gets its default
		} else {
			t.edits.added[pos.added].set[pos.col] = cell
		}
		return true
	}
	if pos.orig < 0 || pos.orig >= len(t.run.result.Rows) {
		return false
	}
	t.edits.cells[dbCellRef{pos.orig, pos.col}] = cell
	return true
}

// databaseSpecialKey is D (DEFAULT) or U (new UUID) on the cells at
// poses (the cursor cell, or a column of the selected rows).
func (m *Model) databaseSpecialKey(key string, t *dbTab, tg *dbEditTarget, poses []dbEditPos) tea.Cmd {
	s := m.dbConsole()
	if len(poses) == 0 {
		return m.pushToastDetail(toast.Info, "Move to a row first", "The cursor is not on a cell of the grid.")
	}
	col := poses[0].col
	if !tg.editable[col] {
		return m.pushToastDetail(toast.Info, "This column cannot be edited", "It is not a column of "+sanitizeDBText(tg.table)+" (or it is in the result twice).")
	}
	if key == "D" && tg.driver == "sqlite" && poses[0].orig >= 0 {
		return m.pushToastDetail(toast.Info, "SQLite cannot set a value back to its DEFAULT", "Type the value instead; on a new row, an empty cell gets its default.")
	}
	n := 0
	for _, p := range poses {
		if p.orig >= 0 && t.edits != nil && t.edits.deleted[p.orig] {
			continue
		}
		cell := dbDefaultCell()
		if key == "U" {
			cell = dbclient.NewCellUUID()
		}
		if dbSetCellSpecial(t, p, cell) {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	s.keepCursor = true
	what := "Set to DEFAULT"
	if key == "U" {
		what = "New UUID"
	}
	if n > 1 {
		what += " in " + dbPlural(n, "row")
	}
	return tea.Batch(m.databaseShowPage(), m.pushToastDetail(toast.Info, what, "w shows the SQL before it runs · u undoes it."))
}
