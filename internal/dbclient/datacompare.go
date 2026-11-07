package dbclient

// Data compare (DBX-12): two result sets (two tables, or two queries,
// maybe on two connections) matched row by row on key columns, and a
// sync script that makes the target hold the source's rows.

import (
	"fmt"
	"strings"
)

// RowDiffKind is how a row differs.
type RowDiffKind int

// Row diff kinds.
const (
	RowChanged RowDiffKind = iota // same key, other values
	RowAdded                      // only in the source
	RowRemoved                    // only in the target
)

func (k RowDiffKind) String() string {
	switch k {
	case RowAdded:
		return "added"
	case RowRemoved:
		return "removed"
	}
	return "changed"
}

// RowDiff is one row that differs. Source and Target hold the cells in
// DataComparison.Columns order (nil on the side that has no such row).
type RowDiff struct {
	Kind    RowDiffKind
	Key     []Cell
	Source  []Cell
	Target  []Cell
	Changed []int // indexes into Columns (RowChanged only)
}

// ChangedNames is the names of the changed columns.
func (d RowDiff) ChangedNames(c DataComparison) []string {
	out := make([]string, 0, len(d.Changed))
	for _, i := range d.Changed {
		out = append(out, c.Columns[i])
	}
	return out
}

// DataComparison is the result of CompareData.
type DataComparison struct {
	Columns    []string // compared columns, source names and order
	Key        []string // key columns (source names)
	OnlySource []string // columns not compared: only in the source
	OnlyTarget []string // ... only in the target
	Diffs      []RowDiff
	Same       int
	Added      int
	Removed    int
	Changed    int
	DupKeys    int // rows skipped because an earlier row had the same key
	NullKeys   int // rows skipped because a key cell is NULL
}

// CompareData matches source and target rows on key (column names; empty
// = the source's primary-key columns or its row key) and lists what
// differs. Column names match exactly first, then ignoring case.
func CompareData(source, target Result, key []string) (DataComparison, error) {
	var c DataComparison
	tIdx := map[string]int{}
	tFold := map[string]int{}
	for i, col := range target.Columns {
		if _, ok := tIdx[col.Name]; !ok {
			tIdx[col.Name] = i
		}
		if _, ok := tFold[strings.ToLower(col.Name)]; !ok {
			tFold[strings.ToLower(col.Name)] = i
		}
	}
	findT := func(name string) (int, bool) {
		if i, ok := tIdx[name]; ok {
			return i, true
		}
		i, ok := tFold[strings.ToLower(name)]
		return i, ok
	}
	var srcCols, tgtCols []int
	usedT := map[int]bool{}
	for i, col := range source.Columns {
		j, ok := findT(col.Name)
		if !ok || usedT[j] {
			c.OnlySource = append(c.OnlySource, col.Name)
			continue
		}
		usedT[j] = true
		c.Columns = append(c.Columns, col.Name)
		srcCols = append(srcCols, i)
		tgtCols = append(tgtCols, j)
	}
	for j, col := range target.Columns {
		if !usedT[j] {
			c.OnlyTarget = append(c.OnlyTarget, col.Name)
		}
	}

	if len(key) == 0 {
		for _, col := range source.Columns {
			if col.PK {
				key = append(key, col.Name)
			}
		}
		if len(key) == 0 {
			for _, i := range source.Key {
				if i >= 0 && i < len(source.Columns) {
					key = append(key, source.Columns[i].Name)
				}
			}
		}
	}
	if len(key) == 0 {
		return c, fmt.Errorf("no key columns: pick the columns that identify a row")
	}
	var keyPos []int // positions in c.Columns
	for _, k := range key {
		pos := -1
		for i, name := range c.Columns {
			if name == k || pos < 0 && strings.EqualFold(name, k) {
				pos = i
			}
		}
		if pos < 0 {
			return c, fmt.Errorf("key column %q is not in both results", k)
		}
		keyPos = append(keyPos, pos)
		c.Key = append(c.Key, c.Columns[pos])
	}

	pick := func(row []Cell, idx []int) []Cell {
		out := make([]Cell, len(idx))
		for i, j := range idx {
			if j < len(row) {
				out[i] = row[j]
			} else {
				out[i] = Cell{Null: true}
			}
		}
		return out
	}
	keyOf := func(cells []Cell) (string, bool) {
		var b strings.Builder
		for _, p := range keyPos {
			if cells[p].Null {
				return "", false
			}
			b.WriteString(cells[p].Text)
			b.WriteByte(0)
		}
		return b.String(), true
	}
	index := func(rows [][]Cell, idx []int) (keys []string, cells [][]Cell, at map[string]int) {
		at = map[string]int{}
		for _, row := range rows {
			cs := pick(row, idx)
			k, ok := keyOf(cs)
			if !ok {
				c.NullKeys++
				continue
			}
			if _, dup := at[k]; dup {
				c.DupKeys++
				continue
			}
			at[k] = len(keys)
			keys = append(keys, k)
			cells = append(cells, cs)
		}
		return keys, cells, at
	}
	sKeys, sCells, _ := index(source.Rows, srcCols)
	tKeys, tCells, tAt := index(target.Rows, tgtCols)

	matched := map[string]bool{}
	keyCells := func(cs []Cell) []Cell {
		out := make([]Cell, len(keyPos))
		for i, p := range keyPos {
			out[i] = cs[p]
		}
		return out
	}
	for i, k := range sKeys {
		s := sCells[i]
		j, ok := tAt[k]
		if !ok {
			c.Added++
			c.Diffs = append(c.Diffs, RowDiff{Kind: RowAdded, Key: keyCells(s), Source: s})
			continue
		}
		matched[k] = true
		t := tCells[j]
		var changed []int
		for col := range s {
			if !sameCell(s[col], t[col]) {
				changed = append(changed, col)
			}
		}
		if len(changed) == 0 {
			c.Same++
			continue
		}
		c.Changed++
		c.Diffs = append(c.Diffs, RowDiff{Kind: RowChanged, Key: keyCells(s), Source: s, Target: t, Changed: changed})
	}
	for j, k := range tKeys {
		if !matched[k] {
			c.Removed++
			c.Diffs = append(c.Diffs, RowDiff{Kind: RowRemoved, Key: keyCells(tCells[j]), Target: tCells[j]})
		}
	}
	return c, nil
}

func sameCell(a, b Cell) bool {
	if a.Null || b.Null {
		return a.Null == b.Null
	}
	return a.Text == b.Text
}

// SyncScript writes the statements that make the target table (schema
// may be "") hold the source's rows: DELETE, then UPDATE (changed
// columns only), then INSERT, in one transaction. It is text for the
// user to review; nothing runs here.
func (c DataComparison) SyncScript(d Dialect, schema, table string) string {
	qual := d.QuoteIdent(table)
	if schema != "" {
		qual = d.QuoteIdent(schema) + "." + qual
	}
	var b strings.Builder
	fmt.Fprintf(&b, "-- Data sync: makes %s match the source.\n", qual)
	if len(c.Diffs) == 0 {
		b.WriteString("-- No differences.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "-- %d to delete, %d to update, %d to insert. Review before you run it.\n", c.Removed, c.Changed, c.Added)
	if _, mysql := d.(MySQLDialect); mysql {
		b.WriteString("START TRANSACTION;\n")
	} else {
		b.WriteString("BEGIN;\n")
	}
	where := func(key []Cell) string {
		parts := make([]string, len(c.Key))
		for i, k := range c.Key {
			parts[i] = d.QuoteIdent(k) + " = " + SQLLiteral(d, key[i])
		}
		return strings.Join(parts, " AND ")
	}
	for _, r := range c.Diffs {
		if r.Kind == RowRemoved {
			fmt.Fprintf(&b, "DELETE FROM %s WHERE %s;\n", qual, where(r.Key))
		}
	}
	for _, r := range c.Diffs {
		if r.Kind != RowChanged {
			continue
		}
		sets := make([]string, len(r.Changed))
		for i, col := range r.Changed {
			sets[i] = d.QuoteIdent(c.Columns[col]) + " = " + SQLLiteral(d, r.Source[col])
		}
		fmt.Fprintf(&b, "UPDATE %s SET %s WHERE %s;\n", qual, strings.Join(sets, ", "), where(r.Key))
	}
	if c.Added > 0 {
		names := make([]string, len(c.Columns))
		for i, n := range c.Columns {
			names[i] = d.QuoteIdent(n)
		}
		head := "INSERT INTO " + qual + " (" + strings.Join(names, ", ") + ") VALUES ("
		for _, r := range c.Diffs {
			if r.Kind != RowAdded {
				continue
			}
			vals := make([]string, len(r.Source))
			for i, cell := range r.Source {
				vals[i] = SQLLiteral(d, cell)
			}
			b.WriteString(head + strings.Join(vals, ", ") + ");\n")
		}
	}
	b.WriteString("COMMIT;\n")
	return b.String()
}
