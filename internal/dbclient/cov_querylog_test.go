package dbclient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// logStubSession answers every call with fixed values.
type logStubSession struct {
	runErr, txErr, introErr, roErr error
	ro                             Session
	closed                         int
}

func (s *logStubSession) Ping(context.Context) error { return nil }
func (s *logStubSession) Introspect(context.Context, IntrospectionRequest) (IntrospectionResult, error) {
	return IntrospectionResult{}, s.introErr
}
func (s *logStubSession) Run(context.Context, string, RunOptions) Result {
	return Result{Rows: [][]Cell{{{Text: "1"}}}, Affected: 2, Err: s.runErr}
}
func (s *logStubSession) Begin(context.Context) error    { return s.txErr }
func (s *logStubSession) Commit(context.Context) error   { return s.txErr }
func (s *logStubSession) Rollback(context.Context) error { return s.txErr }
func (s *logStubSession) ReadOnlySession() (Session, error) {
	return s.ro, s.roErr
}
func (s *logStubSession) Cancel()      {}
func (s *logStubSession) Close() error { s.closed++; return nil }

func TestQueryLogRecordsEveryCallAndIsNilSafe(t *testing.T) {
	var nilLog *QueryLog
	nilLog.Add(QueryLogEntry{})
	nilLog.Clear()
	if nilLog.Entries() != nil {
		t.Error("a nil log has no entries")
	}
	if LogSession(nil, NewQueryLog(5), "c") != nil {
		t.Error("no session, no wrapper")
	}
	stub := &logStubSession{}
	if LogSession(stub, nil, "c") != Session(stub) {
		t.Error("no log, no wrapper")
	}
	log := NewQueryLog(3)
	s := LogSession(stub, log, "conn")
	if LogSession(s, log, "conn") != s {
		t.Error("an already wrapped session is not wrapped twice")
	}
	ctx := context.Background()
	s.Run(ctx, "SELECT 1", RunOptions{})
	stub.runErr = errors.New("boom\nline two")
	s.Run(ctx, "SELECT 2", RunOptions{})
	_, _ = s.Introspect(ctx, IntrospectionRequest{Kind: "table", Database: "d", Object: "t"})
	_ = s.Begin(ctx)
	stub.txErr = errors.New("no tx")
	_ = s.Commit(ctx)
	_ = s.Rollback(ctx)
	got := log.Entries()
	if len(got) != 3 || got[0].SQL != "BEGIN" || got[1].SQL != "COMMIT" || got[2].SQL != "ROLLBACK" || got[1].Err == "" || got[0].Err != "" {
		t.Fatalf("the log keeps the last 3 calls: %+v", got)
	}
	text := FormatQueryLog([]QueryLogEntry{
		{At: time.Now(), Conn: "a\nb", SQL: "SELECT 1\n", Rows: 3, Dur: time.Second},
		{At: time.Now(), SQL: "UPDATE t", Affected: 4},
		{At: time.Now(), SQL: "X", Err: "bad\nthing", Internal: true},
	})
	for _, want := range []string{"3 rows", "4 affected", "error: bad thing", "internal", "a b"} {
		if !strings.Contains(text, want) {
			t.Errorf("log text lacks %q:\n%s", want, text)
		}
	}
	log.Clear()
	if len(log.Entries()) != 0 {
		t.Error("Clear")
	}
	// A read-only twin logs as internal; a failing twin is passed on.
	twin := &logStubSession{}
	stub.ro = twin
	ro, err := s.ReadOnlySession()
	if err != nil || ro == nil {
		t.Fatalf("read-only = %v %v", ro, err)
	}
	ro.Run(ctx, "SELECT 9", RunOptions{})
	if e := log.Entries(); len(e) != 1 || !e[0].Internal {
		t.Errorf("a read-only twin logs as internal: %+v", e)
	}
	stub.roErr = errors.New("no twin")
	if _, err := s.ReadOnlySession(); err == nil {
		t.Error("a failing twin: want error")
	}
	stub.roErr, stub.ro = nil, nil
	if r, err := s.ReadOnlySession(); err != nil || r != nil {
		t.Errorf("no twin = %v %v", r, err)
	}
	// OnClose runs once the session is closed; Unwrap finds the inner session.
	stopped := 0
	closer := OnClose(s, func() { stopped++ })
	if OnClose(s, nil) != s || OnClose(nil, func() {}) != nil {
		t.Error("OnClose needs both")
	}
	_ = closer.Close()
	if stopped != 1 || stub.closed != 1 {
		t.Errorf("close ran stop %d times, closed %d", stopped, stub.closed)
	}
	if UnwrapSession(closer) != Session(stub) {
		t.Error("UnwrapSession finds the session under both wrappers")
	}
	if UnwrapSession(stub) != Session(stub) {
		t.Error("a plain session is itself")
	}
}
