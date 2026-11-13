package dbclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AthenaSession runs SQL on Amazon Athena over its JSON API (DBX-66),
// signed with SigV4: the connection's user is the access key id and the
// password the secret key. params.region (or the regional host),
// params.workgroup (primary by default), params.output (the S3 result
// location, needed unless the workgroup has one) and params.catalog
// (AwsDataCatalog) set where queries run; Database is the Glue database.
// A query is started, polled to the end and its result pages read
// until MaxRows. Athena has no read-only session: the read-only twin
// lets only reads through (warehouseGuard).
type AthenaSession struct {
	*awsJSONEngine
	conn     Conn
	readOnly bool
}

func init() {
	registerHTTPEngine(HTTPEngine{
		ID: "athena", Title: "Amazon Athena", Port: 443, TLSPort: 443,
		Hint:    "JSON API, SigV4 · access key id as user, secret key as password",
		Schemes: []string{"athena"},
		Open: func(c Conn, password string) (Session, error) {
			return NewAthenaSession(c, password)
		},
		QueryParams: true,
		FixURL: func(c *Conn) {
			c.TLS, c.Port = true, 443
			if c.Params["region"] == "" {
				if r := awsHostRegion(c.Host, "athena"); r != "" {
					c.Params["region"] = r
				}
			}
		},
	})
}

// NewAthenaSession makes a session of c; it does not connect.
func NewAthenaSession(c Conn, secret string) (*AthenaSession, error) {
	a, err := newAWSJSONEngine("Athena", "athena", c, secret, "AmazonAthena.", "application/x-amz-json-1.1")
	if err != nil {
		return nil, err
	}
	return &AthenaSession{awsJSONEngine: a, conn: c}, nil
}

func (s *AthenaSession) catalog() string {
	if c := warehouseParam(s.conn, "catalog"); c != "" {
		return c
	}
	return "AwsDataCatalog"
}

func (s *AthenaSession) workgroup() string {
	if w := warehouseParam(s.conn, "workgroup"); w != "" {
		return w
	}
	return "primary"
}

func (s *AthenaSession) Ping(ctx context.Context) error {
	return s.call(ctx, "GetWorkGroup", map[string]any{"WorkGroup": s.workgroup()}, nil)
}

type athenaExecution struct {
	QueryExecution struct {
		Status struct {
			State             string `json:"State"`
			StateChangeReason string `json:"StateChangeReason"`
		} `json:"Status"`
		StatementType string `json:"StatementType"`
	} `json:"QueryExecution"`
}

type athenaResults struct {
	UpdateCount int64  `json:"UpdateCount"`
	NextToken   string `json:"NextToken"`
	ResultSet   struct {
		ResultSetMetadata struct {
			ColumnInfo []struct {
				Name string `json:"Name"`
				Type string `json:"Type"`
			} `json:"ColumnInfo"`
		} `json:"ResultSetMetadata"`
		Rows []struct {
			Data []struct {
				VarCharValue *string `json:"VarCharValue"`
			} `json:"Data"`
		} `json:"Rows"`
	} `json:"ResultSet"`
}

// athenaLiteral is an execution parameter: Athena takes them as SQL
// literals.
func athenaLiteral(a any) string {
	switch v := a.(type) {
	case nil:
		return "NULL"
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(v)
	}
	return "'" + strings.ReplaceAll(fmt.Sprint(a), "'", "''") + "'"
}

func (s *AthenaSession) Run(ctx context.Context, stmt string, opts RunOptions) Result {
	start := time.Now()
	res := s.run(ctx, strings.TrimSpace(stmt), opts)
	res.Elapsed = time.Since(start)
	return res
}

func (s *AthenaSession) run(ctx context.Context, stmt string, opts RunOptions) Result {
	if err := warehouseGuard(s.readOnly, "Athena", stmt); err != nil {
		return Result{Err: err}
	}
	in := map[string]any{"QueryString": stmt, "WorkGroup": s.workgroup(),
		"QueryExecutionContext": map[string]any{"Database": s.conn.Database, "Catalog": s.catalog()}}
	if out := warehouseParam(s.conn, "output"); out != "" {
		in["ResultConfiguration"] = map[string]any{"OutputLocation": out}
	}
	if len(opts.Args) > 0 {
		var ps []string
		for _, a := range opts.Args {
			ps = append(ps, athenaLiteral(a))
		}
		in["ExecutionParameters"] = ps
	}
	var started struct {
		QueryExecutionID string `json:"QueryExecutionId"`
	}
	if err := s.call(ctx, "StartQueryExecution", in, &started); err != nil {
		return Result{Err: err}
	}
	id := map[string]any{"QueryExecutionId": started.QueryExecutionID}
	var ex athenaExecution
	err := warehousePoll(ctx, func() (bool, error) {
		if err := s.call(ctx, "GetQueryExecution", id, &ex); err != nil {
			return false, err
		}
		st := ex.QueryExecution.Status.State
		return st != "QUEUED" && st != "RUNNING", nil
	})
	if err != nil {
		if ctx.Err() != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.call(sctx, "StopQueryExecution", id, nil)
			cancel()
		}
		return Result{Err: err}
	}
	if st := ex.QueryExecution.Status; st.State != "SUCCEEDED" {
		msg := st.StateChangeReason
		if msg == "" {
			msg = "query " + strings.ToLower(st.State)
		}
		return Result{Err: errors.New("Athena: " + s.scrub(msg))}
	}
	var res Result
	token := ""
	for first := true; ; first = false {
		page := map[string]any{"QueryExecutionId": started.QueryExecutionID, "MaxResults": 1000}
		if token != "" {
			page["NextToken"] = token
		}
		var r athenaResults
		if err := s.call(ctx, "GetQueryResults", page, &r); err != nil {
			res.Err = err
			return res
		}
		if first {
			res.Affected = r.UpdateCount
			for _, c := range r.ResultSet.ResultSetMetadata.ColumnInfo {
				res.Columns = append(res.Columns, Column{Name: c.Name, Type: c.Type})
			}
		}
		rows := make([][]*string, 0, len(r.ResultSet.Rows))
		for i, row := range r.ResultSet.Rows {
			vals := make([]*string, len(row.Data))
			for j, d := range row.Data {
				vals[j] = d.VarCharValue
			}
			if first && i == 0 && athenaHeaderRow(res.Columns, vals) {
				continue // a SELECT's first row repeats the column names
			}
			rows = append(rows, vals)
		}
		if !warehouseRows(&res, rows, opts.MaxRows) || r.NextToken == "" {
			if r.NextToken != "" && opts.MaxRows > 0 && len(res.Rows) >= opts.MaxRows {
				res.Truncated = true
			}
			return res
		}
		if opts.MaxRows > 0 && len(res.Rows) >= opts.MaxRows {
			res.Truncated = true
			return res
		}
		token = r.NextToken
	}
}

// athenaHeaderRow: vals are the column names.
func athenaHeaderRow(cols []Column, vals []*string) bool {
	if len(cols) == 0 || len(vals) != len(cols) {
		return false
	}
	for i, c := range cols {
		if vals[i] == nil || *vals[i] != c.Name {
			return false
		}
	}
	return true
}

func (s *AthenaSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	switch {
	case req.Database == "":
		out.Databases = []string{s.catalog()}
	case req.Schema == "":
		token := ""
		for range 50 {
			in := map[string]any{"CatalogName": req.Database}
			if token != "" {
				in["NextToken"] = token
			}
			var r struct {
				DatabaseList []struct {
					Name string `json:"Name"`
				} `json:"DatabaseList"`
				NextToken string `json:"NextToken"`
			}
			if err := s.call(ctx, "ListDatabases", in, &r); err != nil {
				return out, err
			}
			for _, d := range r.DatabaseList {
				out.Schemas = append(out.Schemas, d.Name)
			}
			if token = r.NextToken; token == "" {
				break
			}
		}
	case req.Object == "":
		token := ""
		for range 50 {
			in := map[string]any{"CatalogName": req.Database, "DatabaseName": req.Schema}
			if token != "" {
				in["NextToken"] = token
			}
			var r struct {
				TableMetadataList []struct {
					Name      string `json:"Name"`
					TableType string `json:"TableType"`
				} `json:"TableMetadataList"`
				NextToken string `json:"NextToken"`
			}
			if err := s.call(ctx, "ListTableMetadata", in, &r); err != nil {
				return out, err
			}
			for _, t := range r.TableMetadataList {
				kind := "BASE TABLE"
				if strings.Contains(t.TableType, "VIEW") {
					kind = "VIEW"
				}
				out.Objects = append(out.Objects, SchemaObject{Name: t.Name, Kind: kind})
			}
			if token = r.NextToken; token == "" {
				break
			}
		}
	default:
		var r struct {
			TableMetadata struct {
				Columns, PartitionKeys []struct {
					Name string `json:"Name"`
					Type string `json:"Type"`
				}
			} `json:"TableMetadata"`
		}
		if err := s.call(ctx, "GetTableMetadata", map[string]any{"CatalogName": req.Database, "DatabaseName": req.Schema, "TableName": req.Object}, &r); err != nil {
			return out, err
		}
		for _, c := range append(r.TableMetadata.Columns, r.TableMetadata.PartitionKeys...) {
			out.Columns = append(out.Columns, Column{Name: c.Name, Type: c.Type, Table: req.Object})
		}
	}
	return out, nil
}

func (s *AthenaSession) Begin(context.Context) error    { return errNoTx("Athena") }
func (s *AthenaSession) Commit(context.Context) error   { return errNoTx("Athena") }
func (s *AthenaSession) Rollback(context.Context) error { return errNoTx("Athena") }

// ReadOnlySession is a twin that runs reads only.
func (s *AthenaSession) ReadOnlySession() (Session, error) {
	twin := &awsJSONEngine{httpEngine: s.clone(), target: s.target, ctype: s.ctype}
	return &AthenaSession{awsJSONEngine: twin, conn: s.conn, readOnly: true}, nil
}
