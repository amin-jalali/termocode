package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/ai"
)

// What the hub cleans out of every string before it leaves for the page
// (WEB-7, T50). The page builds its DOM from text only; this keeps
// terminal escape sequences and other controls out of it as well.

// webClean keeps tabs and newlines and drops every other C0 and C1
// control and DEL. Invalid UTF-8 becomes U+FFFD. Bidi controls stay:
// file bodies keep them and the page isolates each line.
func webClean(s string) string {
	if webCleanOK(s, false) {
		return s
	}
	return webCleanSlow(s, false)
}

// webName is webClean for names and chat text shown inline: tabs and
// newlines become spaces and the bidi embeddings, overrides and isolates
// are dropped, so a name cannot reorder the UI around it.
func webName(s string) string {
	if webCleanOK(s, true) {
		return s
	}
	return webCleanSlow(s, true)
}

// webCleanOK reports whether s needs no change.
func webCleanOK(s string, name bool) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f || c >= 0x80 {
			if c == '\t' || c == '\n' {
				if name {
					return false
				}
				continue
			}
			if c < 0x80 {
				return false
			}
			r, size := utf8.DecodeRuneInString(s[i:])
			if webDropRune(r, size, name) {
				return false
			}
			i += size - 1
		}
	}
	return true
}

func webCleanSlow(s string, name bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '\t' || r == '\n':
			if name {
				b.WriteByte(' ')
			} else {
				b.WriteRune(r)
			}
		case r == utf8.RuneError && size == 1:
			b.WriteRune(utf8.RuneError)
		case r < 0x20 || r == 0x7f || webDropRune(r, size, name):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// webDropRune: a C1 control, invalid UTF-8, or (for names) a bidi
// embedding, override or isolate.
func webDropRune(r rune, size int, name bool) bool {
	switch {
	case r == utf8.RuneError && size == 1:
		return true // fixed by the slow path
	case r >= 0x80 && r <= 0x9f:
		return true
	case name && (r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069):
		return true
	}
	return false
}

// webData marshals v for the page (WEB-7, T43): every string in it,
// values and keys, passes the registered-secret redaction and webClean.
// Snapshot and delta types are hand-written structs with no secret
// fields; this is the second line. The result is compact JSON without
// HTML escaping; numbers keep their exact text.
func webData(v any) (json.RawMessage, error) {
	b, err := webMarshal(v)
	if err != nil {
		return nil, err
	}
	// Fast path: no escapes in the JSON means every string's bytes are
	// its text, so redacting the whole document is the same as redacting
	// each string; a change (or any escape) takes the careful path.
	if !webJSONHasEscapes(b) {
		s := string(b)
		if ai.RedactSecrets(s) == s {
			return b, nil
		}
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	return webMarshal(webCleanTree(tree))
}

// webText is webClean plus the registered-secret redaction, for the text
// fields of hub messages (an err's msg).
func webText(s string) string { return webClean(ai.RedactSecrets(s)) }

func webMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// webJSONHasEscapes reports a backslash escape, DEL or a C1 control
// (encoding/json writes those raw) anywhere in b.
func webJSONHasEscapes(b []byte) bool {
	for i := 0; i < len(b); i++ {
		switch c := b[i]; {
		case c == '\\' || c == 0x7f:
			return true
		case c == 0xc2 && i+1 < len(b) && b[i+1] >= 0x80 && b[i+1] <= 0x9f:
			return true
		}
	}
	return false
}

// webCleanTree cleans every string of a decoded JSON value.
func webCleanTree(v any) any {
	switch v := v.(type) {
	case string:
		return webText(v)
	case []any:
		for i := range v {
			v[i] = webCleanTree(v[i])
		}
		return v
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[webText(k)] = webCleanTree(x)
		}
		return out
	}
	return v
}
