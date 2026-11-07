package dbclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PrometheusSession is a PromQL console on the Prometheus HTTP API
// (DBX-70), port 9090. Every request is a GET on /api/v1/query,
// /query_range or a metadata endpoint, so the session can only read.
//
//	<expr>                     instant query: one row per sample
//	range 1h [30s] <expr>      the last hour (step 30s, or 1/60 of the
//	                           window): a terminal chart above a table of
//	                           each series' last, min and max
//
// The tree lists the metric names; a metric's columns are its labels.
type PrometheusSession struct {
	*httpEngine
	now func() time.Time // tests pin the clock
}

func init() {
	registerHTTPEngine(HTTPEngine{
		ID: "prometheus", Title: "Prometheus", Port: 9090,
		Hint:     "a PromQL console with a terminal chart, default port 9090",
		Schemes:  []string{"prometheus", "prometheuss"},
		Classify: func(string) Kind { return KindRead },
		Open: func(c Conn, password string) (Session, error) {
			return NewPrometheusSession(c, password)
		},
	})
}

// NewPrometheusSession makes a session of c; it does not connect.
func NewPrometheusSession(c Conn, password string) (*PrometheusSession, error) {
	e, err := newHTTPEngine("Prometheus", c, password)
	if err != nil {
		return nil, err
	}
	return &PrometheusSession{httpEngine: e, now: time.Now}, nil
}

// PromChartWidth and PromChartHeight size the console's chart.
const (
	PromChartWidth  = 64
	PromChartHeight = 12
)

// promDuration reads a PromQL duration: 30s, 5m, 2h, 1d, 1w (or Go's).
func promDuration(s string) (time.Duration, bool) {
	if n, ok := strings.CutSuffix(s, "d"); ok {
		if v, err := strconv.ParseFloat(n, 64); err == nil && v > 0 {
			return time.Duration(v * float64(24*time.Hour)), true
		}
		return 0, false
	}
	if n, ok := strings.CutSuffix(s, "w"); ok {
		if v, err := strconv.ParseFloat(n, 64); err == nil && v > 0 {
			return time.Duration(v * float64(7*24*time.Hour)), true
		}
		return 0, false
	}
	d, err := time.ParseDuration(s)
	return d, err == nil && d > 0
}

// ParsePromRange reads "range WINDOW [STEP] EXPR"; ok is false for a plain
// instant query.
func ParsePromRange(q string) (window, step time.Duration, expr string, ok bool, err error) {
	rest, isRange := strings.CutPrefix(q, "range ")
	if !isRange {
		return 0, 0, q, false, nil
	}
	f := strings.Fields(rest)
	if len(f) < 2 {
		return 0, 0, "", true, errors.New("prometheus: range WINDOW [STEP] EXPR, e.g. range 1h 30s rate(x[5m])")
	}
	w, good := promDuration(f[0])
	if !good {
		return 0, 0, "", true, fmt.Errorf("prometheus: %q is not a window like 1h", f[0])
	}
	rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), f[0]))
	st := w / 60
	if d, good := promDuration(f[1]); good && len(f) > 2 {
		st = d
		rest = strings.TrimSpace(strings.TrimPrefix(rest, f[1]))
	}
	if st < time.Second {
		st = time.Second
	}
	if w/st > 11000 {
		return 0, 0, "", true, errors.New("prometheus: too many points; use a larger step")
	}
	return w, st, rest, true, nil
}

func (s *PrometheusSession) Ping(ctx context.Context) error {
	ctx, done, err := s.run(ctx)
	if err != nil {
		return err
	}
	defer done()
	_, err = promGet(ctx, s.authClient(), s.base, "/api/v1/status/buildinfo", url.Values{})
	return s.clean(err)
}

func (s *PrometheusSession) Run(parent context.Context, text string, opts RunOptions) Result {
	start := time.Now()
	q := stripLineComments(strings.TrimSuffix(strings.TrimSpace(text), ";"))
	if q == "" {
		return Result{Err: errors.New("prometheus: write a PromQL expression")}
	}
	ctx, done, err := s.run(parent)
	if err != nil {
		return Result{Err: err}
	}
	defer done()
	window, step, expr, isRange, err := ParsePromRange(q)
	if err != nil {
		return Result{Err: err}
	}
	if !isRange {
		res, err := PromInstant(ctx, s.authClient(), s.base, expr, time.Time{})
		if err != nil {
			return Result{Err: s.clean(err), Elapsed: time.Since(start)}
		}
		if opts.MaxRows > 0 && len(res.Rows) > opts.MaxRows {
			res.Rows, res.Truncated = res.Rows[:opts.MaxRows], true
		}
		res.Elapsed = time.Since(start)
		return *res
	}
	to := s.now()
	series, err := PromRange(ctx, s.authClient(), s.base, expr, to.Add(-window), to, step)
	if err != nil {
		return Result{Err: s.clean(err), Elapsed: time.Since(start)}
	}
	res := PromSummary(series)
	res.Notices = PromChart(series, PromChartWidth, PromChartHeight)
	res.Elapsed = time.Since(start)
	return res
}

// PromSummary is one row per series: its labels, points, last, min, max.
func PromSummary(series []PromSeries) Result {
	res := Result{Columns: []Column{{Name: "series", Type: "label"}, {Name: "points", Type: "int"}, {Name: "last", Type: "float"}, {Name: "min", Type: "float"}, {Name: "max", Type: "float"}}}
	f := func(v float64) Cell { return Cell{Text: strconv.FormatFloat(v, 'g', -1, 64)} }
	for _, s := range series {
		lo, hi, last, n := math.Inf(1), math.Inf(-1), math.NaN(), 0
		for _, p := range s.Points {
			if math.IsNaN(p.V) || math.IsInf(p.V, 0) {
				continue
			}
			lo, hi, last = math.Min(lo, p.V), math.Max(hi, p.V), p.V
			n++
		}
		row := []Cell{{Text: s.Label()}, {Text: strconv.Itoa(n)}}
		if n == 0 {
			row = append(row, Cell{Null: true}, Cell{Null: true}, Cell{Null: true})
		} else {
			row = append(row, f(last), f(lo), f(hi))
		}
		res.Rows = append(res.Rows, row)
	}
	return res
}

// Introspect: the connection lists "metrics", which lists the metric
// names; a metric's columns are its label names.
func (s *PrometheusSession) Introspect(parent context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	if req.Database == "" && req.Schema == "" && req.Object == "" {
		out.Databases = []string{"metrics"}
		return out, nil
	}
	ctx, done, err := s.run(parent)
	if err != nil {
		return out, err
	}
	defer done()
	if req.Object == "" {
		names, err := s.strings(ctx, "/api/v1/label/__name__/values", url.Values{})
		for _, n := range names {
			out.Objects = append(out.Objects, SchemaObject{Name: n, Kind: "metric"})
		}
		return out, err
	}
	labels, err := s.strings(ctx, "/api/v1/labels", url.Values{"match[]": {req.Object}})
	for _, l := range labels {
		if l != "__name__" {
			out.Columns = append(out.Columns, Column{Name: l, Type: "label", Table: req.Object})
		}
	}
	out.Columns = append(out.Columns, Column{Name: "value", Type: "float", Table: req.Object})
	return out, err
}

func (s *PrometheusSession) strings(ctx context.Context, path string, q url.Values) ([]string, error) {
	var out struct {
		Status string   `json:"status"`
		Error  string   `json:"error"`
		Data   []string `json:"data"`
	}
	data, err := s.do(ctx, "GET", path+"?"+q.Encode(), nil, "")
	if jerr := json.Unmarshal(data, &out); jerr != nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("prometheus: not a JSON API reply")
	}
	if out.Status != "success" {
		return nil, fmt.Errorf("prometheus: %s", s.scrub(out.Error))
	}
	sort.Strings(out.Data)
	return out.Data, nil
}

func (s *PrometheusSession) Begin(context.Context) error    { return errNoTx("Prometheus") }
func (s *PrometheusSession) Commit(context.Context) error   { return errNoTx("Prometheus") }
func (s *PrometheusSession) Rollback(context.Context) error { return errNoTx("Prometheus") }

// ReadOnlySession is the same session: it can only read.
func (s *PrometheusSession) ReadOnlySession() (Session, error) {
	return &PrometheusSession{httpEngine: s.clone(), now: s.now}, nil
}
