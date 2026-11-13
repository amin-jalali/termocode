package dbclient

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type collectSink struct {
	cols []Column
	rows int
	fail error
}

func (c *collectSink) Columns(cols []Column) error { c.cols = cols; return c.fail }
func (c *collectSink) Row([]Cell) error            { c.rows++; return c.fail }

func TestStreamRowsRefusalsAndFailures(t *testing.T) {
	ctx := context.Background()
	f := (&fakeSQL{}).on("select", fakeSet{cols: []string{"n"}, rows: [][]driver.Value{{int64(1)}, {int64(2)}}})
	s := fakeSession(t, f, PostgresDialect{}, "postgres", true)
	sink := &collectSink{}
	if err := s.StreamRows(ctx, "SELECT 1", nil, sink); err != nil || sink.rows != 2 || len(sink.cols) != 1 {
		t.Errorf("stream = %d rows, %v", sink.rows, err)
	}
	if err := s.StreamRows(ctx, "SELECT 1", nil, &collectSink{fail: errors.New("sink full")}); err == nil || !strings.Contains(err.Error(), "sink full") {
		t.Errorf("a failing sink = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.StreamRows(cancelled, "SELECT 1", nil, &collectSink{}); err == nil {
		t.Error("a cancelled stream: want error")
	}
	bad := (&fakeSQL{}).fail("select", errFake)
	if err := fakeSession(t, bad, PostgresDialect{}, "postgres", false).StreamRows(ctx, "SELECT 1", nil, &collectSink{}); err == nil {
		t.Error("a failing query: want error")
	}
	rofail := (&fakeSQL{}).fail("begin", errFake).fail("set transaction", errFake)
	if err := fakeSession(t, rofail, PostgresDialect{}, "postgres", true).StreamRows(ctx, "SELECT 1", nil, &collectSink{}); err == nil {
		t.Error("a failing read-only begin: want error")
	}
	closed := fakeSession(t, &fakeSQL{}, PostgresDialect{}, "postgres", false)
	_ = closed.db.Close()
	if err := closed.StreamRows(ctx, "SELECT 1", nil, &collectSink{}); err == nil {
		t.Error("a closed pool: want error")
	}
	_ = closed.Close()
	if err := closed.StreamRows(ctx, "SELECT 1", nil, &collectSink{}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("a closed session = %v", err)
	}
}

func TestOpenCSVAsTableRefusals(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "in.csv")
	if err := os.WriteFile(csvPath, []byte("a,b\n1,2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "out.db")
	if err := os.WriteFile(db, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCSVAsTable(context.Background(), csvPath, db); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("an existing database = %v", err)
	}
	if b, _ := os.ReadFile(db); len(b) != 0 {
		t.Error("an existing file is never touched")
	}
	if _, err := OpenCSVAsTable(context.Background(), filepath.Join(dir, "none.csv"), filepath.Join(dir, "x.db")); err == nil {
		t.Error("a missing CSV: want error")
	}
	if _, err := OpenCSVAsTable(context.Background(), csvPath, filepath.Join(dir, "no", "such", "x.db")); err == nil {
		t.Error("a folder that is not there: want error")
	}
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCSVAsTable(context.Background(), csvPath, filepath.Join(blocker, "x.db")); err == nil {
		t.Error("a path under a file: want error")
	}
	old := lookupSQLite
	lookupSQLite = func() (string, error) { return "", os.ErrNotExist }
	defer func() { lookupSQLite = old }()
	target := filepath.Join(dir, "new.db")
	if _, err := OpenCSVAsTable(context.Background(), csvPath, target); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("no sqlite3 = %v", err)
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("a failed load removes the new database again")
	}
}

func TestHTTPEngineCancelCloseAndDocsOfOtherShapes(t *testing.T) {
	block := make(chan struct{})
	srv := newBlockingServer(t, block)
	e, err := newHTTPEngine("t", Conn{URL: srv}, "")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := e.do(context.Background(), "GET", "/slow", nil, "")
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	e.Cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "canceled") {
			t.Errorf("a cancelled request = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Cancel did not end the request")
	}
	e.Cancel() // nothing running
	close(block)
	_ = e.Close()
	docs := []json.RawMessage{json.RawMessage(`[1,2]`), json.RawMessage(`"text"`), json.RawMessage(`{"a":1}`), json.RawMessage(`null`)}
	r := DocsResult(docs, 0)
	if len(r.Rows) != 4 || r.Columns[0].Name != "value" || len(r.Columns) != 2 {
		t.Errorf("odd documents = %+v", r)
	}
	if cut := DocsResult(docs, 2); !cut.Truncated || len(cut.Rows) != 2 {
		t.Errorf("a cut = %+v", cut)
	}
}

func TestLocalRelayStopsAndRefusesLateConnections(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() { _, _ = b.Write([]byte("hi")); _ = b.Close() }()
		return a, nil
	}
	addr, stop, err := localRelay(dial, "target:1")
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	_, _ = c.Read(buf)
	_ = c.Close()
	stop()
	stop() // twice is fine
	if _, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		t.Error("a stopped relay accepts no connection")
	}
}

func newBlockingServer(t *testing.T, release chan struct{}) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
