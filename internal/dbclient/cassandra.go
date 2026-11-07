package dbclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// Cassandra / ScyllaDB (DBX-67): one gocql session per connection. CQL is
// read a page at a time (the page state goes back to the caller), never
// whole. A read-only session sends only SELECT and DESCRIBE.

// CassandraPageSize is the page size of a run without MaxRows.
const CassandraPageSize = 500

// ErrCQLNoTransactions is returned by Begin, Commit and Rollback.
var ErrCQLNoTransactions = errors.New("Cassandra has no transactions; each CQL statement runs on its own")

// CQLPage is one page of a CQL statement.
type CQLPage struct {
	Columns  []Column
	Rows     [][]Cell
	Next     []byte // page state of the next page (nil: last page)
	Warnings []string
}

// CQLExecutor runs one CQL statement a page at a time (tests use a fake).
type CQLExecutor interface {
	Page(ctx context.Context, stmt string, args []any, size int, state []byte) (CQLPage, error)
	Close()
}

// CassandraSession implements Session for Cassandra and ScyllaDB.
type CassandraSession struct {
	cfg      Conn
	exec     CQLExecutor
	readonly bool
	shared   bool // a read-only view: Close leaves exec open

	mu     sync.Mutex
	closed bool
	cancel context.CancelFunc
}

// NewCassandraSessionWith wraps exec (tests, and NewCassandraSession).
func NewCassandraSessionWith(c Conn, exec CQLExecutor) *CassandraSession {
	return &CassandraSession{cfg: c, exec: exec, readonly: c.ReadOnly}
}

// NewCassandraSession connects to c with gocql. c.Dial (the SSH tunnel)
// is used when set; c.SSL or "tls": true turn on TLS.
func NewCassandraSession(ctx context.Context, c Conn, password string) (*CassandraSession, error) {
	port := c.Port
	if port == 0 {
		port = 9042
	}
	cluster := gocql.NewCluster(net.JoinHostPort(c.Host, strconv.Itoa(port)))
	cluster.Keyspace = c.Database
	cluster.ConnectTimeout = 10 * time.Second
	cluster.Timeout = 30 * time.Second
	cluster.NumConns = 1
	// Through a tunnel only the given address is reachable.
	cluster.DisableInitialHostLookup = c.Dial != nil
	if c.User != "" || password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{Username: c.User, Password: password}
	}
	if c.Dial != nil {
		cluster.Dialer = cqlDialer(c.Dial)
	}
	if c.SSL != nil || c.TLS {
		cfg, err := cassandraTLS(c)
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			cluster.SslOpts = &gocql.SslOptions{Config: cfg, EnableHostVerification: !cfg.InsecureSkipVerify}
		}
	}
	sess, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("cassandra: %w", err)
	}
	return NewCassandraSessionWith(c, &gocqlExec{s: sess}), nil
}

type cqlDialer DialFunc

func (d cqlDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d(ctx, network, addr)
}

func cassandraTLS(c Conn) (*tls.Config, error) {
	if c.SSL != nil {
		mode, err := EffectiveSSLMode(c.SSL)
		if err != nil {
			return nil, err
		}
		if mode == SSLDisable {
			if c.TLS {
				return nil, errors.New("the connection asks for TLS (\"tls\": true) but its ssl mode is disable")
			}
			return nil, nil
		}
		return TLSConfig(c.SSL, c.Host)
	}
	return &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}, nil
}

// gocqlExec is the CQLExecutor of a real gocql session.
type gocqlExec struct{ s *gocql.Session }

func (g *gocqlExec) Close() { g.s.Close() }

func (g *gocqlExec) Page(ctx context.Context, stmt string, args []any, size int, state []byte) (CQLPage, error) {
	iter := g.s.Query(stmt, args...).WithContext(ctx).PageSize(size).PageState(state).Iter()
	var page CQLPage
	infos := iter.Columns()
	for _, ci := range infos {
		typ := ""
		if ci.TypeInfo != nil {
			typ = fmt.Sprint(ci.TypeInfo)
		}
		page.Columns = append(page.Columns, Column{Name: ci.Name, Type: typ, Table: ci.Table})
	}
	// Read only the rows of this page: scanning past them would make the
	// iterator fetch the next page.
	for n := iter.NumRows(); n > 0; n-- {
		row := make(map[string]any, len(infos))
		if !iter.MapScan(row) {
			break
		}
		cells := make([]Cell, len(infos))
		for i, ci := range infos {
			cells[i] = cqlCell(row[ci.Name])
		}
		page.Rows = append(page.Rows, cells)
	}
	page.Warnings = iter.Warnings()
	if next := iter.PageState(); len(next) > 0 {
		page.Next = append([]byte(nil), next...)
	}
	if err := iter.Close(); err != nil {
		return CQLPage{}, err
	}
	return page, nil
}

// cqlCell is the cell of one CQL value (collections print as Go values).
func cqlCell(v any) Cell {
	if u, ok := v.(gocql.UUID); ok {
		return Cell{Text: u.String()}
	}
	return CellOf(v)
}

// CQLReadOnly reports whether stmt is one SELECT or DESCRIBE statement.
func CQLReadOnly(stmt string) bool {
	s := strings.TrimSpace(stripCQLComments(stmt))
	s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
	if s == "" || strings.Contains(s, ";") {
		return false
	}
	word := s
	if i := strings.IndexFunc(s, unicode.IsSpace); i >= 0 {
		word = s[:i]
	}
	switch strings.ToUpper(word) {
	case "SELECT", "DESCRIBE", "DESC":
		return true
	}
	return false
}

// stripCQLComments removes --, // and /* */ comments outside strings.
func stripCQLComments(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\'' || s[i] == '"':
			q := s[i]
			j := i + 1
			for j < len(s) {
				if s[j] == q {
					if j+1 < len(s) && s[j+1] == q {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j >= len(s) {
				j = len(s) - 1
			}
			b.WriteString(s[i : j+1])
			i = j
		case strings.HasPrefix(s[i:], "--") || strings.HasPrefix(s[i:], "//"):
			for i < len(s) && s[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				i = len(s)
			} else {
				i += end + 3
			}
			b.WriteByte(' ')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func (s *CassandraSession) start(ctx context.Context) (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, errors.New("the Cassandra session is closed")
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	return ctx, cancel, nil
}

func (s *CassandraSession) page(ctx context.Context, stmt string, args []any, size int, state []byte) (CQLPage, error) {
	ctx, done, err := s.start(ctx)
	if err != nil {
		return CQLPage{}, err
	}
	defer done()
	return s.exec.Page(ctx, stmt, args, size, state)
}

const (
	cqlPing      = "SELECT release_version FROM system.local"
	cqlKeyspaces = "SELECT keyspace_name FROM system_schema.keyspaces"
	cqlTables    = "SELECT table_name FROM system_schema.tables WHERE keyspace_name = ?"
	cqlViews     = "SELECT view_name FROM system_schema.views WHERE keyspace_name = ?"
	cqlColumns   = "SELECT column_name, type, kind, position, clustering_order FROM system_schema.columns WHERE keyspace_name = ? AND table_name = ?"
	cqlIndexes   = "SELECT index_name FROM system_schema.indexes WHERE keyspace_name = ? AND table_name = ?"
	cqlCatalogMx = 10000
)

func (s *CassandraSession) Ping(ctx context.Context) error {
	_, err := s.page(ctx, cqlPing, nil, 1, nil)
	return err
}

// Run is the first page of stmt (MaxRows: the page size).
func (s *CassandraSession) Run(ctx context.Context, stmt string, opts RunOptions) Result {
	r, _ := s.RunPage(ctx, stmt, opts, nil)
	return r
}

// RunPage runs one page of stmt from state (nil: the first page) and
// returns the state of the next page (nil: no more rows).
func (s *CassandraSession) RunPage(ctx context.Context, stmt string, opts RunOptions, state []byte) (Result, []byte) {
	start := time.Now()
	if s.readonly && !CQLReadOnly(stmt) {
		return Result{Err: errors.New("read-only session: only SELECT and DESCRIBE run here")}, nil
	}
	size := opts.MaxRows
	if size <= 0 {
		size = CassandraPageSize
	}
	stmt = strings.TrimSpace(stmt)
	p, err := s.page(ctx, stmt, opts.Args, size, state)
	res := Result{Columns: p.Columns, Rows: p.Rows, Notices: p.Warnings, Elapsed: time.Since(start), Err: err, Why: "Cassandra results are read-only here"}
	if err != nil {
		return res, nil
	}
	res.Truncated = len(p.Next) > 0
	if !res.Truncated {
		p.Next = nil
	}
	return res, p.Next
}

func (s *CassandraSession) Begin(context.Context) error    { return ErrCQLNoTransactions }
func (s *CassandraSession) Commit(context.Context) error   { return ErrCQLNoTransactions }
func (s *CassandraSession) Rollback(context.Context) error { return ErrCQLNoTransactions }

// ReadOnlySession shares the connection and sends reads only.
func (s *CassandraSession) ReadOnlySession() (Session, error) {
	return &CassandraSession{cfg: s.cfg, exec: s.exec, readonly: true, shared: true}, nil
}

func (s *CassandraSession) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *CassandraSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	if !s.shared {
		s.exec.Close()
	}
	return nil
}

// Introspect lists keyspaces (as databases), then the tables and views of
// a keyspace (Schema, or Database), then the columns, indexes and DDL of
// one table.
func (s *CassandraSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	ks := req.Schema
	if ks == "" {
		ks = req.Database
	}
	if ks == "" {
		ks = s.cfg.Database
	}
	if req.Database == "" && req.Schema == "" && req.Object == "" {
		names, err := s.catalog(ctx, cqlKeyspaces)
		sort.Strings(names)
		out.Databases = names
		return out, err
	}
	if req.Object == "" {
		for _, q := range []struct{ stmt, kind string }{{cqlTables, "table"}, {cqlViews, "view"}} {
			names, err := s.catalog(ctx, q.stmt, ks)
			if err != nil {
				return out, err
			}
			sort.Strings(names)
			for _, n := range names {
				out.Objects = append(out.Objects, SchemaObject{Name: n, Kind: q.kind})
			}
		}
		return out, nil
	}
	p, err := s.page(ctx, cqlColumns, []any{ks, req.Object}, cqlCatalogMx, nil)
	if err != nil {
		return out, err
	}
	cols := make([]cqlColumn, 0, len(p.Rows))
	for _, r := range p.Rows {
		if len(r) < 5 {
			continue
		}
		pos, _ := strconv.Atoi(r[3].Text)
		cols = append(cols, cqlColumn{name: r[0].Text, typ: r[1].Text, kind: r[2].Text, pos: pos, order: r[4].Text})
	}
	sort.SliceStable(cols, func(i, j int) bool {
		if cols[i].rank() != cols[j].rank() {
			return cols[i].rank() < cols[j].rank()
		}
		if cols[i].pos != cols[j].pos {
			return cols[i].pos < cols[j].pos
		}
		return cols[i].name < cols[j].name
	})
	for _, c := range cols {
		out.Columns = append(out.Columns, Column{Name: c.name, Type: c.typ, Table: req.Object, PK: c.kind == "partition_key" || c.kind == "clustering"})
	}
	idx, err := s.catalog(ctx, cqlIndexes, ks, req.Object)
	if err != nil {
		return out, err
	}
	for _, n := range idx {
		out.Indexes = append(out.Indexes, SchemaIndex{Name: n})
	}
	out.DDL = cqlTableDDL(ks, req.Object, cols)
	return out, nil
}

func (s *CassandraSession) catalog(ctx context.Context, stmt string, args ...any) ([]string, error) {
	p, err := s.page(ctx, stmt, args, cqlCatalogMx, nil)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range p.Rows {
		if len(r) > 0 && !r[0].Null {
			out = append(out, r[0].Text)
		}
	}
	return out, nil
}

type cqlColumn struct {
	name, typ, kind, order string
	pos                    int
}

func (c cqlColumn) rank() int {
	switch c.kind {
	case "partition_key":
		return 0
	case "clustering":
		return 1
	case "static":
		return 2
	}
	return 3
}

// cqlQuote quotes a CQL identifier.
func cqlQuote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// cqlTableDDL is the CREATE TABLE of cols (sorted: keys first).
func cqlTableDDL(ks, table string, cols []cqlColumn) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s.%s (\n", cqlQuote(ks), cqlQuote(table))
	var pk, ck, order []string
	for _, c := range cols {
		line := "  " + cqlQuote(c.name) + " " + c.typ
		if c.kind == "static" {
			line += " STATIC"
		}
		b.WriteString(line + ",\n")
		switch c.kind {
		case "partition_key":
			pk = append(pk, cqlQuote(c.name))
		case "clustering":
			ck = append(ck, cqlQuote(c.name))
			if o := strings.ToUpper(c.order); o == "ASC" || o == "DESC" {
				order = append(order, cqlQuote(c.name)+" "+o)
			}
		}
	}
	key := strings.Join(pk, ", ")
	if len(pk) > 1 {
		key = "(" + key + ")"
	}
	if len(ck) > 0 {
		key += ", " + strings.Join(ck, ", ")
	}
	fmt.Fprintf(&b, "  PRIMARY KEY (%s)\n)", key)
	if len(order) > 0 {
		fmt.Fprintf(&b, " WITH CLUSTERING ORDER BY (%s)", strings.Join(order, ", "))
	}
	b.WriteString(";")
	return b.String()
}
