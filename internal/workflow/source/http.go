package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// Http source limits.
const (
	DefaultHTTPTimeout = 30 * time.Second
	MaxHTTPTimeout     = 5 * time.Minute
	maxRequestFile     = 1 << 20
)

// HTTPInput is a `"from": "http"` source. The request comes from a
// `.http` file in the workspace (RequestFile, optional RequestName), or
// inline (Method, URL, Body). The answer must be JSON.
type HTTPInput struct {
	// RequestFile is a `.http` / `.rest` file relative to the workspace.
	// Its {{name}} file variables are resolved; the process env and the
	// credential store are not reachable from it.
	RequestFile string
	// RequestName picks a `# @name` request in the file ("": the first).
	RequestName string
	// Method and URL are an inline request (Method "" is GET), used when
	// RequestFile is "".
	Method string
	URL    string
	Body   *string
	// Headers are added to the request (and replace a header of the
	// same name). Values may hold ${secret:NAME}; nothing else may.
	Headers map[string]string
	// Timeout ends the request (0: DefaultHTTPTimeout).
	Timeout time.Duration
	Shape
}

// ErrStatus is returned for an answer that is not 2xx.
var ErrStatus = errors.New("http status")

var (
	secretRefRe = regexp.MustCompile(`\$\{secret:([^}]*)\}`)
	anyRefRe    = regexp.MustCompile(`\$\{[^}]*\}?`)
)

// redactedText replaces a secret value in errors and items.
const redactedText = "[secret]"

// HTTP sends the request and returns the items of its JSON answer.
func (s *Sources) HTTP(ctx context.Context, in HTTPInput) (Result, error) {
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = DefaultHTTPTimeout
	}
	if timeout > MaxHTTPTimeout {
		return Result{}, fmt.Errorf("http: timeout %s is over the limit of %s", timeout, MaxHTTPTimeout)
	}
	col, err := newCollector(in.Shape)
	if err != nil {
		return Result{}, fmt.Errorf("http: %w", err)
	}
	req, vars, err := s.httpRequest(in)
	if err != nil {
		return Result{}, fmt.Errorf("http: %w", err)
	}
	rv := httpclient.Resolver{
		FileVars:  vars,
		LookupEnv: func(string) (string, bool) { return "", false },
	}
	rr, un := rv.Resolve(req)
	if len(un) > 0 {
		names := make([]string, 0, len(un))
		for _, u := range un {
			names = append(names, u.Name)
		}
		return Result{}, fmt.Errorf("http: unknown {{…}} in the request: %s", strings.Join(names, ", "))
	}
	// Headers from the step, with their secrets, go in only now: they
	// are never seen by the placeholder resolver.
	vals, err := s.resolveHeaders(in.Headers)
	if err != nil {
		return Result{}, fmt.Errorf("http: %w", err)
	}
	scrub := scrubber(vals.secrets)
	rr.Headers = mergeHeaders(rr.Headers, vals.headers)

	client := s.Client
	if client == nil {
		client = httpclient.New(httpclient.Options{Timeout: timeout})
		defer client.Close()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp := client.Send(ctx, rr, httpclient.Hooks{AllowIP: s.AllowIP})
	where := rr.Method + " " + httpclient.RedactURL(rr.URL)
	if rr.Method == "" {
		where = "GET " + httpclient.RedactURL(rr.URL)
	}
	switch {
	case resp.Err != "":
		return Result{}, errors.New(scrub("http: " + where + ": " + resp.Err))
	case resp.Status < 200 || resp.Status > 299:
		return Result{}, fmt.Errorf("http: %s: %w %d %s", scrub(where), ErrStatus, resp.Status, scrub(resp.StatusText))
	case resp.Truncated:
		return Result{}, errors.New(scrub("http: " + where + ": the answer is over the size limit"))
	}
	body := []byte(scrub(string(resp.Body)))
	if !json.Valid(body) {
		return Result{}, errors.New(scrub("http: " + where + ": the answer is not JSON"))
	}
	if err := col.addDoc(body); err != nil {
		return Result{}, errors.New(scrub("http: " + where + ": " + err.Error()))
	}
	return col.res, nil
}

// httpRequest builds the request from the file or the inline fields.
// No part of it may hold a ${…} reference.
func (s *Sources) httpRequest(in HTTPInput) (httpclient.Request, []httpclient.Var, error) {
	var req httpclient.Request
	var vars []httpclient.Var
	if in.RequestFile != "" {
		if in.URL != "" || in.Method != "" || in.Body != nil {
			return req, nil, errors.New("use request or method/url, not both")
		}
		p, err := s.inside(in.RequestFile)
		if err != nil {
			return req, nil, fmt.Errorf("request: %w", err)
		}
		fi, err := os.Stat(p)
		if err != nil {
			return req, nil, err
		}
		if !fi.Mode().IsRegular() || fi.Size() > maxRequestFile {
			return req, nil, fmt.Errorf("request: %q is not a file of at most 1 MB", in.RequestFile)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return req, nil, err
		}
		f := httpclient.Parse(string(b))
		var r *httpclient.Request
		if in.RequestName != "" {
			r = f.Find(in.RequestName)
		} else if len(f.Requests) > 0 {
			r = &f.Requests[0]
		}
		if r == nil {
			return req, nil, fmt.Errorf("request: no request %q in %s", in.RequestName, in.RequestFile)
		}
		req, vars = *r, f.Vars
	} else {
		if in.URL == "" {
			return req, nil, errors.New("no request file and no url")
		}
		method := in.Method
		if method == "" {
			method = "GET"
		}
		req = httpclient.Request{Method: strings.ToUpper(method), URL: in.URL, Body: in.Body}
	}
	check := []string{req.URL, req.BodyText()}
	for _, h := range req.Headers {
		check = append(check, h.Name, h.Value)
	}
	for _, v := range vars {
		check = append(check, v.Value)
	}
	for _, c := range check {
		if strings.Contains(c, "${") {
			return req, nil, errors.New("${…} references are allowed only in the step's headers")
		}
	}
	return req, vars, nil
}

type resolvedHeaders struct {
	headers []httpclient.Header
	secrets []string
}

// resolveHeaders resolves ${secret:NAME} in header values. Any other
// ${…} (env, dotenv, cmd) is refused. Errors name the secret, never a
// value.
func (s *Sources) resolveHeaders(hs map[string]string) (resolvedHeaders, error) {
	var out resolvedHeaders
	names := make([]string, 0, len(hs))
	for n := range hs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		v := hs[name]
		if name == "" || strings.ContainsAny(name, " \t\r\n:${}") {
			return out, fmt.Errorf("header name %q is not valid", name)
		}
		if secrets.HasCmdRef(v) {
			return out, fmt.Errorf("header %s: ${cmd:} is not allowed", name)
		}
		var ferr error
		val := secretRefRe.ReplaceAllStringFunc(v, func(m string) string {
			if ferr != nil {
				return ""
			}
			sn := secretRefRe.FindStringSubmatch(m)[1]
			if err := secrets.ValidName(sn); err != nil {
				ferr = fmt.Errorf("header %s: ${secret:} needs a secret name", name)
				return ""
			}
			if s.Secrets == nil {
				ferr = fmt.Errorf("header %s: no secret store for ${secret:%s}", name, sn)
				return ""
			}
			sv, ok, err := s.Secrets.Get(sn)
			switch {
			case err != nil:
				ferr = fmt.Errorf("header %s: secret %s could not be read", name, sn)
			case !ok || sv == "":
				ferr = fmt.Errorf("header %s: secret %s is not set", name, sn)
			default:
				out.secrets = append(out.secrets, sv)
			}
			return sv
		})
		if ferr != nil {
			return resolvedHeaders{}, ferr
		}
		// Left-over ${…} that was not a secret ref (env, dotenv, broken).
		if anyRefRe.MatchString(secretRefRe.ReplaceAllString(v, "")) {
			return resolvedHeaders{}, fmt.Errorf("header %s: only ${secret:NAME} is allowed", name)
		}
		if strings.ContainsAny(val, "\r\n") {
			return resolvedHeaders{}, fmt.Errorf("header %s: the value holds a new line", name)
		}
		out.headers = append(out.headers, httpclient.Header{Name: name, Value: val})
	}
	return out, nil
}

// mergeHeaders replaces headers of base with the same (case-insensitive)
// name as one in add, and appends the rest.
func mergeHeaders(base, add []httpclient.Header) []httpclient.Header {
	out := make([]httpclient.Header, 0, len(base)+len(add))
	for _, h := range base {
		dup := false
		for _, a := range add {
			dup = dup || strings.EqualFold(a.Name, h.Name)
		}
		if !dup {
			out = append(out, h)
		}
	}
	return append(out, add...)
}

// scrubber returns a func that replaces every secret value (raw, and as
// it looks inside a JSON string) with redactedText.
func scrubber(vals []string) func(string) string {
	var olds []string
	for _, v := range vals {
		if v == "" {
			continue
		}
		olds = append(olds, v)
		if b, err := json.Marshal(v); err == nil {
			if e := string(b[1 : len(b)-1]); e != v {
				olds = append(olds, e)
			}
		}
	}
	// Longest first, so a secret that holds another is replaced whole.
	sort.Slice(olds, func(i, j int) bool { return len(olds[i]) > len(olds[j]) })
	return func(s string) string {
		for _, o := range olds {
			s = strings.ReplaceAll(s, o, redactedText)
		}
		return s
	}
}
