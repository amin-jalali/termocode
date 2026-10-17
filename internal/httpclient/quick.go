package httpclient

import (
	"errors"
	"fmt"
	"strings"
)

// ParseQuick reads the "HTTP: Quick Request…" line:
//
//	https://httpbin.org/get                  a bare URL means GET
//	POST https://… {"a":1}                   a { or [ word starts a JSON body
//	PUT https://… -H 'Name: v' [1,2]         -H adds headers (curl words)
//
// A URL without a scheme gets http:// (like curl).
func ParseQuick(line string) (Request, error) {
	line = strings.TrimSpace(line)
	var r Request
	head, body := line, ""
	// The body starts at the first word that begins with { or [.
	inQuote := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inQuote != 0 {
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			inQuote = c
			continue
		}
		if (c == '{' || c == '[') && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') && !strings.HasPrefix(line[i:], "{{") {
			head, body = line[:i], strings.TrimSpace(line[i:])
			break
		}
	}
	words, err := shellWords(head)
	if err != nil {
		return r, err
	}
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case w == "-H" || w == "--header":
			if i+1 >= len(words) {
				return r, errors.New("-H needs 'Name: value'")
			}
			i++
			k, v, ok := strings.Cut(words[i], ":")
			if !ok || strings.TrimSpace(k) == "" {
				return r, fmt.Errorf("bad header %q", words[i])
			}
			r.Headers = append(r.Headers, Header{Name: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
		case strings.HasPrefix(w, "-"):
			return r, fmt.Errorf("unknown option %s (use HTTP: Import from curl… for curl commands)", w)
		case r.URL == "" && r.Method == "" && !LooksLikeURL(w) && isMethodWord(w) && i+1 < len(words):
			r.Method = strings.ToUpper(w)
		case r.URL == "":
			r.URL = w
		default:
			return r, fmt.Errorf("unexpected %q", w)
		}
	}
	switch strings.ToUpper(r.URL) {
	case "", "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return r, errors.New("no URL")
	}
	if !LooksLikeURL(r.URL) {
		r.URL = "http://" + r.URL
	}
	if r.Method == "" {
		r.Method = "GET"
	}
	if body != "" {
		r.Body = &body
	}
	return r, nil
}

func isMethodWord(w string) bool {
	if w == "" || len(w) > 16 {
		return false
	}
	for _, c := range w {
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}
