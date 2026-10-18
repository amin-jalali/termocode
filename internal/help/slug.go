package help

import (
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Slug turns heading text into its anchor the way the website does
// (Python-Markdown's toc extension, default slugify, which MkDocs uses
// with "toc: permalink: true"): NFKD, keep ASCII only, drop everything
// but letters, digits, "_", "-" and spaces, trim, lower case, and turn
// each run of spaces and dashes into one "-".
//
// The text is the heading's plain text: code spans included, markup
// removed. Slug does not make duplicates unique; see Slugs and Slugger.
func Slug(text string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(text) {
		switch {
		case r >= 0x80: // "encode('ascii', 'ignore')"
		case r == '_' || r == '-' || isSpace(r) || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		}
	}
	s := strings.TrimFunc(b.String(), isSpace)
	b.Reset()
	run := false
	for _, r := range s {
		if r == '-' || isSpace(r) {
			if !run {
				b.WriteByte('-')
			}
			run = true
			continue
		}
		run = false
		b.WriteRune(r)
	}
	return b.String()
}

// isSpace is Python's \s for ASCII text.
func isSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

// Slugger hands out the anchors of one page in order: a slug that is
// already taken (or empty) gets "_1", "_2", … as the website's toc does.
// The zero value is ready to use.
type Slugger struct{ used map[string]bool }

// Next returns the unique anchor for the next heading's text.
func (s *Slugger) Next(text string) string {
	if s.used == nil {
		s.used = map[string]bool{}
	}
	id := Slug(text)
	for id == "" || s.used[id] {
		base, n := id, 0
		if i := strings.LastIndexByte(id, '_'); i >= 0 {
			if k, err := strconv.Atoi(id[i+1:]); err == nil && id[i+1:] != "" && isDigits(id[i+1:]) {
				base, n = id[:i], k
			}
		}
		id = base + "_" + strconv.Itoa(n+1)
	}
	s.used[id] = true
	return id
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Slugs returns the anchors of a page's headings, in order.
func Slugs(texts []string) []string {
	var s Slugger
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = s.Next(t)
	}
	return out
}
