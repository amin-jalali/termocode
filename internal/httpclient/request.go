// Package httpclient is termocode's HTTP client for `.http` / `.rest`
// files (VS Code REST Client / JetBrains HTTP Client style): the
// parser and serializer (byte-equal with mobocode's
// lib/core/rest/http_file_parser.dart), variables and environment
// files, the net/http engine, redaction, history and curl import /
// export. It has no UI imports and never runs a child process.
package httpclient

import "strings"

// Header is one request or response header line, kept in order.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Var is one `@name = value` file variable.
type Var struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Line  int    `json:"line"` // 1-based
}

// Request is one request block of a `.http` file. Values keep their
// `{{name}}` placeholders; Resolve substitutes them.
type Request struct {
	Method  string   `json:"method"`
	URL     string   `json:"url"`
	Headers []Header `json:"headers,omitempty"`
	// Body is nil when the block has no body (Dart: null).
	Body *string `json:"body,omitempty"`
	// Name comes from `# @name x` or the `### x` separator; "" = none.
	Name string `json:"name,omitempty"`
	// Line is the 1-based line of the request line (METHOD URL).
	Line int `json:"line,omitempty"`
	// StartLine / EndLine are the 1-based first and last lines of the
	// block (StartLine is the `###` separator when there is one).
	StartLine int `json:"start_line,omitempty"`
	EndLine   int `json:"end_line,omitempty"`
	// Directives holds `# @word value` comments above the request line
	// (no-redirect, no-cookie-jar, timeout). mobocode reads them as
	// plain comments.
	Directives map[string]string `json:"directives,omitempty"`
}

// File is a parsed `.http` file.
type File struct {
	Requests []Request `json:"requests"`
	Vars     []Var     `json:"vars,omitempty"`
}

// Header returns the value of the last header called name (case
// insensitive), or "".
func (r *Request) Header(name string) string {
	v := ""
	for _, h := range r.Headers {
		if strings.EqualFold(h.Name, name) {
			v = h.Value
		}
	}
	return v
}

// HasHeader reports whether a header called name is set.
func (r *Request) HasHeader(name string) bool {
	for _, h := range r.Headers {
		if strings.EqualFold(h.Name, name) {
			return true
		}
	}
	return false
}

// BodyText returns the body or "".
func (r *Request) BodyText() string {
	if r.Body == nil {
		return ""
	}
	return *r.Body
}

// Label is the request name, or "METHOD /path" when it has none (the
// same rule as mobocode's _deriveName).
func (r *Request) Label() string {
	if r.Name != "" {
		return r.Name
	}
	return DeriveName(r.Method, r.URL)
}

// DeriveName is mobocode's _deriveName: "METHOD /path".
func DeriveName(method, url string) string {
	path := url
	if i := strings.Index(path, "://"); i >= 0 {
		path = path[i+3:]
		if j := strings.IndexByte(path, '/'); j >= 0 {
			path = path[j:]
		} else {
			path = "/"
		}
	} else if strings.HasPrefix(path, "{{") {
		if j := strings.Index(path, "}}"); j >= 0 {
			path = path[j+2:]
		}
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		path = "/"
	}
	return method + " " + path
}

// AtLine returns the request whose block holds the 1-based line. A
// line in a block without a request (comments, the area after a body)
// belongs to the request above it; a line above the first request
// belongs to the first one. nil when the file has no request.
func (f *File) AtLine(line int) *Request {
	if len(f.Requests) == 0 {
		return nil
	}
	best := 0
	for i := range f.Requests {
		if f.Requests[i].StartLine <= line {
			best = i
		}
	}
	return &f.Requests[best]
}

// Find returns the request called name (first match), or nil.
func (f *File) Find(name string) *Request {
	for i := range f.Requests {
		if f.Requests[i].Name == name {
			return &f.Requests[i]
		}
	}
	return nil
}

// Names lists the request labels in file order.
func (f *File) Names() []string {
	out := make([]string, 0, len(f.Requests))
	for i := range f.Requests {
		out = append(out, f.Requests[i].Label())
	}
	return out
}
