package dbclient

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Foreign key navigation (DBX-7): from an FK cell to the referenced row
// and back, the rows that point at a row, and the values a key may take
// while editing. Names go through the dialect's QuoteIdent and every
// value is a bound parameter, like EditStatements.

// FKForColumn returns the whole foreign key (all its columns, in order)
// that column is part of. Rows of one key share a Name; an unnamed key
// (SQLite) is one column.
func FKForColumn(fks []SchemaForeignKey, column string) ([]SchemaForeignKey, bool) {
	for _, fk := range fks {
		if !strings.EqualFold(fk.Column, column) {
			continue
		}
		if fk.Name == "" {
			return []SchemaForeignKey{fk}, true
		}
		var g []SchemaForeignKey
		for _, o := range fks {
			if o.Name == fk.Name {
				g = append(g, o)
			}
		}
		return g, true
	}
	return nil, false
}

// IncomingFK is a foreign key of Table that points at another table.
type IncomingFK struct {
	Table string
	Key   []SchemaForeignKey
}

// IncomingFKs lists the keys of every table in all that point at
// target, sorted by table then key name.
func IncomingFKs(all map[string][]SchemaForeignKey, target string) []IncomingFK {
	var out []IncomingFK
	tables := make([]string, 0, len(all))
	for t := range all {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	for _, t := range tables {
		seen := map[string]bool{}
		for _, fk := range all[t] {
			if !strings.EqualFold(fk.RefTable, target) {
				continue
			}
			if fk.Name != "" && seen[fk.Name] {
				continue
			}
			seen[fk.Name] = true
			g, _ := FKForColumn(all[t], fk.Column)
			out = append(out, IncomingFK{Table: t, Key: g})
		}
	}
	return out
}

func fkTableName(d Dialect, schema, table string) string {
	name := d.QuoteIdent(table)
	if schema != "" {
		name = d.QuoteIdent(schema) + "." + name
	}
	return name
}

// fkSelect builds SELECT * FROM table WHERE col = ? AND … with values
// taken from row by the from-names.
func fkSelect(d Dialect, schema, table string, where, from []string, row map[string]Cell, limit int) (EditStatement, error) {
	if d == nil {
		return EditStatement{}, errors.New("this connection does not use SQL")
	}
	if strings.TrimSpace(table) == "" || len(where) == 0 {
		return EditStatement{}, errors.New("no foreign key to follow")
	}
	b := &editBuilder{d: d}
	b.text("SELECT * FROM " + fkTableName(d, schema, table) + " WHERE ")
	for i := range where {
		c, ok := row[from[i]]
		if !ok {
			return EditStatement{}, fmt.Errorf("the row has no column %s", from[i])
		}
		if c.Null {
			return EditStatement{}, fmt.Errorf("%s is NULL: it points to no row", from[i])
		}
		if i > 0 {
			b.text(" AND ")
		}
		if err := b.ident(where[i]); err != nil {
			return EditStatement{}, err
		}
		b.text(" = ")
		b.value(c)
	}
	b.text(d.Limit(limit, 0))
	return EditStatement{SQL: b.sql.String(), Args: b.args, Preview: b.preview.String()}, nil
}

// FKTargetQuery selects the row that key (from FKForColumn) points to,
// with the values of row (column name → cell of the FK side).
func FKTargetQuery(d Dialect, schema string, key []SchemaForeignKey, row map[string]Cell) (EditStatement, error) {
	if len(key) == 0 {
		return EditStatement{}, errors.New("no foreign key to follow")
	}
	where, from := make([]string, len(key)), make([]string, len(key))
	for i, fk := range key {
		where[i], from[i] = fk.RefColumn, fk.Column
	}
	return fkSelect(d, schema, key[0].RefTable, where, from, row, 1)
}

// ReferencingRowsQuery selects up to limit rows of child whose key
// points at parent (column name → cell of the referenced side).
func ReferencingRowsQuery(d Dialect, schema, child string, key []SchemaForeignKey, parent map[string]Cell, limit int) (EditStatement, error) {
	where, from := make([]string, len(key)), make([]string, len(key))
	for i, fk := range key {
		where[i], from[i] = fk.Column, fk.RefColumn
	}
	return fkSelect(d, schema, child, where, from, parent, limit)
}

// FKLookupQuery lists up to limit values a one-column key may take (the
// referenced column's distinct values, sorted).
func FKLookupQuery(d Dialect, schema string, key []SchemaForeignKey, limit int) (EditStatement, error) {
	if d == nil {
		return EditStatement{}, errors.New("this connection does not use SQL")
	}
	if len(key) != 1 {
		return EditStatement{}, errors.New("lookup values need a one-column foreign key")
	}
	fk := key[0]
	if fk.RefTable == "" || fk.RefColumn == "" {
		return EditStatement{}, errors.New("the foreign key has no referenced column")
	}
	col := d.QuoteIdent(fk.RefColumn)
	sql := "SELECT DISTINCT " + col + " FROM " + fkTableName(d, schema, fk.RefTable) + " ORDER BY " + col + d.Limit(limit, 0)
	return EditStatement{SQL: sql, Preview: sql}, nil
}

// FKStep is where a jump started: the table, the row and the column of
// the FK cell, so Back can return there.
type FKStep struct {
	Schema string
	Table  string
	Row    int
	Column string
}

// fkTrailMax caps the back trail; the oldest step is dropped.
const fkTrailMax = 64

// FKTrail is the jump history of one grid.
type FKTrail struct{ steps []FKStep }

// Push records a jump from s.
func (t *FKTrail) Push(s FKStep) {
	t.steps = append(t.steps, s)
	if len(t.steps) > fkTrailMax {
		t.steps = append(t.steps[:0], t.steps[len(t.steps)-fkTrailMax:]...)
	}
}

// Back pops the last jump.
func (t *FKTrail) Back() (FKStep, bool) {
	if len(t.steps) == 0 {
		return FKStep{}, false
	}
	s := t.steps[len(t.steps)-1]
	t.steps = t.steps[:len(t.steps)-1]
	return s, true
}

// Len is the number of steps to go back.
func (t *FKTrail) Len() int { return len(t.steps) }
