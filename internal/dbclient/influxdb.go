package dbclient

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// InfluxDBSession is an InfluxQL and Flux console on InfluxDB (DBX-70),
// port 8086, with net/http only.
//
//   - InfluxQL goes to /query (v1, and the v2 compatibility API). Reads
//     (SELECT, SHOW, EXPLAIN) use GET, which the server itself refuses
//     for anything else; other statements use POST and ask first.
//   - Flux (a query with "|>" or that starts with from( or import) goes to
//     /api/v2/query with the org of params.org; it answers CSV.
//
// A user signs in with basic auth; a password without a user is a v2 API
// token ("Authorization: Token …"). The tree lists the databases
// (buckets), their measurements, and a measurement's tags and fields.
type InfluxDBSession struct {
	*httpEngine
	db       string
	org      string
	readOnly bool
}

func init() {
	registerHTTPEngine(HTTPEngine{
		ID: "influxdb", Title: "InfluxDB", Port: 8086,
		Hint:     "InfluxQL and Flux, default port 8086",
		Schemes:  []string{"influxdb", "influxdbs", "influx"},
		Classify: ClassifyInflux,
		Open: func(c Conn, password string) (Session, error) {
			return NewInfluxDBSession(c, password)
		},
	})
}

// NewInfluxDBSession makes a session of c; it does not connect.
func NewInfluxDBSession(c Conn, password string) (*InfluxDBSession, error) {
	e, err := newHTTPEngine("InfluxDB", c, password)
	if err != nil {
		return nil, err
	}
	if e.user == "" && e.password != "" {
		e.header.Set("Authorization", "Token "+e.password)
	}
	return &InfluxDBSession{httpEngine: e, db: c.Database, org: c.Params["org"]}, nil
}

// IsFlux reports a Flux query (else InfluxQL).
func IsFlux(q string) bool {
	t := strings.TrimSpace(q)
	return strings.Contains(t, "|>") || strings.HasPrefix(t, "from(") || strings.HasPrefix(t, "import ")
}

var fluxWrite = regexp.MustCompile(`(^|[^A-Za-z0-9_.])(to|wideTo|delete)\s*\(|\bsql\.to\s*\(|\bhttp\.post\s*\(|\bexperimental\.to\s*\(`)

// ClassifyInflux judges one query: InfluxQL SELECT (without INTO), SHOW
// and EXPLAIN read, DELETE and DROP are DDL, the rest writes; Flux reads
// unless it calls to(), wideTo(), sql.to() or http.post().
func ClassifyInflux(text string) Kind {
	q := stripLineComments(strings.TrimSuffix(strings.TrimSpace(text), ";"))
	if q == "" {
		return KindUnknown
	}
	if IsFlux(q) {
		if fluxWrite.MatchString(q) {
			return KindWrite
		}
		return KindRead
	}
	worst := KindRead
	for _, st := range strings.Split(q, ";") {
		f := strings.Fields(strings.ToUpper(st))
		if len(f) == 0 {
			continue
		}
		k := KindWrite
		switch f[0] {
		case "SELECT":
			k = KindRead
			for _, w := range f {
				if w == "INTO" {
					k = KindWrite
				}
			}
		case "SHOW", "EXPLAIN":
			k = KindRead
		case "DELETE", "DROP":
			k = KindDDL
		}
		if severity(k) > severity(worst) {
			worst = k
		}
	}
	return worst
}

func (s *InfluxDBSession) Ping(ctx context.Context) error {
	_, err := s.do(ctx, "GET", "/ping", nil, "")
	return err
}

func (s *InfluxDBSession) Run(ctx context.Context, text string, opts RunOptions) Result {
	start := time.Now()
	q := stripLineComments(strings.TrimSpace(text))
	var res Result
	switch {
	case q == "":
		res = Result{Err: errors.New("influxdb: write an InfluxQL or Flux query")}
	case s.readOnly && ClassifyInflux(q) != KindRead:
		res = Result{Err: errors.New("influxdb: the session is read-only")}
	case IsFlux(q):
		res = s.flux(ctx, q, opts.MaxRows)
	default:
		res = s.influxQL(ctx, s.db, strings.TrimSuffix(q, ";"), opts.MaxRows)
	}
	res.Elapsed = time.Since(start)
	return res
}

type influxQLResponse struct {
	Results []struct {
		Error  string `json:"error"`
		Series []struct {
			Name    string              `json:"name"`
			Tags    map[string]string   `json:"tags"`
			Columns []string            `json:"columns"`
			Values  [][]json.RawMessage `json:"values"`
		} `json:"series"`
	} `json:"results"`
	Error string `json:"error"`
}

// influxQL runs q on db: GET for reads, POST for the rest.
func (s *InfluxDBSession) influxQL(ctx context.Context, db, q string, maxRows int) Result {
	v := url.Values{"q": {q}, "epoch": {"ms"}}
	if db != "" {
		v.Set("db", db)
	}
	method, path, body, ctype := "GET", "/query?"+v.Encode(), []byte(nil), ""
	if ClassifyInflux(q) != KindRead {
		method, path, body, ctype = "POST", "/query", []byte(v.Encode()), "application/x-www-form-urlencoded"
	}
	data, err := s.do(ctx, method, path, body, ctype)
	var out influxQLResponse
	if jerr := json.Unmarshal(data, &out); jerr != nil {
		if err != nil {
			return Result{Err: err}
		}
		return Result{Err: errors.New("influxdb: not a JSON reply")}
	}
	if out.Error != "" {
		return Result{Err: errors.New("influxdb: " + s.scrub(out.Error))}
	}
	if err != nil {
		return Result{Err: err}
	}
	res := Result{}
	index := map[string]int{}
	col := func(name, typ string) int {
		if i, ok := index[name]; ok {
			return i
		}
		index[name] = len(res.Columns)
		res.Columns = append(res.Columns, Column{Name: name, Type: typ})
		return index[name]
	}
	type cellAt struct {
		i int
		c Cell
	}
	var rows [][]cellAt
	multi := false
	for _, r := range out.Results {
		if r.Error != "" {
			return Result{Err: errors.New("influxdb: " + s.scrub(r.Error))}
		}
		if len(r.Series) > 1 {
			multi = true
		}
		for _, sr := range r.Series {
			for _, vals := range sr.Values {
				if maxRows > 0 && len(rows) >= maxRows {
					res.Truncated = true
					break
				}
				var row []cellAt
				if multi || len(sr.Tags) > 0 {
					row = append(row, cellAt{col("name", "measurement"), Cell{Text: sr.Name}})
				}
				for k, v := range sr.Tags {
					row = append(row, cellAt{col(k, "tag"), Cell{Text: v}})
				}
				for i, c := range sr.Columns {
					if i < len(vals) {
						row = append(row, cellAt{col(c, ""), JSONCell(vals[i])})
					}
				}
				rows = append(rows, row)
			}
		}
	}
	for _, r := range rows {
		cells := make([]Cell, len(res.Columns))
		for i := range cells {
			cells[i] = Cell{Null: true}
		}
		for _, c := range r {
			cells[c.i] = c.c
		}
		res.Rows = append(res.Rows, cells)
	}
	return res
}

// flux runs a Flux query; the annotated CSV becomes one grid (the
// annotation rows and the leading blank column dropped, tables joined).
func (s *InfluxDBSession) flux(ctx context.Context, q string, maxRows int) Result {
	if s.org == "" {
		return Result{Err: errors.New(`influxdb: Flux needs the organization: set "params": {"org": "…"}`)}
	}
	body, _ := json.Marshal(map[string]any{"query": q, "type": "flux", "dialect": map[string]any{"header": true, "annotations": []string{}}})
	data, err := s.do(ctx, "POST", "/api/v2/query?org="+url.QueryEscape(s.org), body, "")
	if err != nil {
		return Result{Err: err}
	}
	return FluxCSVResult(data, maxRows)
}

// FluxCSVResult reads Flux CSV: each table starts with a header row; a
// blank line separates result sets. Columns are the union of headers.
func FluxCSVResult(data []byte, maxRows int) Result {
	res := Result{}
	index := map[string]int{}
	var rows [][]Cell
	for _, block := range bytes.Split(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\n\n")) {
		r := csv.NewReader(bytes.NewReader(block))
		r.FieldsPerRecord = -1
		var header []int
		for {
			rec, err := r.Read()
			if err == io.EOF || err != nil {
				break
			}
			if len(rec) > 0 && strings.HasPrefix(rec[0], "#") {
				continue
			}
			if header == nil || (len(rec) > 2 && rec[1] == "result" && rec[2] == "table") {
				header = header[:0]
				for _, h := range rec {
					if h == "" {
						header = append(header, -1)
						continue
					}
					i, ok := index[h]
					if !ok {
						i = len(res.Columns)
						index[h] = i
						res.Columns = append(res.Columns, Column{Name: h})
					}
					header = append(header, i)
				}
				continue
			}
			if maxRows > 0 && len(rows) >= maxRows {
				res.Truncated = true
				break
			}
			cells := map[int]Cell{}
			for j, v := range rec {
				if j < len(header) && header[j] >= 0 {
					cells[header[j]] = Cell{Text: v}
				}
			}
			out := make([]Cell, 0)
			for i := 0; i < len(res.Columns); i++ {
				if c, ok := cells[i]; ok {
					out = append(out, c)
				} else {
					out = append(out, Cell{Null: true})
				}
			}
			rows = append(rows, out)
		}
	}
	for _, r := range rows {
		for len(r) < len(res.Columns) {
			r = append(r, Cell{Null: true})
		}
		res.Rows = append(res.Rows, r)
	}
	if len(res.Columns) == 0 && len(bytes.TrimSpace(data)) > 0 {
		return Result{Err: fmt.Errorf("influxdb: %s", httpErrorMessage(data))}
	}
	return res
}

// Introspect: databases (SHOW DATABASES), measurements, then a
// measurement's tag keys and field keys as columns.
func (s *InfluxDBSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	db := req.Database
	if db == "" {
		db = req.Schema
	}
	first := func(r Result) []string {
		var names []string
		for _, row := range r.Rows {
			for i, c := range r.Columns {
				if c.Name == "name" || (len(r.Columns) == 1 && i == 0) {
					names = append(names, row[i].Text)
				}
			}
		}
		return names
	}
	if db == "" && req.Object == "" {
		r := s.influxQL(ctx, "", "SHOW DATABASES", 0)
		out.Databases = first(r)
		return out, r.Err
	}
	if req.Object == "" {
		r := s.influxQL(ctx, db, "SHOW MEASUREMENTS", 0)
		for _, n := range first(r) {
			out.Objects = append(out.Objects, SchemaObject{Name: n, Kind: "measurement"})
		}
		return out, r.Err
	}
	m := `"` + strings.ReplaceAll(req.Object, `"`, `\"`) + `"`
	out.Columns = []Column{{Name: "time", Type: "time", Table: req.Object, PK: true}}
	tags := s.influxQL(ctx, db, "SHOW TAG KEYS FROM "+m, 0)
	if tags.Err != nil {
		return out, tags.Err
	}
	for _, row := range tags.Rows {
		for i, c := range tags.Columns {
			if c.Name == "tagKey" {
				out.Columns = append(out.Columns, Column{Name: row[i].Text, Type: "tag", Table: req.Object})
			}
		}
	}
	fields := s.influxQL(ctx, db, "SHOW FIELD KEYS FROM "+m, 0)
	for _, row := range fields.Rows {
		name, typ := "", ""
		for i, c := range fields.Columns {
			switch c.Name {
			case "fieldKey":
				name = row[i].Text
			case "fieldType":
				typ = row[i].Text
			}
		}
		if name != "" {
			out.Columns = append(out.Columns, Column{Name: name, Type: typ, Table: req.Object})
		}
	}
	return out, fields.Err
}

func (s *InfluxDBSession) Begin(context.Context) error    { return errNoTx("InfluxDB") }
func (s *InfluxDBSession) Commit(context.Context) error   { return errNoTx("InfluxDB") }
func (s *InfluxDBSession) Rollback(context.Context) error { return errNoTx("InfluxDB") }

func (s *InfluxDBSession) ReadOnlySession() (Session, error) {
	return &InfluxDBSession{httpEngine: s.clone(), db: s.db, org: s.org, readOnly: true}, nil
}
