package httpclient

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The runner (API-9) sends every request of one or more `.http` files
// in order, optionally several times with rows from a CSV / JSON data
// file, and builds a report for the screen, JSON and JUnit. A request
// passes when it was sent, had no unresolved placeholder and answered
// with a status below 400, or, when it has `# @assert` lines, when
// every check passed (API-8). Reports keep the template URL (placeholders
// as written) and hide every private value.

// RunFile is one parsed file to run.
type RunFile struct {
	Path string
	File File
}

// RunOptions change a run.
type RunOptions struct {
	// Iterations is how many times the whole set runs; 0 = once, or
	// once per data row when Data is set. Rows repeat when there are
	// more iterations than rows.
	Iterations int
	// Data rows: each iteration uses one row; its values win over the
	// file variables.
	Data []map[string]string
	// Delay is the pause between two requests.
	Delay time.Duration
	// StopOnFail stops the run at the first failed request.
	StopOnFail bool
	// NewResolver makes the resolver for a file; nil = file variables
	// only.
	NewResolver func(RunFile) *Resolver
	// Hooks are passed to every send.
	Hooks Hooks
}

// RunResult is one sent (or refused) request.
type RunResult struct {
	File      string        `json:"file"`
	Name      string        `json:"name"`
	Method    string        `json:"method"`
	URL       string        `json:"url"`
	Line      int           `json:"line,omitempty"`
	Iteration int           `json:"iteration"`
	Status    int           `json:"status,omitempty"`
	Elapsed   time.Duration `json:"elapsed_ns"`
	Passed    bool          `json:"passed"`
	Failure   string        `json:"failure,omitempty"`
	// Checks are the request's @assert / @pre / @post results (API-8).
	Checks []AssertResult `json:"checks,omitempty"`
}

// RunReport is the result of a run.
type RunReport struct {
	Results []RunResult   `json:"results"`
	Passed  int           `json:"passed"`
	Failed  int           `json:"failed"`
	Stopped bool          `json:"stopped,omitempty"`
	Elapsed time.Duration `json:"elapsed_ns"`
}

// Run sends the requests of files in order. It stops early when ctx is
// cancelled or, with StopOnFail, at the first failure.
func Run(ctx context.Context, c *Client, files []RunFile, opts RunOptions) RunReport {
	var rep RunReport
	start := time.Now()
	iters := opts.Iterations
	if iters <= 0 {
		iters = 1
		if len(opts.Data) > 0 {
			iters = len(opts.Data)
		}
	}
	sent := 0
	sess := NewSession() // what @pre/@post set lives for this run
	for it := 0; it < iters; it++ {
		var row map[string]string
		if len(opts.Data) > 0 {
			row = opts.Data[it%len(opts.Data)]
		}
		for _, rf := range files {
			rv := &Resolver{FileVars: rf.File.Vars}
			if opts.NewResolver != nil {
				rv = opts.NewResolver(rf)
			}
			rv.FileVars = withRow(rv.FileVars, row)
			if rv.Session == nil {
				rv.Session = sess
			}
			for _, req := range rf.File.Requests {
				if sent > 0 && opts.Delay > 0 && !sleepCtx(ctx, opts.Delay) {
					rep.Stopped = true
					rep.Elapsed = time.Since(start)
					return rep
				}
				if ctx.Err() != nil {
					rep.Stopped = true
					rep.Elapsed = time.Since(start)
					return rep
				}
				res := runOne(ctx, c, rv, rf.Path, req, opts.Hooks)
				res.Iteration = it + 1
				sent++
				rep.Results = append(rep.Results, res)
				if res.Passed {
					rep.Passed++
				} else {
					rep.Failed++
					if opts.StopOnFail {
						rep.Stopped = true
						rep.Elapsed = time.Since(start)
						return rep
					}
				}
			}
		}
	}
	rep.Elapsed = time.Since(start)
	return rep
}

func withRow(vars []Var, row map[string]string) []Var {
	if len(row) == 0 {
		return vars
	}
	names := make([]string, 0, len(row))
	for k := range row {
		names = append(names, k)
	}
	sort.Strings(names)
	// Lookup takes the last definition, so the row goes after the file
	// variables to win over them.
	out := make([]Var, 0, len(row)+len(vars))
	out = append(out, vars...)
	for _, k := range names {
		out = append(out, Var{Name: k, Value: row[k]})
	}
	return out
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func runOne(ctx context.Context, c *Client, rv *Resolver, path string, req Request, h Hooks) RunResult {
	res := RunResult{File: path, Name: req.Name, Method: req.Method, URL: RedactURL(req.URL), Line: req.Line}
	if res.Name == "" {
		res.Name = req.Method + " " + res.URL
	}
	rr, un := rv.Resolve(req)
	if len(un) > 0 {
		names := make([]string, 0, len(un))
		for _, u := range un {
			names = append(names, u.Name)
		}
		res.Failure = "unresolved: " + strings.Join(names, ", ")
		return res
	}
	resp := c.Send(ctx, rr, h)
	res.Status = resp.Status
	res.Elapsed = resp.Elapsed
	res.Checks = RunTests(rr, resp, rv.Session)
	for i := range res.Checks {
		res.Checks[i].Message = hideSecrets(res.Checks[i].Message, rr.Secrets)
	}
	failed := ""
	for _, ch := range res.Checks {
		if !ch.Pass {
			failed = ch.Expr + ": " + ch.Message
			break
		}
	}
	switch {
	case resp.Err != "":
		res.Failure = hideSecrets(resp.Err, rr.Secrets)
	case failed != "":
		res.Failure = "check failed: " + failed
	case len(rr.Asserts) == 0 && resp.Status >= 400:
		// With asserts, they decide (a test may expect a 404).
		res.Failure = "status " + resp.StatusLine()
	default:
		res.Passed = true
	}
	return res
}

func hideSecrets(s string, secrets []string) string {
	for _, v := range secrets {
		if v != "" {
			s = strings.ReplaceAll(s, v, Redacted)
		}
	}
	return RedactURL(s)
}

// Text is the report for the screen.
func (r RunReport) Text() string {
	var b strings.Builder
	for _, x := range r.Results {
		mark := "ok  "
		if !x.Passed {
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "%s %s  %s", mark, x.File, x.Name)
		if x.Iteration > 1 {
			fmt.Fprintf(&b, "  #%d", x.Iteration)
		}
		if x.Status > 0 {
			fmt.Fprintf(&b, "  %d", x.Status)
		}
		fmt.Fprintf(&b, "  %s", FormatDuration(x.Elapsed))
		if x.Failure != "" {
			fmt.Fprintf(&b, "  — %s", x.Failure)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "%d passed, %d failed in %s", r.Passed, r.Failed, FormatDuration(r.Elapsed))
	if r.Stopped {
		b.WriteString(" (stopped)")
	}
	b.WriteByte('\n')
	return b.String()
}

// JSON is the report as indented JSON.
func (r RunReport) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

type junitOutSuites struct {
	XMLName  xml.Name        `xml:"testsuites"`
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Time     string          `xml:"time,attr"`
	Suites   []junitOutSuite `xml:"testsuite"`
}

type junitOutSuite struct {
	Name     string         `xml:"name,attr"`
	Tests    int            `xml:"tests,attr"`
	Failures int            `xml:"failures,attr"`
	Time     string         `xml:"time,attr"`
	Cases    []junitOutCase `xml:"testcase"`
}

type junitOutCase struct {
	Name      string           `xml:"name,attr"`
	Classname string           `xml:"classname,attr"`
	Time      string           `xml:"time,attr"`
	Failure   *junitOutFailure `xml:"failure,omitempty"`
}

type junitOutFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func seconds(d time.Duration) string { return fmt.Sprintf("%.3f", d.Seconds()) }

// JUnit is the report as JUnit XML: one suite per file, one case per
// sent request.
func (r RunReport) JUnit() ([]byte, error) {
	out := junitOutSuites{Name: "termocode http run", Tests: len(r.Results), Failures: r.Failed, Time: seconds(r.Elapsed)}
	index := map[string]int{}
	var times []time.Duration
	for _, x := range r.Results {
		i, ok := index[x.File]
		if !ok {
			i = len(out.Suites)
			index[x.File] = i
			out.Suites = append(out.Suites, junitOutSuite{Name: x.File})
			times = append(times, 0)
		}
		s := &out.Suites[i]
		name := x.Name
		if x.Iteration > 1 {
			name = fmt.Sprintf("%s #%d", name, x.Iteration)
		}
		jc := junitOutCase{Name: name, Classname: x.File, Time: seconds(x.Elapsed)}
		if !x.Passed {
			jc.Failure = &junitOutFailure{Message: x.Failure, Text: x.Method + " " + x.URL}
			s.Failures++
		}
		s.Tests++
		times[i] += x.Elapsed
		s.Cases = append(s.Cases, jc)
	}
	for i := range out.Suites {
		out.Suites[i].Time = seconds(times[i])
	}
	b, err := xml.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(b, '\n')...), nil
}

// LoadRunData reads data rows from a .csv file (first line = names) or
// a .json file (an array of objects; scalars become text).
func LoadRunData(path string) ([]map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		recs, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
		if err != nil {
			return nil, err
		}
		if len(recs) == 0 {
			return nil, nil
		}
		head := recs[0]
		rows := make([]map[string]string, 0, len(recs)-1)
		for _, rec := range recs[1:] {
			row := map[string]string{}
			for i, name := range head {
				if i < len(rec) {
					row[strings.TrimSpace(name)] = rec[i]
				}
			}
			rows = append(rows, row)
		}
		return rows, nil
	case ".json":
		var raw []map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("%s: want an array of objects: %w", filepath.Base(path), err)
		}
		rows := make([]map[string]string, 0, len(raw))
		for _, obj := range raw {
			row := map[string]string{}
			for k, v := range obj {
				row[k] = jsonScalar(v)
			}
			rows = append(rows, row)
		}
		return rows, nil
	}
	return nil, fmt.Errorf("%s: data file must be .csv or .json", filepath.Base(path))
}

// FindHTTPFiles expands paths: a file is kept, a folder gives every
// .http / .rest file below it (sorted; hidden folders and
// node_modules skipped).
func FindHTTPFiles(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		var found []string
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != p && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
					return filepath.SkipDir
				}
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".http", ".rest":
				found = append(found, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		sort.Strings(found)
		out = append(out, found...)
	}
	if len(out) == 0 {
		return nil, errors.New("no .http or .rest files found")
	}
	return out, nil
}
