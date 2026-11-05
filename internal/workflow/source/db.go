package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Db source limits.
const (
	DefaultDBTimeout = 30 * time.Second
	MaxDBTimeout     = 5 * time.Minute
)

// ErrNotReadOnly is returned for SQL that is not one read statement.
// Nothing was opened or run.
var ErrNotReadOnly = errors.New("the SQL is not a single read-only statement")

// DBOpener opens a session for a saved connection name.
//
// Contract: the session is read-only on the server side (the same
// session the AI's database tools use), and the opener resolves the
// connection's password itself. dialect is the driver name for the SQL
// guard ("postgres", "mysql", "sqlite", ...).
type DBOpener interface {
	OpenReadOnly(ctx context.Context, conn string) (sess dbclient.Session, dialect string, err error)
}

// DBInput is a `"from": "db"` source.
type DBInput struct {
	// Conn is the saved connection name.
	Conn string
	// SQL is one read-only statement.
	SQL string
	// Dialect is the driver name for the early SQL check ("" is the
	// strictest); the opener's own dialect is checked again.
	Dialect string
	// Args are bound to the statement's placeholders.
	Args []any
	// Timeout ends the query (0: DefaultDBTimeout).
	Timeout time.Duration
	// Shape: Items is not used (every row is an item, an object keyed by
	// column name; values are text, NULL is null).
	Shape
}

// DB runs a read-only query and returns one item per row.
func (s *Sources) DB(ctx context.Context, in DBInput) (Result, error) {
	if s.Databases == nil {
		return Result{}, errors.New("db: no database is available")
	}
	if strings.TrimSpace(in.Conn) == "" {
		return Result{}, errors.New("db: conn is empty")
	}
	if strings.Contains(in.SQL, "${") {
		return Result{}, errors.New("db: sql holds a ${…} reference; references are not allowed")
	}
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = DefaultDBTimeout
	}
	if timeout > MaxDBTimeout {
		return Result{}, fmt.Errorf("db: timeout %s is over the limit of %s", timeout, MaxDBTimeout)
	}
	sh := in.Shape
	sh.Items = ""
	col, err := newCollector(sh)
	if err != nil {
		return Result{}, fmt.Errorf("db: %w", err)
	}
	if !readOnlySQL(in.SQL, in.Dialect) {
		return Result{}, fmt.Errorf("db: %w", ErrNotReadOnly)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	sess, dialect, err := s.Databases.OpenReadOnly(ctx, in.Conn)
	if err != nil {
		return Result{}, errors.New(scrubShapes("db: " + in.Conn + ": " + err.Error()))
	}
	// The read-only session is the real guard; this check refuses early,
	// before the connection is even used.
	if !readOnlySQL(in.SQL, dialect) {
		_ = sess.Close()
		return Result{}, fmt.Errorf("db: %w", ErrNotReadOnly)
	}
	defer sess.Close()
	res := sess.Run(ctx, in.SQL, dbclient.RunOptions{MaxRows: col.max + 1, Args: in.Args})
	if res.Err != nil {
		return Result{}, errors.New(scrubShapes("db: " + res.Err.Error()))
	}
	for _, row := range res.Rows {
		data, err := rowJSON(res.Columns, row)
		if err != nil {
			return Result{}, errors.New(scrubShapes("db: " + err.Error()))
		}
		if err := col.add(data, ""); err != nil {
			return Result{}, errors.New(scrubShapes("db: " + err.Error()))
		}
	}
	if res.Truncated && !col.res.Truncated {
		col.res.Truncated = true
	}
	return col.res, nil
}

// rowJSON makes {"col":"text",...} in column order.
func rowJSON(cols []dbclient.Column, row []dbclient.Cell) (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, c := range row {
		name := fmt.Sprintf("col%d", i+1)
		if i < len(cols) && cols[i].Name != "" {
			name = cols[i].Name
		}
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(name)
		b.Write(k)
		b.WriteByte(':')
		if c.Null {
			b.WriteString("null")
			continue
		}
		v, err := json.Marshal(c.Text)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// readOnlySQL is true for exactly one statement that dbclient.Classify
// calls a read.
func readOnlySQL(sql, dialect string) bool {
	kind, stmts := dbclient.Classify(sql, dialect)
	return kind == dbclient.KindRead && len(stmts) == 1
}
