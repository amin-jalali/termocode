package dbclient

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// SCN-24: the database connection drops while rows are arriving. Every
// engine says the same thing: the connection was lost mid-query, how many
// rows came before the drop, and that the result is not complete. A short
// result must never look like a whole one.

func TestConnLostKnowsDroppedConnections(t *testing.T) {
	for _, err := range []error{
		io.ErrUnexpectedEOF,
		driver.ErrBadConn,
		syscall.ECONNRESET,
		syscall.EPIPE,
		fmt.Errorf("read tcp 10.0.0.1:5432: %w", syscall.ECONNRESET),
		errors.New("unexpected EOF: connection reset by peer"),
		errors.New("write: broken pipe"),
		errors.New("invalid connection"),
		errors.New("use of closed network connection"),
	} {
		if !connLost(err) {
			t.Errorf("connLost(%q) = false, want true", err)
		}
	}
	for _, err := range []error{
		nil,
		errors.New(`ERROR: division by zero (SQLSTATE 22012)`),
		errors.New("Code: 62. DB::Exception: Syntax error. (SYNTAX_ERROR)"),
		context.Canceled,
	} {
		if connLost(err) {
			t.Errorf("connLost(%v) = true, want false", err)
		}
	}
}

func TestSQLSessionDropMidQuerySaysLostAndNotComplete(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"n"}, Rows: [][]driver.Value{{int64(1)}, {int64(2)}, {int64(3)}},
		RowsError: errors.New("unexpected EOF: connection reset by peer"), RowsBefore: 2}
	s := stubSession(t, "drop-lost", stub, PostgresDialect{}, "postgres")
	r := s.Run(context.Background(), "SELECT n FROM t", RunOptions{})
	if r.Err == nil {
		t.Fatalf("a dropped connection gave no error: %+v", r)
	}
	for _, want := range []string{"lost mid-query", "after 2 rows", "not complete", "connection reset"} {
		if !strings.Contains(r.Err.Error(), want) {
			t.Errorf("error %q lacks %q", r.Err, want)
		}
	}
}

// A server error that is not a dropped connection keeps its own text.
func TestSQLSessionServerErrorMidRowsIsNotCalledLost(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"n"}, Rows: [][]driver.Value{{int64(1)}, {int64(2)}},
		RowsError: errors.New("ERROR: division by zero (SQLSTATE 22012)"), RowsBefore: 1}
	s := stubSession(t, "drop-server", stub, PostgresDialect{}, "postgres")
	r := s.Run(context.Background(), "SELECT 1/n FROM t", RunOptions{})
	if r.Err == nil || !strings.Contains(r.Err.Error(), "division by zero") || strings.Contains(r.Err.Error(), "lost mid-query") {
		t.Fatalf("server error = %v", r.Err)
	}
}

// The ClickHouse server sends the header lines and one row, then the TCP
// connection closes before the chunked body ends.
func TestClickHouseDropMidQuerySaysLostAndNotComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) == "SELECT 1" {
			_, _ = io.WriteString(w, "[\"1\"]\n[\"UInt8\"]\n[1]\n")
			return
		}
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		chunk := "[\"n\"]\n[\"UInt64\"]\n[\"1\"]\n"
		fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n", len(chunk), chunk)
		_ = buf.Flush()
		_ = conn.Close()
	}))
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	n, _ := strconv.Atoi(port)
	s, err := NewClickHouseSession(context.Background(), Conn{ID: "ch", Driver: "clickhouse", Host: host, Port: n}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := s.Run(context.Background(), "SELECT n FROM big", RunOptions{})
	if r.Err == nil {
		t.Fatalf("a dropped connection gave no error: %+v", r)
	}
	for _, want := range []string{"lost mid-query", "after 1 row", "not complete"} {
		if !strings.Contains(r.Err.Error(), want) {
			t.Errorf("error %q lacks %q", r.Err, want)
		}
	}
}

// sqlite3 dies while it prints rows: the user hears it stopped mid-query
// with the rows read so far, and the next statement runs in a fresh
// process.
func TestSQLiteDropMidQuerySaysStoppedAndRestarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sqlite3 is a shell script")
	}
	script := `#!/bin/sh
if [ "$1" = "-version" ]; then
	echo '3.45.1 2024-01-30'
	exit 0
fi
while IFS= read -r line; do
	case "$line" in
	*FROM\ big*) printf '[{"n":1},\n{"n":2},\n'; exit 3 ;;
	SELECT*) printf '[{"ok":1}]\n' ;;
	.print\ __TC_END_*) printf '%s\n' "${line#*.print }" ;;
	*) : ;;
	esac
done
`
	bin := writeFakeSQLite(t, script)
	old := lookupSQLite
	lookupSQLite = func() (string, error) { return bin, nil }
	t.Cleanup(func() { lookupSQLite = old })
	s, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := s.Run(context.Background(), "SELECT n FROM big", RunOptions{})
	if r.Err == nil {
		t.Fatalf("a dead sqlite3 gave no error: %+v", r)
	}
	for _, want := range []string{"sqlite3 stopped mid-query", "after 2 rows", "not complete", "restarted"} {
		if !strings.Contains(r.Err.Error(), want) {
			t.Errorf("error %q lacks %q", r.Err, want)
		}
	}
	next := s.Run(context.Background(), "SELECT 1 AS ok", RunOptions{})
	if next.Err != nil || len(next.Rows) != 1 {
		t.Fatalf("statement after the restart = %+v", next)
	}
}
