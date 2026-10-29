package dbclient

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// catalogSession answers catalog reads by the first entry whose key is
// in the SQL; failOn makes the matching read fail like a server error.
type catalogSession struct {
	Session
	answers map[string][][]Cell
	failOn  string
	ran     []string
}

func (s *catalogSession) Run(_ context.Context, sql string, _ RunOptions) Result {
	s.ran = append(s.ran, sql)
	if s.failOn != "" && strings.Contains(sql, s.failOn) {
		return Result{Err: errors.New("permission denied for " + s.failOn)}
	}
	for k, rows := range s.answers {
		if strings.Contains(sql, k) {
			return Result{Rows: rows}
		}
	}
	return Result{}
}

// cells builds a row; "<null>" is SQL NULL.
func cells(s ...string) []Cell {
	out := make([]Cell, len(s))
	for i, v := range s {
		if v == "<null>" {
			out[i] = Cell{Null: true}
		} else {
			out[i] = Cell{Text: v}
		}
	}
	return out
}

// PostgreSQL joins the columns of a key or index with the unit separator
// (\x1f). The read must split them before it drops control characters,
// or a two-column key (a, b) shows as one column "ab".
func TestReadCanonSchemaPostgresMultiColumnKeys(t *testing.T) {
	s := &catalogSession{answers: map[string][][]Cell{
		"pg_attribute a ON a.attrelid = c.oid": {
			cells("t", "a", "int", "t", "<null>", ""),
			cells("t", "b", "int", "t", "<null>", ""),
		},
		"pg_get_constraintdef": {
			cells("t", "t_pkey", "p", "a\x1fb", "<null>", "<null>", "<null>", "<null>", "<null>"),
			cells("t", "t_fk", "f", "a\x1fb", "u", "x\x1fy", "a", "a", "<null>"),
		},
		"FROM pg_index ix": {
			cells("t", "t_ba", "f", "btree", "b\x1fa", "<null>"),
		},
	}}
	got, err := ReadCanonSchema(context.Background(), s, "postgres", "public")
	if err != nil {
		t.Fatal(err)
	}
	tb := got.Tables[0]
	if !reflect.DeepEqual(tb.Constraints[0].Columns, []string{"a", "b"}) {
		t.Errorf("primary key columns %q", tb.Constraints[0].Columns)
	}
	if !reflect.DeepEqual(tb.Constraints[1].RefColumns, []string{"x", "y"}) {
		t.Errorf("foreign key target columns %q", tb.Constraints[1].RefColumns)
	}
	if !reflect.DeepEqual(tb.Indexes[0].Exprs, []string{"b", "a"}) {
		t.Errorf("index columns %q", tb.Indexes[0].Exprs)
	}
}
