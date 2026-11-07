package httpclient

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// An assert is one `# @assert <subject> <op> [value]` line above a
// request. Subjects:
//
//	status                 the status code
//	time                   the elapsed time (number = ms, or 300ms / 1s)
//	header <name>          a response header (any case)
//	body                   the body text
//	body $.a.b[0].c        a JSON value picked by a small JSONPath
//
// Ops: == != < <= > >= contains matches exists !exists. Nothing here
// reads files or the network; it only looks at the Response.

// Assert is one parsed assert line.
type Assert struct {
	Subject string // status, time, header, body
	Arg     string // header name or JSONPath ("" = none)
	Op      string
	Value   string
	path    []pathStep
	re      *regexp.Regexp
	schema  map[string]any
}

// AssertResult is the outcome of one assert line.
type AssertResult struct {
	Expr    string `json:"expr"`
	Pass    bool   `json:"pass"`
	Message string `json:"message,omitempty"`
}

// maxAssertPattern caps a `matches` pattern (Go regexps are linear
// time, but a huge pattern still costs memory to compile).
const maxAssertPattern = 1000

var assertOps = map[string]bool{
	"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true,
	"contains": true, "matches": true, "exists": true, "!exists": true,
	"schema": true,
}

// ParseAssert reads one assert expression (the text after `@assert`).
func ParseAssert(s string) (Assert, error) {
	var a Assert
	f := strings.Fields(s)
	if len(f) == 0 {
		return a, fmt.Errorf("empty assert")
	}
	a.Subject = f[0]
	rest := f[1:]
	switch a.Subject {
	case "status", "time":
	case "header":
		if len(rest) == 0 {
			return a, fmt.Errorf("header needs a name")
		}
		a.Arg, rest = rest[0], rest[1:]
	case "body":
		if len(rest) > 0 && !assertOps[rest[0]] {
			if !strings.HasPrefix(rest[0], "$") {
				return a, fmt.Errorf("body path must start with $: %q", rest[0])
			}
			p, err := parsePath(rest[0])
			if err != nil {
				return a, err
			}
			a.Arg, a.path, rest = rest[0], p, rest[1:]
		}
	default:
		return a, fmt.Errorf("unknown subject %q (use status, time, header or body)", a.Subject)
	}
	if len(rest) == 0 {
		return a, fmt.Errorf("missing operator")
	}
	a.Op = rest[0]
	if !assertOps[a.Op] {
		return a, fmt.Errorf("unknown operator %q", a.Op)
	}
	a.Value = strings.Join(rest[1:], " ")
	switch a.Op {
	case "exists", "!exists":
		if a.Value != "" {
			return a, fmt.Errorf("%s takes no value", a.Op)
		}
		return a, nil
	}
	if a.Value == "" {
		return a, fmt.Errorf("%s needs a value", a.Op)
	}
	switch a.Subject {
	case "status":
		if _, err := strconv.Atoi(a.Value); err != nil && isOrderOp(a.Op, true) {
			return a, fmt.Errorf("status needs a number, not %q", a.Value)
		}
	case "time":
		if _, err := parseAssertDuration(a.Value); err != nil {
			return a, err
		}
	}
	if a.Op == "schema" {
		if a.Subject != "body" {
			return a, fmt.Errorf("schema checks the body (body [$.path] schema {…})")
		}
		sc, err := parseSchema(a.Value)
		if err != nil {
			return a, err
		}
		a.schema = sc
		if a.path == nil {
			a.path = []pathStep{}
		}
		return a, nil
	}
	if a.Op == "matches" {
		if len(a.Value) > maxAssertPattern {
			return a, fmt.Errorf("pattern longer than %d characters", maxAssertPattern)
		}
		re, err := regexp.Compile(a.Value)
		if err != nil {
			return a, fmt.Errorf("bad pattern: %v", err)
		}
		a.re = re
	}
	return a, nil
}

// isOrderOp reports a comparison op; eq also counts == and !=.
func isOrderOp(op string, eq bool) bool {
	switch op {
	case "<", "<=", ">", ">=":
		return true
	case "==", "!=":
		return eq
	}
	return false
}

func parseAssertDuration(s string) (time.Duration, error) {
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(n * float64(time.Millisecond)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("time needs ms or a duration like 300ms, not %q", s)
	}
	return d, nil
}

// RunAsserts checks each expression against resp, in order. A line
// that does not parse fails with the reason.
func RunAsserts(exprs []string, resp *Response) []AssertResult {
	if len(exprs) == 0 {
		return nil
	}
	out := make([]AssertResult, 0, len(exprs))
	var doc any
	docState := 0 // 0 = not read, 1 = JSON, 2 = not JSON
	for _, e := range exprs {
		r := AssertResult{Expr: e}
		a, err := ParseAssert(e)
		switch {
		case err != nil:
			r.Message = err.Error()
		case resp == nil || resp.Err != "":
			r.Message = "no response (the send failed)"
		default:
			if a.path != nil && docState == 0 {
				docState = 2
				if json.Unmarshal(resp.Body, &doc) == nil {
					docState = 1
				}
			}
			r.Pass, r.Message = a.check(resp, doc, docState == 1)
		}
		if r.Pass {
			r.Message = ""
		}
		out = append(out, r)
	}
	return out
}

// AssertSummary is "2 of 3 checks passed", or "" with no results.
func AssertSummary(res []AssertResult) string {
	if len(res) == 0 {
		return ""
	}
	n := 0
	for _, r := range res {
		if r.Pass {
			n++
		}
	}
	return fmt.Sprintf("%d of %d checks passed", n, len(res))
}

func (a Assert) check(resp *Response, doc any, isJSON bool) (bool, string) {
	switch a.Subject {
	case "status":
		return compare(strconv.Itoa(resp.Status), true, a)
	case "time":
		want, _ := parseAssertDuration(a.Value)
		got := resp.Elapsed
		ok := false
		switch a.Op {
		case "==":
			ok = got == want
		case "!=":
			ok = got != want
		case "<":
			ok = got < want
		case "<=":
			ok = got <= want
		case ">":
			ok = got > want
		case ">=":
			ok = got >= want
		default:
			return false, fmt.Sprintf("time does not support %s", a.Op)
		}
		if !ok {
			return false, fmt.Sprintf("time was %s", got.Round(time.Millisecond))
		}
		return true, ""
	case "header":
		v, found := "", false
		for _, h := range resp.Headers {
			if strings.EqualFold(h.Name, a.Arg) {
				v, found = h.Value, true
			}
		}
		return compare(v, found, a)
	}
	// body
	if a.path == nil && a.schema == nil {
		return compare(resp.Text(), true, a)
	}
	if !isJSON {
		return false, "the body is not JSON"
	}
	v, found := walkPath(doc, a.path)
	if !found {
		return compare("", false, a)
	}
	if a.schema != nil {
		if errs := ValidateSchema(a.schema, v); len(errs) > 0 {
			return false, strings.Join(errs, "; ")
		}
		return true, ""
	}
	if a.Op == "contains" {
		if arr, ok := v.([]any); ok {
			for _, x := range arr {
				if jsonText(x) == unquote(a.Value) {
					return true, ""
				}
			}
			return false, fmt.Sprintf("%s has no item %s", a.Arg, a.Value)
		}
	}
	return compare(jsonText(v), true, a)
}

// compare checks got (found = the value is there) against a.
func compare(got string, found bool, a Assert) (bool, string) {
	what := a.Subject
	if a.Arg != "" {
		what += " " + a.Arg
	}
	switch a.Op {
	case "exists":
		if found {
			return true, ""
		}
		return false, what + " is missing"
	case "!exists":
		if !found {
			return true, ""
		}
		return false, what + " is there"
	}
	if !found {
		return false, what + " is missing"
	}
	want := unquote(a.Value)
	ok := false
	switch a.Op {
	case "contains":
		ok = strings.Contains(got, want)
	case "matches":
		ok = a.re != nil && a.re.MatchString(got)
	case "==", "!=":
		eq := got == want
		if g, err1 := strconv.ParseFloat(got, 64); err1 == nil {
			if w, err2 := strconv.ParseFloat(want, 64); err2 == nil {
				eq = g == w
			}
		}
		ok = eq == (a.Op == "==")
	default:
		g, err1 := strconv.ParseFloat(got, 64)
		w, err2 := strconv.ParseFloat(want, 64)
		if err1 != nil || err2 != nil {
			return false, fmt.Sprintf("%s: %q and %q are not both numbers", what, got, want)
		}
		switch a.Op {
		case "<":
			ok = g < w
		case "<=":
			ok = g <= w
		case ">":
			ok = g > w
		case ">=":
			ok = g >= w
		}
	}
	if ok {
		return true, ""
	}
	return false, fmt.Sprintf("%s was %s", what, clip(got, 80))
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
		return s[1 : len(s)-1]
	}
	return s
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// jsonText is a JSON value as an assert compares it: strings bare,
// numbers/bools/null as JSON, objects and arrays as compact JSON.
func jsonText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return "null"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// pathStep is one `.key` or `[n]` step of a JSONPath.
type pathStep struct {
	key   string
	index int // -1 = a key step
}

// parsePath reads the small JSONPath subset: $ then .key, ["key"] or
// [n] steps.
func parsePath(p string) ([]pathStep, error) {
	if !strings.HasPrefix(p, "$") {
		return nil, fmt.Errorf("path must start with $")
	}
	steps := []pathStep{}
	s := p[1:]
	for s != "" {
		switch s[0] {
		case '.':
			s = s[1:]
			n := strings.IndexAny(s, ".[")
			if n < 0 {
				n = len(s)
			}
			if n == 0 {
				return nil, fmt.Errorf("empty key in path %q", p)
			}
			steps = append(steps, pathStep{key: s[:n], index: -1})
			s = s[n:]
		case '[':
			end := strings.IndexByte(s, ']')
			if end < 0 {
				return nil, fmt.Errorf("missing ] in path %q", p)
			}
			in := s[1:end]
			s = s[end+1:]
			if len(in) >= 2 && (in[0] == '"' || in[0] == '\'') && in[len(in)-1] == in[0] {
				steps = append(steps, pathStep{key: in[1 : len(in)-1], index: -1})
				continue
			}
			n, err := strconv.Atoi(in)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("bad index %q in path %q", in, p)
			}
			steps = append(steps, pathStep{index: n})
		default:
			return nil, fmt.Errorf("bad path %q", p)
		}
	}
	return steps, nil
}

func walkPath(v any, steps []pathStep) (any, bool) {
	for _, st := range steps {
		if st.index >= 0 {
			arr, ok := v.([]any)
			if !ok || st.index >= len(arr) {
				return nil, false
			}
			v = arr[st.index]
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		if v, ok = m[st.key]; !ok {
			return nil, false
		}
	}
	return v, true
}
