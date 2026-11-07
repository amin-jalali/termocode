package dbclient

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// CouchbaseSession runs N1QL (SQL++) on the Couchbase query service
// (DBX-68): POST /query/service, port 8093 (18093 with TLS), basic auth.
// Read-only is the service's own readonly=true flag, so the server
// refuses a write even when the classifier is fooled. Buckets are the
// tree's databases, "scope.collection" its objects, and an object's
// columns are the top-level keys of its first documents.
type CouchbaseSession struct {
	*httpEngine
	readOnly bool
}

func init() {
	registerHTTPEngine(HTTPEngine{
		ID: "couchbase", Title: "Couchbase", Port: 8093, TLSPort: 18093,
		Hint:    "N1QL over the query service, default port 8093",
		Schemes: []string{"couchbase", "couchbases"},
		Open: func(c Conn, password string) (Session, error) {
			return NewCouchbaseSession(c, password)
		},
	})
}

// NewCouchbaseSession makes a session of c; it does not connect.
func NewCouchbaseSession(c Conn, password string) (*CouchbaseSession, error) {
	e, err := newHTTPEngine("Couchbase", c, password)
	if err != nil {
		return nil, err
	}
	return &CouchbaseSession{httpEngine: e}, nil
}

type n1qlRequest struct {
	Statement string `json:"statement"`
	Args      []any  `json:"args,omitempty"`
	ReadOnly  bool   `json:"readonly,omitempty"`
	Timeout   string `json:"timeout,omitempty"`
}

type n1qlResponse struct {
	Results []json.RawMessage `json:"results"`
	Status  string            `json:"status"`
	Errors  []struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	} `json:"errors"`
	Metrics struct {
		MutationCount int64 `json:"mutationCount"`
	} `json:"metrics"`
}

func (r n1qlResponse) err() error {
	if len(r.Errors) > 0 {
		return fmt.Errorf("Couchbase %d: %s", r.Errors[0].Code, r.Errors[0].Msg)
	}
	if r.Status != "" && r.Status != "success" {
		return fmt.Errorf("Couchbase: query %s", r.Status)
	}
	return nil
}

// query runs one statement and returns the decoded answer (the body of a
// 4xx/5xx answer carries N1QL errors too).
func (s *CouchbaseSession) query(ctx context.Context, stmt string, args []any) (n1qlResponse, error) {
	var out n1qlResponse
	body, _ := json.Marshal(n1qlRequest{Statement: stmt, Args: args, ReadOnly: s.readOnly, Timeout: "60s"})
	data, err := s.do(ctx, "POST", "/query/service", body, "")
	if len(data) > 0 && json.Unmarshal(data, &out) == nil {
		if e := out.err(); e != nil {
			return out, fmt.Errorf("%s", s.scrub(e.Error()))
		}
	}
	if err != nil {
		return out, err
	}
	return out, nil
}

func (s *CouchbaseSession) Ping(ctx context.Context) error {
	_, err := s.query(ctx, "SELECT 1", nil)
	return err
}

func (s *CouchbaseSession) Run(ctx context.Context, stmt string, opts RunOptions) Result {
	start := time.Now()
	resp, err := s.query(ctx, strings.TrimSpace(stmt), opts.Args)
	if err != nil {
		return Result{Err: err, Elapsed: time.Since(start)}
	}
	res := DocsResult(resp.Results, opts.MaxRows)
	res.Affected = resp.Metrics.MutationCount
	res.Elapsed = time.Since(start)
	return res
}

// n1qlIdent quotes one N1QL identifier with backticks.
func n1qlIdent(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }

// couchbaseKeyspace is `bucket`.`scope`.`collection` of a tree object
// ("scope.collection" or a bare collection in _default).
func couchbaseKeyspace(bucket, object string) string {
	scope, coll, ok := strings.Cut(object, ".")
	if !ok {
		scope, coll = "_default", object
	}
	return n1qlIdent(bucket) + "." + n1qlIdent(scope) + "." + n1qlIdent(coll)
}

// CouchbaseSampleSize is how many documents give an object its columns.
const CouchbaseSampleSize = 20

func (s *CouchbaseSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	bucket := req.Database
	if bucket == "" {
		bucket = req.Schema
	}
	if bucket == "" && req.Object == "" {
		resp, err := s.query(ctx, "SELECT RAW name FROM system:buckets ORDER BY name", nil)
		if err != nil {
			return out, err
		}
		out.Databases = rawStrings(resp.Results)
		return out, nil
	}
	if req.Object == "" {
		resp, err := s.query(ctx, "SELECT RAW k.`scope` || '.' || k.name FROM system:keyspaces AS k WHERE k.`bucket` = $1 ORDER BY k.`scope`, k.name", []any{bucket})
		if err != nil {
			return out, err
		}
		for _, n := range rawStrings(resp.Results) {
			out.Objects = append(out.Objects, SchemaObject{Name: n, Kind: "collection"})
		}
		return out, nil
	}
	resp, err := s.query(ctx, fmt.Sprintf("SELECT RAW d FROM %s AS d LIMIT %d", couchbaseKeyspace(bucket, req.Object), CouchbaseSampleSize), nil)
	if err != nil {
		return out, err
	}
	out.Columns = docColumns(resp.Results, req.Object)
	scope, coll, ok := strings.Cut(req.Object, ".")
	if !ok {
		scope, coll = "_default", req.Object
	}
	idx, err := s.query(ctx, "SELECT RAW name FROM system:indexes WHERE bucket_id = $1 AND scope_id = $2 AND keyspace_id = $3 ORDER BY name", []any{bucket, scope, coll})
	if err == nil {
		for _, n := range rawStrings(idx.Results) {
			out.Indexes = append(out.Indexes, SchemaIndex{Name: n})
		}
	}
	return out, nil
}

// rawStrings reads a RAW list of strings.
func rawStrings(rs []json.RawMessage) []string {
	var out []string
	for _, r := range rs {
		var s string
		if json.Unmarshal(r, &s) == nil {
			out = append(out, s)
		}
	}
	return out
}

// docColumns is the union of the top-level keys of sample documents with
// the JSON type of their first value; "id"/"_id" is the key column.
func docColumns(docs []json.RawMessage, table string) []Column {
	seen := map[string]bool{}
	var cols []Column
	for _, d := range docs {
		var obj map[string]json.RawMessage
		if json.Unmarshal(d, &obj) != nil {
			continue
		}
		for _, k := range orderedKeys(d) {
			if seen[k] {
				continue
			}
			seen[k] = true
			cols = append(cols, Column{Name: k, Type: jsonType(obj[k]), Table: table, PK: k == "_id" || k == "id"})
		}
	}
	sort.SliceStable(cols, func(i, j int) bool { return cols[i].PK && !cols[j].PK })
	return cols
}

// jsonType names the JSON type of a value.
func jsonType(v json.RawMessage) string {
	t := strings.TrimSpace(string(v))
	switch {
	case t == "" || t == "null":
		return "null"
	case t[0] == '"':
		return "string"
	case t[0] == '{':
		return "object"
	case t[0] == '[':
		return "array"
	case t == "true" || t == "false":
		return "boolean"
	}
	return "number"
}

func (s *CouchbaseSession) Begin(context.Context) error    { return errNoTx("Couchbase") }
func (s *CouchbaseSession) Commit(context.Context) error   { return errNoTx("Couchbase") }
func (s *CouchbaseSession) Rollback(context.Context) error { return errNoTx("Couchbase") }

// ReadOnlySession is a twin that sends readonly=true with every statement.
func (s *CouchbaseSession) ReadOnlySession() (Session, error) {
	return &CouchbaseSession{httpEngine: s.clone(), readOnly: true}, nil
}
