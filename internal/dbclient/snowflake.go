package dbclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SnowflakeSession runs SQL on a Snowflake account over its SQL API
// (DBX-66): POST /api/v2/statements on <account>.snowflakecomputing.com.
// The password is a token sent as a bearer: a programmatic access token
// by default, params.tokenType "OAUTH" or "KEYPAIR_JWT" for the others.
// Database is the database; params.warehouse, params.schema and
// params.role set the rest of the context. An answer that is not ready
// (202) is polled; a big result is read partition by partition until
// MaxRows. Snowflake has no read-only session: the read-only twin lets
// only reads through (warehouseGuard).
type SnowflakeSession struct {
	*httpEngine
	conn     Conn
	readOnly bool
}

func init() {
	registerHTTPEngine(HTTPEngine{
		ID: "snowflake", Title: "Snowflake", Port: 443, TLSPort: 443,
		Hint:    "SQL API, <account>.snowflakecomputing.com · token as password",
		Schemes: []string{"snowflake"},
		Open: func(c Conn, password string) (Session, error) {
			return NewSnowflakeSession(c, password)
		},
		QueryParams: true,
		URLExample:  "snowflake://user@account.snowflakecomputing.com/DATABASE/SCHEMA?warehouse=WH&role=ROLE",
		FixURL: func(c *Conn) {
			// snowflake://user@account.snowflakecomputing.com/DB/SCHEMA
			c.TLS, c.Port = true, 443
			if db, schema, ok := strings.Cut(c.Database, "/"); ok {
				c.Database = db
				c.Params["schema"] = schema
			}
		},
	})
}

// NewSnowflakeSession makes a session of c; it does not connect.
func NewSnowflakeSession(c Conn, token string) (*SnowflakeSession, error) {
	if c.URL == "" && c.Host != "" {
		c.TLS = true // the SQL API is HTTPS only
	}
	e, err := newHTTPEngine("Snowflake", c, "")
	if err != nil {
		return nil, err
	}
	e.password = token // scrubbed from errors, never basic auth
	e.header.Set("Authorization", "Bearer "+token)
	kind := strings.ToUpper(warehouseParam(c, "tokenType"))
	if kind == "" {
		kind = "PROGRAMMATIC_ACCESS_TOKEN"
	}
	e.header.Set("X-Snowflake-Authorization-Token-Type", kind)
	e.header.Set("User-Agent", "termocode")
	return &SnowflakeSession{httpEngine: e, conn: c}, nil
}

type snowflakeBinding struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type snowflakeRequest struct {
	Statement  string                      `json:"statement"`
	Timeout    int                         `json:"timeout,omitempty"`
	Database   string                      `json:"database,omitempty"`
	Schema     string                      `json:"schema,omitempty"`
	Warehouse  string                      `json:"warehouse,omitempty"`
	Role       string                      `json:"role,omitempty"`
	Bindings   map[string]snowflakeBinding `json:"bindings,omitempty"`
	Parameters map[string]string           `json:"parameters,omitempty"`
}

type snowflakeResponse struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	StatementHandle   string `json:"statementHandle"`
	ResultSetMetaData struct {
		NumRows int64 `json:"numRows"`
		RowType []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"rowType"`
		PartitionInfo []struct {
			RowCount int64 `json:"rowCount"`
		} `json:"partitionInfo"`
	} `json:"resultSetMetaData"`
	Data  [][]*string      `json:"data"`
	Stats map[string]int64 `json:"stats"`
}

// snowflakeRunning is the code of a statement still running.
const snowflakeRunning = "333334"

func (s *SnowflakeSession) Ping(ctx context.Context) error {
	return s.Run(ctx, "SELECT 1", RunOptions{MaxRows: 1}).Err
}

// call sends one request and decodes the answer; a 4xx/5xx answer's
// Snowflake message is the error.
func (s *SnowflakeSession) call(ctx context.Context, method, path string, body any) (snowflakeResponse, error) {
	var out snowflakeResponse
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	raw, err := s.do(ctx, method, path, data, "")
	if len(raw) > 0 {
		if jerr := json.Unmarshal(raw, &out); jerr != nil && err == nil {
			return out, fmt.Errorf("Snowflake: unexpected answer: %v", jerr)
		}
	}
	if err != nil {
		if out.Message != "" {
			return out, fmt.Errorf("Snowflake %s: %s", out.Code, s.scrub(out.Message))
		}
		return out, err
	}
	return out, nil
}

func (s *SnowflakeSession) Run(ctx context.Context, stmt string, opts RunOptions) Result {
	start := time.Now()
	res := s.run(ctx, strings.TrimSpace(stmt), opts)
	res.Elapsed = time.Since(start)
	return res
}

func (s *SnowflakeSession) run(ctx context.Context, stmt string, opts RunOptions) Result {
	if err := warehouseGuard(s.readOnly, "Snowflake", s.conn.Driver, stmt); err != nil {
		return Result{Err: err}
	}
	req := snowflakeRequest{Statement: stmt, Timeout: 3600, Database: s.conn.Database,
		Schema: warehouseParam(s.conn, "schema"), Warehouse: warehouseParam(s.conn, "warehouse"), Role: warehouseParam(s.conn, "role")}
	if len(opts.Args) > 0 {
		req.Bindings = map[string]snowflakeBinding{}
		for i, a := range opts.Args {
			req.Bindings[strconv.Itoa(i+1)] = snowflakeBinding{Type: "TEXT", Value: fmt.Sprint(a)}
		}
	}
	resp, err := s.call(ctx, "POST", "/api/v2/statements?requestId="+snowflakeRequestID(), req)
	if err != nil {
		return Result{Err: err}
	}
	if resp.Code == snowflakeRunning {
		handle := url.PathEscape(resp.StatementHandle)
		err = warehousePoll(ctx, func() (bool, error) {
			var perr error
			resp, perr = s.call(ctx, "GET", "/api/v2/statements/"+handle, nil)
			return perr == nil && resp.Code != snowflakeRunning, perr
		})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				s.cancelStatement(resp.StatementHandle)
			}
			return Result{Err: err}
		}
	}
	var res Result
	for _, c := range resp.ResultSetMetaData.RowType {
		res.Columns = append(res.Columns, Column{Name: c.Name, Type: c.Type})
	}
	more := warehouseRows(&res, resp.Data, opts.MaxRows)
	for p := 1; more && p < len(resp.ResultSetMetaData.PartitionInfo); p++ {
		part, err := s.call(ctx, "GET", "/api/v2/statements/"+url.PathEscape(resp.StatementHandle)+"?partition="+strconv.Itoa(p), nil)
		if err != nil {
			res.Err = err
			return res
		}
		more = warehouseRows(&res, part.Data, opts.MaxRows)
	}
	for k, n := range resp.Stats {
		if strings.HasPrefix(k, "numRows") {
			res.Affected += n
		}
	}
	return res
}

// cancelStatement asks the account to stop a statement (best effort).
func (s *SnowflakeSession) cancelStatement(handle string) {
	if handle == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.call(ctx, "POST", "/api/v2/statements/"+url.PathEscape(handle)+"/cancel", nil)
}

// snowflakeRequestID is a fresh request id (the API retries by it).
func snowflakeRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func (s *SnowflakeSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	return warehouseIntrospect(ctx, s, warehouseQueries{
		databases: "SHOW TERSE DATABASES", databasesCol: "name",
		schemas:    func(db string) string { return "SHOW TERSE SCHEMAS IN DATABASE " + warehouseQuote(db) },
		schemasCol: "name",
		objects: func(db, schema string) (string, []any) {
			return "SELECT TABLE_NAME, TABLE_TYPE FROM " + warehouseQuote(db) + ".INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME", []any{schema}
		},
		columns: func(db, schema, object string) (string, []any) {
			return "SELECT COLUMN_NAME, DATA_TYPE FROM " + warehouseQuote(db) + ".INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION", []any{schema, object}
		},
		ddl: func(db, schema, object string) (string, []any) {
			return "SELECT GET_DDL('TABLE', ?)", []any{warehouseQuote(db) + "." + warehouseQuote(schema) + "." + warehouseQuote(object)}
		},
	}, req)
}

func (s *SnowflakeSession) Begin(context.Context) error    { return errNoTx("Snowflake") }
func (s *SnowflakeSession) Commit(context.Context) error   { return errNoTx("Snowflake") }
func (s *SnowflakeSession) Rollback(context.Context) error { return errNoTx("Snowflake") }

// ReadOnlySession is a twin that runs reads only.
func (s *SnowflakeSession) ReadOnlySession() (Session, error) {
	return &SnowflakeSession{httpEngine: s.clone(), conn: s.conn, readOnly: true}, nil
}
