package httpclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// maxFileBody caps one `< ./file` body or multipart part.
const maxFileBody = 64 << 20

// WireBody is what a send puts on the wire for one request.
type WireBody struct {
	Method string
	Body   []byte
	// HasBody is false when the request has no body at all.
	HasBody bool
	// ContentType is set when the body kind needs one and the request
	// has none ("" = leave the headers as they are).
	ContentType string
}

// BuildBody turns the resolved body text into wire bytes:
//
//   - a body that is one `< ./path` line sends that file's bytes,
//   - multipart/form-data lines are joined with CRLF and a `< ./path`
//     line inside a part is replaced by the file,
//   - a multi-line application/x-www-form-urlencoded body (`key = value`
//     lines, an optional leading `&`) is encoded and joined with `&`,
//   - method GRAPHQL or header `X-Request-Type: GraphQL` sends POST JSON
//     {"query", "variables"}; the variables are the JSON object after
//     the last blank line.
//
// File paths are read relative to dir (the .http file's folder) and
// must stay inside it; dir "" (an unsaved request) cannot read files.
func BuildBody(r Request, dir string) (WireBody, error) {
	wb := WireBody{Method: r.Method}
	isGraphQL := strings.EqualFold(r.Method, "GRAPHQL") || strings.EqualFold(r.Header("X-Request-Type"), "GraphQL")
	if isGraphQL {
		wb.Method = "POST"
		if strings.EqualFold(r.Method, "GET") {
			wb.Method = "GET" // a GET GraphQL request keeps its query in the URL
		}
	}
	if r.Body == nil {
		return wb, nil
	}
	wb.HasBody = true
	text := *r.Body
	ct := strings.ToLower(r.Header("Content-Type"))
	switch {
	case isGraphQL && wb.Method == "POST":
		b, err := graphQLBody(text)
		if err != nil {
			return wb, err
		}
		wb.Body = b
		if !r.HasHeader("Content-Type") {
			wb.ContentType = "application/json"
		}
	case strings.HasPrefix(ct, "multipart/"):
		b, err := multipartBody(text, dir)
		if err != nil {
			return wb, err
		}
		wb.Body = b
	case fileRef(text) != "":
		b, err := readBodyFile(dir, fileRef(text))
		if err != nil {
			return wb, err
		}
		wb.Body = b
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded") && strings.Contains(strings.TrimSpace(text), "\n"):
		wb.Body = []byte(formBody(text))
	default:
		wb.Body = []byte(text)
	}
	return wb, nil
}

// fileRef returns the path of a `< path` line, or "".
func fileRef(line string) string {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "<") || strings.Contains(t, "\n") {
		return ""
	}
	return strings.TrimSpace(t[1:])
}

// readBodyFile reads rel inside dir; paths that leave dir (also through
// a symlink) are refused.
func readBodyFile(dir, rel string) ([]byte, error) {
	if dir == "" {
		return nil, errors.New("file body: save the .http file first (< paths are read next to it)")
	}
	if filepath.IsAbs(rel) {
		return nil, fmt.Errorf("file body: %s: use a path relative to the .http file", rel)
	}
	base, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("file body: %w", err)
	}
	p, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("file body: %s: not found", rel)
	}
	if in, err := filepath.Rel(base, p); err != nil || in == ".." || strings.HasPrefix(in, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("file body: %s: outside the .http file's folder", rel)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("file body: %s: %w", rel, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFileBody+1))
	if err != nil {
		return nil, fmt.Errorf("file body: %s: %w", rel, err)
	}
	if len(b) > maxFileBody {
		return nil, fmt.Errorf("file body: %s: larger than %d MB", rel, maxFileBody>>20)
	}
	return b, nil
}

func multipartBody(text, dir string) ([]byte, error) {
	var out []byte
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if p := fileRef(line); p != "" {
			b, err := readBodyFile(dir, p)
			if err != nil {
				return nil, err
			}
			out = append(out, b...)
		} else {
			out = append(out, line...)
		}
		out = append(out, '\r', '\n')
	}
	return out, nil
}

func formBody(text string) string {
	var parts []string
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		t = strings.TrimSpace(strings.TrimPrefix(t, "&"))
		if t == "" {
			continue
		}
		k, v, _ := strings.Cut(t, "=")
		parts = append(parts, url.QueryEscape(strings.TrimSpace(k))+"="+url.QueryEscape(strings.TrimSpace(v)))
	}
	return strings.Join(parts, "&")
}

func graphQLBody(text string) ([]byte, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	query, vars := strings.TrimSpace(text), ""
	if i := strings.LastIndex(text, "\n\n"); i >= 0 {
		if tail := strings.TrimSpace(text[i:]); strings.HasPrefix(tail, "{") && strings.TrimSpace(text[:i]) != "" {
			query, vars = strings.TrimSpace(text[:i]), tail
		}
	}
	payload := struct {
		Query     string          `json:"query"`
		Variables json.RawMessage `json:"variables,omitempty"`
	}{Query: query}
	if vars != "" {
		var obj map[string]any
		if err := json.Unmarshal([]byte(vars), &obj); err != nil {
			return nil, fmt.Errorf("graphql variables: not a JSON object: %v", err)
		}
		payload.Variables = json.RawMessage(vars)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return b, nil
}
