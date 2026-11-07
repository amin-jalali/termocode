package dbclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fakeProm(t *testing.T, body string, status int, seen *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET (PromQL is read-only)", r.Method)
		}
		if seen != nil {
			*seen = append(*seen, r.URL.Path+"?"+r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPromInstantVectorBecomesGrid(t *testing.T) {
	var seen []string
	srv := fakeProm(t, `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"__name__":"up","job":"api","instance":"a:9090"},"value":[1700000000,"1"]},
		{"metric":{"__name__":"up","job":"db"},"value":[1700000000,"0"]}]}}`, 200, &seen)
	res, err := PromInstant(context.Background(), srv.Client(), srv.URL, `up`, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || !strings.HasPrefix(seen[0], "/api/v1/query?") || !strings.Contains(seen[0], "query=up") {
		t.Fatalf("request = %v", seen)
	}
	var names []string
	for _, c := range res.Columns {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, ","); got != "__name__,instance,job,time,value" {
		t.Fatalf("columns = %s", got)
	}
	if len(res.Rows) != 2 || res.Rows[0][4].Text != "1" || !res.Rows[1][1].Null {
		t.Fatalf("rows = %+v", res.Rows)
	}
}

func TestPromRangeMatrixSeries(t *testing.T) {
	var seen []string
	srv := fakeProm(t, `{"status":"success","data":{"resultType":"matrix","result":[
		{"metric":{"job":"api"},"values":[[10,"1"],[20,"2.5"],[30,"NaN"]]}]}}`, 200, &seen)
	s, err := PromRange(context.Background(), srv.Client(), srv.URL, `rate(x[1m])`, time.Unix(10, 0), time.Unix(30, 0), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen[0], "/api/v1/query_range?") || !strings.Contains(seen[0], "step=10") {
		t.Fatalf("request = %v", seen)
	}
	if len(s) != 1 || s[0].Label() != `{job="api"}` || len(s[0].Points) != 3 || s[0].Points[1].V != 2.5 {
		t.Fatalf("series = %+v", s)
	}
}

func TestPromErrorIsShownRedacted(t *testing.T) {
	srv := fakeProm(t, `{"status":"error","errorType":"bad_data","error":"parse error near token=abc123"}`, 400, nil)
	_, err := PromInstant(context.Background(), srv.Client(), srv.URL, `up{`, time.Time{})
	if err == nil || !strings.Contains(err.Error(), "bad_data") || strings.Contains(err.Error(), "abc123") {
		t.Fatalf("err = %v", err)
	}
}

func TestPromRejectsNonHTTPBase(t *testing.T) {
	if _, err := PromInstant(context.Background(), http.DefaultClient, "file:///etc/passwd", "up", time.Time{}); err == nil {
		t.Fatal("want error for a non-http base URL")
	}
}

func TestPromChartDrawsLineInBox(t *testing.T) {
	s := []PromSeries{{Metric: map[string]string{"job": "api"}, Points: []PromPoint{{T: 0, V: 0}, {T: 1, V: 5}, {T: 2, V: 10}}}}
	lines := PromChart(s, 20, 5)
	if len(lines) != 5+2 {
		t.Fatalf("lines = %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[0], "10") || !strings.Contains(lines[4], "0") {
		t.Fatalf("axis labels missing:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[len(lines)-1], `{job="api"}`) {
		t.Fatalf("legend missing:\n%s", strings.Join(lines, "\n"))
	}
	// The top row holds the highest point at the right edge, the bottom row the lowest at the left.
	if !strings.HasSuffix(strings.TrimRight(lines[0], " "), "•") {
		t.Fatalf("top row should end with the max point:\n%s", strings.Join(lines, "\n"))
	}
}

func TestPromChartEmpty(t *testing.T) {
	if got := PromChart(nil, 20, 5); len(got) != 1 || got[0] != "(no data)" {
		t.Fatalf("got %q", got)
	}
}
