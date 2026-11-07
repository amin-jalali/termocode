package dbclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CSVTable is a CSV/TSV file loaded into a new SQLite database (DBX-38):
// the table name, its columns and the number of rows written.
type CSVTable struct {
	Table   string
	Columns []string
	Rows    int64
}

// CSVTableName makes a safe SQL table name from the file name: lower case,
// only a-z 0-9 and _, never starting with a digit, "data" when nothing is left.
func CSVTableName(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	var b strings.Builder
	under := false
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			under = false
			continue
		}
		if !under && b.Len() > 0 {
			b.WriteByte('_')
			under = true
		}
	}
	name := strings.TrimRight(b.String(), "_")
	if name == "" {
		return "data"
	}
	if name[0] >= '0' && name[0] <= '9' {
		name = "t_" + name
	}
	return name
}

// csvTableColumns are the sniffed column names made unique without regard
// to case (SQLite column names are case-insensitive): "name", "name_2" …
func csvTableColumns(sn CSVSniff) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(sn.Columns))
	for i, c := range sn.Columns {
		c = strings.TrimSpace(c)
		if c == "" {
			c = "column " + strconv.Itoa(i+1)
		}
		name := c
		for n := 2; seen[strings.ToLower(name)]; n++ {
			name = c + "_" + strconv.Itoa(n)
		}
		seen[strings.ToLower(name)] = true
		out = append(out, name)
	}
	return out
}

// CSVTableDDL is the CREATE TABLE for the file: every column is TEXT, so
// no value is changed on the way in.
func CSVTableDDL(table string, sn CSVSniff) string {
	d := SQLiteDialect{}
	cols := csvTableColumns(sn)
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = d.QuoteIdent(c) + " TEXT"
	}
	return "CREATE TABLE " + d.QuoteIdent(table) + " (" + strings.Join(parts, ", ") + ");"
}

// OpenCSVAsTable loads the file at csvPath into a NEW SQLite database at
// dbPath as one table, so it can be shown in the grid and queried with SQL.
// It never touches an existing file at dbPath, and on any failure the new
// database is removed again.
func OpenCSVAsTable(ctx context.Context, csvPath, dbPath string) (tbl CSVTable, err error) {
	if _, statErr := os.Lstat(dbPath); statErr == nil {
		return CSVTable{}, errors.New("the database file already exists")
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return CSVTable{}, fmt.Errorf("cannot check the database file: %s", pathErr(statErr))
	}
	sn, err := SniffCSV(csvPath)
	if err != nil {
		return CSVTable{}, err
	}
	f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return CSVTable{}, fmt.Errorf("cannot create the database file: %s", pathErr(err))
	}
	_ = f.Close()
	defer func() {
		if err != nil {
			_ = os.Remove(dbPath)
		}
	}()
	sess, err := NewSQLiteSession(ctx, Conn{ID: "csv", Driver: "sqlite", Path: dbPath})
	if err != nil {
		return CSVTable{}, err
	}
	defer sess.Close()
	table := CSVTableName(csvPath)
	if r := sess.Run(ctx, CSVTableDDL(table, sn), RunOptions{}); r.Err != nil {
		return CSVTable{}, fmt.Errorf("cannot create the table: %w", r.Err)
	}
	cols := csvTableColumns(sn)
	sources := make([]int, len(cols))
	for i := range sources {
		sources[i] = i
	}
	res := ImportCSV(ctx, sess, ImportSpec{Path: csvPath, Sniff: sn, Dialect: SQLiteDialect{}, Table: table, Targets: cols, Sources: sources}, nil)
	if res.Err != nil {
		if res.FailedLine > 0 {
			return CSVTable{}, fmt.Errorf("line %d: %w", res.FailedLine, res.Err)
		}
		return CSVTable{}, res.Err
	}
	return CSVTable{Table: table, Columns: cols, Rows: res.Rows}, nil
}
