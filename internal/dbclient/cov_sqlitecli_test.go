package dbclient

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeSQLiteScript is a sqlite3 stand-in that speaks the line protocol of
// the session: it prints the sentinel of each ".print", and answers the
// statements whose text holds a marker.
const fakeSQLiteScript = `#!/bin/sh
if [ "$1" = "-version" ]; then echo "3.45.0 2024-01-01 abcdef"; exit 0; fi
while IFS= read -r line; do
  case "$line" in
    .print*) echo "${line#.print }" ;;
    *many*) printf '[{"a":1},\n{"a":2},\n{"a":3},\n{"a":4}]\n' ;;
    *oneerr*) echo 'Parse error near line 1: near "x": syntax error' ;;
    *die*) exit 1 ;;
    *hang*) sleep 30 ;;
    *insert*) echo 'changes:  3   total_changes: 3' ;;
  esac
done
`

func fakeSQLiteSession(t *testing.T) *SQLiteSession {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	bin := filepath.Join(t.TempDir(), "sqlite3")
	if err := os.WriteFile(bin, []byte(fakeSQLiteScript), 0o700); err != nil {
		t.Fatal(err)
	}
	old := lookupSQLite
	lookupSQLite = func() (string, error) { return bin, nil }
	t.Cleanup(func() { lookupSQLite = old })
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSQLiteSessionOverTheLineProtocol(t *testing.T) {
	s := fakeSQLiteSession(t)
	ctx := context.Background()
	r := s.Run(ctx, "SELECT many", RunOptions{})
	if r.Err != nil || len(r.Rows) != 4 {
		t.Fatalf("all rows = %+v", r)
	}
	cut := s.Run(ctx, "SELECT many", RunOptions{MaxRows: 2})
	if cut.Err != nil || len(cut.Rows) != 2 || !cut.Truncated {
		t.Errorf("a cut result = %+v", cut)
	}
	if r := s.Run(ctx, "SELECT oneerr", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "syntax error") || strings.Contains(r.Err.Error(), "near line") {
		t.Errorf("sqlite's error is shown without its line = %v", r.Err)
	}
	ins := s.Run(ctx, "INSERT into insert_t values (1)", RunOptions{})
	if ins.Err != nil || ins.Affected != 3 {
		t.Errorf("insert = %+v", ins)
	}
	// The process dies in the middle of a query: the error says so and the
	// next statement runs on a new process.
	dead := s.Run(ctx, "SELECT die", RunOptions{})
	if dead.Err == nil || !strings.Contains(dead.Err.Error(), "restarted") {
		t.Errorf("a dying process = %+v", dead)
	}
	if r := s.Run(ctx, "SELECT many", RunOptions{}); r.Err != nil || len(r.Rows) != 4 {
		t.Errorf("after the restart = %+v", r)
	}
	// A cancel while the statement runs ends the wait and restarts.
	cctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	hung := s.Run(cctx, "SELECT hang", RunOptions{})
	if hung.Err == nil || time.Since(start) > 10*time.Second {
		t.Errorf("a hanging statement = %+v after %v", hung.Err, time.Since(start))
	}
	if r := s.Run(ctx, "SELECT many", RunOptions{MaxRows: 1}); r.Err != nil || len(r.Rows) != 1 {
		t.Errorf("after the cancel = %+v", r)
	}
	if r := s.Run(ctx, "SELECT 1; SELECT 2", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "one SQL statement") {
		t.Errorf("two statements = %v", r.Err)
	}
	if r := s.Run(ctx, "SELECT 'open", RunOptions{}); r.Err == nil {
		t.Error("an open quote: want error")
	}
	if r := s.Run(ctx, "SELECT 1\n.shell id", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "shell commands") {
		t.Errorf("a dot command = %v", r.Err)
	}
	done, doneCancel := context.WithCancel(ctx)
	doneCancel()
	if r := s.Run(done, "SELECT many", RunOptions{}); r.Err == nil {
		t.Error("a cancelled context: want error")
	}
	_ = s.Close()
	if r := s.Run(ctx, "SELECT many", RunOptions{}); r.Err == nil {
		t.Error("a closed session: want error")
	}
}

func TestSQLiteStartFailuresAndLines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "sqlite3")
	old := lookupSQLite
	lookupSQLite = func() (string, error) { return bin, nil }
	defer func() { lookupSQLite = old }()
	// An old sqlite3 is refused for a read-only (AI) session.
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho '3.36.0 2021'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	vdir := t.TempDir()
	old2 := filepath.Join(vdir, "sqlite3")
	if err := os.WriteFile(old2, []byte("#!/bin/sh\necho '3.36.0 2021'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	lookupSQLite = func() (string, error) { return old2, nil }
	if _, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:", ReadOnly: true}); err == nil || !strings.Contains(err.Error(), "3.37") {
		t.Errorf("an old sqlite3 for a read-only session = %v", err)
	}
	lookupSQLite = func() (string, error) { return "", os.ErrNotExist }
	if _, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("no sqlite3 = %v", err)
	}
	for in, want := range map[string]bool{"Error: x": true, "  Parse error: y": true, "Runtime error: z": true, "fine": false, "": false} {
		if isSQLiteError(in) != want {
			t.Errorf("isSQLiteError(%q)", in)
		}
	}
	if got := cleanSQLiteError("Parse error near line 3: bad"); got != "Parse error: bad" {
		t.Errorf("cleanSQLiteError = %q", got)
	}
	if parseChanges([]string{"x", "changes:   7   total_changes: 9"}) != 7 || parseChanges([]string{"changes:"}) != 0 || parseChanges(nil) != 0 {
		t.Error("parseChanges")
	}
	s := &SQLiteSession{}
	s.transaction = true
	s.markSessionRestarted(true)
	if n := s.takeRestartNotice(); !strings.Contains(n, "cancelled") || !strings.Contains(n, "rolled back") || s.takeRestartNotice() != "" {
		t.Errorf("restart notice = %q", n)
	}
	s.markSessionRestarted(false)
	if n := s.takeRestartNotice(); !strings.Contains(n, "stopped") {
		t.Errorf("stop notice = %q", n)
	}
	e := &cancelMaybeFinishedError{cause: context.Canceled}
	if !strings.Contains(e.Error(), "may already have finished") || e.Unwrap() != context.Canceled {
		t.Error("cancelMaybeFinishedError")
	}
}
