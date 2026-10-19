package dbtest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
)

// Stub records statements and returns a configurable result for SQL session tests.
type Stub struct {
	mu          sync.Mutex
	Statements  []string
	Args        [][]any // bound arguments, parallel to Statements
	Columns     []string
	ColumnTypes []string
	Rows        [][]driver.Value
	RepeatRows  int
	RowsRead    int
	ExecError   error
	QueryError  error
	ExecRows    int64
	Begins      int
	Commits     int
	Rollbacks   int
}

func (s *Stub) Recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.Statements...)
}

// RecordedArgs returns the bound arguments of each statement, parallel to Recorded.
func (s *Stub) RecordedArgs() [][]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]any(nil), s.Args...)
}

func (s *Stub) TransactionCounts() (begins, commits, rollbacks int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Begins, s.Commits, s.Rollbacks
}

func (s *Stub) ReadCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.RowsRead
}

var registry = struct {
	sync.Mutex
	stubs map[string]*Stub
}{stubs: make(map[string]*Stub)}

func Open(name string, stub *Stub) *sql.DB {
	registry.Lock()
	registry.stubs[name] = stub
	registry.Unlock()
	db, err := sql.Open("termocode-dbtest", name)
	if err != nil {
		panic(err)
	}
	return db
}

func init() { sql.Register("termocode-dbtest", stubDriver{}) }

type stubDriver struct{}

func (stubDriver) Open(name string) (driver.Conn, error) {
	registry.Lock()
	s := registry.stubs[name]
	registry.Unlock()
	if s == nil {
		return nil, errors.New("unknown dbtest stub")
	}
	return &stubConn{stub: s}, nil
}

type stubConn struct{ stub *Stub }

func (c *stubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *stubConn) Close() error { return nil }
func (c *stubConn) Begin() (driver.Tx, error) {
	c.stub.mu.Lock()
	c.stub.Begins++
	c.stub.mu.Unlock()
	return &stubTx{stub: c.stub}, nil
}
func (c *stubConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c *stubConn) Ping(context.Context) error { return nil }
func (c *stubConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.stub.mu.Lock()
	defer c.stub.mu.Unlock()
	c.stub.record(query, args)
	if c.stub.ExecError != nil {
		return nil, c.stub.ExecError
	}
	return driver.RowsAffected(c.stub.ExecRows), nil
}
func (s *Stub) record(query string, args []driver.NamedValue) {
	s.Statements = append(s.Statements, query)
	vals := make([]any, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	s.Args = append(s.Args, vals)
}

func (c *stubConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.stub.mu.Lock()
	defer c.stub.mu.Unlock()
	c.stub.record(query, args)
	if c.stub.QueryError != nil {
		return nil, c.stub.QueryError
	}
	return &stubRows{stub: c.stub, columns: append([]string(nil), c.stub.Columns...), types: append([]string(nil), c.stub.ColumnTypes...), rows: append([][]driver.Value(nil), c.stub.Rows...), repeatRows: c.stub.RepeatRows}, nil
}

var _ driver.Conn = (*stubConn)(nil)
var _ driver.ExecerContext = (*stubConn)(nil)
var _ driver.QueryerContext = (*stubConn)(nil)
var _ driver.Pinger = (*stubConn)(nil)
var _ driver.ConnBeginTx = (*stubConn)(nil)

type stubTx struct{ stub *Stub }

func (tx stubTx) Commit() error {
	tx.stub.mu.Lock()
	tx.stub.Commits++
	tx.stub.mu.Unlock()
	return nil
}
func (tx stubTx) Rollback() error {
	tx.stub.mu.Lock()
	tx.stub.Rollbacks++
	tx.stub.mu.Unlock()
	return nil
}

type stubRows struct {
	stub       *Stub
	columns    []string
	types      []string
	rows       [][]driver.Value
	i          int
	repeatRows int
}

func (r *stubRows) Columns() []string { return r.columns }
func (r *stubRows) ColumnTypeDatabaseTypeName(index int) string {
	if index < len(r.types) {
		return r.types[index]
	}
	return ""
}
func (r *stubRows) Close() error { return nil }
func (r *stubRows) Next(dst []driver.Value) error {
	if r.repeatRows > 0 {
		if r.i >= r.repeatRows || len(r.rows) == 0 {
			return io.EOF
		}
		copy(dst, r.rows[0])
		r.i++
		r.stub.mu.Lock()
		r.stub.RowsRead++
		r.stub.mu.Unlock()
		return nil
	}
	if r.i >= len(r.rows) {
		return io.EOF
	}
	copy(dst, r.rows[r.i])
	r.i++
	r.stub.mu.Lock()
	r.stub.RowsRead++
	r.stub.mu.Unlock()
	return nil
}
