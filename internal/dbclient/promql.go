package dbclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PromPoint is one sample of a Prometheus series (T in unix seconds).
type PromPoint struct {
	T float64
	V float64
}

// PromSeries is one labelled series returned by a PromQL query.
type PromSeries struct {
	Metric map[string]string
	Points []PromPoint
}

// Label renders the series labels as {a="x",b="y"} (sorted keys).
func (s PromSeries) Label() string {
	keys := make([]string, 0, len(s.Metric))
	for k := range s.Metric {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, s.Metric[k]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

const promBodyLimit = 32 << 20

type promResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	} `json:"data"`
}

type promRaw struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"`
	Values [][]any           `json:"values"`
}

// promGet runs one read-only GET against the Prometheus HTTP API.
func promGet(ctx context.Context, hc *http.Client, base, path string, q url.Values) (*promResponse, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("prometheus: base URL must be http(s)://host[:port]")
	}
	u.Path += path
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New(Redact(err.Error()))
	}
	req.Header.Set("Accept", "application/json")
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, errors.New(Redact(err.Error()))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, promBodyLimit))
	if err != nil {
		return nil, errors.New(Redact(err.Error()))
	}
	var pr promResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("prometheus: HTTP %d: not a JSON API reply", resp.StatusCode)
	}
	if pr.Status != "success" {
		return nil, fmt.Errorf("prometheus: %s: %s", pr.ErrorType, Redact(pr.Error))
	}
	return &pr, nil
}

func promSample(v []any) (PromPoint, bool) {
	if len(v) != 2 {
		return PromPoint{}, false
	}
	t, ok := v[0].(float64)
	if !ok {
		return PromPoint{}, false
	}
	s, ok := v[1].(string)
	if !ok {
		return PromPoint{}, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return PromPoint{}, false
	}
	return PromPoint{T: t, V: f}, true
}

func promDecode(pr *promResponse) ([]PromSeries, error) {
	switch pr.Data.ResultType {
	case "vector", "matrix":
	case "scalar", "string":
		var v []any
		if err := json.Unmarshal(pr.Data.Result, &v); err != nil {
			return nil, errors.New("prometheus: bad scalar result")
		}
		p, _ := promSample(v)
		return []PromSeries{{Metric: map[string]string{}, Points: []PromPoint{p}}}, nil
	default:
		return nil, fmt.Errorf("prometheus: unknown result type %q", pr.Data.ResultType)
	}
	var raw []promRaw
	if err := json.Unmarshal(pr.Data.Result, &raw); err != nil {
		return nil, errors.New("prometheus: bad result")
	}
	out := make([]PromSeries, 0, len(raw))
	for _, r := range raw {
		s := PromSeries{Metric: r.Metric}
		if s.Metric == nil {
			s.Metric = map[string]string{}
		}
		if r.Value != nil {
			if p, ok := promSample(r.Value); ok {
				s.Points = append(s.Points, p)
			}
		}
		for _, v := range r.Values {
			if p, ok := promSample(v); ok {
				s.Points = append(s.Points, p)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

func promTime(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixNano())/1e9, 'f', -1, 64)
}

// PromInstant runs an instant PromQL query and returns it as a result
// grid: one column per label (sorted), then time and value.
func PromInstant(ctx context.Context, hc *http.Client, base, query string, at time.Time) (*Result, error) {
	start := time.Now()
	q := url.Values{"query": {query}}
	if !at.IsZero() {
		q.Set("time", promTime(at))
	}
	pr, err := promGet(ctx, hc, base, "/api/v1/query", q)
	if err != nil {
		return nil, err
	}
	series, err := promDecode(pr)
	if err != nil {
		return nil, err
	}
	labels := map[string]bool{}
	for _, s := range series {
		for k := range s.Metric {
			labels[k] = true
		}
	}
	names := make([]string, 0, len(labels))
	for k := range labels {
		names = append(names, k)
	}
	sort.Strings(names)
	res := &Result{}
	for _, n := range names {
		res.Columns = append(res.Columns, Column{Name: n, Type: "label"})
	}
	res.Columns = append(res.Columns, Column{Name: "time", Type: "time"}, Column{Name: "value", Type: "float"})
	for _, s := range series {
		for _, p := range s.Points {
			row := make([]Cell, 0, len(res.Columns))
			for _, n := range names {
				if v, ok := s.Metric[n]; ok {
					row = append(row, Cell{Text: v})
				} else {
					row = append(row, Cell{Null: true})
				}
			}
			ts := time.Unix(0, int64(p.T*1e9)).UTC()
			row = append(row, Cell{Text: ts.Format(time.RFC3339Nano), Kind: "time"},
				Cell{Text: strconv.FormatFloat(p.V, 'g', -1, 64)})
			res.Rows = append(res.Rows, row)
		}
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

// PromRange runs a range PromQL query and returns its series.
func PromRange(ctx context.Context, hc *http.Client, base, query string, from, to time.Time, step time.Duration) ([]PromSeries, error) {
	if step <= 0 {
		return nil, errors.New("prometheus: step must be > 0")
	}
	q := url.Values{
		"query": {query},
		"start": {promTime(from)},
		"end":   {promTime(to)},
		"step":  {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)},
	}
	pr, err := promGet(ctx, hc, base, "/api/v1/query_range", q)
	if err != nil {
		return nil, err
	}
	return promDecode(pr)
}

var promGlyphs = []string{"•", "+", "x", "o", "*", "#"}

// PromChart draws series as a terminal line chart: height plot rows
// with a y-axis label, one x-axis row and one legend row. width is the
// plot width in cells. NaN and Inf samples are skipped.
func PromChart(series []PromSeries, width, height int) []string {
	if width < 2 {
		width = 2
	}
	if height < 2 {
		height = 2
	}
	minV, maxV := math.Inf(1), math.Inf(-1)
	minT, maxT := math.Inf(1), math.Inf(-1)
	for _, s := range series {
		for _, p := range s.Points {
			if math.IsNaN(p.V) || math.IsInf(p.V, 0) {
				continue
			}
			minV, maxV = math.Min(minV, p.V), math.Max(maxV, p.V)
			minT, maxT = math.Min(minT, p.T), math.Max(maxT, p.T)
		}
	}
	if math.IsInf(minV, 1) {
		return []string{"(no data)"}
	}
	grid := make([][]string, height)
	for i := range grid {
		grid[i] = make([]string, width)
		for j := range grid[i] {
			grid[i][j] = " "
		}
	}
	for si, s := range series {
		g := promGlyphs[si%len(promGlyphs)]
		for _, p := range s.Points {
			if math.IsNaN(p.V) || math.IsInf(p.V, 0) {
				continue
			}
			x := 0
			if maxT > minT {
				x = int(math.Round((p.T - minT) / (maxT - minT) * float64(width-1)))
			}
			y := height - 1
			if maxV > minV {
				y = height - 1 - int(math.Round((p.V-minV)/(maxV-minV)*float64(height-1)))
			}
			grid[y][x] = g
		}
	}
	top := strconv.FormatFloat(maxV, 'g', 4, 64)
	bot := strconv.FormatFloat(minV, 'g', 4, 64)
	lw := max(len(top), len(bot))
	out := make([]string, 0, height+2)
	for i, row := range grid {
		lab := ""
		switch i {
		case 0:
			lab = top
		case height - 1:
			lab = bot
		}
		out = append(out, fmt.Sprintf("%*s │%s", lw, lab, strings.Join(row, "")))
	}
	out = append(out, strings.Repeat(" ", lw)+" └"+strings.Repeat("─", width))
	leg := make([]string, 0, len(series))
	for si, s := range series {
		leg = append(leg, promGlyphs[si%len(promGlyphs)]+" "+s.Label())
	}
	out = append(out, strings.Join(leg, "  "))
	return out
}
