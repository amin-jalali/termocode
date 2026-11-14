package dbclient

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DatabricksSession runs SQL on a Databricks SQL warehouse over the
// Statement Execution API (DBX-66): POST /api/2.0/sql/statements on the
// workspace host. The password is a personal access token (or an OAuth
// token) sent as a bearer. params.warehouse is the warehouse id (it is
// needed), Database the catalog and params.schema the schema. Results
// come inline as JSON arrays; a statement still running is polled and
// its chunks are followed until MaxRows. There is no read-only session:
// the read-only twin lets only reads through (warehouseGuard).
type DatabricksSession struct {
	*httpEngine
	conn     Conn
	readOnly bool
}

func init() {
	registerHTTPEngine(HTTPEngine{
		ID: "databricks", Title: "Databricks SQL", Port: 443, TLSPort: 443,
		Hint:    "SQL warehouse, workspace host · token as password, params.warehouse",
		Schemes: []string{"databricks"},
		Open: func(c Conn, password string) (Session, error) {
			return NewDatabricksSession(c, password)
		},
		QueryParams: true,
		URLExample:  "databricks://dbc-xxxx.cloud.databricks.com/CATALOG/SCHEMA?warehouse=WAREHOUSE_ID",
		FixURL: func(c *Conn) {
			c.TLS, c.Port = true, 443
			if cat, schema, ok := strings.Cut(c.Database, "/"); ok {
				c.Database = cat
				c.Params["schema"] = schema
			}
		},
	})
}

// NewDatabricksSession makes a session of c; it does not connect.
func NewDatabricksSession(c Conn, token string) (*DatabricksSession, error) {
	if warehouseParam(c, "warehouse") == "" {
		return nil, errors.New(`Databricks SQL: no warehouse: set "params": {"warehouse": "<warehouse id>"}`)
	}
	if c.URL == "" && c.Host != "" {
		c.TLS = true
	}
	e, err := newHTTPEngine("Databricks SQL", c, "")
	if err != nil {
		return nil, err
	}
	e.password = token
	e.header.Set("Authorization", "Bearer "+token)
	return &DatabricksSession{httpEngine: e, conn: c}, nil
}

type databricksParam struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type databricksRequest struct {
	Statement     string            `json:"statement"`
	WarehouseID   string            `json:"warehouse_id"`
	Catalog       string            `json:"catalog,omitempty"`
	Schema        string            `json:"schema,omitempty"`
	Parameters    []databricksParam `json:"parameters,omitempty"`
	WaitTimeout   string            `json:"wait_timeout"`
	OnWaitTimeout string            `json:"on_wait_timeout"`
	Disposition   string            `json:"disposition"`
	Format        string            `json:"format"`
	RowLimit      int               `json:"row_limit,omitempty"`
}

type databricksChunk struct {
	DataArray             [][]*string `json:"data_array"`
	NextChunkInternalLink string      `json:"next_chunk_internal_link"`
}

type databricksResponse struct {
	StatementID string `json:"statement_id"`
	Status      struct {
		State string `json:"state"`
		Error struct {
			ErrorCode string `json:"error_code"`
			Message   string `json:"message"`
		} `json:"error"`
	} `json:"status"`
	Manifest struct {
		Schema struct {
			Columns []struct {
				Name     string `json:"name"`
				TypeName string `json:"type_name"`
			} `json:"columns"`
		} `json:"schema"`
		Truncated bool `json:"truncated"`
	} `json:"manifest"`
	Result databricksChunk `json:"result"`
}

func (r databricksResponse) running() bool {
	return r.Status.State == "PENDING" || r.Status.State == "RUNNING"
}

func (r databricksResponse) err() error {
	switch r.Status.State {
	case "FAILED", "CANCELED", "CLOSED":
		if r.Status.Error.Message != "" {
			return fmt.Errorf("Databricks SQL: %s", r.Status.Error.Message)
		}
		return fmt.Errorf("Databricks SQL: statement %s", strings.ToLower(r.Status.State))
	}
	return nil
}

func (s *DatabricksSession) Ping(ctx context.Context) error {
	return s.Run(ctx, "SELECT 1", RunOptions{MaxRows: 1}).Err
}

func (s *DatabricksSession) get(ctx context.Context, path string, out any) error {
	return s.doJSON(ctx, "GET", path, nil, out)
}

func (s *DatabricksSession) Run(ctx context.Context, stmt string, opts RunOptions) Result {
	start := time.Now()
	res := s.run(ctx, strings.TrimSpace(stmt), opts)
	res.Elapsed = time.Since(start)
	return res
}

func (s *DatabricksSession) run(ctx context.Context, stmt string, opts RunOptions) Result {
	if err := warehouseGuard(s.readOnly, "Databricks SQL", s.conn.Driver, stmt); err != nil {
		return Result{Err: err}
	}
	req := databricksRequest{Statement: namedPlaceholders(stmt, len(opts.Args)), WarehouseID: warehouseParam(s.conn, "warehouse"),
		Catalog: s.conn.Database, Schema: warehouseParam(s.conn, "schema"),
		WaitTimeout: "30s", OnWaitTimeout: "CONTINUE", Disposition: "INLINE", Format: "JSON_ARRAY"}
	for i, a := range opts.Args {
		req.Parameters = append(req.Parameters, databricksParam{Name: "p" + strconv.Itoa(i+1), Value: fmt.Sprint(a)})
	}
	if opts.MaxRows > 0 {
		req.RowLimit = opts.MaxRows + 1 // one more tells a cut result
	}
	var resp databricksResponse
	if err := s.doJSON(ctx, "POST", "/api/2.0/sql/statements", req, &resp); err != nil {
		return Result{Err: err}
	}
	if resp.running() {
		id := url.PathEscape(resp.StatementID)
		err := warehousePoll(ctx, func() (bool, error) {
			if err := s.get(ctx, "/api/2.0/sql/statements/"+id, &resp); err != nil {
				return false, err
			}
			return !resp.running(), nil
		})
		if err != nil {
			if ctx.Err() != nil {
				s.cancelStatement(resp.StatementID)
			}
			return Result{Err: err}
		}
	}
	if err := resp.err(); err != nil {
		return Result{Err: errors.New(s.scrub(err.Error()))}
	}
	var res Result
	for _, c := range resp.Manifest.Schema.Columns {
		res.Columns = append(res.Columns, Column{Name: c.Name, Type: c.TypeName})
	}
	chunk := resp.Result
	for warehouseRows(&res, chunk.DataArray, opts.MaxRows) && chunk.NextChunkInternalLink != "" {
		link := chunk.NextChunkInternalLink
		if !strings.HasPrefix(link, "/api/") {
			res.Err = errors.New("Databricks SQL: unexpected chunk link")
			return res
		}
		chunk = databricksChunk{}
		if err := s.get(ctx, link, &chunk); err != nil {
			res.Err = err
			return res
		}
	}
	if resp.Manifest.Truncated && opts.MaxRows > 0 {
		res.Truncated = true
	}
	if i := warehouseColumn(res, "num_affected_rows"); i >= 0 && len(res.Rows) == 1 {
		res.Affected, _ = strconv.ParseInt(res.Rows[0][i].Text, 10, 64)
	}
	return res
}

// cancelStatement asks the warehouse to stop a statement (best effort).
func (s *DatabricksSession) cancelStatement(id string) {
	if id == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.doJSON(ctx, "POST", "/api/2.0/sql/statements/"+url.PathEscape(id)+"/cancel", nil, nil)
}

// namedPlaceholders turns the first n "?" outside quotes and comments
// into :p1, :p2 … (the API takes named parameters only).
func namedPlaceholders(stmt string, n int) string {
	if n == 0 {
		return stmt
	}
	var b strings.Builder
	var quote byte
	k := 0
	for i := 0; i < len(stmt); i++ {
		ch := stmt[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '-' && i+1 < len(stmt) && stmt[i+1] == '-':
			end := strings.IndexByte(stmt[i:], '\n')
			if end < 0 {
				end = len(stmt) - i
			}
			b.WriteString(stmt[i : i+end])
			i += end - 1
			continue
		case ch == '?' && k < n:
			k++
			b.WriteString(":p" + strconv.Itoa(k))
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

// databricksQuote quotes one identifier with backticks.
func databricksQuote(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }

func (s *DatabricksSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	return warehouseIntrospect(ctx, s, warehouseQueries{
		databases: "SHOW CATALOGS", databasesCol: "catalog",
		schemas:    func(cat string) string { return "SHOW SCHEMAS IN " + databricksQuote(cat) },
		schemasCol: "databaseName",
		objects: func(cat, schema string) (string, []any) {
			return "SHOW TABLES IN " + databricksQuote(cat) + "." + databricksQuote(schema), nil
		},
		columns: func(cat, schema, object string) (string, []any) {
			return "SELECT column_name, data_type FROM " + databricksQuote(cat) + ".information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position", []any{schema, object}
		},
		ddl: func(cat, schema, object string) (string, []any) {
			return "SHOW CREATE TABLE " + databricksQuote(cat) + "." + databricksQuote(schema) + "." + databricksQuote(object), nil
		},
		objectCol: "tableName",
	}, req)
}

func (s *DatabricksSession) Begin(context.Context) error    { return errNoTx("Databricks SQL") }
func (s *DatabricksSession) Commit(context.Context) error   { return errNoTx("Databricks SQL") }
func (s *DatabricksSession) Rollback(context.Context) error { return errNoTx("Databricks SQL") }

// ReadOnlySession is a twin that runs reads only.
func (s *DatabricksSession) ReadOnlySession() (Session, error) {
	return &DatabricksSession{httpEngine: s.clone(), conn: s.conn, readOnly: true}, nil
}
