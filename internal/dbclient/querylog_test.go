package dbclient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type logFake struct{ ro bool }

func (logFake) Ping(context.Context) error { return nil }
func (logFake) Introspect(context.Context, IntrospectionRequest) (IntrospectionResult, error) {
	return IntrospectionResult{}, nil
}
func (f logFake) Run(_ context.Context, sql string, _ RunOptions) Result {
	if strings.HasPrefix(sql, "bad") {
		return Result{Err: errors.New("syntax error")}
	}
	return Result{Rows: [][]Cell{{{Text: "1"}}, {{Text: "2"}}}}
}
func (logFake) Begin(context.Context) error    { return nil }
func (logFake) Commit(context.Context) error   { return nil }
func (logFake) Rollback(context.Context) error { return nil }
func (logFake) ReadOnlySession() (Session, error) {
	return logFake{ro: true}, nil
}
func (logFake) Cancel()      {}
func (logFake) Close() error { return nil }

// DBX-48: every statement a session sends is in the query log, the
// internal ones (read-only sessions, catalog reads) marked as such.
func TestQueryLogSession(t *testing.T) {
	l := NewQueryLog(3)
	s := LogSession(logFake{}, l, "pg")
	ctx := context.Background()
	s.Run(ctx, "select 1", RunOptions{})
	s.Run(ctx, "bad sql", RunOptions{})
	_ = s.Begin(ctx)
	ro, _ := s.ReadOnlySession()
	ro.Run(ctx, "select name from catalog", RunOptions{})
	_, _ = s.Introspect(ctx, IntrospectionRequest{Schema: "public", Kind: "tables"})
	got := l.Entries()
	if len(got) != 3 { // the log keeps the newest 3
		t.Fatalf("entries %d: %+v", len(got), got)
	}
	if got[0].SQL != "BEGIN" || got[1].SQL != "select name from catalog" || !got[1].Internal || !strings.Contains(got[2].SQL, "tables") || !got[2].Internal {
		t.Errorf("entries %+v", got)
	}
	if UnwrapSession(s) != (logFake{}) || UnwrapSession(logFake{}) != (logFake{}) {
		t.Error("unwrap")
	}
	l2 := NewQueryLog(10)
	s2 := LogSession(logFake{}, l2, "pg")
	s2.Run(ctx, "select 1", RunOptions{})
	s2.Run(ctx, "bad sql", RunOptions{})
	e := l2.Entries()
	if e[0].Rows != 2 || e[0].Err != "" || e[1].Err != "syntax error" || e[0].Conn != "pg" || e[0].At.IsZero() {
		t.Errorf("entries %+v", e)
	}
	l2.Clear()
	if len(l2.Entries()) != 0 {
		t.Error("clear")
	}
}

// The log text: one header line per statement (time, connection,
// duration, outcome, internal mark), then the statement indented.
func TestFormatQueryLog(t *testing.T) {
	at := time.Date(2026, 11, 7, 10, 4, 5, 0, time.Local)
	text := FormatQueryLog([]QueryLogEntry{
		{At: at, Conn: "pg", SQL: "select 1\nfrom t", Dur: 12 * time.Millisecond, Rows: 2},
		{At: at, Conn: "pg", SQL: "bad", Err: "syntax error", Internal: true},
	})
	for _, w := range []string{
		"-- 2026-11-07 10:04:05.000 · pg · 12ms · 2 rows\nselect 1\nfrom t\n",
		"-- 2026-11-07 10:04:05.000 · pg · 0s · error: syntax error · internal\nbad\n",
	} {
		if !strings.Contains(text, w) {
			t.Errorf("missing %q in\n%s", w, text)
		}
	}
}
