package dbclient

import (
	"bufio"
	"bytes"
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

// ClickHouseSession talks to ClickHouse over its HTTP interface (port
// 8123, or 8443 with TLS). It needs no driver dependency: every query is
// one POST, rows come back as JSONCompactEachRowWithNamesAndTypes and are
// read line by line, so MaxRows stops the read early.
//
// The password goes in the X-ClickHouse-Key header, never in the URL.
// Read-only is the server's readonly=2 setting (reads and SET only).
type ClickHouseSession struct {
	client   *http.Client
	base     url.URL
	conn     Conn
	password string
	readonly bool
	owns     bool // closes the transport on Close

	mu     sync.Mutex
	cancel context.CancelFunc
	closed bool
}

var errClickHouseNoTx = errors.New("ClickHouse has no transactions; each statement commits on its own")

// NewClickHouseSession opens and pings a ClickHouse HTTP session of c.
func NewClickHouseSession(ctx context.Context, c Conn, password string) (*ClickHouseSession, error) {
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
	if c.Dial != nil {
		dial := c.Dial
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) { return dial(ctx, network, addr) }
	}
	port := c.Port
	if port == 0 {
		port = defaultPort("clickhouse")
	}
	s := &ClickHouseSession{
		client:   &http.Client{Transport: tr},
		base:     url.URL{Scheme: scheme, Host: net.JoinHostPort(c.Host, strconv.Itoa(port)), Path: "/"},
		conn:     c,
		password: password,
		readonly: c.ReadOnly,
		owns:     true,
	}
	if err := s.Ping(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *ClickHouseSession) Ping(ctx context.Context) error {
	return s.Run(ctx, "SELECT 1", RunOptions{MaxRows: 1}).Err
}

func (s *ClickHouseSession) Run(ctx context.Context, query string, opts RunOptions) Result {
	start := time.Now()
	res := s.run(ctx, query, opts)
	res.Elapsed = time.Since(start)
	return res
}

func (s *ClickHouseSession) run(ctx context.Context, query string, opts RunOptions) Result {
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
	q := url.Values{}
	if s.conn.Database != "" {
		q.Set("database", s.conn.Database)
	}
	q.Set("default_format", "JSONCompactEachRowWithNamesAndTypes")
	if s.readonly {
		q.Set("readonly", "2")
	}
	for i, a := range opts.Args {
		q.Set("param_p"+strconv.Itoa(i+1), fmt.Sprint(a))
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(query))
	if err != nil {
		return Result{Err: err}
	}
	if s.conn.User != "" {
		req.Header.Set("X-ClickHouse-User", s.conn.User)
	}
	if s.password != "" {
		req.Header.Set("X-ClickHouse-Key", s.password)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return Result{Err: errors.New(Redact(err.Error()))}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return Result{Err: errors.New(Redact(msg))}
	}
	return readClickHouseRows(resp, opts.MaxRows)
}

// readClickHouseRows reads the names line, the types line and then one
// JSON array per row. A line that is not JSON is the server's error
// after the rows started (ClickHouse cannot change the status then).
func readClickHouseRows(resp *http.Response, maxRows int) Result {
	var res Result
	if sum := resp.Header.Get("X-ClickHouse-Summary"); sum != "" {
		var m map[string]string
		if json.Unmarshal([]byte(sum), &m) == nil {
			res.Affected, _ = strconv.ParseInt(m["written_rows"], 10, 64)
		}
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	var names, types []string
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if line[0] != '[' {
			res.Err = errors.New(Redact(string(line)))
			return res
		}
		if names == nil {
			if err := json.Unmarshal(line, &names); err != nil {
				res.Err = fmt.Errorf("could not read the ClickHouse result: %w", err)
				return res
			}
			continue
		}
		if types == nil {
			if err := json.Unmarshal(line, &types); err != nil {
				res.Err = fmt.Errorf("could not read the ClickHouse result: %w", err)
				return res
			}
			for i, n := range names {
				col := Column{Name: n}
				if i < len(types) {
					col.Type = types[i]
				}
				res.Columns = append(res.Columns, col)
			}
			continue
		}
		if maxRows > 0 && len(res.Rows) >= maxRows {
			res.Truncated = true
			return res
		}
		var raw []json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			res.Err = fmt.Errorf("could not read the ClickHouse result: %w", err)
			return res
		}
		row := make([]Cell, len(raw))
		for i, v := range raw {
			row[i] = clickHouseCell(v)
		}
		res.Rows = append(res.Rows, row)
	}
	if err := sc.Err(); err != nil {
		res.Err = errors.New(Redact(err.Error()))
	}
	return res
}

func clickHouseCell(v json.RawMessage) Cell {
	t := bytes.TrimSpace(v)
	switch {
	case bytes.Equal(t, []byte("null")):
		return Cell{Null: true}
	case len(t) > 0 && t[0] == '"':
		var s string
		if json.Unmarshal(t, &s) == nil {
			return Cell{Text: s}
		}
	case len(t) > 0 && (t[0] == '[' || t[0] == '{'):
		var b bytes.Buffer
		if json.Compact(&b, t) == nil {
			return Cell{Text: b.String(), Kind: "json"}
		}
	}
	return Cell{Text: string(t)}
}

func (s *ClickHouseSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	q := ClickHouseDialect{}.CatalogQueries()
	if req.Schema == "" {
		req.Schema = req.Database
	}
	if req.Schema == "" && req.Object == "" {
		return out, s.readList(ctx, q["databases"], &out.Databases)
	}
	if req.Schema == "" {
		var cur []string
		if err := s.readList(ctx, "SELECT currentDatabase()", &cur); err != nil {
			return out, err
		}
		if len(cur) > 0 {
			req.Schema = cur[0]
		}
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
		if len(row) > 2 {
			pk := row[2].Text == "1" || row[2].Text == "true"
			out.Columns = append(out.Columns, Column{Name: row[0].Text, Type: row[1].Text, Table: req.Object, PK: pk})
			if pk {
				out.Keys = append(out.Keys, row[0].Text)
			}
		}
	}
	d := ClickHouseDialect{}
	ddl := s.Run(ctx, "SHOW CREATE TABLE "+d.QuoteIdent(req.Schema)+"."+d.QuoteIdent(req.Object), RunOptions{MaxRows: 1})
	if ddl.Err != nil {
		return out, ddl.Err
	}
	if len(ddl.Rows) > 0 && len(ddl.Rows[0]) > 0 {
		out.DDL = ddl.Rows[0][0].Text
	}
	return out, nil
}

func (s *ClickHouseSession) readList(ctx context.Context, query string, dst *[]string) error {
	r := s.Run(ctx, query, RunOptions{MaxRows: 10000})
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

func (s *ClickHouseSession) Begin(context.Context) error    { return errClickHouseNoTx }
func (s *ClickHouseSession) Commit(context.Context) error   { return errClickHouseNoTx }
func (s *ClickHouseSession) Rollback(context.Context) error { return errClickHouseNoTx }

// ReadOnlySession shares the transport; the server enforces readonly=2.
func (s *ClickHouseSession) ReadOnlySession() (Session, error) {
	return &ClickHouseSession{client: s.client, base: s.base, conn: s.conn, password: s.password, readonly: true}, nil
}

// Cancel drops the running request; ClickHouse stops a query whose HTTP
// client went away.
func (s *ClickHouseSession) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *ClickHouseSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	if s.owns {
		s.client.CloseIdleConnections()
	}
	return nil
}

// ClickHouseDialect is ClickHouse SQL. Placeholders are the server's
// typed {pN:String} parameters, bound by ClickHouseSession as param_pN.
type ClickHouseDialect struct{}

func (ClickHouseDialect) QuoteIdent(s string) string {
	return "`" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "`", "\\`") + "`"
}
func (ClickHouseDialect) Placeholder(i int) string { return fmt.Sprintf("{p%d:String}", i) }
func (ClickHouseDialect) Limit(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
}
func (ClickHouseDialect) ReadOnlyBegin() string { return "" }
func (ClickHouseDialect) CatalogQueries() map[string]string {
	return map[string]string{
		"databases": `SELECT name FROM system.databases ORDER BY name`,
		"schemas":   `SELECT name FROM system.databases ORDER BY name`,
		"objects":   `SELECT name, if(engine IN ('View', 'MaterializedView', 'LiveView', 'WindowView'), 'VIEW', 'BASE TABLE') AS kind FROM system.tables WHERE database = {p1:String} ORDER BY name`,
		"columns":   `SELECT name, type, is_in_primary_key AS pk FROM system.columns WHERE database = {p1:String} AND table = {p2:String} ORDER BY position`,
	}
}
func (d ClickHouseDialect) DDL(table string) (string, []any) {
	return "SHOW CREATE TABLE " + d.QuoteIdent(table), nil
}
func (ClickHouseDialect) Explain(sql string, analyze bool) string {
	if analyze {
		return "EXPLAIN PIPELINE " + sql
	}
	return "EXPLAIN " + sql
}
