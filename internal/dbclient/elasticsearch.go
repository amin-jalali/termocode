package dbclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ElasticsearchSession is a REST console on Elasticsearch or OpenSearch
// (DBX-70), port 9200, with net/http only. A request is a method line
// and an optional JSON body, the way the docs write it:
//
//	GET /logs-*/_search
//	{"query": {"match": {"msg": "timeout"}}, "size": 20}
//
// Search hits show as rows (_index, _id, _score, then the _source keys);
// aggregations come as a note; a JSON array (the _cat APIs ask for
// format=json by themselves) is one row per element. The tree lists the
// indices and an index's mapping as columns. Searches and other reads
// run at once; any other request asks first, DELETE with the strong card.
// The read-only twin refuses every request that is not a read.
type ElasticsearchSession struct {
	*httpEngine
	readOnly bool
}

func init() {
	registerHTTPEngine(HTTPEngine{
		ID: "elasticsearch", Title: "Elasticsearch / OpenSearch", Port: 9200,
		Hint:     "the REST API with a _search console, default port 9200",
		Schemes:  []string{"elasticsearch", "elasticsearchs", "opensearch", "opensearchs"},
		Classify: ClassifyESRequest,
		Open: func(c Conn, password string) (Session, error) {
			return NewElasticsearchSession(c, password)
		},
	})
}

// NewElasticsearchSession makes a session of c; it does not connect. A
// password without a user is an API key ("Authorization: ApiKey …").
func NewElasticsearchSession(c Conn, password string) (*ElasticsearchSession, error) {
	e, err := newHTTPEngine("Elasticsearch", c, password)
	if err != nil {
		return nil, err
	}
	if e.user == "" && e.password != "" {
		e.header.Set("Authorization", "ApiKey "+e.password)
	}
	return &ElasticsearchSession{httpEngine: e}, nil
}

// ESRequest is one console request.
type ESRequest struct {
	Method string
	Path   string
	Body   []byte
}

// ParseESRequest reads "METHOD /path" and the JSON body under it.
func ParseESRequest(text string) (ESRequest, error) {
	text = stripLineComments(strings.TrimSuffix(strings.TrimSpace(text), ";"))
	first, body, _ := strings.Cut(text, "\n")
	f := strings.Fields(first)
	if len(f) != 2 {
		return ESRequest{}, errors.New("elasticsearch: write METHOD /path, then an optional JSON body")
	}
	m := strings.ToUpper(f[0])
	switch m {
	case "GET", "POST", "PUT", "DELETE", "HEAD":
	default:
		return ESRequest{}, fmt.Errorf("elasticsearch: unknown method %q", f[0])
	}
	p := f[1]
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	r := ESRequest{Method: m, Path: p}
	if b := bytes.TrimSpace([]byte(body)); len(b) > 0 {
		// One JSON value, or NDJSON lines (_bulk, _msearch).
		if !json.Valid(b) {
			for _, l := range bytes.Split(b, []byte("\n")) {
				if l = bytes.TrimSpace(l); len(l) > 0 && !json.Valid(l) {
					return ESRequest{}, errors.New("elasticsearch: the body is not JSON")
				}
			}
			b = append(b, '\n')
		}
		r.Body = b
	}
	return r, nil
}

// esReadEndpoints are POST endpoints that only read.
var esReadEndpoints = []string{"_search", "_msearch", "_count", "_explain", "_validate", "_field_caps", "_mget", "_termvectors", "_mtermvectors", "_rank_eval", "_search_shards", "_sql", "_eql", "_terms_enum", "_analyze"}

// ClassifyESRequest judges one request: GET and HEAD read (except a few
// that change state), so do the search-like POSTs; DELETE is DDL, the
// rest are writes. A body that does not parse is unknown.
func ClassifyESRequest(text string) Kind {
	r, err := ParseESRequest(text)
	if err != nil {
		return KindUnknown
	}
	path, _, _ := strings.Cut(r.Path, "?")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	last := parts[len(parts)-1]
	switch r.Method {
	case "GET", "HEAD":
		return KindRead
	case "POST":
		for _, p := range parts {
			for _, e := range esReadEndpoints {
				if p == e {
					if e == "_sql" && strings.Contains(path, "/_sql/close") {
						return KindWrite
					}
					return KindRead
				}
			}
		}
		if last == "_delete_by_query" {
			return KindDDL
		}
		return KindWrite
	case "DELETE":
		return KindDDL
	}
	return KindWrite
}

// withJSONFormat adds format=json to a _cat request.
func withJSONFormat(p string) string {
	if !strings.HasPrefix(p, "/_cat") || strings.Contains(p, "format=") {
		return p
	}
	if strings.Contains(p, "?") {
		return p + "&format=json"
	}
	return p + "?format=json"
}

func (s *ElasticsearchSession) Ping(ctx context.Context) error {
	_, err := s.do(ctx, "GET", "/", nil, "")
	return err
}

func (s *ElasticsearchSession) Run(ctx context.Context, text string, opts RunOptions) Result {
	start := time.Now()
	res := s.runOne(ctx, text, opts)
	res.Elapsed = time.Since(start)
	return res
}

func (s *ElasticsearchSession) runOne(ctx context.Context, text string, opts RunOptions) Result {
	r, err := ParseESRequest(text)
	if err != nil {
		return Result{Err: err}
	}
	if s.readOnly && ClassifyESRequest(text) != KindRead {
		return Result{Err: errors.New("elasticsearch: the session is read-only")}
	}
	ctype := ""
	if len(r.Body) > 0 && r.Body[len(r.Body)-1] == '\n' {
		ctype = "application/x-ndjson"
	}
	var body []byte
	if len(r.Body) > 0 {
		body = r.Body
	}
	data, err := s.do(ctx, r.Method, withJSONFormat(r.Path), body, ctype)
	if err != nil {
		return Result{Err: err}
	}
	return ESResult(data, opts.MaxRows)
}

// ESResult turns an answer into a grid.
func ESResult(data []byte, maxRows int) Result {
	t := bytes.TrimSpace(data)
	if len(t) == 0 {
		return Result{}
	}
	if t[0] == '[' {
		var arr []json.RawMessage
		if json.Unmarshal(t, &arr) == nil {
			return DocsResult(arr, maxRows)
		}
	}
	var top struct {
		Hits *struct {
			Total json.RawMessage `json:"total"`
			Hits  []struct {
				Index  string          `json:"_index"`
				ID     string          `json:"_id"`
				Score  json.RawMessage `json:"_score"`
				Source json.RawMessage `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
		Aggs json.RawMessage `json:"aggregations"`
		// The SQL API answers columns and rows.
		Columns []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"columns"`
		Rows [][]json.RawMessage `json:"rows"`
	}
	if json.Unmarshal(t, &top) != nil {
		return Result{Columns: []Column{{Name: "response"}}, Rows: [][]Cell{{{Text: RedisDisplay(string(t))}}}}
	}
	if top.Hits != nil {
		docs := make([]json.RawMessage, 0, len(top.Hits.Hits))
		for _, h := range top.Hits.Hits {
			var src map[string]json.RawMessage
			_ = json.Unmarshal(h.Source, &src)
			meta := map[string]any{"_index": h.Index, "_id": h.ID, "_score": h.Score}
			b, _ := json.Marshal(meta)
			// Keep the meta columns first, then the source in its order.
			doc := append([]byte(nil), b[:len(b)-1]...)
			if len(src) > 0 {
				s := bytes.TrimSpace(h.Source)
				doc = append(append(doc, ','), s[1:]...)
			} else {
				doc = append(doc, '}')
			}
			docs = append(docs, json.RawMessage(doc))
		}
		res := DocsResult(docs, maxRows)
		if len(top.Hits.Total) > 0 {
			res.Notices = append(res.Notices, "Total hits: "+esTotal(top.Hits.Total))
		}
		if len(top.Aggs) > 0 {
			var b bytes.Buffer
			if json.Indent(&b, top.Aggs, "", "  ") == nil {
				res.Notices = append(res.Notices, "Aggregations:\n"+b.String())
			}
		}
		return res
	}
	if len(top.Columns) > 0 {
		res := Result{}
		for _, c := range top.Columns {
			res.Columns = append(res.Columns, Column{Name: c.Name, Type: c.Type})
		}
		for _, r := range top.Rows {
			if maxRows > 0 && len(res.Rows) >= maxRows {
				res.Truncated = true
				break
			}
			row := make([]Cell, len(res.Columns))
			for i := range row {
				if i < len(r) {
					row[i] = JSONCell(r[i])
				} else {
					row[i] = Cell{Null: true}
				}
			}
			res.Rows = append(res.Rows, row)
		}
		return res
	}
	return DocsResult([]json.RawMessage{t}, maxRows)
}

func esTotal(t json.RawMessage) string {
	var v struct {
		Value    int64  `json:"value"`
		Relation string `json:"relation"`
	}
	if json.Unmarshal(t, &v) == nil && v.Relation != "" {
		if v.Relation == "gte" {
			return fmt.Sprintf("%d or more", v.Value)
		}
		return fmt.Sprint(v.Value)
	}
	return string(t)
}

// Introspect: the connection lists "indices", which lists the indices
// (hidden ones too, sorted); an index's columns come from its mapping,
// nested fields as dotted names.
func (s *ElasticsearchSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	if req.Database == "" && req.Schema == "" && req.Object == "" {
		out.Databases = []string{"indices"}
		return out, nil
	}
	if req.Object == "" {
		var rows []struct {
			Index string `json:"index"`
		}
		if err := s.doJSON(ctx, "GET", "/_cat/indices?format=json&h=index&expand_wildcards=all", nil, &rows); err != nil {
			return out, err
		}
		names := make([]string, 0, len(rows))
		for _, r := range rows {
			names = append(names, r.Index)
		}
		sort.Strings(names)
		for _, n := range names {
			out.Objects = append(out.Objects, SchemaObject{Name: n, Kind: "index"})
		}
		return out, nil
	}
	var m map[string]struct {
		Mappings struct {
			Properties map[string]esField `json:"properties"`
		} `json:"mappings"`
	}
	if err := s.doJSON(ctx, "GET", "/"+escapeESPath(req.Object)+"/_mapping", nil, &m); err != nil {
		return out, err
	}
	out.Columns = []Column{{Name: "_id", Type: "keyword", Table: req.Object, PK: true}}
	for _, idx := range m {
		out.Columns = append(out.Columns, esColumns("", idx.Mappings.Properties, req.Object)...)
		break
	}
	return out, nil
}

type esField struct {
	Type       string             `json:"type"`
	Properties map[string]esField `json:"properties"`
}

func esColumns(prefix string, props map[string]esField, table string) []Column {
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []Column
	for _, n := range names {
		f := props[n]
		if len(f.Properties) > 0 {
			out = append(out, esColumns(prefix+n+".", f.Properties, table)...)
			continue
		}
		t := f.Type
		if t == "" {
			t = "object"
		}
		out = append(out, Column{Name: prefix + n, Type: t, Table: table})
	}
	return out
}

// escapeESPath keeps an index name inside one path segment.
func escapeESPath(s string) string {
	r := strings.NewReplacer("/", "%2F", "?", "%3F", "#", "%23", " ", "%20")
	return r.Replace(s)
}

func (s *ElasticsearchSession) Begin(context.Context) error    { return errNoTx("Elasticsearch") }
func (s *ElasticsearchSession) Commit(context.Context) error   { return errNoTx("Elasticsearch") }
func (s *ElasticsearchSession) Rollback(context.Context) error { return errNoTx("Elasticsearch") }

func (s *ElasticsearchSession) ReadOnlySession() (Session, error) {
	return &ElasticsearchSession{httpEngine: s.clone(), readOnly: true}, nil
}
