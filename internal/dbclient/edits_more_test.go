package dbclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A row with a two-column key is found by both columns; Redis has no
// SQL edits; an unknown change kind is refused.
func TestEditStatementsTwoColumnKeyAndRefusals(t *testing.T) {
	st, err := EditStatements(PostgresDialect{}, "", "t", []RowEdit{{Kind: EditDelete, Key: []EditValue{
		{Column: "a", Value: Cell{Text: "1"}}, {Column: "b", Value: Cell{Text: "x"}},
	}}})
	if err != nil || st[0].SQL != `DELETE FROM "t" WHERE "a" = $1 AND "b" = $2` || !reflect.DeepEqual(st[0].Args, []any{"1", "x"}) {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := EditStatements(nil, "", "t", nil); err == nil || !strings.Contains(err.Error(), "does not use SQL") {
		t.Errorf("nil dialect = %v", err)
	}
	if _, err := editStatement(PostgresDialect{}, `"t"`, RowEdit{Kind: EditKind(9)}); err == nil || err.Error() != "unknown change" {
		t.Errorf("unknown kind = %v", err)
	}
	if _, err := EditStatements(PostgresDialect{}, "", "t", []RowEdit{{Kind: EditUpdate, Values: []EditValue{{Column: "a", Value: Cell{Text: "1"}}}}}); err == nil || !strings.Contains(err.Error(), "no key") {
		t.Errorf("keyless update = %v", err)
	}
}

// ApplyEdits: nothing to do is no transaction; BEGIN, SAVEPOINT, COMMIT
// and a failing undo are all reported.
func TestApplyEditsTransactionFailures(t *testing.T) {
	ctx := context.Background()
	s, stub := editStub(t, "edits-none", 1)
	if out := ApplyEdits(ctx, s, nil, false); out.Err != nil || out.Failed != -1 {
		t.Fatalf("empty = %+v", out)
	}
	if b, _, _ := stub.TransactionCounts(); b != 0 {
		t.Fatal("a transaction for nothing")
	}
	stub.BeginError = errors.New("too many connections")
	if out := ApplyEdits(ctx, s, twoEdits(t), false); out.Err == nil || !strings.Contains(out.Err.Error(), "too many connections") || len(stub.Recorded()) != 0 {
		t.Fatalf("begin = %+v %q", out, stub.Recorded())
	}
	stub.BeginError = nil
	stub.CommitError = errors.New("could not serialize access")
	if out := ApplyEdits(ctx, s, twoEdits(t), false); out.Err == nil || !strings.Contains(out.Err.Error(), "could not serialize") {
		t.Fatalf("commit = %+v", out)
	}
	stub.CommitError = nil
	stub.ExecError = errors.New("deadlock detected")
	stub.RollbackError = errors.New("connection lost")
	out := ApplyEdits(ctx, s, twoEdits(t), false)
	if out.Err == nil || !strings.Contains(out.Err.Error(), "deadlock detected") || !strings.Contains(out.Err.Error(), "rolling back also failed: connection lost") {
		t.Fatalf("undo failure = %+v", out)
	}
}

func TestApplyEditsSavepointFailures(t *testing.T) {
	ctx := context.Background()
	f := &failSess{fail: func(sql string, _ []any) error {
		if strings.HasPrefix(sql, "SAVEPOINT") {
			return errors.New("current transaction is aborted")
		}
		return nil
	}}
	if out := ApplyEdits(ctx, f, twoEdits(t), true); out.Err == nil || !strings.Contains(out.Err.Error(), "aborted") {
		t.Fatalf("savepoint = %+v", out)
	}
	g := &failSess{fail: func(sql string, _ []any) error {
		switch {
		case strings.HasPrefix(sql, "DELETE"):
			return errors.New("permission denied for table users")
		case strings.HasPrefix(sql, "ROLLBACK TO"):
			return errors.New("no such savepoint")
		}
		return nil
	}}
	out := ApplyEdits(ctx, g, twoEdits(t), true)
	if out.Err == nil || !strings.Contains(out.Err.Error(), "permission denied") || !strings.Contains(out.Err.Error(), "no such savepoint") {
		t.Fatalf("savepoint undo = %+v", out)
	}
}

// The console history: newest first, delete by id, and a broken store
// is an error, not an empty list.
func TestHistoryOrderDeleteAndBrokenStore(t *testing.T) {
	h := History{Path: filepath.Join(t.TempDir(), "sub", "h.jsonl")}
	for _, id := range []string{"a", "b", "c"} {
		if err := h.Append(HistoryEntry{ID: id, SQL: "SELECT " + id}); err != nil {
			t.Fatal(err)
		}
	}
	es, err := h.List()
	if err != nil || len(es) != 3 || es[0].ID != "c" || es[2].ID != "a" {
		t.Fatalf("list = %+v %v", es, err)
	}
	if err := h.Delete("b"); err != nil {
		t.Fatal(err)
	}
	es, _ = h.List()
	if len(es) != 2 || es[0].ID != "c" || es[1].ID != "a" {
		t.Fatalf("after delete = %+v", es)
	}
	dir := History{Path: t.TempDir()} // a folder, not a file
	if _, err := dir.List(); err == nil {
		t.Error("List of a folder")
	}
	if err := dir.Append(HistoryEntry{SQL: "x"}); err == nil {
		t.Error("Append to a folder")
	}
	if err := dir.Delete("x"); err == nil {
		t.Error("Delete in a folder")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	under := History{Path: filepath.Join(blocker, "h.jsonl")}
	if err := under.Append(HistoryEntry{SQL: "x"}); err == nil {
		t.Error("Append under a file")
	}
}

// Sorting puts NULLs and short rows first going up and last going down;
// a negative column changes nothing.
func TestSortNullsAndShortRows(t *testing.T) {
	r := Result{Rows: [][]Cell{{{Text: "b"}}, {}, {{Null: true}}, {{Text: "a"}}}}
	text := func() string {
		var out []string
		for _, row := range r.Rows {
			if len(row) == 0 || row[0].Null {
				out = append(out, "-")
			} else {
				out = append(out, row[0].Text)
			}
		}
		return strings.Join(out, ",")
	}
	Sort(&r, 0, false)
	if got := text(); got != "-,-,a,b" {
		t.Fatalf("asc = %s", got)
	}
	Sort(&r, 0, true)
	if got := text(); got != "b,a,-,-" {
		t.Fatalf("desc = %s", got)
	}
	before := append([][]Cell(nil), r.Rows...)
	Sort(&r, -1, false)
	if !reflect.DeepEqual(before, r.Rows) {
		t.Fatal("negative column sorted")
	}
	if got := Filter(r, ""); len(got.Rows) != len(r.Rows) {
		t.Fatal("empty filter dropped rows")
	}
	if got := ToJSON(Result{Columns: []Column{{Name: "ok"}}, Rows: [][]Cell{{{Text: "true", Kind: "bool"}}}}); got != `[{"ok":true}]` {
		t.Errorf("bool json = %s", got)
	}
}
