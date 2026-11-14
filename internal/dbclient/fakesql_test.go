package dbclient

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeSQL is a scripted database/sql driver for session tests: each
// registered rule answers the statements that start with its prefix
// (case-insensitive, the first matching rule wins). It records every
// statement it ran.
type fakeSQL struct {
	mu    sync.Mutex
	rules []fakeRule
	log   []string
}

type fakeRule struct {
	prefix string
	sets   []fakeSet // one per result set
	err    error
}

type fakeSet struct {
	cols []string
	rows [][]driver.Value
}

var fakeSeq atomic.Int64

// open registers a driver for f and returns a pool on it.
func (f *fakeSQL) open(t *testing.T) *sql.DB {
	t.Helper()
	name := "fakesql-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + strconv.FormatInt(fakeSeq.Add(1), 10)
	sql.Register(name, fakeDriver{f})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func (f *fakeSQL) on(prefix string, sets ...fakeSet) *fakeSQL {
	f.rules = append(f.rules, fakeRule{prefix: strings.ToLower(prefix), sets: sets})
	return f
}

func (f *fakeSQL) fail(prefix string, err error) *fakeSQL {
	f.rules = append(f.rules, fakeRule{prefix: strings.ToLower(prefix), err: err})
	return f
}

func (f *fakeSQL) ran() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

func (f *fakeSQL) answer(q string) (*fakeRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, q)
	low := strings.ToLower(strings.TrimSpace(q))
	for i := range f.rules {
		if strings.HasPrefix(low, f.rules[i].prefix) {
			if f.rules[i].err != nil {
				return nil, f.rules[i].err
			}
			return &f.rules[i], nil
		}
	}
	return &fakeRule{}, nil
}

type fakeDriver struct{ f *fakeSQL }

func (d fakeDriver) Open(string) (driver.Conn, error) { return &fakeConn{d.f}, nil }

type fakeConn struct{ f *fakeSQL }

func (c *fakeConn) Prepare(q string) (driver.Stmt, error) { return &fakeStmt{c.f, q}, nil }
func (c *fakeConn) Close() error                          { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)             { return fakeTx{}, nil }

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

type fakeStmt struct {
	f *fakeSQL
	q string
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }
func (s *fakeStmt) Exec([]driver.Value) (driver.Result, error) {
	if _, err := s.f.answer(s.q); err != nil {
		return nil, err
	}
	return driver.RowsAffected(1), nil
}
func (s *fakeStmt) Query([]driver.Value) (driver.Rows, error) {
	r, err := s.f.answer(s.q)
	if err != nil {
		return nil, err
	}
	return &fakeRows{sets: r.sets}, nil
}

type fakeRows struct {
	sets []fakeSet
	set  int
	row  int
}

func (r *fakeRows) cur() fakeSet {
	if r.set < len(r.sets) {
		return r.sets[r.set]
	}
	return fakeSet{}
}
func (r *fakeRows) Columns() []string { return r.cur().cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	s := r.cur()
	if r.row >= len(s.rows) {
		return io.EOF
	}
	copy(dest, s.rows[r.row])
	r.row++
	return nil
}
func (r *fakeRows) HasNextResultSet() bool { return r.set+1 < len(r.sets) }
func (r *fakeRows) NextResultSet() error {
	if r.set+1 >= len(r.sets) {
		return io.EOF
	}
	r.set++
	r.row = 0
	return nil
}

var errFake = errors.New("fake database error")
