package dbclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInfluxClassifyAndFlux(t *testing.T) {
	for in, want := range map[string]Kind{
		"": KindUnknown, "SELECT * FROM m": KindRead, "SELECT * INTO n FROM m": KindWrite, "SHOW DATABASES": KindRead,
		"DROP DATABASE x": KindDDL, "DELETE FROM m": KindDDL, "CREATE DATABASE x": KindWrite, "SELECT 1; DROP MEASUREMENT m": KindDDL,
		"SELECT 1;;": KindRead, `from(bucket:"b") |> range(start: -1h)`: KindRead, `from(bucket:"b") |> to(bucket:"c")`: KindWrite,
		`import "sql"` + "\nsql.to(x)": KindWrite,
	} {
		if got := ClassifyInflux(in); got != want {
			t.Errorf("ClassifyInflux(%q) = %v, want %v", in, got, want)
		}
	}
	if !IsFlux("from(bucket: \"b\")") || !IsFlux("import \"x\"") || IsFlux("SELECT 1") {
		t.Error("IsFlux")
	}
	csvData := "#datatype,string,long\n,result,table,_time,_value\n,_result,0,2024,1\n,_result,0,2025,2\n\n,result,table,other\n,_result,1,x\n"
	r := FluxCSVResult([]byte(csvData), 0)
	if r.Err != nil || len(r.Columns) != 5 || len(r.Rows) != 3 || !r.Rows[2][3].Null {
		t.Errorf("flux csv = %+v", r)
	}
	if r := FluxCSVResult([]byte(csvData), 1); !r.Truncated || len(r.Rows) != 1 {
		t.Errorf("cut flux = %+v", r)
	}
	if r := FluxCSVResult([]byte(`{"error":"bad flux"}`), 0); r.Err == nil || !strings.Contains(r.Err.Error(), "bad flux") {
		t.Errorf("an error body = %+v", r)
	}
	if r := FluxCSVResult(nil, 0); r.Err != nil || len(r.Rows) != 0 {
		t.Errorf("no data = %+v", r)
	}
}

func TestInfluxSessionQueries(t *testing.T) {
	var lastPath, lastMethod, lastType, lastAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastPath, lastMethod, lastType, lastAuth = r.URL.RequestURI(), r.Method, r.Header.Get("Content-Type"), r.Header.Get("Authorization")
		q := r.URL.Query().Get("q")
		if q == "" {
			q = string(b)
		}
		switch {
		case r.URL.Path == "/ping":
		case strings.HasPrefix(r.URL.Path, "/api/v2/query"):
			_, _ = w.Write([]byte(",result,table,_value\n,_result,0,5\n"))
		case strings.Contains(q, "SHOW DATABASES"):
			_, _ = w.Write([]byte(`{"results":[{"series":[{"name":"databases","columns":["name"],"values":[["a"],["b"]]}]}]}`))
		case strings.Contains(q, "SHOW MEASUREMENTS"):
			_, _ = w.Write([]byte(`{"results":[{"series":[{"name":"measurements","columns":["name"],"values":[["cpu"]]}]}]}`))
		case strings.Contains(q, "SHOW TAG KEYS"):
			_, _ = w.Write([]byte(`{"results":[{"series":[{"columns":["tagKey"],"values":[["host"]]}]}]}`))
		case strings.Contains(q, "SHOW FIELD KEYS"):
			_, _ = w.Write([]byte(`{"results":[{"series":[{"columns":["fieldKey","fieldType"],"values":[["usage","float"]]}]}]}`))
		case strings.Contains(q, "multi"):
			_, _ = w.Write([]byte(`{"results":[{"series":[{"name":"a","tags":{"h":"1"},"columns":["time","v"],"values":[[1,2],[3]]},{"name":"b","columns":["time","w"],"values":[[1,2]]}]}]}`))
		case strings.Contains(q, "resulterr"):
			_, _ = w.Write([]byte(`{"results":[{"error":"measurement not found"}]}`))
		case strings.Contains(q, "toperr"):
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"error parsing query"}`))
		case strings.Contains(q, "html"):
			_, _ = w.Write([]byte(`<html>`))
		case strings.Contains(q, "status500"):
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			_, _ = w.Write([]byte(`{"results":[{"series":[{"columns":["v"],"values":[[1],[2],[3]]}]}]}`))
		}
	}))
	defer srv.Close()
	s, err := NewInfluxDBSession(Conn{URL: srv.URL, Database: "d", Params: map[string]string{"org": "o"}}, "tok")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil || lastAuth != "Token tok" {
		t.Errorf("ping %v auth %q", err, lastAuth)
	}
	if r := s.Run(ctx, "SELECT v FROM m", RunOptions{MaxRows: 2}); r.Err != nil || len(r.Rows) != 2 || !r.Truncated || lastMethod != "GET" {
		t.Errorf("select = %+v via %s", r, lastMethod)
	}
	if r := s.Run(ctx, "CREATE DATABASE x", RunOptions{}); r.Err != nil || lastMethod != "POST" || lastType != "application/x-www-form-urlencoded" {
		t.Errorf("create = %+v via %s %s", r, lastMethod, lastType)
	}
	multi := s.Run(ctx, "SELECT multi", RunOptions{})
	if multi.Err != nil || len(multi.Rows) != 3 || multi.Columns[0].Name != "name" {
		t.Errorf("multi = %+v", multi)
	}
	for q, why := range map[string]string{"": "write an InfluxQL", "SELECT resulterr": "measurement not found", "SELECT toperr": "error parsing query", "SELECT html": "not a JSON", "SELECT status500": "HTTP 500"} {
		if r := s.Run(ctx, q, RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), why) {
			t.Errorf("%q = %v, want %q", q, r.Err, why)
		}
	}
	if r := s.Run(ctx, `from(bucket:"b") |> range(start: -1h)`, RunOptions{}); r.Err != nil || len(r.Rows) != 1 {
		t.Errorf("flux = %+v (path %s)", r, lastPath)
	}
	noOrg, _ := NewInfluxDBSession(Conn{URL: srv.URL}, "")
	if r := noOrg.Run(ctx, `from(bucket:"b") |> x`, RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "organization") {
		t.Errorf("flux without org = %v", r.Err)
	}
	ro, _ := s.ReadOnlySession()
	if r := ro.Run(ctx, "DROP DATABASE x", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "read-only") {
		t.Errorf("read-only drop = %v", r.Err)
	}
	dbs, err := s.Introspect(ctx, IntrospectionRequest{Database: "d"})
	if err != nil || len(dbs.Objects) != 1 || dbs.Objects[0].Name != "cpu" {
		t.Errorf("measurements = %+v %v", dbs, err)
	}
	cols, err := s.Introspect(ctx, IntrospectionRequest{Database: "d", Object: `we"ird`})
	if err != nil || len(cols.Columns) < 2 {
		t.Errorf("columns = %+v %v", cols, err)
	}
	top, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || len(top.Databases) != 2 {
		t.Errorf("databases = %+v %v", top, err)
	}
	for _, f := range []func(context.Context) error{s.Begin, s.Commit, s.Rollback} {
		if f(ctx) == nil {
			t.Error("InfluxDB has no transactions")
		}
	}
	srv.Close()
	if _, err := s.Introspect(ctx, IntrospectionRequest{Database: "d"}); err == nil {
		t.Error("a server that is gone: want error")
	}
	if _, err := s.Introspect(ctx, IntrospectionRequest{Database: "d", Object: "m"}); err == nil {
		t.Error("a server that is gone: want error")
	}
	if _, err := NewInfluxDBSession(Conn{}, ""); err == nil {
		t.Error("no address: want error")
	}
}
