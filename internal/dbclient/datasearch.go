package dbclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Data search: find a value in every column of a set of tables. Each table
// gets one read-only SELECT with a row limit; the value is always a bound
// parameter, never spliced into SQL.

const (
	dataSearchDefaultRows   = 50
	dataSearchMaxRows       = 1000
	dataSearchDefaultTables = 200
	dataSearchMaxValueLen   = 1000
)

// DataSearchTable is one table to search and the columns to look in.
type DataSearchTable struct {
	Schema  string
	Table   string
	Columns []string
}

// DataSearchOptions bound a data search. Zero values pick safe defaults.
type DataSearchOptions struct {
	Value        string
	Exact        bool // whole cell equals Value; otherwise contains, case-insensitive
	RowsPerTable int  // default 50, at most 1000
	MaxTables    int  // default 200
}

// DataSearchQuery is the SELECT for one table.
type DataSearchQuery struct {
	Schema string
	Table  string
	SQL    string
	Args   []any
}

// DataSearchHit is a table with matching rows (Rows is capped by the limit).
type DataSearchHit struct {
	Schema string
	Table  string
	Rows   int
	Query  DataSearchQuery
}

// DataSearchError is a table whose query failed; the search goes on.
type DataSearchError struct {
	Schema string
	Table  string
	Err    error
}

// DataSearchReport is the result of RunDataSearch.
type DataSearchReport struct {
	Hits      []DataSearchHit
	Errors    []DataSearchError
	Searched  int
	Truncated bool // stopped early at the hit cap
	Canceled  bool
}

// DataSearchQueries builds one bounded query per table that has columns.
func DataSearchQueries(driver string, tables []DataSearchTable, opt DataSearchOptions) ([]DataSearchQuery, error) {
	d := DialectFor(driver)
	if d == nil {
		return nil, fmt.Errorf("data search needs a SQL connection, not %q", driver)
	}
	if strings.TrimSpace(opt.Value) == "" {
		return nil, errors.New("data search: value is empty")
	}
	if len(opt.Value) > dataSearchMaxValueLen {
		return nil, fmt.Errorf("data search: value is longer than %d bytes", dataSearchMaxValueLen)
	}
	rows := opt.RowsPerTable
	if rows <= 0 {
		rows = dataSearchDefaultRows
	}
	rows = min(rows, dataSearchMaxRows)
	maxTables := opt.MaxTables
	if maxTables <= 0 {
		maxTables = dataSearchDefaultTables
	}
	castType := "TEXT"
	if normDriver(driver) == "mysql" {
		castType = "CHAR"
	}
	arg := opt.Value
	if !opt.Exact {
		arg = "%" + escapeLike(opt.Value) + "%"
	}
	var out []DataSearchQuery
	for _, t := range tables {
		if len(out) == maxTables {
			break
		}
		if t.Table == "" || len(t.Columns) == 0 {
			continue
		}
		name := d.QuoteIdent(t.Table)
		if t.Schema != "" {
			name = d.QuoteIdent(t.Schema) + "." + name
		}
		conds := make([]string, 0, len(t.Columns))
		args := make([]any, 0, len(t.Columns))
		for i, c := range t.Columns {
			col := "CAST(" + d.QuoteIdent(c) + " AS " + castType + ")"
			ph := d.Placeholder(i + 1)
			if opt.Exact {
				conds = append(conds, col+" = "+ph)
			} else {
				conds = append(conds, "LOWER("+col+") LIKE LOWER("+ph+") ESCAPE '!'")
			}
			args = append(args, arg)
		}
		out = append(out, DataSearchQuery{
			Schema: t.Schema,
			Table:  t.Table,
			SQL:    "SELECT * FROM " + name + " WHERE " + strings.Join(conds, " OR ") + OrderedLimit(d, rows, 0),
			Args:   args,
		})
	}
	return out, nil
}

// escapeLike escapes LIKE wildcards with '!' (same escape on every dialect).
func escapeLike(s string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(s)
}

// RunDataSearch runs the queries one by one through run, which returns the
// number of matching rows. It stops when ctx is done or when maxHits rows
// (0 = no cap) were found. A failing table is recorded and skipped.
func RunDataSearch(ctx context.Context, queries []DataSearchQuery, maxHits int, run func(context.Context, DataSearchQuery) (int, error)) DataSearchReport {
	var rep DataSearchReport
	total := 0
	for _, q := range queries {
		if ctx.Err() != nil {
			rep.Canceled = true
			return rep
		}
		if maxHits > 0 && total >= maxHits {
			rep.Truncated = true
			return rep
		}
		n, err := run(ctx, q)
		if ctx.Err() != nil {
			rep.Canceled = true
			return rep
		}
		rep.Searched++
		if err != nil {
			rep.Errors = append(rep.Errors, DataSearchError{Schema: q.Schema, Table: q.Table, Err: err})
			continue
		}
		if n > 0 {
			rep.Hits = append(rep.Hits, DataSearchHit{Schema: q.Schema, Table: q.Table, Rows: n, Query: q})
			total += n
		}
	}
	return rep
}
