package httpclient

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ToCurl writes a one-line curl command. Without secrets, a header,
// URL or body that holds a private / $secret value is written as its
// template ({{token}}), so the command can be shared.
func ToCurl(rr Resolved, withSecrets bool) string {
	hasSecret := func(s string) bool {
		for _, v := range rr.Secrets {
			if v != "" && strings.Contains(s, v) {
				return true
			}
		}
		return false
	}
	u := rr.URL
	if !withSecrets && hasSecret(u) {
		u = rr.Template.URL
	}
	var b strings.Builder
	b.WriteString("curl -X " + rr.Method + " " + shellQuote(u))
	for i, h := range rr.Headers {
		v := h.Value
		if !withSecrets && (SecretHeader(h.Name) || hasSecret(v)) {
			if i < len(rr.Template.Headers) && rr.Template.Headers[i].Name == h.Name {
				v = rr.Template.Headers[i].Value
			} else {
				v = RedactHeaderValue(h.Name, v)
			}
			if hasSecret(v) {
				v = Redacted
			}
		}
		b.WriteString(" -H " + shellQuote(h.Name+": "+v))
	}
	if rr.Body != nil && *rr.Body != "" {
		body := *rr.Body
		if !withSecrets && hasSecret(body) && rr.Template.Body != nil {
			body = *rr.Template.Body
		}
		b.WriteString(" --data-raw " + shellQuote(body))
	}
	b.WriteString(" --compressed")
	return b.String()
}

// shellQuote wraps s in single quotes ('\” for a quote inside).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellWords splits a command line like a POSIX shell: single quotes,
// double quotes with backslash escapes, backslash-newline
// continuations.
func shellWords(line string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line) && (line[i+1] == '\n' || line[i+1] == '\r'):
			i++
			if line[i] == '\r' && i+1 < len(line) && line[i+1] == '\n' {
				i++
			}
		case c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
			inWord = true
		case c == '\'':
			j := strings.IndexByte(line[i+1:], '\'')
			if j < 0 {
				return nil, errors.New("unterminated ' quote")
			}
			cur.WriteString(line[i+1 : i+1+j])
			i += j + 1
			inWord = true
		case c == '"':
			i++
			closed := false
			for ; i < len(line); i++ {
				if line[i] == '\\' && i+1 < len(line) && strings.IndexByte(`"\$`+"`\n", line[i+1]) >= 0 {
					i++
					if line[i] != '\n' {
						cur.WriteByte(line[i])
					}
					continue
				}
				if line[i] == '"' {
					closed = true
					break
				}
				cur.WriteByte(line[i])
			}
			if !closed {
				return nil, errors.New(`unterminated " quote`)
			}
			inWord = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

// FromCurl reads a curl command: -X/--request, -H/--header,
// -d/--data/--data-raw/--data-binary/--data-ascii/--data-urlencode,
// --json, -u/--user (Basic auth), -A/--user-agent, -b/--cookie,
// -e/--referer, --url, -G/--get; -L, -k, -s, -v, -i, --compressed are
// accepted and ignored.
func FromCurl(line string) (Request, error) {
	words, err := shellWords(line)
	if err != nil {
		return Request{}, err
	}
	if len(words) > 0 && words[0] == "curl" {
		words = words[1:]
	}
	var r Request
	var data []string
	get, jsonFlag := false, false
	for i := 0; i < len(words); i++ {
		w := words[i]
		name, val, hasVal := w, "", false
		switch {
		case strings.HasPrefix(w, "--"):
			if k, v, ok := strings.Cut(w, "="); ok {
				name, val, hasVal = k, v, true
			}
		case strings.HasPrefix(w, "-") && len(w) > 2 && strings.ContainsRune("XHdubAe", rune(w[1])):
			name, val, hasVal = w[:2], w[2:], true
		}
		next := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(words) {
				return "", fmt.Errorf("%s needs a value", name)
			}
			i++
			return words[i], nil
		}
		switch name {
		case "-X", "--request":
			v, err := next()
			if err != nil {
				return r, err
			}
			r.Method = strings.ToUpper(v)
		case "-H", "--header":
			v, err := next()
			if err != nil {
				return r, err
			}
			k, hv, ok := strings.Cut(v, ":")
			if !ok || strings.TrimSpace(k) == "" {
				return r, fmt.Errorf("bad header %q", v)
			}
			r.Headers = append(r.Headers, Header{Name: strings.TrimSpace(k), Value: strings.TrimSpace(hv)})
		case "-d", "--data", "--data-raw", "--data-binary", "--data-ascii", "--data-urlencode", "--json":
			v, err := next()
			if err != nil {
				return r, err
			}
			if strings.HasPrefix(v, "@") && name != "--data-raw" {
				return r, errors.New("reading the body from a file (@file) is not supported")
			}
			data = append(data, v)
			if name == "--json" {
				jsonFlag = true
			}
		case "-u", "--user":
			v, err := next()
			if err != nil {
				return r, err
			}
			r.Headers = append(r.Headers, Header{Name: "Authorization", Value: "Basic " + base64.StdEncoding.EncodeToString([]byte(v))})
		case "-A", "--user-agent":
			v, err := next()
			if err != nil {
				return r, err
			}
			r.Headers = append(r.Headers, Header{Name: "User-Agent", Value: v})
		case "-b", "--cookie":
			v, err := next()
			if err != nil {
				return r, err
			}
			r.Headers = append(r.Headers, Header{Name: "Cookie", Value: v})
		case "-e", "--referer":
			v, err := next()
			if err != nil {
				return r, err
			}
			r.Headers = append(r.Headers, Header{Name: "Referer", Value: v})
		case "--url":
			v, err := next()
			if err != nil {
				return r, err
			}
			r.URL = v
		case "-G", "--get":
			get = true
		case "-L", "--location", "-k", "--insecure", "-s", "--silent", "-S", "--show-error", "-v", "--verbose",
			"-i", "--include", "--compressed", "-f", "--fail", "-sS", "-Ss", "-sL", "-Ls", "-sSL":
		default:
			if strings.HasPrefix(w, "-") {
				return r, fmt.Errorf("unknown curl option %s", w)
			}
			if r.URL != "" {
				return r, fmt.Errorf("two URLs: %s and %s", r.URL, w)
			}
			r.URL = w
		}
	}
	if r.URL == "" {
		return r, errors.New("no URL")
	}
	if jsonFlag {
		if !r.HasHeader("Content-Type") {
			r.Headers = append(r.Headers, Header{Name: "Content-Type", Value: "application/json"})
		}
		if !r.HasHeader("Accept") {
			r.Headers = append(r.Headers, Header{Name: "Accept", Value: "application/json"})
		}
	}
	if len(data) > 0 {
		joined := strings.Join(data, "&")
		if get {
			sep := "?"
			if strings.Contains(r.URL, "?") {
				sep = "&"
			}
			r.URL += sep + joined
		} else {
			r.Body = &joined
			if r.Method == "" {
				r.Method = "POST"
			}
		}
	}
	if r.Method == "" {
		r.Method = "GET"
	}
	return r, nil
}
