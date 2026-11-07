package dbclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LibSQLSession is a remote libSQL database (DBX-64) over the HTTP
// pipeline API (v2). Pure Go: no driver, no cgo. Each statement outside
// a transaction runs on a fresh stream (execute + close); a transaction
// keeps one stream open by its baton. The token goes in the
// Authorization header only; errors never show it.
type LibSQLSession struct {
	base     string
	token    string
	client   *http.Client
	readOnly bool

	mu      sync.Mutex // guards baton, stream, cancel, tx, closing
	baton   string
	stream  string // base URL the server asked for (sticky stream)
	cancel  context.CancelFunc
	tx      bool
	closing bool
}

// libsqlHTTPBase is the HTTP(S) base URL of c: libsql:// and wss:// map
// to https://, ws:// to http://; "?tls=0" on a libsql:// URL means
// plain HTTP (a local server).
func libsqlHTTPBase(c Conn) (string, error) {
	raw := c.URL
	if raw == "" {
		if c.Host == "" {
			return "", errors.New("libsql: no server URL")
		}
		raw = "libsql://" + netHost(c.Host, c.Port)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("libsql: bad URL: %w", err)
	}
	switch u.Scheme {
	case "libsql":
		u.Scheme = "https"
		if u.Query().Get("tls") == "0" {
			u.Scheme = "http"
		}
	case "wss", "https":
		u.Scheme = "https"
	case "ws", "http":
		u.Scheme = "http"
	default:
		return "", fmt.Errorf("libsql: unsupported URL scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("libsql: the URL has no host")
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return strings.TrimRight(u.String(), "/"), nil
}

// NewLibSQLSession makes a session for c; token may be empty (a local
// server without auth). It does not connect: Ping does.
func NewLibSQLSession(c Conn, token string) (*LibSQLSession, error) {
	base, err := libsqlHTTPBase(c)
	if err != nil {
		return nil, err
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	if c.Dial != nil {
		tr.DialContext = c.Dial
	}
	return &LibSQLSession{base: base, token: token, client: &http.Client{Transport: tr}}, nil
}

// ── wire types (HTTP pipeline API v2) ────────────────────────────────────

type libsqlPipelineReq struct {
	Baton    string            `json:"baton,omitempty"`
	Requests []libsqlStreamReq `json:"requests"`
}

type libsqlStreamReq struct {
	Type string      `json:"type"`
	Stmt *libsqlStmt `json:"stmt,omitempty"`
}

type libsqlStmt struct {
	SQL      string        `json:"sql"`
	Args     []libsqlValue `json:"args,omitempty"`
	WantRows bool          `json:"want_rows"`
}

type libsqlValue struct {
	Type   string          `json:"type"`
	Value  json.RawMessage `json:"value,omitempty"`
	Base64 string          `json:"base64,omitempty"`
}

type libsqlPipelineResp struct {
	Baton   string               `json:"baton,omitempty"`
	BaseURL string               `json:"base_url,omitempty"`
	Results []libsqlStreamResult `json:"results"`
}

type libsqlStreamResult struct {
	Type     string                `json:"type"`
	Response *libsqlStreamResponse `json:"response,omitempty"`
	Error    *libsqlError          `json:"error,omitempty"`
}

type libsqlStreamResponse struct {
	Type   string            `json:"type"`
	Result *libsqlExecResult `json:"result,omitempty"`
}

type libsqlExecResult struct {
	Cols             []libsqlCol     `json:"cols"`
	Rows             [][]libsqlValue `json:"rows"`
	AffectedRowCount int64           `json:"affected_row_count"`
}

type libsqlCol struct {
	Name     string `json:"name"`
	Decltype string `json:"decltype,omitempty"`
}

type libsqlError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// libsqlArg turns a Go value into a wire value.
func libsqlArg(v any) libsqlValue {
	str := func(s string) json.RawMessage { b, _ := json.Marshal(s); return b }
	switch x := v.(type) {
	case nil:
		return libsqlValue{Type: "null"}
	case bool:
		if x {
			return libsqlValue{Type: "integer", Value: str("1")}
		}
		return libsqlValue{Type: "integer", Value: str("0")}
	case int:
		return libsqlValue{Type: "integer", Value: str(strconv.FormatInt(int64(x), 10))}
	case int32:
		return libsqlValue{Type: "integer", Value: str(strconv.FormatInt(int64(x), 10))}
	case int64:
		return libsqlValue{Type: "integer", Value: str(strconv.FormatInt(x, 10))}
	case uint:
		return libsqlValue{Type: "integer", Value: str(strconv.FormatUint(uint64(x), 10))}
	case uint64:
		return libsqlValue{Type: "integer", Value: str(strconv.FormatUint(x, 10))}
	case float32:
		return libsqlFloat(float64(x))
	case float64:
		return libsqlFloat(x)
	case []byte:
		return libsqlValue{Type: "blob", Base64: base64.StdEncoding.EncodeToString(x)}
	case time.Time:
		return libsqlValue{Type: "text", Value: str(x.Format(time.RFC3339Nano))}
	case string:
		return libsqlValue{Type: "text", Value: str(x)}
	default:
		return libsqlValue{Type: "text", Value: str(fmt.Sprint(x))}
	}
}

func libsqlFloat(f float64) libsqlValue {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return libsqlValue{Type: "null"}
	}
	b, _ := json.Marshal(f)
	return libsqlValue{Type: "float", Value: b}
}

// libsqlCell turns a wire value into a grid cell.
func libsqlCell(v libsqlValue) Cell {
	switch v.Type {
	case "null", "":
		return Cell{Null: true}
	case "blob":
		b, err := base64.StdEncoding.DecodeString(v.Base64)
		if err != nil {
			b, _ = base64.RawStdEncoding.DecodeString(v.Base64)
		}
		return Cell{Text: fmt.Sprintf("0x%x", b), Kind: "binary"}
	case "text", "integer":
		var s string
		if err := json.Unmarshal(v.Value, &s); err != nil {
			return Cell{Text: string(v.Value)}
		}
		return Cell{Text: s}
	case "float":
		var f float64
		if err := json.Unmarshal(v.Value, &f); err != nil {
			return Cell{Text: string(v.Value)}
		}
		return Cell{Text: strconv.FormatFloat(f, 'g', -1, 64)}
	}
	return Cell{Text: string(v.Value)}
}

// ── transport ────────────────────────────────────────────────────────────

// pipeline sends reqs on the current stream (or a new one) and returns
// the results. keep=false closes the stream after reqs.
func (s *LibSQLSession) pipeline(ctx context.Context, reqs []libsqlStreamReq, keep bool) ([]libsqlStreamResult, error) {
	s.mu.Lock()
	baton, base := s.baton, s.stream
	if base == "" {
		base = s.base
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()
	defer cancel()

	if !keep {
		reqs = append(reqs, libsqlStreamReq{Type: "close"})
	}
	body, err := json.Marshal(libsqlPipelineReq{Baton: baton, Requests: reqs})
	if err != nil {
		return nil, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v2/pipeline", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		hr.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(hr)
	if err != nil {
		return nil, s.clean(fmt.Errorf("libsql: %w", err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, s.clean(fmt.Errorf("libsql: reading the answer: %w", err))
	}
	if resp.StatusCode != http.StatusOK {
		s.dropStream()
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		if msg != "" {
			msg = ": " + msg
		}
		return nil, s.clean(fmt.Errorf("libsql: the server answered %s%s", resp.Status, msg))
	}
	var pr libsqlPipelineResp
	if err := json.Unmarshal(raw, &pr); err != nil {
		s.dropStream()
		return nil, fmt.Errorf("libsql: the server's answer is not a pipeline result: %w", err)
	}
	s.mu.Lock()
	if keep {
		s.baton = pr.Baton
		if pr.BaseURL != "" {
			s.stream = strings.TrimRight(pr.BaseURL, "/")
		}
	} else {
		s.baton, s.stream = "", ""
	}
	s.mu.Unlock()
	if len(pr.Results) < len(reqs) {
		return nil, errors.New("libsql: the server gave fewer results than asked")
	}
	return pr.Results, nil
}

func (s *LibSQLSession) dropStream() {
	s.mu.Lock()
	s.baton, s.stream, s.tx = "", "", false
	s.mu.Unlock()
}

// clean removes the token from an error text.
func (s *LibSQLSession) clean(err error) error {
	if err == nil || s.token == "" || !strings.Contains(err.Error(), s.token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), s.token, "[REDACTED]"))
}

func libsqlResultErr(r libsqlStreamResult) error {
	if r.Type == "error" {
		if r.Error != nil && r.Error.Message != "" {
			return errors.New(r.Error.Message)
		}
		return errors.New("libsql: the statement failed")
	}
	return nil
}

func libsqlExec(sql string, args []any) libsqlStreamReq {
	st := &libsqlStmt{SQL: sql, WantRows: true}
	for _, a := range args {
		st.Args = append(st.Args, libsqlArg(a))
	}
	return libsqlStreamReq{Type: "execute", Stmt: st}
}

// ── Session ──────────────────────────────────────────────────────────────

const libsqlQueryOnly = "PRAGMA query_only = 1"

func (s *LibSQLSession) Run(ctx context.Context, stmt string, opts RunOptions) Result {
	start := time.Now()
	s.mu.Lock()
	inTx := s.tx
	s.mu.Unlock()
	var reqs []libsqlStreamReq
	if s.readOnly && !inTx {
		reqs = append(reqs, libsqlExec(libsqlQueryOnly, nil))
	}
	at := len(reqs)
	reqs = append(reqs, libsqlExec(stmt, opts.Args))
	results, err := s.pipeline(ctx, reqs, inTx)
	out := Result{Elapsed: time.Since(start)}
	if err != nil {
		out.Err = err
		return out
	}
	for i := 0; i < at; i++ {
		if err := libsqlResultErr(results[i]); err != nil {
			out.Err = err
			return out
		}
	}
	r := results[at]
	if err := libsqlResultErr(r); err != nil {
		out.Err = err
		return out
	}
	if r.Response == nil || r.Response.Result == nil {
		return out
	}
	res := r.Response.Result
	for _, c := range res.Cols {
		out.Columns = append(out.Columns, Column{Name: c.Name, Type: c.Decltype})
	}
	rows := res.Rows
	if opts.MaxRows > 0 && len(rows) > opts.MaxRows {
		rows, out.Truncated = rows[:opts.MaxRows], true
	}
	out.Rows = make([][]Cell, 0, len(rows))
	for _, row := range rows {
		cells := make([]Cell, len(row))
		for i, v := range row {
			cells[i] = libsqlCell(v)
		}
		out.Rows = append(out.Rows, cells)
	}
	if len(res.Cols) == 0 {
		out.Affected = res.AffectedRowCount
	}
	return out
}

func (s *LibSQLSession) Ping(ctx context.Context) error {
	return s.Run(ctx, "SELECT 1", RunOptions{}).Err
}

func (s *LibSQLSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	return introspectSQLite(ctx, s, req)
}

func (s *LibSQLSession) Begin(ctx context.Context) error {
	s.mu.Lock()
	if s.tx {
		s.mu.Unlock()
		return errors.New("a transaction is already open")
	}
	s.mu.Unlock()
	var reqs []libsqlStreamReq
	if s.readOnly {
		reqs = append(reqs, libsqlExec(libsqlQueryOnly, nil))
	}
	reqs = append(reqs, libsqlExec("BEGIN", nil))
	results, err := s.pipeline(ctx, reqs, true)
	if err == nil {
		for _, r := range results {
			if err = libsqlResultErr(r); err != nil {
				break
			}
		}
	}
	if err != nil {
		s.closeStream()
		return err
	}
	s.mu.Lock()
	s.tx = true
	s.mu.Unlock()
	return nil
}

func (s *LibSQLSession) end(ctx context.Context, sql string) error {
	s.mu.Lock()
	inTx := s.tx
	s.mu.Unlock()
	if !inTx {
		return errors.New("no open transaction")
	}
	results, err := s.pipeline(ctx, []libsqlStreamReq{libsqlExec(sql, nil)}, false)
	s.dropStream()
	if err != nil {
		return err
	}
	return libsqlResultErr(results[0])
}

func (s *LibSQLSession) Commit(ctx context.Context) error   { return s.end(ctx, "COMMIT") }
func (s *LibSQLSession) Rollback(ctx context.Context) error { return s.end(ctx, "ROLLBACK") }

// ReadOnlySession is a second session on the same server whose every
// stream starts with PRAGMA query_only = 1: the server refuses writes.
func (s *LibSQLSession) ReadOnlySession() (Session, error) {
	return &LibSQLSession{base: s.base, token: s.token, client: s.client, readOnly: true}, nil
}

func (s *LibSQLSession) Cancel() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// closeStream closes an open stream (best effort, short deadline).
func (s *LibSQLSession) closeStream() {
	s.mu.Lock()
	open := s.baton != ""
	s.mu.Unlock()
	if open {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, _ = s.pipeline(ctx, nil, false)
		cancel()
	}
	s.dropStream()
}

func (s *LibSQLSession) Close() error {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil
	}
	s.closing = true
	s.mu.Unlock()
	s.closeStream()
	return nil
}
