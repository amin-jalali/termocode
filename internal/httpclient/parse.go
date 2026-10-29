package httpclient

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// The grammar is mobocode's parseHttpFile
// (lib/core/rest/http_file_parser.dart). termocode accepts a harmless
// superset; the serializer only writes the common subset:
//
//   - `//` comments and `// @name x` (JetBrains),
//   - `@name = value` file variables above a request line,
//   - a bare `https://…` or `{{host}}/…` request line means GET,
//   - `# @no-redirect`, `# @no-cookie-jar`, `# @timeout 10s` directives,
//   - blank lines between the comment lines above a request.

var varLineRe = lazyre.New(`^@([\w.-]+)\s*=\s*(.*?)\s*$`)

// Parse reads a `.http` file. It never fails: blocks it cannot read are
// skipped, like the Dart parser.
func Parse(src string) File {
	var f File
	lines := strings.Split(src, "\n")
	start, first := 1, 1 // separator line, first content line
	var block []string
	name := ""
	flush := func(end int) {
		if r, ok := parseBlock(block, first, name, &f.Vars); ok {
			r.StartLine = start
			r.EndLine = end
			f.Requests = append(f.Requests, r)
		}
	}
	for i, raw := range lines {
		n := i + 1
		line := strings.TrimSuffix(raw, "\r")
		if strings.HasPrefix(line, "###") {
			flush(n - 1)
			block = block[:0]
			start, first = n, n+1
			name = strings.TrimSpace(line[3:])
			continue
		}
		block = append(block, line)
	}
	flush(len(lines))
	return f
}

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

func isComment(s string) bool {
	return strings.HasPrefix(s, "#") || strings.HasPrefix(s, "//")
}

func commentText(s string) string {
	if strings.HasPrefix(s, "//") {
		return strings.TrimSpace(s[2:])
	}
	return strings.TrimSpace(s[1:])
}

// LooksLikeURL reports whether s starts like a URL (a scheme, or a
// `{{host}}` placeholder).
func LooksLikeURL(s string) bool {
	return looksLikeScheme(s) || strings.HasPrefix(s, "{{")
}

func parseBlock(lines []string, first int, name string, vars *[]Var) (Request, bool) {
	var r Request
	i := 0
	for i < len(lines) {
		l := lines[i]
		switch {
		case isBlank(l):
		case isComment(strings.TrimSpace(l)):
			c := commentText(strings.TrimSpace(l))
			if strings.HasPrefix(c, "@name") {
				name = strings.TrimSpace(c[5:])
			} else if strings.HasPrefix(c, "@") && len(c) > 1 {
				word, val, _ := strings.Cut(c[1:], " ")
				if r.Directives == nil {
					r.Directives = map[string]string{}
				}
				r.Directives[word] = strings.TrimSpace(val)
			}
		default:
			t := strings.TrimSpace(l)
			if !strings.HasPrefix(t, "@") {
				goto request
			}
			m := varLineRe.FindStringSubmatch(t)
			if m == nil {
				goto request
			}
			*vars = append(*vars, Var{Name: m[1], Value: m[2], Line: first + i})
		}
		i++
	}
	return r, false

request:
	reqLine := strings.TrimSpace(lines[i])
	r.Line = first + i
	i++
	tok, rest, hasSpace := reqLine, "", false
	if k := strings.IndexAny(reqLine, " \t"); k >= 0 {
		tok, rest, hasSpace = reqLine[:k], reqLine[k+1:], true
	}
	var url string
	switch {
	case LooksLikeURL(tok):
		r.Method, url = "GET", reqLine
	case !hasSpace:
		return r, false
	default:
		r.Method, url = strings.ToUpper(tok), strings.TrimSpace(rest)
	}
	url = strings.TrimSpace(cutHTTPVersion(url))
	if url == "" {
		return r, false
	}
	r.URL = url
	r.Name = name

	for i < len(lines) && !isBlank(lines[i]) {
		h := lines[i]
		i++
		if isComment(h) {
			continue
		}
		k, v, ok := strings.Cut(h, ":")
		if !ok {
			continue
		}
		if k = strings.TrimSpace(k); k != "" {
			r.Headers = append(r.Headers, Header{Name: k, Value: strings.TrimSpace(v)})
		}
	}
	if i < len(lines) && isBlank(lines[i]) {
		i++
	}
	end := len(lines)
	for end > i && isBlank(lines[end-1]) {
		end--
	}
	if end > i {
		b := strings.Join(lines[i:end], "\n")
		r.Body = &b
	}
	return r, true
}

// looksLikeScheme reports whether s starts with a URL scheme and "://"
// (^[A-Za-z][A-Za-z0-9+.-]*://, without a regexp: it runs on every
// request line of every parse).
func looksLikeScheme(s string) bool {
	if s == "" || !isLetter(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case isLetter(c), c >= '0' && c <= '9', c == '+', c == '.', c == '-':
		case c == ':':
			return strings.HasPrefix(s[i:], "://")
		default:
			return false
		}
	}
	return false
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// isRESpace is regexp's \s: [\t\n\f\r ].
func isRESpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' }

// cutHTTPVersion drops a trailing " HTTP/1.1" (the regexp
// \s+HTTP/\S+$ replaced by "").
func cutHTTPVersion(s string) string {
	j := len(s)
	for j > 0 && !isRESpace(s[j-1]) {
		j--
	}
	tok := s[j:]
	if j == 0 || len(tok) <= len("HTTP/") || !strings.HasPrefix(tok, "HTTP/") {
		return s
	}
	for j > 0 && isRESpace(s[j-1]) {
		j--
	}
	return s[:j]
}
