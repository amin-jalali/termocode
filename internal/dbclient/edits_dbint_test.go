//go:build dbint

package dbclient

import (
	"context"
	"errors"
	"os"
	"testing"
)

// Edits against a real PostgreSQL: text values bind into integer, boolean,
// timestamp and jsonb columns; the old-value check catches a row someone
// else changed; a manual transaction keeps the edits until Rollback.
func TestDBIntPostgresEdits(t *testing.T) {
	dsn := os.Getenv("DBINT_POSTGRES_DSN")
	if dsn == "" && dbintEnabled() {
		dsn = "postgres://termocode:termocode@localhost:54329/termocode"
	}
	if dsn == "" {
		t.Skip("DBINT_POSTGRES_DSN is not set")
	}
	conn, err := ParseURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, err := NewPostgresSession(ctx, conn, conn.Password)
	if err != nil {
		unavailableDBInt(t, err)
	}
	defer s.Close()
	for _, q := range []string{
		`DROP TABLE IF EXISTS "Edit Probe"`,
		`CREATE TABLE "Edit Probe" (id bigint PRIMARY KEY, n int, ok boolean, at timestamptz, doc jsonb, note text)`,
		`INSERT INTO "Edit Probe" VALUES (1, 5, false, '2026-10-18 09:12:33+00', '{"a":1}', 'old'), (2, 6, true, NULL, NULL, NULL)`,
	} {
		if r := s.Run(ctx, q, RunOptions{}); r.Err != nil {
			t.Fatalf("%s: %v", q, r.Err)
		}
	}
	defer s.Run(context.Background(), `DROP TABLE IF EXISTS "Edit Probe"`, RunOptions{})
	read := func(id string) []Cell {
		r := s.Run(ctx, `SELECT n, ok, at, doc, note FROM "Edit Probe" WHERE id = $1`, RunOptions{Args: []any{id}})
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		if len(r.Rows) == 0 {
			return nil
		}
		return r.Rows[0]
	}
	before := read("1")
	stmts, err := EditStatements(PostgresDialect{}, "", "Edit Probe", []RowEdit{{
		Kind: EditUpdate,
		Key:  []EditValue{{Column: "id", Value: Cell{Text: "1"}}},
		Values: []EditValue{
			{Column: "n", Value: Cell{Text: "7"}, Old: before[0], Check: true},
			{Column: "ok", Value: Cell{Text: "true"}, Old: before[1], Check: true},
			{Column: "at", Value: Cell{Text: "2026-10-19T10:00:00Z"}, Old: before[2], Check: true},
			{Column: "doc", Value: Cell{Text: `{"b": 2}`}},
			{Column: "note", Value: Cell{Text: "x'); DROP TABLE \"Edit Probe\"; --"}, Old: before[4], Check: true},
		},
	}, {Kind: EditInsert, Values: []EditValue{{Column: "id", Value: Cell{Text: "3"}}, {Column: "note", Value: Cell{Null: true}}}},
		{Kind: EditDelete, Key: []EditValue{{Column: "id", Value: Cell{Text: "2"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out := ApplyEdits(ctx, s, stmts, false); out.Err != nil {
		t.Fatalf("apply: %+v", out)
	}
	after := read("1")
	if after[0].Text != "7" || after[1].Text != "true" || after[4].Text != "x'); DROP TABLE \"Edit Probe\"; --" {
		t.Fatalf("row 1 = %+v", after)
	}
	if read("2") != nil || read("3") == nil {
		t.Fatal("delete or insert did not happen")
	}
	// Someone else changes row 1: our old value no longer matches.
	if r := s.Run(ctx, `UPDATE "Edit Probe" SET n = 99 WHERE id = 1`, RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	stale, _ := EditStatements(PostgresDialect{}, "", "Edit Probe", []RowEdit{{Kind: EditUpdate,
		Key:    []EditValue{{Column: "id", Value: Cell{Text: "1"}}},
		Values: []EditValue{{Column: "n", Value: Cell{Text: "8"}, Old: after[0], Check: true}}}})
	out := ApplyEdits(ctx, s, stale, false)
	var ce *EditCountError
	if !errors.As(out.Err, &ce) || ce.Got != 0 {
		t.Fatalf("stale edit = %+v", out)
	}
	if got := read("1")[0].Text; got != "99" {
		t.Fatalf("n = %s after a refused edit", got)
	}
	// Manual mode: edits join the open transaction and go with Rollback.
	if err := s.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	upd, _ := EditStatements(PostgresDialect{}, "", "Edit Probe", []RowEdit{{Kind: EditUpdate,
		Key:    []EditValue{{Column: "id", Value: Cell{Text: "1"}}},
		Values: []EditValue{{Column: "n", Value: Cell{Text: "1"}}}}})
	if out := ApplyEdits(ctx, s, upd, true); out.Err != nil {
		t.Fatalf("in tx: %+v", out)
	}
	if got := read("1")[0].Text; got != "1" {
		t.Fatalf("own transaction does not see its change: %s", got)
	}
	if err := s.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := read("1")[0].Text; got != "99" {
		t.Fatalf("rollback kept the change: %s", got)
	}
}
