package dbclient

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPromDurationsAndRangeSyntax(t *testing.T) {
	for in, want := range map[string]time.Duration{"30s": 30 * time.Second, "2h": 2 * time.Hour, "1d": 24 * time.Hour, "0.5d": 12 * time.Hour, "1w": 7 * 24 * time.Hour} {
		if got, ok := promDuration(in); !ok || got != want {
			t.Errorf("promDuration(%s) = %v %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "x", "-1d", "0w", "xw", "xd", "0s", "-5m"} {
		if _, ok := promDuration(bad); ok {
			t.Errorf("promDuration(%q) should fail", bad)
		}
	}
	w, st, expr, isRange, err := ParsePromRange("range 1h 30s rate(x[5m])")
	if err != nil || !isRange || w != time.Hour || st != 30*time.Second || expr != "rate(x[5m])" {
		t.Errorf("range = %v %v %q %v %v", w, st, expr, isRange, err)
	}
	if w, st, expr, _, _ := ParsePromRange("range 1h up"); st != time.Minute || expr != "up" || w != time.Hour {
		t.Errorf("default step = %v %v %q", w, st, expr)
	}
	if _, st, _, _, _ := ParsePromRange("range 10s up"); st != time.Second {
		t.Errorf("a step is at least a second, got %v", st)
	}
	if _, _, expr, isRange, err := ParsePromRange("up"); isRange || err != nil || expr != "up" {
		t.Errorf("instant = %q %v %v", expr, isRange, err)
	}
	for q, why := range map[string]string{"range 1h": "range WINDOW", "range soon up": "not a window", "range 1000d 1s up": "too many points"} {
		if _, _, _, _, err := ParsePromRange(q); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%q = %v, want %q", q, err, why)
		}
	}
}

func TestPromDecodeAndSamples(t *testing.T) {
	dec := func(typ, result string) ([]PromSeries, error) {
		pr := &promResponse{}
		pr.Data.ResultType = typ
		pr.Data.Result = json.RawMessage(result)
		return promDecode(pr)
	}
	if s, err := dec("scalar", `[1700000000, "2.5"]`); err != nil || len(s) != 1 || s[0].Points[0].V != 2.5 {
		t.Errorf("scalar = %+v %v", s, err)
	}
	if _, err := dec("string", `"no"`); err == nil {
		t.Error("a string that is no array: want error")
	}
	if _, err := dec("histogram", `[]`); err == nil || !strings.Contains(err.Error(), "unknown result type") {
		t.Errorf("unknown type = %v", err)
	}
	if _, err := dec("vector", `{}`); err == nil {
		t.Error("bad vector: want error")
	}
	s, err := dec("matrix", `[{"metric":{"a":"1"},"values":[[1,"1"],[2,"bad"],[3]]},{"values":[[1,"2"]]},{"metric":null,"value":[1,"7"]}]`)
	if err != nil || len(s) != 3 || len(s[0].Points) != 1 || s[1].Metric == nil || s[2].Points[0].V != 7 {
		t.Errorf("matrix = %+v %v", s, err)
	}
	for _, v := range [][]any{{1.0}, {"t", "1"}, {1.0, 2.0}, {1.0, "x"}} {
		if _, ok := promSample(v); ok {
			t.Errorf("promSample(%v) should fail", v)
		}
	}
}

func TestPromChartEdges(t *testing.T) {
	if got := PromChart(nil, 10, 4); len(got) != 1 || got[0] != "(no data)" {
		t.Errorf("empty = %v", got)
	}
	nan := []PromSeries{{Points: []PromPoint{{T: 1, V: math.NaN()}, {T: 2, V: math.Inf(1)}}}}
	if got := PromChart(nan, 10, 4); got[0] != "(no data)" {
		t.Errorf("only NaN = %v", got)
	}
	flat := []PromSeries{{Metric: map[string]string{"a": "1"}, Points: []PromPoint{{T: 1, V: 5}, {T: 1, V: 5}}}, {Points: []PromPoint{{T: 1, V: math.NaN()}}}}
	if got := PromChart(flat, 0, 0); len(got) < 4 {
		t.Errorf("tiny chart = %v", got)
	}
	many := make([]PromSeries, 8)
	for i := range many {
		many[i] = PromSeries{Points: []PromPoint{{T: float64(i), V: float64(i)}, {T: float64(i + 1), V: float64(i * 2)}}}
	}
	if got := PromChart(many, 30, 6); len(got) != 8 {
		t.Errorf("chart rows = %d", len(got))
	}
}

func promServer(t *testing.T, h http.HandlerFunc) *PrometheusSession {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	s, err := NewPrometheusSession(Conn{URL: srv.URL}, "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPrometheusSessionRunsRangesAndFailures(t *testing.T) {
	s := promServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		switch {
		case r.URL.Path == "/api/v1/query" && r.URL.Query().Get("query") == "boom":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"status":"error","error":"parse error"}`))
		case r.URL.Path == "/api/v1/query" && r.URL.Query().Get("query") == "html":
			_, _ = w.Write([]byte(`<html>`))
		case r.URL.Path == "/api/v1/query":
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"job":"a"},"value":[1,"1"]},{"metric":{"job":"b"},"value":[1,"2"]},{"metric":{},"value":[1,"3"]}]}}`))
		case r.URL.Path == "/api/v1/query_range":
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"job":"a"},"values":[[1,"1"],[2,"NaN"],[3,"3"]]},{"metric":{"job":"b"},"values":[]}]}}`))
		case r.URL.Path == "/api/v1/label/__name__/values":
			_, _ = w.Write([]byte(`{"status":"success","data":["up","down"]}`))
		default:
			w.WriteHeader(404)
		}
	})
	ctx := context.Background()
	if r := s.Run(ctx, "up", RunOptions{MaxRows: 2}); r.Err != nil || len(r.Rows) != 2 || !r.Truncated {
		t.Errorf("instant = %+v", r)
	}
	rr := s.Run(ctx, "range 1h 1m up", RunOptions{})
	if rr.Err != nil || len(rr.Rows) != 2 || rr.Rows[1][1].Text != "0" || !rr.Rows[1][2].Null {
		t.Errorf("range = %+v", rr)
	}
	for q, why := range map[string]string{"": "write a PromQL", "boom": "parse error", "html": "not a JSON", "range x up": "not a window"} {
		if r := s.Run(ctx, q, RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), why) {
			t.Errorf("%q = %v, want %q", q, r.Err, why)
		}
	}
	if _, err := s.Introspect(ctx, IntrospectionRequest{}); err != nil {
		t.Error(err)
	}
	m, err := s.Introspect(ctx, IntrospectionRequest{Database: "metrics"})
	if err != nil || len(m.Objects) != 2 {
		t.Errorf("metrics = %+v %v", m, err)
	}
	for _, f := range []func(context.Context) error{s.Begin, s.Commit, s.Rollback} {
		if f(ctx) == nil {
			t.Error("Prometheus has no transactions")
		}
	}
	if ro, _ := s.ReadOnlySession(); ro == nil {
		t.Error("read-only session")
	}
	// A closed session and an API that errors on the metric list.
	bad := promServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	if _, err := bad.Introspect(ctx, IntrospectionRequest{Database: "metrics"}); err == nil {
		t.Error("a failing metric list: want error")
	}
	_ = s.Close()
	if r := s.Run(ctx, "up", RunOptions{}); r.Err == nil {
		t.Error("a closed session: want error")
	}
	if _, err := s.Introspect(ctx, IntrospectionRequest{Database: "metrics"}); err == nil {
		t.Error("a closed session: want error")
	}
	if _, err := PromRange(ctx, nil, "http://x", "up", time.Now(), time.Now(), 0); err == nil {
		t.Error("a zero step: want error")
	}
	if _, err := PromRange(ctx, nil, "http://127.0.0.1:1", "up", time.Now(), time.Now(), time.Second); err == nil {
		t.Error("an unreachable server: want error")
	}
	if _, err := PromInstant(ctx, nil, "http://127.0.0.1:1", "up", time.Now()); err == nil {
		t.Error("an unreachable server: want error")
	}
}
