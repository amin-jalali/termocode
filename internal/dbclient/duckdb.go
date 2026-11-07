package dbclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// DuckDBSession runs the duckdb command line client, like SQLite uses
// sqlite3: no cgo driver. Each statement is one "duckdb -json -c" run on
// the database file, so the session keeps no state between runs (no
// transactions, and ":memory:" forgets every run).
//
// Read-only is the client's -readonly flag: the file is opened read-only,
// which the database engine enforces.
type DuckDBSession struct {
	bin      string
	path     string
	readonly bool

	mu     sync.Mutex
	cancel context.CancelFunc
	closed bool
}

// lookupDuckDB finds the duckdb client (a variable for tests).
var lookupDuckDB = func() (string, error) { return exec.LookPath("duckdb") }

var errDuckDBNoTx = errors.New("DuckDB runs each statement on its own here; transactions across runs are not kept")

// NewDuckDBSession checks the client and opens c's database file.
func NewDuckDBSession(ctx context.Context, c Conn) (*DuckDBSession, error) {
	bin, err := lookupDuckDB()
	if err != nil {
		return nil, errors.New("duckdb is not installed. Install the DuckDB command line client and put it on PATH")
	}
	if c.Path == "" {
		return nil, errors.New("duckdb path is required")
	}
	s := &DuckDBSession{bin: bin, path: c.Path, readonly: c.ReadOnly}
	if err := s.Ping(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *DuckDBSession) Ping(ctx context.Context) error {
	return s.Run(ctx, "SELECT 1", RunOptions{MaxRows: 1}).Err
}

func (s *DuckDBSession) Run(ctx context.Context, query string, opts RunOptions) Result {
	start := time.Now()
	res := s.run(ctx, query, opts)
	res.Elapsed = time.Since(start)
	return res
}

// duckdbArgs is the command line of one run. The statement goes on stdin,
// never in the arguments, so it does not show in the process list.
func duckdbArgs(path string, readonly bool) []string {
	args := []string{"-json", "-bail", "-init", os.DevNull}
	if readonly {
		args = append(args, "-readonly")
	}
	return append(args, path)
}

func (s *DuckDBSession) run(ctx context.Context, query string, opts RunOptions) Result {
	sql, err := inlineArgs(query, opts.Args)
	if err != nil {
		return Result{Err: err}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Result{Err: errors.New("session is closed")}
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()
	defer cancel()

	cmd := spawn.CommandContext(ctx, s.bin, duckdbArgs(s.path, s.readonly)...)
	cmd.Stdin = strings.NewReader(sql)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{Err: errors.New("the query was cancelled")}
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return Result{Err: errors.New(Redact(msg))}
	}
	return readDuckDBJSON(stdout.Bytes(), opts.MaxRows)
}

// readDuckDBJSON reads the -json output: one JSON array of row objects
// per statement that returns rows. The last array is the result. Object
// keys are read in order, so the columns keep the query's order.
func readDuckDBJSON(out []byte, maxRows int) Result {
	var res Result
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return res
		}
		if err != nil {
			return Result{Err: fmt.Errorf("could not read the DuckDB result: %w", err)}
		}
		if d, ok := tok.(json.Delim); !ok || d != '[' {
			return Result{Err: fmt.Errorf("could not read the DuckDB result: unexpected %v", tok)}
		}
		res = Result{}
		for dec.More() {
			names, row, err := readDuckDBRow(dec)
			if err != nil {
				return Result{Err: fmt.Errorf("could not read the DuckDB result: %w", err)}
			}
			if res.Columns == nil {
				for _, n := range names {
					res.Columns = append(res.Columns, Column{Name: n})
				}
			}
			if maxRows > 0 && len(res.Rows) >= maxRows {
				res.Truncated = true
				continue
			}
			res.Rows = append(res.Rows, row)
		}
		if _, err := dec.Token(); err != nil { // the closing ]
			return Result{Err: fmt.Errorf("could not read the DuckDB result: %w", err)}
		}
	}
}

func readDuckDBRow(dec *json.Decoder) ([]string, []Cell, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, nil, fmt.Errorf("unexpected %v", tok)
	}
	var names []string
	var row []Cell
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		name, _ := k.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		names = append(names, name)
		row = append(row, clickHouseCell(v))
	}
	if _, err := dec.Token(); err != nil { // the closing }
		return nil, nil, err
	}
	return names, row, nil
}

// inlineArgs puts args into the ? placeholders of query as SQL literals
// (the client has no bind API). A ? inside a string, a quoted name or a
// comment is left alone. Only catalog queries of this package pass args.
func inlineArgs(query string, args []any) (string, error) {
	if len(args) == 0 {
		return query, nil
	}
	var b strings.Builder
	n := 0
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case c == '\'' || c == '"':
			j := i + 1
			for j < len(query) {
				if query[j] == c {
					if j+1 < len(query) && query[j+1] == c {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j >= len(query) {
				j = len(query) - 1
			}
			b.WriteString(query[i : j+1])
			i = j
		case c == '-' && i+1 < len(query) && query[i+1] == '-':
			j := strings.IndexByte(query[i:], '\n')
			if j < 0 {
				j = len(query) - i - 1
			}
			b.WriteString(query[i : i+j+1])
			i += j
		case c == '?':
			if n >= len(args) {
				return "", errors.New("more placeholders than arguments")
			}
			b.WriteString(sqlLiteral(args[n]))
			n++
		default:
			b.WriteByte(c)
		}
	}
	if n != len(args) {
		return "", errors.New("more arguments than placeholders")
	}
	return b.String(), nil
}

// sqlLiteral is v as a standard SQL literal.
func sqlLiteral(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case bool:
		if x {
			return "TRUE"
		}
		return "FALSE"
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return "'" + strings.ReplaceAll(fmt.Sprint(v), "'", "''") + "'"
}

// Introspect lists the schemas of the file's own catalog at the top (the
// tree's database level), the tables and views of one schema, then the
// columns and DDL of one object.
func (s *DuckDBSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	q := DuckDBDialect{}.CatalogQueries()
	if req.Schema == "" {
		req.Schema = req.Database
	}
	if req.Schema == "" && req.Object == "" {
		return out, readFirstColumn(ctx, s, q["databases"], nil, &out.Databases)
	}
	if req.Schema == "" {
		req.Schema = "main"
	}
	if req.Object == "" {
		r := s.Run(ctx, q["objects"], RunOptions{MaxRows: 10000, Args: []any{req.Schema}})
		if r.Err != nil {
			return out, r.Err
		}
		for _, row := range r.Rows {
			if len(row) > 1 {
				out.Objects = append(out.Objects, SchemaObject{Name: row[0].Text, Kind: row[1].Text})
			}
		}
		return out, nil
	}
	r := s.Run(ctx, q["columns"], RunOptions{MaxRows: 10000, Args: []any{req.Schema, req.Object}})
	if r.Err != nil {
		return out, r.Err
	}
	for _, row := range r.Rows {
		if len(row) > 1 {
			out.Columns = append(out.Columns, Column{Name: row[0].Text, Type: row[1].Text, Table: req.Object})
		}
	}
	var ddl []string
	if err := readFirstColumn(ctx, s, q["ddl"], []any{req.Schema, req.Object, req.Schema, req.Object}, &ddl); err != nil {
		return out, err
	}
	if len(ddl) > 0 {
		out.DDL = ddl[0]
	}
	return out, nil
}

// readFirstColumn appends the first column of every row of query.
func readFirstColumn(ctx context.Context, s interface {
	Run(context.Context, string, RunOptions) Result
}, query string, args []any, dst *[]string) error {
	r := s.Run(ctx, query, RunOptions{MaxRows: 10000, Args: args})
	if r.Err != nil {
		return r.Err
	}
	for _, row := range r.Rows {
		if len(row) > 0 && !row[0].Null {
			*dst = append(*dst, row[0].Text)
		}
	}
	return nil
}

func (s *DuckDBSession) Begin(context.Context) error    { return errDuckDBNoTx }
func (s *DuckDBSession) Commit(context.Context) error   { return errDuckDBNoTx }
func (s *DuckDBSession) Rollback(context.Context) error { return errDuckDBNoTx }

// ReadOnlySession runs the same file with -readonly.
func (s *DuckDBSession) ReadOnlySession() (Session, error) {
	return &DuckDBSession{bin: s.bin, path: s.path, readonly: true}, nil
}

// Cancel stops the running client process.
func (s *DuckDBSession) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *DuckDBSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

// DuckDBDialect is DuckDB SQL (PostgreSQL-like). Placeholders are ?,
// filled in by DuckDBSession as literals.
type DuckDBDialect struct{}

func (DuckDBDialect) QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
func (DuckDBDialect) Placeholder(int) string { return "?" }
func (DuckDBDialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
}
func (DuckDBDialect) ReadOnlyBegin() string { return "" }
func (DuckDBDialect) CatalogQueries() map[string]string {
	return map[string]string{
		"databases": `SELECT schema_name FROM information_schema.schemata WHERE catalog_name = current_database() ORDER BY schema_name`,
		"schemas":   `SELECT schema_name FROM information_schema.schemata WHERE catalog_name = current_database() ORDER BY schema_name`,
		"objects":   `SELECT table_name, CASE WHEN table_type = 'VIEW' THEN 'VIEW' ELSE 'BASE TABLE' END AS kind FROM information_schema.tables WHERE table_catalog = current_database() AND table_schema = ? ORDER BY table_name`,
		"columns":   `SELECT column_name, data_type FROM information_schema.columns WHERE table_catalog = current_database() AND table_schema = ? AND table_name = ? ORDER BY ordinal_position`,
		"ddl":       `SELECT sql FROM duckdb_tables() WHERE database_name = current_database() AND schema_name = ? AND table_name = ? UNION ALL SELECT sql FROM duckdb_views() WHERE database_name = current_database() AND schema_name = ? AND view_name = ?`,
	}
}
func (d DuckDBDialect) DDL(table string) (string, []any) {
	return `SELECT sql FROM duckdb_tables() WHERE table_name = ? UNION ALL SELECT sql FROM duckdb_views() WHERE view_name = ?`, []any{table, table}
}
func (DuckDBDialect) Explain(sql string, analyze bool) string {
	if analyze {
		return "EXPLAIN ANALYZE " + sql
	}
	return "EXPLAIN " + sql
}
