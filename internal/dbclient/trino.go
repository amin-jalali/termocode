package dbclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TrinoSession talks to a Trino (or Presto) coordinator over its REST
// protocol: POST /v1/statement, then follow nextUri until the query ends.
// It needs no driver dependency. Conn.Database is the catalog; the schema
// is the tree's database level.
//
// The password goes as HTTP basic auth (Trino accepts it only over
// HTTPS). Read-only runs inside START TRANSACTION READ ONLY, which the
// coordinator enforces. Presto uses the same protocol with X-Presto-*
// headers.
type TrinoSession struct {
	client   *http.Client
	base     url.URL
	conn     Conn
	password string
	prefix   string // "X-Trino-" or "X-Presto-"
	readonly bool
	owns     bool

	mu      sync.Mutex
	cancel  context.CancelFunc
	closed  bool
	catalog string
	schema  string
	txID    string // the open transaction, "" when none
}

// NewTrinoSession opens and pings a Trino or Presto session of c.
func NewTrinoSession(ctx context.Context, c Conn, password string) (*TrinoSession, error) {
	scheme := "http"
	tr := &http.Transport{MaxIdleConns: 2, IdleConnTimeout: 90 * time.Second}
	if c.TLS || c.SSL != nil {
		scheme = "https"
		cfg, err := TLSConfig(c.SSL, c.Host)
		if err != nil {
			return nil, err
		}
		tr.TLSClientConfig = cfg
	}
	if password != "" && scheme != "https" {
		return nil, errors.New("Trino sends a password only over TLS: turn on TLS for this connection")
	}
	if c.Dial != nil {
		dial := c.Dial
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) { return dial(ctx, network, addr) }
	}
	port := c.Port
	if port == 0 {
		port = defaultPort(c.Driver)
	}
	prefix := "X-Trino-"
	if c.Driver == "presto" {
		prefix = "X-Presto-"
	}
	s := &TrinoSession{
		client:   &http.Client{Transport: tr},
		base:     url.URL{Scheme: scheme, Host: net.JoinHostPort(c.Host, strconv.Itoa(port))},
		conn:     c,
		password: password,
		prefix:   prefix,
		owns:     true,
		catalog:  c.Database,
	}
	if c.ReadOnly {
		if err := s.startReadOnly(ctx); err != nil {
			_ = s.Close()
			return nil, err
		}
	}
	if err := s.Ping(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *TrinoSession) startReadOnly(ctx context.Context) error {
	s.readonly = true
	return s.Run(ctx, "START TRANSACTION READ ONLY", RunOptions{}).Err
}

func (s *TrinoSession) Ping(ctx context.Context) error {
	return s.Run(ctx, "SELECT 1", RunOptions{MaxRows: 1}).Err
}

func (s *TrinoSession) Run(ctx context.Context, query string, opts RunOptions) Result {
	start := time.Now()
	res := s.run(ctx, query, opts)
	res.Elapsed = time.Since(start)
	return res
}

// trinoPage is one response of the statement protocol.
type trinoPage struct {
	ID      string `json:"id"`
	NextURI string `json:"nextUri"`
	Columns []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"columns"`
	Data        [][]json.RawMessage `json:"data"`
	UpdateType  string              `json:"updateType"`
	UpdateCount *int64              `json:"updateCount"`
	Error       *struct {
		Message   string `json:"message"`
		ErrorName string `json:"errorName"`
	} `json:"error"`
}

func (s *TrinoSession) run(ctx context.Context, query string, opts RunOptions) Result {
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

	u := s.base
	u.Path = "/v1/statement"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(strings.TrimRight(strings.TrimSpace(sql), ";")))
	if err != nil {
		return Result{Err: err}
	}
	s.headers(req)
	var res Result
	next := ""
	for {
		page, hdr, err := s.do(req)
		if err != nil {
			if ctx.Err() != nil && next != "" {
				s.abandon(next)
			}
			return Result{Err: err}
		}
		s.applyHeaders(hdr)
		if page.Error != nil {
			msg := page.Error.Message
			if msg == "" {
				msg = page.Error.ErrorName
			}
			return Result{Err: errors.New(Redact(msg))}
		}
		if res.Columns == nil && len(page.Columns) > 0 {
			for _, c := range page.Columns {
				res.Columns = append(res.Columns, Column{Name: c.Name, Type: c.Type})
			}
		}
		for _, raw := range page.Data {
			if opts.MaxRows > 0 && len(res.Rows) >= opts.MaxRows {
				res.Truncated = true
				break
			}
			row := make([]Cell, len(raw))
			for i, v := range raw {
				row[i] = clickHouseCell(v)
			}
			res.Rows = append(res.Rows, row)
		}
		if page.UpdateCount != nil {
			res.Affected = *page.UpdateCount
		}
		if page.NextURI == "" {
			return res
		}
		if res.Truncated {
			s.abandon(page.NextURI)
			return res
		}
		next = page.NextURI
		nu, err := s.sameServer(next)
		if err != nil {
			return Result{Err: err}
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, nu, nil)
		if err != nil {
			return Result{Err: err}
		}
		s.headers(req)
	}
}

// sameServer refuses a nextUri on another host: the password is never
// sent anywhere but the configured coordinator.
func (s *TrinoSession) sameServer(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("the server sent a bad next page link: %w", err)
	}
	if !u.IsAbs() {
		return s.base.ResolveReference(u).String(), nil
	}
	if u.Host != s.base.Host || u.Scheme != s.base.Scheme {
		return "", errors.New("the server sent a next page link on another host; stopped")
	}
	return u.String(), nil
}

func (s *TrinoSession) headers(req *http.Request) {
	user := s.conn.User
	if user == "" {
		user = "termocode"
	}
	req.Header.Set(s.prefix+"User", user)
	req.Header.Set(s.prefix+"Source", "termocode")
	s.mu.Lock()
	catalog, schema, tx := s.catalog, s.schema, s.txID
	s.mu.Unlock()
	if catalog != "" {
		req.Header.Set(s.prefix+"Catalog", catalog)
	}
	if schema != "" {
		req.Header.Set(s.prefix+"Schema", schema)
	}
	if tx != "" {
		req.Header.Set(s.prefix+"Transaction-Id", tx)
	} else {
		req.Header.Set(s.prefix+"Transaction-Id", "NONE")
	}
	if s.password != "" {
		req.SetBasicAuth(user, s.password)
	}
}

// applyHeaders keeps what the server changed: USE, transactions.
func (s *TrinoSession) applyHeaders(h http.Header) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v := h.Get(s.prefix + "Set-Catalog"); v != "" {
		s.catalog = v
	}
	if v := h.Get(s.prefix + "Set-Schema"); v != "" {
		s.schema = v
	}
	if v := h.Get(s.prefix + "Started-Transaction-Id"); v != "" {
		s.txID = v
	}
	if h.Get(s.prefix+"Clear-Transaction-Id") != "" {
		s.txID = ""
	}
}

func (s *TrinoSession) do(req *http.Request) (trinoPage, http.Header, error) {
	var page trinoPage
	resp, err := s.client.Do(req)
	if err != nil {
		return page, nil, errors.New(Redact(err.Error()))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return page, nil, errors.New(Redact(err.Error()))
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 2000 {
			msg = msg[:2000]
		}
		if msg == "" {
			msg = resp.Status
		}
		return page, nil, errors.New(Redact(msg))
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return page, nil, fmt.Errorf("could not read the Trino result: %w", err)
	}
	return page, resp.Header, nil
}

// abandon tells the coordinator to drop a query the client stops reading.
func (s *TrinoSession) abandon(next string) {
	nu, err := s.sameServer(next)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, nu, nil)
	if err != nil {
		return
	}
	s.headers(req)
	if resp, err := s.client.Do(req); err == nil {
		_ = resp.Body.Close()
	}
}

func (s *TrinoSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	q := TrinoDialect{}.CatalogQueries()
	if req.Schema == "" {
		req.Schema = req.Database
	}
	if req.Schema == "" && req.Object == "" {
		return out, readFirstColumn(ctx, s, q["databases"], nil, &out.Databases)
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
	d := TrinoDialect{}
	ddl := s.Run(ctx, "SHOW CREATE TABLE "+d.QuoteIdent(req.Schema)+"."+d.QuoteIdent(req.Object), RunOptions{MaxRows: 1})
	if ddl.Err == nil && len(ddl.Rows) > 0 && len(ddl.Rows[0]) > 0 {
		out.DDL = ddl.Rows[0][0].Text
	}
	return out, nil
}

// Begin starts a server transaction; the coordinator returns its id.
func (s *TrinoSession) Begin(ctx context.Context) error {
	if s.inTx() {
		return errors.New("a transaction is already open")
	}
	return s.Run(ctx, "START TRANSACTION", RunOptions{}).Err
}

func (s *TrinoSession) Commit(ctx context.Context) error {
	if !s.inTx() {
		return errors.New("no transaction is open")
	}
	return s.Run(ctx, "COMMIT", RunOptions{}).Err
}

func (s *TrinoSession) Rollback(ctx context.Context) error {
	if !s.inTx() {
		return errors.New("no transaction is open")
	}
	return s.Run(ctx, "ROLLBACK", RunOptions{}).Err
}

func (s *TrinoSession) inTx() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.txID != ""
}

// ReadOnlySession shares the transport and opens its own READ ONLY
// transaction on first use.
func (s *TrinoSession) ReadOnlySession() (Session, error) {
	s.mu.Lock()
	catalog, schema := s.catalog, s.schema
	s.mu.Unlock()
	ro := &TrinoSession{client: s.client, base: s.base, conn: s.conn, password: s.password, prefix: s.prefix, catalog: catalog, schema: schema}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ro.startReadOnly(ctx); err != nil {
		return nil, err
	}
	return ro, nil
}

// Cancel drops the running request; the coordinator ends a query whose
// client stops polling.
func (s *TrinoSession) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *TrinoSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	tx := s.txID
	s.mu.Unlock()
	if tx != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.Run(ctx, "ROLLBACK", RunOptions{}).Err
		cancel()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	if s.owns {
		s.client.CloseIdleConnections()
	}
	return nil
}

// TrinoDialect is Trino / Presto SQL. Placeholders are ?, filled in by
// TrinoSession as literals.
type TrinoDialect struct{}

func (TrinoDialect) QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
func (TrinoDialect) Placeholder(int) string { return "?" }
func (TrinoDialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	if offset > 0 {
		return fmt.Sprintf(" OFFSET %d LIMIT %d", offset, limit)
	}
	return fmt.Sprintf(" LIMIT %d", limit)
}
func (TrinoDialect) ReadOnlyBegin() string { return "START TRANSACTION READ ONLY" }
func (TrinoDialect) CatalogQueries() map[string]string {
	return map[string]string{
		"databases": `SELECT schema_name FROM information_schema.schemata WHERE schema_name <> 'information_schema' ORDER BY schema_name`,
		"schemas":   `SELECT schema_name FROM information_schema.schemata WHERE schema_name <> 'information_schema' ORDER BY schema_name`,
		"objects":   `SELECT table_name, CASE WHEN table_type = 'VIEW' THEN 'VIEW' ELSE 'BASE TABLE' END AS kind FROM information_schema.tables WHERE table_schema = ? ORDER BY table_name`,
		"columns":   `SELECT column_name, data_type FROM information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position`,
	}
}
func (d TrinoDialect) DDL(table string) (string, []any) {
	return "SHOW CREATE TABLE " + d.QuoteIdent(table), nil
}
func (TrinoDialect) Explain(sql string, analyze bool) string {
	if analyze {
		return "EXPLAIN ANALYZE " + sql
	}
	return "EXPLAIN " + sql
}
