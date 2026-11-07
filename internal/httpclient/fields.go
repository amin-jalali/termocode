package httpclient

import (
	"errors"
	"strings"
)

// Params and headers as a table (API-3): Fields lists a request's query
// params and headers; EditField changes one row and returns the new
// text of the request line and the header lines, so the table and the
// .http text stay in sync (the table is always read back from the
// text). Values stay raw: `{{name}}` placeholders and %-escapes are
// kept as written.

// FieldKind says whether a Field is a query param or a header.
type FieldKind int

const (
	FieldParam FieldKind = iota
	FieldHeader
)

// Field is one row of the table.
type Field struct {
	Kind  FieldKind
	Name  string
	Value string
	// HasValue is false for a bare `?flag` param (no "=").
	HasValue bool
}

// FieldEdit changes the row Index of Kind (-1 adds a row at the end).
// Delete removes the row; Name/Value are then ignored.
type FieldEdit struct {
	Kind     FieldKind
	Index    int
	Name     string
	Value    string
	HasValue bool
	Delete   bool
}

// splitURL cuts a URL into the part before "?", the query and the
// fragment (with its "#").
func splitURL(u string) (base, query, frag string, hasQuery bool) {
	if i := strings.IndexByte(u, '#'); i >= 0 {
		u, frag = u[:i], u[i:]
	}
	base, query, hasQuery = strings.Cut(u, "?")
	return base, query, frag, hasQuery
}

func queryParams(query string) []Field {
	var out []Field
	for _, p := range strings.Split(query, "&") {
		if p == "" {
			continue
		}
		k, v, has := strings.Cut(p, "=")
		out = append(out, Field{Kind: FieldParam, Name: k, Value: v, HasValue: has})
	}
	return out
}

// Fields lists the query params of the URL (in order), then the headers.
func (r *Request) Fields() []Field {
	_, q, _, _ := splitURL(r.URL)
	out := queryParams(q)
	for _, h := range r.Headers {
		out = append(out, Field{Kind: FieldHeader, Name: h.Name, Value: h.Value, HasValue: true})
	}
	return out
}

func checkField(e FieldEdit) error {
	if e.Delete {
		return nil
	}
	if strings.ContainsAny(e.Name+e.Value, "\r\n") {
		return errors.New("a line break is not allowed")
	}
	name := strings.TrimSpace(e.Name)
	if name == "" {
		return errors.New("the name is empty")
	}
	if e.Kind == FieldParam {
		if strings.ContainsAny(e.Name+e.Value, " \t&#") {
			return errors.New("a query param cannot hold a space, & or # (write them as %20, %26, %23)")
		}
		if strings.Contains(e.Name, "=") {
			return errors.New("a param name cannot hold =")
		}
		return nil
	}
	if strings.ContainsAny(name, ": \t") || strings.HasPrefix(name, "#") || strings.HasPrefix(name, "//") {
		return errors.New("a header name cannot hold a space or :, or start with # or //")
	}
	return nil
}

// EditField applies e to request r of the file lines (the lines r was
// parsed from). It returns the 0-based range [start, end) of lines to
// replace and the new lines: the request line and the header lines.
// The body, comments and other requests are never touched.
func EditField(lines []string, r *Request, e FieldEdit) (start, end int, repl []string, err error) {
	if err := checkField(e); err != nil {
		return 0, 0, nil, err
	}
	start = r.Line - 1
	if start < 0 || start >= len(lines) {
		return 0, 0, nil, errors.New("the request is not in the text")
	}
	end = start + 1
	for end < len(lines) && !isBlank(lines[end]) {
		end++
	}
	repl = append([]string{}, lines[start:end]...)
	name, value := strings.TrimSpace(e.Name), strings.TrimSpace(e.Value)
	if e.Kind == FieldParam {
		base, q, frag, _ := splitURL(r.URL)
		ps := queryParams(q)
		switch {
		case e.Index == -1 && !e.Delete:
			ps = append(ps, Field{})
			e.Index = len(ps) - 1
		case e.Index < 0 || e.Index >= len(ps):
			return 0, 0, nil, errors.New("no such query param")
		}
		if e.Delete {
			ps = append(ps[:e.Index], ps[e.Index+1:]...)
		} else {
			ps[e.Index] = Field{Kind: FieldParam, Name: name, Value: value, HasValue: e.HasValue || value != ""}
		}
		parts := make([]string, 0, len(ps))
		for _, p := range ps {
			s := p.Name
			if p.HasValue {
				s += "=" + p.Value
			}
			parts = append(parts, s)
		}
		u := base
		if len(parts) > 0 {
			u += "?" + strings.Join(parts, "&")
		}
		u += frag
		line := strings.TrimRight(repl[0], "\r")
		i := strings.LastIndex(line, r.URL)
		if i < 0 {
			return 0, 0, nil, errors.New("the request line does not hold the URL")
		}
		repl[0] = line[:i] + u + line[i+len(r.URL):]
		return start, end, repl, nil
	}
	// Header rows map to the non-comment "name: value" lines, as Parse
	// reads them.
	var at []int
	for i := 1; i < len(repl); i++ {
		h := repl[i]
		if isComment(strings.TrimSpace(h)) {
			continue
		}
		if k, _, ok := strings.Cut(h, ":"); ok && strings.TrimSpace(k) != "" {
			at = append(at, i)
		}
	}
	text := name + ": " + value
	switch {
	case e.Index == -1 && !e.Delete:
		repl = append(repl, text)
	case e.Index < 0 || e.Index >= len(at):
		return 0, 0, nil, errors.New("no such header")
	case e.Delete:
		repl = append(repl[:at[e.Index]], repl[at[e.Index]+1:]...)
	default:
		repl[at[e.Index]] = text
	}
	return start, end, repl, nil
}
