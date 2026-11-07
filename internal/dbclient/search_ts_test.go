package dbclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPrometheusSessionInstantRangeAndTree(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		if r.Method != "GET" {
			t.Errorf("Prometheus must only GET, got %s", r.Method)
		}
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"","result":null}}`)
		case "/api/v1/query":
			_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"job":"api"},"value":[1700000000,"3"]}]}}`)
		case "/api/v1/query_range":
			_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"job":"api"},"values":[[1700000000,"1"],[1700000060,"4"],[1700000120,"2"]]}]}}`)
		case "/api/v1/label/__name__/values":
			_, _ = io.WriteString(w, `{"status":"success","data":["up","http_requests_total"]}`)
		case "/api/v1/labels":
			_, _ = io.WriteString(w, `{"status":"success","data":["__name__","instance","job"]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s, err := NewPrometheusSession(Conn{Driver: "prometheus", URL: srv.URL}, "")
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Unix(1700000120, 0) }
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	res := s.Run(ctx, "-- @conn prom\nup", RunOptions{})
	if res.Err != nil || len(res.Rows) != 1 || res.Rows[0][2].Text != "3" {
		t.Fatalf("instant = %+v", res)
	}
	res = s.Run(ctx, "range 2m 1m rate(x[5m])", RunOptions{})
	if res.Err != nil || len(res.Rows) != 1 || res.Rows[0][2].Text != "2" || res.Rows[0][4].Text != "4" || len(res.Notices) != PromChartHeight+2 {
		t.Fatalf("range = %+v", res)
	}
	if last := got[len(got)-1]; !strings.Contains(last, "step=60") || !strings.Contains(last, "query=rate") {
		t.Fatalf("range request = %s", last)
	}
	in, err := s.Introspect(ctx, IntrospectionRequest{Database: "metrics"})
	if err != nil || len(in.Objects) != 2 || in.Objects[0].Name != "http_requests_total" {
		t.Fatalf("metrics = %+v %v", in.Objects, err)
	}
	in, err = s.Introspect(ctx, IntrospectionRequest{Database: "metrics", Object: "up"})
	if err != nil || len(in.Columns) != 3 || in.Columns[0].Name != "instance" {
		t.Fatalf("labels = %+v %v", in.Columns, err)
	}
	if k, _ := Classify("sum(rate(x[5m]))", "prometheus"); k != KindRead {
		t.Fatalf("PromQL is a read: %v", k)
	}
}

func TestParsePromRange(t *testing.T) {
	w, st, expr, ok, err := ParsePromRange("range 1h up")
	if !ok || err != nil || w != time.Hour || st != time.Minute || expr != "up" {
		t.Fatalf("got %v %v %q %v %v", w, st, expr, ok, err)
	}
	w, st, expr, _, err = ParsePromRange("range 1d 5m sum by (job) (up)")
	if err != nil || w != 24*time.Hour || st != 5*time.Minute || expr != "sum by (job) (up)" {
		t.Fatalf("got %v %v %q %v", w, st, expr, err)
	}
	if _, _, _, ok, _ := ParsePromRange("up"); ok {
		t.Fatal("a plain expression is no range")
	}
	if _, _, _, _, err := ParsePromRange("range 30d 1s up"); err == nil {
		t.Fatal("too many points must be refused")
	}
}

func TestElasticsearchSessionSearchCatAndMapping(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.RequestURI())
		if u, p, ok := r.BasicAuth(); !ok || u != "elastic" || p != "pw-1" {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"type":"security_exception","reason":"missing authentication credentials"},"status":401}`)
			return
		}
		switch {
		case r.URL.Path == "/":
			_, _ = io.WriteString(w, `{"version":{"number":"8.11.0"}}`)
		case strings.HasSuffix(r.URL.Path, "/_search"):
			b, _ := io.ReadAll(r.Body)
			if !json.Valid(b) {
				t.Errorf("body = %s", b)
			}
			_, _ = io.WriteString(w, `{"hits":{"total":{"value":2,"relation":"eq"},"hits":[{"_index":"logs","_id":"1","_score":1.0,"_source":{"msg":"timeout","level":"warn"}},{"_index":"logs","_id":"2","_score":0.5,"_source":{"msg":"ok"}}]},"aggregations":{"by_level":{"buckets":[]}}}`)
		case strings.HasPrefix(r.URL.Path, "/_cat/indices"):
			if r.URL.Query().Get("format") != "json" {
				t.Errorf("_cat without format=json: %s", r.URL)
			}
			_, _ = io.WriteString(w, `[{"index":"logs"},{"index":".kibana"}]`)
		case r.URL.Path == "/logs/_mapping":
			_, _ = io.WriteString(w, `{"logs":{"mappings":{"properties":{"msg":{"type":"text"},"user":{"properties":{"name":{"type":"keyword"}}}}}}}`)
		case r.Method == "DELETE":
			_, _ = io.WriteString(w, `{"acknowledged":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s, err := NewElasticsearchSession(Conn{Driver: "elasticsearch", URL: srv.URL, User: "elastic"}, "pw-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	res := s.Run(ctx, "GET /logs/_search\n{\"query\": {\"match\": {\"msg\": \"timeout\"}}};", RunOptions{})
	if res.Err != nil || len(res.Rows) != 2 || len(res.Notices) != 2 || !strings.Contains(res.Notices[0], "2") {
		t.Fatalf("search = %+v", res)
	}
	names := []string{}
	for _, c := range res.Columns {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "_id,_index,_score,msg,level" {
		t.Fatalf("columns = %v", names)
	}
	if cat := s.Run(ctx, "GET /_cat/indices", RunOptions{}); cat.Err != nil || len(cat.Rows) != 2 {
		t.Fatalf("cat = %+v", cat)
	}
	in, err := s.Introspect(ctx, IntrospectionRequest{Database: "indices"})
	if err != nil || len(in.Objects) != 2 || in.Objects[0].Name != ".kibana" {
		t.Fatalf("indices = %+v %v", in.Objects, err)
	}
	in, err = s.Introspect(ctx, IntrospectionRequest{Database: "indices", Object: "logs"})
	if err != nil || len(in.Columns) != 3 || in.Columns[2].Name != "user.name" || in.Columns[2].Type != "keyword" {
		t.Fatalf("mapping = %+v %v", in.Columns, err)
	}
	ro, _ := s.ReadOnlySession()
	if r := ro.Run(ctx, "DELETE /logs", RunOptions{}); r.Err == nil {
		t.Fatal("the read-only twin must refuse DELETE")
	}
	for _, m := range methods {
		if strings.HasPrefix(m, "DELETE") {
			t.Fatalf("DELETE reached the server: %v", methods)
		}
	}
	bad, _ := NewElasticsearchSession(Conn{Driver: "elasticsearch", URL: srv.URL, User: "elastic"}, "secret-pw")
	if err := bad.Ping(ctx); err == nil || strings.Contains(err.Error(), "secret-pw") || !strings.Contains(err.Error(), "missing authentication") {
		t.Fatalf("auth error = %v", err)
	}
}

func TestClassifyESRequest(t *testing.T) {
	for in, want := range map[string]Kind{
		"GET /_cat/indices":                        KindRead,
		"POST /logs/_search\n{}":                   KindRead,
		"POST /logs/_count":                        KindRead,
		"POST /_sql?format=txt\n{\"query\":\"x\"}": KindRead,
		"PUT /logs/_doc/1\n{\"a\":1}":              KindWrite,
		"POST /logs/_doc\n{\"a\":1}":               KindWrite,
		"POST /logs/_delete_by_query\n{}":          KindDDL,
		"DELETE /logs":                             KindDDL,
		"PATCH /x":                                 KindUnknown,
		"GET /x\n{not json":                        KindUnknown,
	} {
		if got := ClassifyESRequest(in); got != want {
			t.Errorf("%q = %v, want %v", in, got, want)
		}
	}
}

func TestInfluxDBSessionInfluxQLFluxAndTree(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		q := r.Form.Get("q")
		got = append(got, r.Method+" "+r.URL.Path+" "+q)
		switch {
		case r.URL.Path == "/ping":
			w.WriteHeader(204)
		case r.URL.Path == "/api/v2/query":
			if r.Header.Get("Authorization") != "Token tok-1" || r.URL.Query().Get("org") != "acme" {
				t.Errorf("flux auth %q org %q", r.Header.Get("Authorization"), r.URL.Query().Get("org"))
			}
			_, _ = io.WriteString(w, ",result,table,_time,_value,host\r\n,_result,0,2024-01-01T00:00:00Z,1.5,a\r\n,_result,1,2024-01-01T00:00:00Z,2.5,b\r\n\r\n")
		case q == "SHOW DATABASES":
			_, _ = io.WriteString(w, `{"results":[{"series":[{"name":"databases","columns":["name"],"values":[["telegraf"],["_internal"]]}]}]}`)
		case q == "SHOW MEASUREMENTS":
			_, _ = io.WriteString(w, `{"results":[{"series":[{"name":"measurements","columns":["name"],"values":[["cpu"]]}]}]}`)
		case strings.HasPrefix(q, "SHOW TAG KEYS"):
			_, _ = io.WriteString(w, `{"results":[{"series":[{"name":"cpu","columns":["tagKey"],"values":[["host"]]}]}]}`)
		case strings.HasPrefix(q, "SHOW FIELD KEYS"):
			_, _ = io.WriteString(w, `{"results":[{"series":[{"name":"cpu","columns":["fieldKey","fieldType"],"values":[["usage","float"]]}]}]}`)
		case strings.HasPrefix(q, "SELECT"):
			_, _ = io.WriteString(w, `{"results":[{"series":[{"name":"cpu","tags":{"host":"a"},"columns":["time","usage"],"values":[[1700000000000,0.5]]}]}]}`)
		case strings.HasPrefix(q, "DROP"):
			if r.Method != "POST" {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"error":"DROP not allowed with GET"}`)
				return
			}
			_, _ = io.WriteString(w, `{"results":[{}]}`)
		}
	}))
	defer srv.Close()
	s, err := NewInfluxDBSession(Conn{Driver: "influxdb", URL: srv.URL, Database: "telegraf", Params: map[string]string{"org": "acme"}}, "tok-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	res := s.Run(ctx, "SELECT usage FROM cpu GROUP BY host", RunOptions{})
	if res.Err != nil || len(res.Rows) != 1 || res.Columns[0].Name != "name" || res.Rows[0][1].Text != "a" {
		t.Fatalf("select = %+v", res)
	}
	if !strings.HasPrefix(got[len(got)-1], "GET /query") {
		t.Fatalf("a read must use GET: %s", got[len(got)-1])
	}
	if d := s.Run(ctx, "DROP MEASUREMENT cpu", RunOptions{}); d.Err != nil || !strings.HasPrefix(got[len(got)-1], "POST") {
		t.Fatalf("drop = %+v via %s", d, got[len(got)-1])
	}
	flux := s.Run(ctx, `from(bucket: "b") |> range(start: -1h)`, RunOptions{})
	if flux.Err != nil || len(flux.Rows) != 2 || flux.Columns[2].Name != "_time" {
		t.Fatalf("flux = %+v", flux)
	}
	in, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(in.Databases, ",") != "telegraf,_internal" {
		t.Fatalf("dbs = %v %v", in.Databases, err)
	}
	in, err = s.Introspect(ctx, IntrospectionRequest{Database: "telegraf", Object: "cpu"})
	if err != nil || len(in.Columns) != 3 || in.Columns[1].Type != "tag" || in.Columns[2].Type != "float" {
		t.Fatalf("columns = %+v %v", in.Columns, err)
	}
	ro, _ := s.ReadOnlySession()
	if r := ro.Run(ctx, `from(bucket:"b") |> to(bucket:"c")`, RunOptions{}); r.Err == nil {
		t.Fatal("the read-only twin must refuse a Flux write")
	}
}

func TestClassifyInflux(t *testing.T) {
	for in, want := range map[string]Kind{
		"SELECT * FROM cpu":                    KindRead,
		"SELECT * INTO b FROM cpu":             KindWrite,
		"SHOW MEASUREMENTS":                    KindRead,
		"DROP MEASUREMENT cpu":                 KindDDL,
		"CREATE DATABASE x":                    KindWrite,
		`from(bucket:"b") |> range(start:-1h)`: KindRead,
		`from(bucket:"b") |> to(bucket:"c")`:   KindWrite,
	} {
		if got := ClassifyInflux(in); got != want {
			t.Errorf("%q = %v, want %v", in, got, want)
		}
	}
}
